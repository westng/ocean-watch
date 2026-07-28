package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteJSONDestinationIsAtomicPrivateAndMatchesStdout(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "exports")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "report.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := new(bytes.Buffer)
	value := map[string]any{"ok": true, "advertiser_id": "1000000000000001"}
	if err := WriteJSONDestination(stdout, value, path); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, stdout.Bytes()) {
		t.Fatalf("destination differs from stdout: file=%q stdout=%q", written, stdout.Bytes())
	}
	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("JSON destination mode is %o", fileInfo.Mode().Perm())
		}
		parentInfo, err := os.Stat(parent)
		if err != nil {
			t.Fatal(err)
		}
		if parentInfo.Mode().Perm() != 0o700 {
			t.Fatalf("JSON destination directory mode is %o", parentInfo.Mode().Perm())
		}
	}
}
