package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type qianchuanReportCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type qianchuanReportTransport struct {
	calls              []qianchuanReportCall
	metricPageTwoFails int
	bodyOverride       string
}

func (transport *qianchuanReportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := qianchuanReportCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.calls = append(transport.calls, call)
	body := transport.bodyOverride
	if body == "" {
		switch call.Path {
		case "/open_api/v1.0/qianchuan/report/all_promotion/get/":
			body = `{"code":0,"message":"OK","request_id":"all-request","data":{"stat_cost_for_roi2":12.5}}`
		case "/open_api/v1.0/qianchuan/report/uni_promotion/get/":
			body = `{"code":0,"message":"OK","request_id":"uni-request","data":{"stat_cost":8.5}}`
		case "/open_api/v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/":
			body = `{"code":0,"message":"OK","request_id":"room-request","data":{"list":[{"advertiser_id":1000000000000001,"room_id":3000000000000001,"stat_cost_for_roi2":3.5}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
		case "/open_api/v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/":
			body = `{"code":0,"message":"OK","request_id":"author-request","data":{"list":[{"advertiser_id":1000000000000001,"aweme_id":4000000000000001,"stat_cost":4.5}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
		case "/open_api/v1.0/qianchuan/report/material/get/":
			body = `{"code":0,"message":"OK","request_id":"material-request","data":{"list":[{"advertiser_id":1000000000000001,"material_id":5000000000000001,"material_type":"video","related_ad_ids":[2000000000000001],"fields":{"stat_cost":1.25,"pay_order_amount":2.5,"pay_order_count":1}}],"page_info":{"page":1,"total_page":1,"total_number":1}}}`
		case "/open_api/v1.0/qianchuan/report/uni_promotion/config/get/":
			body = qianchuanSchemaFixture()
		case "/open_api/v1.0/qianchuan/report/uni_promotion/data/get/":
			page, _ := strconv.Atoi(call.Query.Get("page"))
			if page == 2 && transport.metricPageTwoFails == 0 {
				transport.metricPageTwoFails++
				body = `{"code":40100,"message":"synthetic rate limit","request_id":"metric-retry"}`
				break
			}
			body = qianchuanMetricFixture(page)
		case "/open_api/v1.0/qianchuan/uni_promotion/list/":
			body = `{"code":0,"message":"OK","request_id":"metadata-request","data":{"page_info":{"page":1,"page_size":100,"total_page":1,"total_num":2},"ad_list":[{"ad_info":{"id":2000000000000001,"name":"Plan one"},"stats_info":{"stat_cost":99999999.99}},{"ad_info":{"id":2000000000000002,"name":"Plan two"},"stats_info":{"stat_cost":99999999.99}}]}}`
		default:
			body = `{"code":40400,"message":"unexpected fixture route"}`
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func TestQianchuanUnifiedGeneratedServices(t *testing.T) {
	transport, adapter := qianchuanReportFixture(t)
	ctx := testRequestContext(t, "qianchuan")
	token := "TEST_QIANCHUAN_TOKEN_DO_NOT_USE"
	scope := portreports.AggregateRequest{
		AdvertiserID: "1000000000000001", AccessToken: token,
		StartTime: "2026-08-01 00:00:00", EndTime: "2026-08-02 23:59:59",
		Fields: []string{"stat_cost_for_roi2"}, MarketingGoal: "ALL", OrderPlatform: "QIANCHUAN",
		AdlabScene: "OVERALL_PROJECT", DataPeriod: "ALL_DATA",
	}
	all, err := adapter.FetchAllPromotion(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.StartTime, scope.EndTime, scope.Fields = "2026-08-01", "2026-08-02", []string{"stat_cost"}
	uni, err := adapter.FetchUniPromotion(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	room, err := adapter.FetchRoomDimensionPage(ctx, portreports.DimensionPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token, DimensionID: "3000000000000001",
		StartTime: "2026-08-01 00:00:00", EndTime: "2026-08-02 23:59:59",
		Dimension: "TIME_GRANULARITY_HOURLY", Metrics: []string{"stat_cost_for_roi2"},
		OrderPlatform: "ECP_AWEME", SmartBidType: "SMART_BID_CUSTOM",
		OrderField: "stat_cost_for_roi2", OrderType: "DESC", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	author, err := adapter.FetchAuthorDimensionPage(ctx, portreports.DimensionPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token, DimensionID: "4000000000000001",
		StartTime: "2026-08-01 00:00:00", EndTime: "2026-08-02 23:59:59",
		Dimension: "TIME_GRANULARITY_DAILY", Metrics: []string{"stat_cost"}, MarketingGoal: "ALL",
		OrderPlatform: "ALL", SmartBidType: "SMART_BID_CONSERVATIVE",
		OrderField: "stat_cost", OrderType: "DESC", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 4 || all.Values["stat_cost_for_roi2"].(json.Number).String() != "12.5" ||
		uni.Values["stat_cost"].(json.Number).String() != "8.5" || len(room.Rows) != 1 || len(author.Rows) != 1 {
		t.Fatalf("unified report mapping changed: all=%#v uni=%#v room=%#v author=%#v calls=%#v", all, uni, room, author, transport.calls)
	}
	allCall, uniCall, roomCall, authorCall := transport.calls[0], transport.calls[1], transport.calls[2], transport.calls[3]
	if allCall.Path != "/open_api/v1.0/qianchuan/report/all_promotion/get/" ||
		allCall.Query.Get("start_time") != "2026-08-01 00:00:00" ||
		allCall.Query.Get("adlab_scene") != "OVERALL_PROJECT" || allCall.Query.Get("data_period") != "ALL_DATA" ||
		uniCall.Path != "/open_api/v1.0/qianchuan/report/uni_promotion/get/" ||
		uniCall.Query.Get("start_date") != "2026-08-01" ||
		roomCall.Query.Get("room_id") != "3000000000000001" || roomCall.Query.Get("dimension") != "TIME_GRANULARITY_HOURLY" ||
		!strings.Contains(roomCall.Query.Get("filtering"), "ECP_AWEME") || !strings.Contains(roomCall.Query.Get("filtering"), "SMART_BID_CUSTOM") ||
		authorCall.Query.Get("aweme_id") != "4000000000000001" || authorCall.Query.Get("marketing_goal") != "ALL" {
		t.Fatalf("generated unified query contract changed: %#v", transport.calls)
	}
}

func TestQianchuanSchemaBatchesTopicsAndDataPeriod(t *testing.T) {
	transport, adapter := qianchuanReportFixture(t)
	topics := []string{applicationreports.QianchuanProductTopic, applicationreports.QianchuanOverallProductTopic}
	transport.bodyOverride = `{"code":0,"message":"OK","request_id":"schema-request","data":{"custom_config_datas":[{"data_topic":"SITE_PROMOTION_PRODUCT_PRODUCT","dimensions":[{"field":"product_id"}],"metrics":[{"field":"stat_cost"}]},{"data_topic":"OVERALL_ROI_PRODUCT_PRODUCT","dimensions":[{"field":"product_id"}],"metrics":[{"field":"stat_cost"}]}]}}`
	schemas, err := adapter.FetchSchemas(testRequestContext(t, "qianchuan"), portreports.SchemaRequest{
		AdvertiserID: "1000000000000001", AccessToken: "TEST_QIANCHUAN_TOKEN_DO_NOT_USE",
		Topics: topics, DataPeriod: "ALL_DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 1 || len(schemas) != 2 || transport.calls[0].Query.Get("data_period") != "ALL_DATA" ||
		!strings.Contains(transport.calls[0].Query.Get("data_topics"), applicationreports.QianchuanProductTopic) ||
		!strings.Contains(transport.calls[0].Query.Get("data_topics"), applicationreports.QianchuanOverallProductTopic) {
		t.Fatalf("schema batching changed: calls=%#v schemas=%#v", transport.calls, schemas)
	}
}

func TestQianchuanCustomDataSupportsFiltersAndDataPeriod(t *testing.T) {
	transport, adapter := qianchuanReportFixture(t)
	page, err := adapter.FetchDataPage(testRequestContext(t, "qianchuan"), portreports.DataPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: "TEST_QIANCHUAN_TOKEN_DO_NOT_USE",
		Topic: applicationreports.QianchuanOverallProductTopic, Dimensions: []string{"ad_id"},
		Metrics:   applicationreports.DefaultPlanFields,
		Filters:   []portreports.ReportFilter{{Field: "product_id", Operator: 7, Values: []string{"3747851714615705603"}}},
		StartTime: "2026-08-04 00:00:00", EndTime: "2026-08-04 23:59:59",
		OrderField: "stat_cost", OrderType: 2, DataPeriod: "ALL_DATA", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := transport.calls[0]
	filters := decodeReportQueryJSON(t, call.Query.Get("filters"))
	if call.Query.Get("data_topic") != applicationreports.QianchuanOverallProductTopic ||
		call.Query.Get("data_period") != "ALL_DATA" ||
		!reflect.DeepEqual(filters, []any{map[string]any{
			"field": "product_id", "operator": float64(7), "values": []any{"3747851714615705603"},
		}}) || len(page.Rows) != 1 || page.Rows[0].Dimensions["ad_id"] != "2000000000000001" {
		t.Fatalf("custom Qianchuan report contract changed: call=%#v page=%#v", call, page)
	}
}

func qianchuanSchemaFixture() string {
	dimensions := `[{"field":"ad_id"}]`
	metrics := make([]string, 0, len(applicationreports.DefaultPlanFields))
	for _, field := range applicationreports.DefaultPlanFields {
		metrics = append(metrics, `{"field":"`+field+`"}`)
	}
	return `{"code":0,"message":"OK","request_id":"schema-request","data":{"custom_config_datas":[{"data_topic":"SITE_PROMOTION_PRODUCT_AD","dimensions":` + dimensions + `,"metrics":[` + strings.Join(metrics, ",") + `]}]}}`
}

func qianchuanMetricFixture(page int) string {
	adID := "2000000000000001"
	cost, gmv, settled, orders := "1.25", "2.5", "2", "1"
	unsafeID := "2000000000000000"
	if page == 2 {
		adID, cost, gmv, settled, orders = "2000000000000002", "3.75", "7.5", "6", "2"
		unsafeID = "2000000000000000"
	}
	metrics := map[string]string{
		"stat_cost":                                   cost,
		"total_pay_order_count_for_roi2":              orders,
		"total_pay_order_gmv_include_coupon_for_roi2": gmv,
		"total_prepay_and_pay_order_roi2":             "99",
		"total_order_settle_amount_for_roi2_1h":       settled,
		"total_order_settle_count_for_roi2_1h":        orders,
		"total_prepay_and_pay_settle_roi2_1h":         "88",
	}
	parts := make([]string, 0, len(applicationreports.DefaultPlanFields))
	for _, field := range applicationreports.DefaultPlanFields {
		parts = append(parts, `"`+field+`":{"Value":`+metrics[field]+`,"ValueStr":"999999.99"}`)
	}
	return `{"code":0,"message":"OK","request_id":"metric-request-` + strconv.Itoa(page) + `","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":2,"total_number":2},"rows":[{"dimensions":{"ad_id":{"Value":` + unsafeID + `,"ValueStr":"` + adID + `"}},"metrics":{` + strings.Join(parts, ",") + `}}]}}`
}

func qianchuanReportFixture(t *testing.T) (*qianchuanReportTransport, QianchuanReportAdapter) {
	t.Helper()
	transport := &qianchuanReportTransport{}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return transport, QianchuanReportAdapter{Factory: factory, Retry: platformretry.Policy{
		Delays: []time.Duration{0, 0}, Sleep: func(context.Context, time.Duration) error { return nil },
	}}
}

func TestQianchuanReportGeneratedServiceContracts(t *testing.T) {
	transport, adapter := qianchuanReportFixture(t)
	token := "TEST_QIANCHUAN_TOKEN_DO_NOT_USE"
	material, err := adapter.FetchMaterialPage(testRequestContext(t, "qianchuan"), portreports.MaterialPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token,
		StartDate: "2026-07-15", EndDate: "2026-07-16",
		Fields: []string{"stat_cost", "pay_order_amount", "pay_order_count"},
		Filters: portreports.MaterialFilters{
			MaterialIDs: []string{"5000000000000001"}, MaterialType: "video",
			MaterialMode: []string{"CUSTOM"}, VideoSource: []string{"AWEME"},
		},
		OrderField: "stat_cost", OrderType: "DESC", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	schema, err := adapter.FetchPlanSchema(testRequestContext(t, "qianchuan"), portreports.PlanSchemaRequest{
		AdvertiserID: "1000000000000001", AccessToken: token, Topic: applicationreports.PlanReportTopic,
	})
	if err != nil {
		t.Fatal(err)
	}
	metric, err := adapter.FetchPlanMetricPage(testRequestContext(t, "qianchuan"), portreports.PlanMetricPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token,
		Topic: applicationreports.PlanReportTopic, Dimensions: []string{"ad_id"},
		Metrics:   applicationreports.DefaultPlanFields,
		StartTime: "2026-07-15 00:00:00", EndTime: "2026-07-16 23:59:59",
		OrderField: "stat_cost", OrderType: 2, Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 3 {
		t.Fatalf("generated report Service call count = %d", len(transport.calls))
	}
	for _, call := range transport.calls {
		if call.Method != http.MethodGet || call.Host != BusinessHost || call.Token != token ||
			call.Query.Get("advertiser_id") != "1000000000000001" {
			t.Fatalf("Qianchuan report escaped generated Service boundary: %#v", call)
		}
	}
	materialCall := transport.calls[0]
	if materialCall.Path != "/open_api/v1.0/qianchuan/report/material/get/" ||
		materialCall.Query.Get("start_date") != "2026-07-15" || materialCall.Query.Get("end_date") != "2026-07-16" ||
		materialCall.Query.Get("order_field") != "stat_cost" || materialCall.Query.Get("order_type") != "DESC" ||
		materialCall.Query.Get("page") != "1" || materialCall.Query.Get("page_size") != "100" {
		t.Fatalf("material report request changed: %#v", materialCall)
	}
	if got := decodeReportQueryJSON(t, materialCall.Query.Get("fields")); !reflect.DeepEqual(
		got, []any{"stat_cost", "pay_order_amount", "pay_order_count"},
	) {
		t.Fatalf("material report fields changed: %#v", got)
	}
	filtering := decodeReportQueryJSON(t, materialCall.Query.Get("filtering")).(map[string]any)
	if filtering["material_type"] != "video" || !reflect.DeepEqual(filtering["material_id"], []any{float64(5000000000000001)}) ||
		!reflect.DeepEqual(filtering["material_mode"], []any{"CUSTOM"}) ||
		!reflect.DeepEqual(filtering["video_source"], []any{"AWEME"}) {
		t.Fatalf("material report filtering changed: %#v", filtering)
	}
	if len(material.Rows) != 1 || material.Rows[0].MaterialID != "5000000000000001" ||
		material.Rows[0].Values["stat_cost"].(json.Number).String() != "1.25" {
		t.Fatalf("material report mapping changed: %#v", material)
	}
	schemaCall := transport.calls[1]
	if schemaCall.Path != "/open_api/v1.0/qianchuan/report/uni_promotion/config/get/" ||
		!reflect.DeepEqual(decodeReportQueryJSON(t, schemaCall.Query.Get("data_topics")), []any{applicationreports.PlanReportTopic}) ||
		schema.Topic != applicationreports.PlanReportTopic || !reflect.DeepEqual(schema.Dimensions, []string{"ad_id"}) ||
		!reflect.DeepEqual(schema.Metrics, applicationreports.DefaultPlanFields) {
		t.Fatalf("plan schema contract changed: call=%#v result=%#v", schemaCall, schema)
	}
	metricCall := transport.calls[2]
	if metricCall.Path != "/open_api/v1.0/qianchuan/report/uni_promotion/data/get/" ||
		metricCall.Query.Get("data_topic") != applicationreports.PlanReportTopic ||
		metricCall.Query.Get("start_time") != "2026-07-15 00:00:00" ||
		metricCall.Query.Get("end_time") != "2026-07-16 23:59:59" || metricCall.Query.Get("page") != "1" ||
		metricCall.Query.Get("page_size") != "100" ||
		!reflect.DeepEqual(decodeReportQueryJSON(t, metricCall.Query.Get("dimensions")), []any{"ad_id"}) ||
		!reflect.DeepEqual(decodeReportQueryJSON(t, metricCall.Query.Get("metrics")), stringsToAny(applicationreports.DefaultPlanFields)) ||
		!reflect.DeepEqual(decodeReportQueryJSON(t, metricCall.Query.Get("filters")), []any{}) ||
		!reflect.DeepEqual(decodeReportQueryJSON(t, metricCall.Query.Get("order_by")), []any{map[string]any{"field": "stat_cost", "type": float64(2)}}) {
		t.Fatalf("plan data request changed: %#v", metricCall)
	}
	if len(metric.Rows) != 1 || metric.Rows[0].AdID != "2000000000000001" ||
		metric.Rows[0].Metrics["stat_cost"].String() != "1.25" {
		t.Fatalf("plan metric mapping lost ValueStr ID or Value metric precedence: %#v", metric)
	}
}

type qianchuanReportTokens struct{}

func (qianchuanReportTokens) Ensure(
	_ context.Context,
	query authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: testAuthorizationID,
		AccessToken: "TEST_QIANCHUAN_TOKEN_DO_NOT_USE",
	}, nil
}

func TestQianchuanReportRetriesOnlyCurrentPageAndIgnoresListFinance(t *testing.T) {
	transport, adapter := qianchuanReportFixture(t)
	result, err := (applicationreports.Service{Tokens: qianchuanReportTokens{}, Reader: adapter}).PlanReport(
		testRequestContext(t, "qianchuan"),
		applicationreports.PlanQuery{
			CredentialScope: applicationreports.CredentialScope{AdvertiserID: "1000000000000001"},
			StartDate:       "2026-07-15", EndDate: "2026-07-15", Top: 0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	metricPages := []int{}
	for _, call := range transport.calls {
		if call.Path == "/open_api/v1.0/qianchuan/report/uni_promotion/data/get/" {
			page, parseErr := strconv.Atoi(call.Query.Get("page"))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			metricPages = append(metricPages, page)
		}
	}
	if !reflect.DeepEqual(metricPages, []int{1, 2, 2}) {
		t.Fatalf("plan report restarted completed pages: %v", metricPages)
	}
	if result.Summary.TotalCost.String() != "5" || result.Summary.TotalPayOrderGMV.String() != "10" ||
		result.Summary.TotalPayOrderCount != 3 || result.Summary.TotalPayROI.String() != "2" ||
		result.Summary.TotalSettledAmount1H.String() != "8" {
		t.Fatalf("plan-list stats_info contaminated financial summary: %#v", result.Summary)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "99999999") || strings.Contains(string(encoded), "stats_info") {
		t.Fatalf("plan-list finance escaped report result: %s", encoded)
	}
}

func TestQianchuanReportEnvelopeFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code int64
	}{
		{name: "missing-data", body: `{"code":0,"message":"OK","request_id":"missing"}`},
		{name: "business-error", body: `{"code":40000,"message":"synthetic failure","request_id":"business"}`, code: 40000},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &qianchuanReportTransport{bodyOverride: test.body}
			factory, err := NewClientFactory(FactoryOptions{
				TransportFactory: func(HostProfile) http.RoundTripper { return transport },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = (QianchuanReportAdapter{Factory: factory}).FetchMaterialPage(
				testRequestContext(t, "qianchuan"),
				portreports.MaterialPageRequest{
					AdvertiserID: "1000000000000001", AccessToken: "TEST_QIANCHUAN_TOKEN_DO_NOT_USE",
					StartDate: "2026-07-15", EndDate: "2026-07-15", Fields: []string{"stat_cost"},
					OrderField: "stat_cost", OrderType: "DESC", Page: 1, PageSize: 100,
				},
			)
			if err == nil {
				t.Fatal("invalid report envelope was accepted")
			}
			if test.code != 0 {
				var envelope *EnvelopeError
				if !errors.As(err, &envelope) || envelope.Code != test.code {
					t.Fatalf("business envelope was not preserved: %T %v", err, err)
				}
			}
		})
	}
}

func decodeReportQueryJSON(t *testing.T, value string) any {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("invalid generated query JSON %q: %v", value, err)
	}
	return decoded
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
