package cli

import (
	"bytes"
	"context"
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
	applicationreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/reports"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

type qianchuanReportRunnerCall struct {
	Host  string
	Path  string
	Query url.Values
	Token string
}

type qianchuanReportRunnerTransport struct {
	mu    sync.Mutex
	calls []qianchuanReportRunnerCall
}

func (transport *qianchuanReportRunnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := qianchuanReportRunnerCall{
		Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, call)
	transport.mu.Unlock()
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch call.Path {
	case "/open_api/v1.0/qianchuan/report/uni_promotion/config/get/":
		metrics := make([]string, 0, len(applicationreports.DefaultPlanFields))
		for _, field := range applicationreports.DefaultPlanFields {
			metrics = append(metrics, `{"field":"`+field+`"}`)
		}
		body = `{"code":0,"message":"OK","request_id":"schema-request","data":{"custom_config_datas":[{"data_topic":"SITE_PROMOTION_PRODUCT_AD","dimensions":[{"field":"ad_id"}],"metrics":[` + strings.Join(metrics, ",") + `]}]}}`
	case "/open_api/v1.0/qianchuan/report/uni_promotion/data/get/":
		metrics := []string{
			`"stat_cost":{"Value":5,"ValueStr":"500000"}`,
			`"total_pay_order_count_for_roi2":{"Value":2,"ValueStr":"200000"}`,
			`"total_pay_order_gmv_include_coupon_for_roi2":{"Value":10,"ValueStr":"1000000"}`,
			`"total_prepay_and_pay_order_roi2":{"Value":2,"ValueStr":"200000"}`,
			`"total_order_settle_amount_for_roi2_1h":{"Value":9,"ValueStr":"900000"}`,
			`"total_order_settle_count_for_roi2_1h":{"Value":2,"ValueStr":"200000"}`,
			`"total_prepay_and_pay_settle_roi2_1h":{"Value":1.8,"ValueStr":"180000"}`,
		}
		body = `{"code":0,"message":"OK","request_id":"data-request","data":{"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1},"rows":[{"dimensions":{"ad_id":{"Value":2000000000000000,"ValueStr":"2000000000000001"}},"metrics":{` + strings.Join(metrics, ",") + `}}]}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/list/":
		body = `{"code":0,"message":"OK","request_id":"metadata-request","data":{"page_info":{"page":1,"page_size":100,"total_page":1,"total_num":1},"ad_list":[{"ad_info":{"id":2000000000000001,"name":"Fixture plan"},"stats_info":{"stat_cost":99999999.99}}]}}`
	case "/open_api/v1.0/qianchuan/report/material/get/":
		body = `{"code":0,"message":"OK","request_id":"material-request","data":{"list":[{"advertiser_id":1000000000000001,"material_id":5000000000000001,"material_type":"video","fields":{"stat_cost":1.25,"pay_order_amount":2.5,"pay_order_count":1}}],"page_info":{"page":1,"total_page":1,"total_number":1}}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func (transport *qianchuanReportRunnerTransport) snapshot() []qianchuanReportRunnerCall {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]qianchuanReportRunnerCall(nil), transport.calls...)
}

func TestRunnerQianchuanReportShadowContracts(t *testing.T) {
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
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token": "fixture-qianchuan-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
		"oceanengine-auth-marketing-decoy-r1": {
			"access_token": "fixture-marketing-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
	}}
	transport := &qianchuanReportRunnerTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	routes := application.DefaultRouteManifest().Snapshot()
	routes["qc-reports plans"] = application.RuntimeGo
	routes["qc-reports materials"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	fallback := &fallbackSpy{code: 99}
	fixedNow := func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) }
	newRunner := func(stdout io.Writer) Runner {
		return Runner{
			Routes: manifest, Fallback: fallback, Stdout: stdout, Cwd: root, UserHome: root,
			Getenv: func(name string) string {
				if name == "CODEX_HOME" {
					return codexHome
				}
				return ""
			},
			Credentials: credentials,
			QianchuanReports: QianchuanReportRuntime{
				ClientFactory: factory, Now: fixedNow,
				Retry: platformretry.Policy{
					Delays: []time.Duration{0, 0},
					Sleep:  func(context.Context, time.Duration) error { return nil },
				},
			},
		}
	}
	planOutput := new(bytes.Buffer)
	if code := newRunner(planOutput).Execute(context.Background(), []string{
		"qc-reports", "plans", "--advertiser-id", "1000000000000001",
	}); code != 0 {
		t.Fatalf("plan report Shadow exit = %d: %s", code, planOutput.String())
	}
	planResult := decodeSingleJSONObject(t, planOutput.Bytes())
	if planResult["channel"] != "qianchuan" || planResult["transport"] != "official_sdk_rest" ||
		planResult["advertiser_id"] != "1000000000000001" {
		t.Fatalf("plan report Shadow result changed: %#v", planResult)
	}
	dateRange := planResult["date_range"].(map[string]any)
	if dateRange["start_date"] != "2026-07-25" || dateRange["end_date"] != "2026-07-25" {
		t.Fatalf("plan report default date changed: %#v", dateRange)
	}
	presentation := planResult["presentation"].(map[string]any)
	if presentation["required"] != true || len(presentation["columns"].([]any)) != 15 ||
		!strings.HasPrefix(presentation["rendered_markdown"].(string), "| 排名 | 计划 | 达人 | 商品 |") {
		t.Fatalf("plan report mandatory presentation changed: %#v", presentation)
	}
	materialOutputPath := filepath.Join(root, "qianchuan-material-report.json")
	materialOutput := new(bytes.Buffer)
	if code := newRunner(materialOutput).Execute(context.Background(), []string{
		"qc-reports", "materials", "--advertiser-id", "1000000000000001", "--top", "1",
		"--out", materialOutputPath,
	}); code != 0 {
		t.Fatalf("material report Shadow exit = %d: %s", code, materialOutput.String())
	}
	written, err := os.ReadFile(materialOutputPath)
	if err != nil || !bytes.Equal(written, materialOutput.Bytes()) {
		t.Fatalf("material report --out differs from stdout: %v", err)
	}
	materialResult := decodeSingleJSONObject(t, materialOutput.Bytes())
	if materialResult["mode"] != "qianchuan_material_report" || materialResult["row_count"] != float64(1) ||
		materialResult["page_count"] != float64(1) || materialResult["displayed_count"] != float64(1) {
		t.Fatalf("material report Shadow result changed: %#v", materialResult)
	}
	if fallback.args != nil {
		t.Fatalf("Qianchuan report Shadow invoked Python fallback: %v", fallback.args)
	}
	if credentials.writes != 0 || !reflect.DeepEqual(credentials.reads, []string{
		"oceanengine-auth-qianchuan-qianchuan-auth-r1",
		"oceanengine-auth-qianchuan-qianchuan-auth-r1",
	}) {
		t.Fatalf("Qianchuan report credential isolation changed: reads=%v writes=%d", credentials.reads, credentials.writes)
	}
	calls := transport.snapshot()
	wantPaths := []string{
		"/open_api/v1.0/qianchuan/report/uni_promotion/config/get/",
		"/open_api/v1.0/qianchuan/report/uni_promotion/data/get/",
		"/open_api/v1.0/qianchuan/uni_promotion/list/",
		"/open_api/v1.0/qianchuan/report/material/get/",
	}
	if len(calls) != len(wantPaths) {
		t.Fatalf("Qianchuan report SDK calls = %d: %#v", len(calls), calls)
	}
	for index, call := range calls {
		if call.Path != wantPaths[index] || call.Host != oceanengine.BusinessHost ||
			call.Token != "fixture-qianchuan-token" {
			t.Fatalf("Qianchuan report SDK call %d changed: %#v", index, call)
		}
	}
	for _, call := range calls[1:] {
		if call.Query.Get("start_time") != "" {
			if call.Query.Get("start_time") != "2026-07-25 00:00:00" ||
				call.Query.Get("end_time") != "2026-07-25 23:59:59" {
				t.Fatalf("plan report date range diverged across APIs: %#v", call)
			}
		}
		if call.Query.Get("start_date") != "" {
			if call.Query.Get("start_date") != "2026-07-25" || call.Query.Get("end_date") != "2026-07-25" {
				t.Fatalf("material report default date changed: %#v", call)
			}
		}
	}
}

func TestRunQianchuanReportRejectsInvalidArgumentsBeforeService(t *testing.T) {
	stub := &qianchuanReportServiceStub{}
	for _, test := range []struct {
		action string
		args   []string
	}{
		{action: "plans", args: []string{"--advertiser-id", "bad"}},
		{action: "plans", args: []string{"--advertiser-id", "1000000000000001", "--top", "-1"}},
		{action: "materials", args: []string{"--advertiser-id", "1000000000000001", "--page-size", "101"}},
		{action: "materials", args: []string{"--advertiser-id", "1000000000000001", "--material-id", "bad"}},
	} {
		stdout := new(bytes.Buffer)
		if code := RunQianchuanReport(context.Background(), test.action, test.args, stub, stdout, nil); code != 2 {
			t.Fatalf("%s invalid exit = %d: %s", test.action, code, stdout.String())
		}
		if decodeSingleJSONObject(t, stdout.Bytes())["ok"] != false {
			t.Fatalf("invalid report arguments did not return JSON error: %s", stdout.String())
		}
	}
	if stub.calls != 0 {
		t.Fatalf("invalid Qianchuan report arguments reached service %d times", stub.calls)
	}
}

func TestDefaultRouteKeepsQianchuanReportsOnPython(t *testing.T) {
	for _, command := range []string{"qc-reports plans", "qc-reports materials"} {
		runtime, ok := application.DefaultRouteManifest().RouteFor(command)
		if !ok || runtime != application.RuntimePython {
			t.Fatalf("default %s route = %q, want Python", command, runtime)
		}
	}
}

type qianchuanReportServiceStub struct {
	calls int
}

func (stub *qianchuanReportServiceStub) MaterialReport(
	context.Context,
	applicationreports.MaterialQuery,
) (applicationreports.MaterialResult, error) {
	stub.calls++
	return applicationreports.MaterialResult{}, nil
}

func (stub *qianchuanReportServiceStub) PlanReport(
	context.Context,
	applicationreports.PlanQuery,
) (applicationreports.PlanResult, error) {
	stub.calls++
	return applicationreports.PlanResult{}, nil
}
