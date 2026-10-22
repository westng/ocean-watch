package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestMultiProcessInitializeWriteAndCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.db")
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, 5)
	for index := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStateMultiProcessHelper$")
			command.Env = []string{"OCEAN_WATCH_TEST_PROCESS_PATH=" + path, fmt.Sprintf("OCEAN_WATCH_TEST_PROCESS_INDEX=%d", index)}
			if runtime.GOOS == "windows" {
				command.Env = append(command.Env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
			}
			output, err := command.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("process %d: %w: %s", index, err, output)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	store, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM operation_versions").Scan(&count); err != nil || count != 50 {
		t.Fatalf("multiprocess writes count=%d err=%v", count, err)
	}
	if err := checkIntegrity(ctx, store.db); err != nil {
		t.Fatal(err)
	}
}

func TestStateMultiProcessHelper(t *testing.T) {
	path := os.Getenv("OCEAN_WATCH_TEST_PROCESS_PATH")
	if path == "" {
		return
	}
	store, err := Initialize(context.Background(), Options{Path: path, BusyTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := range 10 {
		snapshot := testSnapshot(t, 1, `{"id":"multiprocess"}`)
		snapshot.OperationID = fmt.Sprintf("op_process_%s_%d", os.Getenv("OCEAN_WATCH_TEST_PROCESS_INDEX"), index)
		if err := store.SavePreflight(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
		var busy, logged, checkpointed int
		if err := store.db.QueryRow("PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logged, &checkpointed); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShortBusyTimeoutFailsAtomicallyAndCanRetry(t *testing.T) {
	store := initializedStore(t)
	other, err := Open(context.Background(), Options{Path: store.path, BusyTimeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	snapshot := testSnapshot(t, 1, `{"id":"busy"}`)
	start := time.Now()
	if err := other.SavePreflight(context.Background(), snapshot); err == nil {
		t.Fatal("contended writer ignored busy timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatal("busy timeout was not bounded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := other.GetSnapshot(context.Background(), snapshot.OperationID, 1); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("failed writer left partial snapshot: %v", err)
	}
	if err := other.SavePreflight(context.Background(), snapshot); err != nil {
		t.Fatalf("retry after lock released: %v", err)
	}
}
