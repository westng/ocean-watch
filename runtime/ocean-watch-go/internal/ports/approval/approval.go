package approval

import (
	"context"
	"errors"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

type Request struct {
	OperationID    string
	Version        int
	SnapshotDigest string
	ScopeDigest    string
	ExpiresAt      time.Time
	Summary        operations.ConfirmationSummary
}

type Provider interface {
	Request(context.Context, Request) error
}

// EvidenceSource is a protected, in-process delivery channel. It is never
// exposed as a tool or populated from CLI/MCP arguments.
type EvidenceSource interface {
	Receive(context.Context, Request) (Evidence, error)
	Discard(Request)
}

type Evidence struct {
	ApprovalID       string
	ProviderID       string
	AdapterID        string
	HostVersion      string
	OperationID      string
	OperationVersion int
	SnapshotDigest   string
	ScopeDigest      string
	SourceReference  string
	Nonce            string
	IssuedAt         time.Time
	ExpiresAt        time.Time
}

type EvidenceBinding struct {
	Decision     string
	EvidenceHash string
	NonceHash    string
}

// ProtectedTrustRoot is supplied only by the protected composition root, never by an operation request.
// It must authenticate a real human decision and reject automatic tool permission or untrusted callbacks.
type ProtectedTrustRoot interface {
	ProviderID() string
	AdapterID() string
	HostVersion() string
	VerifyEvidence(context.Context, Evidence) (EvidenceBinding, error)
}

type VerifiedApproval struct {
	record   operations.ApprovalRecord
	verified bool
}

func (a VerifiedApproval) Record() (operations.ApprovalRecord, error) {
	if !a.verified {
		return operations.ApprovalRecord{}, ErrUntrustedEvidence
	}
	if err := operations.VerifyEvidenceDigest(a.record); err != nil {
		return operations.ApprovalRecord{}, ErrUntrustedEvidence
	}
	return a.record, nil
}

type Verifier interface {
	Verify(context.Context, Evidence) (VerifiedApproval, error)
}

type TrustRootVerifier struct {
	root        ProtectedTrustRoot
	providerID  string
	adapterID   string
	hostVersion string
}

var (
	ErrVerifierUnavailable = errors.New("approval verifier is unavailable")
	ErrUntrustedEvidence   = errors.New("approval evidence is not trusted")
	ErrNotApproved         = errors.New("approval evidence does not contain an approved decision")
)

func NewVerifier(root ProtectedTrustRoot) (*TrustRootVerifier, error) {
	if root == nil || root.ProviderID() == "" || root.AdapterID() == "" || root.HostVersion() == "" {
		return nil, ErrVerifierUnavailable
	}
	return &TrustRootVerifier{root: root, providerID: root.ProviderID(), adapterID: root.AdapterID(), hostVersion: root.HostVersion()}, nil
}

func (v *TrustRootVerifier) Verify(ctx context.Context, evidence Evidence) (VerifiedApproval, error) {
	if v == nil || v.root == nil {
		return VerifiedApproval{}, ErrVerifierUnavailable
	}
	if ctx == nil {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	if err := ctx.Err(); err != nil {
		return VerifiedApproval{}, err
	}
	if evidence.ProviderID != v.providerID || evidence.AdapterID != v.adapterID || evidence.HostVersion != v.hostVersion {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	nonceHash, err := operations.NonceDigest(evidence.Nonce)
	if err != nil {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	record := operations.ApprovalRecord{
		ID: evidence.ApprovalID, OperationID: evidence.OperationID, Version: evidence.OperationVersion,
		SnapshotDigest: evidence.SnapshotDigest, ScopeDigest: evidence.ScopeDigest, ProviderID: evidence.ProviderID,
		AdapterID: evidence.AdapterID, HostVersion: evidence.HostVersion, NonceHash: nonceHash,
		SourceReference: evidence.SourceReference, IssuedAt: evidence.IssuedAt.UTC(), ExpiresAt: evidence.ExpiresAt.UTC(), Status: operations.ApprovalValid,
	}
	computed, err := operations.EvidenceDigest(record)
	if err != nil {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	binding, err := v.root.VerifyEvidence(ctx, evidence)
	if err != nil {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	if binding.Decision != "approved" {
		return VerifiedApproval{}, ErrNotApproved
	}
	if binding.NonceHash != nonceHash || binding.EvidenceHash != computed {
		return VerifiedApproval{}, ErrUntrustedEvidence
	}
	record.EvidenceHash = computed
	return VerifiedApproval{record: record, verified: true}, nil
}
