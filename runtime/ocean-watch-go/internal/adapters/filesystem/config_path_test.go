package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedRuntimeUsesHomeConfigUnlessExplicitlyOverridden(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "runtime-slot")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), []byte(`{"name":"ocean-watch"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	getenv := func(name string) string {
		switch name {
		case ManagedRuntimeEnv:
			return "1"
		case CodexHomeEnv:
			return filepath.Join(home, ".codex-custom")
		default:
			return ""
		}
	}
	want := filepath.Join(home, ".codex-custom", "ads-plan-monitor", "config.json")
	if got := ResolveConfigPath("", pluginRoot, getenv, home); got != want {
		t.Fatalf("managed query config = %q, want %q", got, want)
	}
	if got := ResolveInitializationConfigPath("", false, pluginRoot, getenv, home); got != want {
		t.Fatalf("managed init config = %q, want %q", got, want)
	}
	explicit := filepath.Join(root, "explicit.json")
	if got := ResolveInitializationConfigPath(explicit, false, pluginRoot, getenv, home); got != explicit {
		t.Fatalf("explicit config was ignored: %q", got)
	}
}
