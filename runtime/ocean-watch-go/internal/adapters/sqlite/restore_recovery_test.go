package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreFailureLeavesNameAvailableForRetry(t *testing.T) {
	ctx := context.Background()
	source := initializedStore(t)
	backup := filepath.Join(t.TempDir(), "private", "backup.db")
	if _, err := source.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	options := Options{Path: filepath.Join(filepath.Dir(backup), "restored.db"), BusyTimeout: 50 * time.Millisecond}
	release, _, err := acquireMaintenance(ctx, options.Path, true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Restore(ctx, backup, options); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected publication to time out: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// Orphaned recovery data might belong to a previous database. Preserve it.
	orphan := options.Path + "-journal"
	if err := createStateFile(orphan); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, backup, options); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("restore ignored preexisting recovery evidence: %v", err)
	}
	if err := os.Remove(orphan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(options.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed restore occupied destination: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(backup), ".restore-*")); err != nil || len(matches) != 0 {
		t.Fatalf("staging artifacts after failure: %v, %v", matches, err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			store, err := Restore(ctx, backup, options)
			if err == nil {
				err = store.Close()
			}
			errs <- err
		}()
	}
	close(start)
	first, second := <-errs, <-errs
	if first != nil {
		first, second = second, first
	}
	if first != nil || !errors.Is(second, os.ErrExist) {
		t.Fatalf("concurrent retry should publish exactly once: %v, %v", first, second)
	}
	store, err := Open(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := checkIntegrity(ctx, store.db); err != nil {
		t.Fatal(err)
	}
}
