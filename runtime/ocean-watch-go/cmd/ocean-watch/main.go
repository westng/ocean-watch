package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/mcpserver"
)

var (
	gitCommit  = "UNSET"
	sdkVersion = "v1.1.92"
)

func main() {
	_ = gitCommit
	_ = sdkVersion
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if !mcpserver.IsServeCommand(os.Args[1:]) {
			fmt.Fprintln(os.Stderr, `{"timestamp":"","level":"error","status":"error","error_code":"INVALID_MCP_COMMAND"}`)
			os.Exit(2)
		}
		if err := mcpserver.RunManaged(ctx, cli.Version); err != nil {
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
