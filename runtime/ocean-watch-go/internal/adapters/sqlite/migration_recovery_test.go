package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Model a process exit at the durable boundary between SQL commit and record publication.
func TestUpgradeRepairsCommittedRecordWithoutReapplying(t *testing.T) {
	ctx := context.Background()
	old := versionOneStore(t)
	from := SchemaVersion - 1
	steps, err := migrationSteps()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.applyMigrations(ctx, steps[:from], "previous_runtime", "fixture"); err != nil {
		t.Fatal(err)
	}
	id := "migration_00112233445566778899aabbccddeeff"
	backup := filepath.Join(filepath.Dir(old.path), "backups", id+".db")
	if _, err := old.backup(ctx, backup, from); err != nil {
		t.Fatal(err)
	}
	if err := old.applyMigrations(ctx, steps, id, filepath.Base(backup)); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	options := Options{Path: old.path, RuntimeVersion: "retry-runtime"}
	path := backup + ".migration.json"
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Upgrade(ctx, options); !errors.Is(err, ErrMigrationCommitted) {
		t.Fatalf("record publication failed after commit: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "interrupted", "legacy_timestamp"} {
		t.Run(mode, func(t *testing.T) {
			var existing MigrationRecord
			if mode == "interrupted" {
				if err := os.WriteFile(path, []byte(`{"id":`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if mode == "legacy_timestamp" {
				if err := readPrivateJSON(path, &existing); err != nil {
					t.Fatal(err)
				}
				existing.CompletedAt = existing.CompletedAt.Add(time.Minute)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := writePrivateJSON(path, existing); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Upgrade(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var count int
			if err := store.db.QueryRow("SELECT count(*) FROM migration_attempts WHERE from_version>0").Scan(&count); err != nil || count != 1 {
				t.Fatalf("migration was repeated: %d, %v", count, err)
			}
			var record MigrationRecord
			if err := readPrivateJSON(path, &record); err != nil || record.ID != id || record.FromVersion != from || record.ToVersion != SchemaVersion || record.RuntimeVersion != old.runtimeVersion || record.Result != "succeeded" || record.Backup != filepath.Base(backup) || record.CompletedAt.IsZero() {
				t.Fatalf("repaired record: %+v, %v", record, err)
			}
			if mode == "legacy_timestamp" && !record.CompletedAt.Equal(existing.CompletedAt) {
				t.Fatal("valid historical record was rewritten")
			}
			if _, err := store.GetSnapshot(ctx, "op_sqlite_test", 1); err != nil {
				t.Fatal(err)
			}
		})
	}
}
