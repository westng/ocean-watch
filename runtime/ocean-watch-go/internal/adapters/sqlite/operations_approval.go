package sqlite

import (
	"context"
	"errors"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

var _ state.OperationRepository = (*Store)(nil)

func (store *Store) MarkAwaitingApproval(ctx context.Context, operationID string, version int, snapshotDigest string, now time.Time) error {
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	snapshot, err := currentOperationSnapshot(ctx, tx, operationID, version, now)
	if err != nil {
		return err
	}
	if snapshot.SnapshotDigest != snapshotDigest {
		return domain.ErrOperationNotExecutable
	}
	current, err := readOperationState(ctx, tx, operationID, version)
	if err != nil {
		return err
	}
	if current.Status == domain.StatusAwaitingApproval || current.Status == domain.StatusApproved {
		return store.commitOperationTx(tx)
	}
	if err := domain.ValidateTransition(current.Status, domain.StatusAwaitingApproval); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operation_states SET status='awaiting_approval', updated_at=? WHERE operation_id=? AND operation_version=? AND status='preflight_ready'`, operationTime(now), operationID, version); err != nil {
		return err
	}
	return store.commitOperationTx(tx)
}

func (store *Store) SaveApproval(ctx context.Context, verified approval.VerifiedApproval, now time.Time) error {
	tx, err := store.beginOperationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	record, err := verified.Record()
	if err != nil {
		return err
	}
	if !record.ValidAt(now) {
		return domain.ErrApprovalExpired
	}
	snapshot, err := currentOperationSnapshot(ctx, tx, record.OperationID, record.Version, now)
	if err != nil {
		return err
	}
	scope, err := domain.ApprovalScopeDigest(snapshot)
	if err != nil {
		return err
	}
	if snapshot.SnapshotDigest != record.SnapshotDigest || scope != record.ScopeDigest || record.IssuedAt.Before(snapshot.CreatedAt) || record.ExpiresAt.After(snapshot.ExpiresAt) {
		return domain.ErrOperationNotExecutable
	}
	var replay int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM approvals WHERE id=? OR nonce_hash=?", record.ID, record.NonceHash).Scan(&replay); err != nil {
		return err
	}
	if replay != 0 {
		return domain.ErrApprovalReplay
	}
	current, err := readOperationState(ctx, tx, record.OperationID, record.Version)
	if err != nil {
		return err
	}
	if current.Status != domain.StatusAwaitingApproval {
		return domain.ErrOperationNotExecutable
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approvals(id,operation_id,operation_version,snapshot_digest,scope_digest,provider_id,adapter_id,host_version,evidence_hash,nonce_hash,source_reference,issued_at,expires_at,status)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'valid')`, record.ID, record.OperationID, record.Version, record.SnapshotDigest, record.ScopeDigest, record.ProviderID, record.AdapterID, record.HostVersion, record.EvidenceHash, record.NonceHash, record.SourceReference, operationTime(record.IssuedAt), operationTime(record.ExpiresAt)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE operation_states SET status='approved',updated_at=? WHERE operation_id=? AND operation_version=? AND status='awaiting_approval'", operationTime(now), record.OperationID, record.Version)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return errors.Join(domain.ErrInvalidTransition, err)
	}
	return store.commitOperationTx(tx)
}
