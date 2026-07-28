package contractrunner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contracts"
)

func TestReadCommandManifestMatchesGoContract(t *testing.T) {
	commands, digest, err := ReadCommandManifest(commandManifestPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != len(contracts.Commands) {
		t.Fatalf("got %d commands, want %d", len(commands), len(contracts.Commands))
	}
	if len(digest) != 64 {
		t.Fatalf("unexpected manifest digest: %q", digest)
	}
}

func TestReadCommandManifestRejectsChangedOrder(t *testing.T) {
	payload, err := os.ReadFile(commandManifestPath(t))
	if err != nil {
		t.Fatal(err)
	}
	first := []byte("  command: setup doctor")
	changed := []byte("  command: setup unexpected")
	payload = []byte(strings.Replace(string(payload), string(first), string(changed), 1))
	path := filepath.Join(t.TempDir(), "commands.yaml")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCommandManifest(path); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinCasesCoverVersionAndEveryHelpLevel(t *testing.T) {
	cases := BuiltinCases(contracts.Names())
	wantMinimum := 2 + len(contracts.Commands)
	if len(cases) <= wantMinimum {
		t.Fatalf("got %d cases; domain help cases are missing", len(cases))
	}
	seen := map[string]bool{}
	for _, item := range cases {
		if seen[item.Spec.ID] {
			t.Fatalf("duplicate case ID: %s", item.Spec.ID)
		}
		seen[item.Spec.ID] = true
	}
	for _, id := range []string{"version", "help-global", "help-domain-accounts", "help-accounts-list"} {
		if !seen[id] {
			t.Fatalf("missing built-in case %s", id)
		}
	}
}

func commandManifestPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve contract runner source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../contracts/commands.yaml"))
}
