package f2config

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStatusChecksVaultWithoutClaimingOnlineValidity(t *testing.T) {
	for _, test := range []struct {
		name, backend, cookie, code string
		err                         error
	}{
		{"saved", "macos-keychain", "sid=fixture-secret", "", nil},
		{"missing", "macos-keychain", "", "f2_cookie_missing", nil},
		{"invalid", "macos-keychain", "invalid", "f2_cookie_invalid", nil},
		{"locked", "macos-keychain", "sid=fixture-secret", "f2_credential_unavailable", errors.New("fixture-secret")},
		{"file backend", "file-fallback", "sid=fixture-secret", "f2_credential_unavailable", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fixtureStore{backend: test.backend, entry: map[string]any{"cookie": test.cookie}, err: test.err}
			status := (Settings{Store: store}).Status(context.Background())
			if status.Configured != (test.code == "") || status.OnlineVerified || status.ErrorCode != test.code || store.writes != 0 {
				t.Fatalf("unexpected storage status: %+v", status)
			}
			encoded, _ := json.Marshal(status)
			if strings.Contains(string(encoded), "fixture-secret") {
				t.Fatal("status leaked credential")
			}
		})
	}
}

type fixtureStore struct {
	backend string
	entry   map[string]any
	writes  int
	err     error
}

func (store *fixtureStore) BackendName() string { return store.backend }
func (store *fixtureStore) Read(context.Context, string) (map[string]any, error) {
	return store.entry, store.err
}
func (store *fixtureStore) Write(_ context.Context, _ string, value map[string]any) (string, error) {
	store.writes++
	if store.err == nil {
		store.entry = value
	}
	return store.backend, store.err
}

func TestSettingsRequiresSystemVaultAndPreservesCookie(t *testing.T) {
	ctx := context.Background()
	for _, backend := range []string{"file-fallback", "windows-dpapi", "unavailable"} {
		store := &fixtureStore{backend: backend}
		settings := Settings{Store: store}
		if _, err := settings.Save(ctx, "sessionid=fixture"); err == nil || store.writes != 0 {
			t.Fatal("non-system store accepted")
		}
		if _, err := settings.Cookie(ctx); err == nil {
			t.Fatal("non-system store read accepted")
		}
	}
	store := &fixtureStore{backend: "macos-keychain"}
	settings := Settings{Store: store}
	if _, err := settings.Cookie(ctx); err == nil {
		t.Fatal("missing Cookie not reported")
	}
	want := "sessionid=fixture; token=a=b+c/==;"
	if _, err := settings.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, err := settings.Cookie(ctx); err != nil || got != want {
		t.Fatal("saved Cookie changed")
	}
	store.err = errors.New("sessionid=fixture must never appear")
	if _, err := settings.Cookie(ctx); err == nil || strings.Contains(err.Error(), "sessionid=") {
		t.Fatal("credential read failure leaked")
	}
	if _, err := settings.Save(ctx, want); err == nil || strings.Contains(err.Error(), "sessionid=") {
		t.Fatal("credential write failure leaked")
	}
}

func TestSettingsRejectsMalformedCookieBeforeWrite(t *testing.T) {
	store := &fixtureStore{backend: "macos-keychain"}
	for _, cookie := range []string{"", "Cookie: sid=fixture", "sid=fixture\r\nHeader: value", "not a cookie", "名=值", "=value", strings.Repeat("x", MaxCookieBytes) + "=x"} {
		if _, err := (Settings{Store: store}).Save(context.Background(), cookie); err == nil {
			t.Fatal("invalid Cookie accepted")
		}
	}
	if store.writes != 0 {
		t.Fatal("invalid Cookie reached vault")
	}
}
