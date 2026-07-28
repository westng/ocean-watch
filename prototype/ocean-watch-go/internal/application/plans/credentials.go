package plans

import (
	"context"
	"errors"
	"strings"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

type TokenCredentialProvider struct {
	Tokens authapplication.TokenProvider
}

func (provider TokenCredentialProvider) AccessToken(
	ctx context.Context,
	channel domainplans.Channel,
	advertiserID string,
	authAccountID string,
) (CredentialLease, error) {
	if provider.Tokens == nil {
		return CredentialLease{}, errors.New("write token provider is required")
	}
	lease, err := provider.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: string(channel), AdvertiserID: strings.TrimSpace(advertiserID),
		AuthAccountID: strings.TrimSpace(authAccountID),
	})
	if err != nil {
		return CredentialLease{}, err
	}
	return CredentialLease{
		AuthorizationID: strings.TrimSpace(lease.AuthorizationID),
		AccessToken:     strings.TrimSpace(lease.AccessToken),
	}, nil
}
