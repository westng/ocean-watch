package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateOptionsFailBeforeCreatingFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Options)
	}{
		{name: "negative timeout", edit: func(o *Options) { o.BusyTimeout = -1 }},
		{name: "excess timeout", edit: func(o *Options) { o.BusyTimeout = 31 * time.Second }},
		{name: "negative connections", edit: func(o *Options) { o.MaxConnections = -1 }},
		{name: "excess connections", edit: func(o *Options) { o.MaxConnections = 9 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			o := Options{Path: filepath.Join(root, "private", "state.db")}
			test.edit(&o)
			if _, err := Initialize(context.Background(), o); err == nil {
				t.Fatal("invalid options accepted")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("invalid options created state")
			}
		})
	}
}

func TestStateRejectsUnsupportedEngineVersions(t *testing.T) {
	for version, want := range map[string]bool{
		"3.51.2": false, "3.51.3": true, "3.53.4": true, "4.0.0": true,
		"3.9.0": false, "3.51": false, "broken": false, "3.-1.4": false,
	} {
		if got := engineSupported(version); got != want {
			t.Errorf("engineSupported(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestInspectorRejectsRelativePathAndHonorsCancellation(t *testing.T) {
	if _, err := (Inspector{Path: "relative/state.db"}).Inspect(context.Background()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("relative inspector path: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "missing", "state.db")
	if _, err := Initialize(ctx, Options{Path: path}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled initialization: %v", err)
	}
	if _, err := (Inspector{Path: path}).Inspect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inspection: %v", err)
	}
}

func TestStateRejectsSymlinkDatabaseAndSidecars(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		t.Run("database"+suffix, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "private", "state.db")
			if err := createStateDirectory(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			if suffix != "" {
				if err := createStateFile(path); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(root, "target")
			if err := createStateFile(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path+suffix); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := Initialize(context.Background(), Options{Path: path}); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("symlink accepted: %v", err)
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != 0 {
				t.Fatal("symlink target was changed")
			}
		})
	}
}
