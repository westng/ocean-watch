package onboarding

import (
	"context"
	"testing"
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
