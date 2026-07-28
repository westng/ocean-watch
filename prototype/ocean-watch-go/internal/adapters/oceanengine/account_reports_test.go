package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationaccounts "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/accounts"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

type accountReportCall struct {
	Host  string
	Path  string
	Query url.Values
	Token string
}

type accountReportTransport struct {
	mu    sync.Mutex
	calls []accountReportCall
}

func (transport *accountReportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.calls = append(transport.calls, accountReportCall{
		Host: request.URL.Host, Path: request.URL.Path, Query: request.URL.Query(),
		Token: request.Header.Get("Access-Token"),
	})
	transport.mu.Unlock()
	body := `{"code":0,"message":"OK","request_id":"fixture-request"}`
	switch request.URL.Path {
	case "/open_api/v3.0/report/custom/get/":
		body = `{"code":0,"message":"OK","request_id":"marketing-request","data":{"rows":[],"total_metrics":{"stat_cost":"12.34","in_app_order_count":"2","in_app_order_gmv":"30.00","in_app_order_roi":"2.43","in_app_order_net_count_1h":"1","in_app_order_net_gmv_1h":"20.00","in_app_order_net_roi_1h":"1.62"}}}`
	case "/open_api/v1.0/qianchuan/report/uni_promotion/get/":
		body = `{"code":0,"message":"OK","request_id":"qianchuan-request","data":{"stat_cost":193.95,"total_pay_order_count_for_roi2":1,"total_pay_order_gmv_include_coupon_for_roi2":99,"total_prepay_and_pay_order_roi2":0.5104}}`
	default:
		body = `{"code":40400,"message":"unexpected fixture route"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
		Request: request,
	}, nil
}

func TestAccountReportAdaptersUseOnlyGeneratedAggregateServices(t *testing.T) {
	transport := &accountReportTransport{}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := applicationaccounts.ReportRequest{
		AdvertiserID: "1234567890123456", StartDate: "2026-07-25", EndDate: "2026-07-25",
	}
	request.AccessToken = "fixture-marketing-token"
	marketing, err := (MarketingAccountReportAdapter{Factory: factory}).QueryAccount(testRequestContext(t, "marketing"), request)
	if err != nil {
		t.Fatal(err)
	}
	request.AccessToken = "fixture-qianchuan-token"
	qianchuan, err := (QianchuanAccountReportAdapter{Factory: factory}).QueryAccount(testRequestContext(t, "qianchuan"), request)
	if err != nil {
		t.Fatal(err)
	}
	if marketing.Spend.StringFixed(2) != "12.34" || marketing.Orders != 2 ||
		marketing.GMV.StringFixed(2) != "30.00" || marketing.ROI.String() != "2.43" {
		t.Fatalf("marketing metrics mapped incorrectly: %#v", marketing)
	}
	if qianchuan.Spend.StringFixed(2) != "193.95" || qianchuan.Orders != 1 ||
		qianchuan.GMV.StringFixed(2) != "99.00" || qianchuan.ROI.String() != "0.5104" {
		t.Fatalf("Qianchuan metrics mapped incorrectly: %#v", qianchuan)
	}
	wantPaths := []string{
		"/open_api/v3.0/report/custom/get/",
		"/open_api/v1.0/qianchuan/report/uni_promotion/get/",
	}
	paths := make([]string, 0, len(transport.calls))
	for _, call := range transport.calls {
		paths = append(paths, call.Path)
		if call.Host != BusinessHost {
			t.Fatalf("account report escaped business host: %#v", call)
		}
		if strings.Contains(call.Path, "/qianchuan/uni_promotion/list/") {
			t.Fatalf("account report called Qianchuan plan list: %#v", call)
		}
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("account report service calls = %v, want %v", paths, wantPaths)
	}
	assertMarketingAccountQuery(t, transport.calls[0])
	assertQianchuanAccountQuery(t, transport.calls[1])
}

func TestAccountReportRetryAttemptsConsumeCommandBudget(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt := calls.Add(1)
		body := `{"code":40100,"message":"synthetic rate limit","request_id":"retry"}`
		if attempt > 1 {
			body = `{"code":0,"message":"OK","request_id":"success","data":{"rows":[],"total_metrics":{"stat_cost":"12.34","in_app_order_count":"2","in_app_order_gmv":"30.00","in_app_order_roi":"2.43","in_app_order_net_count_1h":"1","in_app_order_net_gmv_1h":"20.00","in_app_order_net_roi_1h":"1.62"}}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
		}, nil
	})
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := MarketingAccountReportAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0}, Sleep: func(context.Context, time.Duration) error { return nil },
		},
	}
	request := applicationaccounts.ReportRequest{
		AdvertiserID: "1234567890123456", AccessToken: "fixture-access",
		StartDate: "2026-07-25", EndDate: "2026-07-25",
	}
	ctx, budget, metrics := controlledTestRequestContext(t, "marketing", testAuthorizationID, 2)
	if _, err := adapter.QueryAccount(ctx, request); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || budget.Snapshot().Used != 2 || metrics.Snapshot().Attempts != 2 {
		t.Fatalf("retry attempts were not budgeted exactly: calls=%d budget=%#v metrics=%#v",
			calls.Load(), budget.Snapshot(), metrics.Snapshot())
	}

	calls.Store(0)
	ctx, budget, metrics = controlledTestRequestContext(t, "marketing", testAuthorizationID, 1)
	if _, err := adapter.QueryAccount(ctx, request); !errors.Is(err, requestcontrol.ErrRequestBudgetExceeded) {
		t.Fatalf("exhausted retry budget was not preserved: %v", err)
	}
	if calls.Load() != 1 || budget.Snapshot().Used != 1 || metrics.Snapshot().Attempts != 1 {
		t.Fatalf("exhausted budget reached the underlying transport: calls=%d budget=%#v metrics=%#v",
			calls.Load(), budget.Snapshot(), metrics.Snapshot())
	}
}

func assertMarketingAccountQuery(t *testing.T, call accountReportCall) {
	t.Helper()
	if call.Token != "fixture-marketing-token" || call.Query.Get("advertiser_id") != "1234567890123456" ||
		call.Query.Get("start_time") != "2026-07-25 00:00:00" ||
		call.Query.Get("end_time") != "2026-07-25 23:59:59" ||
		call.Query.Get("data_topic") != "BASIC_DATA" || call.Query.Get("page") != "1" ||
		call.Query.Get("page_size") != "100" {
		t.Fatalf("marketing account report query is incomplete: %#v", call)
	}
	var dimensions []string
	if err := json.Unmarshal([]byte(call.Query.Get("dimensions")), &dimensions); err != nil || len(dimensions) != 0 {
		t.Fatalf("marketing report is not dimensionless: %q, %v", call.Query.Get("dimensions"), err)
	}
}

func assertQianchuanAccountQuery(t *testing.T, call accountReportCall) {
	t.Helper()
	if call.Token != "fixture-qianchuan-token" || call.Query.Get("advertiser_id") != "1234567890123456" ||
		call.Query.Get("start_date") != "2026-07-25 00:00:00" ||
		call.Query.Get("end_date") != "2026-07-25 23:59:59" ||
		call.Query.Get("marketing_goal") != "ALL" || call.Query.Get("order_platform") != "QIANCHUAN" {
		t.Fatalf("Qianchuan account report query is incomplete: %#v", call)
	}
	var fields []string
	if err := json.Unmarshal([]byte(call.Query.Get("fields")), &fields); err != nil ||
		!reflect.DeepEqual(fields, qianchuanAccountMetrics) {
		t.Fatalf("Qianchuan account report fields = %v, %v", fields, err)
	}
}
