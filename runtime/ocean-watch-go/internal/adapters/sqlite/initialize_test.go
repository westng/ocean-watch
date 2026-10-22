package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentInitializeUsesOneMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.db")
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store, err := Initialize(context.Background(), Options{Path: path})
			if err == nil {
				err = store.Close()
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("concurrent initialization: %v", err)
		}
	}
	store, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != SchemaVersion {
		t.Fatalf("migration count = %d, want %d", count, SchemaVersion)
	}
}

func TestOpenDoesNotInitializeExistingEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.db")
	if _, err := preparePath(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), Options{Path: path}); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("empty file Open: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatal("Open initialized an empty database")
	}
	inspection, err := (Inspector{Path: path}).Inspect(context.Background())
	if err != nil || inspection.Status != "not_initialized" || inspection.Evidence != "empty_database_file" || inspection.NextAction != "initialize_state_explicitly" {
		t.Fatalf("empty file inspection: %#v %v", inspection, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("empty file inspection created sidecars: %v %v", entries, err)
	}
	store, err := Initialize(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("explicit initialization of empty file: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("Open after explicit initialization: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
