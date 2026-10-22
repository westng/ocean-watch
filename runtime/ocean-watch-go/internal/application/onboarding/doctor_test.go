package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type probeFixture struct {
	checks []Check
	index  int
}

func (probe *probeFixture) next() Check {
	result := probe.checks[probe.index]
	probe.index++
	return result
}

func (probe *probeFixture) Python(context.Context) Check            { return probe.next() }
func (probe *probeFixture) F2(context.Context) Check                { return probe.next() }
func (probe *probeFixture) Platform(context.Context) Check          { return probe.next() }
func (probe *probeFixture) CodexCLI(context.Context) Check          { return probe.next() }
func (probe *probeFixture) CredentialBackend(context.Context) Check { return probe.next() }
func (probe *probeFixture) Callback(context.Context, string) Check  { return probe.next() }

func TestDoctorSeparatesBlockersAndWarnings(t *testing.T) {
	probe := &probeFixture{checks: []Check{
		{"id": "python", "required": true, "status": "ready"},
		{"id": "f2", "required": true, "status": "ready"},
		{"id": "platform", "required": true, "status": "ready"},
		{"id": "codex_cli", "required": false, "status": "warning"},
		{"id": "credential_backend", "required": true, "status": "blocked"},
		{"id": "oauth_callback", "required": true, "status": "ready"},
	}}
	report := (Doctor{Probe: probe}).Report(context.Background(), "marketing", "http://127.0.0.1:8787/oauth/callback")
	if report.OK || len(report.BlockingChecks) != 1 || report.BlockingChecks[0] != "credential_backend" {
		t.Fatalf("unexpected blockers: %#v", report)
	}
	if len(report.Warnings) != 1 || report.Warnings[0] != "codex_cli" || report.NextAction != "resolve_blocking_checks" {
		t.Fatalf("unexpected warnings: %#v", report)
	}
}

type inspectorFixture struct {
	inspection state.Inspection
	err        error
}

func (fixture inspectorFixture) Inspect(context.Context) (state.Inspection, error) {
	return fixture.inspection, fixture.err
}

func TestDoctorStateInspectionAndEvidence(t *testing.T) {
	for _, test := range []struct {
		status  string
		err     error
		blocked bool
	}{
		{status: "not_initialized"}, {status: "unverified"}, {status: "unknown"},
		{status: "ready"}, {status: "blocked", blocked: true}, {status: "error", blocked: true},
		{err: errors.New("private-path SQL diagnostic-marker"), blocked: true},
	} {
		t.Run(test.status, func(t *testing.T) {
			probe := &probeFixture{checks: []Check{
				{"id": "python", "required": false, "status": "warning"},
				{"id": "f2", "required": false, "status": "warning"},
				{"id": "platform", "required": true, "status": "ready"},
				{"id": "codex_cli", "required": false, "status": "warning"},
				{"id": "credential_backend", "required": true, "status": "ready"},
				{"id": "oauth_callback", "required": true, "status": "ready"},
			}}
			report := (Doctor{Probe: probe, StateInspector: inspectorFixture{
				inspection: state.Inspection{Status: test.status, Evidence: "fixture_metadata"}, err: test.err,
			}}).Report(context.Background(), "qianchuan", "http://127.0.0.1:8787/oauth/callback")
			if report.OK == test.blocked || (len(report.BlockingChecks) != 0) != test.blocked {
				t.Fatalf("unexpected readiness: %#v", report)
			}
			for _, check := range report.Checks {
				if check["check_id"] != check["id"] || check["evidence"] == "" {
					t.Fatalf("missing stable check ID or evidence: %#v", check)
				}
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "diagnostic-marker") || strings.Contains(string(encoded), "private-path") {
				t.Fatal("state inspector error leaked internal details")
			}
		})
	}
}

func TestDoctorReportsRuntimeVersion(t *testing.T) {
	probe := &probeFixture{checks: []Check{
		{"id": "python", "required": true, "status": "ready"},
		{"id": "f2", "required": true, "status": "ready"},
		{"id": "platform", "required": true, "status": "ready"},
		{"id": "codex_cli", "required": false, "status": "ready"},
		{"id": "credential_backend", "required": true, "status": "ready"},
		{"id": "oauth_callback", "required": true, "status": "ready"},
	}}
	report := (Doctor{Probe: probe, RuntimeVersion: "1.0.9+codex.test"}).Report(
		context.Background(), "marketing", "http://127.0.0.1:8787/oauth/callback",
	)
	if len(report.Checks) != 7 {
		t.Fatalf("runtime check missing: %#v", report.Checks)
	}
	check := report.Checks[len(report.Checks)-1]
	if check["id"] != "runtime" || check["version"] != "1.0.9+codex.test" {
		t.Fatalf("unexpected runtime check: %#v", check)
	}
}
