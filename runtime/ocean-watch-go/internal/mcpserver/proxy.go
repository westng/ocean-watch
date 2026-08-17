package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/runtimeupdate"
)

type proxySession struct {
	candidate runtimeupdate.Candidate
	session   *mcp.ClientSession
	lease     *filesystem.FileLock
	active    int
	retired   bool
}

var errHostContractChanged = errors.New("installed Ocean Watch runtime changed the stable MCP tool contract")

type Proxy struct {
	Manager   runtimeupdate.Manager
	Connect   func(context.Context, runtimeupdate.Candidate) (*mcp.ClientSession, []*mcp.Tool, error)
	LogWriter io.Writer
	Now       func() time.Time

	mu       sync.Mutex
	current  *proxySession
	expected []byte
}

func RunProxy(ctx context.Context, version, pluginRoot string) error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	proxy := &Proxy{Manager: runtimeupdate.Manager{
		CodexRoot: filesystem.CodexHome(os.Getenv, userHome), PluginRoot: filepath.Clean(pluginRoot),
		AlwaysDiscover: true,
	}, LogWriter: os.Stderr}
	defer proxy.Close()
	if _, err := proxy.ensureSession(ctx); err != nil {
		return err
	}
	go proxy.monitor(ctx)
	server := Runtime{Forward: proxy.Forward}.NewServer(version)
	err = server.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (proxy *Proxy) Forward(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := proxy.now()
	proxy.mu.Lock()
	current := proxy.current
	if current != nil {
		current.active++
	}
	proxy.mu.Unlock()
	if current == nil {
		return nil, errors.New("Ocean Watch business runtime is unavailable")
	}
	params := request.Params
	result, err := current.session.CallTool(ctx, &mcp.CallToolParams{
		Meta: params.Meta, Name: params.Name, Arguments: json.RawMessage(params.Arguments),
		InputResponses: params.InputResponses, RequestState: params.RequestState,
	})
	duration := proxy.now().Sub(started).Milliseconds()
	status := "ok"
	if err != nil || result != nil && result.IsError {
		status = "error"
	}
	proxy.log("business_tool", status, duration, current.candidate.Version, current.candidate.Version)
	if result != nil {
		if result.Meta == nil {
			result.Meta = mcp.Meta{}
		}
		result.Meta["ocean_watch"] = map[string]any{
			"proxy_duration_ms": duration,
			"runtime_version":   current.candidate.Version,
		}
	}
	proxy.releaseSession(current)
	return result, err
}

func (proxy *Proxy) ensureSession(ctx context.Context) (*mcp.ClientSession, error) {
	started := proxy.now()
	candidate, lease, err := proxy.Manager.ResolveLeased(ctx)
	if err != nil {
		return nil, err
	}
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	previousVersion := ""
	if proxy.current != nil {
		previousVersion = proxy.current.candidate.Version
	}
	if proxy.current != nil && proxy.current.candidate.SHA256 == candidate.SHA256 {
		_ = lease.Release()
		return proxy.current.session, nil
	}
	session, err := proxy.connectValidated(ctx, candidate, lease, true)
	if err != nil {
		if errors.Is(err, errHostContractChanged) && proxy.current != nil {
			proxy.log("host_contract_deferred", "new_task", proxy.now().Sub(started).Milliseconds(), previousVersion, candidate.Version)
			return proxy.current.session, nil
		}
		rejectedVersion := candidate.Version
		restored, restoreErr := proxy.Manager.Reject(ctx, candidate)
		if restoreErr != nil {
			return nil, errors.Join(err, fmt.Errorf("restore previous Ocean Watch runtime: %w", restoreErr))
		}
		if proxy.current != nil && proxy.current.candidate.SHA256 == restored.SHA256 {
			proxy.log("runtime_rejected", "rollback", proxy.now().Sub(started).Milliseconds(), rejectedVersion, restored.Version)
			return proxy.current.session, nil
		}
		candidate, lease, err = proxy.Manager.ResolveLeased(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve restored Ocean Watch runtime: %w", err)
		}
		if candidate.SHA256 != restored.SHA256 {
			_ = lease.Release()
			return nil, errors.New("restored Ocean Watch runtime identity changed")
		}
		session, err = proxy.connectValidated(ctx, candidate, lease, false)
		if err != nil {
			return nil, fmt.Errorf("start restored Ocean Watch runtime: %w", err)
		}
		candidate = restored
		proxy.log("runtime_rejected", "rollback", proxy.now().Sub(started).Milliseconds(), rejectedVersion, restored.Version)
	}
	if proxy.current != nil {
		proxy.current.retired = true
		if proxy.current.active == 0 {
			_ = proxy.current.session.Close()
			_ = proxy.current.lease.Release()
		}
	}
	proxy.current = &proxySession{candidate: candidate, session: session, lease: lease}
	phase := "runtime_initialized"
	if previousVersion != "" {
		phase = "runtime_switched"
	}
	proxy.log(phase, "ok", proxy.now().Sub(started).Milliseconds(), previousVersion, candidate.Version)
	_ = proxy.Manager.PruneObsoleteRuntimes(ctx, candidate)
	return session, nil
}

func (proxy *Proxy) connectValidated(ctx context.Context, candidate runtimeupdate.Candidate, lease *filesystem.FileLock, publishHost bool) (*mcp.ClientSession, error) {
	connect := proxy.Connect
	if connect == nil {
		connect = connectRuntime
	}
	session, tools, err := connect(ctx, candidate)
	if err != nil {
		_ = lease.Release()
		return nil, err
	}
	catalog, err := json.Marshal(tools)
	if err != nil {
		_ = session.Close()
		_ = lease.Release()
		return nil, err
	}
	if proxy.expected == nil {
		proxy.expected, err = stableToolCatalog(ctx)
		if err != nil {
			_ = session.Close()
			_ = lease.Release()
			return nil, err
		}
	}
	if publishHost {
		if err := proxy.Manager.PreserveDeletedHostRoot(ctx, candidate); err != nil {
			_ = session.Close()
			_ = lease.Release()
			return nil, err
		}
	}
	if string(catalog) != string(proxy.expected) {
		_ = session.Close()
		_ = lease.Release()
		return nil, errHostContractChanged
	}
	return session, nil
}

func (proxy *Proxy) monitor(ctx context.Context) {
	cacheRoot := proxy.Manager.InstalledCacheRoot()
	lastFingerprint := installedVersionFingerprint(cacheRoot)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fingerprint := installedVersionFingerprint(cacheRoot)
			if fingerprint == "" || fingerprint == lastFingerprint {
				continue
			}
			lastFingerprint = fingerprint
			if _, err := proxy.ensureSession(ctx); err != nil {
				proxy.log("runtime_monitor", "error", 0, "", "")
			}
		}
	}
}

func (proxy *Proxy) now() time.Time {
	if proxy.Now != nil {
		return proxy.Now()
	}
	return time.Now().UTC()
}

func (proxy *Proxy) log(phase, status string, durationMS int64, fromVersion, toVersion string) {
	if proxy.LogWriter == nil {
		return
	}
	record := struct {
		Timestamp   string `json:"timestamp"`
		Level       string `json:"level"`
		Phase       string `json:"phase"`
		Status      string `json:"status"`
		DurationMS  int64  `json:"duration_ms"`
		FromVersion string `json:"from_version,omitempty"`
		ToVersion   string `json:"to_version,omitempty"`
	}{
		Timestamp: proxy.now().Format(time.RFC3339Nano), Level: "info", Phase: phase,
		Status: status, DurationMS: durationMS, FromVersion: fromVersion, ToVersion: toVersion,
	}
	_ = json.NewEncoder(proxy.LogWriter).Encode(record)
}

func installedVersionFingerprint(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	return strings.Join(versions, "\x00")
}

func (proxy *Proxy) Close() error {
	proxy.mu.Lock()
	current := proxy.current
	proxy.current = nil
	proxy.mu.Unlock()
	var result error
	if current != nil {
		result = errors.Join(result, current.session.Close())
		result = errors.Join(result, current.lease.Release())
		result = errors.Join(result, proxy.Manager.PruneObsoleteRuntimes(context.Background(), current.candidate))
	}
	return result
}

func (proxy *Proxy) releaseSession(session *proxySession) {
	proxy.mu.Lock()
	if session.active > 0 {
		session.active--
	}
	cleanup := session.retired && session.active == 0
	current := proxy.current
	proxy.mu.Unlock()
	if cleanup {
		_ = session.session.Close()
		_ = session.lease.Release()
		if current != nil {
			_ = proxy.Manager.PruneObsoleteRuntimes(context.Background(), current.candidate)
		}
	}
}

func connectRuntime(ctx context.Context, candidate runtimeupdate.Candidate) (*mcp.ClientSession, []*mcp.Tool, error) {
	command := exec.Command(candidate.BinaryPath, "mcp", "serve", "--stdio")
	command.Dir = candidate.PluginRoot
	command.Env = managedRuntimeEnvironment(os.Environ())
	command.Stderr = os.Stderr
	client := mcp.NewClient(
		&mcp.Implementation{Name: "ocean-watch-stable-proxy", Version: candidate.Version},
		&mcp.ClientOptions{Logger: slog.New(slog.DiscardHandler), Capabilities: &mcp.ClientCapabilities{}},
	)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("start Ocean Watch business runtime: %w", err)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return nil, nil, fmt.Errorf("inspect Ocean Watch business runtime tools: %w", err)
	}
	return session, listed.Tools, nil
}

func managedRuntimeEnvironment(values []string) []string {
	prefix := filesystem.ManagedRuntimeEnv + "="
	result := make([]string, 0, len(values)+1)
	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	return append(result, prefix+"1")
}

func stableToolCatalog(ctx context.Context) ([]byte, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := Runtime{Forward: func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("stable catalog handler must not run")
	}}.NewServer("stable")
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "ocean-watch-catalog", Version: "stable"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return json.Marshal(listed.Tools)
}
