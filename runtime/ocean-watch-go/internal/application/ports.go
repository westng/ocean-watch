package application

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type ConfigUpdater = func(map[string]any) (result any, changed bool, err error)

type AccountStore interface {
	Read(context.Context) (domain.AccountBook, error)
	Update(context.Context, func(*domain.AccountBook) error) (any, error)
}

type RunStore interface {
	List(context.Context, int) ([]domain.RunSummary, error)
	Show(context.Context, string) (domain.RunSummary, domain.RunJournal, error)
}

type AuthorizationReader interface {
	ReadChannel(context.Context, string) (domain.AuthorizationState, error)
}

type CredentialStore interface {
	BackendName() string
	Read(context.Context, string) (map[string]any, error)
	Write(context.Context, string, map[string]any) (string, error)
}
