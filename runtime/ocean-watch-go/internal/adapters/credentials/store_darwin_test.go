package credentials

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMacOSKeychainWritesAndUpdatesWithRealSecurityTool(t *testing.T) {
	const security = "/usr/bin/security"
	if _, err := os.Stat(security); err != nil {
		t.Skip("macOS security tool is unavailable")
	}
	directory := t.TempDir()
	keychain := filepath.Join(directory, "isolated fixture.keychain-db")
	before := runFixtureSecurity(t, security, "list-keychains", "-d", "user")
	runFixtureSecurity(t, security, "create-keychain", "-p", "isolated-synthetic-password", keychain)
	t.Cleanup(func() {
		runFixtureSecurity(t, security, "delete-keychain", keychain)
		after := runFixtureSecurity(t, security, "list-keychains", "-d", "user")
		if string(before) != string(after) {
			t.Error("temporary keychain changed the user's keychain search list")
		}
	})

	// Redirect only the keychain selection. Password parsing and storage use the
	// real system tool, with no access to the user's login keychain.
	argumentsPath := filepath.Join(directory, "arguments")
	t.Setenv("OCEAN_WATCH_FIXTURE_KEYCHAIN", keychain)
	t.Setenv("OCEAN_WATCH_FIXTURE_ARGUMENTS", argumentsPath)
	command := filepath.Join(directory, "security")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" >> "$OCEAN_WATCH_FIXTURE_ARGUMENTS"
case "$1" in
  -i)
    IFS= read -r command
    printf '%s "%s"\n' "$command" "$OCEAN_WATCH_FIXTURE_KEYCHAIN" | /usr/bin/security -i
    ;;
  find-generic-password)
    exec /usr/bin/security "$@" "$OCEAN_WATCH_FIXTURE_KEYCHAIN"
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	store := Store{Root: directory, LookPath: func(name string) (string, error) {
		if name != "security" {
			return "", fmt.Errorf("unexpected command %q", name)
		}
		return command, nil
	}}
	account := "oceanengine-auth-qianchuan-isolated-fixture-r1"
	values := []map[string]any{
		{
			"access_token":  strings.Repeat("synthetic-access-", 16),
			"refresh_token": "synthetic-refresh-first",
			"label":         "中文\nquoted \"value\" and \\backslash",
		},
		{
			"access_token":  "synthetic-access-updated",
			"refresh_token": "synthetic-refresh-updated",
			"label":         "updated synthetic fixture",
		},
	}
	for _, value := range values {
		backend, err := store.Write(context.Background(), account, value)
		if err != nil {
			t.Fatal(err)
		}
		if backend != BackendMacOSKeychain {
			t.Fatalf("backend = %q, want %q", backend, BackendMacOSKeychain)
		}
		written, err := store.Read(context.Background(), account)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(written, value) {
			t.Fatal("credential did not survive a real Keychain write/update and read-back")
		}
	}

	// Some security versions reject long interactive commands. A rejected
	// candidate must leave the previous entry intact instead of deleting it.
	large := map[string]any{"fixture": strings.Repeat("isolated-synthetic-data", 1024)}
	_, writeErr := store.Write(context.Background(), account, large)
	want := large
	if writeErr != nil {
		want = values[len(values)-1]
	}
	written, err := store.Read(context.Background(), account)
	if err != nil || !reflect.DeepEqual(written, want) {
		t.Fatal("credential update did not preserve the expected stored value")
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		payload, err := encode(value, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(arguments), string(payload)) ||
			strings.Contains(string(arguments), hex.EncodeToString(payload)) {
			t.Fatal("credential payload was exposed in command arguments")
		}
		for _, field := range value {
			if strings.Contains(string(arguments), field.(string)) {
				t.Fatal("credential value was exposed in command arguments")
			}
		}
	}
}

func runFixtureSecurity(t *testing.T, security string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, security, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("temporary keychain command %s failed: %v", args[0], err)
	}
	return output
}
