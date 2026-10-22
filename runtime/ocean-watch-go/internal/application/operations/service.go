package operations

import (
	"context"
	"errors"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type Clock func() time.Time

type Service struct {
	Repository state.OperationRepository
	Verifier   approval.Verifier
	Now        Clock
}

type ExecuteRequest struct {
	OperationID    string
	Version        int
	SnapshotDigest string
	ApprovalID     string
	OwnerID        string
	TTL            time.Duration
}

type ExecutionResult struct {
	Lease domain.ExecutionLease
}

func (s Service) RequestConfirmation(ctx context.Context, operationID string, version int, snapshotDigest string) error {
	if s.Repository == nil {
		return errors.New("operation repository is required")
	}
	return s.Repository.MarkAwaitingApproval(ctx, operationID, version, snapshotDigest, s.now())
}

func (s Service) AcceptApproval(ctx context.Context, evidence approval.Evidence) (domain.ApprovalRecord, error) {
	if s.Repository == nil || s.Verifier == nil {
		return domain.ApprovalRecord{}, errors.New("approval repository and verifier are required")
	}
	verified, err := s.Verifier.Verify(ctx, evidence)
	if err != nil {
		return domain.ApprovalRecord{}, err
	}
	now := s.now()
	record, err := verified.Record()
	if err != nil {
		return domain.ApprovalRecord{}, err
	}
	if !record.ValidAt(now) {
		return domain.ApprovalRecord{}, domain.ErrApprovalExpired
	}
	if err := s.Repository.SaveApproval(ctx, verified, now); err != nil {
		return domain.ApprovalRecord{}, err
	}
	return record, nil
}

func (s Service) Execute(ctx context.Context, request ExecuteRequest) (ExecutionResult, error) {
	if s.Repository == nil {
		return ExecutionResult{}, errors.New("operation repository is required")
	}
	lease, err := s.Repository.ClaimExecution(ctx, domain.ClaimExecutionRequest{
		OperationID: request.OperationID, Version: request.Version, SnapshotDigest: request.SnapshotDigest,
		ApprovalID: request.ApprovalID, OwnerID: request.OwnerID, Now: s.now(), TTL: request.TTL,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	return ExecutionResult{Lease: lease}, nil
}

func (s Service) TakeOverExecution(ctx context.Context, operationID string, version int, ownerID string, now time.Time, ttl time.Duration) (domain.ExecutionLease, error) {
	if s.Repository == nil {
		return domain.ExecutionLease{}, errors.New("operation repository is required")
	}
	now = s.now()
	return s.Repository.TakeOverExecution(ctx, operationID, version, ownerID, now, ttl)
}

func (s Service) BeginDispatch(ctx context.Context, request domain.DispatchIntentRequest) (domain.DispatchAttempt, error) {
	if s.Repository == nil {
		return domain.DispatchAttempt{}, errors.New("operation repository is required")
	}
	request.Now = s.now()
	return s.Repository.BeginDispatch(ctx, request)
}

func (s Service) CompleteDispatch(ctx context.Context, outcome domain.DispatchOutcome) error {
	if s.Repository == nil {
		return errors.New("operation repository is required")
	}
	outcome.Now = s.now()
	return s.Repository.CompleteDispatch(ctx, outcome)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
