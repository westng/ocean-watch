package contractrunner

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolatedEnvironmentUsesSyntheticEmptyPath(t *testing.T) {
	workspace := t.TempDir()
	environment := isolatedEnvironment(workspace, nil, nil)
	want := "PATH=" + filepath.Join(workspace, "empty-path")
	pathEntries := 0
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") {
			pathEntries++
			if entry != want {
				t.Fatalf("unexpected isolated PATH: %q", entry)
			}
		}
	}
	if pathEntries != 1 {
		t.Fatalf("got %d PATH entries, want 1", pathEntries)
	}
}

func TestIsolatedEnvironmentAllowsExplicitFixturePath(t *testing.T) {
	workspace := t.TempDir()
	want := filepath.Join(workspace, "fixture-bin")
	environment := isolatedEnvironment(workspace, nil, map[string]string{"PATH": "{{workspace}}/fixture-bin"})
	for _, entry := range environment {
		if entry == "PATH="+want {
			return
		}
	}
	t.Fatalf("explicit fixture PATH was not applied: %#v", environment)
}
