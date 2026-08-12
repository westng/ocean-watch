package onboarding

import "context"

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
	Probe EnvironmentProbe
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
	blockers := []string{}
	warnings := []string{}
	for _, check := range checks {
		identifier, _ := check["id"].(string)
		status, _ := check["status"].(string)
		required, _ := check["required"].(bool)
		if required && status == "blocked" {
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
