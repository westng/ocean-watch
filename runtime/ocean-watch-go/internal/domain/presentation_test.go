package domain

import (
	"os"
	"path/filepath"
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
	assertMandatoryPresentation(t, presentation, 4)
}

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
