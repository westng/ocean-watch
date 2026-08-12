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

const (
	marketingReportAdapterAdvertiserID = "1000000000000001"
	marketingReportAdapterToken        = "TEST_MARKETING_REPORT_TOKEN_DO_NOT_USE"
	marketingReportAdapterHighID       = "9007199254740993"
)

type marketingReportAdapterCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type marketingReportAdapterTransport struct {
	calls              []marketingReportAdapterCall
	reportPageTwoFails int
	bodyOverride       string
}

func (transport *marketingReportAdapterTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := marketingReportAdapterCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.calls = append(transport.calls, call)
	body := transport.bodyOverride
	if body == "" {
		switch call.Path {
		case "/open_api/v3.0/report/custom/config/get/":
			body = marketingSchemaAdapterFixture(call.Query.Get("data_topics"))
		case "/open_api/v3.0/report/custom/get/":
			page, _ := strconv.Atoi(call.Query.Get("page"))
			if page == 2 && transport.reportPageTwoFails == 0 {
				transport.reportPageTwoFails++
				body = `{"code":40100,"message":"synthetic rate limit","request_id":"report-retry"}`
				break
			}
			body = marketingReportAdapterFixture(page)
		case "/open_api/v3.0/promotion/list/":
			body = `{"code":0,"message":"OK","request_id":"promotion-request","data":{"list":[{"advertiser_id":1000000000000001,"project_id":9007199254740991,"promotion_id":9007199254740993,"promotion_name":"Fixture promotion","status":"ENABLE","status_first":"RUNNING","status_second":[],"opt_status":"ENABLE","promotion_materials":{"video_material_list":[{"material_id":9007199254740995,"video_id":"video-fixture","video_cover_id":"cover-fixture","material_status":"MATERIAL_STATUS_OK","material_opt_status":"ENABLE","image_mode":"CREATIVE_IMAGE_MODE_VIDEO","create_time":"2026-07-15 08:00:00"}]} }],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
		default:
			body = `{"code":40400,"message":"unexpected synthetic route"}`
		}
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)), Request: request,
	}, nil
}

func marketingSchemaAdapterFixture(dataTopics string) string {
	topic := applicationreports.MarketingPlanTopic
	if strings.Contains(dataTopics, applicationreports.MarketingMaterialTopic) &&
		!strings.Contains(dataTopics, applicationreports.MarketingPlanTopic) {
		topic = applicationreports.MarketingMaterialTopic
	}
	dimensions := `[{"field":"project_id","name":"Project ID","sort_able":true},{"field":"project_name","name":"Project name"}]`
	if topic == applicationreports.MarketingMaterialTopic {
		dimensions = `[{"field":"material_id","name":"Material ID","sort_able":true},{"field":"cdp_promotion_id","name":"Promotion ID"},{"field":"cdp_promotion_name","name":"Promotion name"}]`
	}
	return `{"code":0,"message":"OK","request_id":"schema-request","data":{"list":[{"data_topic":"` + topic + `","dimensions":` + dimensions + `,"metrics":[{"field":"stat_cost","name":"Spend"},{"field":"in_app_order_gmv","name":"GMV"}]}]}}`
}

func marketingReportAdapterFixture(page int) string {
	projectID := marketingReportAdapterHighID
	name, cost, gmv := "Plan one", "1.005", "2.5"
	if page == 2 {
		projectID, name, cost, gmv = "9007199254740995", "Plan two", "2.005", "4"
	}
	return `{"code":0,"message":"OK","request_id":"report-request-` + strconv.Itoa(page) + `","data":{"rows":[{"dimensions":{"project_id":"` + projectID + `","project_name":"` + name + `"},"metrics":{"stat_cost":"` + cost + `","in_app_order_gmv":"` + gmv + `"}}],"total_metrics":{"stat_cost":"3.010","in_app_order_gmv":"6.5"},"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":2,"total_number":2}}}`
}

func marketingReportAdapterFixtureClient(t *testing.T) (*marketingReportAdapterTransport, MarketingReportsAdapter) {
	t.Helper()
	transport := &marketingReportAdapterTransport{}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return transport, MarketingReportsAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
	}
}

func TestMarketingReportGeneratedServiceContracts(t *testing.T) {
	transport, adapter := marketingReportAdapterFixtureClient(t)
	schema, err := adapter.FetchSchema(testRequestContext(t, "marketing"), portreports.MarketingSchemaRequest{
		AdvertiserID: marketingReportAdapterAdvertiserID, AccessToken: marketingReportAdapterToken,
		DataTopics: []string{applicationreports.MarketingMaterialTopic},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := adapter.FetchReportPage(testRequestContext(t, "marketing"), portreports.MarketingReportPageRequest{
		AdvertiserID: marketingReportAdapterAdvertiserID, AccessToken: marketingReportAdapterToken,
		DataTopic:  applicationreports.MarketingPlanTopic,
		Dimensions: []string{"project_id", "project_name"}, Metrics: []string{"stat_cost", "in_app_order_gmv"},
		Filters: []portreports.MarketingFilter{{
			Field: "project_id", Type: 2, Operator: 1, Values: []string{marketingReportAdapterHighID},
		}},
		StartTime: "2026-07-15 00:00:00", EndTime: "2026-07-15 23:59:59",
		OrderField: "stat_cost", OrderType: "DESC", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := adapter.FetchPromotionPage(testRequestContext(t, "marketing"), portreports.MarketingPromotionPageRequest{
		AdvertiserID: marketingReportAdapterAdvertiserID, AccessToken: marketingReportAdapterToken,
		ProjectID: "9007199254740991", PromotionIDs: []string{marketingReportAdapterHighID},
		Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 3 {
		t.Fatalf("generated Marketing report Service call count = %d", len(transport.calls))
	}
	for _, call := range transport.calls {
		if call.Method != http.MethodGet || call.Host != BusinessHost ||
			call.Token != marketingReportAdapterToken ||
			call.Query.Get("advertiser_id") != marketingReportAdapterAdvertiserID {
			t.Fatalf("Marketing report escaped generated Service boundary: %#v", call)
		}
	}
	if call := transport.calls[0]; call.Path != "/open_api/v3.0/report/custom/config/get/" ||
		!reflect.DeepEqual(decodeMarketingReportQueryJSON(t, call.Query.Get("data_topics")), []any{applicationreports.MarketingMaterialTopic}) ||
		len(schema.Topics) != 1 || schema.Topics[0].DataTopic != applicationreports.MarketingMaterialTopic ||
		len(schema.Topics[0].Dimensions) != 3 || len(schema.Topics[0].Metrics) != 2 {
		t.Fatalf("Marketing schema SDK contract changed: call=%#v result=%#v", call, schema)
	}
	reportCall := transport.calls[1]
	if reportCall.Path != "/open_api/v3.0/report/custom/get/" ||
		reportCall.Query.Get("data_topic") != applicationreports.MarketingPlanTopic ||
		reportCall.Query.Get("start_time") != "2026-07-15 00:00:00" ||
		reportCall.Query.Get("end_time") != "2026-07-15 23:59:59" ||
		reportCall.Query.Get("page") != "1" || reportCall.Query.Get("page_size") != "100" ||
		!reflect.DeepEqual(decodeMarketingReportQueryJSON(t, reportCall.Query.Get("dimensions")), []any{"project_id", "project_name"}) ||
		!reflect.DeepEqual(decodeMarketingReportQueryJSON(t, reportCall.Query.Get("metrics")), []any{"stat_cost", "in_app_order_gmv"}) {
		t.Fatalf("Marketing custom report request changed: %#v", reportCall)
	}
	filters := decodeMarketingReportQueryJSON(t, reportCall.Query.Get("filters")).([]any)
	if len(filters) != 1 || filters[0].(map[string]any)["field"] != "project_id" {
		t.Fatalf("Marketing report filters changed: %#v", filters)
	}
	if len(report.Rows) != 1 || report.Rows[0].Dimensions["project_id"] != marketingReportAdapterHighID ||
		report.Rows[0].Metrics["stat_cost"] != "1.005" || report.TotalMetrics["stat_cost"] != "3.010" {
		t.Fatalf("Marketing report mapping changed: %#v", report)
	}
	promotionCall := transport.calls[2]
	if promotionCall.Path != "/open_api/v3.0/promotion/list/" ||
		promotionCall.Query.Get("page") != "1" || promotionCall.Query.Get("page_size") != "20" ||
		!strings.Contains(promotionCall.Query.Get("fields"), "promotion_materials") ||
		!strings.Contains(promotionCall.Query.Get("filtering"), marketingReportAdapterHighID) {
		t.Fatalf("Marketing promotion request changed: %#v", promotionCall)
	}
	if len(promotion.Rows) != 1 || promotion.Rows[0].PromotionID != marketingReportAdapterHighID ||
		promotion.Rows[0].ProjectID != "9007199254740991" || len(promotion.Rows[0].Materials) != 1 ||
		promotion.Rows[0].Materials[0].MaterialID != "9007199254740995" {
		t.Fatalf("Marketing promotion mapping lost exact IDs: %#v", promotion)
	}
}

func TestMarketingReportRetriesOnlyCurrentPage(t *testing.T) {
	transport, adapter := marketingReportAdapterFixtureClient(t)
	result, err := (applicationreports.MarketingService{
		Tokens: marketingAdapterTokenProvider{}, Reader: adapter,
	}).Plans(testRequestContext(t, "marketing"), applicationreports.MarketingPlanQuery{
		CredentialScope: applicationreports.CredentialScope{AdvertiserID: marketingReportAdapterAdvertiserID},
		StartDate:       "2026-07-15", EndDate: "2026-07-15", Top: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	pages := []int{}
	for _, call := range transport.calls {
		if call.Path == "/open_api/v3.0/report/custom/get/" {
			page, parseErr := strconv.Atoi(call.Query.Get("page"))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			pages = append(pages, page)
		}
	}
	if !reflect.DeepEqual(pages, []int{1, 2, 2}) {
		t.Fatalf("Marketing report restarted completed pages: %v", pages)
	}
	if result.RowCount != 2 || result.Summary.TotalSpend.String() != "3.01" ||
		result.Summary.TotalGMV == nil || result.Summary.TotalGMV.String() != "6.5" {
		t.Fatalf("Marketing report retry changed full-page summary: %#v", result)
	}
}

type marketingAdapterTokenProvider struct{}

func (marketingAdapterTokenProvider) Ensure(
	_ context.Context,
	query authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: testAuthorizationID,
		AccessToken: marketingReportAdapterToken,
	}, nil
}

func TestMarketingReportEnvelopeFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code int64
	}{
		{name: "missing-data", body: `{"code":0,"message":"OK","request_id":"missing"}`},
		{name: "business-error", body: `{"code":40000,"message":"synthetic failure","request_id":"business"}`, code: 40000},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &marketingReportAdapterTransport{bodyOverride: test.body}
			factory, err := NewClientFactory(FactoryOptions{
				TransportFactory: func(HostProfile) http.RoundTripper { return transport },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = (MarketingReportsAdapter{
				Factory: factory,
				Retry: platformretry.Policy{
					Delays: []time.Duration{},
					Sleep:  func(context.Context, time.Duration) error { return nil },
				},
			}).FetchReportPage(testRequestContext(t, "marketing"), portreports.MarketingReportPageRequest{
				AdvertiserID: marketingReportAdapterAdvertiserID, AccessToken: marketingReportAdapterToken,
				DataTopic:  applicationreports.MarketingPlanTopic,
				Dimensions: []string{"project_id"}, Metrics: []string{"stat_cost"},
				StartTime: "2026-07-15 00:00:00", EndTime: "2026-07-15 23:59:59",
				OrderField: "stat_cost", OrderType: "DESC", Page: 1, PageSize: 100,
			})
			if err == nil {
				t.Fatal("invalid Marketing report envelope was accepted")
			}
			if test.code != 0 {
				var envelope *EnvelopeError
				if !errors.As(err, &envelope) || envelope.Code != test.code ||
					envelope.RequestID != "business" {
					t.Fatalf("Marketing business envelope was not preserved: %T %v", err, err)
				}
			}
		})
	}
}

func decodeMarketingReportQueryJSON(t *testing.T, value string) any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("invalid generated JSON query %q: %v", value, err)
	}
	return result
}
