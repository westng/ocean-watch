package sqlite

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestOpenStoreRejectsReplacedDatabaseIdentity(t *testing.T) {
	store := initializedStore(t)
	path := store.path
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := createStateFile(path); err != nil {
		t.Fatal(err)
	}
	if err := store.ensureIdentity(); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("replacement accepted: %v", err)
	}
	if err := store.SavePreflight(context.Background(), testSnapshot(t, 1, `{"id":"replacement"}`)); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("snapshot attempted a replaced path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("replacement was modified: %v", err)
	}
}
