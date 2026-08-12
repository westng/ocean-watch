package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

const DefaultRefreshMargin = 30 * time.Minute

type TokenManager struct {
	Credentials    CredentialStore
	Authorizations AuthorizationStore
	Locks          RefreshLocker
	OAuth          OAuthAdapter
	Now            func() time.Time
}

type TokenQuery struct {
	Channel         string
	AdvertiserID    string
	AuthAccountID   string
	AuthorizationID string
	AllowPending    bool
	ForceRefresh    bool
	Margin          time.Duration
}

type TokenLease struct {
	Channel           string
	AuthorizationID   string
	TokenRevision     int
	AccessToken       string
	AccessTokenExpiry string
	Refreshed         bool
}

func (manager TokenManager) Ensure(ctx context.Context, query TokenQuery) (TokenLease, error) {
	if manager.Credentials == nil || manager.Authorizations == nil || manager.Locks == nil || manager.OAuth == nil {
		return TokenLease{}, errors.New("token manager dependencies are incomplete")
	}
	state, err := manager.Authorizations.LoadChannel(ctx, query.Channel)
	if err != nil {
		return TokenLease{}, err
	}
	initial, domainErr := domain.ResolveAuthorization(
		query.Channel, state, query.AdvertiserID, query.AuthAccountID,
		query.AuthorizationID, query.AllowPending,
	)
	if domainErr != nil {
		return TokenLease{}, domainErr
	}
	release, err := manager.Locks.Acquire(ctx, query.Channel, initial.AuthorizationID)
	if err != nil {
		return TokenLease{}, err
	}
	defer func() { _ = release() }()

	state, err = manager.Authorizations.LoadChannel(ctx, query.Channel)
	if err != nil {
		return TokenLease{}, err
	}
	binding, domainErr := domain.ResolveAuthorization(
		query.Channel, state, query.AdvertiserID, query.AuthAccountID,
		initial.AuthorizationID, query.AllowPending,
	)
	if domainErr != nil {
		return TokenLease{}, domainErr
	}
	credential, err := manager.Credentials.Read(ctx, binding.CredentialAccount)
	if err != nil {
		return TokenLease{}, err
	}
	now := time.Now().UTC()
	if manager.Now != nil {
		now = manager.Now().UTC()
	}
	margin := query.Margin
	if margin == 0 {
		margin = DefaultRefreshMargin
	}
	forceRefresh := query.ForceRefresh && binding.TokenRevision == initial.TokenRevision
	if !forceRefresh && accessTokenReady(credential, now, margin) {
		return tokenLease(binding, credential, false), nil
	}
	refreshToken := credentialString(credential, "refresh_token")
	if refreshToken == "" || tokenExpired(credential["refresh_token_expires_at"], now) {
		return TokenLease{}, domain.NewError(
			"reauthorization_required",
			fmt.Sprintf("%s refresh token is missing or expired; authorize again", query.Channel),
			1,
			nil,
		)
	}
	appAccount, err := domain.AppCredentialAccount(query.Channel)
	if err != nil {
		return TokenLease{}, err
	}
	appCredential, err := manager.Credentials.Read(ctx, appAccount)
	if err != nil {
		return TokenLease{}, err
	}
	app := domain.OAuthApp{
		AppID: credentialString(appCredential, "app_id"), Secret: credentialString(appCredential, "secret"),
	}
	if app.AppID == "" || app.Secret == "" {
		return TokenLease{}, domain.NewError("oauth_app_missing", "OAuth app_id and secret are required", 2, nil)
	}
	refreshContext, err := WithTokenLease(ctx, tokenLease(binding, credential, false))
	if err != nil {
		return TokenLease{}, err
	}
	refreshed, err := manager.OAuth.RefreshToken(refreshContext, query.Channel, app, refreshToken)
	if err != nil {
		return TokenLease{}, err
	}
	updated, err := applyToken(credential, refreshed, now)
	if err != nil {
		return TokenLease{}, err
	}
	nextRevision := binding.TokenRevision + 1
	nextAccount, err := domain.AuthorizationCredentialAccount(query.Channel, binding.AuthorizationID, nextRevision)
	if err != nil {
		return TokenLease{}, err
	}
	if _, err := manager.Credentials.Write(ctx, nextAccount, updated); err != nil {
		return TokenLease{}, fmt.Errorf("persist refreshed credential: %w", err)
	}
	err = manager.Authorizations.UpdateChannel(ctx, query.Channel, func(candidate map[string]any) error {
		authorizations := configuration.Object(candidate["authorizations"])
		metadata := configuration.Object(authorizations[binding.AuthorizationID])
		currentRevision, parseErr := positiveInteger(metadata["token_revision"], 1)
		if parseErr != nil {
			return parseErr
		}
		if currentRevision != binding.TokenRevision {
			return errors.New("authorization token revision changed during refresh")
		}
		metadata["token_revision"] = nextRevision
		candidate["generation"] = nextGeneration(candidate["generation"])
		return nil
	})
	if err != nil {
		return TokenLease{}, fmt.Errorf("activate refreshed credential: %w", err)
	}
	binding.TokenRevision = nextRevision
	binding.CredentialAccount = nextAccount
	return tokenLease(binding, updated, true), nil
}

func accessTokenReady(credential map[string]any, now time.Time, margin time.Duration) bool {
	if credentialString(credential, "access_token") == "" {
		return false
	}
	raw := credential["access_token_expires_at"]
	if raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "" {
		return true
	}
	expiresAt, err := parseCredentialTime(raw)
	return err == nil && expiresAt.Sub(now) > margin
}

func tokenExpired(raw any, now time.Time) bool {
	if raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "" {
		return false
	}
	expiresAt, err := parseCredentialTime(raw)
	return err != nil || !expiresAt.After(now)
}

func applyToken(current map[string]any, token domain.OAuthToken, now time.Time) (map[string]any, error) {
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, errors.New("OAuth refresh response did not include access_token")
	}
	updated := configuration.CloneMap(current)
	updated["access_token"] = token.AccessToken
	if strings.TrimSpace(token.RefreshToken) != "" {
		updated["refresh_token"] = token.RefreshToken
	}
	if token.AccessTokenTTL > 0 {
		updated["access_token_expires_at"] = now.Add(token.AccessTokenTTL).Format(time.RFC3339Nano)
	}
	if token.RefreshTokenTTL > 0 {
		updated["refresh_token_expires_at"] = now.Add(token.RefreshTokenTTL).Format(time.RFC3339Nano)
	}
	updated["last_token_update_at"] = now.Format(time.RFC3339Nano)
	return updated, nil
}

func tokenLease(binding domain.AuthorizationBinding, credential map[string]any, refreshed bool) TokenLease {
	return TokenLease{
		Channel: binding.Channel, AuthorizationID: binding.AuthorizationID,
		TokenRevision: binding.TokenRevision, AccessToken: credentialString(credential, "access_token"),
		AccessTokenExpiry: credentialString(credential, "access_token_expires_at"), Refreshed: refreshed,
	}
}

func credentialString(value map[string]any, key string) string {
	text := strings.TrimSpace(fmt.Sprint(value[key]))
	if text == "<nil>" || strings.HasPrefix(text, "REPLACE_WITH") {
		return ""
	}
	return text
}

func parseCredentialTime(value any) (time.Time, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid credential expiry timestamp")
	}
	return parsed.UTC(), nil
}

func positiveInteger(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || parsed < 1 {
		return 0, errors.New("token_revision must be positive")
	}
	return parsed, nil
}

func nextGeneration(value any) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || parsed < 0 {
		return 1
	}
	return parsed + 1
}
