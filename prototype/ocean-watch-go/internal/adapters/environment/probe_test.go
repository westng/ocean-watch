package environment

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/python"
)

type backendFixture string

func (backend backendFixture) BackendName() string { return string(backend) }

type listenerFixture struct{ closed bool }

func (*listenerFixture) Accept() (net.Conn, error) { return nil, errors.New("unused") }
func (listener *listenerFixture) Close() error     { listener.closed = true; return nil }
func (*listenerFixture) Addr() net.Addr            { return &net.TCPAddr{} }

func TestPythonCheckUsesResolvedLauncherRuntime(t *testing.T) {
	probe := Probe{PythonResolver: python.Resolver{
		Getenv:   func(string) string { return "/synthetic/python" },
		LookPath: func(name string) (string, error) { return name, nil },
		Run: func(context.Context, string, []string) (python.CommandResult, error) {
			return python.CommandResult{Stdout: "3.12.4"}, nil
		},
	}}
	check := probe.Python(context.Background())
	if check["status"] != "ready" || check["executable"] != "/synthetic/python" || check["version"] != "3.12.4" {
		t.Fatalf("unexpected Python check: %#v", check)
	}
	if check["minimum_version"] != "3.9" || check["available"] != nil || check["source"] != nil {
		t.Fatalf("Python check changed the public contract: %#v", check)
	}
}

func TestPlatformCheckMatchesPythonMachineAndReleaseShape(t *testing.T) {
	probe := Probe{
		GOOS: "darwin", GOARCH: "amd64",
		Run: func(_ context.Context, executable string, arguments []string) (python.CommandResult, error) {
			if executable != "/usr/bin/uname" || len(arguments) != 1 || arguments[0] != "-r" {
				t.Fatalf("unexpected release probe: %s %#v", executable, arguments)
			}
			return python.CommandResult{Stdout: "25.4.0\n"}, nil
		},
	}
	check := probe.Platform(context.Background())
	if check["machine"] != "x86_64" || check["release"] != "25.4.0" {
		t.Fatalf("unexpected platform check: %#v", check)
	}
}

func TestCredentialBackendClassification(t *testing.T) {
	for backend, status := range map[string]string{
		"macos-keychain": "ready", "file-fallback": "warning", "unavailable": "blocked",
	} {
		check := (Probe{Credentials: backendFixture(backend)}).CredentialBackend(context.Background())
		if check["status"] != status {
			t.Fatalf("backend %s = %#v", backend, check)
		}
	}
}

func TestCallbackRequiresLoopbackAndClosesProbe(t *testing.T) {
	listener := new(listenerFixture)
	var network, address string
	probe := Probe{Listen: func(gotNetwork, gotAddress string) (net.Listener, error) {
		network, address = gotNetwork, gotAddress
		return listener, nil
	}}
	check := probe.Callback(context.Background(), "http://127.0.0.1:8787/oauth/callback")
	if check["status"] != "ready" || network != "tcp4" || address != "127.0.0.1:8787" || !listener.closed {
		t.Fatalf("unexpected callback probe: %#v, %s, %s, closed=%v", check, network, address, listener.closed)
	}
	blocked := probe.Callback(context.Background(), "http://example.test:8787/oauth/callback")
	if blocked["status"] != "blocked" {
		t.Fatalf("non-loopback callback passed: %#v", blocked)
	}
}

func TestCodexVersionClassification(t *testing.T) {
	probe := Probe{
		LookPath: func(string) (string, error) { return "/synthetic/codex", nil },
		Run: func(context.Context, string, []string) (python.CommandResult, error) {
			return python.CommandResult{Stdout: "codex-cli 0.144.1"}, nil
		},
	}
	if check := probe.CodexCLI(context.Background()); check["status"] != "ready" || check["version"] != "0.144.1" {
		t.Fatalf("unexpected Codex check: %#v", check)
	}
}
