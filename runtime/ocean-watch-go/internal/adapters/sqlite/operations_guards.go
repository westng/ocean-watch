package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func currentOperationSnapshot(ctx context.Context, tx *sql.Tx, id string, version int, now time.Time) (domain.Snapshot, error) {
	var current int
	if err := tx.QueryRowContext(ctx, "SELECT current_version FROM operations WHERE id=?", id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Snapshot{}, domain.ErrOperationNotFound
		}
		return domain.Snapshot{}, err
	}
	if current != version {
		return domain.Snapshot{}, domain.ErrOperationNotExecutable
	}
	snapshot, err := readSnapshot(ctx, tx, id, version)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if now.IsZero() || now.Before(snapshot.CreatedAt) || !now.Before(snapshot.ExpiresAt) {
		return domain.Snapshot{}, domain.ErrApprovalExpired
	}
	return snapshot, nil
}

func checkOperationLegacyBlockers(ctx context.Context, tx *sql.Tx, id string) error {
	var scope legacy.Scope
	if err := tx.QueryRowContext(ctx, "SELECT channel, subject_id FROM operations WHERE id=?", id).Scan(&scope.Channel, &scope.AdvertiserID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT source_root, source_record FROM legacy_mappings ORDER BY operation_id")
	if err != nil {
		return err
	}
	var sources [][2]string
	for rows.Next() {
		var source [2]string
		if err := rows.Scan(&source[0], &source[1]); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, source)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, source := range sources {
		mapping, err := readLegacyMapping(ctx, tx, source[0], source[1])
		if err != nil {
			return errors.Join(legacy.ErrWriteBlocked, err)
		}
		for _, blocker := range mapping.Blockers() {
			if !blocker.Scope.Known() || blocker.Scope == scope {
				return legacy.ErrWriteBlocked
			}
		}
	}
	return nil
}

func (store *Store) commitOperationTx(tx *sql.Tx) error {
	if err := store.ensureIdentity(); err != nil {
		return err
	}
	return tx.Commit()
}
