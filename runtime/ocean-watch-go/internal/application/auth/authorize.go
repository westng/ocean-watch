package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type Authorizer struct {
	Credentials    CredentialStore
	Authorizations AuthorizationStore
	Discovery      AdvertiserDiscovery
	Now            func() time.Time
	NewID          func() (string, error)
}

type AuthorizationRequest struct {
	Channel        string
	Token          domain.OAuthToken
	RebindExisting bool
}

type AuthorizationResult struct {
	Channel            string                         `json:"channel"`
	AuthorizationID    string                         `json:"authorization_id"`
	TokenRevision      int                            `json:"token_revision"`
	AccessTokenExpiry  string                         `json:"access_token_expires_at,omitempty"`
	RefreshTokenExpiry string                         `json:"refresh_token_expires_at,omitempty"`
	AuthorizedAccounts int                            `json:"authorized_account_count"`
	AdvertiserIDs      []string                       `json:"authorized_advertiser_ids"`
	DiscoveryIssues    []domain.AccountDiscoveryIssue `json:"account_discovery_issues,omitempty"`
	SyncedAt           string                         `json:"synced_at"`
}

func (authorizer Authorizer) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (AuthorizationResult, error) {
	if authorizer.Credentials == nil || authorizer.Authorizations == nil || authorizer.Discovery == nil {
		return AuthorizationResult{}, errors.New("authorization dependencies are incomplete")
	}
	if request.Channel != "marketing" && request.Channel != "qianchuan" {
		return AuthorizationResult{}, fmt.Errorf("unsupported authorization channel %q", request.Channel)
	}
	now := time.Now().UTC()
	if authorizer.Now != nil {
		now = authorizer.Now().UTC()
	}
	credential, err := applyToken(map[string]any{}, request.Token, now)
	if err != nil {
		return AuthorizationResult{}, err
	}
	if credentialString(credential, "refresh_token") == "" {
		return AuthorizationResult{}, errors.New("OAuth authorization response did not include refresh_token")
	}
	authorizationID, err := authorizer.authorizationID()
	if err != nil {
		return AuthorizationResult{}, err
	}
	credentialAccount, err := domain.AuthorizationCredentialAccount(request.Channel, authorizationID, 1)
	if err != nil {
		return AuthorizationResult{}, err
	}
	if _, err := authorizer.Credentials.Write(ctx, credentialAccount, credential); err != nil {
		return AuthorizationResult{}, fmt.Errorf("persist OAuth credential: %w", err)
	}
	if err := authorizer.Authorizations.UpdateChannel(ctx, request.Channel, func(state map[string]any) error {
		initializeAuthorizationState(state)
		authorizations := configuration.Object(state["authorizations"])
		if _, exists := authorizations[authorizationID]; exists {
			return errors.New("generated authorization ID already exists")
		}
		authorizations[authorizationID] = map[string]any{
			"token_revision": 1, "pending_account_sync": true,
			"authorized_accounts": []any{}, "advertiser_ids": []any{},
			"account_discovery_issues": []any{},
		}
		state["generation"] = nextGeneration(state["generation"])
		return nil
	}); err != nil {
		return AuthorizationResult{}, fmt.Errorf("persist authorization metadata: %w", err)
	}
	lease := TokenLease{
		Channel: request.Channel, AuthorizationID: authorizationID, TokenRevision: 1,
		AccessToken:       credentialString(credential, "access_token"),
		AccessTokenExpiry: credentialString(credential, "access_token_expires_at"),
	}
	syncer := AdvertiserSnapshotSync{
		Tokens: staticLeaseProvider{lease: lease}, Authorizations: authorizer.Authorizations,
		Discovery: authorizer.Discovery, Now: authorizer.Now,
	}
	synced, err := syncer.Sync(ctx, AdvertiserSyncQuery{
		Channel: request.Channel, AuthorizationID: authorizationID,
		RebindExisting: request.RebindExisting,
	})
	if err != nil {
		return AuthorizationResult{}, fmt.Errorf("authorization saved pending complete advertiser discovery: %w", err)
	}
	return AuthorizationResult{
		Channel: request.Channel, AuthorizationID: authorizationID, TokenRevision: 1,
		AccessTokenExpiry:  credentialString(credential, "access_token_expires_at"),
		RefreshTokenExpiry: credentialString(credential, "refresh_token_expires_at"),
		AuthorizedAccounts: synced.AuthorizedAccounts,
		AdvertiserIDs:      append([]string(nil), synced.AdvertiserIDs...),
		DiscoveryIssues:    append([]domain.AccountDiscoveryIssue(nil), synced.DiscoveryIssues...),
		SyncedAt:           synced.SyncedAt,
	}, nil
}

func (authorizer Authorizer) authorizationID() (string, error) {
	if authorizer.NewID != nil {
		value, err := authorizer.NewID()
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, "/\\\x00\r\n") {
			return "", errors.New("generated authorization ID is invalid")
		}
		return value, nil
	}
	payload := make([]byte, 12)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("generate authorization ID: %w", err)
	}
	return hex.EncodeToString(payload), nil
}

func initializeAuthorizationState(state map[string]any) {
	if state["generation"] == nil {
		state["generation"] = 0
	}
	if _, ok := state["authorizations"].(map[string]any); !ok {
		state["authorizations"] = map[string]any{}
	}
	if _, ok := state["account_index"].(map[string]any); !ok {
		state["account_index"] = map[string]any{}
	}
	if _, ok := state["advertiser_index"].(map[string]any); !ok {
		state["advertiser_index"] = map[string]any{}
	}
}

type staticLeaseProvider struct {
	lease TokenLease
}

func (provider staticLeaseProvider) Ensure(context.Context, TokenQuery) (TokenLease, error) {
	return provider.lease, nil
}
