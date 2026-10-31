package localauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type fixtureCredentials struct {
	mu      sync.Mutex
	entries map[string]map[string]any
}

func (store *fixtureCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return configuration.CloneMap(store.entries[account]), nil
}
func (store *fixtureCredentials) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[account] = configuration.CloneMap(value)
	return "memory", nil
}

type localTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (transport localTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme, copy.URL.Host, copy.Host = transport.base.Scheme, transport.base.Host, transport.base.Host
	return transport.transport.RoundTrip(copy)
}

type flowFixture struct {
	manager         *Manager
	client          *http.Client
	store           filesystem.AuthorizationStore
	failDiscovery   atomic.Bool
	refreshCode     atomic.Int64
	blockExchange   atomic.Bool
	exchangeStarted chan struct{}
	exchangeStopped chan struct{}
	mu              sync.Mutex
	paths           []string
}

func newFlowFixture(t *testing.T, lifetime time.Duration) *flowFixture {
	t.Helper()
	fixture := &flowFixture{exchangeStarted: make(chan struct{}, 1), exchangeStopped: make(chan struct{}, 1), client: &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture.mu.Lock()
		fixture.paths = append(fixture.paths, request.URL.Path)
		fixture.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/open_api/oauth2/access_token/", "/open_api/oauth2/refresh_token/":
			if fixture.blockExchange.Load() {
				_, _ = io.Copy(io.Discard, request.Body)
				fixture.exchangeStarted <- struct{}{}
				select {
				case <-request.Context().Done():
					fixture.exchangeStopped <- struct{}{}
				case <-time.After(2 * time.Second):
					t.Error("SDK cancellation did not reach local HTTP fixture")
				}
				return
			}
			if strings.Contains(request.URL.Path, "refresh_token") && fixture.refreshCode.Load() != 0 {
				_ = json.NewEncoder(writer).Encode(map[string]any{"code": fixture.refreshCode.Load(), "message": "fixture refresh invalid"})
				return
			}
			_, _ = io.WriteString(writer, `{"code":0,"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":7200,"refresh_token_expires_in":86400}}`)
		case "/open_api/2/user/info/":
			_, _ = io.WriteString(writer, `{"code":0,"data":{"id":7001}}`)
		case "/open_api/oauth2/advertiser/get/":
			if fixture.failDiscovery.Load() {
				_, _ = io.WriteString(writer, `{"code":40002,"message":"fixture discovery denied"}`)
				return
			}
			_, _ = io.WriteString(writer, `{"code":0,"data":{"list":[{"account_id":9001,"account_type":"ADVERTISER","advertiser_id":1001,"is_valid":true}]}}`)
		case "/open_api/2/advertiser/info/":
			_, _ = io.WriteString(writer, `{"code":0,"data":[{"id":1001}]}`)
		default:
			writer.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(upstream.Close)
	base, _ := url.Parse(upstream.URL)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return localTransport{base, transport} }})
	if err != nil {
		t.Fatal(err)
	}
	credentials := &fixtureCredentials{entries: map[string]map[string]any{
		"oceanengine-app-qianchuan": {"app_id": "123", "secret": "fixture-secret"},
		"oceanengine-app-marketing": {"app_id": "456", "secret": "fixture-marketing-secret"},
	}}
	fixture.store = filesystem.AuthorizationStore{Root: t.TempDir()}
	oauth := oceanengine.OAuthAdapter{Factory: factory}
	discovery := oceanengine.AdvertiserDiscoveryAdapter{Factory: factory}
	tokens := &auth.TokenManager{Credentials: credentials, Authorizations: fixture.store, Locks: filesystem.RefreshLocker{StateRoot: fixture.store.Root}, OAuth: oauth}
	fixture.manager = New(context.Background(), Options{Address: "127.0.0.1:0", Lifetime: lifetime, Credentials: credentials, OAuth: oauth, Users: oauth,
		Authorizer: auth.Authorizer{Credentials: credentials, Authorizations: fixture.store, Locks: tokens.Locks},
		Syncer:     auth.AdvertiserSnapshotSync{Tokens: tokens, Authorizations: fixture.store, Discovery: discovery},
	})
	t.Cleanup(fixture.manager.Close)
	return fixture
}

func (fixture *flowFixture) start(t *testing.T, advertiser string) auth.RecoverySession {
	t.Helper()
	view, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", AdvertiserID: advertiser})
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (fixture *flowFixture) syncStart(t *testing.T, advertiser, authorizationID string) auth.RecoverySession {
	t.Helper()
	view, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", AdvertiserID: advertiser, AuthorizationID: authorizationID, Operation: "sync_accounts"})
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (fixture *flowFixture) localURL(t *testing.T, view auth.RecoverySession) *url.URL {
	t.Helper()
	fixture.manager.mu.Lock()
	origin := fixture.manager.active.origin
	fixture.manager.mu.Unlock()
	page, err := url.Parse(origin + "/oauth/confirm?session=" + url.QueryEscape(view.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	return page
}
func (fixture *flowFixture) confirm(t *testing.T, view auth.RecoverySession) *http.Response {
	t.Helper()
	page := fixture.localURL(t, view)
	return fixture.confirmWithHeaders(t, view, page.Scheme+"://"+page.Host, "")
}
func (fixture *flowFixture) confirmWithHeaders(t *testing.T, view auth.RecoverySession, origin, referer string) *http.Response {
	t.Helper()
	page := fixture.localURL(t, view)
	request, _ := http.NewRequest(http.MethodPost, page.Scheme+"://"+page.Host+"/oauth/confirm", strings.NewReader(url.Values{"session": {view.SessionID}}.Encode()))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response
}
func (fixture *flowFixture) callback(t *testing.T, view auth.RecoverySession, state string) int {
	t.Helper()
	page, _ := url.Parse(view.AuthorizationURL)
	response, err := fixture.client.Get(page.Query().Get("redirect_uri") + "?" + url.Values{"state": {state}, "auth_code": {"fixture-code"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}
func (fixture *flowFixture) callbackState(t *testing.T, view auth.RecoverySession) string {
	t.Helper()
	page, _ := url.Parse(view.AuthorizationURL)
	return page.Query().Get("state")
}
func (fixture *flowFixture) officialURL(t *testing.T, view auth.RecoverySession) *url.URL {
	t.Helper()
	page, err := url.Parse(view.AuthorizationURL)
	if err != nil || page.Scheme != "https" {
		t.Fatalf("authorization URL is not an official HTTPS URL: %q", view.AuthorizationURL)
	}
	return page
}

func TestLocalConfirmationSourceValidationForAccountSync(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	view := fixture.syncStart(t, "1001", "grant-1")
	page := fixture.localURL(t, view)
	localhost := "http://localhost:" + page.Port()
	if response := fixture.confirmWithHeaders(t, view, localhost, ""); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("localhost origin rejected with status %d", response.StatusCode)
	}
	pageResponse, err := fixture.client.Get(page.String())
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(pageResponse.Body)
	_ = pageResponse.Body.Close()
	if pageResponse.StatusCode != http.StatusOK || !strings.Contains(string(pageBody), "同步") {
		t.Fatal("account sync confirmation page unavailable")
	}

	fixture = newFlowFixture(t, time.Minute)
	view = fixture.syncStart(t, "1001", "grant-1")
	page = fixture.localURL(t, view)
	if response := fixture.confirmWithHeaders(t, view, "", page.String()); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("same-origin referer fallback rejected with status %d", response.StatusCode)
	}

	fixture = newFlowFixture(t, time.Minute)
	view = fixture.syncStart(t, "1001", "grant-1")
	if response := fixture.confirmWithHeaders(t, view, "", ""); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("headerless loopback form rejected with status %d", response.StatusCode)
	}

	fixture = newFlowFixture(t, time.Minute)
	view = fixture.syncStart(t, "1001", "grant-1")
	if response := fixture.confirmWithHeaders(t, view, "https://untrusted.example", ""); response.StatusCode != http.StatusForbidden {
		t.Fatal("cross-origin confirmation accepted")
	}
}

func TestDirectAuthorizationDoesNotRequireBrowserConfirmation(t *testing.T) {
	for _, channel := range []string{"marketing", "qianchuan", "star_map"} {
		t.Run(channel, func(t *testing.T) {
			fixture := newFlowFixture(t, time.Minute)
			credentials := fixture.manager.options.Credentials.(*fixtureCredentials)
			account, err := domain.AppCredentialAccount(channel)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = credentials.Write(context.Background(), account, map[string]any{"app_id": "123", "secret": "fixture-secret"}); err != nil {
				t.Fatal(err)
			}
			view, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: channel, Operation: "authorize"})
			if err != nil {
				t.Fatal(err)
			}
			if view.Status != "awaiting_callback" || view.CanResume || strings.Contains(view.AuthorizationURL, "/oauth/confirm") {
				t.Fatalf("authorization did not start directly at the provider: %+v", view)
			}
			official := fixture.officialURL(t, view)
			if official.Query().Get("app_id") != "123" || official.Query().Get("state") == "" {
				t.Fatalf("official URL missing OAuth parameters: %s", official)
			}
			redirect, err := url.Parse(official.Query().Get("redirect_uri"))
			if err != nil || redirect.Scheme != "http" || redirect.Path != callbackPath || !isLocalBrowserHost(redirect.Hostname()) {
				t.Fatalf("official URL has unsafe callback: %q", official.Query().Get("redirect_uri"))
			}
			invalid := view
			invalid.SessionID = strings.Repeat("0", len(view.SessionID))
			invalidPage, err := fixture.client.Get(redirect.Scheme + "://" + redirect.Host + "/oauth/confirm?session=" + invalid.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			_ = invalidPage.Body.Close()
			if invalidPage.StatusCode != http.StatusForbidden {
				t.Fatal("invalid local session was accepted")
			}
		})
	}
}

func TestMissingAppCredentialsFailBeforeBrowserRecovery(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	credentials := fixture.manager.options.Credentials.(*fixtureCredentials)
	credentials.mu.Lock()
	delete(credentials.entries, "oceanengine-app-qianchuan")
	credentials.mu.Unlock()
	_, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", AdvertiserID: "1001", Operation: "authorize"})
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != "oauth_app_missing" {
		t.Fatalf("missing OAuth app was not rejected at start: %v", err)
	}
}

func (fixture *flowFixture) await(t *testing.T, view auth.RecoverySession) auth.RecoverySession {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if terminal(current.Status) {
			return current
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("authorization interaction did not finish")
	return auth.RecoverySession{}
}

func (fixture *flowFixture) awaitShutdown(t *testing.T, view auth.RecoverySession) {
	t.Helper()
	fixture.manager.mu.Lock()
	current := fixture.manager.sessions[view.SessionID]
	fixture.manager.mu.Unlock()
	if current == nil {
		t.Fatal("authorization session disappeared before shutdown")
	}
	if view.Status != "cancelled" && view.Status != "expired" {
		page := fixture.localURL(t, view)
		response, err := fixture.client.Get(page.String())
		if err != nil {
			t.Fatalf("terminal result page was not delivered before shutdown: %v", err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || strings.Contains(string(body), `http-equiv="refresh"`) {
			t.Fatalf("invalid terminal page: status=%d error=%v", response.StatusCode, err)
		}
	}
	select {
	case <-current.shutdownDone:
	case <-time.After(3 * time.Second):
		t.Fatal("authorization service did not shut down after terminal state")
	}
}
func (fixture *flowFixture) authorize(t *testing.T, view auth.RecoverySession) auth.RecoverySession {
	t.Helper()
	location := fixture.officialURL(t, view)
	if view.Channel == "qianchuan" && (location.Host != "qianchuan.jinritemai.com" || location.Query().Get("material_auth") != "1") {
		t.Fatal("authorization redirected to wrong channel")
	}
	if fixture.callback(t, view, location.Query().Get("state")) != http.StatusSeeOther {
		t.Fatal("valid callback rejected")
	}
	return fixture.await(t, view)
}

func TestDirectAuthorizationStateReplayAndRealHTTPAuthorization(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	view := fixture.start(t, "1001")
	if view.Status != "awaiting_callback" || view.CanResume || strings.Contains(view.AuthorizationURL, "/oauth/confirm") {
		t.Fatalf("authorization did not start without browser confirmation: %+v", view)
	}
	location := fixture.officialURL(t, view)
	state := location.Query().Get("state")
	if state == "" || location.Query().Get("redirect_uri") == "" {
		t.Fatal("official URL omitted callback state or redirect URI")
	}
	if fixture.callback(t, view, "marketing:wrong") != http.StatusForbidden {
		t.Fatal("wrong-channel state accepted")
	}
	completed := fixture.authorize(t, view)
	if completed.Status != "completed" || !completed.CanResume || completed.AccountCount != 1 || completed.AdvertiserCount != 1 {
		t.Fatalf("unexpected completed result: %+v", completed)
	}
	fixture.awaitShutdown(t, completed)
	page, _ := url.Parse(view.AuthorizationURL)
	if _, err := fixture.client.Get(page.Query().Get("redirect_uri") + "?" + url.Values{"state": {state}, "auth_code": {"fixture-code"}}.Encode()); err == nil {
		t.Fatal("callback listener remained available after processing completed")
	}
	stored, _ := fixture.store.ReadChannel(context.Background(), "qianchuan")
	if len(stored.Authorizations) != 1 {
		t.Fatal("completed before persisting authorization")
	}
	payload, _ := json.Marshal(completed)
	for _, secret := range []string{"fixture-secret", "fixture-code", "fixture-access", "fixture-refresh", state} {
		if strings.Contains(string(payload), secret) {
			t.Fatal("session exposed a secret")
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	want := []string{"/open_api/oauth2/access_token/", "/open_api/2/user/info/", "/open_api/oauth2/advertiser/get/", "/open_api/2/advertiser/info/"}
	if !slices.Equal(fixture.paths, want) {
		t.Fatalf("actual SDK HTTP calls: %v", fixture.paths)
	}
	t.Logf("real SDK requests to local HTTP fixture: %v", fixture.paths)
}

func TestCallbackPagePollsWhileAuthorizationRuns(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	fixture.blockExchange.Store(true)
	view := fixture.start(t, "1001")
	location := fixture.officialURL(t, view)
	if fixture.callback(t, view, location.Query().Get("state")) != http.StatusSeeOther {
		t.Fatal("valid callback rejected")
	}
	select {
	case <-fixture.exchangeStarted:
	case <-time.After(time.Second):
		t.Fatal("authorization exchange did not start")
	}
	if fixture.callback(t, view, location.Query().Get("state")) != http.StatusConflict {
		t.Fatal("in-flight callback replay was accepted")
	}
	page := fixture.localURL(t, view)
	response, err := fixture.client.Get(page.String())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `http-equiv="refresh" content="2"`) {
		t.Fatalf("in-flight callback page did not poll: status=%d body=%s", response.StatusCode, body)
	}
	cancelled, err := fixture.manager.Cancel(context.Background(), view.Channel, view.SessionID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("in-flight authorization was not cancelled: %+v, %v", cancelled, err)
	}
	select {
	case <-fixture.exchangeStopped:
	case <-time.After(time.Second):
		t.Fatal("in-flight exchange did not stop after cancellation")
	}

	completedFixture := newFlowFixture(t, time.Minute)
	completed := completedFixture.authorize(t, completedFixture.start(t, "1001"))
	completedPage := completedFixture.localURL(t, completed)
	completedFixture.awaitShutdown(t, completed)
	listener, err := net.Listen("tcp4", completedPage.Host)
	if err != nil {
		t.Fatalf("completed authorization listener was not released: %v", err)
	}
	_ = listener.Close()
}

func TestPendingDiscoveryRecoversWithSyncWithoutAnotherOAuth(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	fixture.failDiscovery.Store(true)
	failed := fixture.authorize(t, fixture.start(t, "1001"))
	if failed.Status != "failed" || !failed.SyncRequired || failed.AuthorizationID == "" || failed.CanResume {
		t.Fatalf("lost pending authorization: %+v", failed)
	}
	fixture.failDiscovery.Store(false)
	syncView, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", AuthorizationID: failed.AuthorizationID, AdvertiserID: "1001"})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.confirm(t, syncView).StatusCode != http.StatusSeeOther {
		t.Fatal("sync confirmation failed")
	}
	completed := fixture.await(t, syncView)
	if completed.Status != "completed" || !completed.CanResume {
		t.Fatalf("sync did not recover: %+v", completed)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	count := 0
	for _, path := range fixture.paths {
		if path == "/open_api/oauth2/access_token/" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("account sync repeated OAuth grant")
	}
}

func TestScopeMismatchDoesNotResumeOriginalRequest(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	failed := fixture.authorize(t, fixture.start(t, "2002"))
	if failed.Status != "failed" || failed.ErrorCode != "AUTHORIZATION_SCOPE_MISSING" || failed.CanResume || failed.SyncRequired {
		t.Fatalf("wrong advertiser resumed: %+v", failed)
	}
}

func TestInvalidRefreshOverRealHTTPIsTerminalAndKeepsStoredRevision(t *testing.T) {
	for _, code := range []int64{40103, 40107} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			fixture := newFlowFixture(t, time.Minute)
			completed := fixture.authorize(t, fixture.start(t, "1001"))
			account, _ := domain.AuthorizationCredentialAccount("qianchuan", completed.AuthorizationID, 1)
			credential, _ := fixture.manager.options.Credentials.Read(context.Background(), account)
			credential["access_token_expires_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			_, _ = fixture.manager.options.Credentials.Write(context.Background(), account, credential)
			fixture.refreshCode.Store(code)
			view, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", AuthorizationID: completed.AuthorizationID, AdvertiserID: "1001"})
			if err != nil {
				t.Fatal(err)
			}
			fixture.confirm(t, view)
			failed := fixture.await(t, view)
			if failed.Status != "failed" || failed.CanResume || failed.ErrorCode != "REAUTHORIZATION_REQUIRED" {
				t.Fatalf("invalid refresh was retried or hidden: %+v", failed)
			}
			state, _ := fixture.store.LoadChannel(context.Background(), "qianchuan")
			metadata := configuration.Object(configuration.Object(state["authorizations"])[completed.AuthorizationID])
			if fmt.Sprint(metadata["token_revision"]) != "1" {
				t.Fatal("failed refresh advanced token revision")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			count := 0
			for _, path := range fixture.paths {
				if path == "/open_api/oauth2/refresh_token/" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("terminal refresh was retried %d times", count)
			}
		})
	}
}

func TestSessionReuseIsolationCancelExpiryAndPortConflict(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	view := fixture.start(t, "1001")
	if again := fixture.start(t, "1001"); again.SessionID != view.SessionID {
		t.Fatal("duplicate start did not reuse session")
	}
	if _, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "marketing"}); err == nil {
		t.Fatal("concurrent conflicting start accepted")
	}
	if _, err := fixture.manager.Get(context.Background(), "marketing", view.SessionID); err == nil {
		t.Fatal("cross-channel session visible")
	}
	if _, err := fixture.manager.Cancel(context.Background(), "marketing", view.SessionID); err == nil {
		t.Fatal("cross-channel session cancelled")
	}
	cancelled, err := fixture.manager.Cancel(context.Background(), "qianchuan", view.SessionID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatal("session not cancelled")
	}
	fixture.mu.Lock()
	count := len(fixture.paths)
	fixture.mu.Unlock()
	if count != 0 {
		t.Fatal("request dispatched before consent")
	}
	short := newFlowFixture(t, 20*time.Millisecond)
	expired := short.await(t, short.start(t, "1001"))
	if expired.Status != "expired" || expired.CanResume {
		t.Fatalf("session did not expire: %+v", expired)
	}
	fixture.start(t, "1001")
	fixture.manager.mu.Lock()
	address := fixture.manager.active.origin
	fixture.manager.mu.Unlock()
	_, addressPort, _ := net.SplitHostPort(strings.TrimPrefix(address, "http://"))
	other := New(context.Background(), Options{Address: "127.0.0.1:" + addressPort, Credentials: fixture.manager.options.Credentials, OAuth: fixture.manager.options.OAuth})
	t.Cleanup(other.Close)
	if _, err := other.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan"}); err == nil {
		t.Fatal("occupied callback port was taken over")
	}
}

func TestCancellationStopsInFlightOAuthAndRuntimeCloseReleasesListener(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	fixture.blockExchange.Store(true)
	view := fixture.start(t, "1001")
	location := fixture.officialURL(t, view)
	fixture.callback(t, view, location.Query().Get("state"))
	select {
	case <-fixture.exchangeStarted:
	case <-time.After(time.Second):
		t.Fatal("SDK request did not reach local HTTP fixture")
	}
	cancelled, err := fixture.manager.Cancel(context.Background(), view.Channel, view.SessionID)
	if err != nil || cancelled.Status != "cancelled" || cancelled.CanResume {
		t.Fatal("in-flight authorization cancellation failed")
	}
	select {
	case <-fixture.exchangeStopped:
	case <-time.After(time.Second):
		t.Fatal("in-flight HTTP request was not cancelled")
	}
	state, _ := fixture.store.ReadChannel(context.Background(), "qianchuan")
	if len(state.Authorizations) != 0 {
		t.Fatal("cancelled token exchange persisted an authorization")
	}
	fixture.manager.mu.Lock()
	address := fixture.manager.active.origin
	fixture.manager.mu.Unlock()
	_, addressPort, _ := net.SplitHostPort(strings.TrimPrefix(address, "http://"))
	fixture.manager.Close()
	other := New(context.Background(), Options{Address: "127.0.0.1:" + addressPort, Credentials: fixture.manager.options.Credentials, OAuth: fixture.manager.options.OAuth})
	t.Cleanup(other.Close)
	if _, err := other.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan"}); err != nil {
		t.Fatal("runtime close retained callback listener")
	}
}
