package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type oauthLifecycleGate struct {
	entered, cancelled, released chan struct{}
	once                         sync.Once
}

func newOAuthLifecycleGate() *oauthLifecycleGate {
	return &oauthLifecycleGate{entered: make(chan struct{}), cancelled: make(chan struct{}), released: make(chan struct{})}
}

func (gate *oauthLifecycleGate) release() { gate.once.Do(func() { close(gate.released) }) }

func (gate *oauthLifecycleGate) wait(ctx context.Context) error {
	close(gate.entered)
	select {
	case <-gate.released:
		return nil
	case <-ctx.Done():
		close(gate.cancelled)
		<-gate.released
		return ctx.Err()
	}
}

func awaitOAuthLifecycle(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("local OAuth stage did not settle")
	}
}

type oauthLifecycleCredentials struct {
	mu      sync.Mutex
	entries map[string]map[string]any
}

func (store *oauthLifecycleCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return configuration.CloneMap(store.entries[account]), nil
}

func (store *oauthLifecycleCredentials) Write(ctx context.Context, account string, value map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[account] = configuration.CloneMap(value)
	return "fixture", nil
}

type oauthLifecycleExchange struct {
	gate  *oauthLifecycleGate
	calls atomic.Int32
}

func (exchange *oauthLifecycleExchange) ExchangeCode(ctx context.Context, _ string, _ domain.OAuthApp, _ string) (domain.OAuthToken, error) {
	exchange.calls.Add(1)
	if exchange.gate != nil {
		if err := exchange.gate.wait(ctx); err != nil {
			return domain.OAuthToken{}, err
		}
	}
	return domain.OAuthToken{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccessTokenTTL: time.Hour, RefreshTokenTTL: 24 * time.Hour}, nil
}

func (*oauthLifecycleExchange) RefreshToken(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, errors.New("unexpected refresh")
}

func (*oauthLifecycleExchange) GetAuthorizedUser(context.Context, string, string) (domain.AuthorizedUser, error) {
	return domain.AuthorizedUser{ID: "7001", AppID: "123"}, nil
}

type oauthLifecycleDiscovery struct {
	gate *oauthLifecycleGate
	fail bool
}

func (discovery oauthLifecycleDiscovery) Discover(ctx context.Context, _, _ string) (domain.AdvertiserSnapshot, error) {
	if discovery.gate != nil {
		if err := discovery.gate.wait(ctx); err != nil {
			return domain.AdvertiserSnapshot{}, err
		}
	}
	if discovery.fail {
		return domain.AdvertiserSnapshot{}, errors.New("fixture discovery failure")
	}
	return domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}, nil
}

type oauthLifecycleStore struct {
	auth.AuthorizationStore
	gate        *oauthLifecycleGate
	updates     int
	afterCommit bool
}

func (store *oauthLifecycleStore) UpdateChannel(ctx context.Context, channel string, update func(map[string]any) error) error {
	store.updates++
	if store.updates != 2 {
		return store.AuthorizationStore.UpdateChannel(ctx, channel, update)
	}
	if store.afterCommit {
		if err := store.AuthorizationStore.UpdateChannel(ctx, channel, update); err != nil {
			return err
		}
		_ = store.gate.wait(ctx)
		return nil
	}
	if err := store.gate.wait(ctx); err != nil {
		return err
	}
	return store.AuthorizationStore.UpdateChannel(ctx, channel, update)
}

type oauthLifecycleFixture struct {
	request  localOAuthRequest
	store    filesystem.AuthorizationStore
	exchange *oauthLifecycleExchange
	client   *http.Client
	opened   chan string
	done     chan localOAuthEvent
	finished chan struct{}
	cancel   context.CancelFunc
	gates    []*oauthLifecycleGate
}

func newOAuthLifecycleFixture(t *testing.T) *oauthLifecycleFixture {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	credentials := &oauthLifecycleCredentials{entries: map[string]map[string]any{"oceanengine-app-qianchuan": {"app_id": "123", "secret": "fixture-secret"}}}
	fixture := &oauthLifecycleFixture{store: filesystem.AuthorizationStore{Root: t.TempDir()}, exchange: &oauthLifecycleExchange{}, opened: make(chan string, 1), done: make(chan localOAuthEvent, 1), finished: make(chan struct{}), client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	fixture.request = localOAuthRequest{Channel: "qianchuan", RedirectURI: "http://" + address + "/oauth/callback", Timeout: time.Minute, Credentials: credentials, Stderr: io.Discard,
		OpenURL: func(value string) error { fixture.opened <- value; return nil },
		Input:   auth.RecoveryRequest{Channel: "qianchuan", Operation: "authorize", AdvertiserID: "1001"},
		Flow: auth.Service{Credentials: credentials, OAuth: fixture.exchange, Users: fixture.exchange,
			Authorizer: auth.Authorizer{Credentials: credentials, Authorizations: fixture.store, Locks: filesystem.RefreshLocker{StateRoot: fixture.store.Root}, NewID: func() (string, error) { return "grant", nil }},
			Syncer:     auth.AdvertiserSnapshotSync{Discovery: oauthLifecycleDiscovery{}}},
	}
	t.Cleanup(func() {
		for _, gate := range fixture.gates {
			gate.release()
		}
		if fixture.cancel != nil {
			fixture.cancel()
			awaitOAuthLifecycle(t, fixture.finished)
		}
		fixture.client.CloseIdleConnections()
	})
	return fixture
}

func (fixture *oauthLifecycleFixture) block(stage string) *oauthLifecycleGate {
	gate := newOAuthLifecycleGate()
	fixture.gates = append(fixture.gates, gate)
	switch stage {
	case "exchange":
		fixture.exchange.gate = gate
	case "discovery":
		fixture.request.Flow.Syncer.Discovery = oauthLifecycleDiscovery{gate: gate}
	case "commit", "committed":
		fixture.request.Flow.Authorizer.Authorizations = &oauthLifecycleStore{AuthorizationStore: fixture.store, gate: gate, afterCommit: stage == "committed"}
	}
	return gate
}

func (fixture *oauthLifecycleFixture) start(t *testing.T) *url.URL {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	fixture.cancel = cancel
	go func() {
		defer close(fixture.finished)
		result, err := runLocalOAuth(ctx, fixture.request)
		fixture.done <- localOAuthEvent{result: result, err: err}
	}()
	select {
	case value := <-fixture.opened:
		parsed, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	case event := <-fixture.done:
		t.Fatalf("local OAuth failed before browser start: %v", event.err)
	case <-time.After(3 * time.Second):
		t.Fatal("local OAuth did not start")
	}
	return nil
}

func (fixture *oauthLifecycleFixture) callbackURL(official *url.URL) string {
	return fixture.request.RedirectURI + "?" + url.Values{"state": {official.Query().Get("state")}, "auth_code": {"fixture-code"}}.Encode()
}

type oauthPageResult struct {
	status int
	body   string
	err    error
}

func (fixture *oauthLifecycleFixture) getAsync(ctx context.Context, address string) <-chan oauthPageResult {
	result := make(chan oauthPageResult, 1)
	go func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			result <- oauthPageResult{err: err}
			return
		}
		response, err := fixture.client.Do(request)
		if err != nil {
			result <- oauthPageResult{err: err}
			return
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		result <- oauthPageResult{status: response.StatusCode, body: string(body), err: err}
	}()
	return result
}

func awaitOAuthPage(t *testing.T, result <-chan oauthPageResult) oauthPageResult {
	t.Helper()
	select {
	case page := <-result:
		return page
	case <-time.After(5 * time.Second):
		t.Fatal("callback response did not finish")
	}
	return oauthPageResult{}
}

func (fixture *oauthLifecycleFixture) result(t *testing.T) localOAuthEvent {
	t.Helper()
	select {
	case event := <-fixture.done:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("local OAuth command did not finish")
	}
	return localOAuthEvent{}
}

func (fixture *oauthLifecycleFixture) assertRunning(t *testing.T) {
	t.Helper()
	select {
	case <-fixture.finished:
		t.Fatal("local OAuth command returned before worker cleanup")
	default:
	}
}

func (fixture *oauthLifecycleFixture) assertReleased(t *testing.T) {
	t.Helper()
	redirect, _ := url.Parse(fixture.request.RedirectURI)
	listener, err := net.Listen("tcp4", redirect.Host)
	if err != nil {
		t.Fatalf("CLI left callback listener bound: %v", err)
	}
	_ = listener.Close()
}

func TestLocalOAuthWaitsForCommitAndDeliversFinalPage(t *testing.T) {
	fixture := newOAuthLifecycleFixture(t)
	gate := fixture.block("commit")
	official := fixture.start(t)
	invalid := fixture.request.RedirectURI + "?state=marketing%3Awrong&auth_code=fixture-code"
	if page := awaitOAuthPage(t, fixture.getAsync(context.Background(), invalid)); page.err != nil || page.status != http.StatusForbidden {
		t.Fatalf("invalid state was not rejected: %+v", page)
	}
	callback := fixture.getAsync(context.Background(), fixture.callbackURL(official))
	awaitOAuthLifecycle(t, gate.entered)
	fixture.assertRunning(t)
	select {
	case page := <-callback:
		t.Fatalf("callback reported completion before commit: %+v", page)
	default:
	}
	if page := awaitOAuthPage(t, fixture.getAsync(context.Background(), fixture.callbackURL(official))); page.err != nil || page.status != http.StatusConflict {
		t.Fatalf("in-flight callback replay was not rejected: %+v", page)
	}
	gate.release()
	page := awaitOAuthPage(t, callback)
	if page.err != nil || page.status != http.StatusOK || !strings.Contains(page.body, "授权及账户同步已完成") {
		t.Fatalf("final page was not delivered: %+v", page)
	}
	event := fixture.result(t)
	if event.err != nil || event.result.Authorization.AuthorizationID != "grant" || event.result.Authorization.AuthorizedAccounts != 1 {
		t.Fatalf("incomplete command result: %+v", event)
	}
	stored, err := fixture.store.LoadChannel(context.Background(), "qianchuan")
	if err != nil || configuration.Object(configuration.Object(stored["authorizations"])["grant"])["pending_account_sync"] != false {
		t.Fatal("CLI succeeded before activating snapshot")
	}
	if fixture.exchange.calls.Load() != 1 {
		t.Fatal("CLI exchanged the callback code more than once")
	}
	fixture.assertReleased(t)
}

func TestLocalOAuthBrowserDisconnectDoesNotCancelAuthorization(t *testing.T) {
	fixture := newOAuthLifecycleFixture(t)
	gate := fixture.block("exchange")
	official := fixture.start(t)
	browser, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	callback := fixture.getAsync(browser, fixture.callbackURL(official))
	awaitOAuthLifecycle(t, gate.entered)
	disconnect()
	if page := awaitOAuthPage(t, callback); page.err == nil {
		t.Fatal("browser request did not disconnect")
	}
	fixture.assertRunning(t)
	select {
	case <-gate.cancelled:
		t.Fatal("browser disconnect cancelled the authorization worker")
	default:
	}
	if page := awaitOAuthPage(t, fixture.getAsync(context.Background(), fixture.callbackURL(official))); page.err != nil || page.status != http.StatusConflict {
		t.Fatalf("listener did not remain active: %+v", page)
	}
	gate.release()
	if event := fixture.result(t); event.err != nil || event.result.Authorization.AuthorizationID != "grant" {
		t.Fatalf("disconnected browser interrupted authorization: %+v", event)
	}
	fixture.assertReleased(t)
}

func TestLocalOAuthCancellationAndExpiryWaitForWorker(t *testing.T) {
	for _, stage := range []string{"exchange", "discovery", "commit"} {
		for _, stop := range []string{"cancel", "expiry"} {
			t.Run(stage+"/"+stop, func(t *testing.T) {
				fixture := newOAuthLifecycleFixture(t)
				gate := fixture.block(stage)
				if stop == "expiry" {
					fixture.request.Timeout = 250 * time.Millisecond
				}
				official := fixture.start(t)
				callback := fixture.getAsync(context.Background(), fixture.callbackURL(official))
				awaitOAuthLifecycle(t, gate.entered)
				if stop == "cancel" {
					fixture.cancel()
				}
				awaitOAuthLifecycle(t, gate.cancelled)
				fixture.assertRunning(t)
				if page := awaitOAuthPage(t, fixture.getAsync(context.Background(), fixture.callbackURL(official))); page.err != nil || page.status != http.StatusConflict {
					t.Fatalf("listener closed before worker cleanup: %+v", page)
				}
				gate.release()
				if page := awaitOAuthPage(t, callback); page.err != nil || strings.Contains(page.body, "授权及账户同步已完成") {
					t.Fatalf("cancelled callback reported success: %+v", page)
				}
				event := fixture.result(t)
				var domainErr *domain.Error
				expected := "authorization_cancelled"
				if stop == "expiry" {
					expected = "authorization_expired"
				}
				if !errors.As(event.err, &domainErr) || domainErr.Code != expected {
					t.Fatalf("incorrect stop error: %v", event.err)
				}
				pending := stage != "exchange"
				if domainErr.Details["sync_required"] != pending || pending && domainErr.Details["authorization_id"] != "grant" {
					t.Fatalf("stop hid persisted state: %+v", domainErr.Details)
				}
				fixture.assertReleased(t)
			})
		}
	}
}

func TestLocalOAuthCommittedSuccessWinsConcurrentCancellation(t *testing.T) {
	fixture := newOAuthLifecycleFixture(t)
	gate := fixture.block("committed")
	official := fixture.start(t)
	callback := fixture.getAsync(context.Background(), fixture.callbackURL(official))
	awaitOAuthLifecycle(t, gate.entered)
	fixture.cancel()
	awaitOAuthLifecycle(t, gate.cancelled)
	fixture.assertRunning(t)
	gate.release()
	if page := awaitOAuthPage(t, callback); page.err != nil || !strings.Contains(page.body, "授权及账户同步已完成") {
		t.Fatalf("committed authorization was reported as cancelled: %+v", page)
	}
	if event := fixture.result(t); event.err != nil || event.result.Authorization.AuthorizationID != "grant" {
		t.Fatalf("committed result lost: %+v", event)
	}
	fixture.assertReleased(t)
}

func TestLocalOAuthSetupAndConfigurationOnly(t *testing.T) {
	for _, mode := range []string{"missing-app", "configure-only"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newOAuthLifecycleFixture(t)
			if mode == "missing-app" {
				fixture.request.Credentials.(*oauthLifecycleCredentials).entries = map[string]map[string]any{}
			} else {
				fixture.request.ConfigureOnly = true
			}
			setup := fixture.start(t)
			if setup.Path != "/oauth/setup" || setup.Query().Get("setup_token") == "" {
				t.Fatal("CLI did not offer local application setup")
			}
			response, err := fixture.client.PostForm(setup.Scheme+"://"+setup.Host+"/oauth/setup", url.Values{"setup_token": {setup.Query().Get("setup_token")}, "app_id": {"123"}, "secret": {"fixture-secret"}})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if mode == "configure-only" {
				if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "安全保存") {
					t.Fatal("application configuration page was not delivered")
				}
			} else {
				if response.StatusCode != http.StatusSeeOther {
					t.Fatalf("setup did not redirect to official OAuth: %d", response.StatusCode)
				}
				official, err := response.Location()
				if err != nil {
					t.Fatal(err)
				}
				if page := awaitOAuthPage(t, fixture.getAsync(context.Background(), fixture.callbackURL(official))); page.err != nil || !strings.Contains(page.body, "授权及账户同步已完成") {
					t.Fatalf("authorization after setup failed: %+v", page)
				}
			}
			if event := fixture.result(t); event.err != nil {
				t.Fatalf("setup flow failed: %v", event.err)
			}
			fixture.assertReleased(t)
		})
	}
}

func TestLocalOAuthReauthorizationUsesExplicitGrantAndPreservesFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "discovery-failed"}[fail], func(t *testing.T) {
			fixture := newOAuthLifecycleFixture(t)
			_, err := fixture.request.Flow.Authorize(context.Background(), auth.RecoveryRequest{Channel: "qianchuan", Operation: "authorize"}, "fixture-initial-code", nil)
			if err != nil {
				t.Fatal(err)
			}
			fixture.request.Input.Operation, fixture.request.Input.AuthorizationID = "reauthorize", "grant"
			fixture.request.Flow.Syncer.Discovery = oauthLifecycleDiscovery{fail: fail}
			official := fixture.start(t)
			page := awaitOAuthPage(t, fixture.getAsync(context.Background(), fixture.callbackURL(official)))
			if page.err != nil {
				t.Fatal(page.err)
			}
			event := fixture.result(t)
			if (event.err != nil) != fail {
				t.Fatalf("reauthorization result: %v", event.err)
			}
			stored, err := fixture.store.ReadChannel(context.Background(), "qianchuan")
			if err != nil || len(stored.Authorizations) != 1 {
				t.Fatal("reauthorization duplicated grant")
			}
			expected := 2
			if stored.Authorizations[0].AuthorizationID != "grant" || stored.Authorizations[0].TokenRevision != expected {
				t.Fatal("reauthorization activated wrong revision")
			}
			if stored.Authorizations[0].PendingAccountSync != fail {
				t.Fatal("reauthorization did not retain the saved grant for account sync")
			}
			fixture.assertReleased(t)
		})
	}
}
