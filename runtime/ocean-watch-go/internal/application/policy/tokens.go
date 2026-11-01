package policy

import (
	"context"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

// Tokens applies the same local subject boundary to SDK reads. OAuth scope is
// still resolved by the existing provider; a policy never grants OAuth access.
type Tokens struct {
	Instance       Instance
	Provider       auth.TokenProvider
	Required       []string
	Minimum        contracts.QueryExecutionMode
	Authorizations auth.AuthorizationStore
}

func (guard Tokens) Ensure(ctx context.Context, query auth.TokenQuery) (auth.TokenLease, error) {
	if ctx == nil {
		return auth.TokenLease{}, ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return auth.TokenLease{}, err
	}
	minimum := guard.Minimum
	if minimum == "" {
		minimum = contracts.QueryModeRead
	}
	if guard.Provider == nil || guard.Instance.Check(guard.Required, minimum) != nil {
		return auth.TokenLease{}, ErrDenied
	}
	if knownToolset(query.Channel) && guard.Instance.Check([]string{query.Channel}, contracts.QueryModeRead) != nil {
		return auth.TokenLease{}, ErrDenied
	}
	if query.AdvertiserID != "" && guard.Instance.CheckSubject(query.Channel, "advertiser", query.AdvertiserID) != nil {
		return auth.TokenLease{}, ErrDenied
	}
	if guard.Instance.AuthorizationRestricted(query.Channel) {
		if query.AuthorizationID == "" {
			if guard.Authorizations == nil {
				return auth.TokenLease{}, ErrDenied
			}
			state, err := guard.Authorizations.LoadChannel(ctx, query.Channel)
			if err != nil {
				return auth.TokenLease{}, ErrDenied
			}
			binding, bindingErr := domain.ResolveAuthorization(query.Channel, state, query.AdvertiserID, query.AuthAccountID, "", false)
			if bindingErr != nil {
				return auth.TokenLease{}, ErrDenied
			}
			query.AuthorizationID = binding.AuthorizationID
		}
		if guard.Instance.CheckAuthorization(query.Channel, query.AuthorizationID) != nil {
			return auth.TokenLease{}, ErrDenied
		}
	}
	lease, err := guard.Provider.Ensure(ctx, query)
	if err == nil && guard.Instance.AuthorizationRestricted(query.Channel) && guard.Instance.CheckAuthorization(query.Channel, lease.AuthorizationID) != nil {
		return auth.TokenLease{}, ErrDenied
	}
	return lease, err
}
