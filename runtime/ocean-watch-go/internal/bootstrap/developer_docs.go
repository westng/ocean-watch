package bootstrap

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// DeveloperAppReferences reads only the selected grant's non-secret App ID.
// It never opens the business credential record or prepares a TokenLease.
type DeveloperAppReferences struct {
	Authorizations auth.AuthorizationStore
	Policy         policy.Instance
}

func (references DeveloperAppReferences) AppID(ctx context.Context, reference string) (string, error) {
	if ctx == nil || ctx.Err() != nil || references.Authorizations == nil || references.Policy.CheckAuthorization("qianchuan", reference) != nil {
		return "", docs.ErrMetadata
	}
	state, err := references.Authorizations.LoadChannel(ctx, "qianchuan")
	if err != nil {
		return "", docs.ErrMetadata
	}
	grant := configuration.Object(configuration.Object(state["authorizations"])[reference])
	app, _ := grant["app_id"].(string)
	if app == "" {
		return "", docs.ErrMetadata
	}
	return app, nil
}
