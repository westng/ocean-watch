package onboarding

import (
	"context"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type Check map[string]any

type EnvironmentProbe interface {
	Python(context.Context) Check
	F2(context.Context) Check
	Platform(context.Context) Check
	CodexCLI(context.Context) Check
	CredentialBackend(context.Context) Check
	Callback(context.Context, string) Check
}

type EnvironmentReport struct {
	OK             bool     `json:"ok"`
	Mode           string   `json:"mode"`
	Channel        string   `json:"channel"`
	Checks         []Check  `json:"checks"`
	BlockingChecks []string `json:"blocking_checks"`
	Warnings       []string `json:"warnings"`
	NextAction     string   `json:"next_action"`
}

type Doctor struct {
	Probe          EnvironmentProbe
	RuntimeVersion string
	RuntimeCheck   func(context.Context) Check
	StateInspector state.Inspector
}

func (doctor Doctor) Report(ctx context.Context, channel, redirectURI string) EnvironmentReport {
	checks := []Check{
		doctor.Probe.Python(ctx),
		doctor.Probe.F2(ctx),
		doctor.Probe.Platform(ctx),
		doctor.Probe.CodexCLI(ctx),
		doctor.Probe.CredentialBackend(ctx),
		doctor.Probe.Callback(ctx, redirectURI),
	}
	if version := strings.TrimSpace(doctor.RuntimeVersion); version != "" {
		check := Check{
			"id": "runtime", "required": false, "status": "ready", "version": version,
			"message":     "This command is running on the reported Ocean Watch Runtime.",
			"remediation": nil,
		}
		if doctor.RuntimeCheck != nil {
			check = doctor.RuntimeCheck(ctx)
			check["version"] = version
		}
		checks = append(checks, check)
	}
	if doctor.StateInspector != nil {
		checks = append(checks, doctor.inspectState(ctx))
	}
	blockers := []string{}
	warnings := []string{}
	for _, check := range checks {
		identifier, _ := check["id"].(string)
		check["check_id"] = identifier
		if _, exists := check["evidence"]; !exists {
			check["evidence"] = map[string]string{
				"python": "python_version_probe", "f2": "f2_metadata_version_probe",
				"platform": "runtime_platform", "codex_cli": "cli_version_probe_not_host_discovery",
				"credential_backend": "backend_metadata_only", "oauth_callback": "callback_configuration_only",
				"runtime": "local_runtime_metadata_not_host_discovery",
			}[identifier]
		}
		status, _ := check["status"].(string)
		required, _ := check["required"].(bool)
		if required && (status == "blocked" || status == "error") {
			blockers = append(blockers, identifier)
		}
		if status == "warning" {
			warnings = append(warnings, identifier)
		}
	}
	nextAction := "ready"
	if len(blockers) != 0 {
		nextAction = "resolve_blocking_checks"
	}
	return EnvironmentReport{
		OK: len(blockers) == 0, Mode: "environment_check", Channel: channel,
		Checks: checks, BlockingChecks: blockers, Warnings: warnings, NextAction: nextAction,
	}
}

func (doctor Doctor) inspectState(ctx context.Context) Check {
	inspection, err := doctor.StateInspector.Inspect(ctx)
	check := Check{
		"id": "state", "required": false, "status": "warning",
		"evidence": "state_not_verified", "message": "State was not fully verified.",
		"remediation": "Validate state explicitly before using persisted operations.",
	}
	if err != nil {
		check["required"], check["status"] = true, "error"
		check["evidence"] = "state_inspection_failed"
		return check
	}
	check["inspection"] = inspection
	check["evidence"] = inspection.Evidence
	switch inspection.Status {
	case "ready":
		check["status"], check["message"], check["remediation"] = "ready", "State inspection passed.", nil
	case "not_initialized":
		check["message"] = "State is not initialized; unrelated read capabilities remain available."
	case "blocked", "error":
		check["required"], check["status"] = true, inspection.Status
		check["message"] = "State inspection requires explicit remediation."
	}
	return check
}
