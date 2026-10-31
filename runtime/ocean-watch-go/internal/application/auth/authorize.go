package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// Authorizer persists grants; it does not perform OAuth or discover accounts.
// Service holds the enrollment/grant locks before calling save.
type Authorizer struct {
	Credentials    CredentialStore
	Authorizations AuthorizationStore
	Locks          RefreshLocker
	Now            func() time.Time
	NewID          func() (string, error)
}

type AuthorizationRequest struct {
	Channel         string
	AuthorizationID string
	AppID           string
	UserID          string
	Token           domain.OAuthToken
}

type AuthorizationResult struct {
	Channel            string                         `json:"channel"`
	AuthorizationID    string                         `json:"authorization_id"`
	AppID              string                         `json:"app_id,omitempty"`
	UserID             string                         `json:"authorized_user_id,omitempty"`
	TokenRevision      int                            `json:"token_revision"`
	AccessTokenExpiry  string                         `json:"access_token_expires_at,omitempty"`
	RefreshTokenExpiry string                         `json:"refresh_token_expires_at,omitempty"`
	AuthorizedAccounts int                            `json:"authorized_account_count"`
	AdvertiserIDs      []string                       `json:"authorized_advertiser_ids"`
	DiscoveryIssues    []domain.AccountDiscoveryIssue `json:"account_discovery_issues,omitempty"`
	SyncedAt           string                         `json:"synced_at"`
	PendingAccountSync bool                           `json:"pending_account_sync"`
}

func (authorizer Authorizer) save(ctx context.Context, request AuthorizationRequest) (AuthorizationResult, TokenLease, error) {
	if authorizer.Credentials == nil || authorizer.Authorizations == nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, errors.New("authorization stores are incomplete"))
	}
	if _, err := domain.ParseChannel(request.Channel); err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	if request.AuthorizationID == "" || request.AppID == "0" || request.UserID == "0" || domain.ValidateDecimalID(request.AppID, "app_id") != nil || domain.ValidateDecimalID(request.UserID, "authorized_user_id") != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizedUser, identityMismatch())
	}
	state, err := authorizer.Authorizations.LoadChannel(ctx, request.Channel)
	if err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	raw, existed := configuration.Object(state["authorizations"])[request.AuthorizationID]
	currentRevision := 0
	if existed {
		current := configuration.Object(raw)
		currentRevision, err = positiveInteger(current["token_revision"], 1)
		if err != nil {
			return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, err)
		}
		if app := credentialString(current, "app_id"); app != "" && app != request.AppID {
			return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizedUser, identityMismatch())
		}
		if user := credentialString(current, "authorized_user_id"); user != "" && user != request.UserID {
			return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizedUser, identityMismatch())
		}
	}
	now := time.Now().UTC()
	if authorizer.Now != nil {
		now = authorizer.Now().UTC()
	}
	credential, err := applyToken(map[string]any{}, request.Token, now)
	if err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureTokenExchange, err)
	}
	if credentialString(credential, "refresh_token") == "" {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureTokenExchange, errors.New("OAuth authorization response did not include refresh_token"))
	}
	credential["app_id"], credential["authorized_user_id"] = request.AppID, request.UserID
	revision := currentRevision + 1
	credentialAccount, err := domain.AuthorizationCredentialAccount(request.Channel, request.AuthorizationID, revision)
	if err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	if _, err := authorizer.Credentials.Write(ctx, credentialAccount, credential); err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureCredentialStorage, err)
	}
	err = authorizer.Authorizations.UpdateChannel(ctx, request.Channel, func(candidate map[string]any) error {
		initializeAuthorizationState(candidate)
		rows := configuration.Object(candidate["authorizations"])
		row, exists := rows[request.AuthorizationID]
		if exists != existed {
			return errors.New("authorization grant changed during credential persistence")
		}
		current := configuration.Object(row)
		if exists {
			observed, err := positiveInteger(current["token_revision"], 1)
			if err != nil || observed != currentRevision {
				return errors.New("authorization credential changed during persistence")
			}
		} else {
			current = map[string]any{"authorized_accounts": []any{}, "advertiser_ids": []any{}, "account_discovery_issues": []any{}}
			rows[request.AuthorizationID] = current
		}
		current["app_id"], current["authorized_user_id"] = request.AppID, request.UserID
		current["token_revision"], current["pending_account_sync"] = revision, true
		current["last_authorized_at"] = now.Format(time.RFC3339Nano)
		candidate["generation"] = nextGeneration(candidate["generation"])
		return nil
	})
	if err != nil {
		return AuthorizationResult{}, TokenLease{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	result := AuthorizationResult{Channel: request.Channel, AuthorizationID: request.AuthorizationID, AppID: request.AppID, UserID: request.UserID, TokenRevision: revision,
		AccessTokenExpiry: credentialString(credential, "access_token_expires_at"), RefreshTokenExpiry: credentialString(credential, "refresh_token_expires_at"), PendingAccountSync: true}
	lease := TokenLease{Channel: request.Channel, AuthorizationID: request.AuthorizationID, TokenRevision: revision, AccessToken: credentialString(credential, "access_token"), AccessTokenExpiry: result.AccessTokenExpiry}
	return result, lease, nil
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
		return "", err
	}
	return "auth-" + hex.EncodeToString(payload), nil
}

func initializeAuthorizationState(state map[string]any) {
	for _, key := range []string{"authorizations", "account_index", "advertiser_index"} {
		if _, ok := state[key].(map[string]any); !ok {
			state[key] = map[string]any{}
		}
	}
	state["schema_version"] = 2
}
