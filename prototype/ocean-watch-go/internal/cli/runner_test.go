package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/onboarding"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/resources"
)

type fallbackSpy struct {
	args []string
	code int
}

type runnerCredentialStore struct {
	mu      sync.Mutex
	entries map[string]map[string]any
	reads   []string
	writes  int
	backend string
}

func (store *runnerCredentialStore) BackendName() string {
	if store.backend != "" {
		return store.backend
	}
	return "memory"
}

func (store *runnerCredentialStore) Read(_ context.Context, account string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reads = append(store.reads, account)
	return configuration.CloneMap(store.entries[account]), nil
}

func (store *runnerCredentialStore) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.writes++
	store.entries[account] = configuration.CloneMap(value)
	return "memory", nil
}

type runnerAuthorizationReader struct {
	states map[string]domain.AuthorizationState
	reads  []string
}

func (reader *runnerAuthorizationReader) ReadChannel(_ context.Context, channel string) (domain.AuthorizationState, error) {
	reader.reads = append(reader.reads, channel)
	return reader.states[channel], nil
}

type runnerEnvironmentProbe struct {
	calls       []string
	redirectURI string
	blocked     string
}

func (probe *runnerEnvironmentProbe) check(identifier string, required bool) onboarding.Check {
	probe.calls = append(probe.calls, identifier)
	status := "ready"
	if probe.blocked == identifier {
		status = "blocked"
	}
	return onboarding.Check{"id": identifier, "required": required, "status": status}
}

func (probe *runnerEnvironmentProbe) Python(context.Context) onboarding.Check {
	return probe.check("python", true)
}

func (probe *runnerEnvironmentProbe) Platform(context.Context) onboarding.Check {
	return probe.check("platform", true)
}

func (probe *runnerEnvironmentProbe) CodexCLI(context.Context) onboarding.Check {
	return probe.check("codex_cli", false)
}

func (probe *runnerEnvironmentProbe) CredentialBackend(context.Context) onboarding.Check {
	return probe.check("credential_backend", true)
}

func (probe *runnerEnvironmentProbe) Callback(_ context.Context, redirectURI string) onboarding.Check {
	probe.redirectURI = redirectURI
	return probe.check("oauth_callback", true)
}

type runnerMCPProbe struct {
	calls int
}

func (probe *runnerMCPProbe) Status(_ context.Context, hasAppID, hasDeveloperID bool) map[string]any {
	probe.calls++
	return map[string]any{
		"server": "oceanengine-developer-docs", "codex_cli_available": false,
		"has_app_id": hasAppID, "has_developer_id": hasDeveloperID,
		"registered": false, "uses_sse_bridge": false, "ready": false,
		"next_action": "register_mcp",
	}
}

func (spy *fallbackSpy) Run(_ context.Context, args []string, stdout, _ io.Writer) int {
	spy.args = append([]string(nil), args...)
	_, _ = io.WriteString(stdout, `{"ok":true}`)
	return spy.code
}

func TestUnmigratedCommandPreservesArgumentsAndExitCode(t *testing.T) {
	spy := &fallbackSpy{code: 17}
	stdout := new(bytes.Buffer)
	runner := Runner{Routes: application.DefaultRouteManifest(), Fallback: spy, Stdout: stdout}
	args := []string{"reports", "plans", "--advertiser-id", "1000000000000001", "--top", "10"}
	if code := runner.Execute(context.Background(), args); code != 17 {
		t.Fatalf("got exit %d, want 17", code)
	}
	if !reflect.DeepEqual(spy.args, args) {
		t.Fatalf("fallback arguments changed: %#v", spy.args)
	}
}

func TestTopLevelAndDomainHelpPreserveArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"accounts", "--help"}} {
		spy := &fallbackSpy{}
		runner := Runner{Routes: application.DefaultRouteManifest(), Fallback: spy, Stdout: io.Discard}
		if code := runner.Execute(context.Background(), args); code != 0 {
			t.Fatalf("%v returned %d", args, code)
		}
		if !reflect.DeepEqual(spy.args, args) {
			t.Fatalf("help arguments changed: got %#v, want %#v", spy.args, args)
		}
	}
}

func TestAccountListUsesGoWithoutFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := `{"managed_account_schema_version":1,"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"账户","enabled":true}],"qianchuan":[]}}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	spy := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: spy, Stdout: stdout,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
	}
	if code := runner.Execute(context.Background(), []string{"accounts", "list", "--config", path}); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if spy.args != nil {
		t.Fatal("local account list invoked Python fallback")
	}
	var result AccountListEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Accounts) != 1 || len(result.Presentation.Columns) != 4 {
		t.Fatalf("unexpected account list envelope: %#v", result)
	}
}

func TestSetupDoctorUsesGoOnceWithoutCredentialsOrConfigWrites(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := []byte(`{"channels":{"qianchuan":{"oauth":{"redirect_uri":"http://127.0.0.1:9922/from-config"}}}}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(root, "doctor.json")
	fallback := &fallbackSpy{code: 99}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
	authorizations := &runnerAuthorizationReader{states: map[string]domain.AuthorizationState{}}
	probe := &runnerEnvironmentProbe{}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: fallback, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		Credentials: credentials, Authorizations: authorizations, EnvironmentProbe: probe,
	}
	args := []string{
		"setup", "doctor", "--config", configPath, "--channel", "qianchuan",
		"--redirect-uri", "http://127.0.0.1:9933/explicit", "--out", outPath,
	}
	if code := runner.Execute(context.Background(), args); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if fallback.args != nil {
		t.Fatal("setup doctor invoked Python fallback")
	}
	if len(credentials.reads) != 0 || credentials.writes != 0 || len(authorizations.reads) != 0 {
		t.Fatalf("doctor touched credential state: reads=%v writes=%d auth=%v", credentials.reads, credentials.writes, authorizations.reads)
	}
	wantCalls := []string{"python", "platform", "codex_cli", "credential_backend", "oauth_callback"}
	if !reflect.DeepEqual(probe.calls, wantCalls) || probe.redirectURI != "http://127.0.0.1:9933/explicit" {
		t.Fatalf("unexpected environment probes: calls=%v redirect=%q", probe.calls, probe.redirectURI)
	}
	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, stdout.Bytes()) {
		t.Fatal("doctor --out differs from stdout")
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["mode"] != "environment_check" || result["channel"] != "qianchuan" || result["ok"] != true {
		t.Fatalf("unexpected doctor result: %#v", result)
	}
	current, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(current, config) {
		t.Fatalf("doctor changed config: %v", err)
	}
}

func TestSetupValidateUsesOnlyReadOnlyLocalState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := []byte(`{
  "config_schema_version": 2,
  "default_channel": "marketing",
  "channels": {"marketing": {"api": {"base_url": "https://api.oceanengine.com/open_api"}}},
  "account": {"channel": "marketing", "advertiser_id": "1000000000000001"},
  "plan_templates": {},
  "plan_template_schema_version": 6
}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-app-marketing":            {"app_id": "fixture-app", "secret": "fixture-secret"},
		"oceanengine-auth-marketing-auth-1-r1": {"access_token": "fixture-access"},
	}}
	authorizations := &runnerAuthorizationReader{states: map[string]domain.AuthorizationState{
		"marketing": {
			AuthorizationCount: 1, AuthorizedAccountCount: 1,
			AdvertiserIDs: []string{"1000000000000001"}, Generation: 1,
			Authorizations: []domain.AuthorizationSummary{{
				AuthorizationID: "auth-1", TokenRevision: 1,
				AdvertiserIDs: []string{"1000000000000001"}, AccountDiscoveryComplete: true,
			}},
		},
	}}
	fallback := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: fallback, Stdout: stdout,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		Credentials: credentials, Authorizations: authorizations,
	}
	if code := runner.Execute(context.Background(), []string{
		"setup", "validate", "--config", configPath, "--mode", "query",
	}); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if fallback.args != nil {
		t.Fatal("setup validate invoked Python fallback")
	}
	if credentials.writes != 0 {
		t.Fatalf("setup validate wrote credentials %d times", credentials.writes)
	}
	wantReads := []string{
		"oceanengine-app-marketing", "oceanengine-oauth", "oceanengine-auth-marketing-auth-1-r1",
	}
	if !reflect.DeepEqual(credentials.reads, wantReads) || !reflect.DeepEqual(authorizations.reads, []string{"marketing"}) {
		t.Fatalf("unexpected local reads: credentials=%v authorizations=%v", credentials.reads, authorizations.reads)
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["validation_mode"] != "query" || result["selected_mode_ready"] != true {
		t.Fatalf("unexpected validate result: %#v", result)
	}
	current, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(current, config) {
		t.Fatalf("validate changed config: %v", err)
	}
}

func TestSetupInitCreatesPreservesAndForceReplacesOnlyTargetConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "nested", "config.json")
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
	authorizations := &runnerAuthorizationReader{states: map[string]domain.AuthorizationState{}}
	probe := &runnerEnvironmentProbe{}
	mcp := &runnerMCPProbe{}
	fallback := &fallbackSpy{code: 99}
	run := func(extra ...string) map[string]any {
		t.Helper()
		stdout := new(bytes.Buffer)
		runner := Runner{
			Routes: application.DefaultRouteManifest(), Fallback: fallback, Stdout: stdout,
			Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
			Credentials: credentials, Authorizations: authorizations,
			EnvironmentProbe: probe, MCPProbe: mcp,
		}
		args := append([]string{"setup", "init", "--config", configPath}, extra...)
		if code := runner.Execute(context.Background(), args); code != 0 {
			t.Fatalf("got exit %d: %s", code, stdout.String())
		}
		return decodeSingleJSONObject(t, stdout.Bytes())
	}

	first := run()
	if first["mode"] != "first_run_guide" || first["config"] != configPath || first["created_config_from_template"] != true {
		t.Fatalf("init returned an incomplete guide: %#v", first)
	}
	for _, field := range []string{
		"channels", "environment", "available_plan_templates", "template_setup",
		"oauth_setup", "official_docs_mcp", "minimum_fields_for_query_data",
		"additional_fields_for_create_plan", "safe_notes", "example_first_prompts",
	} {
		if _, exists := first[field]; !exists {
			t.Fatalf("first-run guide omitted %s", field)
		}
	}
	if fallback.args != nil || credentials.writes != 0 {
		t.Fatalf("setup init crossed runtime/write boundary: fallback=%v credential_writes=%d", fallback.args, credentials.writes)
	}
	embedded, err := resources.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var storedConfig map[string]any
	if err := json.Unmarshal(stored, &storedConfig); err != nil {
		t.Fatal(err)
	}
	if !configuration.Equal(storedConfig, embedded) {
		t.Fatal("setup init did not write the embedded config template")
	}

	preserved := append([]byte(nil), stored...)
	preserved = bytes.Replace(preserved, []byte("{\n"), []byte("{\n  \"future_preserved\": true,\n"), 1)
	if err := os.WriteFile(configPath, preserved, 0o600); err != nil {
		t.Fatal(err)
	}
	second := run()
	if second["created_config_from_template"] != false {
		t.Fatalf("non-force init reported replacement: %#v", second)
	}
	current, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(current, preserved) {
		t.Fatalf("non-force init changed existing config: %v", err)
	}

	forced := run("--force")
	if forced["created_config_from_template"] != true {
		t.Fatalf("force init did not report replacement: %#v", forced)
	}
	backup, err := os.ReadFile(configPath + ".bak")
	if err != nil || !bytes.Equal(backup, preserved) {
		t.Fatalf("force init did not preserve exact backup: %v", err)
	}
	current, err = os.ReadFile(configPath)
	if err != nil || bytes.Contains(current, []byte("future_preserved")) {
		t.Fatalf("force init did not replace target config: %v", err)
	}
	if credentials.writes != 0 || mcp.calls != 3 {
		t.Fatalf("unexpected init side effects: credential_writes=%d mcp_calls=%d", credentials.writes, mcp.calls)
	}
	if len(probe.calls) != 15 {
		t.Fatalf("environment probe count = %d, want 15", len(probe.calls))
	}
	for _, account := range credentials.reads {
		if strings.Contains(account, "access_token") || strings.Contains(account, "refresh_token") {
			t.Fatalf("credential account name leaked token material: %s", account)
		}
	}
}

func TestSetupExitCodesRemainCompatible(t *testing.T) {
	root := t.TempDir()
	blockedProbe := &runnerEnvironmentProbe{blocked: "python"}
	doctorOutput := new(bytes.Buffer)
	doctorRunner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: &fallbackSpy{}, Stdout: doctorOutput,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		Credentials:      &runnerCredentialStore{entries: map[string]map[string]any{}},
		Authorizations:   &runnerAuthorizationReader{states: map[string]domain.AuthorizationState{}},
		EnvironmentProbe: blockedProbe,
	}
	if code := doctorRunner.Execute(context.Background(), []string{"setup", "doctor"}); code != 1 {
		t.Fatalf("blocked doctor exit = %d, want 1", code)
	}
	if decodeSingleJSONObject(t, doctorOutput.Bytes())["ok"] != false {
		t.Fatal("blocked doctor did not return structured readiness failure")
	}

	validateOutput := new(bytes.Buffer)
	validateRunner := doctorRunner
	validateRunner.Stdout = validateOutput
	if code := validateRunner.Execute(context.Background(), []string{
		"setup", "validate", "--config", filepath.Join(root, "missing.json"),
	}); code != 2 {
		t.Fatalf("missing validate config exit = %d, want 2", code)
	}
	result := decodeSingleJSONObject(t, validateOutput.Bytes())
	if result["error"].(map[string]any)["code"] != "configuration_error" {
		t.Fatalf("unexpected validate error: %#v", result)
	}
	expectedPath, err := absoluteLocalPath(filepath.Join(root, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result["error"].(map[string]any)["message"] != "missing config: "+expectedPath {
		t.Fatalf("missing config message changed: %#v", result)
	}
}

func TestResolveSkillRootUsesPluginOrFallbackEntrypoint(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugin")
	if got := resolveSkillRoot(pluginRoot, func(string) string { return "" }); got != filepath.Join(pluginRoot, "skills", "ads-plan-monitor") {
		t.Fatalf("plugin skill root = %q", got)
	}
	entrypoint := filepath.Join(t.TempDir(), "skills", "ads-plan-monitor", "run.py")
	got := resolveSkillRoot("", func(name string) string {
		if name == "OCEAN_WATCH_PYTHON_ENTRYPOINT" {
			return entrypoint
		}
		return ""
	})
	if got != filepath.Dir(entrypoint) {
		t.Fatalf("fallback skill root = %q", got)
	}
}

func TestAuthorizationMigrationUsesGoWithoutFallback(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := `{
  "future": {"preserved": true},
  "api": {
    "app_id": "fixture-app",
    "secret": "fixture-secret",
    "access_token": "fixture-access",
    "refresh_token": "fixture-refresh"
  },
  "account": {"advertiser_id": "1000000000000001"}
}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	credentialStore := &runnerCredentialStore{entries: map[string]map[string]any{}}
	fallback := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: fallback, Stdout: stdout,
		Cwd: root, UserHome: root, Credentials: credentialStore,
		Getenv: func(name string) string {
			if name == "CODEX_HOME" {
				return filepath.Join(root, "codex-home")
			}
			return ""
		},
	}
	if code := runner.Execute(context.Background(), []string{"auth", "migrate", "--config", configPath}); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if fallback.args != nil {
		t.Fatal("authorization migration invoked Python fallback")
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["activation"] != "schema_v2_active" || result["config"] != configPath {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	migrated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var migratedConfig map[string]any
	if err := json.Unmarshal(migrated, &migratedConfig); err != nil {
		t.Fatal(err)
	}
	if migratedConfig["api"] != nil || migratedConfig["future"].(map[string]any)["preserved"] != true {
		t.Fatalf("migration did not clean and preserve config: %#v", migratedConfig)
	}
	if credentialStore.entries["oceanengine-app-marketing"] == nil {
		t.Fatal("migration did not write the split Marketing app credential")
	}
	currentPath := filepath.Join(root, "codex-home", "ads-plan-monitor", "state", "channels", "marketing", "current.json")
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("authorization pointer was not committed: %v", err)
	}
}

func TestInvalidCommandProducesOneJSONError(t *testing.T) {
	stdout := new(bytes.Buffer)
	runner := Runner{Routes: application.DefaultRouteManifest(), Fallback: &fallbackSpy{}, Stdout: stdout}
	if code := runner.Execute(context.Background(), []string{"unknown", "command"}); code != 2 {
		t.Fatalf("got exit %d", code)
	}
	decoder := json.NewDecoder(stdout)
	var first ErrorEnvelope
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	var second any
	if err := decoder.Decode(&second); err != io.EOF {
		t.Fatal("stdout contained more than one JSON value")
	}
}

func TestAccountRemoveMatchesPythonResponseShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := `{"managed_account_schema_version":1,"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"account","enabled":true}],"qianchuan":[]}}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: &fallbackSpy{}, Stdout: stdout,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
	}
	code := runner.Execute(context.Background(), []string{
		"accounts", "remove", "--config", path,
		"--channel", "marketing", "--advertiser-id", "1000000000000001",
	})
	if code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	account, ok := result["account"].(map[string]any)
	if !ok {
		t.Fatalf("missing account result: %#v", result)
	}
	want := map[string]any{"channel": "marketing", "advertiser_id": "1000000000000001"}
	if !reflect.DeepEqual(account, want) {
		t.Fatalf("remove account = %#v, want %#v", account, want)
	}
}

func TestRunListUsesManagedStateRootWithoutFallback(t *testing.T) {
	codexHome := t.TempDir()
	runsRoot := filepath.Join(codexHome, "ads-plan-monitor", "state", "runs")
	if err := os.MkdirAll(runsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runsRoot, "creator-batch-abc.json"), []byte(`{"jobs":{"one":{"status":"completed"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spy := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: spy, Stdout: stdout,
		UserHome: t.TempDir(), Getenv: func(name string) string {
			if name == "CODEX_HOME" {
				return codexHome
			}
			return ""
		},
	}
	if code := runner.Execute(context.Background(), []string{"runs", "list"}); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if spy.args != nil {
		t.Fatal("run list invoked Python fallback")
	}
	var result RunListEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Mode != "run_history" || result.RunCount != 1 || len(result.Runs) != 1 {
		t.Fatalf("unexpected run list: %#v", result)
	}
}

func TestTemplateListUsesGoWithoutFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"plan_templates":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spy := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := Runner{
		Routes: application.DefaultRouteManifest(), Fallback: spy, Stdout: stdout,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
	}
	if code := runner.Execute(context.Background(), []string{"templates", "list", "--config", path}); code != 0 {
		t.Fatalf("got exit %d: %s", code, stdout.String())
	}
	if spy.args != nil {
		t.Fatal("template list invoked Python fallback")
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["source"] != "local_config" || result["config"] != path {
		t.Fatalf("unexpected template result: %#v", result)
	}
}
