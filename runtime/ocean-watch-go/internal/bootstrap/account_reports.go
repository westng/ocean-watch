package bootstrap

import (
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	accounts "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/accounts"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
)

func NewManagedAccountReporter(store filesystem.ManagedConfigStore, root string, credentials auth.CredentialStore, instance policy.Instance) (accounts.Reporter, error) {
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{SharedQianchuanControl: filesystem.QianchuanRequestController{Root: root}})
	if err != nil {
		return accounts.Reporter{}, err
	}
	tokens := &auth.TokenManager{Credentials: credentials, Authorizations: filesystem.AuthorizationStore{Root: root}, Locks: filesystem.RefreshLocker{StateRoot: root}, OAuth: oceanengine.OAuthAdapter{Factory: factory}}
	return accounts.Reporter{Store: filesystem.AccountStore{Path: store.Path}, Tokens: policy.Tokens{Instance: instance, Provider: tokens, Required: []string{"reports"}, Authorizations: tokens.Authorizations}, Marketing: oceanengine.MarketingAccountReportAdapter{Factory: factory}, Qianchuan: oceanengine.QianchuanAccountReportAdapter{Factory: factory}}, nil
}
