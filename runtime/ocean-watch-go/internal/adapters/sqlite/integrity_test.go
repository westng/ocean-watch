package sqlite

import (
	"context"
	"errors"
	"testing"
)

func TestActualEngineMeetsAdmissionAndInitializationIsIdempotent(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	version, err := store.EngineVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !engineSupported(version) {
		t.Fatalf("unsupported actual engine: %s", version)
	}
	t.Logf("actual SQLite engine: %s", version)
	if err := store.SavePreflight(ctx, testSnapshot(t, 1, `{"id":"preserved"}`)); err != nil {
		t.Fatal(err)
	}
	second, err := Initialize(ctx, Options{Path: store.path})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.GetSnapshot(ctx, "op_sqlite_test", 1); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptedSnapshotCannotBeReadOrRetried(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	snapshot := testSnapshot(t, 1, `{"id":"original"}`)
	if err := store.SavePreflight(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER immutable_preflight_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE preflight_snapshots SET payload_json=?", `{"id":"corrupted"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSnapshot(ctx, snapshot.OperationID, 1); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("corrupted read: %v", err)
	}
	if err := store.SavePreflight(ctx, snapshot); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("corrupted retry: %v", err)
	}
}

func TestUnsupportedSchemaIsNotSilentlyMigrated(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, "PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Options{Path: store.path}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("unsupported Open: %v", err)
	}
	if _, err := Initialize(ctx, Options{Path: store.path}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("unsupported Initialize: %v", err)
	}
	_, version, err := identity(ctx, store.db)
	if err != nil {
		t.Fatal(err)
	}
	if version != 999 {
		t.Fatal("unsupported schema was changed")
	}
}
