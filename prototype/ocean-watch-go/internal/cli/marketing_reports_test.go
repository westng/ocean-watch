package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	applicationreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/reports"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

const (
	marketingReportCLIAdvertiserID = "1000000000000001"
	marketingReportCLIAuthAccount  = "9000000000000001"
	marketingReportCLIProjectID    = "9007199254740991"
	marketingReportCLIPromotionID  = "9007199254740993"
	marketingReportCLIMaterialID   = "9007199254740995"
	marketingReportCLIToken        = "TEST_MARKETING_REPORT_ACCESS_TOKEN_DO_NOT_USE"
)

type marketingReportServiceSpy struct {
	schemaQueries   []applicationreports.MarketingSchemaQuery
	customQueries   []applicationreports.MarketingCustomQuery
	planQueries     []applicationreports.MarketingPlanQuery
	materialQueries []applicationreports.MarketingMaterialQuery
}

func (spy *marketingReportServiceSpy) Schema(
	_ context.Context,
	query applicationreports.MarketingSchemaQuery,
) (applicationreports.MarketingSchemaResult, error) {
	spy.schemaQueries = append(spy.schemaQueries, query)
	return applicationreports.MarketingSchemaResult{
		Endpoint: applicationreports.MarketingSchemaEndpoint,
		Params: map[string]any{
			"advertiser_id": query.AdvertiserID,
			"data_topics":   append([]string(nil), query.DataTopics...),
		},
		Topics: []applicationreports.MarketingSchemaTopic{},
	}, nil
}

func (spy *marketingReportServiceSpy) Custom(
	_ context.Context,
	query applicationreports.MarketingCustomQuery,
) (applicationreports.MarketingCustomResult, error) {
	spy.customQueries = append(spy.customQueries, query)
	return applicationreports.MarketingCustomResult{
		Endpoint: applicationreports.MarketingReportEndpoint,
		FlatRows: []map[string]any{{
			"material_id": marketingReportCLIMaterialID,
			"stat_cost":   "1.005",
		}},
	}, nil
}

func (spy *marketingReportServiceSpy) Plans(
	_ context.Context,
	query applicationreports.MarketingPlanQuery,
) (applicationreports.MarketingPlanResult, error) {
	spy.planQueries = append(spy.planQueries, query)
	return applicationreports.MarketingPlanResult{
		Mode: "marketing_plan_report", AdvertiserID: query.AdvertiserID,
		Rows: []map[string]any{{"project_id": marketingReportCLIProjectID, "stat_cost": "1.005"}},
	}, nil
}

func (spy *marketingReportServiceSpy) Materials(
	_ context.Context,
	query applicationreports.MarketingMaterialQuery,
) (applicationreports.MarketingMaterialResult, error) {
	spy.materialQueries = append(spy.materialQueries, query)
	return applicationreports.MarketingMaterialResult{
		Mode: "unit_materials_report",
		Rows: []map[string]any{{
			"project_id": marketingReportCLIProjectID, "promotion_id": marketingReportCLIPromotionID,
			"material_id": marketingReportCLIMaterialID, "stat_cost": "1.005",
		}},
	}, nil
}

type marketingReportConfigReaderSpy struct {
	path  string
	mu    *sync.Mutex
	reads *int
}

func (reader marketingReportConfigReaderSpy) Read(ctx context.Context) (map[string]any, error) {
	reader.mu.Lock()
	*reader.reads++
	reader.mu.Unlock()
	return (filesystem.ConfigStore{Path: reader.path}).Read(ctx)
}

func writeMarketingReportCLIConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "marketing-report-config.json")
	payload := []byte(`{
  "config_schema_version": 2,
  "default_channel": "marketing",
  "channels": {"marketing": {"api": {"base_url": "https://api.oceanengine.com/open_api"}}},
  "account": {"channel": "marketing", "advertiser_id": "1000000000000001"},
  "future_field": {"must_remain": true}
}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func marketingReportGoManifest(t *testing.T) application.RouteManifest {
	t.Helper()
	routes := application.DefaultRouteManifest().Snapshot()
	for _, command := range []string{
		"reports schema", "reports custom", "reports plans", "reports materials",
	} {
		routes[command] = application.RuntimeGo
	}
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestRunnerMarketingReportMapsCompletePythonArguments(t *testing.T) {
	root := t.TempDir()
	configPath := writeMarketingReportCLIConfig(t, root)
	service := &marketingReportServiceSpy{}
	fallback := &fallbackSpy{code: 99}
	var configMu sync.Mutex
	configReads := 0
	runner := Runner{
		Routes: marketingReportGoManifest(t), Fallback: fallback,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		MarketingReports: MarketingReportRuntime{
			Service: service,
			Now: func() time.Time {
				return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC)
			},
			ConfigFactory: func(path string) MarketingReportConfigReader {
				return marketingReportConfigReaderSpy{path: path, mu: &configMu, reads: &configReads}
			},
		},
	}
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "schema",
			args: []string{
				"reports", "schema", "--config", configPath,
				"--data-topic", "MATERIAL_DATA,BASIC_DATA", "--data-topic", "UNI_PROJECT_DATA",
				"--full", "--channel", "marketing", "--auth-account-id", marketingReportCLIAuthAccount,
			},
		},
		{
			name: "custom",
			args: []string{
				"reports", "custom", "--config", configPath,
				"--data-topic", "MATERIAL_DATA",
				"--dimension", "material_id,cdp_promotion_id", "--dimension", "cdp_promotion_name",
				"--metric", "stat_cost,in_app_order_gmv", "--metric", "in_app_order_roi",
				"--filter", `{"field":"material_id","type":2,"operator":1,"values":["9007199254740995",9007199254740997]}`,
				"--filter", "cdp_promotion_id:2:1:9007199254740993,9007199254740999",
				"--start-time", "2026-07-20 01:02:03", "--end-time", "2026-07-21 22:23:24",
				"--start-date", "2026-07-01", "--end-date", "2026-07-02",
				"--order-field", "in_app_order_gmv", "--order-type", "ASC",
				"--page", "3", "--page-size", "77", "--channel", "marketing",
				"--auth-account-id", marketingReportCLIAuthAccount,
				"--csv-out", filepath.Join(root, "custom.csv"),
			},
		},
		{
			name: "plans",
			args: []string{
				"reports", "plans", "--config", configPath,
				"--advertiser-id", "1000000000000002", "--auth-account-id", marketingReportCLIAuthAccount,
				"--start-date", "2026-07-20", "--end-date", "2026-07-21",
				"--metric", "stat_cost,in_app_order_gmv", "--metric", "in_app_order_roi",
				"--page-size", "66", "--max-pages", "123", "--top", "0",
			},
		},
		{
			name: "materials",
			args: []string{
				"reports", "materials", "--config", configPath,
				"--advertiser-id", "1000000000000003", "--auth-account-id", marketingReportCLIAuthAccount,
				"--start-date", "2026-07-22", "--end-date", "2026-07-23",
				"--data-topic", "MATERIAL_DATA", "--dimension", "material_id,cdp_promotion_id",
				"--metric", "stat_cost,in_app_order_gmv", "--filter-material-ids",
				"--include-extra-report-materials", "--project-id", marketingReportCLIProjectID,
				"--promotion-id", marketingReportCLIPromotionID + ",9007199254740997",
				"--active-only", "--include-non-active", "--promotion-page", "2",
				"--promotion-page-size", "33", "--report-page", "4", "--report-page-size", "88",
				"--single-page", "--order-field", "in_app_order_gmv", "--order-type", "ASC",
				"--channel", "marketing", "--csv-out", filepath.Join(root, "materials.csv"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(root, test.name+".json")
			args := append(append([]string(nil), test.args...), "--out", outputPath)
			stdout := new(bytes.Buffer)
			runner.Stdout = stdout
			if code := runner.Execute(context.Background(), args); code != 0 {
				t.Fatalf("%s exit = %d: %s", test.name, code, stdout.String())
			}
			written, err := os.ReadFile(outputPath)
			if err != nil || !bytes.Equal(written, stdout.Bytes()) {
				t.Fatalf("%s --out differs from stdout: %v", test.name, err)
			}
		})
	}
	if fallback.args != nil || configReads != len(tests) {
		t.Fatalf("report mapping crossed runtime boundary: fallback=%v config_reads=%d", fallback.args, configReads)
	}
	if len(service.schemaQueries) != 1 || len(service.customQueries) != 1 ||
		len(service.planQueries) != 1 || len(service.materialQueries) != 1 {
		t.Fatalf("report service calls changed: %#v", service)
	}
	schema := service.schemaQueries[0]
	if schema.AdvertiserID != marketingReportCLIAdvertiserID || schema.AuthAccountID != marketingReportCLIAuthAccount ||
		!schema.Full || !reflect.DeepEqual(schema.DataTopics, []string{
		"MATERIAL_DATA", "BASIC_DATA", "UNI_PROJECT_DATA",
	}) {
		t.Fatalf("schema arguments changed: %#v", schema)
	}
	custom := service.customQueries[0]
	if custom.AdvertiserID != marketingReportCLIAdvertiserID || custom.AuthAccountID != marketingReportCLIAuthAccount ||
		custom.DataTopic != "MATERIAL_DATA" ||
		!reflect.DeepEqual(custom.Dimensions, []string{"material_id", "cdp_promotion_id", "cdp_promotion_name"}) ||
		!reflect.DeepEqual(custom.Metrics, []string{"stat_cost", "in_app_order_gmv", "in_app_order_roi"}) ||
		custom.StartTime != "2026-07-20 01:02:03" || custom.EndTime != "2026-07-21 22:23:24" ||
		custom.OrderField != "in_app_order_gmv" || custom.OrderType != "ASC" ||
		custom.Page != 3 || custom.PageSize != 77 || len(custom.Filters) != 2 ||
		!reflect.DeepEqual(custom.Filters[0], applicationreports.MarketingFilter{
			Field: "material_id", Type: 2, Operator: 1,
			Values: []string{marketingReportCLIMaterialID, "9007199254740997"},
		}) || !reflect.DeepEqual(custom.Filters[1], applicationreports.MarketingFilter{
		Field: "cdp_promotion_id", Type: 2, Operator: 1,
		Values: []string{marketingReportCLIPromotionID, "9007199254740999"},
	}) {
		t.Fatalf("custom arguments changed: %#v", custom)
	}
	plans := service.planQueries[0]
	if plans.AdvertiserID != "1000000000000002" || plans.AuthAccountID != marketingReportCLIAuthAccount ||
		plans.StartDate != "2026-07-20" || plans.EndDate != "2026-07-21" ||
		!reflect.DeepEqual(plans.Metrics, []string{"stat_cost", "in_app_order_gmv", "in_app_order_roi"}) ||
		plans.PageSize != 66 || plans.MaxPages != 123 || plans.Top != 0 {
		t.Fatalf("plan arguments changed: %#v", plans)
	}
	materials := service.materialQueries[0]
	if materials.AdvertiserID != "1000000000000003" || materials.AuthAccountID != marketingReportCLIAuthAccount ||
		materials.StartDate != "2026-07-22" || materials.EndDate != "2026-07-23" ||
		materials.DataTopic != "MATERIAL_DATA" ||
		!reflect.DeepEqual(materials.Dimensions, []string{"material_id", "cdp_promotion_id"}) ||
		!reflect.DeepEqual(materials.Metrics, []string{"stat_cost", "in_app_order_gmv"}) ||
		!materials.IncludeExtraReportMaterials || materials.ProjectID != marketingReportCLIProjectID ||
		!reflect.DeepEqual(materials.PromotionIDs, []string{marketingReportCLIPromotionID, "9007199254740997"}) ||
		!materials.ActiveOnly || materials.PromotionPage != 2 || materials.PromotionPageSize != 33 ||
		materials.ReportPage != 4 || materials.ReportPageSize != 88 || !materials.SinglePage ||
		materials.OrderField != "in_app_order_gmv" || materials.OrderType != "ASC" {
		t.Fatalf("material arguments changed: %#v", materials)
	}
}

type marketingReportRunnerCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type marketingReportRunnerTransport struct {
	mu    sync.Mutex
	calls []marketingReportRunnerCall
}

func (transport *marketingReportRunnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := marketingReportRunnerCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, call)
	transport.mu.Unlock()
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch call.Path {
	case "/open_api/v3.0/report/custom/config/get/":
		if strings.Contains(call.Query.Get("data_topics"), applicationreports.MarketingPlanTopic) {
			body = `{"code":0,"message":"OK","request_id":"plan-schema","data":{"list":[{"data_topic":"UNI_PROJECT_DATA","dimensions":[{"field":"project_id","name":"Project ID"},{"field":"project_name","name":"Project name"}],"metrics":[{"field":"stat_cost","name":"Spend"},{"field":"in_app_order_gmv","name":"GMV"}]}]}}`
		} else {
			body = `{"code":0,"message":"OK","request_id":"material-schema","data":{"list":[{"data_topic":"MATERIAL_DATA","dimensions":[{"field":"material_id","name":"Material ID"},{"field":"cdp_promotion_id","name":"Promotion ID"},{"field":"cdp_promotion_name","name":"Promotion name"}],"metrics":[{"field":"stat_cost","name":"Spend"},{"field":"in_app_order_gmv","name":"GMV"}]}]}}`
		}
	case "/open_api/v3.0/report/custom/get/":
		if call.Query.Get("data_topic") == applicationreports.MarketingPlanTopic {
			body = `{"code":0,"message":"OK","request_id":"plan-report","data":{"rows":[{"dimensions":{"project_id":"9007199254740991","project_name":"Fixture project"},"metrics":{"stat_cost":"1.005","in_app_order_gmv":"2.5"}}],"total_metrics":{"stat_cost":"1.005","in_app_order_gmv":"2.5"},"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
		} else {
			body = `{"code":0,"message":"OK","request_id":"material-report","data":{"rows":[{"dimensions":{"material_id":"9007199254740995","cdp_promotion_id":"9007199254740993","cdp_promotion_name":"Fixture promotion"},"metrics":{"stat_cost":"1.005"}}],"total_metrics":{"stat_cost":"1.005"},"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
		}
	case "/open_api/v3.0/promotion/list/":
		body = `{"code":0,"message":"OK","request_id":"promotion-list","data":{"list":[{"advertiser_id":1000000000000001,"project_id":9007199254740991,"promotion_id":9007199254740993,"promotion_name":"Fixture promotion","status":"ENABLE","status_first":"RUNNING","status_second":[],"opt_status":"ENABLE","promotion_materials":{"video_material_list":[{"material_id":9007199254740995,"video_id":"fixture-video","video_cover_id":"fixture-cover","material_status":"MATERIAL_STATUS_OK","material_opt_status":"ENABLE","image_mode":"CREATIVE_IMAGE_MODE_VIDEO","create_time":"2026-07-25 08:00:00"}]} }],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func (transport *marketingReportRunnerTransport) snapshot() []marketingReportRunnerCall {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]marketingReportRunnerCall(nil), transport.calls...)
}

func TestRunnerMarketingReportShadowUsesGeneratedSDKAndIsolatedCredentials(t *testing.T) {
	root := t.TempDir()
	configPath := writeMarketingReportCLIConfig(t, root)
	codexHome := filepath.Join(root, "codex-home")
	stateRoot := filepath.Join(codexHome, "ads-plan-monitor", "state")
	if err := (filesystem.AuthorizationStore{Root: stateRoot}).CommitChannel(context.Background(), "marketing", map[string]any{
		"generation": 1,
		"authorizations": map[string]any{"marketing-auth": map[string]any{
			"token_revision": 1, "pending_account_sync": false,
			"advertiser_ids": []any{marketingReportCLIAdvertiserID},
			"authorized_accounts": []any{map[string]any{
				"account_id":     marketingReportCLIAuthAccount,
				"advertiser_ids": []any{marketingReportCLIAdvertiserID},
			}},
		}},
		"account_index":    map[string]any{marketingReportCLIAuthAccount: "marketing-auth"},
		"advertiser_index": map[string]any{marketingReportCLIAdvertiserID: []any{"marketing-auth"}},
	}); err != nil {
		t.Fatal(err)
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-marketing-marketing-auth-r1": {
			"access_token": marketingReportCLIToken, "access_token_expires_at": "2026-07-27T00:00:00Z",
		},
		"oceanengine-auth-qianchuan-qianchuan-decoy-r1": {
			"access_token": "TEST_QIANCHUAN_DECOY_DO_NOT_USE", "access_token_expires_at": "2026-07-27T00:00:00Z",
		},
	}}
	transport := &marketingReportRunnerTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) }
	fallback := &fallbackSpy{code: 99}
	var configMu sync.Mutex
	configReads := 0
	newRunner := func(stdout io.Writer) Runner {
		return Runner{
			Routes: marketingReportGoManifest(t), Fallback: fallback, Stdout: stdout,
			Cwd: root, UserHome: root, Credentials: credentials,
			Getenv: func(name string) string {
				if name == "CODEX_HOME" {
					return codexHome
				}
				return ""
			},
			MarketingReports: MarketingReportRuntime{
				ClientFactory: factory, Now: fixedNow,
				Retry: platformretry.Policy{
					Delays: []time.Duration{},
					Sleep:  func(context.Context, time.Duration) error { return nil },
				},
				ConfigFactory: func(path string) MarketingReportConfigReader {
					return marketingReportConfigReaderSpy{path: path, mu: &configMu, reads: &configReads}
				},
			},
		}
	}
	customCSV := filepath.Join(root, "shadow-custom.csv")
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, map[string]any)
	}{
		{
			name: "schema", args: []string{"reports", "schema", "--config", configPath},
			assert: func(t *testing.T, result map[string]any) {
				params := result["params"].(map[string]any)
				if params["advertiser_id"] != marketingReportCLIAdvertiserID {
					t.Fatalf("schema advertiser changed: %#v", result)
				}
			},
		},
		{
			name: "custom", args: []string{
				"reports", "custom", "--config", configPath,
				"--dimension", "material_id,cdp_promotion_id,cdp_promotion_name", "--metric", "stat_cost",
				"--csv-out", customCSV,
			},
			assert: func(t *testing.T, result map[string]any) {
				row := result["flat_rows"].([]any)[0].(map[string]any)
				if row["material_id"] != marketingReportCLIMaterialID || row["stat_cost"] != "1.005" {
					t.Fatalf("custom report lost exact values: %#v", row)
				}
			},
		},
		{
			name: "plans", args: []string{
				"reports", "plans", "--config", configPath,
				"--metric", "stat_cost,in_app_order_gmv", "--top", "0",
			},
			assert: func(t *testing.T, result map[string]any) {
				dateRange := result["date_range"].(map[string]any)
				row := result["rows"].([]any)[0].(map[string]any)
				if dateRange["start_date"] != "2026-07-25" || dateRange["end_date"] != "2026-07-25" ||
					row["project_id"] != marketingReportCLIProjectID {
					t.Fatalf("plan defaults or exact ID changed: %#v", result)
				}
			},
		},
		{
			name: "materials", args: []string{
				"reports", "materials", "--config", configPath,
				"--dimension", "material_id,cdp_promotion_id", "--metric", "stat_cost",
			},
			assert: func(t *testing.T, result map[string]any) {
				dateRange := result["date_range"].(map[string]any)
				row := result["rows"].([]any)[0].(map[string]any)
				if dateRange["start_date"] != "2026-07-25" || dateRange["end_date"] != "2026-07-25" ||
					row["promotion_id"] != marketingReportCLIPromotionID ||
					row["material_id"] != marketingReportCLIMaterialID || row["stat_cost"] != "1.005" {
					t.Fatalf("material join or exact IDs changed: %#v", result)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(root, "shadow-"+test.name+".json")
			args := append(append([]string(nil), test.args...), "--out", outputPath)
			stdout := new(bytes.Buffer)
			if code := newRunner(stdout).Execute(context.Background(), args); code != 0 {
				t.Fatalf("%s Shadow exit = %d: %s", test.name, code, stdout.String())
			}
			written, readErr := os.ReadFile(outputPath)
			if readErr != nil || !bytes.Equal(written, stdout.Bytes()) {
				t.Fatalf("%s --out differs from stdout: %v", test.name, readErr)
			}
			test.assert(t, decodeSingleJSONObject(t, stdout.Bytes()))
		})
	}
	if fallback.args != nil || configReads != len(tests) {
		t.Fatalf("Marketing report Shadow boundary changed: fallback=%v config_reads=%d", fallback.args, configReads)
	}
	wantCredentialReads := []string{
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-marketing-marketing-auth-r1",
	}
	if credentials.writes != 0 || !reflect.DeepEqual(credentials.reads, wantCredentialReads) {
		t.Fatalf("Marketing report credential isolation changed: reads=%v writes=%d", credentials.reads, credentials.writes)
	}
	calls := transport.snapshot()
	wantPaths := []string{
		"/open_api/v3.0/report/custom/config/get/",
		"/open_api/v3.0/report/custom/get/",
		"/open_api/v3.0/report/custom/config/get/",
		"/open_api/v3.0/report/custom/get/",
		"/open_api/v3.0/promotion/list/",
		"/open_api/v3.0/report/custom/get/",
	}
	if len(calls) != len(wantPaths) {
		t.Fatalf("Marketing report SDK calls = %d: %#v", len(calls), calls)
	}
	for index, call := range calls {
		if call.Method != http.MethodGet || call.Host != oceanengine.BusinessHost ||
			call.Path != wantPaths[index] || call.Token != marketingReportCLIToken ||
			call.Query.Get("advertiser_id") != marketingReportCLIAdvertiserID {
			t.Fatalf("Marketing generated SDK call %d changed: %#v", index, call)
		}
		if call.Path == "/open_api/v3.0/report/custom/get/" &&
			(call.Query.Get("start_time") != "2026-07-25 00:00:00" ||
				call.Query.Get("end_time") != "2026-07-25 23:59:59") {
			t.Fatalf("Marketing report default date changed: %#v", call)
		}
	}
	csvPayload, err := os.ReadFile(customCSV)
	if err != nil || !bytes.HasPrefix(csvPayload, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatalf("custom CSV BOM changed: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(csvPayload[3:])).ReadAll()
	if err != nil || !reflect.DeepEqual(rows, [][]string{
		{"material_id", "cdp_promotion_id", "cdp_promotion_name", "stat_cost"},
		{marketingReportCLIMaterialID, marketingReportCLIPromotionID, "Fixture promotion", "1.005"},
	}) {
		t.Fatalf("custom CSV columns or values changed: rows=%v err=%v", rows, err)
	}
}

func TestRunnerMarketingReportRejectsInvalidArgumentsBeforeState(t *testing.T) {
	root := t.TempDir()
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{}}
	fallback := &fallbackSpy{code: 99}
	service := &marketingReportServiceSpy{}
	configReads := 0
	var configMu sync.Mutex
	runner := Runner{
		Routes: marketingReportGoManifest(t), Fallback: fallback, Credentials: credentials,
		Cwd: root, UserHome: root, Getenv: func(string) string { return "" },
		MarketingReports: MarketingReportRuntime{
			Service: service,
			ConfigFactory: func(path string) MarketingReportConfigReader {
				return marketingReportConfigReaderSpy{path: path, mu: &configMu, reads: &configReads}
			},
		},
	}
	for _, args := range [][]string{
		{"reports", "schema", "--channel", "qianchuan"},
		{"reports", "custom", "--filter", "invalid"},
		{"reports", "plans", "--advertiser-id", "not-an-id"},
		{"reports", "materials", "--promotion-id", "9007199254740993,bad"},
	} {
		stdout := new(bytes.Buffer)
		runner.Stdout = stdout
		if code := runner.Execute(context.Background(), args); code != 2 {
			t.Fatalf("invalid %v exit = %d: %s", args, code, stdout.String())
		}
		if decodeSingleJSONObject(t, stdout.Bytes())["ok"] != false {
			t.Fatalf("invalid report arguments did not return JSON error: %s", stdout.String())
		}
	}
	serviceCalls := len(service.schemaQueries) + len(service.customQueries) +
		len(service.planQueries) + len(service.materialQueries)
	if configReads != 0 || len(credentials.reads) != 0 || credentials.writes != 0 ||
		serviceCalls != 0 || fallback.args != nil {
		t.Fatalf("invalid reports crossed boundary: config=%d credentials=%v/%d service=%d fallback=%v",
			configReads, credentials.reads, credentials.writes, serviceCalls, fallback.args)
	}
}

func TestMarketingReportCSVEmptyResultMatchesPythonShape(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "exports")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "empty.csv")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeMarketingReportCSV(path, nil, []string{"must_not_be_written"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(payload, []byte{0xef, 0xbb, 0xbf}) ||
		strings.TrimSpace(string(payload[3:])) != "" {
		t.Fatalf("empty CSV compatibility changed: %q", payload)
	}
	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("CSV destination mode is %o", fileInfo.Mode().Perm())
		}
		parentInfo, err := os.Stat(parent)
		if err != nil {
			t.Fatal(err)
		}
		if parentInfo.Mode().Perm() != 0o700 {
			t.Fatalf("CSV destination directory mode is %o", parentInfo.Mode().Perm())
		}
	}
}

func TestMarketingReportFilterJSONPreservesHighNumbersAsStrings(t *testing.T) {
	filter, err := parseMarketingFilter(
		`{"field":"material_id","type":2,"operator":1,"values":[9007199254740993,"9007199254740995"]}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(filter.Values, []string{marketingReportCLIPromotionID, marketingReportCLIMaterialID}) {
		t.Fatalf("JSON filter lost high IDs: %#v", filter)
	}
}

func TestDefaultRouteKeepsMarketingReportsOnPython(t *testing.T) {
	for _, command := range []string{
		"reports schema", "reports custom", "reports plans", "reports materials",
	} {
		runtime, ok := application.DefaultRouteManifest().RouteFor(command)
		if !ok || runtime != application.RuntimePython {
			t.Fatalf("default %s route = %q, want Python", command, runtime)
		}
	}
}
