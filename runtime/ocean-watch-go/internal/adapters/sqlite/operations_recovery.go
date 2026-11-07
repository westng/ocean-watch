package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"time"
)

func (s *Store) ReadDispatches(ctx context.Context, id string, version int) ([]domain.DispatchRecord, error) {
	if err := s.ensureIdentity(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.item_id, a.state, COALESCE(r.object_id,NULLIF(a.object_id,''),''), r.observed_at
	FROM operation_items i JOIN dispatch_attempts a ON a.operation_id=i.operation_id AND a.operation_version=i.operation_version AND a.item_id=i.item_id
	LEFT JOIN operation_readbacks r ON r.operation_id=i.operation_id AND r.operation_version=i.operation_version AND r.item_id=i.item_id
	WHERE i.operation_id=? AND i.operation_version=? AND a.attempt_no=(SELECT MAX(b.attempt_no) FROM dispatch_attempts b WHERE b.operation_id=i.operation_id AND b.operation_version=i.operation_version AND b.item_id=i.item_id)
	ORDER BY i.item_id`, id, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.DispatchRecord{}
	for rows.Next() {
		var item domain.DispatchRecord
		var observed *string
		if err := rows.Scan(&item.ItemID, &item.State, &item.ObjectID, &observed); err != nil {
			return nil, err
		}
		if observed != nil {
			item.ObservedAt, err = parseOperationTime(*observed)
			if err != nil {
				return nil, err
			}
			item.Applied = true
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) SaveReadback(ctx context.Context, r domain.Readback) error {
	if err := r.Validate(); err != nil {
		return err
	}
	tx, err := s.beginOperationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := checkLease(ctx, tx, domain.DispatchIntentRequest{OperationID: r.Lease.OperationID, Version: r.Lease.Version, OwnerID: r.Lease.OwnerID, FencingEpoch: r.Lease.FencingEpoch, Now: r.Now}); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM operation_items WHERE operation_id=? AND operation_version=? AND item_id=?`, r.Lease.OperationID, r.Lease.Version, r.ItemID).Scan(&status); err != nil {
		return err
	}
	if status != "acknowledged" && status != "unknown" && status != "intent" {
		return domain.ErrInvalidDispatch
	}
	var target string
	err = tx.QueryRowContext(ctx, `SELECT object_id FROM operation_readbacks WHERE operation_id=? AND operation_version=? AND item_id=?`, r.Lease.OperationID, r.Lease.Version, r.ItemID).Scan(&target)
	if err == nil && target != r.ObjectID {
		return domain.ErrInvalidDispatch
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operation_readbacks(operation_id,operation_version,item_id,object_id,evidence_digest,observed_at) VALUES(?,?,?,?,?,?)
	ON CONFLICT(operation_id,operation_version,item_id) DO NOTHING`, r.Lease.OperationID, r.Lease.Version, r.ItemID, r.ObjectID, r.EvidenceDigest, operationTime(r.Now))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Finish derives status from durable facts. No caller can claim success or
// convert an unknown dispatch into a retryable failure. Releasing this lease
// permits a later read-only reconciliation, never an automatic continuation.
func (s *Store) FinishExecution(ctx context.Context, lease domain.ExecutionLease, now time.Time) (domain.Status, error) {
	tx, err := s.beginOperationTx(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := checkLease(ctx, tx, domain.DispatchIntentRequest{OperationID: lease.OperationID, Version: lease.Version, OwnerID: lease.OwnerID, FencingEpoch: lease.FencingEpoch, Now: now}); err != nil {
		return "", err
	}
	snapshot, err := readSnapshot(ctx, tx, lease.OperationID, lease.Version)
	if err != nil {
		return "", err
	}
	var payload struct {
		Items []domain.ConfirmationItem `json:"items"`
	}
	if json.Unmarshal(snapshot.Payload, &payload) != nil || len(payload.Items) == 0 {
		return "", domain.ErrInvalidSnapshot
	}
	var applied, uncertain int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_readbacks WHERE operation_id=? AND operation_version=?`, lease.OperationID, lease.Version).Scan(&applied); err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_items i LEFT JOIN operation_readbacks r ON r.operation_id=i.operation_id AND r.operation_version=i.operation_version AND r.item_id=i.item_id WHERE i.operation_id=? AND i.operation_version=? AND i.status IN ('intent','unknown','acknowledged') AND r.item_id IS NULL`, lease.OperationID, lease.Version).Scan(&uncertain); err != nil {
		return "", err
	}
	status := domain.StatusFailed
	if applied == len(payload.Items) {
		status = domain.StatusSucceeded
	} else if uncertain > 0 {
		status = domain.StatusUnknown
	} else if applied > 0 {
		status = domain.StatusPartiallySucceeded
	}
	current, err := readOperationState(ctx, tx, lease.OperationID, lease.Version)
	if err != nil {
		return "", err
	}
	if current.Status == domain.StatusUnknown || current.Status == domain.StatusPartiallySucceeded {
		if _, err = tx.ExecContext(ctx, `UPDATE operation_states SET status='reconciling',updated_at=? WHERE operation_id=? AND operation_version=?`, operationTime(now), lease.OperationID, lease.Version); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE operation_states SET status=?,updated_at=? WHERE operation_id=? AND operation_version=?`, string(status), operationTime(now), lease.OperationID, lease.Version); err != nil {
		return "", err
	}
	expires := now
	if !expires.After(lease.AcquiredAt) {
		expires = lease.AcquiredAt.Add(time.Nanosecond)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execution_leases SET expires_at=? WHERE operation_id=? AND operation_version=?`, operationTime(expires), lease.OperationID, lease.Version); err != nil {
		return "", err
	}
	return status, tx.Commit()
}
