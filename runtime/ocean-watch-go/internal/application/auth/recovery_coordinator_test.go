package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type recoveryOAuthFunc func(context.Context) (domain.OAuthToken, error)

func (exchange recoveryOAuthFunc) ExchangeCode(ctx context.Context, _ string, _ domain.OAuthApp, _ string) (domain.OAuthToken, error) {
	return exchange(ctx)
}
func (recoveryOAuthFunc) RefreshToken(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, errors.New("unexpected refresh")
}

func (recoveryOAuthFunc) GetAuthorizedUser(context.Context, string, string) (domain.AuthorizedUser, error) {
	return domain.AuthorizedUser{ID: "7001", AppID: "123"}, nil
}

type recoveryDiscoveryFunc func(context.Context) (domain.AdvertiserSnapshot, error)

func (discover recoveryDiscoveryFunc) Discover(ctx context.Context, _, _ string) (domain.AdvertiserSnapshot, error) {
	return discover(ctx)
}

type recoveryGate struct {
	entered   chan struct{}
	cancelled chan struct{}
	released  chan struct{}
	once      sync.Once
}

func newRecoveryGate(t *testing.T) *recoveryGate {
	t.Helper()
	gate := &recoveryGate{entered: make(chan struct{}), cancelled: make(chan struct{}), released: make(chan struct{})}
	t.Cleanup(gate.release)
	return gate
}
func (gate *recoveryGate) release() { gate.once.Do(func() { close(gate.released) }) }
func (gate *recoveryGate) wait(ctx context.Context) error {
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
func awaitRecoverySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("authorization stage did not settle")
	}
}

type recoveryCommitStore struct {
	AuthorizationStore
	gate    *recoveryGate
	updates int
}

func (store *recoveryCommitStore) UpdateChannel(ctx context.Context, channel string, update func(map[string]any) error) error {
	store.updates++
	if store.updates == 2 {
		if err := store.gate.wait(ctx); err != nil {
			return err
		}
	}
	return store.AuthorizationStore.UpdateChannel(ctx, channel, update)
}

func recoveryCoordinatorFixture(t *testing.T, lifetime time.Duration, configure func(*Service)) (*RecoveryCoordinator, *memoryAuthorizationStore, string) {
	t.Helper()
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"qianchuan": {}}}
	credentials := &memoryCredentials{entries: map[string]map[string]any{"oceanengine-app-qianchuan": {"app_id": "123", "secret": "fixture"}}}
	snapshot := domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}
	flow := Service{Credentials: credentials, OAuth: recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
		return domain.OAuthToken{AccessToken: "access", RefreshToken: "refresh"}, nil
	}),
		Users:      recoveryOAuthFunc(nil),
		Authorizer: Authorizer{Credentials: credentials, Authorizations: store, Locks: &keyedMemoryLocker{locks: make(map[string]*sync.Mutex)}, NewID: func() (string, error) { return "grant", nil }},
		Syncer:     AdvertiserSnapshotSync{Discovery: staticDiscovery{snapshot: snapshot}}}
	if configure != nil {
		configure(&flow)
	}
	state, err := domain.BuildOAuthState("qianchuan", "fixture-nonce")
	if err != nil {
		t.Fatal(err)
	}
	input := RecoveryRequest{Channel: "qianchuan", Operation: "authorize", AdvertiserID: "1001"}
	coordinator := NewRecoveryCoordinator(context.Background(), input, RecoverySession{Channel: input.Channel, Operation: input.Operation, AdvertiserID: input.AdvertiserID, Status: "awaiting_callback"}, state, lifetime, flow)
	t.Cleanup(func() { coordinator.RequestCancel(); awaitRecoverySignal(t, coordinator.Done()) })
	return coordinator, store, state
}

func TestRecoveryCoordinatorCompletesOnlyAfterSnapshotCommit(t *testing.T) {
	gate := newRecoveryGate(t)
	coordinator, store, state := recoveryCoordinatorFixture(t, time.Minute, func(flow *Service) {
		flow.Authorizer.Authorizations = &recoveryCommitStore{AuthorizationStore: flow.Authorizer.Authorizations, gate: gate}
	})
	t.Cleanup(gate.release)
	if err := coordinator.AcceptCallback(state, "code", ""); err != nil {
		t.Fatal(err)
	}
	awaitRecoverySignal(t, gate.entered)
	if view := coordinator.View(); view.Status != "syncing" || view.CanResume {
		t.Fatalf("premature success: %+v", view)
	}
	select {
	case <-coordinator.Done():
		t.Fatal("worker exited before commit")
	default:
	}
	gate.release()
	awaitRecoverySignal(t, coordinator.Done())
	view := coordinator.View()
	if view.Status != "completed" || !view.CanResume || view.AccountCount != 1 {
		t.Fatalf("incomplete final result: %+v", view)
	}
	stored, err := store.LoadChannel(context.Background(), "qianchuan")
	if err != nil || stored["advertiser_index"] == nil {
		t.Fatal("completed before account activation")
	}
}

func TestRecoveryCoordinatorCancellationDuringUserLookupDoesNotSaveUnverifiedGrant(t *testing.T) {
	gate := newRecoveryGate(t)
	coordinator, store, state := recoveryCoordinatorFixture(t, time.Minute, func(flow *Service) {
		flow.Users = authorizedUserFunc(func(ctx context.Context, _, _ string) (domain.AuthorizedUser, error) {
			return domain.AuthorizedUser{}, gate.wait(ctx)
		})
	})
	t.Cleanup(gate.release)
	if err := coordinator.AcceptCallback(state, "fixture-code", ""); err != nil {
		t.Fatal(err)
	}
	awaitRecoverySignal(t, gate.entered)
	coordinator.RequestCancel()
	awaitRecoverySignal(t, gate.cancelled)
	if RecoveryTerminal(coordinator.View().Status) {
		t.Fatal("cancellation completed while user lookup worker was running")
	}
	gate.release()
	awaitRecoverySignal(t, coordinator.Done())
	view := coordinator.View()
	if view.Status != "cancelled" || view.SyncRequired {
		t.Fatalf("cancelled user lookup created a recoverable grant: %+v", view)
	}
	stored, _ := store.LoadChannel(context.Background(), "qianchuan")
	if stored["authorizations"] != nil {
		t.Fatal("user identity was not verified before persistence")
	}
}

func TestRecoveryCoordinatorClaimsCallbackOnceAndIgnoresInvalidState(t *testing.T) {
	gate := newRecoveryGate(t)
	var exchanges atomic.Int32
	coordinator, _, state := recoveryCoordinatorFixture(t, time.Minute, func(flow *Service) {
		flow.OAuth = recoveryOAuthFunc(func(ctx context.Context) (domain.OAuthToken, error) {
			exchanges.Add(1)
			return domain.OAuthToken{}, gate.wait(ctx)
		})
	})
	t.Cleanup(gate.release)
	if coordinator.AcceptCallback("marketing:wrong", "code", "") == nil {
		t.Fatal("accepted invalid state")
	}
	if coordinator.View().Status != "awaiting_callback" {
		t.Fatal("invalid callback ended the valid session")
	}
	if err := coordinator.AcceptCallback(state, "code", ""); err != nil {
		t.Fatal(err)
	}
	awaitRecoverySignal(t, gate.entered)
	var callers sync.WaitGroup
	for range 12 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if coordinator.AcceptCallback(state, "code", "") == nil {
				t.Error("accepted duplicate callback")
			}
		}()
	}
	callers.Wait()
	if exchanges.Load() != 1 {
		t.Fatal("callback exchanged more than once")
	}
	coordinator.RequestCancel()
	awaitRecoverySignal(t, gate.cancelled)
	select {
	case <-coordinator.Done():
		t.Fatal("cancelled before exchange worker exited")
	default:
	}
	if terminal := RecoveryTerminal(coordinator.View().Status); terminal {
		t.Fatal("terminal status exposed while worker was running")
	}
	gate.release()
	awaitRecoverySignal(t, coordinator.Done())
	if coordinator.View().Status != "cancelled" {
		t.Fatalf("cancel result: %+v", coordinator.View())
	}
}

func TestRecoveryCoordinatorCancellationExpiryAndServiceFailureWaitForWorker(t *testing.T) {
	for _, stop := range []string{"cancelled", "expired", "failed"} {
		t.Run(stop, func(t *testing.T) {
			gate := newRecoveryGate(t)
			lifetime := time.Minute
			if stop == "expired" {
				lifetime = 100 * time.Millisecond
			}
			coordinator, _, state := recoveryCoordinatorFixture(t, lifetime, func(flow *Service) {
				flow.Syncer.Discovery = recoveryDiscoveryFunc(func(ctx context.Context) (domain.AdvertiserSnapshot, error) {
					return domain.AdvertiserSnapshot{}, gate.wait(ctx)
				})
			})
			t.Cleanup(gate.release)
			if err := coordinator.AcceptCallback(state, "code", ""); err != nil {
				t.Fatal(err)
			}
			awaitRecoverySignal(t, gate.entered)
			if stop == "cancelled" {
				coordinator.RequestCancel()
			}
			if stop == "failed" {
				coordinator.FailService()
			}
			awaitRecoverySignal(t, gate.cancelled)
			if RecoveryTerminal(coordinator.View().Status) {
				t.Fatal("terminal state preceded worker cleanup")
			}
			gate.release()
			awaitRecoverySignal(t, coordinator.Done())
			view := coordinator.View()
			if view.Status != stop || view.CanResume || !view.SyncRequired || view.AuthorizationID != "grant" {
				t.Fatalf("lost pending authorization on stop: %+v", view)
			}
			if stop == "failed" && view.ErrorCode != "AUTHORIZATION_SERVICE_FAILED" {
				t.Fatalf("service failure hidden: %+v", view)
			}
		})
	}
}

func TestServicePrepareDoesNotInferReauthorizationFromAdvertiser(t *testing.T) {
	credentials := &memoryCredentials{entries: map[string]map[string]any{"oceanengine-app-qianchuan": {"app_id": "123", "secret": "fixture"}}}
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"qianchuan": {
		"advertiser_index": map[string]any{"1001": []any{"grant-a", "grant-b"}},
		"authorizations":   map[string]any{"grant-a": map[string]any{}},
	}}}
	flow := Service{Credentials: credentials, Authorizer: Authorizer{Authorizations: store}, OAuth: recoveryOAuthFunc(nil)}
	for _, input := range []RecoveryRequest{
		{Channel: "qianchuan", AdvertiserID: "1001"},
		{Channel: "qianchuan", Operation: "authorize", AdvertiserID: "1001"},
		{Channel: "qianchuan", Operation: "reauthorize", AuthorizationID: "grant-a", AdvertiserID: "1001"},
	} {
		prepared, _, err := flow.Prepare(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.AuthorizationID != input.AuthorizationID || prepared.RebindExisting {
			t.Fatalf("advertiser silently selected a grant: %+v", prepared)
		}
		if input.AuthorizationID == "" && prepared.Operation != "authorize" {
			t.Fatal("new authorization became reauthorization")
		}
	}
	for _, input := range []RecoveryRequest{
		{Channel: "qianchuan", Operation: "authorize", AuthorizationID: "grant-a"},
		{Channel: "qianchuan", Operation: "reauthorize"},
		{Channel: "qianchuan", Operation: "sync_accounts"},
		{Channel: "qianchuan", Operation: "invalid"},
		{Channel: "qianchuan", AdvertiserID: "invalid"},
		{Channel: "invalid"},
	} {
		if _, _, err := flow.Prepare(context.Background(), input); err == nil {
			t.Fatalf("invalid recovery request accepted: %+v", input)
		}
	}
}

func TestRecoveryCoordinatorRejectedCallbackAndFailureAreTerminal(t *testing.T) {
	for _, test := range []struct{ name, code, providerError, expected string }{
		{"rejected", "", "access_denied", "OAUTH_REJECTED"},
		{"exchange failure", "code", "", "AUTHORIZATION_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, _, state := recoveryCoordinatorFixture(t, time.Minute, func(flow *Service) {
				flow.OAuth = recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
					return domain.OAuthToken{}, errors.New("exchange failed")
				})
			})
			if err := coordinator.AcceptCallback(state, test.code, test.providerError); err != nil {
				t.Fatal(err)
			}
			awaitRecoverySignal(t, coordinator.Done())
			view := coordinator.View()
			if view.Status != "failed" || view.ErrorCode != test.expected || view.CanResume {
				t.Fatalf("failure result: %+v", view)
			}
			coordinator.RequestCancel()
			if coordinator.View() != view {
				t.Fatal("cancellation overwrote terminal result")
			}
		})
	}
}
