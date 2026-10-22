package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAncestorSymlinkIsRejectedBeforeCreatingChildren(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := createStateDirectory(target); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked-parent")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(link, "missing", "state.db")
	if _, err := Initialize(context.Background(), Options{Path: path}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("ancestor symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state initialization followed an ancestor symlink")
	}
	if _, err := (Inspector{Path: path}).Inspect(context.Background()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("inspector ancestor symlink: %v", err)
	}
}

func TestStateAllowsMacOSSystemPathAliases(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system aliases only")
	}
	t.Run("tmp", func(t *testing.T) {
		root, err := os.MkdirTemp("/tmp", "ocean-watch-path-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		store, err := Initialize(context.Background(), Options{Path: filepath.Join(root, "private", "state.db")})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("var", func(t *testing.T) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(root, "/private/var/") {
			t.Skip("temporary directory is outside the system var alias")
		}
		path := filepath.Join(strings.TrimPrefix(root, "/private"), "private", "state.db")
		store, err := Initialize(context.Background(), Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEmptyDatabaseWithRecoverySidecarsIsNotInitialized(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "state.db")
			if _, err := preparePath(path, true); err != nil {
				t.Fatal(err)
			}
			if err := createStateFile(path + suffix); err != nil {
				t.Fatal(err)
			}
			for _, open := range []func(context.Context, Options) (*Store, error){Open, Initialize} {
				store, err := open(context.Background(), Options{Path: path})
				if store != nil {
					_ = store.Close()
				}
				if !errors.Is(err, ErrIncompatible) {
					t.Fatalf("empty database with recovery sidecar: %v", err)
				}
			}
			inspection, err := (Inspector{Path: path}).Inspect(context.Background())
			if err != nil || inspection.Status != "unverified" || inspection.NextAction != "validate_state_explicitly" {
				t.Fatalf("empty database with sidecar inspection: %#v %v", inspection, err)
			}
			for _, name := range []string{path, path + suffix} {
				info, err := os.Stat(name)
				if err != nil || info.Size() != 0 {
					t.Fatalf("empty database or sidecar was modified: %v", err)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 2 {
				t.Fatalf("unexpected recovery sidecars: %v %v", entries, err)
			}
		})
	}
}

func TestInspectorReportsUnverifiedRecoverySidecars(t *testing.T) {
	for suffix, evidence := range map[string]string{
		"-wal":     "wal_present_header_not_current",
		"-journal": "rollback_journal_present_recovery_unverified",
		"-shm":     "shared_memory_without_wal_unverified",
	} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "state.db")
			store, err := Initialize(context.Background(), Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := createStateFile(path + suffix); err != nil {
				t.Fatal(err)
			}
			inspection, err := (Inspector{Path: path}).Inspect(context.Background())
			if err != nil || inspection.Status != "unverified" || inspection.SchemaVersion != 0 || inspection.Evidence != evidence {
				t.Fatalf("sidecar inspection: %#v %v", inspection, err)
			}
		})
	}
}
