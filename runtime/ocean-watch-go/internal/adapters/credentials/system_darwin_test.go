package credentials

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSystemKeychainRoundTripsLongCookieWithoutTruncation(t *testing.T) {
	const security = "/usr/bin/security"
	if _, err := os.Stat(security); err != nil {
		t.Skip("macOS security tool is unavailable")
	}
	directory := t.TempDir()
	keychain := filepath.Join(directory, "isolated F2 fixture.keychain-db")
	before := runFixtureSecurity(t, security, "list-keychains", "-d", "user")
	runFixtureSecurity(t, security, "create-keychain", "-p", "isolated-synthetic-password", keychain)
	t.Cleanup(func() {
		runFixtureSecurity(t, security, "delete-keychain", keychain)
		if string(before) != string(runFixtureSecurity(t, security, "list-keychains", "-d", "user")) {
			t.Error("fixture changed the user's keychain search list")
		}
	})
	argumentsPath, lengthsPath := filepath.Join(directory, "arguments"), filepath.Join(directory, "lengths")
	t.Setenv("F2_FIXTURE_KEYCHAIN", keychain)
	t.Setenv("F2_FIXTURE_ARGUMENTS", argumentsPath)
	t.Setenv("F2_FIXTURE_LENGTHS", lengthsPath)
	t.Setenv("F2_FIXTURE_WRITE_FAIL", "")
	commandPath := filepath.Join(directory, "security")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" >> "$F2_FIXTURE_ARGUMENTS"
case "$1" in
  -i)
    IFS= read -r command
    printf '%s\n' "${#command}" >> "$F2_FIXTURE_LENGTHS"
    if [ "$F2_FIXTURE_WRITE_FAIL" = 1 ]; then exit 9; fi
    printf '%s "%s"\n' "$command" "$F2_FIXTURE_KEYCHAIN" | /usr/bin/security -i
    ;;
  find-generic-password|delete-generic-password)
    exec /usr/bin/security "$@" "$F2_FIXTURE_KEYCHAIN"
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(commandPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	store := SystemStore{Store: Store{LookPath: func(name string) (string, error) {
		if name != "security" {
			return "", fmt.Errorf("unexpected command")
		}
		return commandPath, nil
	}}}
	const account = "douyin-f2-isolated-fixture"
	ctx := context.Background()
	// Reproduce the invalid entry left by a truncated legacy command. Updating
	// it must work without deleting or exposing anything from the user's vault.
	if err := store.Store.runCommand(ctx, "security", []string{"-i"}, macOSWriteInput(account, []byte(`{"cookie":"synthetic-incomplete`)), true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, account); err == nil {
		t.Fatal("damaged legacy entry was accepted")
	}
	var last string
	for _, length := range []int{3000, 16000, 32768} {
		last = "sessionid=" + strings.Repeat("fixture-cookie-", (length/15)+1)
		last = last[:length]
		backend, err := store.Write(ctx, account, map[string]any{"cookie": last})
		if err != nil || backend != BackendMacOSKeychain {
			t.Fatalf("length=%d: system write failed: %v", length, err)
		}
		entry, err := store.Read(ctx, account)
		if err != nil || entry["cookie"] != last {
			t.Fatalf("length=%d: Cookie was truncated", length)
		}
		root, err := store.readCommand(ctx, "security", macOSReadArguments(account), 44)
		if err != nil || root["format"] != macOSSystemFormat || root["cookie"] != nil {
			t.Fatal("invalid root index")
		}
	}
	t.Setenv("F2_FIXTURE_WRITE_FAIL", "1")
	if _, err := store.Write(ctx, account, map[string]any{"cookie": "sessionid=next-fixture"}); err == nil {
		t.Fatal("failed write accepted")
	}
	t.Setenv("F2_FIXTURE_WRITE_FAIL", "")
	entry, err := store.Read(ctx, account)
	if err != nil || entry["cookie"] != last {
		t.Fatal("failed update damaged the previous revision")
	}
	lengths, err := os.ReadFile(lengthsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Fields(string(lengths)) {
		n, err := strconv.Atoi(line)
		if err != nil || n+len(keychain)+4 >= 4096 {
			t.Fatal("interactive command exceeded the safe line length")
		}
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil || strings.Contains(string(arguments), "sessionid=") || strings.Contains(string(arguments), last) {
		t.Fatal("Cookie appeared in process arguments")
	}
}
