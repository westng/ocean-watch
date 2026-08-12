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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

type qianchuanRunnerCall struct {
	Host  string
	Path  string
	Query url.Values
	Token string
}

type qianchuanRunnerTransport struct {
	mu             sync.Mutex
	calls          []qianchuanRunnerCall
	creatorFixture string
}

func (transport *qianchuanRunnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := qianchuanRunnerCall{
		Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, call)
	transport.mu.Unlock()
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch request.URL.Path {
	case "/open_api/v1.0/qianchuan/uni_promotion/product/get/":
		body = `{"code":0,"message":"OK","request_id":"product-request","data":{"page_info":{"page":1,"total_page":1,"total_number":1},"product_list":[{"id":3000000000000001,"name":"Fixture product"}]}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/list/":
		body = `{"code":0,"message":"OK","request_id":"plan-request","data":{"page_info":{"page":1,"page_size":100,"total_page":1,"total_num":1},"ad_list":[{"ad_info":{"id":2000000000000001,"name":"Fixture plan"},"stats_info":{"stat_cost":99999999.99}}]}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/ad/detail/":
		body = `{"code":0,"message":"OK","request_id":"detail-request","data":{"ad_id":2000000000000001,"name":"Fixture plan"}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/ad/material/get/":
		body = `{"code":0,"message":"OK","request_id":"material-request","data":{"page_info":{"page":1,"total_page":1,"total_number":1},"ad_material_infos":[{"material_info":{"material_type":"VIDEO","video_material":{"material_id":5000000000000001,"aweme_item_id":5000000000000002,"video_id":"fixture-video","title":"Fixture material"}},"stats_info":{"stat_cost":88888888.88}}]}}`
	case "/open_api/v1.0/qianchuan/uni_aweme/authorized/get/":
		if transport.creatorFixture != "" {
			body = transport.creatorFixture
		} else {
			body = `{"code":0,"message":"OK","request_id":"creator-request","data":{"aweme_id_list":[{"aweme_id":4000000000000001,"aweme_show_id":"creator001","aweme_name":"Fixture creator","auth_type":["VIDEO"],"has_authorized":true,"is_product_uni_prom_disabled":false}],"page_info":{"page":1,"page_size":100,"total_number":1,"total_page":1}}}`
		}
	case "/open_api/v1.0/qianchuan/file/video/aweme/get/":
		body = `{"code":0,"message":"OK","request_id":"video-request","data":{"page_info":{"count":1,"cursor":0,"has_more":0},"video_list":[{"aweme_item_id":5000000000000003,"material_id":5000000000000004,"video_id":"creator-video","image_mode":"VIDEO_VERTICAL","title":"Fixture creator material"}]}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func TestRunnerCreatorVideosRejectsTemplateBeforeCredentialOrHTTP(t *testing.T) {
	for _, test := range []struct {
		name       string
		selector   string
		template   string
		wantDetail string
	}{
		{
			name: "missing-template", selector: "missing",
			template:   `{}`,
			wantDetail: "Qianchuan product template not found",
		},
		{
			name: "inactive-template", selector: "qcpt_fixture",
			template:   qianchuanRunnerTemplateFixture("inactive"),
			wantDetail: "Qianchuan product template is inactive",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQianchuanCreatorRunnerFixture(t, test.template, "")
			stdout := new(bytes.Buffer)
			code := fixture.runner(stdout).Execute(context.Background(), []string{
				"qc-materials", "creator-videos", "--config", fixture.configPath,
				"--plan-template", test.selector, "--douyin-id", "creator001",
			})
			if code == 0 || !strings.Contains(stdout.String(), test.wantDetail) {
				t.Fatalf("template preflight exit=%d output=%s", code, stdout.String())
			}
			if len(fixture.credentials.reads) != 0 || fixture.credentials.writes != 0 {
				t.Fatalf("template preflight reached credentials: reads=%v writes=%d", fixture.credentials.reads, fixture.credentials.writes)
			}
			if calls := fixture.transport.snapshot(); len(calls) != 0 {
				t.Fatalf("template preflight reached HTTP: %#v", calls)
			}
		})
	}
}

func TestRunnerCreatorVideosStopsAfterInexactOrAmbiguousAuthorization(t *testing.T) {
	for _, test := range []struct {
		name           string
		creatorFixture string
		wantDetail     string
	}{
		{
			name:           "fuzzy-only",
			creatorFixture: `{"code":0,"message":"OK","request_id":"creator-request","data":{"aweme_id_list":[{"aweme_id":4000000000000001,"aweme_show_id":"creator001-fuzzy","aweme_name":"Fixture creator","auth_type":["VIDEO"]}],"page_info":{"page":1,"page_size":100,"total_number":1,"total_page":1}}}`,
			wantDetail:     "No exact authorized Qianchuan creator matched douyin_id",
		},
		{
			name:           "multiple-exact",
			creatorFixture: `{"code":0,"message":"OK","request_id":"creator-request","data":{"aweme_id_list":[{"aweme_id":4000000000000001,"aweme_show_id":"creator001","aweme_name":"Fixture creator A","auth_type":["VIDEO"]},{"aweme_id":4000000000000002,"aweme_show_id":"creator001","aweme_name":"Fixture creator B","auth_type":["VIDEO"]}],"page_info":{"page":1,"page_size":100,"total_number":2,"total_page":1}}}`,
			wantDetail:     "douyin_id matched multiple authorized Qianchuan creators",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newQianchuanCreatorRunnerFixture(t, qianchuanRunnerTemplateFixture("active"), test.creatorFixture)
			stdout := new(bytes.Buffer)
			code := fixture.runner(stdout).Execute(context.Background(), []string{
				"qc-materials", "creator-videos", "--config", fixture.configPath,
				"--plan-template", "qcpt_fixture", "--douyin-id", "creator001",
			})
			if code == 0 || !strings.Contains(stdout.String(), test.wantDetail) {
				t.Fatalf("creator resolution exit=%d output=%s", code, stdout.String())
			}
			if !reflect.DeepEqual(fixture.credentials.reads, []string{"oceanengine-auth-qianchuan-qianchuan-auth-r1"}) ||
				fixture.credentials.writes != 0 {
				t.Fatalf("creator resolution credential access changed: reads=%v writes=%d", fixture.credentials.reads, fixture.credentials.writes)
			}
			calls := fixture.transport.snapshot()
			if len(calls) != 1 || calls[0].Path != "/open_api/v1.0/qianchuan/uni_aweme/authorized/get/" {
				t.Fatalf("inexact creator resolution reached video API: %#v", calls)
			}
		})
	}
}

type qianchuanCreatorRunnerFixture struct {
	runner      func(io.Writer) Runner
	configPath  string
	credentials *runnerCredentialStore
	transport   *qianchuanRunnerTransport
}

func newQianchuanCreatorRunnerFixture(
	t *testing.T,
	config string,
	creatorFixture string,
) qianchuanCreatorRunnerFixture {
	t.Helper()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	stateRoot := filepath.Join(codexHome, "ads-plan-monitor", "state")
	if err := (filesystem.AuthorizationStore{Root: stateRoot}).CommitChannel(context.Background(), "qianchuan", map[string]any{
		"generation": 1,
		"authorizations": map[string]any{"qianchuan-auth": map[string]any{
			"token_revision": 1, "pending_account_sync": false,
			"advertiser_ids": []any{"1000000000000001"},
			"authorized_accounts": []any{map[string]any{
				"account_id": "9000000000000001", "advertiser_ids": []any{"1000000000000001"},
			}},
		}},
		"account_index":    map[string]any{"9000000000000001": "qianchuan-auth"},
		"advertiser_index": map[string]any{"1000000000000001": []any{"qianchuan-auth"}},
	}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "synthetic-config.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token": "fixture-qianchuan-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
	}}
	transport := &qianchuanRunnerTransport{creatorFixture: creatorFixture}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	routes := application.DefaultRouteManifest().Snapshot()
	routes["qc-materials creator-videos"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	return qianchuanCreatorRunnerFixture{
		configPath: configPath, credentials: credentials, transport: transport,
		runner: func(stdout io.Writer) Runner {
			return Runner{
				Routes: manifest, Stdout: stdout,
				Cwd: root, UserHome: root, Credentials: credentials,
				Getenv: func(name string) string {
					if name == "CODEX_HOME" {
						return codexHome
					}
					return ""
				},
				QianchuanReads: QianchuanReadRuntime{
					ClientFactory: factory,
					Now:           func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) },
					Retry: platformretry.Policy{
						Delays: []time.Duration{0, 0},
						Sleep:  func(context.Context, time.Duration) error { return nil },
					},
				},
			}
		},
	}
}

func (transport *qianchuanRunnerTransport) snapshot() []qianchuanRunnerCall {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]qianchuanRunnerCall(nil), transport.calls...)
}

func qianchuanRunnerTemplateFixture(status string) string {
	return strings.Replace(`{
  "qianchuan_product_template_schema_version": 5,
  "qianchuan_product_templates": {
    "qcpt_fixture": {
      "template_id": "qcpt_fixture",
      "display_name": "巨量千川-1000000000000001-Fixture product-3000000000000001/3000000000000002-商品全域",
      "template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN",
      "status": "STATUS_FIXTURE",
      "bindings": {
        "channel": "qianchuan",
        "advertiser_id": "1000000000000001",
        "product_name": "Fixture product",
        "product_ids": ["3000000000000001", "3000000000000002"]
      },
      "delivery_setting": {
        "smart_bid_type": "SMART_BID_CUSTOM",
        "roi2_goal": 1.7,
        "qcpx_mode": "QCPX_MODE_ON",
        "budget": 5000,
        "video_schedule_type": "SCHEDULE_FROM_NOW",
        "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"
      },
      "plan_name_template": "{product_name}-{creator_name}-{datetime}",
      "material_strategy": {"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false}
    }
  }
}`, "STATUS_FIXTURE", status, 1)
}

func TestRunnerQianchuanReadRuntimeAssemblesTokenAndSDKAdapters(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	stateRoot := filepath.Join(codexHome, "ads-plan-monitor", "state")
	authorizations := filesystem.AuthorizationStore{Root: stateRoot}
	state := map[string]any{
		"generation": 1,
		"authorizations": map[string]any{"qianchuan-auth": map[string]any{
			"token_revision": 1, "pending_account_sync": false,
			"advertiser_ids": []any{"1000000000000001"},
			"authorized_accounts": []any{map[string]any{
				"account_id": "9000000000000001", "advertiser_ids": []any{"1000000000000001"},
			}},
		}},
		"account_index":    map[string]any{"9000000000000001": "qianchuan-auth"},
		"advertiser_index": map[string]any{"1000000000000001": []any{"qianchuan-auth"}},
	}
	if err := authorizations.CommitChannel(context.Background(), "qianchuan", state); err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token": "fixture-qianchuan-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
	}}
	transport := &qianchuanRunnerTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range []string{
		"qc-products list", "qc-products search", "qc-plans list", "qc-plans show", "qc-plans materials",
		"qc-materials authorized-creators", "qc-materials creator-videos",
	} {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) }
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
			QianchuanReads: QianchuanReadRuntime{
				ClientFactory: factory, Now: fixedNow,
				Retry: platformretry.Policy{
					Delays: []time.Duration{0, 0},
					Sleep:  func(context.Context, time.Duration) error { return nil },
				},
			},
		}
	}
	configPath := filepath.Join(root, "synthetic-config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "qianchuan_product_template_schema_version": 5,
  "qianchuan_product_templates": {
    "qcpt_fixture": {
      "template_id": "qcpt_fixture",
      "display_name": "巨量千川-1000000000000001-Fixture product-3000000000000001/3000000000000002-商品全域",
      "template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN",
      "status": "active",
      "bindings": {
        "channel": "qianchuan",
        "advertiser_id": "1000000000000001",
        "product_name": "Fixture product",
        "product_ids": ["3000000000000001", "3000000000000002"]
      },
      "delivery_setting": {
        "smart_bid_type": "SMART_BID_CUSTOM",
        "roi2_goal": 1.7,
        "qcpx_mode": "QCPX_MODE_ON",
        "budget": 5000,
        "video_schedule_type": "SCHEDULE_FROM_NOW",
        "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"
      },
      "plan_name_template": "{product_name}-{creator_name}-{datetime}",
      "material_strategy": {"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false}
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "product-search.json")
	tests := []struct {
		name       string
		args       []string
		mode       string
		countField string
	}{
		{
			name: "product-list", mode: "qianchuan_product_list", countField: "product_count",
			args: []string{"qc-products", "list", "--config", configPath, "--advertiser-id", "1000000000000001"},
		},
		{
			name: "product-search", mode: "qianchuan_product_search", countField: "product_count",
			args: []string{
				"qc-products", "search", "--config", configPath, "--advertiser-id", "1000000000000001",
				"--product-id", "3000000000000001,3000000000000002", "--name", "Fixture", "--out", outputPath,
			},
		},
		{
			name: "plan-list-compact", mode: "qianchuan_plan_list", countField: "plan_count",
			args: []string{"qc-plans", "list", "--config", configPath, "--advertiser-id", "1000000000000001"},
		},
		{
			name: "plan-list-full", mode: "qianchuan_plan_list", countField: "plan_count",
			args: []string{"qc-plans", "list", "--config", configPath, "--advertiser-id", "1000000000000001", "--full"},
		},
		{
			name: "plan-show", mode: "qianchuan_plan_detail",
			args: []string{"qc-plans", "show", "--config", configPath, "--advertiser-id", "1000000000000001", "--ad-id", "2000000000000001", "--full"},
		},
		{
			name: "plan-materials", mode: "qianchuan_plan_materials", countField: "material_count",
			args: []string{"qc-plans", "materials", "--config", configPath, "--advertiser-id", "1000000000000001", "--ad-id", "2000000000000001", "--full"},
		},
		{
			name: "authorized-creators", countField: "creator_count",
			args: []string{
				"qc-materials", "authorized-creators", "--config", configPath,
				"--advertiser-id", "1000000000000001", "--query", "creator001",
			},
		},
		{
			name: "creator-videos", countField: "material_count",
			args: []string{
				"qc-materials", "creator-videos", "--config", configPath,
				"--plan-template", "qcpt_fixture", "--douyin-id", "creator001",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			if code := newRunner(stdout).Execute(context.Background(), test.args); code != 0 {
				t.Fatalf("runtime exit = %d: %s", code, stdout.String())
			}
			result := decodeSingleJSONObject(t, stdout.Bytes())
			if test.mode != "" && result["mode"] != test.mode {
				t.Fatalf("unexpected Runtime result: %#v", result)
			}
			if result["advertiser_id"] != "1000000000000001" {
				t.Fatalf("unexpected Runtime advertiser: %#v", result)
			}
			if test.countField != "" && result[test.countField] != float64(1) {
				t.Fatalf("%s = %#v, want 1", test.countField, result[test.countField])
			}
			if bytes.Contains(stdout.Bytes(), []byte("stats_info")) || bytes.Contains(stdout.Bytes(), []byte("stat_cost")) ||
				bytes.Contains(stdout.Bytes(), []byte("99999999")) || bytes.Contains(stdout.Bytes(), []byte("88888888")) {
				t.Fatalf("SDK finance escaped stable Runtime DTO: %s", stdout.String())
			}
			if test.name == "product-search" {
				written, readErr := os.ReadFile(outputPath)
				if readErr != nil || !bytes.Equal(written, stdout.Bytes()) {
					t.Fatalf("--out differs from stdout: %v", readErr)
				}
			}
			if test.name == "plan-list-compact" || test.name == "plan-list-full" {
				plans := result["plans"].([]any)
				plan := plans[0].(map[string]any)
				_, hasProducts := plan["products"]
				_, hasCreatorIDs := plan["creator_ids"]
				if test.name == "plan-list-compact" && (hasProducts || !hasCreatorIDs) {
					t.Fatalf("compact plan DTO changed: %#v", plan)
				}
				if test.name == "plan-list-full" && (!hasProducts || hasCreatorIDs) {
					t.Fatalf("full plan DTO changed: %#v", plan)
				}
			}
			if test.name == "creator-videos" {
				materials := result["materials"].([]any)
				if len(materials) != 1 || !reflect.DeepEqual(
					materials[0].(map[string]any)["matched_product_ids"],
					[]any{"3000000000000001", "3000000000000002"},
				) {
					t.Fatalf("creator materials were not stably merged by product: %#v", result)
				}
			}
		})
	}
	wantCredentialReads := make([]string, len(tests))
	for index := range wantCredentialReads {
		wantCredentialReads[index] = "oceanengine-auth-qianchuan-qianchuan-auth-r1"
	}
	if credentials.writes != 0 || !reflect.DeepEqual(credentials.reads, wantCredentialReads) {
		t.Fatalf("unexpected Qianchuan credential access: reads=%v writes=%d", credentials.reads, credentials.writes)
	}
	transport.mu.Lock()
	calls := append([]qianchuanRunnerCall(nil), transport.calls...)
	transport.mu.Unlock()
	if len(calls) != 10 {
		t.Fatalf("Qianchuan Runtime SDK calls = %d, want 10: %#v", len(calls), calls)
	}
	wantPaths := []string{
		"/open_api/v1.0/qianchuan/uni_promotion/product/get/",
		"/open_api/v1.0/qianchuan/uni_promotion/product/get/",
		"/open_api/v1.0/qianchuan/uni_promotion/list/",
		"/open_api/v1.0/qianchuan/uni_promotion/list/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/detail/",
		"/open_api/v1.0/qianchuan/uni_promotion/ad/material/get/",
		"/open_api/v1.0/qianchuan/uni_aweme/authorized/get/",
		"/open_api/v1.0/qianchuan/uni_aweme/authorized/get/",
		"/open_api/v1.0/qianchuan/file/video/aweme/get/",
		"/open_api/v1.0/qianchuan/file/video/aweme/get/",
	}
	wantHosts := []string{
		oceanengine.BusinessHost, oceanengine.BusinessHost, oceanengine.BusinessHost,
		oceanengine.BusinessHost, oceanengine.BusinessHost, oceanengine.BusinessHost,
		oceanengine.BusinessHost, oceanengine.BusinessHost,
		oceanengine.OAuthHost, oceanengine.OAuthHost,
	}
	for index, call := range calls {
		if call.Path != wantPaths[index] || call.Host != wantHosts[index] || call.Token != "fixture-qianchuan-token" {
			t.Fatalf("Qianchuan Runtime SDK call %d changed: %#v", index, call)
		}
	}
	planCall := calls[2]
	if planCall.Query.Get("start_time") != "2026-07-25 00:00:00" ||
		planCall.Query.Get("end_time") != "2026-07-25 23:59:59" ||
		planCall.Query.Get("page") != "1" || planCall.Query.Get("page_size") != "100" {
		t.Fatalf("Qianchuan Runtime plan-list default changed: %#v", planCall)
	}
	for _, creatorCall := range calls[6:8] {
		var filtering map[string]any
		if err := json.Unmarshal([]byte(creatorCall.Query.Get("filtering")), &filtering); err != nil ||
			filtering["marketing_goal"] != "VIDEO_PROM_GOODS" || filtering["scene"] != "CREATE" ||
			filtering["search_key_words"] != "creator001" {
			t.Fatalf("authorized creator filtering changed: call=%#v filter=%#v err=%v", creatorCall, filtering, err)
		}
	}
	queriedProducts := make([]string, 0, 2)
	for _, videoCall := range calls[8:10] {
		var filtering map[string]any
		if err := json.Unmarshal([]byte(videoCall.Query.Get("filtering")), &filtering); err != nil {
			t.Fatalf("invalid creator video filtering: %v", err)
		}
		queriedProducts = append(queriedProducts, strconv.FormatInt(int64(filtering["product_id"].(float64)), 10))
		if videoCall.Query.Get("advertiser_id") != "1000000000000001" ||
			videoCall.Query.Get("aweme_id") != "4000000000000001" || videoCall.Query.Get("count") != "50" {
			t.Fatalf("creator video request changed: %#v", videoCall)
		}
	}
	if !reflect.DeepEqual(queriedProducts, []string{"3000000000000001", "3000000000000002"}) {
		t.Fatalf("template products were not queried once in order: %v", queriedProducts)
	}
}

func TestRunQianchuanReadRejectsInvalidArgumentsBeforeService(t *testing.T) {
	stub := &qianchuanReadServiceStub{}
	for _, test := range []struct {
		domain string
		action string
		args   []string
	}{
		{domain: "qc-products", action: "list", args: []string{"--advertiser-id", "bad"}},
		{domain: "qc-products", action: "search", args: []string{"--advertiser-id", "1000000000000001"}},
		{domain: "qc-plans", action: "show", args: []string{"--advertiser-id", "1000000000000001"}},
		{domain: "qc-plans", action: "list", args: []string{"--advertiser-id", "1000000000000001", "--top", "-1"}},
	} {
		stdout := new(bytes.Buffer)
		if code := RunQianchuanRead(context.Background(), test.domain, test.action, test.args, stub, stdout); code != 2 {
			t.Fatalf("%s %s invalid exit = %d: %s", test.domain, test.action, code, stdout.String())
		}
		if decodeSingleJSONObject(t, stdout.Bytes())["ok"] != false {
			t.Fatalf("invalid arguments did not return a JSON error: %s", stdout.String())
		}
	}
	if stub.calls != 0 {
		t.Fatalf("invalid Qianchuan arguments reached service %d times", stub.calls)
	}
}

func TestDefaultRouteUsesQianchuanReadsGo(t *testing.T) {
	for _, command := range []string{
		"qc-products list", "qc-products search", "qc-plans list", "qc-plans show", "qc-plans materials",
		"qc-materials authorized-creators", "qc-materials creator-videos",
	} {
		runtime, ok := application.DefaultRouteManifest().RouteFor(command)
		if !ok || runtime != application.RuntimeGo {
			t.Fatalf("default %s route = %q, want Go", command, runtime)
		}
	}
}

type qianchuanReadServiceStub struct {
	calls int
}

func (stub *qianchuanReadServiceStub) QueryProducts(
	context.Context,
	applicationqianchuan.ProductQuery,
	string,
) (applicationqianchuan.ProductResult, error) {
	stub.calls++
	return applicationqianchuan.ProductResult{}, nil
}

func (stub *qianchuanReadServiceStub) ListPlans(
	context.Context,
	applicationqianchuan.PlanListQuery,
) (applicationqianchuan.PlanListResult, error) {
	stub.calls++
	return applicationqianchuan.PlanListResult{}, nil
}

func (stub *qianchuanReadServiceStub) ShowPlan(
	context.Context,
	applicationqianchuan.CredentialScope,
	string,
) (applicationqianchuan.PlanDetailResult, error) {
	stub.calls++
	return applicationqianchuan.PlanDetailResult{}, nil
}

func (stub *qianchuanReadServiceStub) ListPlanMaterials(
	context.Context,
	applicationqianchuan.PlanMaterialsQuery,
) (applicationqianchuan.PlanMaterialsResult, error) {
	stub.calls++
	return applicationqianchuan.PlanMaterialsResult{}, nil
}

func (stub *qianchuanReadServiceStub) ListAuthorizedCreators(
	context.Context,
	applicationqianchuan.AuthorizedCreatorQuery,
) (applicationqianchuan.AuthorizedCreatorResult, error) {
	stub.calls++
	return applicationqianchuan.AuthorizedCreatorResult{}, nil
}

func (stub *qianchuanReadServiceStub) QueryCreatorVideos(
	context.Context,
	applicationqianchuan.CreatorVideoQuery,
) (applicationqianchuan.CreatorVideoResult, error) {
	stub.calls++
	return applicationqianchuan.CreatorVideoResult{}, nil
}
