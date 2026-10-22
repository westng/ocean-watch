package cli

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/environment"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

func TestDoctorJSONHasNoStateCredentialOrNetworkSideEffects(t *testing.T) {
	for _, inspectState := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_state_inspector", true: "with_state_inspector"}[inspectState], func(t *testing.T) {
			root := t.TempDir()
			stateRoot := filepath.Join(root, "shared-state")
			credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
			authorizations := &runnerAuthorizationReader{}
			stdout := new(bytes.Buffer)
			probe := environment.Probe{
				GOOS: "darwin", GOARCH: "arm64", Credentials: credentials,
				SystemRelease: func(context.Context, string) string { return "synthetic" },
				LookPath:      func(string) (string, error) { return "", errors.New("not installed") },
				PythonResolver: python.Resolver{
					Getenv:   func(string) string { return "" },
					LookPath: func(string) (string, error) { return "", errors.New("not installed") },
					Run: func(context.Context, string, []string) (python.CommandResult, error) {
						t.Fatal("missing Python must not run an executable")
						return python.CommandResult{}, errors.New("unexpected command")
					},
				},
				Listen: func(string, string) (net.Listener, error) {
					t.Fatal("doctor must not listen")
					return nil, errors.New("unexpected listener")
				},
			}
			observed := false
			runner := Runner{
				Routes: application.DefaultRouteManifest(), Stdout: stdout,
				Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
				Credentials: credentials, Authorizations: authorizations, EnvironmentProbe: probe,
				RequestObserver: func(command string, budget requestcontrol.BudgetSnapshot, metrics requestcontrol.MetricsSnapshot) {
					observed = true
					if command != "setup doctor" || budget != (requestcontrol.BudgetSnapshot{}) || metrics != (requestcontrol.MetricsSnapshot{}) {
						t.Fatalf("unexpected request use: %s %#v %#v", command, budget, metrics)
					}
				},
			}
			if inspectState {
				runner.StateInspector = sqlite.Inspector{Path: filepath.Join(stateRoot, "ocean-watch.db")}
			}
			if code := runner.Execute(context.Background(), []string{"setup", "doctor", "--json"}); code != 0 {
				t.Fatalf("doctor exit %d: %s", code, stdout.String())
			}
			result := decodeSingleJSONObject(t, stdout.Bytes())
			if result["ok"] != true || !observed {
				t.Fatalf("unexpected report: %#v", result)
			}
			foundState := false
			for _, value := range result["checks"].([]any) {
				check := value.(map[string]any)
				if check["id"] == "state" {
					foundState = true
					if check["status"] != "warning" || check["inspection"].(map[string]any)["status"] != "not_initialized" {
						t.Fatalf("uninitialized state misclassified: %#v", check)
					}
				}
			}
			if foundState != inspectState {
				t.Fatal("state inspector injection was not preserved")
			}
			if len(credentials.reads) != 0 || credentials.writes != 0 || len(authorizations.reads) != 0 {
				t.Fatal("doctor accessed credential or authorization contents")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("doctor created files: %v", entries)
			}
		})
	}
}
