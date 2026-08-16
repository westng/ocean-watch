package bootstrap

import (
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type MarketingOptions struct {
	StateRoot       string
	CredentialStore authapplication.CredentialStore
	Authorizations  authapplication.AuthorizationStore
	RefreshLocker   authapplication.RefreshLocker
	OAuth           authapplication.OAuthAdapter
	ClientFactory   *oceanengine.ClientFactory
	MaterialReader  portmarketing.MaterialReader
	ReportReader    portreports.MarketingReader
	Tokens          authapplication.TokenProvider
	Retry           platformretry.Policy
	Now             func() time.Time
}

type MarketingRuntime struct {
	Auth      authapplication.QueryService
	Materials applicationmaterials.Service
	Reports   applicationreports.MarketingService
}

func NewMarketingRuntime(options MarketingOptions) (MarketingRuntime, error) {
	factory := options.ClientFactory
	var err error
	if factory == nil {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{})
		if err != nil {
			return MarketingRuntime{}, err
		}
	}
	authorizations := options.Authorizations
	if authorizations == nil {
		authorizations = filesystem.AuthorizationStore{Root: options.StateRoot}
	}
	refreshLocker := options.RefreshLocker
	if refreshLocker == nil {
		refreshLocker = filesystem.RefreshLocker{StateRoot: options.StateRoot}
	}
	oauth := options.OAuth
	if oauth == nil {
		oauth = oceanengine.OAuthAdapter{Factory: factory}
	}
	tokens := options.Tokens
	if tokens == nil {
		tokens = &authapplication.TokenManager{
			Credentials: options.CredentialStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: options.Now,
		}
	}
	materialReader := options.MaterialReader
	if materialReader == nil {
		materialReader = oceanengine.MarketingMaterialsAdapter{Factory: factory, Retry: options.Retry}
	}
	reportReader := options.ReportReader
	if reportReader == nil {
		reportReader = oceanengine.MarketingReportsAdapter{Factory: factory, Retry: options.Retry}
	}
	return MarketingRuntime{
		Auth: authapplication.QueryService{
			Credentials: options.CredentialStore, Authorizations: authorizations,
		},
		Materials: applicationmaterials.Service{
			Tokens: tokens, Reader: materialReader, Now: options.Now,
		},
		Reports: applicationreports.MarketingService{
			Tokens: tokens, Reader: reportReader, Now: options.Now,
		},
	}, nil
}
