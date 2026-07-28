package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	applicationaccounts "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/accounts"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type accountReportServiceStub struct {
	queries []applicationaccounts.Query
	result  domain.AccountReportResult
	err     *domain.Error
}

func (stub *accountReportServiceStub) Report(
	_ context.Context,
	query applicationaccounts.Query,
) (domain.AccountReportResult, *domain.Error) {
	stub.queries = append(stub.queries, query)
	return stub.result, stub.err
}

func TestRunAccountReportParsesContractAndPreservesPartialResult(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 7, 24, 16, 30, 0, 0, time.UTC) }
	partial := domain.NewAccountReportResult([]domain.AccountReportRow{
		{
			Channel: domain.Marketing, AdvertiserID: "1000000000000001", Name: "Fixture",
			Enabled: true, ChannelName: domain.Marketing.DisplayName(), QueryStatus: "failed",
			Error: &domain.AccountReportFailure{Code: "api_error", Message: "synthetic failure", Details: map[string]any{}},
		},
	}, "2026-07-25", "2026-07-25")
	stub := &accountReportServiceStub{result: partial}
	stdout := new(bytes.Buffer)
	out := filepath.Join(t.TempDir(), "report.json")
	code := RunAccountReport(context.Background(), []string{
		"--channel", "qianchuan", "--channel", "marketing",
		"--include-disabled", "--concurrency", "8", "--out", out,
	}, stub, stdout, now)
	if code != 1 {
		t.Fatalf("partial account report exit = %d, want 1: %s", code, stdout.String())
	}
	if len(stub.queries) != 1 {
		t.Fatalf("account report calls = %d, want 1", len(stub.queries))
	}
	query := stub.queries[0]
	if query.StartDate != "2026-07-25" || query.EndDate != "2026-07-25" ||
		!query.IncludeDisabled || query.Concurrency != 8 ||
		!reflect.DeepEqual(query.Channels, []domain.Channel{domain.Qianchuan, domain.Marketing}) {
		t.Fatalf("account report query changed: %#v", query)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, stdout.Bytes()) {
		t.Fatal("account report --out differs from stdout")
	}
	var result domain.AccountReportResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || len(result.Presentation.Columns) != 13 || len(result.Presentation.RequiredSections) != 5 {
		t.Fatalf("partial account report lost mandatory presentation: %#v", result.Presentation)
	}
}

func TestRunAccountReportRejectsInvalidArgumentsBeforeService(t *testing.T) {
	for _, args := range [][]string{
		{"--concurrency", "9"},
		{"--start-date", "2026-07-26", "--end-date", "2026-07-25"},
		{"--channel", "unknown"},
	} {
		stub := &accountReportServiceStub{}
		stdout := new(bytes.Buffer)
		if code := RunAccountReport(context.Background(), args, stub, stdout, nil); code != 2 {
			t.Fatalf("%v exit = %d, want 2", args, code)
		}
		if len(stub.queries) != 0 {
			t.Fatalf("invalid arguments reached report service: %v", args)
		}
		result := decodeSingleJSONObject(t, stdout.Bytes())
		if result["ok"] != false {
			t.Fatalf("invalid arguments did not return an error: %#v", result)
		}
	}
}

type accountReportRunnerCall struct {
	Path  string
	Token string
}

type accountReportRunnerTransport struct {
	mu    sync.Mutex
	calls []accountReportRunnerCall
}

func (transport *accountReportRunnerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.calls = append(transport.calls, accountReportRunnerCall{
		Path: request.URL.Path, Token: request.Header.Get("Access-Token"),
	})
	transport.mu.Unlock()
	body := ""
	switch request.URL.Path {
	case "/open_api/v3.0/report/custom/get/":
		body = `{"code":0,"message":"OK","request_id":"marketing-request","data":{"rows":[],"total_metrics":{"stat_cost":"12.34","in_app_order_count":"2","in_app_order_gmv":"30.00","in_app_order_roi":"2.43","in_app_order_net_count_1h":"1","in_app_order_net_gmv_1h":"20.00","in_app_order_net_roi_1h":"1.62"}}}`
	case "/open_api/v1.0/qianchuan/report/uni_promotion/get/":
		body = `{"code":0,"message":"OK","request_id":"qianchuan-request","data":{"stat_cost":193.95,"total_pay_order_count_for_roi2":1,"total_pay_order_gmv_include_coupon_for_roi2":99,"total_prepay_and_pay_order_roi2":0.5104}}`
	default:
		body = `{"code":40400,"message":"unexpected fixture route"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func TestRunnerAccountReportShadowAssemblesTokenAndSDKAdapters(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := `{
  "managed_account_schema_version": 1,
  "managed_accounts": {
    "marketing": [{"advertiser_id":"1000000000000001","name":"Marketing","enabled":true}],
    "qianchuan": [{"advertiser_id":"1000000000000002","name":"Qianchuan","enabled":true}]
  }
}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(root, "codex-home")
	stateRoot := filepath.Join(codexHome, "ads-plan-monitor", "state")
	authorizations := filesystem.AuthorizationStore{Root: stateRoot}
	for _, fixture := range []struct {
		channel, authorizationID, advertiserID, accountID string
	}{
		{"marketing", "marketing-auth", "1000000000000001", "9000000000000001"},
		{"qianchuan", "qianchuan-auth", "1000000000000002", "9000000000000002"},
	} {
		state := map[string]any{
			"generation": 1,
			"authorizations": map[string]any{fixture.authorizationID: map[string]any{
				"token_revision": 1, "pending_account_sync": false,
				"advertiser_ids": []any{fixture.advertiserID},
				"authorized_accounts": []any{map[string]any{
					"account_id": fixture.accountID, "advertiser_ids": []any{fixture.advertiserID},
				}},
			}},
			"account_index":    map[string]any{fixture.accountID: fixture.authorizationID},
			"advertiser_index": map[string]any{fixture.advertiserID: []any{fixture.authorizationID}},
		}
		if err := authorizations.CommitChannel(context.Background(), fixture.channel, state); err != nil {
			t.Fatal(err)
		}
	}
	credentials := &runnerCredentialStore{entries: map[string]map[string]any{
		"oceanengine-auth-marketing-marketing-auth-r1": {
			"access_token": "fixture-marketing-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
		"oceanengine-auth-qianchuan-qianchuan-auth-r1": {
			"access_token": "fixture-qianchuan-token", "access_token_expires_at": "2026-07-26T00:00:00Z",
		},
	}}
	transport := &accountReportRunnerTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	routes := application.DefaultRouteManifest().Snapshot()
	routes["accounts report"] = application.RuntimeGo
	manifest, err := application.NewRouteManifest(6, routes)
	if err != nil {
		t.Fatal(err)
	}
	fallback := &fallbackSpy{code: 99}
	stdout := new(bytes.Buffer)
	fixedNow := func() time.Time { return time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC) }
	runner := Runner{
		Routes: manifest, Fallback: fallback, Stdout: stdout, Cwd: root, UserHome: root,
		Getenv: func(name string) string {
			if name == "CODEX_HOME" {
				return codexHome
			}
			return ""
		},
		Credentials:    credentials,
		AccountReports: AccountReportRuntime{ClientFactory: factory, Now: fixedNow},
	}
	code := runner.Execute(context.Background(), []string{
		"accounts", "report", "--config", configPath,
		"--start-date", "2026-07-25", "--end-date", "2026-07-25", "--concurrency", "2",
	})
	if code != 0 {
		t.Fatalf("account report shadow exit = %d: %s", code, stdout.String())
	}
	if fallback.args != nil {
		t.Fatal("account report shadow invoked Python fallback")
	}
	var result domain.AccountReportResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Accounts) != 2 || len(result.Presentation.Columns) != 13 {
		t.Fatalf("account report shadow returned incomplete result: %#v", result)
	}
	transport.mu.Lock()
	calls := append([]accountReportRunnerCall(nil), transport.calls...)
	transport.mu.Unlock()
	sort.Slice(calls, func(left, right int) bool { return calls[left].Path < calls[right].Path })
	wantCalls := []accountReportRunnerCall{
		{Path: "/open_api/v1.0/qianchuan/report/uni_promotion/get/", Token: "fixture-qianchuan-token"},
		{Path: "/open_api/v3.0/report/custom/get/", Token: "fixture-marketing-token"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("account report shadow calls = %#v, want %#v", calls, wantCalls)
	}
	for _, call := range calls {
		if strings.Contains(call.Path, "/qianchuan/uni_promotion/list/") {
			t.Fatalf("account report shadow called Qianchuan plan list: %#v", call)
		}
	}
	sort.Strings(credentials.reads)
	wantReads := []string{
		"oceanengine-auth-marketing-marketing-auth-r1",
		"oceanengine-auth-qianchuan-qianchuan-auth-r1",
	}
	if !reflect.DeepEqual(credentials.reads, wantReads) || credentials.writes != 0 {
		t.Fatalf("account report credential access = %v writes=%d", credentials.reads, credentials.writes)
	}
}

func TestDefaultRouteKeepsAccountReportOnPython(t *testing.T) {
	runtime, ok := application.DefaultRouteManifest().RouteFor("accounts report")
	if !ok || runtime != application.RuntimePython {
		t.Fatalf("default accounts report route = %q, want Python", runtime)
	}
}
