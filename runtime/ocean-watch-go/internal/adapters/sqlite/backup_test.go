package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIncludesCommittedWALAndRestoresWithoutOverwriting(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(t, 1, `{"id":"wal-backup"}`)
	if err := store.SavePreflight(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "private", "backup.db")
	record, err := store.Backup(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != SchemaVersion || record.Digest == "" || record.RuntimeVersion == "" || !engineSupported(record.EngineVersion) {
		t.Fatalf("incomplete backup metadata: %#v", record)
	}
	options := Options{Path: filepath.Join(t.TempDir(), "private", "restored.db")}
	restored, err := Restore(ctx, backup, options)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.GetSnapshot(ctx, snapshot.OperationID, 1)
	if err != nil || !sameSnapshot(got, snapshot) {
		t.Fatalf("backup lost committed WAL state: %v", err)
	}
	if _, err := Restore(ctx, backup, options); !errors.Is(err, os.ErrExist) {
		t.Fatalf("restore overwrote existing destination: %v", err)
	}
	if _, err := store.Backup(ctx, backup); !errors.Is(err, os.ErrExist) {
		t.Fatalf("backup overwrote previous backup: %v", err)
	}
}

func TestRestoreRejectsTamperedOrIncompleteBackupBeforeCreatingDestination(t *testing.T) {
	for _, mode := range []string{"missing_manifest", "tampered_database", "future_schema"} {
		t.Run(mode, func(t *testing.T) {
			store := initializedStore(t)
			backup := filepath.Join(t.TempDir(), "private", "backup.db")
			record, err := store.Backup(context.Background(), backup)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing_manifest":
				if err := os.Remove(backup + ".manifest.json"); err != nil {
					t.Fatal(err)
				}
			case "tampered_database":
				file, err := os.OpenFile(backup, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.Write([]byte("tampered"))
				if closeErr := file.Close(); err != nil || closeErr != nil {
					t.Fatal(errors.Join(err, closeErr))
				}
			case "future_schema":
				if err := os.Remove(backup + ".manifest.json"); err != nil {
					t.Fatal(err)
				}
				record.SchemaVersion = SchemaVersion + 1
				if err := writePrivateJSON(backup+".manifest.json", record); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(t.TempDir(), "missing", "restored.db")
			if _, err := Restore(context.Background(), backup, Options{Path: destination}); !errors.Is(err, ErrIncompatible) {
				t.Fatalf("unsafe backup accepted: %v", err)
			}
			if _, err := os.Stat(filepath.Dir(destination)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid backup created destination")
			}
		})
	}
}
