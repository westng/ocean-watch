package sqlite

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
)

func TestDatabaseURIKeepsWindowsDriveInPath(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"C:/Users/example/state.db", "file:///C:/Users/example/state.db?mode=rw"},
		{"/private/tmp/state.db", "file:///private/tmp/state.db?mode=rw"},
		{"D:/space # ? %/state.db", "file:///D:/space%20%23%20%3F%20%25/state.db?mode=rw"},
	} {
		if got := databaseURI(test.path, url.Values{"mode": {"rw"}}); got != test.want {
			t.Errorf("databaseURI(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestDatabasePathSpecialCharactersRemainLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private # %", "state # %.db")
	store, err := Initialize(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
}
