package state

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
)

type LegacyMappings interface {
	ImportLegacyMapping(context.Context, legacy.Mapping) (legacy.Mapping, bool, error)
	GetLegacyMapping(context.Context, string, string) (legacy.Mapping, error)
	LegacyBlockers(context.Context, legacy.Scope) ([]legacy.Blocker, error)
}
