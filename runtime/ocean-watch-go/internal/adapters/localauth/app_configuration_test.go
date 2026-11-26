package localauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

func appSetupFixture(t *testing.T) (*flowFixture, auth.RecoverySession, url.Values) {
	t.Helper()
	f := newFlowFixture(t, time.Minute)
	store := f.manager.options.Credentials.(*fixtureCredentials)
	store.mu.Lock()
	delete(store.entries, "oceanengine-app-qianchuan")
	store.mu.Unlock()
	view := f.start(t, "1001")
	response, err := f.client.Get(view.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	match := regexp.MustCompile(`name="csrf" value="([a-f0-9]{48})"`).FindSubmatch(body)
	if response.StatusCode != http.StatusOK || len(match) != 2 {
		t.Fatal("configuration page did not expose a protected form")
	}
	return f, view, url.Values{"session": {view.SessionID}, "csrf": {string(match[1])}, "app_id": {"123"}, "secret": {"synthetic-local-app-secret"}}
}

func submitAppForm(t *testing.T, f *flowFixture, view auth.RecoverySession, form url.Values, origin string) *http.Response {
	t.Helper()
	u, _ := url.Parse(view.AuthorizationURL)
	request, _ := http.NewRequest(http.MethodPost, u.Scheme+"://"+u.Host+"/oauth/setup", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", origin)
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestFirstUseConfigurationContinuesSameOAuthSession(t *testing.T) {
	f, view, form := appSetupFixture(t)
	u, _ := url.Parse(view.AuthorizationURL)
	response := submitAppForm(t, f, view, form, u.Scheme+"://"+u.Host)
	if response.StatusCode != http.StatusSeeOther || !strings.HasPrefix(response.Header.Get("Location"), "https://qianchuan.jinritemai.com/") {
		t.Fatalf("configuration did not continue official OAuth: %d", response.StatusCode)
	}
	updated, err := f.manager.Get(context.Background(), view.Channel, view.SessionID)
	if err != nil || updated.Status != "awaiting_callback" || updated.SessionID != view.SessionID {
		t.Fatalf("configuration replaced the session: %+v %v", updated, err)
	}
	serialized, _ := json.Marshal(updated)
	if strings.Contains(string(serialized), form.Get("secret")) || strings.Contains(string(serialized), form.Get("csrf")) {
		t.Fatal("private form data reached the tool session")
	}
	if f.callback(t, updated, f.callbackState(t, updated)) != http.StatusSeeOther {
		t.Fatal("configured session rejected its official callback")
	}
	completed := f.await(t, updated)
	if completed.Status != "completed" || !completed.CanResume {
		t.Fatalf("first-use flow did not complete: %+v", completed)
	}
	if duplicate := submitAppForm(t, f, view, form, u.Scheme+"://"+u.Host); duplicate.StatusCode != http.StatusConflict {
		t.Fatal("completed setup accepted a second save")
	}
}

func TestAppConfigurationRejectsForgedCancelledAndReplacementForms(t *testing.T) {
	for _, scenario := range []string{"csrf", "origin", "oversize", "configured", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			f, view, form := appSetupFixture(t)
			u, _ := url.Parse(view.AuthorizationURL)
			origin := u.Scheme + "://" + u.Host
			store := f.manager.options.Credentials.(*fixtureCredentials)
			switch scenario {
			case "csrf":
				form.Set("csrf", strings.Repeat("0", 48))
			case "origin":
				origin = "https://untrusted.example"
			case "oversize":
				form.Set("secret", strings.Repeat("x", 10000))
			case "configured":
				store.mu.Lock()
				store.entries["oceanengine-app-qianchuan"] = map[string]any{"app_id": "456", "secret": "existing-fixture-secret"}
				store.mu.Unlock()
			case "cancelled":
				current, _ := f.manager.session(view.Channel, view.SessionID)
				current.coordinator.RequestCancel()
			}
			var response *http.Response
			if scenario == "cancelled" {
				current, _ := f.manager.session(view.Channel, view.SessionID)
				request := httptest.NewRequest(http.MethodPost, origin+"/oauth/setup", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				recorder := httptest.NewRecorder()
				f.manager.handler(current).ServeHTTP(recorder, request)
				response = recorder.Result()
				defer response.Body.Close()
			} else {
				response = submitAppForm(t, f, view, form, origin)
			}
			if response.StatusCode < 400 {
				t.Fatal("unsafe configuration was accepted")
			}
			stored, _ := store.Read(context.Background(), "oceanengine-app-qianchuan")
			if scenario == "configured" {
				if stored["secret"] != "existing-fixture-secret" {
					t.Fatal("existing app was overwritten")
				}
			} else if len(stored) != 0 {
				t.Fatal("rejected form wrote credentials")
			}
		})
	}
}

func TestAppConfigurationSourceHandlesEmbeddedBrowserReferers(t *testing.T) {
	for _, tc := range []struct {
		origin, referer, site string
		allowed               bool
	}{
		{"null", "http://127.0.0.1:8787/oauth/setup?session=fixture", "same-origin", true},
		{"", "http://localhost:8787/oauth/setup?session=fixture", "", true},
		{"null", "https://foreign.example/oauth/setup", "", false},
		{"", "http://127.0.0.1:8787/oauth/confirm", "", false},
		{"http://127.0.0.1:8787", "", "cross-site", false},
	} {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/oauth/setup", nil)
		request.Header.Set("Origin", tc.origin)
		request.Header.Set("Referer", tc.referer)
		request.Header.Set("Sec-Fetch-Site", tc.site)
		if appFormSource(request, "http://127.0.0.1:8787") != tc.allowed {
			t.Fatalf("incorrect browser source decision: %+v", tc)
		}
	}
}
