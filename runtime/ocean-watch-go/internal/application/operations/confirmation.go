package operations

import (
	"context"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

const ConfirmationLifetime = 5 * time.Minute

// ConfirmationResult contains only safe references. Evidence and private
// challenges stay between the Provider, EvidenceSource, and Verifier.
type ConfirmationResult struct {
	OperationID    string    `json:"operation_id"`
	Version        int       `json:"version"`
	SnapshotDigest string    `json:"snapshot_digest"`
	ApprovalID     string    `json:"approval_id"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Confirm never executes a business write or accepts caller-written summary
// text. The repository checks current version/expiry again when saving the
// decision, so a version change while the UI is open invalidates it.
func (s Service) Confirm(ctx context.Context, operationID string, version int, digest string) (ConfirmationResult, error) {
	if ctx == nil {
		return ConfirmationResult{}, approval.ErrVerifierUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ConfirmationResult{}, err
	}
	if s.Repository == nil || s.Snapshots == nil || s.Provider == nil || s.Evidence == nil || s.Verifier == nil {
		return ConfirmationResult{}, approval.ErrVerifierUnavailable
	}
	snapshot, err := s.Snapshots.GetSnapshot(ctx, operationID, version)
	if err != nil {
		return ConfirmationResult{}, err
	}
	if snapshot.SnapshotDigest != digest {
		return ConfirmationResult{}, domain.ErrOperationNotExecutable
	}
	now := s.now()
	if now.Before(snapshot.CreatedAt) || !now.Before(snapshot.ExpiresAt) {
		return ConfirmationResult{}, domain.ErrApprovalExpired
	}
	expires := now.Add(ConfirmationLifetime)
	if snapshot.ExpiresAt.Before(expires) {
		expires = snapshot.ExpiresAt
	}
	summary, err := domain.NewConfirmationSummary(snapshot, expires)
	if err != nil {
		return ConfirmationResult{}, err
	}
	request := approval.Request{OperationID: operationID, Version: version, SnapshotDigest: digest,
		ScopeDigest: summary.ScopeDigest, ExpiresAt: expires, Summary: summary}
	if err := s.RequestConfirmation(ctx, operationID, version, digest); err != nil {
		return ConfirmationResult{}, err
	}
	if err := s.Provider.Request(ctx, request); err != nil {
		return ConfirmationResult{}, err
	}
	defer s.Evidence.Discard(request)
	evidence, err := s.Evidence.Receive(ctx, request)
	if err != nil {
		return ConfirmationResult{}, err
	}
	// A trusted provider is still required to answer this exact request.
	if evidence.OperationID != operationID || evidence.OperationVersion != version ||
		evidence.SnapshotDigest != digest || evidence.ScopeDigest != request.ScopeDigest || evidence.ExpiresAt.After(expires) {
		return ConfirmationResult{}, approval.ErrUntrustedEvidence
	}
	record, err := s.AcceptApproval(ctx, evidence)
	if err != nil {
		return ConfirmationResult{}, err
	}
	return ConfirmationResult{OperationID: operationID, Version: version, SnapshotDigest: digest,
		ApprovalID: record.ID, ExpiresAt: record.ExpiresAt}, nil
}
