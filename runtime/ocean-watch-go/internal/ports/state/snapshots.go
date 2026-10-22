package state

import (
	"context"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

type Snapshots interface {
	SavePreflight(context.Context, operations.Snapshot) error
	GetSnapshot(context.Context, string, int) (operations.Snapshot, error)
}
