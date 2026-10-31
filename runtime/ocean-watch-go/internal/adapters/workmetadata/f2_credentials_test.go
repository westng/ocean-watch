package workmetadata

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pythonruntime "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func TestF2ResolverPassesSystemCookieOnlyThroughStdin(t *testing.T) {
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python is not available for the subprocess fixture")
	}
	directory := t.TempDir()
	entrypoint := filepath.Join(directory, "fixture.py")
	// This process stands in for F2. It cannot access a real credential store or network.
	script := `import json, os, sys
assert "--credentials-stdin" in sys.argv
assert not any("fixture-secret" in value for value in sys.argv)
assert "OCEAN_WATCH_F2_DOUYIN_COOKIE" not in os.environ
assert json.load(sys.stdin) == {"cookie": "sessionid=fixture-secret; token=a=b+==;"}
print(json.dumps({"results": {"7001": {"code": 200, "data": {"author": {"uid": "9001", "unique_id": "creator", "nickname": "fixture"}, "video": {"video_info_id": "7001"}}}}, "errors": {}}))
`
	if err := os.WriteFile(entrypoint, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCEAN_WATCH_F2_DOUYIN_COOKIE", "sessionid=stale-environment")
	resolver := F2Resolver{Entrypoint: entrypoint, Directory: directory, Cookie: func(context.Context) (string, error) { return "sessionid=fixture-secret; token=a=b+==;", nil },
		Python: pythonruntime.Resolver{Getenv: func(string) string { return pythonPath }, LookPath: func(string) (string, error) { return pythonPath, nil }, Run: func(_ context.Context, _ string, args []string) (pythonruntime.CommandResult, error) {
			version := "3.12.0"
			if strings.Contains(strings.Join(args, " "), "importlib.metadata") {
				version = pythonruntime.RequiredF2Version
			}
			return pythonruntime.CommandResult{Stdout: version}, nil
		}},
	}
	result, err := resolver.ResolveMany(context.Background(), []string{"7001"}, 1)
	if err != nil || result.Rows["7001"].AwemeID != "9001" {
		t.Fatalf("credential subprocess failed: %v", err)
	}
}

func TestF2ResolverMissingSystemCookieStopsBeforePython(t *testing.T) {
	called := false
	resolver := F2Resolver{Cookie: func(context.Context) (string, error) {
		return "", domain.NewWorkLinkError("f2_cookie_missing", "configure F2")
	}, Python: pythonruntime.Resolver{Run: func(context.Context, string, []string) (pythonruntime.CommandResult, error) {
		called = true
		return pythonruntime.CommandResult{}, errors.New("must not run")
	}}}
	_, err := resolver.ResolveMany(context.Background(), []string{"7001"}, 1)
	var linkErr *domain.WorkLinkError
	if !errors.As(err, &linkErr) || linkErr.Code != "f2_cookie_missing" || called {
		t.Fatal("missing credentials launched Python")
	}
}
