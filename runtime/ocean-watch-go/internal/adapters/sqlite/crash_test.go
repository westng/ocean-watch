package sqlite

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStateRecoversAfterAbruptProcessExit(t *testing.T) {
	for _, mode := range []string{"committed", "uncommitted"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "state.db")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStateCrashHelper$")
			command.Env = []string{"OCEAN_WATCH_TEST_CRASH_MODE=" + mode, "OCEAN_WATCH_TEST_CRASH_PATH=" + path}
			if runtime.GOOS == "windows" {
				command.Env = append(command.Env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("crash helper: %v: %s", err, output)
			}
			store, err := Open(context.Background(), Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.GetSnapshot(context.Background(), "op_sqlite_test", 1); err != nil {
				t.Fatal(err)
			}
			var version, count int
			if err := store.db.QueryRow("SELECT current_version FROM operations WHERE id='op_sqlite_test'").Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow("SELECT count(*) FROM operation_versions").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if version != 1 || count != 1 {
				t.Fatalf("partial transaction survived: current=%d count=%d", version, count)
			}
			var integrity string
			if err := store.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
				t.Fatal(err)
			}
			if integrity != "ok" {
				t.Fatalf("recovered database integrity: %s", integrity)
			}
		})
	}
}

func TestStateCrashHelper(t *testing.T) {
	mode := os.Getenv("OCEAN_WATCH_TEST_CRASH_MODE")
	if mode == "" {
		return
	}
	if mode != "committed" && mode != "uncommitted" {
		t.Fatal("invalid synthetic crash mode")
	}
	store, err := Initialize(context.Background(), Options{Path: os.Getenv("OCEAN_WATCH_TEST_CRASH_PATH"), MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreflight(context.Background(), testSnapshot(t, 1, `{"id":"durable"}`)); err != nil {
		t.Fatal(err)
	}
	if mode == "uncommitted" {
		tx, err := store.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("UPDATE operations SET current_version=2"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO operation_versions
			SELECT operation_id,2,operation_schema_version,runtime_version,input_digest,snapshot_digest,created_at,expires_at
			FROM operation_versions WHERE version=1`); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(0)
}
