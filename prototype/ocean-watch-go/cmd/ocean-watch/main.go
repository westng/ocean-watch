package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/cli"
)

var (
	productVersion = cli.Version
	gitCommit      = "UNSET"
	sdkVersion     = "v1.1.92"
)

func main() {
	cli.Version = productVersion
	_ = gitCommit
	_ = sdkVersion
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	resolver := python.Resolver{}
	fallback := python.Fallback{
		Entrypoint: os.Getenv("OCEAN_WATCH_PYTHON_ENTRYPOINT"),
		Resolver:   resolver,
	}
	runner := cli.Runner{
		Routes: application.DefaultRouteManifest(), Fallback: fallback,
		PythonResolver: resolver,
	}
	os.Exit(runner.Execute(ctx, os.Args[1:]))
}
