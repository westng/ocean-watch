package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func versionOneStore(t *testing.T) *Store {
	t.Helper()
	path, err := preparePath(filepath.Join(t.TempDir(), "private", "state.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), url.Values{"mode": {"rw"}, "_txlock": {"immediate"}, "_pragma": {"foreign_keys(1)"}}))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: path, runtimeVersion: "fixture-v1"}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	if err := store.migrate(context.Background(), migrationBytes()); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreflight(context.Background(), testSnapshot(t, 1, `{"id":"before-upgrade"}`)); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestExplicitUpgradeBacksUpAndPreservesState(t *testing.T) {
	old := versionOneStore(t)
	path := old.path
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Open(ctx, Options{Path: path}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("old schema silently opened: %v", err)
	}
	if _, err := Initialize(ctx, Options{Path: path}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("old schema silently migrated: %v", err)
	}
	store, err := Upgrade(ctx, Options{Path: path, RuntimeVersion: "runtime-upgrade-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.GetSnapshot(ctx, "op_sqlite_test", 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != SchemaVersion {
		t.Fatalf("upgrade history count=%d err=%v", count, err)
	}
	var runtime, result, backupRef string
	if err := store.db.QueryRow("SELECT source_runtime,result,backup_reference FROM schema_migrations WHERE version=?", SchemaVersion).Scan(&runtime, &result, &backupRef); err != nil {
		t.Fatal(err)
	}
	if runtime != "runtime-upgrade-test" || result != "succeeded" || backupRef == "initial_creation" {
		t.Fatalf("missing migration provenance: %q %q %q", runtime, result, backupRef)
	}
	backup := filepath.Join(filepath.Dir(path), "backups", backupRef)
	record, err := readBackupRecord(backup + ".manifest.json")
	if err != nil || record.SchemaVersion != 1 {
		t.Fatalf("pre-upgrade backup missing: %#v %v", record, err)
	}
	payload, err := os.ReadFile(backup + ".migration.json")
	if err != nil {
		t.Fatal(err)
	}
	var attempt MigrationRecord
	if err := json.Unmarshal(payload, &attempt); err != nil || attempt.Result != "succeeded" || attempt.FromVersion != 1 || attempt.ToVersion != SchemaVersion {
		t.Fatalf("migration result: %#v %v", attempt, err)
	}
	restored, err := Restore(ctx, backup, Options{Path: filepath.Join(t.TempDir(), "private", "recovered.db"), RuntimeVersion: "runtime-restore-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.GetSnapshot(ctx, "op_sqlite_test", 1); err != nil {
		t.Fatalf("pre-upgrade backup cannot recover: %v", err)
	}
	if err := checkIntegrity(ctx, store.db); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedUpgradeRollsBackAllNumberedSteps(t *testing.T) {
	store := versionOneStore(t)
	ctx := context.Background()
	backup := filepath.Join(filepath.Dir(store.path), "backups", "before.db")
	if _, err := store.backup(ctx, backup, 1); err != nil {
		t.Fatal(err)
	}
	steps, err := migrationSteps()
	if err != nil {
		t.Fatal(err)
	}
	steps[2].payload = []byte("CREATE TABLE partial_upgrade(value TEXT); INVALID SQL;")
	if err := store.applyMigrations(ctx, steps, "injected_failure", "before.db"); err == nil {
		t.Fatal("injected migration failure ignored")
	}
	if err := store.validateVersion(ctx, 1); err != nil {
		t.Fatalf("failed upgrade changed original identity/history: %v", err)
	}
	if _, err := store.GetSnapshot(ctx, "op_sqlite_test", 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('partial_upgrade','approvals')").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed upgrade retained schema: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("failed upgrade removed backup")
	}
	steps, err = migrationSteps()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.applyMigrations(ctx, steps, "retry", "before.db"); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if err := store.validate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeCannotRunWhileAnotherStoreIsOpen(t *testing.T) {
	store := initializedStore(t)
	if _, err := Upgrade(context.Background(), Options{Path: store.path, BusyTimeout: 10 * time.Millisecond}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("upgrade ignored active state consumer: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Upgrade(context.Background(), Options{Path: store.path})
	if err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
}
