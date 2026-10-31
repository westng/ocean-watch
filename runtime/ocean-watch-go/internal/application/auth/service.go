package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// Service owns the authorization business flow for every transport. Browser
// callbacks and session lifetime belong to adapters and RecoveryCoordinator.
type Service struct {
	Credentials CredentialStore
	OAuth       OAuthAdapter
	Users       AuthorizedUserProvider
	Authorizer  Authorizer
	Syncer      AdvertiserSnapshotSync
}

func (service Service) Prepare(ctx context.Context, input RecoveryRequest) (RecoveryRequest, domain.OAuthApp, error) {
	if _, err := domain.ParseChannel(input.Channel); err != nil {
		return input, domain.OAuthApp{}, err
	}
	if input.AdvertiserID != "" {
		if err := domain.ValidateDecimalID(input.AdvertiserID, "advertiser_id"); err != nil {
			return input, domain.OAuthApp{}, err
		}
	}
	input.Operation = strings.TrimSpace(input.Operation)
	if input.Operation == "" {
		input.Operation = "authorize"
		if input.AuthorizationID != "" {
			input.Operation = "sync_accounts"
		}
	}
	if input.Operation != "authorize" && input.Operation != "reauthorize" && input.Operation != "sync_accounts" ||
		input.Operation == "authorize" && input.AuthorizationID != "" ||
		input.Operation != "authorize" && input.AuthorizationID == "" {
		return input, domain.OAuthApp{}, domain.NewError("invalid_arguments", "invalid authorization operation", 1, nil)
	}
	if input.Operation == "sync_accounts" {
		return input, domain.OAuthApp{}, nil
	}
	app, err := service.readApp(ctx, input.Channel)
	if err != nil {
		return input, app, atAuthorizationStep(failureAppCredentials, err)
	}
	if input.AuthorizationID != "" {
		if err := service.validateGrant(ctx, input.Channel, input.AuthorizationID, app.AppID, ""); err != nil {
			return input, app, atAuthorizationStep(failureAuthorizationStorage, err)
		}
	}
	return input, app, nil
}

func (service Service) readApp(ctx context.Context, channel string) (domain.OAuthApp, error) {
	if service.Credentials == nil || service.OAuth == nil {
		return domain.OAuthApp{}, errors.New("authorization dependencies are incomplete")
	}
	account, err := domain.AppCredentialAccount(channel)
	if err != nil {
		return domain.OAuthApp{}, err
	}
	value, err := service.Credentials.Read(ctx, account)
	if err != nil {
		return domain.OAuthApp{}, err
	}
	app := domain.OAuthApp{AppID: credentialString(value, "app_id"), Secret: credentialString(value, "secret")}
	if app.AppID == "" || app.Secret == "" {
		return domain.OAuthApp{}, domain.NewError("oauth_app_missing", "OAuth app is not configured for this channel", 1, map[string]any{"channel": channel})
	}
	return app, nil
}

// Authorize follows the reference flow: exchange, identify, save, then sync.
// Once saved, a failed or cancelled sync returns the durable grant so it can be
// recovered without exchanging another single-use authorization code.
func (service Service) Authorize(ctx context.Context, input RecoveryRequest, code string, phase func(string)) (AuthorizationResult, error) {
	if service.Users == nil || service.Authorizer.Credentials == nil || service.Authorizer.Authorizations == nil || service.Authorizer.Locks == nil {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, errors.New("authorization service dependencies are incomplete"))
	}
	// Enrollment uses a separate key to serialize provider identity selection.
	// Every explicit reauthorization also holds the same per-grant lock used by
	// TokenManager, from before code exchange through snapshot activation.
	releaseEnrollment, err := service.Authorizer.Locks.Acquire(ctx, input.Channel, ".oauth-enrollment")
	if err != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	defer func() { _ = releaseEnrollment() }()
	if input.AuthorizationID != "" {
		release, err := service.Authorizer.Locks.Acquire(ctx, input.Channel, input.AuthorizationID)
		if err != nil {
			return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, err)
		}
		defer func() { _ = release() }()
	}
	prepared, app, err := service.Prepare(ctx, input)
	if err != nil {
		var step *authorizationStepError
		if errors.As(err, &step) {
			return AuthorizationResult{}, err
		}
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, err)
	}
	if prepared.Operation == "sync_accounts" {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, domain.NewError("invalid_arguments", "account synchronization does not exchange authorization codes", 1, nil))
	}
	if phase != nil {
		phase("authorizing")
	}
	token, err := service.OAuth.ExchangeCode(ctx, input.Channel, app, code)
	if err != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureTokenExchange, err)
	}
	if err := ctx.Err(); err != nil {
		return AuthorizationResult{}, err
	}
	identityCtx, err := WithTokenLease(ctx, TokenLease{Channel: input.Channel, AuthorizationID: "oauth-bootstrap", TokenRevision: 1, AccessToken: token.AccessToken})
	if err != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureTokenExchange, err)
	}
	user, err := service.Users.GetAuthorizedUser(identityCtx, input.Channel, token.AccessToken)
	if err != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizedUser, err)
	}
	if user.ID == "0" || domain.ValidateDecimalID(user.ID, "authorized_user_id") != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizedUser, domain.NewError("authorized_user_invalid", "official authorized user identity is invalid", 1, nil))
	}
	if user.AppID != "" && user.AppID != app.AppID {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizedUser, identityMismatch())
	}
	if err := ctx.Err(); err != nil {
		return AuthorizationResult{}, err
	}
	authorizationID := input.AuthorizationID
	if authorizationID == "" {
		authorizationID, err = service.grantForUser(ctx, input.Channel, app.AppID, user.ID)
		if err != nil {
			return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, err)
		}
		release, err := service.Authorizer.Locks.Acquire(ctx, input.Channel, authorizationID)
		if err != nil {
			return AuthorizationResult{}, atAuthorizationStep(failureAuthorizationStorage, err)
		}
		defer func() { _ = release() }()
	} else if err := service.validateGrant(ctx, input.Channel, authorizationID, app.AppID, user.ID); err != nil {
		return AuthorizationResult{}, atAuthorizationStep(failureAuthorizedUser, err)
	}
	result, lease, err := service.Authorizer.save(ctx, AuthorizationRequest{Channel: input.Channel, AuthorizationID: authorizationID, AppID: app.AppID, UserID: user.ID, Token: token})
	if err != nil {
		return result, err
	}
	if phase != nil {
		phase("syncing")
	}
	syncer := service.Syncer
	// Credential metadata and account activation must use the same state port.
	syncer.Authorizations = service.Authorizer.Authorizations
	if syncer.Now == nil {
		syncer.Now = service.Authorizer.Now
	}
	synced, err := syncer.syncLease(ctx, lease, input.RebindExisting)
	if err != nil {
		return result, atAuthorizationStep(failureAccountSync, err)
	}
	result.AuthorizedAccounts, result.AdvertiserIDs = synced.AuthorizedAccounts, synced.AdvertiserIDs
	result.DiscoveryIssues, result.SyncedAt, result.PendingAccountSync = synced.DiscoveryIssues, synced.SyncedAt, false
	return result, nil
}

func (service Service) Sync(ctx context.Context, input RecoveryRequest) (AuthorizationResult, error) {
	result := AuthorizationResult{Channel: input.Channel, AuthorizationID: input.AuthorizationID}
	if service.Syncer.Authorizations == nil {
		return result, atAuthorizationStep(failureAccountSync, errors.New("authorization state is unavailable"))
	}
	state, err := service.Syncer.Authorizations.LoadChannel(ctx, input.Channel)
	if err != nil {
		return result, atAuthorizationStep(failureAccountSync, err)
	}
	binding, domainErr := domain.ResolveAuthorization(input.Channel, state, "", "", input.AuthorizationID, true)
	if domainErr != nil {
		return result, atAuthorizationStep(failureAccountSync, domainErr)
	}
	result.AuthorizationID, result.AppID, result.UserID = binding.AuthorizationID, binding.AppID, binding.AuthorizedUserID
	// Preserve the pending state even if discovery fails again or is cancelled.
	// A failed refresh of an already active snapshot does not make it pending.
	result.PendingAccountSync = binding.PendingAccountSync
	synced, err := service.Syncer.Sync(ctx, AdvertiserSyncQuery{Channel: input.Channel, AuthorizationID: input.AuthorizationID, RebindExisting: input.RebindExisting})
	if err != nil {
		return result, atAuthorizationStep(failureAccountSync, err)
	}
	result.AuthorizedAccounts, result.AdvertiserIDs = synced.AuthorizedAccounts, synced.AdvertiserIDs
	result.DiscoveryIssues, result.SyncedAt, result.PendingAccountSync = synced.DiscoveryIssues, synced.SyncedAt, false
	return result, nil
}

func (service Service) validateGrant(ctx context.Context, channel, id, appID, userID string) error {
	if service.Authorizer.Authorizations == nil {
		return errors.New("authorization state is unavailable")
	}
	state, err := service.Authorizer.Authorizations.LoadChannel(ctx, channel)
	if err != nil {
		return err
	}
	metadata, exists := configuration.Object(state["authorizations"])[id]
	if !exists {
		return domain.NewError("authorization_not_found", "authorization grant was not found", 1, map[string]any{"authorization_id": id})
	}
	row := configuration.Object(metadata)
	if storedApp := credentialString(row, "app_id"); storedApp != "" && storedApp != appID {
		return identityMismatch()
	}
	if storedUser := credentialString(row, "authorized_user_id"); userID != "" && storedUser != "" && storedUser != userID {
		return identityMismatch()
	}
	return nil
}

func (service Service) grantForUser(ctx context.Context, channel, appID, userID string) (string, error) {
	state, err := service.Authorizer.Authorizations.LoadChannel(ctx, channel)
	if err != nil {
		return "", err
	}
	selected := ""
	for id, metadata := range configuration.Object(state["authorizations"]) {
		row := configuration.Object(metadata)
		if credentialString(row, "app_id") == appID && credentialString(row, "authorized_user_id") == userID {
			if selected != "" {
				return "", domain.NewError("authorization_ambiguous", "multiple grants belong to the authorized user; select an authorization ID", 1, nil)
			}
			selected = id
		}
	}
	if selected != "" {
		return selected, nil
	}
	id, err := service.Authorizer.authorizationID()
	if err != nil {
		return "", err
	}
	if _, exists := configuration.Object(state["authorizations"])[id]; exists {
		return "", domain.NewError("authorization_ambiguous", "new grant ID conflicts with an existing authorization", 1, nil)
	}
	return id, nil
}

func identityMismatch() error {
	return domain.NewError("authorization_identity_mismatch", "authorized user or application does not match the selected grant", 1, nil)
}
