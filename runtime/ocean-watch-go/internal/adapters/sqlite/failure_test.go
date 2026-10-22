package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func initializedStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "state.db")
	store, err := Initialize(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestEveryPhysicalConnectionHasRequiredSettings(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	var connections []*sql.Conn
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()
	for range 4 {
		connection, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		for pragma, want := range map[string]int{
			"foreign_keys": 1, "busy_timeout": 2000, "synchronous": 2, "trusted_schema": 0,
		} {
			var got int
			if err := connection.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("%s = %d, want %d", pragma, got, want)
			}
		}
	}
	version, err := store.EngineVersion(contextWithTimeout(t))
	if err == nil || version != "" {
		t.Fatal("pool capacity was not enforced")
	}
}

func contextWithTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestPreflightTransactionRollsBackAllRowsOnPayloadFailure(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER injected_failure BEFORE INSERT ON preflight_snapshots BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreflight(ctx, testSnapshot(t, 1, `{"id":"one"}`)); err == nil {
		t.Fatal("injected failure was ignored")
	}
	for _, table := range []string{"operations", "operation_versions", "preflight_snapshots"} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d partial rows", table, count)
		}
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER injected_failure"); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreflight(ctx, testSnapshot(t, 1, `{"id":"one"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentStoresPreserveIdempotencyAndVersionConflicts(t *testing.T) {
	store := initializedStore(t)
	ctx := context.Background()
	other, err := Open(ctx, Options{Path: store.path})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	snapshot := testSnapshot(t, 1, `{"id":"shared"}`)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for index := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writer := store
			if index%2 == 1 {
				writer = other
			}
			results <- writer.SavePreflight(ctx, snapshot)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent idempotent retry: %v", err)
		}
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM operation_versions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("version count = %d, want 1", count)
	}
	if err := other.SavePreflight(ctx, testSnapshot(t, 3, `{"id":"skipped"}`)); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("skipped version: %v", err)
	}
	var busy, logged, checkpointed int
	if err := store.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("checkpoint stayed busy: %d", busy)
	}
	if _, err := other.GetSnapshot(ctx, snapshot.OperationID, 1); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationFailureRollsBackSchemaAndCanBeRetried(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.db")
	path, err := preparePath(path, true)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), url.Values{"mode": {"rw"}, "_txlock": {"immediate"}}))
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{db: db, path: path}
	defer store.Close()
	if err := store.migrate(ctx, []byte("CREATE TABLE partial(value TEXT); INVALID SQL;")); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name='partial'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed migration retained partial schema")
	}
	appID, version, err := identity(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if appID != 0 || version != 0 {
		t.Fatal("failed migration changed identity")
	}
	if err := store.initialize(ctx); err != nil {
		t.Fatalf("retry after migration rollback: %v", err)
	}
}

func TestInspectorDoesNotMutateOrTrustWALMainHeader(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing", "state.db")
	inspection, err := (Inspector{Path: missing}).Inspect(ctx)
	if err != nil || inspection.Status != "not_initialized" {
		t.Fatalf("missing inspection: %#v %v", inspection, err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspector created directory")
	}
	store := initializedStore(t)
	if err := store.SavePreflight(ctx, testSnapshot(t, 1, `{"id":"wal"}`)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	walBefore, err := os.ReadFile(store.path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err = (Inspector{Path: store.path}).Inspect(ctx)
	if err != nil || inspection.Status != "unverified" || inspection.SchemaVersion != 0 || inspection.Evidence != "wal_present_header_not_current" {
		t.Fatalf("WAL inspection: %#v %v", inspection, err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	walAfter, err := os.ReadFile(store.path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(walBefore) != string(walAfter) {
		t.Fatal("inspector changed database or WAL")
	}
}
