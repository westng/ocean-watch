package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
)

func WithTokenLease(ctx context.Context, lease TokenLease) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("token lease context is required")
	}
	if strings.TrimSpace(lease.Channel) == "" || strings.TrimSpace(lease.AuthorizationID) == "" {
		return nil, errors.New("token lease authorization scope is incomplete")
	}
	return requestcontrol.WithAuthorization(ctx, lease.Channel, lease.AuthorizationID)
}

func WithAdvertiserTokenLease(ctx context.Context, lease TokenLease, advertiserID string) (context.Context, error) {
	scoped, err := WithTokenLease(ctx, lease)
	if err != nil {
		return nil, err
	}
	return requestcontrol.WithAdvertiser(scoped, advertiserID)
}
