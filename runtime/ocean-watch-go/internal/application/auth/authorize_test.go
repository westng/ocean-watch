package auth

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type authorizedUserFunc func(context.Context, string, string) (domain.AuthorizedUser, error)

func (f authorizedUserFunc) GetAuthorizedUser(ctx context.Context, channel, token string) (domain.AuthorizedUser, error) {
	return f(ctx, channel, token)
}

func authorizationServiceFixture() (Service, *memoryAuthorizationStore, *memoryCredentials) {
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"marketing": {}}}
	credentials := &memoryCredentials{entries: map[string]map[string]any{"oceanengine-app-marketing": {"app_id": "123", "secret": "fixture-secret"}}}
	now := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	service := Service{Credentials: credentials,
		OAuth: recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
			return domain.OAuthToken{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccessTokenTTL: time.Hour}, nil
		}),
		Users:      recoveryOAuthFunc(nil),
		Authorizer: Authorizer{Credentials: credentials, Authorizations: store, Locks: &keyedMemoryLocker{locks: make(map[string]*sync.Mutex)}, Now: func() time.Time { return now }, NewID: func() (string, error) { return "grant", nil }},
		Syncer:     AdvertiserSnapshotSync{Discovery: staticDiscovery{snapshot: domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}}},
	}
	return service, store, credentials
}

func authorizeFixture(t *testing.T, service Service, id string) (AuthorizationResult, error) {
	t.Helper()
	operation := "authorize"
	if id != "" {
		operation = "reauthorize"
	}
	return service.Authorize(context.Background(), RecoveryRequest{Channel: "marketing", Operation: operation, AuthorizationID: id}, "fixture-code", nil)
}

func TestAuthorizationServiceSavesIdentifiedGrantBeforeAccountDiscovery(t *testing.T) {
	service, store, credentials := authorizationServiceFixture()
	var order []string
	exchange := service.OAuth
	service.OAuth = recoveryOAuthFunc(func(ctx context.Context) (domain.OAuthToken, error) {
		order = append(order, "exchange")
		return exchange.ExchangeCode(ctx, "marketing", domain.OAuthApp{}, "fixture-code")
	})
	service.Users = authorizedUserFunc(func(context.Context, string, string) (domain.AuthorizedUser, error) {
		order = append(order, "user")
		return domain.AuthorizedUser{ID: "7001", AppID: "123"}, nil
	})
	discovery := service.Syncer.Discovery
	service.Syncer.Discovery = recoveryDiscoveryFunc(func(ctx context.Context) (domain.AdvertiserSnapshot, error) {
		order = append(order, "discovery")
		state, _ := store.LoadChannel(ctx, "marketing")
		row := configuration.Object(configuration.Object(state["authorizations"])["grant"])
		if row["pending_account_sync"] != true || row["authorized_user_id"] != "7001" || row["app_id"] != "123" {
			t.Fatal("identity and durable grant must precede discovery")
		}
		saved, _ := credentials.Read(ctx, "oceanengine-auth-marketing-grant-r1")
		if saved["access_token"] != "fixture-access" {
			t.Fatal("credential was not saved before discovery")
		}
		return discovery.Discover(ctx, "marketing", "fixture-access")
	})
	result, err := authorizeFixture(t, service, "")
	if err != nil || result.AuthorizationID != "grant" || result.UserID != "7001" || result.PendingAccountSync || result.AuthorizedAccounts != 1 {
		t.Fatalf("authorization result: %+v, %v", result, err)
	}
	if !reflect.DeepEqual(order, []string{"exchange", "user", "discovery"}) {
		t.Fatalf("unexpected flow: %v", order)
	}
}

func TestAuthorizationServiceReusesSameProviderIdentityWithoutDuplicatingGrants(t *testing.T) {
	service, store, _ := authorizationServiceFixture()
	for i := 1; i <= 2; i++ {
		result, err := authorizeFixture(t, service, "")
		if err != nil || result.AuthorizationID != "grant" || result.TokenRevision != i {
			t.Fatalf("same-user login: %+v, %v", result, err)
		}
	}
	state, _ := store.LoadChannel(context.Background(), "marketing")
	if len(configuration.Object(state["authorizations"])) != 1 {
		t.Fatal("same provider identity created duplicate grants")
	}
}

func TestAuthorizationServiceKeepsSameProviderUserIsolatedByChannel(t *testing.T) {
	service, store, credentials := authorizationServiceFixture()
	store.channels["qianchuan"] = map[string]any{}
	credentials.entries["oceanengine-app-qianchuan"] = map[string]any{"app_id": "123", "secret": "fixture-secret"}
	for _, channel := range []string{"marketing", "qianchuan"} {
		result, err := service.Authorize(context.Background(), RecoveryRequest{Channel: channel, Operation: "authorize"}, "fixture-code", nil)
		if err != nil || result.Channel != channel || result.UserID != "7001" || result.TokenRevision != 1 {
			t.Fatalf("channel %s adopted another channel's grant: %+v, %v", channel, result, err)
		}
		account, _ := domain.AuthorizationCredentialAccount(channel, result.AuthorizationID, 1)
		saved, _ := credentials.Read(context.Background(), account)
		if saved["authorized_user_id"] != "7001" {
			t.Fatal("channel credential did not retain the verified user")
		}
	}
	for _, channel := range []string{"marketing", "qianchuan"} {
		state, _ := store.LoadChannel(context.Background(), channel)
		if len(configuration.Object(state["authorizations"])) != 1 {
			t.Fatal("provider identity lookup crossed channel state")
		}
	}
}

func TestAuthorizationServiceSavedGrantCanRecoverDiscoveryFailureWithoutOAuth(t *testing.T) {
	for _, reauthorize := range []bool{false, true} {
		t.Run(fmt.Sprint(reauthorize), func(t *testing.T) {
			service, store, credentials := authorizationServiceFixture()
			expectedRevision, id := 1, ""
			if reauthorize {
				if _, err := authorizeFixture(t, service, ""); err != nil {
					t.Fatal(err)
				}
				expectedRevision, id = 2, "grant"
			}
			before, _ := store.LoadChannel(context.Background(), "marketing")
			service.Syncer.Discovery = staticDiscovery{err: errors.New("middle page failed")}
			result, err := authorizeFixture(t, service, id)
			if err == nil || result.AuthorizationID != "grant" || result.TokenRevision != expectedRevision || !result.PendingAccountSync {
				t.Fatalf("saved pending result: %+v, %v", result, err)
			}
			state, _ := store.LoadChannel(context.Background(), "marketing")
			row := configuration.Object(configuration.Object(state["authorizations"])["grant"])
			if fmt.Sprint(row["token_revision"]) != fmt.Sprint(expectedRevision) || row["pending_account_sync"] != true {
				t.Fatal("saved credential was not selected for recovery")
			}
			if reauthorize && !reflect.DeepEqual(state["advertiser_index"], before["advertiser_index"]) {
				t.Fatal("failed discovery replaced the prior account snapshot")
			}
			if _, err := (&TokenManager{Credentials: credentials, Authorizations: store, Locks: service.Authorizer.Locks, OAuth: service.OAuth}).Ensure(context.Background(), TokenQuery{Channel: "marketing", AuthorizationID: "grant"}); err == nil {
				t.Fatal("pending account scope was usable by a business request")
			}
			service.Syncer = AdvertiserSnapshotSync{Tokens: staticTokenProvider{lease: TokenLease{Channel: "marketing", AuthorizationID: "grant", TokenRevision: expectedRevision, AccessToken: "fixture-access"}}, Authorizations: store, Discovery: staticDiscovery{snapshot: domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}}}
			service.OAuth = recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
				t.Fatal("account sync exchanged OAuth code")
				return domain.OAuthToken{}, nil
			})
			if _, err := service.Sync(context.Background(), RecoveryRequest{Channel: "marketing", AuthorizationID: "grant", Operation: "sync_accounts"}); err != nil {
				t.Fatal(err)
			}
			state, _ = store.LoadChannel(context.Background(), "marketing")
			if configuration.Object(configuration.Object(state["authorizations"])["grant"])["pending_account_sync"] != false {
				t.Fatal("account sync did not activate the saved grant")
			}
		})
	}
}

func TestAuthorizationServiceRejectsIdentityMismatchBeforeCredentialPersistence(t *testing.T) {
	for _, user := range []domain.AuthorizedUser{{ID: ""}, {ID: "0"}, {ID: "7001", AppID: "456"}, {ID: "7002", AppID: "123"}} {
		t.Run(fmt.Sprintf("%s-%s", user.ID, user.AppID), func(t *testing.T) {
			service, store, credentials := authorizationServiceFixture()
			if _, err := authorizeFixture(t, service, ""); err != nil {
				t.Fatal(err)
			}
			before, _ := store.LoadChannel(context.Background(), "marketing")
			service.Users = authorizedUserFunc(func(context.Context, string, string) (domain.AuthorizedUser, error) { return user, nil })
			if _, err := authorizeFixture(t, service, "grant"); err == nil {
				t.Fatal("different or missing provider identity replaced an existing grant")
			}
			after, _ := store.LoadChannel(context.Background(), "marketing")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("identity mismatch changed grant metadata")
			}
			if _, exists := credentials.entries["oceanengine-auth-marketing-grant-r2"]; exists {
				t.Fatal("identity mismatch persisted a credential")
			}
		})
	}
}

func TestAuthorizationServiceCredentialFailureKeepsOriginalRevision(t *testing.T) {
	service, store, credentials := authorizationServiceFixture()
	if _, err := authorizeFixture(t, service, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := store.LoadChannel(context.Background(), "marketing")
	credentials.failAccount = "oceanengine-auth-marketing-grant-r2"
	if _, err := authorizeFixture(t, service, "grant"); err == nil {
		t.Fatal("credential storage failure was accepted")
	}
	after, _ := store.LoadChannel(context.Background(), "marketing")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed credential persistence changed the selected revision")
	}
}

func TestAuthorizationServiceAccountConflictKeepsSavedCredentialPending(t *testing.T) {
	service, store, _ := authorizationServiceFixture()
	if _, err := authorizeFixture(t, service, ""); err != nil {
		t.Fatal(err)
	}
	_ = store.UpdateChannel(context.Background(), "marketing", func(state map[string]any) error {
		configuration.Object(state["authorizations"])["other"] = map[string]any{"token_revision": 1, "pending_account_sync": false, "authorized_accounts": []any{map[string]any{"account_id": "9002", "advertiser_ids": []any{"1002"}}}, "advertiser_ids": []any{"1002"}}
		return rebuildAuthorizationIndexes(state)
	})
	service.Syncer.Discovery = staticDiscovery{snapshot: domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9002", AdvertiserIDs: []string{"1002"}}}, AdvertiserIDs: []string{"1002"}}}
	result, err := authorizeFixture(t, service, "grant")
	if err == nil || result.TokenRevision != 2 || !result.PendingAccountSync {
		t.Fatalf("conflicting reauthorization: %+v, %v", result, err)
	}
	state, _ := store.LoadChannel(context.Background(), "marketing")
	if configuration.Object(state["account_index"])["9002"] != "other" {
		t.Fatal("reauthorization rebound another grant's account")
	}
}

func TestAuthorizationServiceDoesNotGuessUnknownOrDuplicateProviderIdentity(t *testing.T) {
	service, store, _ := authorizationServiceFixture()
	if _, err := authorizeFixture(t, service, ""); err != nil {
		t.Fatal(err)
	}
	_ = store.UpdateChannel(context.Background(), "marketing", func(state map[string]any) error {
		configuration.Object(state["authorizations"])["duplicate"] = map[string]any{"token_revision": 1, "app_id": "123", "authorized_user_id": "7001"}
		return nil
	})
	if _, err := authorizeFixture(t, service, ""); err == nil {
		t.Fatal("duplicate provider identities were silently merged")
	}
	if _, _, err := service.Prepare(context.Background(), RecoveryRequest{Channel: "marketing", Operation: "reauthorize", AuthorizationID: "missing"}); err == nil {
		t.Fatal("unknown reauthorization started OAuth")
	}
}

func TestAuthorizationServiceDoesNotAdoptLegacyGrantWithoutVerifiedIdentity(t *testing.T) {
	service, store, _ := authorizationServiceFixture()
	legacy := map[string]any{"token_revision": 1, "pending_account_sync": false, "authorized_accounts": []any{}, "advertiser_ids": []any{}}
	_ = store.UpdateChannel(context.Background(), "marketing", func(state map[string]any) error {
		state["authorizations"] = map[string]any{"legacy": configuration.CloneMap(legacy)}
		return nil
	})
	result, err := authorizeFixture(t, service, "")
	if err != nil || result.AuthorizationID == "legacy" {
		t.Fatalf("legacy identity was guessed: %+v, %v", result, err)
	}
	state, _ := store.LoadChannel(context.Background(), "marketing")
	rows := configuration.Object(state["authorizations"])
	if len(rows) != 2 || !reflect.DeepEqual(rows["legacy"], legacy) {
		t.Fatal("new authorization modified an unidentified legacy grant")
	}
}

func TestAuthorizationServiceSyncFailurePreservesPendingState(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			service, store, _ := authorizationServiceFixture()
			if _, err := authorizeFixture(t, service, ""); err != nil {
				t.Fatal(err)
			}
			_ = store.UpdateChannel(context.Background(), "marketing", func(state map[string]any) error {
				configuration.Object(configuration.Object(state["authorizations"])["grant"])["pending_account_sync"] = pending
				return nil
			})
			before, _ := store.LoadChannel(context.Background(), "marketing")
			service.Syncer = AdvertiserSnapshotSync{Authorizations: store, Tokens: staticTokenProvider{lease: TokenLease{Channel: "marketing", AuthorizationID: "grant", TokenRevision: 1, AccessToken: "fixture-access"}}, Discovery: staticDiscovery{err: errors.New("page failed again")}}
			input := RecoveryRequest{Channel: "marketing", AuthorizationID: "grant", Operation: "sync_accounts"}
			coordinator := NewRecoveryCoordinator(context.Background(), input, RecoverySession{Channel: input.Channel, Operation: input.Operation, AuthorizationID: input.AuthorizationID, Status: "awaiting_user"}, "", time.Minute, service)
			t.Cleanup(func() { coordinator.RequestCancel(); awaitRecoverySignal(t, coordinator.Done()) })
			if err := coordinator.StartSync(); err != nil {
				t.Fatal(err)
			}
			awaitRecoverySignal(t, coordinator.Done())
			view := coordinator.View()
			if view.Status != "failed" || view.SyncRequired != pending || view.AuthorizationID != "grant" {
				t.Fatalf("sync failure lost durable recovery state: %+v", view)
			}
			after, _ := store.LoadChannel(context.Background(), "marketing")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed sync changed the selected credential or account scope")
			}
		})
	}
}
