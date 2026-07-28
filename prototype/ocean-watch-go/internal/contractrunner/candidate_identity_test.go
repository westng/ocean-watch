package contractrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validCandidateIdentity() CandidateIdentity {
	return CandidateIdentity{
		SchemaVersion:            1,
		GitSHA:                   strings.Repeat("a", 40),
		ProductVersion:           "0.9.1",
		PluginVersion:            "0.9.1+codex.test",
		SDKVersion:               officialSDKVersion,
		SourceTreeSHA256:         strings.Repeat("1", 64),
		CandidateChecksumsSHA256: strings.Repeat("2", 64),
		ReleasePublicKeySHA256:   strings.Repeat("3", 64),
		Release:                  true,
	}
}

func writeCandidateIdentity(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "candidate-identity.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCandidateIdentityAcceptsDirectAndVerificationDocuments(t *testing.T) {
	want := validCandidateIdentity()
	for name, document := range map[string]any{
		"direct":       want,
		"verification": map[string]any{"status": "passed", "candidate_identity": want},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := LoadCandidateIdentity(writeCandidateIdentity(t, document), want.GitSHA)
			if err != nil {
				t.Fatal(err)
			}
			if *got != want {
				t.Fatalf("got %#v, want %#v", *got, want)
			}
		})
	}
}

func TestLoadCandidateIdentityFailsClosed(t *testing.T) {
	identity := validCandidateIdentity()
	for name, mutate := range map[string]func(map[string]any){
		"missing":      func(value map[string]any) { delete(value, "candidate_checksums_sha256") },
		"unexpected":   func(value map[string]any) { value["unsealed"] = true },
		"placeholder":  func(value map[string]any) { value["source_tree_sha256"] = strings.Repeat("0", 64) },
		"wrong commit": func(value map[string]any) { value["git_sha"] = strings.Repeat("b", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(identity)
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(payload, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			if _, err := LoadCandidateIdentity(writeCandidateIdentity(t, value), identity.GitSHA); err == nil {
				t.Fatal("malformed candidate identity was accepted")
			}
		})
	}
}

func TestComparisonReportSerializesCandidateIdentity(t *testing.T) {
	identity := validCandidateIdentity()
	payload, err := json.Marshal(ComparisonReport{CandidateIdentity: &identity})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"candidate_identity":{"schema_version":1`) {
		t.Fatalf("candidate identity was not sealed in report: %s", payload)
	}
}
