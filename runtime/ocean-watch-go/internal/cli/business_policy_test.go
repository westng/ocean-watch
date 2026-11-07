package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
)

func executeFixturePolicy(t *testing.T) *policy.Instance {
	t.Helper()
	instance, err := policy.New(policy.Config{Mode: "execute", Toolsets: []string{"core", "marketing", "qianchuan", "reports"}})
	if err != nil {
		t.Fatal(err)
	}
	return &instance
}

func TestPreviewBlocksLegacyBusinessWritesBeforeRuntimePreparation(t *testing.T) {
	stdout := new(bytes.Buffer)
	runner := Runner{Policy: executeFixturePolicy(t), RuntimeVersion: "1.1.0-alpha.1", Routes: application.DefaultRouteManifest(), Stdout: stdout, Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" }}
	for _, args := range [][]string{{"plans", "batch-qianchuan-works", "--submit", "--preflight-id", "fixture"}, {"plans", "update-budget", "--submit"}, {"qc-plans", "update-status", "--submit"}, {"plans", "update-budget", "-submit"}, {"qc-plans", "update-status", "-submit=true"}} {
		stdout.Reset()
		if runner.Execute(context.Background(), args) != 2 || !strings.Contains(stdout.String(), "preview_write_unavailable") {
			t.Fatalf("legacy write escaped preview: %s", stdout)
		}
	}
}

func TestCLIPlanModeAllowsPreviewButDeniesSubmissionAndForeignAccounts(t *testing.T) {
	instance, _ := policy.New(policy.Config{Mode: "plan", Toolsets: []string{"core", "marketing", "qianchuan", "reports"}, AllowedSubjects: map[string]map[string][]string{"marketing": {"advertiser": {"1001"}}}})
	stdout := new(bytes.Buffer)
	runner := Runner{Policy: &instance, Routes: application.DefaultRouteManifest(), Stdout: stdout, Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" }}
	for _, args := range [][]string{{"plans", "update-budget", "--advertiser-id", "1001", "--promotion-id", "2001", "--value", "30", "--submit"}, {"plans", "update-budget", "--advertiser-id", "999", "--promotion-id", "2001", "--value", "30"}} {
		stdout.Reset()
		if runner.Execute(context.Background(), args) != 2 || !strings.Contains(stdout.String(), "policy_denied") {
			t.Fatal("CLI execution or local scope escaped startup policy")
		}
	}
}
