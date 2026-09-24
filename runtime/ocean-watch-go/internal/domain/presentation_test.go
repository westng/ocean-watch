package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedAccountEmptyPresentationMatchesGolden(t *testing.T) {
	presentation := ManagedAccountPresentation(nil, false)
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "presentation", "managed-accounts-empty.md"))
	if err != nil {
		t.Fatal(err)
	}
	if presentation.RenderedMarkdown+"\n" != string(want) {
		t.Fatalf("managed account presentation drifted:\n%s", presentation.RenderedMarkdown)
	}
	assertMandatoryPresentation(t, presentation, 5)
}

func TestManagedAccountSubjectSemantics(t *testing.T) {
	star := ManagedAccount{Channel: StarMap, AdvertiserID: "3001", Name: "星图账户", Enabled: true}
	if got := star.Subject(); got.ID != "3001" || got.Kind != "star_account" || got.Label != "星图账号 ID" {
		t.Fatalf("star account subject = %#v", got)
	}
	marketing := ManagedAccount{Channel: Marketing, AdvertiserID: "1001", Name: "营销账户", Enabled: true}
	if got := marketing.Subject(); got.Kind != "advertiser" || got.Label != "广告主 ID" {
		t.Fatalf("marketing account subject = %#v", got)
	}
	payload, err := json.Marshal(star)
	if err != nil || !containsJSONField(string(payload), `"subject_kind":"star_account"`) ||
		!containsJSONField(string(payload), `"subject_id":"3001"`) {
		t.Fatalf("managed account JSON lacks subject identity: %s", payload)
	}
}

func containsJSONField(payload, field string) bool { return strings.Contains(payload, field) }

func TestQianchuanBatchEmptyPresentationMatchesGolden(t *testing.T) {
	presentation := NewQianchuanBatchPresentation(nil, nil)
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "presentation", "qianchuan-batch-empty.md"))
	if err != nil {
		t.Fatal(err)
	}
	if presentation.RenderedMarkdown+"\n" != string(want) {
		t.Fatalf("Qianchuan presentation drifted:\n%s", presentation.RenderedMarkdown)
	}
	assertMandatoryPresentation(t, presentation, 5)
}

func TestMarkdownEscapesCells(t *testing.T) {
	rows := []map[string]any{{"value": "one|two\nthree"}}
	result := RenderMarkdownTable([]PresentationColumn{{Field: "value", Label: "值"}}, rows)
	if result != "| 值 |\n| --- |\n| one\\|two three |" {
		t.Fatalf("unexpected markdown: %s", result)
	}
}

func assertMandatoryPresentation(t *testing.T, presentation Presentation, columns int) {
	t.Helper()
	if !presentation.Required || presentation.AllowColumnOmission || presentation.AllowColumnReordering {
		t.Fatal("mandatory presentation controls are not fail-closed")
	}
	if len(presentation.Columns) != columns {
		t.Fatalf("got %d columns, want %d", len(presentation.Columns), columns)
	}
}
