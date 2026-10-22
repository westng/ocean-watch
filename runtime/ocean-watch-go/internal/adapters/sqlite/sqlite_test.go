package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func TestInitializeOpenAndInspectState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "nested", "state.db")

	if _, err := Open(ctx, Options{Path: path}); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("Open before Initialize = %v, want ErrNotInitialized", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open created state directory: %v", err)
	}

	store, err := Initialize(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer store.Close()

	var journal string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	appID, version, err := identity(ctx, store.db)
	if err != nil {
		t.Fatal(err)
	}
	if appID != applicationID || version != SchemaVersion {
		t.Fatalf("identity = (%x, %d), want (%x, %d)", appID, version, applicationID, SchemaVersion)
	}

	var foreignKeys int
	if err := store.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open after Initialize: %v", err)
	}
	defer store.Close()

	inspection, err := (Inspector{Path: path}).Inspect(ctx)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.Status != "unverified" {
		t.Fatalf("Inspect status = %q, want unverified", inspection.Status)
	}
}

func TestInitializeRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	if _, err := Initialize(ctx, Options{Path: "relative/state.db"}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("relative path = %v, want ErrUnsafePath", err)
	}

	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := Initialize(ctx, Options{Path: filepath.Join(link, "state.db")}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink directory path = %v, want ErrUnsafePath", err)
	}

}

func TestMigrationChecksumAndUnknownSchemaAreBlocked(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := createStateDirectory(stateDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "state.db")
	store, err := Initialize(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE schema_migrations SET checksum='tampered' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Options{Path: path}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("tampered migration = %v, want ErrIncompatible", err)
	}

	unknownRoot := t.TempDir()
	unknownDir := filepath.Join(unknownRoot, "state")
	if err := createStateDirectory(unknownDir); err != nil {
		t.Fatal(err)
	}
	unknownPath := filepath.Join(unknownDir, "unknown.db")
	if err := createStateFile(unknownPath); err != nil {
		t.Fatal(err)
	}
	unknown, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(unknownPath), url.Values{"mode": {"rw"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unknown.ExecContext(ctx, "CREATE TABLE foreign_schema(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := unknown.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(ctx, Options{Path: unknownPath}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("unknown schema = %v, want ErrIncompatible", err)
	}
}

func TestPreflightSnapshotsAreVersionedImmutableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := createStateDirectory(stateDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "state.db")
	store, err := Initialize(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := testSnapshot(t, 1, `{"items":[{"id":"one"}]}`)
	if err := store.SavePreflight(ctx, first); err != nil {
		t.Fatalf("SavePreflight first: %v", err)
	}
	if err := store.SavePreflight(ctx, first); err != nil {
		t.Fatalf("SavePreflight idempotent retry: %v", err)
	}
	got, err := store.GetSnapshot(ctx, first.OperationID, 1)
	if err != nil {
		t.Fatalf("GetSnapshot first: %v", err)
	}
	if string(got.Payload) != string(first.Payload) || got.SnapshotDigest != first.SnapshotDigest {
		t.Fatalf("GetSnapshot = %#v, want %#v", got, first)
	}

	second := testSnapshot(t, 2, `{"items":[{"id":"two"}]}`)
	second.CreatedAt = first.CreatedAt.Add(time.Minute)
	second.ExpiresAt = second.CreatedAt.Add(time.Hour)
	if err := store.SavePreflight(ctx, second); err != nil {
		t.Fatalf("SavePreflight second: %v", err)
	}
	if _, err := store.GetSnapshot(ctx, first.OperationID, 3); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("missing snapshot = %v, want ErrSnapshotNotFound", err)
	}

	conflict := first
	conflict.Payload = []byte(`{"items":[{"id":"tampered"}]}`)
	conflict.SnapshotDigest, err = operations.Digest(conflict.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreflight(ctx, conflict); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("conflicting retry = %v, want ErrSnapshotConflict", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE preflight_snapshots SET payload_json='{}' WHERE operation_id=? AND operation_version=1", first.OperationID); err == nil {
		t.Fatal("direct snapshot update unexpectedly succeeded")
	}
}

func testSnapshot(t *testing.T, version int, payload string) operations.Snapshot {
	t.Helper()
	digest, err := operations.Digest([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.October, 21, 10, version, 0, 0, time.UTC)
	return operations.Snapshot{
		OperationID:    "op_sqlite_test",
		Version:        version,
		Kind:           "qianchuan_preflight",
		Channel:        "qianchuan",
		SubjectID:      "subject-1",
		RuntimeVersion: "runtime-test",
		SchemaVersion:  operations.SchemaVersion,
		InputDigest:    "sha256:" + strings.Repeat("1", 64),
		SnapshotDigest: digest,
		CreatedAt:      created,
		ExpiresAt:      created.Add(time.Hour),
		Payload:        []byte(payload),
	}
}
