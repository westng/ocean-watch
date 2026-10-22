package legacy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixture(t *testing.T, transform func(map[string]any)) ([]byte, Source, []RecoveryContract) {
	t.Helper()
	value := map[string]any{
		"schema_version": 1, "fingerprint": "fixture-business-digest", "created_at": "2025-01-01T00:00:00Z",
		"source_version": "v-old", "runtime_build": "build-old", "runtime_digest": strings.Repeat("a", 64),
		"channel": "marketing", "operation_id": "source-operation", "jobs": map[string]any{
			"job-1": map[string]any{"status": "project_dispatching", "advertiser_id": "123", "request_id": "request-1", "dispatch_key": "dispatch-1", "dispatch_state": "unknown"},
		},
	}
	if transform != nil {
		transform(value)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{"root-1", "runs/source-1.json", Digest(payload)}
	contracts := []RecoveryContract{{"readonly-contract", "v-old", "build-old", strings.Repeat("a", 64), "1", "none", "core-tested"}}
	return payload, source, contracts
}

func TestDecodeClassificationAndFrozenMetadata(t *testing.T) {
	tests := []struct {
		name     string
		change   func(map[string]any)
		contract bool
		want     RecoveryState
		reason   string
	}{
		{"pending", nil, true, PendingReadOnly, "awaiting_read_only_reconciliation"},
		{"missing-source-version", func(v map[string]any) { delete(v, "source_version") }, true, QuarantinedUnknown, "metadata_unknown"},
		{"missing-operation-id", func(v map[string]any) { delete(v, "operation_id") }, true, QuarantinedUnknown, "metadata_unknown"},
		{"missing-schema", func(v map[string]any) { delete(v, "schema_version") }, true, QuarantinedUnknown, "metadata_unknown"},
		{"missing-build", func(v map[string]any) { delete(v, "runtime_build") }, true, QuarantinedUnknown, "metadata_unknown"},
		{"missing-digest", func(v map[string]any) { delete(v, "runtime_digest") }, true, QuarantinedUnknown, "metadata_unknown"},
		{"no-contract", nil, false, QuarantinedUnknown, "runtime_incompatible"},
		{"incompatible-runtime", func(v map[string]any) { v["source_version"] = "unverified" }, true, QuarantinedUnknown, "runtime_incompatible"},
		{"unknownRequestID", func(v map[string]any) { delete(job(v), "request_id") }, true, QuarantinedUnknown, "unknown_request_id"},
		{"no-scope", func(v map[string]any) { delete(v, "channel") }, true, QuarantinedUnknown, "scope_unknown"},
		{"no-advertiser", func(v map[string]any) { delete(job(v), "advertiser_id") }, true, QuarantinedUnknown, "scope_unknown"},
		{"unknown-status", func(v map[string]any) { job(v)["status"] = "online_safe" }, true, QuarantinedUnknown, "ambiguous_evidence"},
		{"unknown-fields", func(v map[string]any) { v["unrecognized"] = true }, true, QuarantinedUnknown, "unknown_fields"},
		{"closed", func(v map[string]any) {
			j := job(v)
			j["status"] = "completed"
			j["promotion_id"] = "456"
			j["dispatch_state"] = "acknowledged"
		}, true, HistoryReadOnly, "closed_history"},
		{"closed-missing-source-version", func(v map[string]any) {
			delete(v, "source_version")
			j := job(v)
			j["status"] = "completed"
			j["promotion_id"] = "456"
			j["dispatch_state"] = "acknowledged"
		}, true, QuarantinedUnknown, "metadata_unknown"},
		{"closed-no-contract", func(v map[string]any) {
			j := job(v)
			j["status"] = "completed"
			j["promotion_id"] = "456"
			j["dispatch_state"] = "acknowledged"
		}, false, QuarantinedUnknown, "runtime_incompatible"},
		{"ambiguous-official-facts", func(v map[string]any) {
			job(v)["reconciliation"] = map[string]any{"state": "ambiguous", "candidates": 2}
		}, true, QuarantinedUnknown, "ambiguous_evidence"},
		{"closed-ambiguous", func(v map[string]any) { j := job(v); j["status"] = "completed"; j["promotion_id"] = "456" }, true, PendingReadOnly, "awaiting_read_only_reconciliation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, source, contracts := fixture(t, tt.change)
			if !tt.contract {
				contracts = nil
			}
			mapping, err := Decode(source, payload, contracts)
			if err != nil {
				t.Fatal(err)
			}
			if mapping.State != tt.want || mapping.Items[0].Reason != tt.reason {
				t.Fatalf("state=%s reason=%s", mapping.State, mapping.Items[0].Reason)
			}
			if tt.name == "missing-source-version" && mapping.RuntimeVersion != Unknown {
				t.Fatal("inferred current runtime")
			}
			if tt.name == "missing-schema" && mapping.JournalSchema != Unknown {
				t.Fatal("inferred source schema")
			}
		})
	}
}

func job(value map[string]any) map[string]any {
	return value["jobs"].(map[string]any)["job-1"].(map[string]any)
}

func TestDecodeClosedConflictsRemainQuarantined(t *testing.T) {
	cases := map[string]struct {
		change func(map[string]any)
		reason string
	}{
		"unknown-request":  {func(v map[string]any) { delete(job(v), "request_id") }, "unknown_request_id"},
		"request-conflict": {func(v map[string]any) { job(v)["last_response"] = map[string]any{"request_id": "different-request"} }, "ambiguous_evidence"},
		"object-conflict":  {func(v map[string]any) { job(v)["last_response"] = map[string]any{"promotion_id": "789"} }, "ambiguous_evidence"},
		"official-error":   {func(v map[string]any) { job(v)["last_response"] = map[string]any{"code": 1} }, "ambiguous_evidence"},
		"not-applied": {func(v map[string]any) {
			job(v)["reconciliation"] = map[string]any{"state": "not_applied", "candidates": 0}
		}, "ambiguous_evidence"},
		"invalid-reconciliation": {func(v map[string]any) { job(v)["reconciliation"] = map[string]any{"state": "unsafe", "candidates": 1} }, "ambiguous_evidence"},
		"multiple-candidates": {func(v map[string]any) {
			job(v)["reconciliation"] = map[string]any{"state": "applied", "object_id": "456", "candidates": 2}
		}, "ambiguous_evidence"},
		"reconciled-object-conflict": {func(v map[string]any) {
			job(v)["reconciliation"] = map[string]any{"state": "applied", "object_id": "789", "candidates": 1}
		}, "ambiguous_evidence"},
		"unknown-response-field": {func(v map[string]any) { job(v)["last_response"] = map[string]any{"unrecognized": true} }, "unknown_fields"},
		"unknown-reconciliation-field": {func(v map[string]any) {
			job(v)["reconciliation"] = map[string]any{"state": "applied", "object_id": "456", "candidates": 1, "unrecognized": true}
		}, "unknown_fields"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			payload, source, contracts := fixture(t, func(v map[string]any) {
				job(v)["status"], job(v)["dispatch_state"], job(v)["promotion_id"] = "completed", "acknowledged", "456"
				test.change(v)
			})
			mapping, err := Decode(source, payload, contracts)
			if err != nil || mapping.State != QuarantinedUnknown || mapping.Items[0].Reason != test.reason || len(mapping.Blockers()) == 0 {
				t.Fatalf("state=%s items=%+v error=%v", mapping.State, mapping.Items, err)
			}
		})
	}
}

func TestDecodeDoesNotInferSourceMetadata(t *testing.T) {
	payload, source, contracts := fixture(t, func(v map[string]any) {
		delete(v, "source_version")
		v["runtime_version"] = "v-old"
	})
	mapping, err := Decode(source, payload, contracts)
	if err != nil || mapping.RuntimeVersion != Unknown || mapping.State != QuarantinedUnknown {
		t.Fatalf("runtime was inferred: %+v error=%v", mapping, err)
	}
	for _, schema := range []any{nil, 3} {
		payload, source, contracts := fixture(t, func(v map[string]any) {
			snapshot := map[string]any{}
			if schema != nil {
				snapshot["schema_version"] = schema
			}
			v["snapshot"] = snapshot
		})
		mapping, err := Decode(source, payload, contracts)
		if err != nil || mapping.State != QuarantinedUnknown || mapping.ContractID != Unknown {
			t.Fatalf("snapshot was inferred: %+v error=%v", mapping, err)
		}
	}
}

func TestDecodeRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	valid, _, _ := fixture(t, nil)
	cases := map[string][]byte{
		"duplicate":          []byte(`{"jobs":{},"jobs":{}}`),
		"escaped-duplicate":  []byte(`{"jobs":{},"j\u006fbs":{}}`),
		"unicode-alias":      []byte(`{"jobs":{"j":{"status":"pending","\u017ftatus":"completed"}}}`),
		"nested-alias":       []byte(`{"jobs":{"j":{"status":"pending","Status":"completed"}}}`),
		"response-duplicate": []byte(`{"jobs":{"j":{"last_response":{"request_id":"a","request_id":"b"}}}}`),
		"alias-duplicate":    []byte(`{"jobs":{},"Jobs":{}}`),
		"nested-duplicate":   []byte(`{"jobs":{"j":{"status":"pending","status":"completed"}}}`),
		"trailing":           append(append([]byte{}, valid...), []byte(` {}`)...),
		"garbage":            append(append([]byte{}, valid...), 'x'),
		"oversize":           []byte(strings.Repeat(" ", MaxJournalBytes+1)),
		"long-string":        []byte(`{"fingerprint":"` + strings.Repeat("a", MaxStringBytes+1) + `"}`),
		"deep":               []byte(strings.Repeat("[", MaxDepth+2) + "0" + strings.Repeat("]", MaxDepth+2)),
		"invalid-utf8":       []byte{'"', 0xff, '"'},
		"null":               []byte("null"),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			source := Source{"root-1", "runs/source-1.json", Digest(payload)}
			if _, err := Decode(source, payload, nil); !errors.Is(err, ErrUnsafeJournal) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestDecodeWhitelistAndDeterministicIdentity(t *testing.T) {
	payload, source, contracts := fixture(t, func(v map[string]any) {
		v["access_token"] = "SYNTHETIC_SECRET"
		v["approval"] = map[string]any{"confirmed": true}
		job(v)["payload"] = map[string]any{"url": "https://synthetic.invalid/private", "token": "SYNTHETIC_SECRET"}
		job(v)["last_response"] = map[string]any{"request_id": "request-1", "message": "OFFICIAL_RAW_RESPONSE"}
	})
	first, err := Decode(source, payload, contracts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Decode(source, payload, contracts)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(first)
	other, _ := json.Marshal(second)
	if string(encoded) != string(other) || first.Version != 1 || first.TargetSchema != TargetSchemaVersion {
		t.Fatal("mapping not deterministic")
	}
	for _, forbidden := range []string{"SYNTHETIC_SECRET", "https:", "OFFICIAL_RAW_RESPONSE", "confirmed", "payload", "approval"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("persistable mapping contains %q", forbidden)
		}
	}
	changed := source
	changed.Checksum = strings.Repeat("b", 64)
	if _, err := Decode(changed, payload, contracts); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("checksum mismatch: %v", err)
	}
	if changed.OperationID() != source.OperationID() || changed.MigrationID() == source.MigrationID() {
		t.Fatal("source identity depends on mutable checksum")
	}
}

func TestQuarantineDuplicateDoesNotMutateSource(t *testing.T) {
	payload, source, contracts := fixture(t, nil)
	mapping, err := Decode(source, payload, contracts)
	if err != nil {
		t.Fatal(err)
	}
	quarantined := QuarantineDuplicate(mapping)
	if mapping.State != PendingReadOnly || mapping.Items[0].State != PendingReadOnly || quarantined.State != QuarantinedUnknown {
		t.Fatal("duplicate quarantine mutated caller evidence")
	}
}

func TestSourcePathsAndConservativeBlockers(t *testing.T) {
	for _, path := range []string{"../source.json", "/runs/source.json", "runs/../source.json", `runs\source.json`, "runs/a/b.json", "runs/a.json/other", "runs/.json", "runs/a.json:secret", "runs/a.json\n"} {
		if ValidRecordID(path) {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	blocker := Blocker{Scope: Scope{"marketing", "123"}}
	if !blocker.Blocks(Scope{"marketing", "123"}) || blocker.Blocks(Scope{"marketing", "456"}) || blocker.Blocks(Scope{"qianchuan", "123"}) || !blocker.Blocks(Scope{}) {
		t.Fatal("wrong conservative scope overlap")
	}
	if !(Blocker{Scope: Scope{Unknown, Unknown}}).Blocks(Scope{"qianchuan", "999"}) {
		t.Fatal("unknown scope must block root")
	}
	if !(Blocker{Scope: Scope{"marketing", "123"}}).Blocks(Scope{}) {
		t.Fatal("unknown requested scope must block")
	}
}
