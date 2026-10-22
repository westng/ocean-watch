//go:build !windows

package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixStatePermissionsAreNotSilentlyRepaired(t *testing.T) {
	for _, component := range []string{"directory", "database", "wal", "shm", "journal"} {
		t.Run(component, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "state.db")
			if _, err := preparePath(path, true); err != nil {
				t.Fatal(err)
			}
			target, mode := path, os.FileMode(0o644)
			switch component {
			case "directory":
				target, mode = filepath.Dir(path), 0o755
			case "wal", "shm", "journal":
				target += "-" + component
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(target, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := Initialize(context.Background(), Options{Path: path}); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("unsafe %s: %v", component, err)
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Fatal("unsafe permissions were silently repaired")
			}
		})
	}
}
