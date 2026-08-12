package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli"
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
	runner := cli.Runner{
		Routes: application.DefaultRouteManifest(),
	}
	os.Exit(runner.Execute(ctx, os.Args[1:]))
}
