package workmetadata

import (
	"context"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type Resolver interface {
	Resolve(context.Context, string) (domain.ResolvedWorkLink, error)
}
