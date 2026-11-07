package state

import (
	"context"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"time"
)

type OperationRecovery interface {
	ReadDispatches(context.Context, string, int) ([]operations.DispatchRecord, error)
	SaveReadback(context.Context, operations.Readback) error
	FinishExecution(context.Context, operations.ExecutionLease, time.Time) (operations.Status, error)
}
