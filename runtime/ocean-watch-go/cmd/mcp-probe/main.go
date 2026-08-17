package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fixture = `{
  "plan_template_schema_version": 6,
  "default_plan_template": {"defaults":{"daily_budget":300,"roi_goal":1.5,"product_info":{"product_image_type":"DPA","product_image_fields":[]}}},
  "plan_templates": {
    "marketing-probe": {
      "display_name":"marketing-probe",
      "bindings":{"channel":"marketing","advertiser_id":"1000000000000001","platform":"抖音","traffic_source":"CID","product_id":"3001","product_name":"探针商品"},
      "material_strategy":{"source_type":"ACCOUNT_UPLOAD","selection_mode":"MANUAL","max_materials_per_unit":5},
      "copy_materials":{"titles":[]},"overrides":{}
    }
  },
  "qianchuan_product_template_schema_version": 8,
  "qianchuan_product_templates": {
    "qcpt_probe": {
      "template_id":"qcpt_probe","display_name":"千川探针模板","template_type":"QIANCHUAN_PRODUCT_ALL_DOMAIN","status":"active",
      "bindings":{"channel":"qianchuan","advertiser_id":"2000000000000001","product_name":"探针商品","product_short_name":"探针","product_ids":["8000000000000001"]},
      "plan_name_template":"{product_name}-{creator_name}-{datetime}",
      "delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.7,"qcpx_mode":"QCPX_MODE_ON","budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW","deep_external_action":"AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"},
      "material_strategy":{"source_type":"CREATOR_RUNTIME_QUERY","persist_material_ids":false}
    }
  },
  "qianchuan_live_template_schema_version": 1,
  "qianchuan_live_templates": {}
}`

type stringValues []string

func (values *stringValues) String() string {
	return strings.Join(*values, ",")
}

func (values *stringValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	binaryFlag := flag.String("binary", "", "existing Ocean Watch binary to probe")
	proxyRoot := flag.String("proxy-root", "", "start the stable proxy for this plugin root")
	codexHome := flag.String("codex-home", "", "use an existing Codex home for a runtime-switch probe")
	expectInitialRuntime := flag.String("expect-initial-runtime", "", "require this initial Runtime version")
	waitRuntime := flag.String("wait-runtime", "", "wait for this Runtime version in the same MCP session")
	waitTimeout := flag.Duration("wait-timeout", 30*time.Second, "maximum runtime-switch wait")
	preflightTemplate := flag.String("preflight-template", "", "call one read-only Qianchuan preflight with this template")
	preflightWorkURLs := stringValues{}
	flag.Var(&preflightWorkURLs, "preflight-work-url", "repeatable work row for the read-only Qianchuan preflight")
	flag.Parse()
	if (*preflightTemplate == "") != (len(preflightWorkURLs) == 0) {
		fmt.Fprintln(os.Stderr, "MCP stdio probe failed: preflight template and work URL must be provided together")
		os.Exit(2)
	}
	if err := run(*binaryFlag, *proxyRoot, *codexHome, *expectInitialRuntime, *waitRuntime, *waitTimeout, *preflightTemplate, preflightWorkURLs); err != nil {
		fmt.Fprintln(os.Stderr, "MCP stdio probe failed:", err)
		os.Exit(1)
	}
	if *waitRuntime != "" {
		fmt.Printf("MCP runtime switch probe passed: %s -> %s\n", *expectInitialRuntime, *waitRuntime)
		return
	}
	fmt.Println("MCP stdio process probe passed")
}

func run(binaryFlag, proxyRoot, codexHome, expectInitialRuntime, waitRuntime string, waitTimeout time.Duration, preflightTemplate string, preflightWorkURLs []string) error {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "ocean-watch-mcp-probe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)

	binary := strings.TrimSpace(binaryFlag)
	if binary == "" {
		binary = filepath.Join(temporary, "ocean-watch")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		if err := buildBinary(moduleRoot, binary); err != nil {
			return err
		}
	} else if !filepath.IsAbs(binary) {
		binary, err = filepath.Abs(binary)
		if err != nil {
			return err
		}
	}

	switchProbe := strings.TrimSpace(waitRuntime) != ""
	if switchProbe && (strings.TrimSpace(proxyRoot) == "" || strings.TrimSpace(codexHome) == "" || waitTimeout <= 0) {
		return errors.New("runtime-switch probe requires --proxy-root, --codex-home, and a positive --wait-timeout")
	}
	resolvedCodexHome := filepath.Join(temporary, "codex-home")
	configRoot := filepath.Join(resolvedCodexHome, "ads-plan-monitor")
	configPath := filepath.Join(configRoot, "config.json")
	before := ""
	isolatedState := strings.TrimSpace(codexHome) == ""
	if !isolatedState {
		resolvedCodexHome, err = filepath.Abs(codexHome)
		if err != nil {
			return err
		}
		configRoot = filepath.Join(resolvedCodexHome, "ads-plan-monitor")
		configPath = filepath.Join(configRoot, "config.json")
	} else {
		if err := os.MkdirAll(configRoot, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(configPath, []byte(fixture), 0o600); err != nil {
			return err
		}
		before, err = snapshotManagedState(configRoot, configPath)
		if err != nil {
			return err
		}
	}

	probeTimeout := 15 * time.Second
	if switchProbe {
		probeTimeout = waitTimeout + 10*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	arguments := []string{"mcp", "serve", "--stdio"}
	if strings.TrimSpace(proxyRoot) != "" {
		resolvedRoot, resolveErr := filepath.Abs(proxyRoot)
		if resolveErr != nil {
			return resolveErr
		}
		arguments = []string{"mcp", "proxy", "--stdio", "--plugin-root", resolvedRoot}
	}
	command := exec.Command(binary, arguments...)
	command.Dir = filepath.Dir(moduleRoot)
	command.Env = replaceEnvironment(os.Environ(), "CODEX_HOME", resolvedCodexHome)
	if strings.TrimSpace(proxyRoot) == "" {
		command.Env = replaceEnvironment(command.Env, "OCEAN_WATCH_MANAGED_RUNTIME", "1")
	}
	command.Env = replaceEnvironment(command.Env, "OCEAN_WATCH_PROBE_SECRET", "must-not-be-inherited")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "ocean-watch-mcp-probe", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command: command, TerminateDuration: 2 * time.Second,
	}, nil)
	if err != nil {
		return fmt.Errorf("initialize: %w; stderr=%s", err, sanitizedDiagnostic(stderr.String(), temporary))
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("tools/list: %w", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	expectedNames := []string{
		"get_capabilities",
		"get_marketing_authorization",
		"get_qianchuan_authorization",
		"get_qianchuan_plan",
		"get_qianchuan_preflight",
		"get_template",
		"list_managed_accounts",
		"list_qianchuan_plans",
		"list_templates",
		"preflight_qianchuan_works",
		"report_marketing_materials",
		"report_marketing_plans",
		"report_qianchuan_account",
		"report_qianchuan_plans",
		"search_marketing_creator_materials",
		"search_marketing_videos",
		"search_qianchuan_products",
	}
	if strings.Join(names, ",") != strings.Join(expectedNames, ",") {
		_ = session.Close()
		return fmt.Errorf("unexpected tools: %v", names)
	}
	if preflightTemplate != "" && !switchProbe {
		if err := callReadOnlyPreflight(ctx, session, preflightTemplate, preflightWorkURLs); err != nil {
			_ = session.Close()
			return err
		}
		if err := session.Close(); err != nil {
			return fmt.Errorf("normal shutdown: %w", err)
		}
		return validateStderr(stderr.Bytes(), temporary)
	}
	if switchProbe {
		initial, err := runtimeVersion(ctx, session)
		if err != nil {
			_ = session.Close()
			return err
		}
		if expectInitialRuntime != "" && initial != expectInitialRuntime {
			_ = session.Close()
			return fmt.Errorf("initial Runtime version = %q, want %q", initial, expectInitialRuntime)
		}
		fmt.Printf("MCP runtime switch probe ready: %s\n", initial)
		deadline := time.NewTimer(waitTimeout)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer deadline.Stop()
		defer ticker.Stop()
		for initial != waitRuntime {
			select {
			case <-ctx.Done():
				_ = session.Close()
				return ctx.Err()
			case <-deadline.C:
				_ = session.Close()
				return fmt.Errorf("Runtime did not switch from %q to %q", initial, waitRuntime)
			case <-ticker.C:
				initial, err = runtimeVersion(ctx, session)
				if err != nil {
					_ = session.Close()
					return err
				}
			}
		}
		if preflightTemplate != "" {
			if callErr := callReadOnlyPreflight(ctx, session, preflightTemplate, preflightWorkURLs); callErr != nil {
				_ = session.Close()
				return callErr
			}
		}
		if err := session.Close(); err != nil {
			return fmt.Errorf("normal shutdown: %w", err)
		}
		return validateStderr(stderr.Bytes(), temporary)
	}

	listed, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_templates", Arguments: map[string]any{"channel": "all", "limit": 1},
	})
	if err != nil || listed.IsError {
		_ = session.Close()
		return fmt.Errorf("list_templates: result_error=%t error=%v", listed != nil && listed.IsError, err)
	}
	detail, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_template", Arguments: map[string]any{"channel": "qianchuan", "template_id": "qcpt_probe"},
	})
	if err != nil || detail.IsError {
		_ = session.Close()
		return fmt.Errorf("get_template: result_error=%t error=%v", detail != nil && detail.IsError, err)
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_template", Arguments: map[string]any{"channel": "qianchuan", "template_id": "qcpt_probe", "config": "/tmp/forbidden"},
	})
	if err != nil || !invalid.IsError {
		_ = session.Close()
		return fmt.Errorf("invalid tools/call did not fail closed: result_error=%t error=%v", invalid != nil && invalid.IsError, err)
	}

	canceledContext, cancelCall := context.WithCancel(ctx)
	cancelCall()
	if _, err := session.ListTools(canceledContext, nil); !errors.Is(err, context.Canceled) {
		_ = session.Close()
		return fmt.Errorf("cancellation: got %v", err)
	}
	if err := session.Close(); err != nil {
		return fmt.Errorf("normal shutdown: %w", err)
	}
	if isolatedState {
		after, err := snapshotManagedState(configRoot, configPath)
		if err != nil {
			return err
		}
		if before != after {
			return errors.New("read-only tools changed managed local state")
		}
	}
	if err := validateStderr(stderr.Bytes(), temporary); err != nil {
		return err
	}
	return nil
}

func callReadOnlyPreflight(ctx context.Context, session *mcp.ClientSession, template string, workURLs []string) error {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "preflight_qianchuan_works", Arguments: map[string]any{
		"plan_template": template, "work_urls": append([]string(nil), workURLs...), "concurrency": min(8, len(workURLs)),
	}})
	if err != nil {
		return fmt.Errorf("preflight_qianchuan_works: %w", err)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("encode preflight result: %w", err)
	}
	fmt.Printf("MCP read-only preflight result: %s\n", payload)
	return nil
}

func runtimeVersion(ctx context.Context, session *mcp.ClientSession) (string, error) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_capabilities", Arguments: map[string]any{"channel": "shared"},
	})
	if err != nil || result.IsError {
		return "", fmt.Errorf("get_capabilities: result_error=%t error=%v", result != nil && result.IsError, err)
	}
	metadata, ok := result.Meta["ocean_watch"].(map[string]any)
	if !ok {
		return "", errors.New("get_capabilities omitted proxy metadata")
	}
	version, _ := metadata["runtime_version"].(string)
	if strings.TrimSpace(version) == "" {
		return "", errors.New("get_capabilities omitted proxy runtime_version")
	}
	return version, nil
}

func sanitizedDiagnostic(value, temporary string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), temporary, "<temp>")
	if len(value) > 2048 {
		value = value[len(value)-2048:]
	}
	return value
}

func snapshotManagedState(root, configPath string) (string, error) {
	payload, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return fmt.Sprintf("%x|%d|%d|%s", payload, info.Size(), info.ModTime().UnixNano(), strings.Join(names, ",")), nil
}

func buildBinary(moduleRoot, destination string) error {
	command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-o", destination, "./cmd/ocean-watch")
	command.Dir = moduleRoot
	command.Env = replaceEnvironment(os.Environ(), "CGO_ENABLED", "0")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build probe binary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func findModuleRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && info.Mode().IsRegular() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("Go module root was not found")
		}
		current = parent
	}
}

func replaceEnvironment(values []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(values)+1)
	for _, item := range values {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func validateStderr(payload []byte, temporary string) error {
	for _, line := range bytes.Split(bytes.TrimSpace(payload), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("stderr is not structured JSON: %q", line)
		}
		for key := range record {
			switch key {
			case "timestamp", "level", "request_id", "tool", "phase", "duration_ms", "status", "error_code", "from_version", "to_version":
			default:
				return fmt.Errorf("stderr contains unsupported field %q", key)
			}
		}
	}
	for _, forbidden := range []string{temporary, "1000000000000001", "2000000000000001", "8000000000000001", "/tmp/forbidden"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			return fmt.Errorf("stderr leaked protected data")
		}
	}
	return nil
}
