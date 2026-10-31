package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSystemStoreNeverFallsBackToFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows always selects native Credential Manager; cross-build validates that adapter")
	}
	root := t.TempDir()
	store := SystemStore{Store: Store{Root: root, Getenv: func(string) string { return "1" }, LookPath: func(string) (string, error) { return "", errors.New("missing") }}}
	if store.BackendName() != BackendUnavailable {
		t.Fatal("insecure fallback was enabled")
	}
	if _, err := store.Write(context.Background(), "douyin-f2-cookie", map[string]any{"cookie": "sessionid=fixture"}); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if _, err := store.Read(context.Background(), "douyin-f2-cookie"); err == nil {
		t.Fatal("unavailable vault silently treated as missing")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("system credential created a local file")
	}
}

func TestSystemStoreVerifiesWriteAndDoesNotHideAccessFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture exercises the macOS/Linux system adapters")
	}
	directory := t.TempDir()
	commandPath := filepath.Join(directory, "credential-fixture")
	store := SystemStore{Store: Store{LookPath: func(string) (string, error) { return commandPath, nil }}}
	for _, test := range []struct {
		name, script string
		missing      bool
	}{
		{"missing", "exit 44", true},
		{"denied", "exit 36", false},
	} {
		if runtime.GOOS != "darwin" && test.missing {
			test.script = "exit 1"
		}
		if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"+test.script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		entry, err := store.Read(context.Background(), "douyin-f2-cookie")
		if test.missing {
			if err != nil || len(entry) != 0 {
				t.Fatal("missing item not identified")
			}
		} else if err == nil {
			t.Fatal("denied access silently treated as missing")
		}
	}
	if runtime.GOOS == "darwin" {
		return
	} // The chunk protocol is tested against an isolated real Keychain.
	for _, match := range []bool{true, false} {
		value := "fixture-secret"
		if !match {
			value = "stale-value"
		}
		script := `#!/bin/sh
case "$*" in *fixture-secret*) exit 10;; esac
case "$1" in -i|store) cat >/dev/null;; *) printf '%s' '{"cookie":"sessionid=` + value + `"}';; esac
`
		if err := os.WriteFile(commandPath, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := store.Write(context.Background(), "douyin-f2-cookie", map[string]any{"cookie": "sessionid=fixture-secret"})
		if match && err != nil {
			t.Fatal(err)
		}
		if !match && (err == nil || strings.Contains(err.Error(), "fixture-secret")) {
			t.Fatal("stale read-back accepted or Cookie exposed")
		}
	}
}
