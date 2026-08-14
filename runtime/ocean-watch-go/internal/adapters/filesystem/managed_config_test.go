package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestManagedConfigStoreReadsPrivateUserState(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".codex", "ads-plan-monitor")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, []byte(`{"plan_templates":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewManagedConfigStore(func(string) string { return "" }, home)
	if err != nil {
		t.Fatal(err)
	}
	config, revision, err := store.ReadWithRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config["plan_templates"] == nil || revision == "" {
		t.Fatalf("unexpected managed config read: %#v %q", config, revision)
	}
}

func TestManagedConfigStoreRejectsSymlinkAndBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission and symlink contract")
	}
	home := t.TempDir()
	root := filepath.Join(home, ".codex", "ads-plan-monitor")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "outside.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	store, err := NewManagedConfigStore(func(string) string { return "" }, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("managed read accepted a symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("got %v, want permission error", err)
	}
}

func TestManagedConfigStoreRejectsSymlinkedStateRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink contract")
	}
	home := t.TempDir()
	realCodexRoot := filepath.Join(home, "real-codex")
	realStateRoot := filepath.Join(home, "real-state")
	for _, root := range []string{realCodexRoot, realStateRoot} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(realStateRoot, "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkedCodexRoot := filepath.Join(home, "codex-link")
	if err := os.Symlink(realCodexRoot, symlinkedCodexRoot); err != nil {
		t.Fatal(err)
	}
	store, err := NewManagedConfigStore(func(name string) string {
		if name == CodexHomeEnv {
			return symlinkedCodexRoot
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("managed read accepted a symlinked Codex state root")
	}
	if err := os.Remove(symlinkedCodexRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(symlinkedCodexRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realStateRoot, filepath.Join(symlinkedCodexRoot, "ads-plan-monitor")); err != nil {
		t.Fatal(err)
	}
	store, err = NewManagedConfigStore(func(name string) string {
		if name == CodexHomeEnv {
			return symlinkedCodexRoot
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("managed read accepted a symlinked managed state root")
	}
}
