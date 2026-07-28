package environment

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/python"
)

const officialMCPServer = "oceanengine-developer-docs"

type MCPProbe struct {
	LookPath func(string) (string, error)
	Run      func(context.Context, string, []string) (python.CommandResult, error)
}

func (probe MCPProbe) Status(ctx context.Context, hasAppID, hasDeveloperID bool) map[string]any {
	lookPath := probe.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	executable, err := lookPath("codex")
	available := err == nil
	registered := false
	usesBridge := false
	if available {
		result, runErr := probe.run(ctx, executable, []string{"mcp", "get", officialMCPServer, "--json"})
		if runErr == nil && result.ExitCode == 0 {
			var payload map[string]any
			if json.Unmarshal([]byte(result.Stdout), &payload) == nil && payload != nil {
				registered = true
				usesBridge = isCompatibleBridge(payload)
			}
		}
	}
	ready := registered && usesBridge && hasAppID && hasDeveloperID
	nextAction := "register_mcp"
	if ready {
		nextAction = "ready"
	} else if !hasAppID {
		nextAction = "save_app_credentials"
	} else if !hasDeveloperID {
		nextAction = "configure_developer_id"
	}
	return map[string]any{
		"server": officialMCPServer, "codex_cli_available": available,
		"has_app_id": hasAppID, "has_developer_id": hasDeveloperID,
		"registered": registered, "uses_sse_bridge": usesBridge,
		"ready": ready, "next_action": nextAction,
	}
}

func (probe MCPProbe) run(ctx context.Context, executable string, arguments []string) (python.CommandResult, error) {
	if probe.Run != nil {
		return probe.Run(ctx, executable, arguments)
	}
	return (Probe{}).run(ctx, executable, arguments)
}

func isCompatibleBridge(server map[string]any) bool {
	transport, _ := server["transport"].(map[string]any)
	if transport == nil || transport["type"] != "stdio" {
		return false
	}
	arguments, _ := transport["args"].([]any)
	if len(arguments) != 1 {
		return false
	}
	argument, ok := arguments[0].(string)
	if !ok {
		return false
	}
	return strings.EqualFold(filepath.Base(argument), "oceanengine_mcp_bridge.py")
}
