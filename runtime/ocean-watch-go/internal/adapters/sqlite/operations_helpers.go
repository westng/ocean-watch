package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func (store *Store) beginOperationTx(ctx context.Context) (*sql.Tx, error) {
	if err := store.ensureIdentity(); err != nil {
		return nil, err
	}
	return store.db.BeginTx(ctx, nil)
}

func operationTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOperationTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid operation timestamp", ErrIncompatible)
	}
	return parsed.UTC(), nil
}

func readOperationVersion(ctx context.Context, db queryer, operationID string, version int) (string, time.Time, error) {
	var digest, created string
	err := db.QueryRowContext(ctx, "SELECT snapshot_digest, created_at FROM operation_versions WHERE operation_id=? AND version=?", operationID, version).Scan(&digest, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, domain.ErrOperationNotFound
	}
	if err != nil {
		return "", time.Time{}, err
	}
	createdAt, err := parseOperationTime(created)
	if err != nil {
		return "", time.Time{}, err
	}
	return digest, createdAt, nil
}

func readOperationState(ctx context.Context, db queryer, operationID string, version int) (domain.OperationState, error) {
	var state domain.OperationState
	var status string
	var created, updated string
	err := db.QueryRowContext(ctx, `SELECT operation_id, operation_version, status, snapshot_digest, created_at, updated_at
		FROM operation_states WHERE operation_id=? AND operation_version=?`, operationID, version).Scan(
		&state.OperationID, &state.Version, &status, &state.SnapshotDigest, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OperationState{}, domain.ErrOperationNotFound
	}
	if err != nil {
		return domain.OperationState{}, err
	}
	state.Status = domain.Status(status)
	state.CreatedAt, err = parseOperationTime(created)
	if err != nil {
		return domain.OperationState{}, err
	}
	state.UpdatedAt, err = parseOperationTime(updated)
	if err != nil {
		return domain.OperationState{}, err
	}
	if err := state.Validate(); err != nil {
		return domain.OperationState{}, fmt.Errorf("%w: persisted operation state: %v", ErrIncompatible, err)
	}
	return state, nil
}

func readLease(ctx context.Context, db queryer, operationID string, version int) (domain.ExecutionLease, error) {
	var lease domain.ExecutionLease
	var acquired, expires, renewed string
	err := db.QueryRowContext(ctx, `SELECT operation_id, operation_version, owner_id, fencing_epoch, acquired_at, expires_at, renewed_at
		FROM execution_leases WHERE operation_id=? AND operation_version=?`, operationID, version).Scan(
		&lease.OperationID, &lease.Version, &lease.OwnerID, &lease.FencingEpoch, &acquired, &expires, &renewed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	lease.AcquiredAt, err = parseOperationTime(acquired)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	lease.ExpiresAt, err = parseOperationTime(expires)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	lease.RenewedAt, err = parseOperationTime(renewed)
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	if err := lease.Validate(); err != nil {
		return domain.ExecutionLease{}, fmt.Errorf("%w: persisted execution lease: %v", ErrIncompatible, err)
	}
	return lease, nil
}

func checkLease(ctx context.Context, db queryer, request domain.DispatchIntentRequest) (domain.ExecutionLease, error) {
	lease, err := readLease(ctx, db, request.OperationID, request.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	if err != nil {
		return domain.ExecutionLease{}, err
	}
	now := request.Now.UTC()
	if lease.OwnerID != request.OwnerID || lease.FencingEpoch != request.FencingEpoch {
		return domain.ExecutionLease{}, domain.ErrStaleEpoch
	}
	if !now.Before(lease.ExpiresAt) {
		return domain.ExecutionLease{}, domain.ErrLeaseExpired
	}
	return lease, nil
}

func checkClaimLease(ctx context.Context, db queryer, request domain.ClaimExecutionRequest) (domain.ExecutionLease, bool, error) {
	lease, err := readLease(ctx, db, request.OperationID, request.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutionLease{}, false, nil
	}
	if err != nil {
		return domain.ExecutionLease{}, false, err
	}
	if request.Now.UTC().Before(lease.ExpiresAt) {
		return lease, true, nil
	}
	return lease, false, nil
}

func mapDispatchItemStatus(state domain.DispatchState) domain.ItemStatus {
	switch state {
	case domain.DispatchNotSent:
		return domain.ItemNotSent
	case domain.DispatchAcknowledged:
		return domain.ItemAcknowledged
	case domain.DispatchUnknown:
		return domain.ItemUnknown
	default:
		return domain.ItemIntent
	}
}
