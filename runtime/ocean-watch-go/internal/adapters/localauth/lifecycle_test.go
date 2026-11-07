package localauth

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type lifecycleGate struct {
	entered   chan struct{}
	cancelled chan struct{}
	released  chan struct{}
	once      sync.Once
}

func newLifecycleGate() *lifecycleGate {
	return &lifecycleGate{entered: make(chan struct{}), cancelled: make(chan struct{}), released: make(chan struct{})}
}

func (gate *lifecycleGate) release() { gate.once.Do(func() { close(gate.released) }) }

func (gate *lifecycleGate) wait(ctx context.Context) error {
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

func awaitLifecycleSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("authorization lifecycle stage did not settle")
	}
}

// Trigger the deadline only after the tested worker stage has started. A short
// wall-clock deadline can expire during setup on a busy CI runner instead.
type lifecycleExpiryContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newLifecycleExpiryContext() *lifecycleExpiryContext {
	return &lifecycleExpiryContext{Context: context.Background(), done: make(chan struct{})}
}

func (ctx *lifecycleExpiryContext) Done() <-chan struct{} { return ctx.done }
func (ctx *lifecycleExpiryContext) expire()               { ctx.once.Do(func() { close(ctx.done) }) }
func (ctx *lifecycleExpiryContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

type lifecycleOAuth struct{ gate *lifecycleGate }

func (oauth lifecycleOAuth) ExchangeCode(ctx context.Context, _ string, _ domain.OAuthApp, _ string) (domain.OAuthToken, error) {
	if oauth.gate != nil {
		if err := oauth.gate.wait(ctx); err != nil {
			return domain.OAuthToken{}, err
		}
	}
	return domain.OAuthToken{AccessToken: "fixture-access", RefreshToken: "fixture-refresh"}, nil
}

func (lifecycleOAuth) RefreshToken(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, errors.New("unexpected refresh")
}

type lifecycleDiscovery struct{ gate *lifecycleGate }

func (discovery lifecycleDiscovery) Discover(ctx context.Context, _, _ string) (domain.AdvertiserSnapshot, error) {
	if discovery.gate != nil {
		if err := discovery.gate.wait(ctx); err != nil {
			return domain.AdvertiserSnapshot{}, err
		}
	}
	return domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}, nil
}

type lifecycleStore struct {
	auth.AuthorizationStore
	gate    *lifecycleGate
	updates int
	blockAt int
}

func (store *lifecycleStore) UpdateChannel(ctx context.Context, channel string, update func(map[string]any) error) error {
	store.updates++
	blockAt := store.blockAt
	if blockAt == 0 {
		blockAt = 2
	}
	if store.updates == blockAt {
		if err := store.gate.wait(ctx); err != nil {
			return err
		}
	}
	return store.AuthorizationStore.UpdateChannel(ctx, channel, update)
}

func blockLifecycleStage(fixture *flowFixture, gate *lifecycleGate, stage string) {
	fixture.manager.options.OAuth = lifecycleOAuth{}
	fixture.manager.options.Syncer.Discovery = lifecycleDiscovery{}
	switch stage {
	case "exchange":
		fixture.manager.options.OAuth = lifecycleOAuth{gate: gate}
	case "discovery":
		fixture.manager.options.Syncer.Discovery = lifecycleDiscovery{gate: gate}
	case "commit":
		fixture.manager.options.Authorizer.Authorizations = &lifecycleStore{AuthorizationStore: fixture.store, gate: gate}
	}
}

func assertListenerReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("authorization listener was not released: %v", err)
	}
	_ = listener.Close()
}

func TestAuthorizationOutlivesStartRequestAndKeepsListenerUntilCommit(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	gate := newLifecycleGate()
	t.Cleanup(gate.release)
	blockLifecycleStage(fixture, gate, "commit")
	ctx, cancel := context.WithCancel(context.Background())
	view, err := fixture.manager.Start(ctx, auth.RecoveryRequest{Channel: "qianchuan", AdvertiserID: "1001"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	fixture.callback(t, view, fixture.callbackState(t, view))
	awaitLifecycleSignal(t, gate.entered)
	current, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
	if err != nil || current.Status != "syncing" || current.CanResume {
		t.Fatalf("request return interrupted the authorization: %+v, %v", current, err)
	}
	page := fixture.localURL(t, view)
	response, err := fixture.client.Get(page.String())
	if err != nil {
		t.Fatalf("listener stopped while snapshot commit was in flight: %v", err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("in-flight page status: %d", response.StatusCode)
	}
	gate.release()
	completed := fixture.await(t, view)
	if completed.Status != "completed" || !completed.CanResume {
		t.Fatalf("authorization did not finish after request cancellation: %+v", completed)
	}
	fixture.awaitShutdown(t, completed)
	assertListenerReleased(t, page.Host)
	retained, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
	if err != nil || retained != completed {
		t.Fatal("listener shutdown lost the final session result")
	}
}

func TestAuthorizationStopWaitsForEveryInFlightStage(t *testing.T) {
	for _, stage := range []string{"exchange", "discovery", "commit"} {
		for _, stop := range []string{"cancel", "expiry", "close", "parent"} {
			t.Run(stage+"/"+stop, func(t *testing.T) {
				fixture := newFlowFixture(t, time.Minute)
				gate := newLifecycleGate()
				t.Cleanup(gate.release)
				blockLifecycleStage(fixture, gate, stage)
				var expiry *lifecycleExpiryContext
				if stop == "expiry" {
					expiry = newLifecycleExpiryContext()
					fixture.manager.parent = expiry
					t.Cleanup(expiry.expire)
				}
				var cancelParent context.CancelFunc
				if stop == "parent" {
					fixture.manager.parent, cancelParent = context.WithCancel(context.Background())
					t.Cleanup(cancelParent)
				}
				view := fixture.start(t, "1001")
				page := fixture.localURL(t, view)
				fixture.callback(t, view, fixture.callbackState(t, view))
				awaitLifecycleSignal(t, gate.entered)
				stopped := make(chan error, 1)
				switch stop {
				case "cancel":
					go func() {
						_, err := fixture.manager.Cancel(context.Background(), view.Channel, view.SessionID)
						stopped <- err
					}()
				case "close":
					go func() { fixture.manager.Close(); stopped <- nil }()
				case "parent":
					cancelParent()
				case "expiry":
					expiry.expire()
				}
				awaitLifecycleSignal(t, gate.cancelled)
				select {
				case err := <-stopped:
					t.Fatalf("stop returned while worker was still cleaning up: %v", err)
				default:
				}
				current, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
				if err != nil || terminal(current.Status) || current.CanResume {
					t.Fatalf("premature terminal state: %+v, %v", current, err)
				}
				response, err := fixture.client.Get(page.String())
				if err != nil {
					t.Fatalf("listener closed before worker exited: %v", err)
				}
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("pending stop page status: %d", response.StatusCode)
				}
				if fixture.callback(t, view, fixture.callbackState(t, view)) != http.StatusConflict {
					t.Fatal("callback replay started another worker during stop")
				}
				gate.release()
				if stop == "cancel" || stop == "close" {
					select {
					case err := <-stopped:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("stop did not wait and settle")
					}
				}
				final := fixture.await(t, view)
				expected := "cancelled"
				if stop == "expiry" {
					expected = "expired"
				}
				if final.Status != expected || final.CanResume {
					t.Fatalf("unexpected stop result: %+v", final)
				}
				if stage != "exchange" && (!final.SyncRequired || final.AuthorizationID == "") {
					t.Fatalf("stop hid pending authorization: %+v", final)
				}
				fixture.awaitShutdown(t, final)
				assertListenerReleased(t, page.Host)
				stored, err := fixture.store.LoadChannel(context.Background(), view.Channel)
				if err != nil {
					t.Fatal(err)
				}
				for _, raw := range configuration.Object(stored["authorizations"]) {
					if configuration.Object(raw)["pending_account_sync"] != true {
						t.Fatal("cancelled worker activated a snapshot")
					}
				}
			})
		}
	}
}

func TestAccountSyncWaitsForWorkerBeforeReleasingListener(t *testing.T) {
	for _, stage := range []string{"discovery", "commit"} {
		for _, stop := range []string{"complete", "cancel", "expiry", "close"} {
			t.Run(stage+"/"+stop, func(t *testing.T) {
				fixture := newFlowFixture(t, time.Minute)
				grant := fixture.authorize(t, fixture.start(t, "1001"))
				gate := newLifecycleGate()
				t.Cleanup(gate.release)
				if stage == "discovery" {
					fixture.manager.options.Syncer.Discovery = lifecycleDiscovery{gate: gate}
				} else {
					fixture.manager.options.Syncer.Authorizations = &lifecycleStore{AuthorizationStore: fixture.store, gate: gate, blockAt: 1}
				}
				var expiry *lifecycleExpiryContext
				if stop == "expiry" {
					expiry = newLifecycleExpiryContext()
					fixture.manager.parent = expiry
					t.Cleanup(expiry.expire)
				}
				view := fixture.syncStart(t, "1001", grant.AuthorizationID)
				page := fixture.localURL(t, view)
				if fixture.confirm(t, view).StatusCode != http.StatusSeeOther {
					t.Fatal("sync confirmation rejected")
				}
				awaitLifecycleSignal(t, gate.entered)
				if fixture.confirm(t, view).StatusCode != http.StatusConflict {
					t.Fatal("duplicate sync confirmation accepted")
				}
				stopped := make(chan struct{})
				switch stop {
				case "cancel":
					go func() {
						_, err := fixture.manager.Cancel(context.Background(), view.Channel, view.SessionID)
						if err != nil {
							t.Error(err)
						}
						close(stopped)
					}()
				case "close":
					go func() { fixture.manager.Close(); close(stopped) }()
				case "expiry":
					expiry.expire()
				}
				if stop != "complete" {
					awaitLifecycleSignal(t, gate.cancelled)
				}
				current, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
				if err != nil || terminal(current.Status) || current.CanResume {
					t.Fatalf("sync ended before worker returned: %+v, %v", current, err)
				}
				select {
				case <-stopped:
					t.Fatal("stop returned before sync worker cleanup")
				default:
				}
				response, err := fixture.client.Get(page.String())
				if err != nil {
					t.Fatalf("sync listener stopped before worker cleanup: %v", err)
				}
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				gate.release()
				if stop == "cancel" || stop == "close" {
					awaitLifecycleSignal(t, stopped)
				}
				final := fixture.await(t, view)
				expected := "cancelled"
				if stop == "complete" {
					expected = "completed"
				}
				if stop == "expiry" {
					expected = "expired"
				}
				if final.Status != expected || final.CanResume != (stop == "complete") || final.AuthorizationID != grant.AuthorizationID {
					t.Fatalf("unexpected sync result: %+v", final)
				}
				fixture.awaitShutdown(t, final)
				assertListenerReleased(t, page.Host)
			})
		}
	}
}

func TestCancelRequestDeadlineDoesNotAbandonWorkerCleanup(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	gate := newLifecycleGate()
	t.Cleanup(gate.release)
	blockLifecycleStage(fixture, gate, "exchange")
	view := fixture.start(t, "1001")
	page := fixture.localURL(t, view)
	fixture.callback(t, view, fixture.callbackState(t, view))
	awaitLifecycleSignal(t, gate.entered)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := fixture.manager.Cancel(ctx, view.Channel, view.SessionID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel request did not honor its own deadline: %v", err)
	}
	awaitLifecycleSignal(t, gate.cancelled)
	current, err := fixture.manager.Get(context.Background(), view.Channel, view.SessionID)
	if err != nil || terminal(current.Status) {
		t.Fatal("cancel deadline hid the running cleanup")
	}
	response, err := fixture.client.Get(page.String())
	if err != nil {
		t.Fatalf("cancel deadline closed listener early: %v", err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	gate.release()
	final := fixture.await(t, view)
	if final.Status != "cancelled" {
		t.Fatalf("background cancellation did not finish: %+v", final)
	}
	fixture.awaitShutdown(t, final)
	assertListenerReleased(t, page.Host)
}

func TestTerminalSessionWithoutBrowserReleasesListenerWithinGrace(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	view := fixture.start(t, "1001")
	page := fixture.localURL(t, view)
	completed := fixture.authorize(t, view)
	fixture.manager.mu.Lock()
	current := fixture.manager.sessions[view.SessionID]
	fixture.manager.mu.Unlock()
	if completed.Status != "completed" {
		t.Fatalf("authorization failed: %+v", completed)
	}
	select {
	case <-current.shutdownDone:
	case <-time.After(7 * time.Second):
		t.Fatal("missing browser left callback listener bound")
	}
	assertListenerReleased(t, page.Host)
}

func TestIdleTCPConnectionDoesNotBypassWorkerOrRetainListener(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	gate := newLifecycleGate()
	t.Cleanup(gate.release)
	blockLifecycleStage(fixture, gate, "exchange")
	view := fixture.start(t, "1001")
	page := fixture.localURL(t, view)
	fixture.callback(t, view, fixture.callbackState(t, view))
	awaitLifecycleSignal(t, gate.entered)
	connection, err := net.DialTimeout("tcp4", page.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	stopped := make(chan struct{})
	go func() { fixture.manager.Close(); close(stopped) }()
	awaitLifecycleSignal(t, gate.cancelled)
	fixture.manager.mu.Lock()
	current := fixture.manager.sessions[view.SessionID]
	fixture.manager.mu.Unlock()
	select {
	case <-current.serveDone:
		t.Fatal("listener stopped before authorization worker cleanup")
	default:
	}
	select {
	case <-stopped:
		t.Fatal("Runtime close returned before authorization worker cleanup")
	default:
	}
	gate.release()
	select {
	case <-stopped:
	case <-time.After(6 * time.Second):
		t.Fatal("idle TCP connection outlived the five-second HTTP shutdown budget")
	}
	if final := current.coordinator.View(); final.Status != "cancelled" {
		t.Fatalf("unexpected final state: %+v", final)
	}
	assertListenerReleased(t, page.Host)
}

func TestClosedAuthorizationManagerCannotRestartListener(t *testing.T) {
	fixture := newFlowFixture(t, time.Minute)
	view := fixture.start(t, "1001")
	page := fixture.localURL(t, view)
	fixture.manager.Close()
	fixture.manager.Close()
	if _, err := fixture.manager.Start(context.Background(), auth.RecoveryRequest{Channel: "qianchuan"}); err == nil {
		t.Fatal("closed manager started a new callback listener")
	}
	assertListenerReleased(t, page.Host)
}
