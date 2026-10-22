package legacy

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

func Decode(source Source, payload []byte, contracts []RecoveryContract) (Mapping, error) {
	if err := source.Validate(); err != nil {
		return Mapping{}, err
	}
	if len(payload) == 0 || len(payload) > MaxJournalBytes || !utf8.Valid(payload) {
		return Mapping{}, ErrUnsafeJournal
	}
	if Digest(payload) != source.Checksum {
		return Mapping{}, ErrSourceChanged
	}
	if err := checkJSON(payload); err != nil {
		return Mapping{}, err
	}
	for _, contract := range contracts {
		if err := contract.Validate(); err != nil {
			return Mapping{}, err
		}
	}
	var journal plans.Journal
	if err := json.Unmarshal(payload, &journal); err != nil {
		return Mapping{}, ErrUnsafeJournal
	}
	if len(journal.Jobs) > MaxItems {
		return Mapping{}, ErrUnsafeJournal
	}
	if _, err := plans.NewJournal(journal.Fingerprint, journal.Jobs, time.Unix(1, 0)); err != nil {
		return Mapping{}, ErrUnsafeJournal
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Mapping{}, ErrUnsafeJournal
	}
	mapping := Mapping{
		Source: source, OperationID: source.OperationID(), Version: 1, TargetSchema: TargetSchemaVersion,
		MigrationID: source.MigrationID(), Decoder: DecoderVersion,
		SourceOperationID: readID(raw, "operation_id"), FingerprintDigest: Digest([]byte(journal.Fingerprint)),
		RuntimeVersion: readSourceVersion(raw), RuntimeBuild: readID(raw, "runtime_build"),
		RuntimeDigest: readID(raw, "runtime_digest"), JournalSchema: readSchema(raw, "schema_version"), SnapshotSchema: "none",
		ContractID: Unknown, CompatibleRuntime: Unknown,
	}
	if mapping.RuntimeDigest != Unknown && !ValidDigest(mapping.RuntimeDigest) {
		mapping.RuntimeDigest = Unknown
	}
	if snapshot, exists := raw["snapshot"]; exists {
		var header map[string]json.RawMessage
		if err := json.Unmarshal(snapshot, &header); err != nil {
			return Mapping{}, ErrUnsafeJournal
		}
		mapping.SnapshotSchema = readSchema(header, "schema_version")
	}
	for _, contract := range contracts {
		if contract.SourceRuntimeVersion == mapping.RuntimeVersion && contract.SourceRuntimeBuild == mapping.RuntimeBuild &&
			contract.SourceRuntimeDigest == mapping.RuntimeDigest && contract.JournalSchema == mapping.JournalSchema && contract.SnapshotSchema == mapping.SnapshotSchema {
			if mapping.ContractID != Unknown {
				return Mapping{}, ErrUnsafeJournal
			}
			mapping.ContractID, mapping.CompatibleRuntime = contract.ID, contract.CompatibleRuntime
		}
	}
	unknownRootFields := unknownFields(raw, "schema_version", "fingerprint", "created_at", "jobs", "operation_id", "dispatch_key", "channel", "source_version", "runtime_build", "runtime_digest", "snapshot", "kind", "batch_kind", "scope_fingerprint", "upload_batch")
	var rawJobs map[string]json.RawMessage
	if err := json.Unmarshal(raw["jobs"], &rawJobs); err != nil {
		return Mapping{}, ErrUnsafeJournal
	}
	keys := make([]string, 0, len(journal.Jobs))
	for key := range journal.Jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		job := journal.Jobs[key]
		sourceID := key
		if !SafeID(sourceID) {
			sourceID = derivedID("digest_", key)
		}
		item := Item{
			SourceItemID: sourceID, TargetItemID: derivedID("item_", mapping.OperationID, sourceID),
			DispatchKey: readID(job.Extra, "dispatch_key"), RequestID: safeOrUnknown(job.RequestID),
			ProjectID: decimalOrUnknown(job.ProjectID), PromotionID: decimalOrUnknown(job.PromotionID), AdID: decimalOrUnknown(job.AdID),
			Status: job.Status, DispatchState: string(job.DispatchState), ReconciliationState: Unknown,
			Scope: Scope{Channel: Unknown, AdvertiserID: Unknown}, State: QuarantinedUnknown, Reason: "metadata_unknown",
		}
		if !knownStatus(item.Status) {
			item.Status = Unknown
		}
		if !validDispatch(item.DispatchState) {
			item.DispatchState = Unknown
		}
		if item.DispatchKey == Unknown {
			item.DispatchKey = readID(raw, "dispatch_key")
		}
		if job.Reconciliation != nil && validReconciliation(string(job.Reconciliation.State)) {
			item.ReconciliationState = string(job.Reconciliation.State)
		}
		if job.LastResponse != nil {
			if item.RequestID == Unknown {
				item.RequestID = safeOrUnknown(job.LastResponse.RequestID)
			}
			if job.RequestID != "" && job.LastResponse.RequestID != "" && job.RequestID != job.LastResponse.RequestID {
				item.RequestID = Unknown
			}
		}
		channel := readID(raw, "channel")
		jobChannel := readID(job.Extra, "channel")
		if channel == Unknown {
			channel = jobChannel
		} else if jobChannel != Unknown && jobChannel != channel {
			channel = Unknown
		}
		candidate := Scope{channel, job.AdvertiserID}
		if candidate.Known() {
			item.Scope = candidate
		}
		encoded, err := json.Marshal(job)
		if err != nil {
			return Mapping{}, ErrUnsafeJournal
		}
		item.EvidenceDigest = Digest(encoded)
		unknown := unknownRootFields || unknownFields(job.Extra, "dispatch_key", "channel", "prepared_group", "plan_template", "creator_id") || unknownEvidenceFields(rawJobs[key])
		switch {
		case !item.Scope.Known():
			item.Reason = "scope_unknown"
		case unknown:
			item.Reason = "unknown_fields"
		case mapping.RuntimeVersion == Unknown || mapping.RuntimeBuild == Unknown || mapping.RuntimeDigest == Unknown || mapping.JournalSchema != "1" || mapping.SnapshotSchema == Unknown:
			item.Reason = "metadata_unknown"
		case mapping.ContractID == Unknown:
			item.Reason = "runtime_incompatible"
		case mapping.SourceOperationID == Unknown:
			item.Reason = "metadata_unknown"
		case item.Status == Unknown || item.Status == "ambiguous" || item.ReconciliationState == "ambiguous" || conflictingEvidence(job):
			item.Reason = "ambiguous_evidence"
		case item.RequestID == Unknown:
			item.Reason = "unknown_request_id"
		case closed(item) && mapping.JournalSchema == "1":
			item.State, item.Reason = HistoryReadOnly, "closed_history"
		case item.DispatchKey == Unknown:
			item.Reason = "metadata_unknown"
		default:
			item.State, item.Reason = PendingReadOnly, "awaiting_read_only_reconciliation"
		}
		mapping.Items = append(mapping.Items, item)
	}
	mapping.State = aggregate(mapping.Items)
	if err := mapping.Validate(); err != nil {
		return Mapping{}, err
	}
	return mapping, nil
}

func conflictingEvidence(job plans.JournalJob) bool {
	if response := job.LastResponse; response != nil {
		if job.RequestID != "" && response.RequestID != "" && job.RequestID != response.RequestID ||
			job.ProjectID != "" && response.ProjectID != "" && job.ProjectID != response.ProjectID ||
			job.PromotionID != "" && response.PromotionID != "" && job.PromotionID != response.PromotionID ||
			job.Status == "completed" && response.Code != nil && *response.Code != 0 {
			return true
		}
	}
	if reconciliation := job.Reconciliation; reconciliation != nil {
		if !validReconciliation(string(reconciliation.State)) || reconciliation.Candidates < 0 || reconciliation.Candidates > 1 ||
			job.Status == "completed" && reconciliation.State == plans.ReconciliationNotApplied {
			return true
		}
		if reconciliation.State == plans.ReconciliationApplied {
			if reconciliation.Candidates != 1 || !positiveID.MatchString(reconciliation.ObjectID) {
				return true
			}
			if (job.ProjectID != "" || job.PromotionID != "" || job.AdID != "") && reconciliation.ObjectID != job.ProjectID && reconciliation.ObjectID != job.PromotionID && reconciliation.ObjectID != job.AdID {
				return true
			}
		}
	}
	return false
}

func unknownEvidenceFields(raw json.RawMessage) bool {
	var job map[string]json.RawMessage
	if err := json.Unmarshal(raw, &job); err != nil {
		return true
	}
	for field, allowed := range map[string][]string{
		"last_response":  {"code", "message", "request_id", "project_id", "promotion_id"},
		"reconciliation": {"state", "object_id", "candidates", "reason"},
	} {
		if nested, exists := job[field]; exists {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(nested, &fields); err != nil || unknownFields(fields, allowed...) {
				return true
			}
		}
	}
	return false
}

func safeOrUnknown(value string) string {
	if !SafeID(value) {
		return Unknown
	}
	return value
}
func decimalOrUnknown(value string) string {
	if !positiveID.MatchString(value) {
		return Unknown
	}
	return value
}
func readID(raw map[string]json.RawMessage, key string) string {
	var value string
	if err := json.Unmarshal(raw[key], &value); err != nil {
		return Unknown
	}
	return safeOrUnknown(value)
}

func readSourceVersion(raw map[string]json.RawMessage) string {
	return readID(raw, "source_version")
}
func readSchema(raw map[string]json.RawMessage, key string) string {
	var value int
	if err := json.Unmarshal(raw[key], &value); err != nil || value <= 0 {
		return Unknown
	}
	return strconv.Itoa(value)
}
func unknownFields(raw map[string]json.RawMessage, allowed ...string) bool {
	keys := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		keys[key] = true
	}
	for key := range raw {
		if !keys[key] {
			return true
		}
	}
	return false
}

func checkJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	budget := 100000
	if err := checkValue(decoder, 0, &budget); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrUnsafeJournal
	}
	return nil
}

func checkValue(decoder *json.Decoder, depth int, budget *int) error {
	*budget--
	if depth > MaxDepth || *budget < 0 {
		return ErrUnsafeJournal
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrUnsafeJournal
	}
	switch value := token.(type) {
	case string:
		if len(value) > MaxStringBytes {
			return ErrUnsafeJournal
		}
	case json.Delim:
		switch value {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				text, ok := key.(string)
				if err != nil || !ok || len(text) > MaxStringBytes {
					return ErrUnsafeJournal
				}
				canonical := strings.Map(func(r rune) rune {
					minimum := r
					for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
						if folded < minimum {
							minimum = folded
						}
					}
					return minimum
				}, text)
				if seen[canonical] {
					return ErrUnsafeJournal
				}
				seen[canonical] = true
				if err := checkValue(decoder, depth+1, budget); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return ErrUnsafeJournal
			}
		case '[':
			for decoder.More() {
				if err := checkValue(decoder, depth+1, budget); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return ErrUnsafeJournal
			}
		default:
			return ErrUnsafeJournal
		}
	}
	return nil
}
