package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func (store *Store) ClaimExecution(ctx context.Context, request domain.ClaimExecutionRequest) (domain.ExecutionLease, error) {
	if err := request.Validate(); err != nil {
		return domain.ExecutionLease{}, err
	}
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	defer tx.Rollback()
	if err := checkOperationLegacyBlockers(ctx, tx, request.OperationID); err != nil {
		return domain.ExecutionLease{}, err
	}
	persistedDigest, _, err := readOperationVersion(ctx, tx, request.OperationID, request.Version)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if persistedDigest != request.SnapshotDigest {
		return domain.ExecutionLease{}, fmt.Errorf("%w: snapshot digest mismatch", domain.ErrOperationNotExecutable)
	}
	state, err := readOperationState(ctx, tx, request.OperationID, request.Version)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if state.SnapshotDigest != request.SnapshotDigest {
		return domain.ExecutionLease{}, fmt.Errorf("%w: state digest mismatch", domain.ErrOperationNotExecutable)
	}
	if state.Status != domain.StatusApproved {
		return domain.ExecutionLease{}, fmt.Errorf("%w: status is %s", domain.ErrOperationNotExecutable, state.Status)
	}
	currentLease, active, err := checkClaimLease(ctx, tx, request)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if active {
		return domain.ExecutionLease{}, domain.ErrLeaseHeld
	}
	now := request.Now.UTC()
	lease := domain.ExecutionLease{
		OperationID: request.OperationID, Version: request.Version, OwnerID: request.OwnerID,
		FencingEpoch: 1, AcquiredAt: now, ExpiresAt: now.Add(request.TTL), RenewedAt: now,
	}
	if !active && currentLease.FencingEpoch > 0 {
		lease.FencingEpoch = currentLease.FencingEpoch + 1
	}
	var approvalIssued, approvalExpires, approvalStatus string
	err = tx.QueryRowContext(ctx, `SELECT issued_at, expires_at, status FROM approvals
		WHERE id=? AND operation_id=? AND operation_version=? AND snapshot_digest=?`, request.ApprovalID,
		request.OperationID, request.Version, request.SnapshotDigest).Scan(&approvalIssued, &approvalExpires, &approvalStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, domain.ErrApprovalNotFound
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if approvalStatus != string(domain.ApprovalValid) {
		if approvalStatus == string(domain.ApprovalConsumed) {
			return domain.ExecutionLease{}, domain.ErrApprovalConsumed
		}
		return domain.ExecutionLease{}, domain.ErrApprovalExpired
	}
	issuedAt, err := parseOperationTime(approvalIssued)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	expiresAt, err := parseOperationTime(approvalExpires)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if now.Before(issuedAt) || !now.Before(expiresAt) {
		return domain.ExecutionLease{}, domain.ErrApprovalExpired
	}
	if currentLease.FencingEpoch > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE execution_leases SET owner_id=?, fencing_epoch=?, expires_at=?, renewed_at=?
			WHERE operation_id=? AND operation_version=? AND fencing_epoch=?`, lease.OwnerID, lease.FencingEpoch,
			operationTime(lease.ExpiresAt), operationTime(lease.RenewedAt), lease.OperationID, lease.Version, currentLease.FencingEpoch)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_leases(operation_id, operation_version, owner_id, fencing_epoch, acquired_at, expires_at, renewed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, lease.OperationID, lease.Version, lease.OwnerID, lease.FencingEpoch,
			operationTime(lease.AcquiredAt), operationTime(lease.ExpiresAt), operationTime(lease.RenewedAt))
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status='consumed', consumed_at=?
		WHERE id=? AND operation_id=? AND operation_version=? AND snapshot_digest=? AND status='valid'`,
		operationTime(now), request.ApprovalID, request.OperationID, request.Version, request.SnapshotDigest)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return domain.ExecutionLease{}, err
		}
		return domain.ExecutionLease{}, domain.ErrApprovalConsumed
	}
	result, err = tx.ExecContext(ctx, `UPDATE operation_states SET status='executing', updated_at=?
		WHERE operation_id=? AND operation_version=? AND status='approved'`, operationTime(now), request.OperationID, request.Version)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return domain.ExecutionLease{}, err
		}
		return domain.ExecutionLease{}, domain.ErrInvalidTransition
	}
	if err := lease.Validate(); err != nil {
		return domain.ExecutionLease{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ExecutionLease{}, err
	}
	return lease, nil
}

func (store *Store) TakeOverExecution(ctx context.Context, operationID string, version int, ownerID string, now time.Time, ttl time.Duration) (domain.ExecutionLease, error) {
	if operationID == "" || version < 1 || ownerID == "" || now.IsZero() || ttl <= 0 || ttl > 24*time.Hour {
		return domain.ExecutionLease{}, domain.ErrInvalidOperation
	}
	if !domainOperationID(operationID) {
		return domain.ExecutionLease{}, domain.ErrInvalidOperation
	}
	if err := validateOwnerID(ownerID); err != nil {
		return domain.ExecutionLease{}, err
	}
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	defer tx.Rollback()
	state, err := readOperationState(ctx, tx, operationID, version)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if state.Status != domain.StatusExecuting && state.Status != domain.StatusUnknown && state.Status != domain.StatusPartiallySucceeded && state.Status != domain.StatusReconciling {
		return domain.ExecutionLease{}, fmt.Errorf("%w: status is %s", domain.ErrOperationNotExecutable, state.Status)
	}
	current, err := readLease(ctx, tx, operationID, version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	now = now.UTC()
	if now.Before(current.ExpiresAt) {
		return domain.ExecutionLease{}, domain.ErrLeaseHeld
	}
	lease := domain.ExecutionLease{
		OperationID: operationID, Version: version, OwnerID: ownerID,
		FencingEpoch: current.FencingEpoch + 1, AcquiredAt: current.AcquiredAt,
		ExpiresAt: now.Add(ttl), RenewedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dispatch_attempts SET state='unknown', finished_at=?
		WHERE operation_id=? AND operation_version=? AND state='intent'`, operationTime(now), operationID, version); err != nil {
		return domain.ExecutionLease{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operation_items SET status='unknown', updated_at=?
		WHERE operation_id=? AND operation_version=? AND status='intent'`, operationTime(now), operationID, version); err != nil {
		return domain.ExecutionLease{}, err
	}
	var unknownCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_items WHERE operation_id=? AND operation_version=? AND status='unknown'`, operationID, version).Scan(&unknownCount); err != nil {
		return domain.ExecutionLease{}, err
	}
	if unknownCount > 0 && state.Status == domain.StatusExecuting {
		if _, err := tx.ExecContext(ctx, `UPDATE operation_states SET status='unknown', updated_at=?
			WHERE operation_id=? AND operation_version=? AND status='executing'`, operationTime(now), operationID, version); err != nil {
			return domain.ExecutionLease{}, err
		}
		state.Status = domain.StatusUnknown
	}
	if state.Status == domain.StatusUnknown || state.Status == domain.StatusPartiallySucceeded {
		if _, err := tx.ExecContext(ctx, `UPDATE operation_states SET status='reconciling', updated_at=?
			WHERE operation_id=? AND operation_version=? AND status IN ('unknown', 'partially_succeeded')`, operationTime(now), operationID, version); err != nil {
			return domain.ExecutionLease{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE execution_leases SET owner_id=?, fencing_epoch=?, expires_at=?, renewed_at=?
		WHERE operation_id=? AND operation_version=? AND fencing_epoch=? AND expires_at<=?`,
		lease.OwnerID, lease.FencingEpoch, operationTime(lease.ExpiresAt), operationTime(lease.RenewedAt), operationID, version,
		current.FencingEpoch, operationTime(now))
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return domain.ExecutionLease{}, err
		}
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	if err := lease.Validate(); err != nil {
		return domain.ExecutionLease{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ExecutionLease{}, err
	}
	return lease, nil
}

func (store *Store) RenewExecutionLease(ctx context.Context, lease domain.ExecutionLease, now time.Time, ttl time.Duration) (domain.ExecutionLease, error) {
	if err := lease.Validate(); err != nil {
		return domain.ExecutionLease{}, err
	}
	if now.IsZero() || ttl <= 0 || ttl > 24*time.Hour {
		return domain.ExecutionLease{}, domain.ErrInvalidOperation
	}
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	defer tx.Rollback()
	current, err := readLease(ctx, tx, lease.OperationID, lease.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if current.OwnerID != lease.OwnerID || current.FencingEpoch != lease.FencingEpoch {
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	now = now.UTC()
	if !now.Before(current.ExpiresAt) {
		return domain.ExecutionLease{}, domain.ErrLeaseExpired
	}
	updated := current
	updated.RenewedAt = now
	updated.ExpiresAt = now.Add(ttl)
	result, err := tx.ExecContext(ctx, `UPDATE execution_leases SET expires_at=?, renewed_at=?
		WHERE operation_id=? AND operation_version=? AND owner_id=? AND fencing_epoch=? AND expires_at>?`,
		operationTime(updated.ExpiresAt), operationTime(updated.RenewedAt), updated.OperationID, updated.Version,
		updated.OwnerID, updated.FencingEpoch, operationTime(now))
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return domain.ExecutionLease{}, err
		}
		return domain.ExecutionLease{}, domain.ErrLeaseExpired
	}
	if err := tx.Commit(); err != nil {
		return domain.ExecutionLease{}, err
	}
	return updated, nil
}

func (store *Store) GetOperationState(ctx context.Context, operationID string, version int) (domain.OperationState, error) {
	if err := store.ensureIdentity(); err != nil {
		return domain.OperationState{}, err
	}
	if operationID == "" || version < 1 {
		return domain.OperationState{}, domain.ErrInvalidOperation
	}
	return readOperationState(ctx, store.db, operationID, version)
}
