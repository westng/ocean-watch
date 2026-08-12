package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

const (
	marketingCLIAdvertiserID = "1000000000000001"
	marketingCLIAuthAccount  = "9000000000000001"
	marketingCLIHighID       = "9007199254740993"
)

type marketingCLIConfigReader struct {
	path  string
	mu    *sync.Mutex
	reads *int
}

func (reader marketingCLIConfigReader) Read(ctx context.Context) (map[string]any, error) {
	reader.mu.Lock()
	*reader.reads++
	reader.mu.Unlock()
	return (filesystem.ConfigStore{Path: reader.path}).Read(ctx)
}

type marketingCLICall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type marketingCLITransport struct {
	mu    sync.Mutex
	calls []marketingCLICall
}

func (transport *marketingCLITransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.calls = append(transport.calls, marketingCLICall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	})
	transport.mu.Unlock()
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch request.URL.Path {
	case "/open_api/2/file/video/ad/get/":
		body = `{"code":0,"message":"OK","request_id":"video-request","data":{"list":[{"id":"video-fixture","material_id":9007199254740993,"filename":"Fixture video"}]}}`
	case "/open_api/2/tools/aweme_auth_list/":
		body = `{"code":0,"message":"OK","request_id":"creator-request","data":{"list":[{"aweme_id":"creator-fixture","aweme_name":"Fixture creator","open_id":"open-fixture","auth_type":"VIDEO_ITEM","auth_status":"AUTHRIZED","start_time":"2026-07-20 08:00:00","end_time":"2026-08-20 08:00:00","video_info":{"item_id":9007199254740993,"mid":9007199254740994,"video_id":"creator-video","video_cover_id":"creator-cover","title":"Fixture work"}}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
	case "/open_api/2/file/image/ad/get/":
		body = `{"code":0,"message":"OK","request_id":"image-request","data":{"list":[{"id":"image-fixture","material_id":9007199254740993,"filename":"Fixture image"}]}}`
	case "/open_api/2/dpa/clue_product/list/":
		body = `{"code":0,"message":"OK","request_id":"product-request","data":{"products":[{"product_id":9007199254740993,"name":"Fixture product","images_url":[{"url":"https://example.test/product.jpg"}]}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (transport *marketingCLITransport) snapshot() []marketingCLICall {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]marketingCLICall(nil), transport.calls...)
}

func TestRunnerMarketingMaterialRuntimeUsesSDKAndFrozenDefaults(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	stateRoot := filepath.Join(codexHome, "ads-plan-monitor", "state")
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
	configPath := filepath.Join(root, "synthetic-config.json")
	config := []byte(`{
  "config_schema_version": 2,
  "default_channel": "marketing",
  "channels": {"marketing": {"api": {"base_url": "https://api.oceanengine.com/open_api"}}},
  "account": {"channel": "marketing", "advertiser_id": "1000000000000001"},
  "materials": {"video_ids": ["video-fixture"]},
  "resolved_ids": {
    "product_image_ids": ["image-fixture"],
    "product_platform_id": "platform-compatibility-only"
  },
  "defaults": {"product_id": "9007199254740993"},
  "future_field": {"must_remain": true}
}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-marketing-marketing-auth-r1": {
			"access_token":            "TEST_MARKETING_ACCESS_TOKEN_DO_NOT_USE",
			"access_token_expires_at": "2026-07-26T00:00:00Z",
		},
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token":            "TEST_QIANCHUAN_ACCESS_TOKEN_DO_NOT_USE",
			"access_token_expires_at": "2026-07-26T00:00:00Z",
		},
	}}
	transport := &marketingCLITransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range []string{"materials videos", "materials creator", "materials images", "materials products"} {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	var configMu sync.Mutex
	configReads := 0
	newRunner := func(stdout io.Writer) Runner {
		return Runner{
			Routes: manifest, Stdout: stdout, Cwd: root, UserHome: root,
			Getenv: func(name string) string {
				if name == "CODEX_HOME" {
					return codexHome
				}
				return ""
			},
			Credentials: credentials,
			MarketingMaterials: MarketingMaterialRuntime{
				ClientFactory: factory,
				Now:           func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) },
				Retry: platformretry.Policy{
					Delays: []time.Duration{},
					Sleep:  func(context.Context, time.Duration) error { return nil },
				},
				ConfigFactory: func(path string) applicationmaterials.ConfigReader {
					return marketingCLIConfigReader{path: path, mu: &configMu, reads: &configReads}
				},
			},
		}
	}
	tests := []struct {
		name         string
		args         []string
		wantEndpoint string
		assert       func(*testing.T, map[string]any)
	}{
		{
			name: "videos", wantEndpoint: applicationmaterials.AdVideoEndpoint,
			args: []string{"materials", "videos", "--config", configPath, "--mode", "ad-get"},
			assert: func(t *testing.T, result map[string]any) {
				params := result["params"].(map[string]any)
				if !reflect.DeepEqual(params["video_ids"], []any{"video-fixture"}) {
					t.Fatalf("configured video IDs changed: %#v", params)
				}
			},
		},
		{
			name: "creator", wantEndpoint: applicationmaterials.CreatorAuthorizationEndpoint,
			args: []string{
				"materials", "creator", "--config", configPath,
				"--aweme-id", "creator-fixture", "--item-id", marketingCLIHighID,
			},
			assert: func(t *testing.T, result map[string]any) {
				if result["source_type"] != applicationmaterials.CreatorAuthorizedSource || result["candidate_count"] != float64(1) {
					t.Fatalf("creator result changed: %#v", result)
				}
				candidate := result["candidates"].([]any)[0].(map[string]any)
				if candidate["item_id"] != marketingCLIHighID || candidate["usable"] != true {
					t.Fatalf("creator candidate changed: %#v", candidate)
				}
			},
		},
		{
			name: "images", wantEndpoint: applicationmaterials.AdImageEndpoint,
			args: []string{"materials", "images", "--config", configPath},
			assert: func(t *testing.T, result map[string]any) {
				params := result["params"].(map[string]any)
				if !reflect.DeepEqual(params["image_ids"], []any{"image-fixture"}) {
					t.Fatalf("configured image IDs changed: %#v", params)
				}
			},
		},
		{
			name: "products", wantEndpoint: applicationmaterials.ProductEndpoint,
			args: []string{"materials", "products", "--config", configPath},
			assert: func(t *testing.T, result map[string]any) {
				params := result["params"].(map[string]any)
				if params["product_id"] != marketingCLIHighID || params["product_platform_id"] != "platform-compatibility-only" {
					t.Fatalf("configured product defaults changed: %#v", params)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(root, test.name+".json")
			args := append(append([]string(nil), test.args...), "--out", outputPath)
			stdout := new(bytes.Buffer)
			if code := newRunner(stdout).Execute(context.Background(), args); code != 0 {
				t.Fatalf("Runtime exit = %d: %s", code, stdout.String())
			}
			result := decodeSingleJSONObject(t, stdout.Bytes())
			if result["endpoint"] != test.wantEndpoint {
				t.Fatalf("endpoint = %#v, want %s", result["endpoint"], test.wantEndpoint)
			}
			test.assert(t, result)
			written, readErr := os.ReadFile(outputPath)
			if readErr != nil || !bytes.Equal(written, stdout.Bytes()) {
				t.Fatalf("--out differs from stdout: %v", readErr)
			}
		})
	}
	if configReads != len(tests) {
		t.Fatalf("config reads = %d, want %d", configReads, len(tests))
	}
	writtenConfig, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(writtenConfig, config) {
		t.Fatalf("read-only commands changed config: %v", err)
	}
	wantCredentialReads := []string{
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
	}
	if credentials.writes != 0 || !reflect.DeepEqual(credentials.reads, wantCredentialReads) {
		t.Fatalf("unexpected credential access: reads=%v writes=%d", credentials.reads, credentials.writes)
	}
	calls := transport.snapshot()
	wantPaths := []string{
		"/open_api/2/file/video/ad/get/",
		"/open_api/2/tools/aweme_auth_list/",
		"/open_api/2/file/image/ad/get/",
		"/open_api/2/dpa/clue_product/list/",
	}
	if len(calls) != len(wantPaths) {
		t.Fatalf("SDK calls = %d, want %d: %#v", len(calls), len(wantPaths), calls)
	}
	for index, call := range calls {
		if call.Path != wantPaths[index] || call.Host != oceanengine.BusinessHost ||
			call.Method != http.MethodGet || call.Token != "TEST_MARKETING_ACCESS_TOKEN_DO_NOT_USE" ||
			call.Query.Get("advertiser_id") != marketingCLIAdvertiserID {
			t.Fatalf("Marketing SDK call %d changed: %#v", index, call)
		}
	}
	if strings.Contains(calls[3].Query.Encode(), "product_platform_id") ||
		!strings.Contains(calls[3].Query.Get("filtering"), marketingCLIHighID) {
		t.Fatalf("compatibility-only product field crossed SDK boundary: %#v", calls[3].Query)
	}
}

func TestRunnerMarketingMaterialRejectsInvalidArgumentsBeforeLocalState(t *testing.T) {
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range []string{"materials videos", "materials products"} {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
	configReads := 0
	var mu sync.Mutex
	runner := Runner{
		Routes: manifest, Credentials: credentials,
		Cwd: t.TempDir(), UserHome: t.TempDir(), Getenv: func(string) string { return "" },
		MarketingMaterials: MarketingMaterialRuntime{
			ConfigFactory: func(path string) applicationmaterials.ConfigReader {
				return marketingCLIConfigReader{path: path, mu: &mu, reads: &configReads}
			},
		},
	}
	for _, args := range [][]string{
		{"materials", "videos", "--channel", "qianchuan", "--config", filepath.Join(t.TempDir(), "missing.json")},
		{"materials", "products", "--path", "/2/dpa/other/list/", "--config", filepath.Join(t.TempDir(), "missing.json")},
	} {
		stdout := new(bytes.Buffer)
		runner.Stdout = stdout
		if code := runner.Execute(context.Background(), args); code != 2 {
			t.Fatalf("invalid args exit = %d: %s", code, stdout.String())
		}
		if decodeSingleJSONObject(t, stdout.Bytes())["ok"] != false {
			t.Fatalf("invalid arguments did not return error JSON: %s", stdout.String())
		}
	}
	if configReads != 0 || len(credentials.reads) != 0 || credentials.writes != 0 {
		t.Fatalf("invalid args crossed local/runtime boundary: config=%d credential=%v/%d",
			configReads, credentials.reads, credentials.writes)
	}
}

func TestDefaultRouteUsesMarketingMaterialsGo(t *testing.T) {
	routes := application.DefaultRouteManifest()
	for _, command := range []string{"materials videos", "materials creator", "materials images", "materials products"} {
		if runtime, ok := routes.RouteFor(command); !ok || runtime != application.RuntimeGo {
			t.Fatalf("default route changed for %s: %s %v", command, runtime, ok)
		}
	}
}

func TestMarketingCLITransportFixtureIsJSON(t *testing.T) {
	transport := &marketingCLITransport{}
	request, err := http.NewRequest(http.MethodGet, "https://api.oceanengine.com/open_api/2/file/video/ad/get/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil || value["code"] != float64(0) {
		t.Fatalf("invalid synthetic response: %#v %v", value, err)
	}
}
