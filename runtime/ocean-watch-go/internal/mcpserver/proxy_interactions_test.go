package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/runtimeupdate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pinnedRecoveryFixture struct {
	recoveryFixture
	saved auth.RecoverySession
}

func (f *pinnedRecoveryFixture) Start(ctx context.Context, input auth.RecoveryRequest) (auth.RecoverySession, error) {
	view, err := f.recoveryFixture.Start(ctx, input)
	view.ExpiresAt = time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	f.saved = view
	return view, err
}
func (f *pinnedRecoveryFixture) Get(ctx context.Context, channel, id string) (auth.RecoverySession, error) {
	if f.saved.SessionID == id && f.saved.Channel == channel {
		return f.saved, nil
	}
	return f.recoveryFixture.Get(ctx, channel, id)
}
func (f *pinnedRecoveryFixture) Cancel(ctx context.Context, channel, id string) (auth.RecoverySession, error) {
	view, err := f.Get(ctx, channel, id)
	view.Status = "cancelled"
	view.AuthorizationURL = ""
	f.saved = view
	return view, err
}

func TestProxyInteractionPinsHaveBoundedLifetimeAndCoverF2(t *testing.T) {
	now := time.Now().UTC()
	current := &proxySession{}
	proxy := &Proxy{current: current, Now: func() time.Time { return now }}
	for _, tool := range []string{"start_authorization", "start_account_sync", "start_f2_configuration"} {
		view := map[string]any{"session_id": "fixture", "status": "awaiting_user", "expires_at": now.Add(time.Minute).Format(time.RFC3339)}
		proxy.trackInteraction(current, tool, resultFor(map[string]any{"session": view}, false))
		if !proxy.mustRetainCurrentLocked() {
			t.Fatalf("%s did not pin the runtime", tool)
		}
		now = now.Add(3 * time.Minute)
		if proxy.mustRetainCurrentLocked() {
			t.Fatal("expired interaction pinned runtime forever")
		}
	}
	current.openingInteractions = 1
	if !proxy.mustRetainCurrentLocked() {
		t.Fatal("an in-flight session start can be switched before its handle is returned")
	}
}
func TestProxyKeepsAuthorizationRuntimeUntilCancellation(t *testing.T) {
	root := t.TempDir()
	oldSource := writeProxyCandidate(t, root, "source-old", "1.0.9+codex.old", "old")
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", "ocean-watch", "ocean-watch")
	newSource := writeProxyCandidate(t, cacheRoot, "1.0.10+codex.new", "1.0.10+codex.new", "new")
	installed := oldSource
	hostRoot := filepath.Join(cacheRoot, "1.0.9+codex.host")
	manager := runtimeupdate.Manager{
		CodexRoot: filepath.Join(root, "codex"), PluginRoot: hostRoot,
		GOOS: "linux", GOARCH: "amd64", Now: func() time.Time { return time.Unix(1, 0) },
		AlwaysDiscover: true,
		Discover: func() (string, string, error) {
			return installed.PluginRoot, installed.Version, nil
		},
		ProbeVersion: func(_ context.Context, path string) ([]byte, error) {
			payload, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), "runtime-manifest.json"))
			if err != nil {
				return nil, err
			}
			var manifest runtimeupdate.Manifest
			if err := json.Unmarshal(payload, &manifest); err != nil {
				return nil, err
			}
			return []byte("ocean-watch " + strings.SplitN(manifest.Version, "+", 2)[0]), nil
		},
	}
	badContract := false
	logs := new(bytes.Buffer)
	proxy := &Proxy{Manager: manager, LogWriter: logs, Connect: func(ctx context.Context, candidate runtimeupdate.Candidate) (*mcp.ClientSession, []*mcp.Tool, error) {
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		server := Runtime{AuthorizationRecovery: &pinnedRecoveryFixture{}}.NewServer(candidate.Version)
		if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
			return nil, nil, err
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			return nil, nil, err
		}
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		if badContract {
			listed.Tools = listed.Tools[:len(listed.Tools)-1]
		}
		return session, listed.Tools, nil
	}}
	defer proxy.Close()
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertProxyHostVersion(t, hostRoot, oldSource.Version)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	outerServer := Runtime{Forward: proxy.Forward}.NewServer("stable")
	if _, err := outerServer.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	outerClient := mcp.NewClient(&mcp.Implementation{Name: "outer-test", Version: "1"}, nil)
	outer, err := outerClient.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	if got := proxyRuntimeVersion(t, outer); got != oldSource.Version {
		t.Fatalf("initial runtime version = %q", got)
	}

	callTestTool(t, outer, "start_authorization", map[string]any{"channel": "qianchuan"})
	installed = newSource
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := proxyRuntimeVersion(t, outer); got != oldSource.Version {
		t.Fatalf("pending authorization lost its runtime: %s", got)
	}
	callTestTool(t, outer, "get_authorization_session", map[string]any{"channel": "qianchuan", "session_id": strings.Repeat("a", 48)})
	callTestTool(t, outer, "cancel_authorization_session", map[string]any{"channel": "qianchuan", "session_id": strings.Repeat("a", 48)})
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := proxyRuntimeVersion(t, outer); got != newSource.Version {
		t.Fatalf("terminal session pinned runtime forever: %s", got)
	}
	assertProxyHostVersion(t, hostRoot, newSource.Version)

	badSource := writeProxyCandidate(t, cacheRoot, "1.0.11+codex.bad", "1.0.11+codex.bad", "bad")
	installed = badSource
	badContract = true
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := proxyRuntimeVersion(t, outer); got != newSource.Version {
		t.Fatalf("incompatible runtime replaced the active implementation: %q", got)
	}
	assertProxyHostVersion(t, hostRoot, badSource.Version)
	state, err := manager.Status()
	if err != nil || state.Current.Version != badSource.Version ||
		state.RejectedSHA256 != "" {
		t.Fatalf("new-task Host version was incorrectly rejected: state=%#v err=%v", state, err)
	}
	for _, phase := range []string{"runtime_initialized", "runtime_switched", "host_contract_deferred", "business_tool"} {
		if !strings.Contains(logs.String(), `"phase":"`+phase+`"`) {
			t.Fatalf("proxy timing log is missing %s: %s", phase, logs.String())
		}
	}
}
