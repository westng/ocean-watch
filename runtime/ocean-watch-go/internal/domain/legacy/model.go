package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
)

const (
	Unknown             = "unknown"
	TargetSchemaVersion = 1
	DecoderVersion      = "legacy-journal-v1"
	MaxJournalBytes     = 2 << 20
	MaxItems            = 2000
	MaxDepth            = 32
	MaxStringBytes      = 8192
)

type RecoveryState string

const (
	HistoryReadOnly    RecoveryState = "history_read_only"
	PendingReadOnly    RecoveryState = "pending_reconciliation_read_only"
	QuarantinedUnknown RecoveryState = "quarantined_unknown"
)

var (
	ErrUnsafeJournal   = errors.New("legacy journal cannot be safely decoded")
	ErrSourceChanged   = errors.New("legacy source differs from frozen input")
	ErrMappingConflict = errors.New("legacy source mapping conflicts with persisted input")
	ErrMappingNotFound = errors.New("legacy source mapping was not found")
	ErrWriteBlocked    = errors.New("legacy unresolved evidence blocks writes")
	identifier         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	positiveID         = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)
)

type Source struct {
	RootID   string
	RecordID string
	Checksum string
}

func (source Source) Validate() error {
	if !SafeID(source.RootID) || source.RootID == Unknown || !ValidRecordID(source.RecordID) || !ValidDigest(source.Checksum) {
		return ErrUnsafeJournal
	}
	return nil
}

func ValidRecordID(value string) bool {
	if !strings.HasPrefix(value, "runs/") || path.Clean(value) != value || !strings.HasSuffix(value, ".json") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "runs/"), ".json")
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for i, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && r == '-') {
			return false
		}
	}
	return true
}

func SafeID(value string) bool { return identifier.MatchString(value) }
func ValidDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}
func Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
func derivedID(prefix string, values ...string) string {
	encoded, _ := json.Marshal(values)
	return prefix + Digest(encoded)
}
func (source Source) OperationID() string {
	return derivedID("legacy_", source.RootID, source.RecordID)
}
func (source Source) MigrationID() string {
	return derivedID("migration_", source.RootID, source.RecordID, source.Checksum)
}

type Scope struct {
	Channel      string
	AdvertiserID string
}

func (scope Scope) Known() bool {
	return (scope.Channel == "marketing" || scope.Channel == "qianchuan") && positiveID.MatchString(scope.AdvertiserID)
}
func (scope Scope) Validate() error {
	if scope.Known() || scope.Channel == Unknown && scope.AdvertiserID == Unknown {
		return nil
	}
	return ErrUnsafeJournal
}

type RecoveryContract struct {
	ID                   string
	SourceRuntimeVersion string
	SourceRuntimeBuild   string
	SourceRuntimeDigest  string
	JournalSchema        string
	SnapshotSchema       string
	CompatibleRuntime    string
}

func (contract RecoveryContract) Validate() error {
	for _, value := range []string{contract.ID, contract.SourceRuntimeVersion, contract.SourceRuntimeBuild, contract.CompatibleRuntime} {
		if !SafeID(value) || value == Unknown {
			return ErrUnsafeJournal
		}
	}
	if !ValidDigest(contract.SourceRuntimeDigest) || contract.JournalSchema != "1" ||
		!(contract.SnapshotSchema == "none" || contract.SnapshotSchema == "1" || contract.SnapshotSchema == "2") {
		return ErrUnsafeJournal
	}
	return nil
}

type Mapping struct {
	Source            Source
	OperationID       string
	Version           int
	TargetSchema      int
	MigrationID       string
	Decoder           string
	SourceOperationID string
	FingerprintDigest string
	RuntimeVersion    string
	RuntimeBuild      string
	RuntimeDigest     string
	JournalSchema     string
	SnapshotSchema    string
	ContractID        string
	CompatibleRuntime string
	State             RecoveryState
	Items             []Item
}

type Item struct {
	SourceItemID        string
	TargetItemID        string
	DispatchKey         string
	RequestID           string
	ProjectID           string
	PromotionID         string
	AdID                string
	Status              string
	DispatchState       string
	ReconciliationState string
	EvidenceDigest      string
	Scope               Scope
	State               RecoveryState
	Reason              string
}

type Blocker struct {
	OperationID string
	Scope       Scope
	State       RecoveryState
	Reason      string
}

func (blocker Blocker) Blocks(scope Scope) bool {
	return !scope.Known() || !blocker.Scope.Known() || blocker.Scope == scope
}

func (mapping Mapping) Blockers() []Blocker {
	var blockers []Blocker
	for _, item := range mapping.Items {
		if item.State != HistoryReadOnly {
			blockers = append(blockers, Blocker{mapping.OperationID, item.Scope, item.State, item.Reason})
		}
	}
	return blockers
}

func (mapping Mapping) Validate() error {
	if err := mapping.Source.Validate(); err != nil {
		return err
	}
	if mapping.OperationID != mapping.Source.OperationID() || mapping.MigrationID != mapping.Source.MigrationID() ||
		mapping.Version != 1 || mapping.TargetSchema != TargetSchemaVersion || mapping.Decoder != DecoderVersion ||
		!ValidDigest(mapping.FingerprintDigest) || len(mapping.Items) == 0 || len(mapping.Items) > MaxItems {
		return ErrUnsafeJournal
	}
	for _, value := range []string{mapping.SourceOperationID, mapping.RuntimeVersion, mapping.RuntimeBuild, mapping.ContractID, mapping.CompatibleRuntime} {
		if !SafeID(value) {
			return ErrUnsafeJournal
		}
	}
	if mapping.RuntimeDigest != Unknown && !ValidDigest(mapping.RuntimeDigest) {
		return ErrUnsafeJournal
	}
	for _, value := range []string{mapping.JournalSchema, mapping.SnapshotSchema} {
		if !SafeID(value) {
			return ErrUnsafeJournal
		}
	}
	seen := map[string]bool{}
	for _, item := range mapping.Items {
		if !SafeID(item.SourceItemID) || item.TargetItemID != derivedID("item_", mapping.OperationID, item.SourceItemID) ||
			seen[item.SourceItemID] || !ValidDigest(item.EvidenceDigest) || item.Scope.Validate() != nil {
			return ErrUnsafeJournal
		}
		seen[item.SourceItemID] = true
		for _, value := range []string{item.DispatchKey, item.RequestID} {
			if !SafeID(value) {
				return ErrUnsafeJournal
			}
		}
		for _, value := range []string{item.ProjectID, item.PromotionID, item.AdID} {
			if value != Unknown && !positiveID.MatchString(value) {
				return ErrUnsafeJournal
			}
		}
		if !knownStatus(item.Status) || !validDispatch(item.DispatchState) || !validReconciliation(item.ReconciliationState) {
			return ErrUnsafeJournal
		}
		switch item.State {
		case HistoryReadOnly:
			if !mapping.metadataKnown() || !item.Scope.Known() || item.RequestID == Unknown || !closed(item) {
				return ErrUnsafeJournal
			}
		case PendingReadOnly:
			if !mapping.metadataKnown() || !item.Scope.Known() || item.RequestID == Unknown || item.DispatchKey == Unknown ||
				item.Status == Unknown || item.Status == "ambiguous" || item.ReconciliationState == "ambiguous" {
				return ErrUnsafeJournal
			}
		case QuarantinedUnknown:
		default:
			return ErrUnsafeJournal
		}
		if !validReason(item.Reason) {
			return ErrUnsafeJournal
		}
	}
	if mapping.State != aggregate(mapping.Items) {
		return ErrUnsafeJournal
	}
	return nil
}

func (mapping Mapping) metadataKnown() bool {
	return mapping.SourceOperationID != Unknown && mapping.RuntimeVersion != Unknown && mapping.RuntimeBuild != Unknown && mapping.RuntimeDigest != Unknown &&
		mapping.JournalSchema == "1" && (mapping.SnapshotSchema == "none" || mapping.SnapshotSchema == "1" || mapping.SnapshotSchema == "2") &&
		mapping.ContractID != Unknown && mapping.CompatibleRuntime != Unknown
}
func aggregate(items []Item) RecoveryState {
	state := HistoryReadOnly
	for _, item := range items {
		if item.State == QuarantinedUnknown {
			return QuarantinedUnknown
		}
		if item.State == PendingReadOnly {
			state = PendingReadOnly
		}
	}
	return state
}
func closed(item Item) bool {
	if item.DispatchState == "unknown" || item.ReconciliationState == "ambiguous" || item.ReconciliationState == "not_applied" {
		return false
	}
	return item.Status == "completed" && item.DispatchState == "acknowledged" && (item.PromotionID != Unknown || item.AdID != Unknown)
}
func knownStatus(value string) bool {
	switch value {
	case Unknown, "pending", "prepared", "account_pending", "project_dispatching", "promotion_dispatching", "project_created", "promotion_failed", "promotion_retrying", "project_failed", "completed", "ambiguous":
		return true
	}
	return false
}
func validDispatch(value string) bool {
	return value == Unknown || value == "acknowledged" || value == "not_sent"
}
func validReconciliation(value string) bool {
	return value == Unknown || value == "applied" || value == "not_applied" || value == "ambiguous"
}
func validReason(value string) bool {
	switch value {
	case "closed_history", "awaiting_read_only_reconciliation", "metadata_unknown", "runtime_incompatible", "scope_unknown", "unknown_request_id", "ambiguous_evidence", "unknown_fields", "duplicate_evidence":
		return true
	}
	return false
}

func QuarantineDuplicate(mapping Mapping) Mapping {
	mapping.Items = append([]Item(nil), mapping.Items...)
	for i := range mapping.Items {
		mapping.Items[i].State = QuarantinedUnknown
		mapping.Items[i].Reason = "duplicate_evidence"
	}
	mapping.State = QuarantinedUnknown
	return mapping
}
