package operations

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func confirmationSnapshot(t *testing.T, payload string) Snapshot {
	t.Helper()
	digest, err := Digest([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return Snapshot{OperationID: "op_confirmation", Version: 1, Kind: "qianchuan_batch", Channel: "qianchuan",
		SubjectID: "9007199254740993123", RuntimeVersion: "test", SchemaVersion: SchemaVersion,
		InputDigest: digest, SnapshotDigest: digest, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Payload: []byte(payload)}
}

func confirmationPayload() string {
	return `{"items":[{"item_id":"item-1","group_id":"group-1","action":"update","target_id":"9007199254740993123","payload_digest":"sha256:` + strings.Repeat("1", 64) + `","expected_digest":"sha256:` + strings.Repeat("2", 64) + `","changes":[{"field":"日预算（元）","before":"100.00","after":"200.00"}]}]}`
}

func TestConfirmationSummaryBindsCompletePersistedReview(t *testing.T) {
	snapshot := confirmationSnapshot(t, confirmationPayload())
	summary, err := NewConfirmationSummary(snapshot, snapshot.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	text, err := summary.ReviewText()
	if err != nil || !strings.Contains(text, "100.00 → 200.00") || !strings.Contains(text, snapshot.SubjectID) || !strings.Contains(text, "+08:00") {
		t.Fatalf("review lost business facts: %q, %v", text, err)
	}
	before, _ := summary.Canonical()
	summary.Items[0].Changes[0].After = "300.00"
	after, _ := summary.Canonical()
	if string(before) == string(after) {
		t.Fatal("business change was not included in canonical document")
	}
}

func TestConfirmationRejectsIncompleteAmbiguousAndHiddenReviews(t *testing.T) {
	base := confirmationPayload()
	for name, payload := range map[string]string{
		"legacy journal": `{"jobs":{}}`,
		"no changes":     strings.ReplaceAll(base, `[{"field":"日预算（元）","before":"100.00","after":"200.00"}]`, `[]`),
		"unknown action": strings.ReplaceAll(base, `"update"`, `"auto"`),
		"hidden text":    strings.ReplaceAll(base, `日预算（元）`, `预算\u202e隐藏`),
		"unchanged":      strings.ReplaceAll(base, `200.00`, `100.00`),
		"overlong value": strings.ReplaceAll(base, `200.00`, strings.Repeat("x", 513)),
		"duplicate item": strings.ReplaceAll(base, `}]}`, `},`+strings.TrimSuffix(strings.TrimPrefix(base, `{"items":[`), `]}`)+`]}`),
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := confirmationSnapshot(t, payload)
			if _, err := NewConfirmationSummary(snapshot, snapshot.CreatedAt.Add(time.Minute)); !errors.Is(err, ErrConfirmationSummary) {
				t.Fatalf("unsafe review accepted: %v", err)
			}
		})
	}
	snapshot := confirmationSnapshot(t, base)
	if _, err := NewConfirmationSummary(snapshot, snapshot.ExpiresAt.Add(time.Second)); !errors.Is(err, ErrConfirmationSummary) {
		t.Fatalf("approval outlived snapshot: %v", err)
	}
}

func TestConfirmationDeleteRiskAndNoTruncation(t *testing.T) {
	snapshot := confirmationSnapshot(t, strings.ReplaceAll(confirmationPayload(), `"update"`, `"delete"`))
	summary, err := NewConfirmationSummary(snapshot, snapshot.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := summary.ReviewText()
	if !strings.Contains(text, "风险：删除操作") {
		t.Fatal("deletion risk was omitted")
	}
	for index := 1; index < 100; index++ {
		item := summary.Items[0]
		item.ItemID = "item-" + strings.Repeat("x", index)
		summary.Items = append(summary.Items, item)
	}
	if _, err := summary.Canonical(); !errors.Is(err, ErrConfirmationSummary) {
		t.Fatalf("oversized document was truncated/accepted: %v", err)
	}
	// The wire format preserves large identifiers as strings.
	encoded, _ := json.Marshal(snapshot)
	if !strings.Contains(string(encoded), snapshot.SubjectID) {
		t.Fatal("large subject ID lost")
	}
}
