package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/mcpserver"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/runtimeupdate"
)

var (
	gitCommit      = "UNSET"
	sdkVersion     = "v1.1.92"
	runtimeVersion = "UNSET"
)

func main() {
	_ = gitCommit
	_ = sdkVersion
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "runtime" {
		os.Exit(runRuntimeCommand(ctx, os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if len(os.Args) == 6 && os.Args[2] == "proxy" && os.Args[3] == "--stdio" && os.Args[4] == "--plugin-root" {
			if err := mcpserver.RunProxy(ctx, effectiveRuntimeVersion(), os.Args[5]); err != nil {
				fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"MCP_PROXY_FAILED"}`)
				os.Exit(1)
			}
			return
		}
		if !mcpserver.IsServeCommand(os.Args[1:]) {
			fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"INVALID_MCP_COMMAND"}`)
			os.Exit(2)
		}
		if os.Getenv(filesystem.ManagedRuntimeEnv) != "1" {
			pluginRoot, err := os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"MCP_PROXY_FAILED"}`)
				os.Exit(1)
			}
			if err := mcpserver.RunProxy(ctx, effectiveRuntimeVersion(), pluginRoot); err != nil {
				fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"MCP_PROXY_FAILED"}`)
				os.Exit(1)
			}
			return
		}
		if err := mcpserver.RunManaged(ctx, effectiveRuntimeVersion()); err != nil {
			fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"MCP_SERVER_FAILED"}`)
			os.Exit(1)
		}
		return
	}
	runner := cli.Runner{
		Routes: application.DefaultRouteManifest(),
	}
	os.Exit(runner.Execute(ctx, os.Args[1:]))
}

func effectiveRuntimeVersion() string {
	if runtimeVersion == "" || runtimeVersion == "UNSET" {
		return cli.Version
	}
	return runtimeVersion
}

func runRuntimeCommand(ctx context.Context, arguments []string) int {
	pluginRoot := ""
	filtered := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "--plugin-root" {
			if index+1 >= len(arguments) {
				fmt.Fprintln(os.Stderr, "runtime --plugin-root requires a path")
				return 2
			}
			pluginRoot = arguments[index+1]
			index++
			continue
		}
		filtered = append(filtered, arguments[index])
	}
	arguments = filtered
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	manager := runtimeupdate.Manager{
		CodexRoot: filesystem.CodexHome(os.Getenv, userHome), PluginRoot: filepath.Clean(pluginRoot),
	}
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, "runtime command requires resolve, status, or exec")
		return 2
	}
	switch arguments[0] {
	case "resolve":
		if len(arguments) != 1 {
			fmt.Fprintln(os.Stderr, "runtime resolve accepts no arguments")
			return 2
		}
		candidate, resolveErr := manager.Resolve(ctx)
		if resolveErr != nil {
			fmt.Fprintln(os.Stderr, resolveErr)
			return 1
		}
		fmt.Fprintln(os.Stdout, candidate.BinaryPath)
	case "status":
		if len(arguments) != 1 {
			fmt.Fprintln(os.Stderr, "runtime status accepts no arguments")
			return 2
		}
		state, statusErr := manager.Status()
		if statusErr != nil {
			fmt.Fprintln(os.Stderr, statusErr)
			return 1
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(state); err != nil {
			return 1
		}
	case "exec":
		if len(arguments) < 2 || arguments[1] != "--" {
			fmt.Fprintln(os.Stderr, "runtime exec requires -- before the child arguments")
			return 2
		}
		candidate, lease, resolveErr := manager.ResolveLeased(ctx)
		if resolveErr != nil {
			fmt.Fprintln(os.Stderr, resolveErr)
			return 1
		}
		defer lease.Release()
		command := exec.CommandContext(ctx, candidate.BinaryPath, arguments[2:]...)
		command.Dir = candidate.PluginRoot
		command.Env = runtimeChildEnvironment(os.Environ())
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				return exitError.ExitCode()
			}
			if ctx.Err() != nil {
				return 130
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown runtime command")
		return 2
	}
	return 0
}

func runtimeChildEnvironment(values []string) []string {
	prefix := filesystem.ManagedRuntimeEnv + "="
	result := make([]string, 0, len(values)+1)
	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	return append(result, prefix+"1")
}
