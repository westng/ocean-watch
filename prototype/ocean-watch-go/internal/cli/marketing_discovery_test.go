package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	applicationdiscovery "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/discovery"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

const marketingDiscoveryToken = "TEST_MARKETING_DISCOVERY_TOKEN_DO_NOT_USE"

type marketingDiscoveryConfigStoreSpy struct {
	path       string
	mu         sync.Mutex
	reads      int
	writes     int
	revisions  []string
	updates    []map[string]any
	conflict   bool
	readErr    error
	compareErr error
}

func (store *marketingDiscoveryConfigStoreSpy) ReadWithRevision(ctx context.Context) (map[string]any, string, error) {
	store.mu.Lock()
	store.reads++
	store.mu.Unlock()
	if store.readErr != nil {
		return nil, "", store.readErr
	}
	return (filesystem.ConfigStore{Path: store.path}).ReadWithRevision(ctx)
}

func (store *marketingDiscoveryConfigStoreSpy) CompareAndSwap(
	_ context.Context,
	revision string,
	updated map[string]any,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.writes++
	store.revisions = append(store.revisions, revision)
	store.updates = append(store.updates, configuration.CloneMap(updated))
	if store.conflict {
		return errors.New("configuration changed while this operation was running; reload and retry")
	}
	if store.compareErr != nil {
		return store.compareErr
	}
	return nil
}

func (store *marketingDiscoveryConfigStoreSpy) snapshot() (int, int, []map[string]any) {
	store.mu.Lock()
	defer store.mu.Unlock()
	updates := make([]map[string]any, len(store.updates))
	for index, update := range store.updates {
		updates[index] = configuration.CloneMap(update)
	}
	return store.reads, store.writes, updates
}

type marketingDiscoveryCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
	Body   string
}

type marketingDiscoveryTransport struct {
	mu        sync.Mutex
	calls     []marketingDiscoveryCall
	adminBody func(url.Values) string
}

func (transport *marketingDiscoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(payload)
	}
	call := marketingDiscoveryCall{
		Method: request.Method,
		Host:   request.URL.Host,
		Path:   request.URL.Path,
		Query:  request.URL.Query(),
		Token:  request.Header.Get("Access-Token"),
		Body:   body,
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, call)
	transport.mu.Unlock()
	responseBody := `{"code":40400,"message":"unexpected synthetic route"}`
	switch request.URL.Path {
	case "/open_api/v3.0/project/list/":
		responseBody = `{"code":0,"message":"OK","request_id":"project-request","data":{"list":[{"project_id":9007199254740993,"advertiser_id":1000000000000001,"name":"Fixture project"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	case "/open_api/v3.0/promotion/list/":
		responseBody = `{"code":0,"message":"OK","request_id":"promotion-request","data":{"list":[{"promotion_id":9007199254740993,"project_id":9007199254740994,"advertiser_id":1000000000000001,"promotion_name":"Fixture promotion"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	case "/open_api/2/dpa/asset_v2/detail/read/":
		responseBody = `{"code":0,"message":"OK","request_id":"dpa-request","data":{"asset_list":[]}}`
	case "/open_api/2/tools/event/all_assets/list/":
		responseBody = `{"code":0,"message":"OK","request_id":"event-request","data":{"asset_list":[],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":0}}}`
	case "/open_api/v3.0/event_manager/deep_bid_type/get/":
		responseBody = `{"code":0,"message":"OK","request_id":"deep-bid-request","data":{"deep_bid_type":["NET_ORDER_ROI"],"deep_bid_type_detail":[]}}`
	case "/open_api/v3.0/event_manager/optimized_goal/get_v2/":
		responseBody = `{"code":0,"message":"OK","request_id":"goal-request","data":{"asset_ids":[9007199254740993],"goals":[{"external_action":"AD_CONVERT_TYPE_APP_ORDER","optimization_name":"净成交","history_back":false,"twenty_four_hour_back":false}]}}`
	case "/open_api/2/tools/admin/info/":
		if transport.adminBody != nil {
			responseBody = transport.adminBody(request.URL.Query())
		} else {
			responseBody = `{"code":0,"message":"OK","request_id":"admin-request","data":{"districts":[],"version":"V2_3_2"}}`
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
		Request:    request,
	}, nil
}

func (transport *marketingDiscoveryTransport) snapshot() []marketingDiscoveryCall {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	result := make([]marketingDiscoveryCall, len(transport.calls))
	copy(result, transport.calls)
	return result
}

func newMarketingDiscoveryShadowManifest(t *testing.T) application.RouteManifest {
	t.Helper()
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range []string{
		"discover projects", "discover promotions", "discover dpa", "discover events",
		"discover deep-bids", "discover goals", "discover cities",
	} {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeMarketingDiscoveryFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "synthetic-config.json")
	payload := []byte(`{
  "config_schema_version": 2,
  "default_channel": "qianchuan",
  "channels": {
    "marketing": {"api": {"base_url": "https://api.oceanengine.com/open_api"}},
    "qianchuan": {"api": {"base_url": "https://api.oceanengine.com/open_api"}}
  },
  "account": {"channel": "qianchuan", "advertiser_id": "1000000000000001"},
  "defaults": {
    "external_action": "AD_CONVERT_TYPE_APP_ORDER",
    "delivery_mode": "PROCEDURAL",
    "landing_type": "SHOP",
    "ad_type": "ALL",
    "asset_type": "THIRDPARTY",
    "marketing_goal": "VIDEO_AND_IMAGE"
  },
  "resolved_ids": {
    "event_asset_ids": ["9007199254740993"],
    "product_platform_id": "9007199254740993",
    "unique_product_id": "9007199254740993",
    "existing": "preserve-me"
  },
  "default_plan_template": {
    "defaults": {"deep_external_action": "AD_CONVERT_TYPE_APP_PAY"}
  },
  "default_qianchuan_product_template": {
    "delivery_setting": {"deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"}
  },
  "future_field": {"must_remain": true}
}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func prepareMarketingDiscoveryState(t *testing.T, stateRoot string) *runnerCredentialStore {
	t.Helper()
	if err := (filesystem.AuthorizationStore{Root: stateRoot}).CommitChannel(context.Background(), "marketing", map[string]any{
		"generation": 1,
		"authorizations": map[string]any{"marketing-auth": map[string]any{
			"token_revision": 1, "pending_account_sync": false,
			"advertiser_ids": []any{marketingCLIAdvertiserID},
			"authorized_accounts": []any{map[string]any{
				"account_id": marketingCLIAuthAccount, "advertiser_ids": []any{marketingCLIAdvertiserID},
			}},
		}},
		"account_index":    map[string]any{marketingCLIAuthAccount: "marketing-auth"},
		"advertiser_index": map[string]any{marketingCLIAdvertiserID: []any{"marketing-auth"}},
	}); err != nil {
		t.Fatal(err)
	}
	return &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-marketing-marketing-auth-r1": {
			"access_token": marketingDiscoveryToken, "access_token_expires_at": "2026-07-27T00:00:00Z",
		},
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token":            "POISON_QIANCHUAN_TOKEN_MUST_NOT_BE_READ",
			"access_token_expires_at": "2026-07-27T00:00:00Z",
		},
	}}
}

func newMarketingDiscoveryRunner(
	t *testing.T,
	root string,
	configPath string,
	transport *marketingDiscoveryTransport,
	store *marketingDiscoveryConfigStoreSpy,
	credentials *runnerCredentialStore,
	fallback *fallbackSpy,
	stdout io.Writer,
) Runner {
	t.Helper()
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(root, "codex-home")
	return Runner{
		Routes: newMarketingDiscoveryShadowManifest(t), Fallback: fallback, Stdout: stdout,
		Cwd: root, UserHome: root, Credentials: credentials,
		Getenv: func(name string) string {
			if name == "CODEX_HOME" {
				return codexHome
			}
			return ""
		},
		MarketingDiscovery: MarketingDiscoveryRuntime{
			ClientFactory: factory,
			Now:           func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) },
			Retry: platformretry.Policy{
				Delays: []time.Duration{},
				Sleep:  func(context.Context, time.Duration) error { return nil },
			},
			ConfigFactory: func(path string) MarketingDiscoveryConfigStore {
				if filepath.Clean(path) != filepath.Clean(configPath) {
					t.Fatalf("config path = %s, want %s", path, configPath)
				}
				return store
			},
		},
	}
}

func TestRunnerMarketingDiscoveryShadowUsesGeneratedSDK(t *testing.T) {
	root := t.TempDir()
	configPath := writeMarketingDiscoveryFixture(t, root)
	stateRoot := filepath.Join(root, "codex-home", "ads-plan-monitor", "state")
	credentials := prepareMarketingDiscoveryState(t, stateRoot)
	transport := &marketingDiscoveryTransport{}
	store := &marketingDiscoveryConfigStoreSpy{path: configPath}
	fallback := &fallbackSpy{code: 99}
	tests := []struct {
		name         string
		args         []string
		wantEndpoint string
		wantMethod   string
		wantPath     string
		assert       func(*testing.T, map[string]any, marketingDiscoveryCall)
	}{
		{
			name: "projects", args: []string{"discover", "projects", "--config", configPath},
			wantEndpoint: applicationdiscovery.ProjectEndpoint, wantPath: "/open_api/v3.0/project/list/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				filtering := result["params"].(map[string]any)["filtering"].(map[string]any)
				if filtering["landing_type"] != "SHOP" || filtering["marketing_goal"] != "VIDEO_AND_IMAGE" {
					t.Fatalf("project defaults changed: %#v", filtering)
				}
				if !strings.Contains(call.Query.Get("fields"), "project_id") {
					t.Fatalf("project fields missing: %#v", call.Query)
				}
			},
		},
		{
			name: "promotions",
			args: []string{"discover", "promotions", "--config", configPath,
				"--project-id", marketingCLIHighID, "--promotion-id", marketingCLIHighID},
			wantEndpoint: applicationdiscovery.PromotionEndpoint, wantPath: "/open_api/v3.0/promotion/list/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				filtering := result["params"].(map[string]any)["filtering"].(map[string]any)
				if filtering["project_id"].(json.Number).String() != marketingCLIHighID {
					t.Fatalf("high project ID changed: %#v", filtering)
				}
				if !strings.Contains(call.Query.Get("filtering"), marketingCLIHighID) {
					t.Fatalf("high promotion ID missing from SDK query: %#v", call.Query)
				}
			},
		},
		{
			name:         "dpa",
			args:         []string{"discover", "dpa", "--config", configPath, "--mode", "asset-detail"},
			wantEndpoint: applicationdiscovery.DPAAssetDetailEndpoint, wantMethod: http.MethodPost,
			wantPath: "/open_api/2/dpa/asset_v2/detail/read/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				if !strings.Contains(call.Body, marketingCLIHighID) {
					t.Fatalf("high DPA IDs changed: %s", call.Body)
				}
				payload := result["payload"].(map[string]any)
				if payload["advertiser_id"] != marketingCLIAdvertiserID {
					t.Fatalf("DPA payload changed: %#v", payload)
				}
			},
		},
		{
			name:         "events",
			args:         []string{"discover", "events", "--config", configPath, "--asset-id", marketingCLIHighID},
			wantEndpoint: applicationdiscovery.EventEndpoint, wantPath: "/open_api/2/tools/event/all_assets/list/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				if !strings.Contains(call.Query.Get("filtering"), marketingCLIHighID) {
					t.Fatalf("high event asset ID missing: %#v", call.Query)
				}
			},
		},
		{
			name: "deep-bids", args: []string{"discover", "deep-bids", "--config", configPath},
			wantEndpoint: applicationdiscovery.DeepBidEndpoint,
			wantPath:     "/open_api/v3.0/event_manager/deep_bid_type/get/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				params := result["params"].(map[string]any)
				if params["external_action"] != "AD_CONVERT_TYPE_APP_ORDER" ||
					params["deep_external_action"] != "AD_CONVERT_TYPE_APP_PAY" {
					t.Fatalf("Marketing defaults changed or Qianchuan leaked: %#v", params)
				}
				if strings.Contains(call.Query.Encode(), "LIVE_PURE_PAY_ROI") {
					t.Fatalf("Qianchuan default crossed Marketing SDK boundary: %#v", call.Query)
				}
			},
		},
		{
			name: "goals", args: []string{"discover", "goals", "--config", configPath},
			wantEndpoint: applicationdiscovery.GoalEndpoint,
			wantPath:     "/open_api/v3.0/event_manager/optimized_goal/get_v2/",
			assert: func(t *testing.T, result map[string]any, call marketingDiscoveryCall) {
				if !strings.Contains(call.Query.Encode(), marketingCLIHighID) {
					t.Fatalf("high goal asset ID missing: %#v", call.Query)
				}
				if len(result["goal_summary"].([]any)) != 1 {
					t.Fatalf("goal summary changed: %#v", result["goal_summary"])
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeCalls := len(transport.snapshot())
			beforeReads, beforeWrites, _ := store.snapshot()
			outputPath := filepath.Join(root, test.name+".json")
			args := append(append([]string(nil), test.args...), "--out", outputPath)
			stdout := new(bytes.Buffer)
			runner := newMarketingDiscoveryRunner(
				t, root, configPath, transport, store, credentials, fallback, stdout,
			)
			if code := runner.Execute(context.Background(), args); code != 0 {
				t.Fatalf("Shadow exit = %d: %s", code, stdout.String())
			}
			written, err := os.ReadFile(outputPath)
			if err != nil || !bytes.Equal(written, stdout.Bytes()) {
				t.Fatalf("--out differs from stdout: %v", err)
			}
			if !bytes.Contains(stdout.Bytes(), []byte(marketingCLIHighID)) {
				t.Fatalf("high ID missing from exact output bytes: %s", stdout.String())
			}
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			decoder.UseNumber()
			var result map[string]any
			if err := decoder.Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result["endpoint"] != test.wantEndpoint || result["response_code"] != json.Number("0") {
				t.Fatalf("discovery result changed: %#v", result)
			}
			calls := transport.snapshot()
			if len(calls) != beforeCalls+1 {
				t.Fatalf("SDK calls = %d, want %d", len(calls)-beforeCalls, 1)
			}
			call := calls[len(calls)-1]
			wantMethod := test.wantMethod
			if wantMethod == "" {
				wantMethod = http.MethodGet
			}
			if call.Path != test.wantPath || call.Method != wantMethod || call.Host != oceanengine.BusinessHost ||
				call.Token != marketingDiscoveryToken {
				t.Fatalf("generated SDK call changed: %#v", call)
			}
			if call.Query.Get("advertiser_id") != marketingCLIAdvertiserID &&
				call.Query.Get("account_id") != marketingCLIAdvertiserID && call.Method != http.MethodPost {
				t.Fatalf("advertiser missing from SDK call: %#v", call)
			}
			test.assert(t, result, call)
			afterReads, afterWrites, _ := store.snapshot()
			if afterReads != beforeReads+1 || afterWrites != beforeWrites {
				t.Fatalf("read-only config transaction changed: reads=%d writes=%d", afterReads-beforeReads, afterWrites-beforeWrites)
			}
		})
	}
	if fallback.args != nil {
		t.Fatalf("Marketing discovery Shadow invoked Python fallback: %v", fallback.args)
	}
	wantCredentialReads := make([]string, len(tests))
	for index := range wantCredentialReads {
		wantCredentialReads[index] = "oceanengine-auth-marketing-marketing-auth-r1"
	}
	if credentials.writes != 0 || !reflect.DeepEqual(credentials.reads, wantCredentialReads) {
		t.Fatalf("unexpected credential access: reads=%v writes=%d", credentials.reads, credentials.writes)
	}
}

func TestRunnerMarketingCitiesBOMChineseHeaderAndAtomicWrite(t *testing.T) {
	root := t.TempDir()
	configPath := writeMarketingDiscoveryFixture(t, root)
	cityCSV := filepath.Join(root, "cities.csv")
	if err := os.WriteFile(cityCSV, []byte("\ufeff城市,备注\n北京市,one\n上海市,two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(root, "codex-home", "ads-plan-monitor", "state")
	credentials := prepareMarketingDiscoveryState(t, stateRoot)
	transport := &marketingDiscoveryTransport{adminBody: func(query url.Values) string {
		if strings.Contains(query.Get("codes"), "CN") {
			return `{"code":0,"message":"OK","request_id":"admin-cn","data":{"districts":[{"name":"北京市","code":"11","sub_districts":[]}],"version":"V2_3_2"}}`
		}
		return `{"code":0,"message":"OK","request_id":"admin-chn","data":{"districts":[{"name":"北京市","code":"11","sub_districts":[]},{"name":"上海市","code":"31","sub_districts":[]}],"version":"V2_3_2"}}`
	}}
	store := &marketingDiscoveryConfigStoreSpy{path: configPath}
	fallback := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	runner := newMarketingDiscoveryRunner(t, root, configPath, transport, store, credentials, fallback, stdout)
	code := runner.Execute(context.Background(), []string{
		"discover", "cities", "--config", configPath, "--city-csv", cityCSV, "--write-config",
		"--country-code", "ALT",
	})
	if code != 0 {
		t.Fatalf("cities exit = %d: %s", code, stdout.String())
	}
	result := decodeSingleJSONObject(t, stdout.Bytes())
	if result["best_country_code"] != "CHN" || result["resolved_count"] != float64(2) ||
		result["config_updated"] != configPath {
		t.Fatalf("city result changed: %#v", result)
	}
	reads, writes, updates := store.snapshot()
	if reads != 1 || writes != 1 || len(updates) != 1 {
		t.Fatalf("city config transaction = reads %d writes %d updates %d", reads, writes, len(updates))
	}
	if configuration.Value(updates[0], "future_field.must_remain") != true ||
		configuration.Value(updates[0], "resolved_ids.existing") != "preserve-me" ||
		!reflect.DeepEqual(configuration.Value(updates[0], "resolved_ids.city_ids"), []any{"11", "31"}) ||
		!reflect.DeepEqual(configuration.Value(updates[0], "resolved_ids.city_names"), []any{"北京市", "上海市"}) {
		t.Fatalf("city CAS did not preserve unknown fields: %#v", updates[0])
	}
	calls := transport.snapshot()
	if len(calls) != 2 {
		t.Fatalf("admin calls = %d, want 2: %#v", len(calls), calls)
	}
	for _, call := range calls {
		if call.Path != "/open_api/2/tools/admin/info/" || call.Token != marketingDiscoveryToken {
			t.Fatalf("admin SDK call changed: %#v", call)
		}
	}
	if len(credentials.reads) != 1 || credentials.reads[0] != "oceanengine-auth-marketing-marketing-auth-r1" ||
		credentials.writes != 0 || fallback.args != nil {
		t.Fatalf("cities resolved more than one token lease: reads=%v writes=%d fallback=%v",
			credentials.reads, credentials.writes, fallback.args)
	}
}

func TestRunnerMarketingCitiesPartialAndConflictFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		adminBody  string
		conflict   bool
		wantWrites int
		wantCode   string
	}{
		{
			name:       "partial resolution",
			adminBody:  `{"code":0,"message":"OK","request_id":"admin-partial","data":{"districts":[{"name":"北京市","code":"11","sub_districts":[]}],"version":"V2_3_2"}}`,
			wantWrites: 0,
		},
		{
			name:      "CAS conflict",
			adminBody: `{"code":0,"message":"OK","request_id":"admin-complete","data":{"districts":[{"name":"北京市","code":"11","sub_districts":[]},{"name":"上海市","code":"31","sub_districts":[]}],"version":"V2_3_2"}}`,
			conflict:  true, wantWrites: 1, wantCode: "configuration_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := writeMarketingDiscoveryFixture(t, root)
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			cityCSV := filepath.Join(root, "cities.csv")
			if err := os.WriteFile(cityCSV, []byte("city\n北京\n上海\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			stateRoot := filepath.Join(root, "codex-home", "ads-plan-monitor", "state")
			credentials := prepareMarketingDiscoveryState(t, stateRoot)
			transport := &marketingDiscoveryTransport{adminBody: func(url.Values) string { return test.adminBody }}
			store := &marketingDiscoveryConfigStoreSpy{path: configPath, conflict: test.conflict}
			stdout := new(bytes.Buffer)
			runner := newMarketingDiscoveryRunner(
				t, root, configPath, transport, store, credentials, &fallbackSpy{code: 99}, stdout,
			)
			code := runner.Execute(context.Background(), []string{
				"discover", "cities", "--config", configPath, "--city-csv", cityCSV,
				"--country-code", "ONLY", "--write-config",
			})
			if code != 1 && !(test.conflict && code == 2) {
				t.Fatalf("fail-closed exit = %d: %s", code, stdout.String())
			}
			_, writes, _ := store.snapshot()
			if writes != test.wantWrites {
				t.Fatalf("CAS writes = %d, want %d", writes, test.wantWrites)
			}
			if test.wantCode != "" {
				result := decodeSingleJSONObject(t, stdout.Bytes())
				if result["error"].(map[string]any)["code"] != test.wantCode {
					t.Fatalf("error envelope changed: %#v", result)
				}
			}
			after, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed city transaction mutated config: %v", err)
			}
		})
	}
}

func TestRunnerMarketingDiscoveryRejectsInvalidInputBeforeState(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string) []string
	}{
		{
			name: "wrong channel",
			prepare: func(_ *testing.T, root string) []string {
				return []string{"discover", "projects", "--channel", "qianchuan", "--config", filepath.Join(root, "missing.json")}
			},
		},
		{
			name: "invalid enum",
			prepare: func(_ *testing.T, root string) []string {
				return []string{"discover", "events", "--asset-type", "NOT_REAL", "--config", filepath.Join(root, "missing.json")}
			},
		},
		{
			name: "invalid page",
			prepare: func(_ *testing.T, root string) []string {
				return []string{"discover", "promotions", "--page", "0", "--config", filepath.Join(root, "missing.json")}
			},
		},
		{
			name: "invalid high ID",
			prepare: func(_ *testing.T, root string) []string {
				return []string{"discover", "goals", "--asset-id", "not-an-id", "--config", filepath.Join(root, "missing.json")}
			},
		},
		{
			name: "malformed city CSV",
			prepare: func(t *testing.T, root string) []string {
				path := filepath.Join(root, "cities.csv")
				if err := os.WriteFile(path, []byte("wrong\n北京\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"discover", "cities", "--city-csv", path, "--config", filepath.Join(root, "missing.json")}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			transport := &marketingDiscoveryTransport{}
			store := &marketingDiscoveryConfigStoreSpy{path: filepath.Join(root, "missing.json")}
			credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
			fallback := &fallbackSpy{code: 99}
			stdout := new(bytes.Buffer)
			runner := newMarketingDiscoveryRunner(
				t, root, store.path, transport, store, credentials, fallback, stdout,
			)
			if code := runner.Execute(context.Background(), test.prepare(t, root)); code != 2 {
				t.Fatalf("invalid input exit = %d: %s", code, stdout.String())
			}
			result := decodeSingleJSONObject(t, stdout.Bytes())
			if result["ok"] != false || result["error"].(map[string]any)["code"] != "invalid_arguments" {
				t.Fatalf("invalid input envelope changed: %#v", result)
			}
			reads, writes, _ := store.snapshot()
			if reads != 0 || writes != 0 || len(credentials.reads) != 0 || credentials.writes != 0 ||
				len(transport.snapshot()) != 0 || fallback.args != nil {
				t.Fatalf("invalid input crossed boundary: config=%d/%d credentials=%v/%d HTTP=%d fallback=%v",
					reads, writes, credentials.reads, credentials.writes, len(transport.snapshot()), fallback.args)
			}
		})
	}
}

func TestDefaultRouteKeepsMarketingDiscoveryOnPython(t *testing.T) {
	routes := application.DefaultRouteManifest()
	for _, command := range []string{
		"discover projects", "discover promotions", "discover dpa", "discover events",
		"discover deep-bids", "discover goals", "discover cities",
	} {
		if runtime, ok := routes.RouteFor(command); !ok || runtime != application.RuntimePython {
			t.Fatalf("default route changed for %s: %s %v", command, runtime, ok)
		}
	}
}
