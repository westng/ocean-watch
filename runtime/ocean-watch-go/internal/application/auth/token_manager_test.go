package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type memoryCredentials struct {
	mu          sync.Mutex
	entries     map[string]map[string]any
	failAccount string
}

func (store *memoryCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return configuration.CloneMap(store.entries[account]), nil
}

func (store *memoryCredentials) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if account == store.failAccount {
		return "", errors.New("injected credential write failure")
	}
	store.entries[account] = configuration.CloneMap(value)
	return "memory", nil
}

type memoryAuthorizationStore struct {
	mu         sync.Mutex
	channels   map[string]map[string]any
	failUpdate bool
}

func (store *memoryAuthorizationStore) LoadChannel(_ context.Context, channel string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return configuration.CloneMap(store.channels[channel]), nil
}

func (store *memoryAuthorizationStore) UpdateChannel(
	_ context.Context,
	channel string,
	update func(map[string]any) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.failUpdate {
		return errors.New("injected authorization activation failure")
	}
	candidate := configuration.CloneMap(store.channels[channel])
	if err := update(candidate); err != nil {
		return err
	}
	store.channels[channel] = candidate
	return nil
}

type keyedMemoryLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (locker *keyedMemoryLocker) Acquire(_ context.Context, channel, authorizationID string) (func() error, error) {
	key := channel + ":" + authorizationID
	locker.mu.Lock()
	lock := locker.locks[key]
	if lock == nil {
		lock = new(sync.Mutex)
		locker.locks[key] = lock
	}
	locker.mu.Unlock()
	lock.Lock()
	return func() error { lock.Unlock(); return nil }, nil
}

type countingOAuth struct {
	mu     sync.Mutex
	counts map[string]int
	delay  time.Duration
}

func (oauth *countingOAuth) ExchangeCode(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, errors.New("not implemented in this fixture")
}

func (oauth *countingOAuth) RefreshToken(
	ctx context.Context,
	channel string,
	_ domain.OAuthApp,
	_ string,
) (domain.OAuthToken, error) {
	if oauth.delay > 0 {
		timer := time.NewTimer(oauth.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return domain.OAuthToken{}, ctx.Err()
		case <-timer.C:
		}
	}
	oauth.mu.Lock()
	oauth.counts[channel]++
	count := oauth.counts[channel]
	oauth.mu.Unlock()
	return domain.OAuthToken{
		AccessToken:    "new-" + channel + "-access-" + fmt.Sprint(count),
		RefreshToken:   "new-" + channel + "-refresh-" + fmt.Sprint(count),
		AccessTokenTTL: time.Hour, RefreshTokenTTL: 2 * time.Hour,
	}, nil
}

func TestTokenRefreshSingleflightAndIsolation(t *testing.T) {
	now := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	credentials := tokenFixtureCredentials(now, "marketing", "shared")
	authorizations := &memoryAuthorizationStore{channels: map[string]map[string]any{
		"marketing": tokenFixtureState("shared"),
	}}
	oauth := &countingOAuth{counts: map[string]int{}, delay: 10 * time.Millisecond}
	manager := TokenManager{
		Credentials: credentials, Authorizations: authorizations,
		Locks: &keyedMemoryLocker{locks: map[string]*sync.Mutex{}}, OAuth: oauth,
		Now: func() time.Time { return now },
	}

	const callers = 50
	results := make(chan TokenLease, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			lease, err := manager.Ensure(context.Background(), TokenQuery{
				Channel: "marketing", AuthorizationID: "shared",
			})
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- lease
		}()
	}
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
	for lease := range results {
		if lease.AccessToken != "new-marketing-access-1" || lease.TokenRevision != 2 {
			t.Fatalf("waiter received the wrong lease: %#v", lease)
		}
	}
	if oauth.counts["marketing"] != 1 {
		t.Fatalf("refresh count = %d, want 1", oauth.counts["marketing"])
	}

	credentials.entries["oceanengine-app-qianchuan"] = map[string]any{"app_id": "200", "secret": "qianchuan-secret"}
	credentials.entries["oceanengine-auth-qianchuan-shared-r1"] = map[string]any{
		"access_token": "old-qianchuan-access", "refresh_token": "old-qianchuan-refresh",
		"access_token_expires_at":  now.Add(-time.Hour).Format(time.RFC3339),
		"refresh_token_expires_at": now.Add(time.Hour).Format(time.RFC3339),
	}
	authorizations.channels["qianchuan"] = tokenFixtureState("shared")
	lease, err := manager.Ensure(context.Background(), TokenQuery{Channel: "qianchuan", AuthorizationID: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if lease.AccessToken != "new-qianchuan-access-1" || oauth.counts["qianchuan"] != 1 {
		t.Fatalf("cross-channel refresh was not isolated: %#v counts=%#v", lease, oauth.counts)
	}
	if marketing, _ := credentials.Read(context.Background(), "oceanengine-auth-marketing-shared-r2"); marketing["access_token"] != "new-marketing-access-1" {
		t.Fatal("qianchuan refresh overwrote marketing credentials")
	}
}

func TestTokenRefreshFailureKeepsOldRevisionActive(t *testing.T) {
	now := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		failWrite  bool
		failUpdate bool
	}{
		{name: "credential-write", failWrite: true},
		{name: "state-activation", failUpdate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			credentials := tokenFixtureCredentials(now, "marketing", "stable")
			if test.failWrite {
				credentials.failAccount = "oceanengine-auth-marketing-stable-r2"
			}
			authorizations := &memoryAuthorizationStore{
				channels:   map[string]map[string]any{"marketing": tokenFixtureState("stable")},
				failUpdate: test.failUpdate,
			}
			manager := TokenManager{
				Credentials: credentials, Authorizations: authorizations,
				Locks: &keyedMemoryLocker{locks: map[string]*sync.Mutex{}},
				OAuth: &countingOAuth{counts: map[string]int{}}, Now: func() time.Time { return now },
			}
			if _, err := manager.Ensure(context.Background(), TokenQuery{
				Channel: "marketing", AuthorizationID: "stable",
			}); err == nil {
				t.Fatal("injected persistence failure was ignored")
			}
			state, _ := authorizations.LoadChannel(context.Background(), "marketing")
			metadata := configuration.Object(configuration.Object(state["authorizations"])["stable"])
			if fmt.Sprint(metadata["token_revision"]) != "1" || fmt.Sprint(state["generation"]) != "1" {
				t.Fatalf("failed refresh activated a new revision: %#v", state)
			}
			old, _ := credentials.Read(context.Background(), "oceanengine-auth-marketing-stable-r1")
			if old["access_token"] != "old-marketing-access" {
				t.Fatal("failed refresh overwrote the active credential")
			}
		})
	}
}

func TestForceRefreshWaiterReusesConcurrentRotation(t *testing.T) {
	now := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	credentials := tokenFixtureCredentials(now, "marketing", "forced")
	credentials.entries["oceanengine-auth-marketing-forced-r1"]["access_token_expires_at"] = now.Add(time.Hour).Format(time.RFC3339)
	oauth := &countingOAuth{counts: map[string]int{}, delay: 10 * time.Millisecond}
	manager := TokenManager{
		Credentials: credentials,
		Authorizations: &memoryAuthorizationStore{channels: map[string]map[string]any{
			"marketing": tokenFixtureState("forced"),
		}},
		Locks: &keyedMemoryLocker{locks: map[string]*sync.Mutex{}}, OAuth: oauth,
		Now: func() time.Time { return now },
	}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := manager.Ensure(context.Background(), TokenQuery{
				Channel: "marketing", AuthorizationID: "forced", ForceRefresh: true,
			})
			errorsSeen <- err
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if oauth.counts["marketing"] != 1 {
		t.Fatalf("forced concurrent refresh count = %d, want 1", oauth.counts["marketing"])
	}
}

func tokenFixtureCredentials(now time.Time, channel, authorizationID string) *memoryCredentials {
	return &memoryCredentials{entries: map[string]map[string]any{
		"oceanengine-app-" + channel: {"app_id": "100", "secret": channel + "-secret"},
		"oceanengine-auth-" + channel + "-" + authorizationID + "-r1": {
			"access_token": "old-" + channel + "-access", "refresh_token": "old-" + channel + "-refresh",
			"access_token_expires_at":  now.Add(-time.Hour).Format(time.RFC3339),
			"refresh_token_expires_at": now.Add(time.Hour).Format(time.RFC3339),
		},
	}}
}

func tokenFixtureState(authorizationID string) map[string]any {
	return map[string]any{
		"generation": 1,
		"authorizations": map[string]any{
			authorizationID: map[string]any{
				"token_revision": 1, "pending_account_sync": false,
				"authorized_accounts": []any{}, "advertiser_ids": []any{},
			},
		},
		"account_index": map[string]any{}, "advertiser_index": map[string]any{},
	}
}
