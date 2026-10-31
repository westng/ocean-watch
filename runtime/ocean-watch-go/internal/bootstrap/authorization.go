package bootstrap

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/localauth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

func NewAuthorizationRecovery(ctx context.Context, stateRoot string, credentials auth.CredentialStore) (*localauth.Manager, error) {
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		SharedQianchuanControl: filesystem.QianchuanRequestController{Root: stateRoot},
	})
	if err != nil {
		return nil, err
	}
	authorizations := filesystem.AuthorizationStore{Root: stateRoot}
	oauth := oceanengine.OAuthAdapter{Factory: factory}
	discovery := oceanengine.AdvertiserDiscoveryAdapter{Factory: factory}
	tokens := &auth.TokenManager{Credentials: credentials, Authorizations: authorizations,
		Locks: filesystem.RefreshLocker{StateRoot: stateRoot}, OAuth: oauth}
	return localauth.New(ctx, localauth.Options{Credentials: credentials, OAuth: oauth, Users: oauth,
		Authorizer: auth.Authorizer{Credentials: credentials, Authorizations: authorizations, Locks: tokens.Locks},
		Syncer:     auth.AdvertiserSnapshotSync{Tokens: tokens, Authorizations: authorizations, Discovery: discovery},
	}), nil
}
