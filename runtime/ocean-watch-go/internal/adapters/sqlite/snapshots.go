package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

var (
	ErrSnapshotConflict = errors.New("operation snapshot conflicts with persisted version")
	ErrSnapshotNotFound = errors.New("operation snapshot was not found")
)

var _ state.Snapshots = (*Store)(nil)

func (store *Store) SavePreflight(ctx context.Context, snapshot operations.Snapshot) error {
	if err := store.ensureIdentity(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind, channel, subject, status string
	var current int
	err = tx.QueryRowContext(ctx, "SELECT kind, channel, subject_id, status, current_version FROM operations WHERE id=?", snapshot.OperationID).Scan(&kind, &channel, &subject, &status, &current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if snapshot.Version != 1 {
			return ErrSnapshotConflict
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO operations(id,kind,channel,subject_id,status,current_version,created_at,updated_at) VALUES(?,?,?,?, 'preflight_ready',?,?,?)", snapshot.OperationID, snapshot.Kind, snapshot.Channel, snapshot.SubjectID, snapshot.Version, snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), snapshot.CreatedAt.UTC().Format(time.RFC3339Nano))
	case err != nil:
		return err
	default:
		if kind != snapshot.Kind || channel != snapshot.Channel || subject != snapshot.SubjectID || status != "preflight_ready" {
			return ErrSnapshotConflict
		}
		if snapshot.Version <= current {
			previous, err := readSnapshot(ctx, tx, snapshot.OperationID, snapshot.Version)
			if err != nil {
				return err
			}
			if !sameSnapshot(previous, snapshot) {
				return ErrSnapshotConflict
			}
			if err := ensurePreflightState(ctx, tx, snapshot); err != nil {
				return err
			}
			return nil
		}
		if snapshot.Version != current+1 {
			return ErrSnapshotConflict
		}
		previous, err := readSnapshot(ctx, tx, snapshot.OperationID, current)
		if err != nil {
			return err
		}
		if snapshot.CreatedAt.Before(previous.CreatedAt) {
			return ErrSnapshotConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE operations SET current_version=?,updated_at=? WHERE id=? AND current_version=?", snapshot.Version, snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), snapshot.OperationID, current)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO operation_versions(operation_id,version,operation_schema_version,runtime_version,input_digest,snapshot_digest,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)", snapshot.OperationID, snapshot.Version, snapshot.SchemaVersion, snapshot.RuntimeVersion, snapshot.InputDigest, snapshot.SnapshotDigest, snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO preflight_snapshots(operation_id,operation_version,payload_json) VALUES(?,?,?)", snapshot.OperationID, snapshot.Version, string(snapshot.Payload)); err != nil {
		return err
	}
	if err := ensurePreflightState(ctx, tx, snapshot); err != nil {
		return err
	}
	return tx.Commit()
}

func ensurePreflightState(ctx context.Context, tx *sql.Tx, snapshot operations.Snapshot) error {
	var table string
	if err := tx.QueryRowContext(ctx, "SELECT name FROM sqlite_schema WHERE type='table' AND name='operation_states'").Scan(&table); errors.Is(err, sql.ErrNoRows) {
		// Schema version 1 predates the operation state table and is kept
		// readable until the explicit upgrade path runs.
		return nil
	} else if err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO operation_states(
		operation_id, operation_version, status, snapshot_digest, created_at, updated_at
	) VALUES(?, ?, 'preflight_ready', ?, ?, ?)`,
		snapshot.OperationID, snapshot.Version, snapshot.SnapshotDigest,
		snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), snapshot.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (store *Store) GetSnapshot(ctx context.Context, operationID string, version int) (operations.Snapshot, error) {
	if err := store.ensureIdentity(); err != nil {
		return operations.Snapshot{}, err
	}
	return readSnapshot(ctx, store.db, operationID, version)
}

func readSnapshot(ctx context.Context, db queryer, operationID string, version int) (operations.Snapshot, error) {
	var result operations.Snapshot
	var created, expires, payload string
	err := db.QueryRowContext(ctx, `SELECT o.id, v.version, o.kind, o.channel, o.subject_id,
		v.runtime_version, v.operation_schema_version, v.input_digest, v.snapshot_digest,
		v.created_at, v.expires_at, s.payload_json
		FROM operations o JOIN operation_versions v ON v.operation_id=o.id
		JOIN preflight_snapshots s ON s.operation_id=v.operation_id AND s.operation_version=v.version
		WHERE o.id=? AND v.version=?`, operationID, version).Scan(
		&result.OperationID, &result.Version, &result.Kind, &result.Channel, &result.SubjectID,
		&result.RuntimeVersion, &result.SchemaVersion, &result.InputDigest, &result.SnapshotDigest,
		&created, &expires, &payload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return operations.Snapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return operations.Snapshot{}, err
	}
	result.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return operations.Snapshot{}, ErrIncompatible
	}
	result.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return operations.Snapshot{}, ErrIncompatible
	}
	result.Payload = []byte(payload)
	if err := result.Validate(); err != nil {
		return operations.Snapshot{}, ErrIncompatible
	}
	return result, nil
}

func sameSnapshot(left, right operations.Snapshot) bool {
	return left.OperationID == right.OperationID && left.Version == right.Version &&
		left.Kind == right.Kind && left.Channel == right.Channel && left.SubjectID == right.SubjectID &&
		left.RuntimeVersion == right.RuntimeVersion && left.SchemaVersion == right.SchemaVersion &&
		left.InputDigest == right.InputDigest && left.SnapshotDigest == right.SnapshotDigest &&
		left.CreatedAt.Equal(right.CreatedAt) && left.ExpiresAt.Equal(right.ExpiresAt)
}
