package state

import (
	"context"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

type OperationRepository interface {
	MarkAwaitingApproval(context.Context, string, int, string, time.Time) error
	SaveApproval(context.Context, approval.VerifiedApproval, time.Time) error
	ClaimExecution(context.Context, operations.ClaimExecutionRequest) (operations.ExecutionLease, error)
	TakeOverExecution(context.Context, string, int, string, time.Time, time.Duration) (operations.ExecutionLease, error)
	RenewExecutionLease(context.Context, operations.ExecutionLease, time.Time, time.Duration) (operations.ExecutionLease, error)
	BeginDispatch(context.Context, operations.DispatchIntentRequest) (operations.DispatchAttempt, error)
	CompleteDispatch(context.Context, operations.DispatchOutcome) error
	GetOperationState(context.Context, string, int) (operations.OperationState, error)
}
