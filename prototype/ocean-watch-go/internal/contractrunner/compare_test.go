package contractrunner

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProcessResultCanonicalizesJSONAndPresentation(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	stdout := `{
  "count": 1.50,
  "path": "` + workspace + `/config.json",
  "presentation": {"rendered_markdown": "line  \r\n"}
}`
	result, err := normalizeProcessResult(
		CaseSpec{Normalizers: []string{"trim-trailing-space"}},
		workspace,
		0,
		false,
		stdout,
		"warning\r\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.StdoutKind != "json" || result.Stderr != "warning\n" {
		t.Fatalf("unexpected normalized result: %#v", result)
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(result.StdoutJSON)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value["count"].(json.Number).String() != "1.50" {
		t.Fatalf("decimal precision changed: %s", result.StdoutJSON)
	}
	if value["path"] != "{{WORKSPACE}}/config.json" || result.Presentation != "line\n" {
		t.Fatalf("dynamic values were not normalized: %#v", result)
	}
}

func TestCompareResultsReportsEveryBoundary(t *testing.T) {
	expected := CaseResult{
		ExitCode: 0, StdoutKind: "json", StdoutJSON: json.RawMessage(`{"ok":true}`),
		Stderr: "", Presentation: "expected", BeforeFiles: map[string]FileState{},
		AfterFiles: map[string]FileState{"config.json": {Kind: "file", SHA256: "a"}},
	}
	actual := CaseResult{
		ExitCode: 2, TimedOut: true, StdoutKind: "json", StdoutJSON: json.RawMessage(`{"ok":false}`),
		Stderr: "error", Presentation: "actual", BeforeFiles: map[string]FileState{"extra": {Kind: "file"}},
		AfterFiles: map[string]FileState{"config.json": {Kind: "file", SHA256: "b"}},
	}
	differences := compareResults(expected, actual)
	fields := map[string]bool{}
	for _, difference := range differences {
		fields[difference.Field] = true
	}
	for _, field := range []string{"exit_code", "timed_out", "stdout_json", "stderr", "presentation", "before_files", "after_files"} {
		if !fields[field] {
			t.Fatalf("missing difference for %s: %#v", field, differences)
		}
	}
}

func TestNormalizeFileCanonicalizesJSONAndLockMetadata(t *testing.T) {
	first := normalizeFile("config.json", []byte("{\n  \"b\": 2, \"a\": 1\n}\n"), nil)
	second := normalizeFile("config.json", []byte(`{"a":1,"b":2}`), nil)
	if string(first) != string(second) {
		t.Fatalf("JSON files were not canonicalized: %s != %s", first, second)
	}
	lock := normalizeFile("config.json.lock", []byte(`{"pid":123,"nonce":"random","future":true}`), nil)
	if string(lock) != `{"future":true,"nonce":"{{NONCE}}","pid":"{{PID}}"}` {
		t.Fatalf("lock metadata was not normalized: %s", lock)
	}
}

func TestNormalizeQianchuanTemplateIDsAcrossTextAndJSONKeys(t *testing.T) {
	normalizers := []string{"qianchuan-template-ids"}
	text := normalizeText(
		"qcpt_0123456789ab qclt_abcdef123456 qcpt_example",
		"",
		normalizers,
	)
	if text != "qcpt_{{DYNAMIC_ID}} qclt_{{DYNAMIC_ID}} qcpt_example" {
		t.Fatalf("unexpected normalized text: %s", text)
	}
	first := normalizeFile(
		"config.json",
		[]byte(`{"qianchuan_product_templates":{"qcpt_0123456789ab":{"template_id":"qcpt_0123456789ab"}}}`),
		normalizers,
	)
	second := normalizeFile(
		"config.json",
		[]byte(`{"qianchuan_product_templates":{"qcpt_abcdef123456":{"template_id":"qcpt_abcdef123456"}}}`),
		normalizers,
	)
	if string(first) != string(second) {
		t.Fatalf("dynamic template IDs were not normalized: %s != %s", first, second)
	}
}

func TestNormalizeWorkspaceResolvesMacOSTemporaryDirectoryAlias(t *testing.T) {
	workspace, err := os.MkdirTemp("/tmp", "ocean-watch-contract-alias-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil || canonical == workspace {
		t.Skip("temporary directory alias is unavailable")
	}
	got := normalizeText(filepath.Join(canonical, "config.json"), workspace, nil)
	if got != "{{WORKSPACE}}/config.json" {
		t.Fatalf("temporary directory alias was not normalized: %s", got)
	}
}

func TestNormalizeLauncherCommandAllowsApprovedGoEntrypointEvolution(t *testing.T) {
	pythonCommand := `"/repo/.venv/bin/python" "/repo/skills/ads-plan-monitor/run.py" setup doctor --config "/tmp/config.json"`
	goCommand := `ocean-watch setup doctor --config "/tmp/config.json"`
	pythonNormalized := normalizeText(pythonCommand, "", []string{"launcher-command"})
	goNormalized := normalizeText(goCommand, "", []string{"launcher-command"})
	if pythonNormalized != goNormalized || pythonNormalized != `{{OCEAN_WATCH_COMMAND}} setup doctor --config "/tmp/config.json"` {
		t.Fatalf("launcher commands differ: %q != %q", pythonNormalized, goNormalized)
	}
}

func TestWriteJUnitProducesMachineReadableFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit", "report.xml")
	report := ComparisonReport{
		Total: 2, Passed: 1, Failed: 1,
		Cases: []ComparedCase{
			{ID: "pass", Category: "help", Passed: true},
			{ID: "fail", Category: "behavior", Differences: []Difference{{Field: "exit_code", Expected: "0", Actual: "2"}}},
		},
	}
	if err := writeJUnit(path, report); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var suite junitSuite
	if err := xml.Unmarshal(payload, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Tests != 2 || suite.Failures != 1 || suite.Cases[1].Failure == nil {
		t.Fatalf("unexpected JUnit payload: %#v", suite)
	}
}

func TestValidateEvidenceRejectsCredentials(t *testing.T) {
	for _, payload := range []string{
		`Authorization: Bearer real_credential_value_12345`,
		`{"refresh_token":"real_credential_value_12345"}`,
		`{"app_secret":"real_credential_value_12345"}`,
		`{"url":"https://example.test/mcp/session/private"}`,
	} {
		if err := validateEvidence([]byte(payload)); err == nil {
			t.Fatalf("credential-like evidence was accepted: %s", payload)
		}
	}
	for _, payload := range evidenceAllowlist {
		if err := validateEvidence([]byte(`{"access_token":"` + payload + `"}`)); err != nil {
			t.Fatalf("fixture credential was rejected: %v", err)
		}
	}
}

func TestValidateGitSHARejectsMissingUppercaseAndPlaceholderValues(t *testing.T) {
	if err := validateGitSHA("a123456789012345678901234567890123456789"); err != nil {
		t.Fatalf("valid git SHA was rejected: %v", err)
	}
	for _, value := range []string{"", "A123456789012345678901234567890123456789", strings.Repeat("0", 40), "abc"} {
		if err := validateGitSHA(value); err == nil {
			t.Fatalf("invalid git SHA was accepted: %q", value)
		}
	}
}

func TestReadCaptureRequiresCommitIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "capture.json")
	payload := `{"schema_version":1,"kind":"python-baseline","manifest_sha256":"abc","command_count":0,"cases":[]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCapture(path); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("capture without commit identity was accepted: %v", err)
	}
}
