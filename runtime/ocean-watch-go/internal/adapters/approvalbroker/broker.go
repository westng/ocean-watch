// Package approvalbroker implements a short-lived, local human confirmation
// boundary. It has no credential store, official API client, or write executor.
package approvalbroker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

var (
	ErrUnavailable = errors.New("local approval broker is unavailable")
	ErrBusy        = errors.New("local approval broker has a pending confirmation")
)

const (
	providerID = "local-broker"
	adapterID  = "local-os-dialog"
	protocolID = "broker-v1"
)

type exchangeFunc func(context.Context, approval.Request) error

type pendingApproval struct {
	evidence  approval.Evidence
	document  string
	delivered bool
}

type Broker struct {
	mu       sync.Mutex
	busy     bool
	pending  *pendingApproval
	exchange exchangeFunc
	now      func() time.Time
}

var (
	_ approval.Provider           = (*Broker)(nil)
	_ approval.EvidenceSource     = (*Broker)(nil)
	_ approval.ProtectedTrustRoot = (*Broker)(nil)
)

// New uses the running Runtime itself as the independent broker executable.
// Neither executable paths, provider identities nor UI implementations are
// ordinary operation inputs. Creating it does not open a UI or a socket.
func New() (*Broker, error) {
	if !platformAvailable() {
		return nil, ErrUnavailable
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Broker{now: time.Now, exchange: func(ctx context.Context, request approval.Request) error {
		return exchangeProcess(ctx, executable, request)
	}}, nil
}

func (*Broker) ProviderID() string  { return providerID }
func (*Broker) AdapterID() string   { return adapterID }
func (*Broker) HostVersion() string { return protocolID }

func (b *Broker) Request(ctx context.Context, request approval.Request) error {
	if ctx == nil || b == nil || b.exchange == nil || b.now == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	document, err := requestDocument(request)
	if err != nil {
		return err
	}
	// Own all nested slices while the UI is open.
	canonical, _ := request.Summary.Canonical()
	var frozen domain.ConfirmationSummary
	if err := json.Unmarshal(canonical, &frozen); err != nil {
		return approval.ErrUntrustedEvidence
	}
	request.Summary = frozen
	now := b.now().UTC()
	if now.Before(request.Summary.PreflightAt) || !now.Before(request.ExpiresAt) || request.ExpiresAt.After(now.Add(5*time.Minute)) {
		return domain.ErrApprovalExpired
	}
	b.mu.Lock()
	if b.pending != nil && !now.Before(b.pending.evidence.ExpiresAt) {
		b.pending = nil
	}
	if b.busy || b.pending != nil {
		b.mu.Unlock()
		return ErrBusy
	}
	b.busy = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.busy = false
		b.mu.Unlock()
	}()
	bounded, cancel := context.WithDeadline(ctx, request.ExpiresAt)
	defer cancel()
	if err := b.exchange(bounded, request); err != nil {
		if bounded.Err() != nil {
			return bounded.Err()
		}
		return err
	}
	if err := bounded.Err(); err != nil {
		return err
	}
	now = b.now().UTC()
	if !now.Before(request.ExpiresAt) {
		return domain.ErrApprovalExpired
	}
	nonce, err := randomID()
	if err != nil {
		return ErrUnavailable
	}
	id, err := randomID()
	if err != nil {
		return ErrUnavailable
	}
	event, err := randomID()
	if err != nil {
		return ErrUnavailable
	}
	b.mu.Lock()
	b.pending = &pendingApproval{document: document, evidence: approval.Evidence{
		ApprovalID: "ap_" + id, ProviderID: providerID, AdapterID: adapterID, HostVersion: protocolID,
		OperationID: request.OperationID, OperationVersion: request.Version, SnapshotDigest: request.SnapshotDigest,
		ScopeDigest: request.ScopeDigest, SourceReference: "event_" + event, Nonce: nonce,
		IssuedAt: now, ExpiresAt: request.ExpiresAt.UTC(),
	}}
	b.mu.Unlock()
	return nil
}

func (b *Broker) Receive(ctx context.Context, request approval.Request) (approval.Evidence, error) {
	if ctx == nil || b == nil {
		return approval.Evidence{}, approval.ErrUntrustedEvidence
	}
	if err := ctx.Err(); err != nil {
		return approval.Evidence{}, err
	}
	document, err := requestDocument(request)
	if err != nil {
		return approval.Evidence{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil || b.pending.delivered || b.pending.document != document {
		return approval.Evidence{}, approval.ErrUntrustedEvidence
	}
	if !b.now().Before(b.pending.evidence.ExpiresAt) {
		b.pending = nil
		return approval.Evidence{}, domain.ErrApprovalExpired
	}
	b.pending.delivered = true
	return b.pending.evidence, nil
}

func (b *Broker) Discard(request approval.Request) {
	if b == nil {
		return
	}
	document, err := requestDocument(request)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending != nil && b.pending.document == document {
		b.pending = nil
	}
}

func (b *Broker) VerifyEvidence(ctx context.Context, evidence approval.Evidence) (approval.EvidenceBinding, error) {
	if ctx == nil || b == nil {
		return approval.EvidenceBinding{}, approval.ErrUntrustedEvidence
	}
	if err := ctx.Err(); err != nil {
		return approval.EvidenceBinding{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil || !b.pending.delivered || b.pending.evidence != evidence {
		return approval.EvidenceBinding{}, approval.ErrUntrustedEvidence
	}
	if !b.now().Before(evidence.ExpiresAt) || b.now().Before(evidence.IssuedAt) {
		b.pending = nil
		return approval.EvidenceBinding{}, domain.ErrApprovalExpired
	}
	nonceHash, err := domain.NonceDigest(evidence.Nonce)
	if err != nil {
		return approval.EvidenceBinding{}, err
	}
	digest, err := domain.EvidenceDigest(domain.ApprovalRecord{
		ID: evidence.ApprovalID, OperationID: evidence.OperationID, Version: evidence.OperationVersion,
		SnapshotDigest: evidence.SnapshotDigest, ScopeDigest: evidence.ScopeDigest, ProviderID: evidence.ProviderID,
		AdapterID: evidence.AdapterID, HostVersion: evidence.HostVersion, NonceHash: nonceHash,
		SourceReference: evidence.SourceReference, IssuedAt: evidence.IssuedAt, ExpiresAt: evidence.ExpiresAt, Status: domain.ApprovalValid,
	})
	if err != nil {
		return approval.EvidenceBinding{}, approval.ErrUntrustedEvidence
	}
	b.pending = nil
	return approval.EvidenceBinding{Decision: "approved", EvidenceHash: digest, NonceHash: nonceHash}, nil
}

func requestDocument(request approval.Request) (string, error) {
	if request.OperationID != request.Summary.OperationID || request.Version != request.Summary.Version ||
		request.SnapshotDigest != request.Summary.SnapshotDigest || request.ScopeDigest != request.Summary.ScopeDigest ||
		!request.ExpiresAt.Equal(request.Summary.ApprovalExpiresAt) {
		return "", approval.ErrUntrustedEvidence
	}
	document, err := request.Summary.Canonical()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:]), nil
}

func randomID() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
