package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

func TestAuthorizerActivatesOnlyCompleteSnapshot(t *testing.T) {
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"marketing": {}}}
	credentials := &memoryCredentials{entries: map[string]map[string]any{}}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	authorizer := Authorizer{
		Credentials: credentials, Authorizations: store, Now: func() time.Time { return now },
		NewID: func() (string, error) { return "new-auth", nil },
		Discovery: staticDiscovery{snapshot: domain.AdvertiserSnapshot{
			Accounts:      []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}},
			AdvertiserIDs: []string{"1001"},
		}},
	}
	result, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		Channel: "marketing", Token: domain.OAuthToken{
			AccessToken: "access", RefreshToken: "refresh", AccessTokenTTL: time.Hour,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationID != "new-auth" || result.AuthorizedAccounts != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	state, _ := store.LoadChannel(context.Background(), "marketing")
	metadata := configuration.Object(configuration.Object(state["authorizations"])["new-auth"])
	if metadata["pending_account_sync"] != false {
		t.Fatalf("snapshot was not activated: %#v", metadata)
	}
}

func TestAuthorizerKeepsNewAuthorizationPendingAfterDiscoveryFailure(t *testing.T) {
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"qianchuan": {}}}
	authorizer := Authorizer{
		Credentials: &memoryCredentials{entries: map[string]map[string]any{}}, Authorizations: store,
		NewID:     func() (string, error) { return "pending-auth", nil },
		Discovery: staticDiscovery{err: errors.New("middle page failed")},
	}
	_, err := authorizer.Authorize(context.Background(), AuthorizationRequest{
		Channel: "qianchuan", Token: domain.OAuthToken{AccessToken: "access", RefreshToken: "refresh"},
	})
	if err == nil {
		t.Fatal("discovery failure was accepted")
	}
	state, _ := store.LoadChannel(context.Background(), "qianchuan")
	metadata := configuration.Object(configuration.Object(state["authorizations"])["pending-auth"])
	if metadata["pending_account_sync"] != true || len(anyList(metadata["authorized_accounts"])) != 0 {
		t.Fatalf("failed discovery activated state: %#v", metadata)
	}
}
