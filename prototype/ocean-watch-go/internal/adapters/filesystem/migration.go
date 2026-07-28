package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type MigrationJournalStore struct {
	Root        string
	LockTimeout time.Duration
}

func (store MigrationJournalStore) Acquire(ctx context.Context) (func() error, error) {
	lock, err := AcquireLock(ctx, filepath.Join(store.Root, "migration", "migration.lock"), store.LockTimeout)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}

func (store MigrationJournalStore) Read(ctx context.Context) (map[string]any, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	value, err := readJSONObject(filepath.Join(store.Root, "migration", "journal.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (store MigrationJournalStore) Write(ctx context.Context, value map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return AtomicWritePrivateJSON(filepath.Join(store.Root, "migration", "journal.json"), value)
}
