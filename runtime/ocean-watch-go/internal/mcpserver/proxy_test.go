package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/runtimeupdate"
)

func TestProxySwitchesRuntimeInsideOneOuterSessionAndRejectsContractChanges(t *testing.T) {
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
		server := Runtime{}.NewServer(candidate.Version)
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

	installed = newSource
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := proxyRuntimeVersion(t, outer); got != newSource.Version {
		t.Fatalf("hot-switched runtime version = %q", got)
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

func assertProxyHostVersion(t *testing.T, hostRoot, version string) {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(hostRoot, ".codex-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plugin struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(payload, &plugin); err != nil || plugin.Version != version {
		t.Fatalf("Host version=%q want=%q err=%v", plugin.Version, version, err)
	}
}

func TestProxyLetsInflightCallFinishBeforeClosingRetiredRuntime(t *testing.T) {
	root := t.TempDir()
	oldSource := writeProxyCandidate(t, root, "source-old", "1.0.9+codex.old", "old")
	newSource := writeProxyCandidate(t, filepath.Join(root, "codex", "plugins", "cache", "ocean-watch", "ocean-watch"), "1.0.10+codex.new", "1.0.10+codex.new", "new")
	installed := oldSource
	manager := runtimeupdate.Manager{
		CodexRoot: filepath.Join(root, "codex"), PluginRoot: oldSource.PluginRoot,
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
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var oldSession *mcp.ClientSession
	proxy := &Proxy{Manager: manager, Connect: func(ctx context.Context, candidate runtimeupdate.Candidate) (*mcp.ClientSession, []*mcp.Tool, error) {
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		handler := func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if candidate.Version == oldSource.Version {
				close(oldStarted)
				select {
				case <-releaseOld:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return resultFor(capabilityOutput{OK: true, RuntimeVersion: candidate.Version, Commands: []capabilityItem{}}, false), nil
		}
		server := Runtime{Forward: handler}.NewServer(candidate.Version)
		if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
			return nil, nil, err
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "proxy-inflight-test", Version: "1"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			return nil, nil, err
		}
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		if candidate.Version == oldSource.Version {
			oldSession = session
		}
		return session, listed.Tools, nil
	}}
	defer proxy.Close()
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	outerServer := Runtime{Forward: proxy.Forward}.NewServer("stable")
	if _, err := outerServer.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	outerClient := mcp.NewClient(&mcp.Implementation{Name: "outer-inflight-test", Version: "1"}, nil)
	outer, err := outerClient.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()

	type callResult struct {
		version string
		err     error
	}
	oldResult := make(chan callResult, 1)
	go func() {
		result, callErr := outer.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "get_capabilities", Arguments: map[string]any{"channel": "shared"},
		})
		version := ""
		if callErr == nil && !result.IsError {
			version = decodeStructured[capabilityOutput](t, result).RuntimeVersion
		}
		oldResult <- callResult{version: version, err: callErr}
	}()
	<-oldStarted

	installed = newSource
	if _, err := proxy.ensureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := proxyRuntimeVersion(t, outer); got != newSource.Version {
		t.Fatalf("new calls did not use switched runtime: %q", got)
	}
	close(releaseOld)
	completed := <-oldResult
	if completed.err != nil || completed.version != oldSource.Version {
		t.Fatalf("in-flight call was interrupted: result=%#v", completed)
	}
	if _, err := oldSession.ListTools(context.Background(), nil); err == nil {
		t.Fatal("retired runtime remained open after its in-flight call completed")
	}
}

func TestInstalledVersionFingerprintTracksAtomicCacheReplacement(t *testing.T) {
	root := t.TempDir()
	oldRoot := filepath.Join(root, "1.0.9+codex.old")
	newRoot := filepath.Join(root, "1.0.9+codex.new")
	if err := os.Mkdir(oldRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	before := installedVersionFingerprint(root)
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	after := installedVersionFingerprint(root)
	if before == "" || after == "" || before == after {
		t.Fatalf("cache replacement was not observed: before=%q after=%q", before, after)
	}
}

func proxyRuntimeVersion(t *testing.T, session *mcp.ClientSession) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_capabilities", Arguments: map[string]any{"channel": "shared"},
	})
	if err != nil || result.IsError {
		t.Fatalf("get_capabilities failed: result=%#v err=%v", result, err)
	}
	metadata, ok := result.Meta["ocean_watch"].(map[string]any)
	if !ok || metadata["runtime_version"] == "" || metadata["proxy_duration_ms"] == nil {
		t.Fatalf("proxy metadata is missing: %#v", result.Meta)
	}
	return decodeStructured[capabilityOutput](t, result).RuntimeVersion
}

func writeProxyCandidate(t *testing.T, root, name, version, body string) runtimeupdate.Candidate {
	t.Helper()
	pluginRoot := filepath.Join(root, name)
	binaryPath := filepath.Join(pluginRoot, ".codex-plugin", "bin", "ocean-watch_linux_amd64")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	pluginPayload := []byte(`{"name":"ocean-watch","version":"` + version + `"}`)
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), pluginPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "f2"), 0o700); err != nil {
		t.Fatal(err)
	}
	resources := writeProxyHostResources(t, pluginRoot)
	manifest := runtimeupdate.Manifest{
		SchemaVersion: 1, Version: version,
		Plugin:    runtimeupdate.ManifestFile{Name: "ocean-watch", Version: version, SHA256: proxyHash(pluginPayload)},
		Resources: resources,
		SHA256:    map[string]string{"ocean-watch_linux_amd64": proxyHash([]byte(body))},
	}
	payload, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "runtime-manifest.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimeupdate.Candidate{Version: version, PluginRoot: pluginRoot, BinaryPath: binaryPath}
}

func writeProxyHostResources(t *testing.T, pluginRoot string) map[string]string {
	t.Helper()
	resources := map[string][]byte{
		".mcp.json":                        []byte(`{"mcpServers":{}}`),
		"f2/resolve.py":                    []byte("# fixture\n"),
		"bin/ocean-watch-launcher":         []byte("launcher"),
		"skills/ads-plan-monitor/SKILL.md": []byte("ads skill"),
		"skills/ads-plan-monitor/run":      []byte("ads run"),
		"skills/qc-plan-monitor/SKILL.md":  []byte("qc skill"),
		"skills/qc-plan-monitor/run":       []byte("qc run"),
	}
	hashes := make(map[string]string, len(resources))
	for relative, payload := range resources {
		path := filepath.Join(pluginRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o600)
		if relative == "bin/ocean-watch-launcher" || strings.HasSuffix(relative, "/run") {
			mode = 0o700
		}
		if err := os.WriteFile(path, payload, mode); err != nil {
			t.Fatal(err)
		}
		hashes[relative] = proxyHash(payload)
	}
	return hashes
}

func proxyHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func TestManagedRuntimeEnvironmentOverridesInheritedValue(t *testing.T) {
	values := managedRuntimeEnvironment([]string{"PATH=/bin", "OCEAN_WATCH_MANAGED_RUNTIME=0", "LANG=C"})
	want := []string{"PATH=/bin", "LANG=C", "OCEAN_WATCH_MANAGED_RUNTIME=1"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("unexpected managed Runtime environment: %#v", values)
	}
}
