package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type forbiddenPromptInput struct{ t *testing.T }

func (r forbiddenPromptInput) Read([]byte) (int, error) {
	r.t.Fatal("structured creation read interactive stdin")
	return 0, nil
}

func TestTemplateJSONEntryReturnsOneDocumentWithoutPrompts(t *testing.T) {
	for _, input := range []string{`{}`, `{"channel":"qianchuan"}`, `{"channel":"marketing"}`} {
		output := new(bytes.Buffer)
		code := RunTemplatesInteractive(context.Background(), "create", []string{"--input-json", input}, nil, nil, "/synthetic/config.json", forbiddenPromptInput{t}, output)
		var result map[string]any
		if code != 0 || json.Unmarshal(output.Bytes(), &result) != nil || result["status"] != "needs_input" || result["changed"] != false {
			t.Fatalf("entry emitted a prompt/non-JSON response: %s", output)
		}
	}
}

func TestTemplateJSONEntryRejectsAmbiguousOrUnsupportedInput(t *testing.T) {
	for _, input := range []string{`null`, `{"channel":"marketing","channel":"qianchuan"}`, `{"secret":"synthetic-hidden"}`, `{} {}`, `{"channel":"qianchuan","product":{"template_id":"injected"}}`} {
		output := new(bytes.Buffer)
		code := RunTemplatesInteractive(context.Background(), "create", []string{"--input-json", input}, nil, nil, "/synthetic/config.json", forbiddenPromptInput{t}, output)
		var result map[string]any
		if code != 2 || json.Unmarshal(output.Bytes(), &result) != nil || result["ok"] != false || strings.Contains(output.String(), "synthetic-hidden") {
			t.Fatalf("invalid input was not safely rejected: %s", output)
		}
	}
}

func TestTemplateJSONEntryRejectsNumericIDsAndHiddenProductFields(t *testing.T) {
	for _, input := range []string{`{"channel":"qianchuan","template_kind":"product","product":{"product_ids":[123]}}`, `{"channel":"marketing","marketing":{"material_source_type":"ACCOUNT_UPLOAD","product_info":{"secret":"synthetic-hidden"}}}`} {
		output := new(bytes.Buffer)
		code := RunTemplatesInteractive(context.Background(), "create", []string{"--input-json", input}, nil, nil, "/synthetic/config.json", forbiddenPromptInput{t}, output)
		if code != 2 || strings.Contains(output.String(), "synthetic-hidden") {
			t.Fatalf("unsafe structured field accepted or leaked: %s", output)
		}
	}
}
