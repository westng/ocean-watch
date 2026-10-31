package localauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/f2config"
)

type f2VaultFixture struct {
	mu     sync.Mutex
	value  map[string]any
	writes int
	err    error
}

func (*f2VaultFixture) BackendName() string { return "macos-keychain" }
func (vault *f2VaultFixture) Read(context.Context, string) (map[string]any, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	return vault.value, vault.err
}
func (vault *f2VaultFixture) Write(_ context.Context, account string, value map[string]any) (string, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if account != f2config.CredentialAccount {
		return "", errors.New("wrong account")
	}
	vault.writes++
	if vault.err != nil {
		return "", vault.err
	}
	vault.value = value
	return "macos-keychain", nil
}

func startF2Fixture(t *testing.T, lifetime time.Duration) (*F2Manager, *f2VaultFixture, f2config.Session, url.Values) {
	t.Helper()
	vault := &f2VaultFixture{}
	manager := NewF2(context.Background(), F2Options{Settings: f2config.Settings{Store: vault}, Lifetime: lifetime})
	t.Cleanup(manager.Close)
	view, err := manager.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(view.ConfigurationURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	match := regexp.MustCompile(`name="csrf" value="([a-f0-9]{48})"`).FindStringSubmatch(string(body))
	if response.StatusCode != 200 || len(match) != 2 || response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(response.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("unsafe configuration form")
	}
	return manager, vault, view, url.Values{"session": {view.SessionID}, "csrf": {match[1]}, "cookie": {"sessionid=fixture-secret; token=a=b+==;"}}
}

func TestF2LocalPageSavesOnlyToVaultAndClosesListener(t *testing.T) {
	manager, vault, view, form := startF2Fixture(t, time.Minute)
	reused, err := manager.Start(context.Background())
	if err != nil || reused.SessionID != view.SessionID {
		t.Fatal("active session not reused")
	}
	response, err := http.PostForm(view.ConfigurationURL, form)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || strings.Contains(string(body), "fixture-secret") || !strings.Contains(string(body), "已保存至系统凭据库") {
		t.Fatal("unsafe or failed save result")
	}
	got, err := manager.Get(context.Background(), view.SessionID)
	if err != nil || got.Status != "completed" || !got.CredentialSaved || !got.CanResume || got.ConfigurationURL != "" {
		t.Fatalf("unexpected state: %+v %v", got, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "fixture-secret") {
		t.Fatal("Cookie leaked in session")
	}
	value, err := (f2config.Settings{Store: vault}).Cookie(context.Background())
	if err != nil || value != form.Get("cookie") || vault.writes != 1 {
		t.Fatal("Cookie not saved exactly once")
	}
	current, _ := manager.find(view.SessionID)
	select {
	case <-current.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("completed listener did not close")
	}
	if _, err := http.Get(view.ConfigurationURL); err == nil {
		t.Fatal("listener remains open")
	}
	// A new Runtime has no session history, but uses the same system vault.
	manager.Close()
	restarted := NewF2(context.Background(), F2Options{Settings: f2config.Settings{Store: vault}})
	defer restarted.Close()
	if _, err := restarted.Get(context.Background(), view.SessionID); err == nil {
		t.Fatal("new Runtime must not claim it owns the previous session")
	}
	if status := restarted.Status(context.Background()); !status.Configured || status.OnlineVerified || vault.writes != 1 {
		t.Fatalf("session loss must not imply credential loss: %+v", status)
	}
}

func TestF2LocalPageRejectsForgedAndOversizeRequests(t *testing.T) {
	manager, vault, view, form := startF2Fixture(t, time.Minute)
	current, _ := manager.find(view.SessionID)
	for _, test := range []struct {
		name, host, origin, referer, body string
		expected                          int
	}{
		{"cross origin", "", "https://evil.example", "", form.Encode(), 403},
		{"null cross-site origin", "", "null", "", form.Encode(), 403},
		{"null origin wrong CSRF", "", "null", "", url.Values{"session": {view.SessionID}, "csrf": {"wrong"}, "cookie": {"sessionid=fixture-secret"}}.Encode(), 403},
		{"forged host", "evil.example:80", "", "", form.Encode(), 403},
		{"wrong CSRF", "", "", "", url.Values{"session": {view.SessionID}, "csrf": {"wrong"}, "cookie": {"sessionid=fixture-secret"}}.Encode(), 403},
		{"wrong session", "", "", "", url.Values{"session": {strings.Repeat("0", 48)}, "csrf": {form.Get("csrf")}, "cookie": {"sessionid=fixture-secret"}}.Encode(), 403},
		{"foreign referer", "", "", "http://evil.example/f2/configure", form.Encode(), 403},
		{"oversize", "", "", "", form.Encode() + strings.Repeat("x", 110000), 403},
		{"newline", "", "", "", url.Values{"session": {view.SessionID}, "csrf": {form.Get("csrf")}, "cookie": {"sessionid=fixture-secret\r\nX-Test: value"}}.Encode(), 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", view.ConfigurationURL, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if test.host != "" {
				request.Host = test.host
			}
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Referer", test.referer)
			if test.name == "null cross-site origin" {
				request.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			response := httptest.NewRecorder()
			manager.f2Handler(current).ServeHTTP(response, request)
			if response.Code != test.expected || strings.Contains(response.Body.String(), "fixture-secret") {
				t.Fatalf("response status=%d", response.Code)
			}
		})
	}
	if vault.writes != 0 {
		t.Fatal("invalid request wrote a credential")
	}
	if _, err := manager.Cancel(context.Background(), view.SessionID); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	status := current.view.Status
	current.mu.Unlock()
	if status != "cancelled" {
		t.Fatal("session not cancelled")
	}
}

func TestF2LocalPageAcceptsEmbeddedBrowserOpaqueOrigin(t *testing.T) {
	manager, vault, view, form := startF2Fixture(t, time.Minute)
	request, err := http.NewRequest(http.MethodPost, view.ConfigurationURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "null")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	got, err := manager.Get(context.Background(), view.SessionID)
	if err != nil || response.StatusCode != http.StatusOK || got.Status != "completed" || !got.CredentialSaved || vault.writes != 1 {
		t.Fatalf("embedded-browser save failed: status=%d session=%+v err=%v", response.StatusCode, got, err)
	}
	if strings.Contains(string(body), "fixture-secret") {
		t.Fatal("Cookie was echoed")
	}
}

func TestF2FailedSaveAndExpiryDoNotClaimSuccess(t *testing.T) {
	manager, vault, view, form := startF2Fixture(t, time.Minute)
	vault.err = errors.New("sensitive fixture-secret")
	response, err := http.PostForm(view.ConfigurationURL, form)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	got, _ := manager.Get(context.Background(), view.SessionID)
	if got.Status != "failed" || got.CredentialSaved || got.CanResume || strings.Contains(string(body), "fixture-secret") {
		t.Fatal("failed save claimed success or leaked")
	}

	expiring := NewF2(context.Background(), F2Options{Settings: f2config.Settings{Store: &f2VaultFixture{}}, Lifetime: 20 * time.Millisecond})
	defer expiring.Close()
	expired, err := expiring.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, _ := expiring.find(expired.SessionID)
	select {
	case <-current.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("expired listener not closed")
	}
	status, _ := expiring.Get(context.Background(), expired.SessionID)
	if status.Status != "expired" || status.CanResume {
		t.Fatal("expiry not reported")
	}
}
