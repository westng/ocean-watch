package contractrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFixtureManifestValidatesStrictly(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "account-book"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureManifest(t, root, `{
  "schema_version": 1,
  "cases": [{
    "id": "accounts-list",
    "argv": ["accounts", "list", "--config", "{{workspace}}/config.json"],
    "fixture": "account-book",
    "normalizers": ["trim-trailing-space", "launcher-command"],
    "network_policy": "forbidden"
  }]
}`)
	manifest, err := ReadFixtureManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Cases) != 1 || manifest.Cases[0].ID != "accounts-list" || len(manifest.Cases[0].Normalizers) != 2 {
		t.Fatalf("unexpected fixture manifest: %#v", manifest)
	}
}

func TestReadFixtureManifestRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	tests := map[string]string{
		"path traversal":     `{"schema_version":1,"cases":[{"id":"bad","argv":["x"],"fixture":"../escape"}]}`,
		"unknown normalizer": `{"schema_version":1,"cases":[{"id":"bad","argv":["x"],"normalizers":["drop-errors"]}]}`,
		"unknown field":      `{"schema_version":1,"cases":[],"unexpected":true}`,
		"trailing value":     `{"schema_version":1,"cases":[]} {}`,
		"duplicate ID":       `{"schema_version":1,"cases":[{"id":"same","argv":["x"]},{"id":"same","argv":["y"]}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixtureManifest(t, root, payload)
			if _, err := ReadFixtureManifest(root); err == nil {
				t.Fatal("invalid fixture manifest was accepted")
			}
		})
	}
}

func TestCopyDirectoryRejectsSymbolicLinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(source, "escape")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	err := copyDirectory(source, filepath.Join(root, "target"))
	if err == nil || !strings.Contains(err.Error(), "symbolic links are forbidden") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeFixtureManifest(t *testing.T, root, payload string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "cases.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
