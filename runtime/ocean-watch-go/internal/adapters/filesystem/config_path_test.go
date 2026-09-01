package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateHomePrefersTheHostNeutralOverride(t *testing.T) {
	home := t.TempDir()
	neutral := filepath.Join(home, "shared-state")
	codex := filepath.Join(home, "codex-state")
	cases := []struct {
		name    string
		environ map[string]string
		want    string
	}{
		{name: "neutral wins over codex", environ: map[string]string{
			StateHomeEnv: neutral, CodexHomeEnv: codex,
		}, want: neutral},
		{name: "codex still honoured alone", environ: map[string]string{
			CodexHomeEnv: codex,
		}, want: codex},
		{name: "default is unchanged", environ: nil, want: filepath.Join(home, ".codex")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			getenv := func(name string) string { return testCase.environ[name] }
			if got := StateHome(getenv, home); got != testCase.want {
				t.Fatalf("StateHome = %q, want %q", got, testCase.want)
			}
			if got := CodexHome(getenv, home); got != testCase.want {
				t.Fatalf("CodexHome alias = %q, want %q", got, testCase.want)
			}
			wantConfig := filepath.Join(testCase.want, "ads-plan-monitor", "config.json")
			managed := func(name string) string {
				if name == ManagedRuntimeEnv {
					return "1"
				}
				return testCase.environ[name]
			}
			if got := ResolveConfigPath("", home, managed, home); got != wantConfig {
				t.Fatalf("managed config = %q, want %q", got, wantConfig)
			}
		})
	}
}

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
