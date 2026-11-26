package auth

import (
	"context"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

// ConfigureMissingApp receives credentials only from the local browser form.
// It cannot replace an existing app or change an existing grant's identity.
func (service Service) ConfigureMissingApp(ctx context.Context, input RecoveryRequest, app domain.OAuthApp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	app.AppID, app.Secret = strings.TrimSpace(app.AppID), strings.TrimSpace(app.Secret)
	if domain.ValidateDecimalID(app.AppID, "app_id") != nil || app.Secret == "" || len(app.Secret) > 512 || strings.ContainsAny(app.Secret, "\r\n\x00") {
		return domain.NewError("invalid_app_configuration", "App ID or Secret is invalid", 2, nil)
	}
	account, err := domain.AppCredentialAccount(input.Channel)
	if err != nil {
		return err
	}
	backend, ok := service.Credentials.(interface{ BackendName() string })
	if !ok || !secureAppBackend(backend.BackendName()) || service.Authorizer.Locks == nil {
		return domain.NewError("app_configuration_unavailable", "a system credential store is required", 1, nil)
	}
	release, err := service.Authorizer.Locks.Acquire(ctx, input.Channel, ".oauth-app-configuration")
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.AuthorizationID != "" {
		if err := service.validateGrant(ctx, input.Channel, input.AuthorizationID, app.AppID, ""); err != nil {
			return err
		}
	}
	existing, err := service.Credentials.Read(ctx, account)
	if err != nil {
		return err
	}
	if credentialString(existing, "app_id") != "" && credentialString(existing, "secret") != "" {
		return domain.NewError("app_configuration_changed", "the app was already configured; restart authorization", 1, nil)
	}
	if id := credentialString(existing, "app_id"); id != "" && id != app.AppID {
		return identityMismatch()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := service.Credentials.Write(ctx, account, map[string]any{"app_id": app.AppID, "secret": app.Secret})
	if err != nil || !secureAppBackend(name) {
		return domain.NewError("app_configuration_failed", "system credential storage failed", 1, nil)
	}
	written, err := service.Credentials.Read(ctx, account)
	if err != nil || credentialString(written, "app_id") != app.AppID || credentialString(written, "secret") != app.Secret {
		return domain.NewError("app_configuration_failed", "system credential verification failed", 1, nil)
	}
	return nil
}

func secureAppBackend(name string) bool {
	return name == "macos-keychain" || name == "linux-secret-service" || name == "windows-dpapi" || name == "windows-credential-manager"
}
