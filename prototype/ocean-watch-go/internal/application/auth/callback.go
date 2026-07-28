package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type OAuthCallbackSession struct {
	Channel       string
	ExpectedState string
	ExpiresAt     time.Time
	App           domain.OAuthApp
	OAuth         OAuthAdapter
	Now           func() time.Time

	mu       sync.Mutex
	consumed bool
}

func (session *OAuthCallbackSession) Exchange(
	ctx context.Context,
	state string,
	code string,
) (domain.OAuthToken, error) {
	if session == nil || session.OAuth == nil {
		return domain.OAuthToken{}, errors.New("OAuth callback session is incomplete")
	}
	if ctx == nil {
		return domain.OAuthToken{}, errors.New("OAuth callback context is required")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.consumed {
		return domain.OAuthToken{}, domain.NewError(
			"oauth_callback_replayed", "OAuth callback was already consumed", 1, nil,
		)
	}
	now := time.Now().UTC()
	if session.Now != nil {
		now = session.Now().UTC()
	}
	if session.ExpiresAt.IsZero() || !now.Before(session.ExpiresAt.UTC()) {
		return domain.OAuthToken{}, domain.NewError(
			"oauth_callback_expired", "OAuth callback session expired", 1, nil,
		)
	}
	if stateErr := domain.ValidateOAuthCallbackState(state, session.ExpectedState, session.Channel); stateErr != nil {
		return domain.OAuthToken{}, stateErr
	}
	if code == "" {
		return domain.OAuthToken{}, domain.NewError(
			"oauth_code_missing", "OAuth callback code is required", 1, nil,
		)
	}
	session.consumed = true
	return session.OAuth.ExchangeCode(ctx, session.Channel, session.App, code)
}
