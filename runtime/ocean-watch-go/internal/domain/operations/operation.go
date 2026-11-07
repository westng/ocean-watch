package operations

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Status string

const (
	StatusDraft              Status = "draft"
	StatusPreparing          Status = "preparing"
	StatusPreflightReady     Status = "preflight_ready"
	StatusAwaitingApproval   Status = "awaiting_approval"
	StatusApproved           Status = "approved"
	StatusExecuting          Status = "executing"
	StatusBlocked            Status = "blocked"
	StatusCancelled          Status = "cancelled"
	StatusExpired            Status = "expired"
	StatusSucceeded          Status = "succeeded"
	StatusFailed             Status = "failed"
	StatusPartiallySucceeded Status = "partially_succeeded"
	StatusUnknown            Status = "unknown"
	StatusReconciling        Status = "reconciling"
)

type DispatchState string

const (
	DispatchIntent       DispatchState = "intent"
	DispatchNotSent      DispatchState = "not_sent"
	DispatchAcknowledged DispatchState = "acknowledged"
	DispatchUnknown      DispatchState = "unknown"
)

type ItemStatus string

const (
	ItemPending      ItemStatus = "pending"
	ItemIntent       ItemStatus = "intent"
	ItemNotSent      ItemStatus = "not_sent"
	ItemAcknowledged ItemStatus = "acknowledged"
	ItemUnknown      ItemStatus = "unknown"
)

type ApprovalStatus string

const (
	ApprovalValid    ApprovalStatus = "valid"
	ApprovalConsumed ApprovalStatus = "consumed"
	ApprovalExpired  ApprovalStatus = "expired"
	ApprovalRejected ApprovalStatus = "rejected"
)

var (
	ErrInvalidOperation       = errors.New("invalid operation")
	ErrInvalidApproval        = errors.New("invalid approval")
	ErrInvalidDispatch        = errors.New("invalid dispatch")
	ErrInvalidTransition      = errors.New("invalid operation status transition")
	ErrOperationNotFound      = errors.New("operation was not found")
	ErrApprovalNotFound       = errors.New("approval was not found")
	ErrApprovalExpired        = errors.New("approval is expired")
	ErrApprovalConsumed       = errors.New("approval was already consumed")
	ErrApprovalReplay         = errors.New("approval nonce or id was already used")
	ErrLeaseHeld              = errors.New("execution lease is held by another owner")
	ErrLeaseExpired           = errors.New("execution lease is expired")
	ErrStaleEpoch             = errors.New("execution lease fencing epoch is stale")
	ErrOperationNotExecutable = errors.New("operation is not executable")
	ErrDispatchInProgress     = errors.New("dispatch is already in progress")
	ErrDispatchAcknowledged   = errors.New("dispatch was already acknowledged")
	ErrDispatchUnknown        = errors.New("dispatch result is unknown and requires reconciliation")
)

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPreparing, StatusPreflightReady, StatusAwaitingApproval, StatusApproved,
		StatusExecuting, StatusBlocked, StatusCancelled, StatusExpired, StatusSucceeded, StatusFailed,
		StatusPartiallySucceeded, StatusUnknown, StatusReconciling:
		return true
	default:
		return false
	}
}

func (s ItemStatus) Valid() bool {
	switch s {
	case ItemPending, ItemIntent, ItemNotSent, ItemAcknowledged, ItemUnknown:
		return true
	default:
		return false
	}
}

func (s DispatchState) Valid() bool {
	switch s {
	case DispatchIntent, DispatchNotSent, DispatchAcknowledged, DispatchUnknown:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusDraft:
		return to == StatusPreparing || to == StatusBlocked || to == StatusCancelled
	case StatusPreparing:
		return to == StatusPreflightReady || to == StatusBlocked || to == StatusCancelled
	case StatusPreflightReady:
		return to == StatusAwaitingApproval || to == StatusBlocked || to == StatusCancelled || to == StatusExpired
	case StatusAwaitingApproval:
		return to == StatusApproved || to == StatusBlocked || to == StatusCancelled || to == StatusExpired
	case StatusApproved:
		return to == StatusExecuting || to == StatusCancelled || to == StatusExpired
	case StatusExecuting:
		return to == StatusSucceeded || to == StatusFailed || to == StatusPartiallySucceeded || to == StatusUnknown
	case StatusUnknown, StatusPartiallySucceeded:
		return to == StatusReconciling
	case StatusReconciling:
		return to == StatusSucceeded || to == StatusFailed || to == StatusPartiallySucceeded || to == StatusUnknown
	default:
		return false
	}
}

func ValidateTransition(from, to Status) error {
	if !from.Valid() || !to.Valid() || !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

type OperationState struct {
	OperationID    string
	Version        int
	Status         Status
	SnapshotDigest string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s OperationState) Validate() error {
	if !operationIDPattern.MatchString(s.OperationID) {
		return invalidOperation("operation_id is invalid")
	}
	if s.Version < 1 {
		return invalidOperation("version must be positive")
	}
	if !s.Status.Valid() {
		return invalidOperation("status is invalid")
	}
	if !digestPattern.MatchString(s.SnapshotDigest) {
		return invalidOperation("snapshot_digest is invalid")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		return invalidOperation("created_at and updated_at are required")
	}
	if s.UpdatedAt.Before(s.CreatedAt) {
		return invalidOperation("updated_at must not precede created_at")
	}
	return nil
}

type ApprovalRecord struct {
	ID              string
	OperationID     string
	Version         int
	SnapshotDigest  string
	ScopeDigest     string
	ProviderID      string
	AdapterID       string
	HostVersion     string
	EvidenceHash    string
	NonceHash       string
	SourceReference string
	IssuedAt        time.Time
	ExpiresAt       time.Time
	Status          ApprovalStatus
}

func (a ApprovalRecord) Validate() error {
	if !safeIDPattern.MatchString(a.ID) || !strings.HasPrefix(a.ID, "ap_") {
		return invalidApproval("approval id is invalid")
	}
	if !operationIDPattern.MatchString(a.OperationID) || a.Version < 1 {
		return invalidApproval("approval operation binding is invalid")
	}
	for value, field := range map[string]string{
		a.SnapshotDigest: "snapshot_digest", a.ScopeDigest: "scope_digest", a.EvidenceHash: "evidence_hash", a.NonceHash: "nonce_hash",
	} {
		if !digestPattern.MatchString(value) {
			return invalidApproval("%s is invalid", field)
		}
	}
	for value, field := range map[string]string{
		a.ProviderID: "provider_id", a.AdapterID: "adapter_id", a.HostVersion: "host_version",
	} {
		if err := validateIdentifier(value, field); err != nil {
			return err
		}
	}
	if !safeIDPattern.MatchString(a.SourceReference) || !strings.HasPrefix(a.SourceReference, "event_") || strings.ContainsAny(a.SourceReference, ":=") {
		return invalidApproval("source_reference must be a non-secret opaque event reference")
	}
	if a.IssuedAt.IsZero() || a.ExpiresAt.IsZero() || !a.ExpiresAt.After(a.IssuedAt) {
		return invalidApproval("issued_at and expires_at must be an increasing interval")
	}
	if a.Status != ApprovalValid {
		return invalidApproval("new approvals must be valid")
	}
	return nil
}

func (a ApprovalRecord) ValidAt(now time.Time) bool {
	now = now.UTC()
	return a.Status == ApprovalValid && !now.Before(a.IssuedAt.UTC()) && now.Before(a.ExpiresAt.UTC())
}

type ExecutionLease struct {
	OperationID  string
	Version      int
	OwnerID      string
	FencingEpoch int64
	AcquiredAt   time.Time
	ExpiresAt    time.Time
	RenewedAt    time.Time
}

func (l ExecutionLease) Validate() error {
	if !operationIDPattern.MatchString(l.OperationID) || l.Version < 1 {
		return invalidOperation("lease operation binding is invalid")
	}
	if err := validateIdentifier(l.OwnerID, "owner_id"); err != nil {
		return err
	}
	if l.FencingEpoch < 1 {
		return invalidOperation("fencing_epoch must be positive")
	}
	if l.AcquiredAt.IsZero() || l.ExpiresAt.IsZero() || !l.ExpiresAt.After(l.AcquiredAt) {
		return invalidOperation("lease times are invalid")
	}
	if l.RenewedAt.IsZero() || l.RenewedAt.Before(l.AcquiredAt) {
		return invalidOperation("renewed_at is invalid")
	}
	return nil
}

type ClaimExecutionRequest struct {
	OperationID    string
	Version        int
	SnapshotDigest string
	ApprovalID     string
	OwnerID        string
	Now            time.Time
	TTL            time.Duration
}

func (r ClaimExecutionRequest) Validate() error {
	if !operationIDPattern.MatchString(r.OperationID) || r.Version < 1 {
		return invalidOperation("claim operation binding is invalid")
	}
	if !digestPattern.MatchString(r.SnapshotDigest) {
		return invalidOperation("claim snapshot_digest is invalid")
	}
	if !safeIDPattern.MatchString(r.ApprovalID) || !strings.HasPrefix(r.ApprovalID, "ap_") {
		return invalidOperation("claim approval_id is invalid")
	}
	if err := validateIdentifier(r.OwnerID, "owner_id"); err != nil {
		return err
	}
	if r.Now.IsZero() || r.TTL <= 0 || r.TTL > 24*time.Hour {
		return invalidOperation("claim now and ttl are invalid")
	}
	return nil
}

type DispatchIntentRequest struct {
	OperationID    string
	Version        int
	ItemID         string
	GroupID        string
	Action         string
	TargetID       string
	PayloadDigest  string
	ExpectedDigest string
	OwnerID        string
	FencingEpoch   int64
	Now            time.Time
}

func (r DispatchIntentRequest) Validate() error {
	if !operationIDPattern.MatchString(r.OperationID) || r.Version < 1 {
		return invalidDispatch("dispatch operation binding is invalid")
	}
	for value, field := range map[string]string{r.ItemID: "item_id", r.GroupID: "group_id", r.Action: "action", r.TargetID: "target_id"} {
		if err := validateIdentifier(value, field); err != nil {
			return err
		}
	}
	for value, field := range map[string]string{r.PayloadDigest: "payload_digest", r.ExpectedDigest: "expected_digest"} {
		if !digestPattern.MatchString(value) {
			return invalidDispatch("%s is invalid", field)
		}
	}
	if err := validateIdentifier(r.OwnerID, "owner_id"); err != nil {
		return err
	}
	if r.FencingEpoch < 1 || r.Now.IsZero() {
		return invalidDispatch("fencing epoch and now are required")
	}
	return nil
}

type DispatchAttempt struct {
	OperationID       string
	Version           int
	ItemID            string
	AttemptNo         int
	DispatchKey       string
	State             DispatchState
	OfficialRequestID string
	StartedAt         time.Time
	FinishedAt        time.Time
	ResponseDigest    string
}

type DispatchOutcome struct {
	OperationID       string
	Version           int
	ItemID            string
	DispatchKey       string
	OwnerID           string
	FencingEpoch      int64
	State             DispatchState
	OfficialRequestID string
	ObjectID          string
	ResponseDigest    string
	Now               time.Time
}

func (o DispatchOutcome) Validate() error {
	if o.ObjectID != "" && !safeIDPattern.MatchString(o.ObjectID) {
		return ErrInvalidDispatch
	}
	if !operationIDPattern.MatchString(o.OperationID) || o.Version < 1 {
		return invalidDispatch("dispatch outcome operation binding is invalid")
	}
	if err := validateIdentifier(o.ItemID, "item_id"); err != nil {
		return err
	}
	if !digestPattern.MatchString(o.DispatchKey) {
		return invalidDispatch("dispatch_key is required")
	}
	if err := validateIdentifier(o.OwnerID, "owner_id"); err != nil {
		return err
	}
	if o.FencingEpoch < 1 || o.Now.IsZero() || !o.State.Valid() || o.State == DispatchIntent {
		return invalidDispatch("dispatch outcome is invalid")
	}
	if o.ResponseDigest != "" && !digestPattern.MatchString(o.ResponseDigest) {
		return invalidDispatch("response_digest is invalid")
	}
	if o.State == DispatchAcknowledged && o.OfficialRequestID == "" {
		return invalidDispatch("acknowledged dispatch requires official_request_id")
	}
	if o.OfficialRequestID != "" && !safeIDPattern.MatchString(o.OfficialRequestID) {
		return invalidDispatch("official_request_id is invalid")
	}
	return nil
}

func (s ItemStatus) DispatchState() DispatchState {
	switch s {
	case ItemIntent:
		return DispatchIntent
	case ItemNotSent:
		return DispatchNotSent
	case ItemAcknowledged:
		return DispatchAcknowledged
	case ItemUnknown:
		return DispatchUnknown
	default:
		return ""
	}
}

func validateIdentifier(value, field string) error {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || !safeIDPattern.MatchString(value) {
		return invalidOperation("%s is invalid", field)
	}
	return nil
}

func validateText(value, field string) error {
	if value == "" || len(value) > 512 || !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return invalidOperation("%s is invalid", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return invalidOperation("%s contains control characters", field)
		}
	}
	return nil
}

func invalidOperation(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidOperation, fmt.Sprintf(format, args...))
}

func invalidApproval(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidApproval, fmt.Sprintf(format, args...))
}

func invalidDispatch(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDispatch, fmt.Sprintf(format, args...))
}
