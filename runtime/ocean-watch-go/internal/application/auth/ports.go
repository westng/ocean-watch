package auth

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type CredentialStore interface {
	Read(context.Context, string) (map[string]any, error)
	Write(context.Context, string, map[string]any) (string, error)
}

type AuthorizationStore interface {
	LoadChannel(context.Context, string) (map[string]any, error)
	UpdateChannel(context.Context, string, func(map[string]any) error) error
}

type RefreshLocker interface {
	Acquire(context.Context, string, string) (func() error, error)
}

type OAuthAdapter interface {
	ExchangeCode(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error)
	RefreshToken(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error)
}
