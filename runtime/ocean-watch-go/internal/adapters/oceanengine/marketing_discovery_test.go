package oceanengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	discoveryAdapterAdvertiserID = "1000000000000001"
	discoveryAdapterToken        = "TEST_MARKETING_DISCOVERY_TOKEN_DO_NOT_USE"
	discoveryAdapterHighID       = "9007199254740993"
)

type discoveryAdapterCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
	Body   string
}

type discoveryAdapterTransport struct {
	calls    []discoveryAdapterCall
	response func(discoveryAdapterCall) (int, string)
}

func (transport *discoveryAdapterTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(payload)
	}
	call := discoveryAdapterCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"), Body: body,
	}
	transport.calls = append(transport.calls, call)
	status, payload := http.StatusOK, `{"code":40400,"message":"unexpected synthetic route"}`
	if transport.response != nil {
		status, payload = transport.response(call)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    request,
	}, nil
}

func newDiscoveryAdapter(t *testing.T, transport http.RoundTripper) MarketingDiscoveryAdapter {
	t.Helper()
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return MarketingDiscoveryAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
	}
}

func discoveryScope() portmarketing.DiscoveryScope {
	return portmarketing.DiscoveryScope{
		AdvertiserID: discoveryAdapterAdvertiserID,
		AccessToken:  discoveryAdapterToken,
	}
}

type discoveryAdapterCase struct {
	name       string
	method     string
	path       string
	success    string
	run        func(*testing.T, MarketingDiscoveryAdapter) error
	assertCall func(*testing.T, discoveryAdapterCall)
}

func discoveryAdapterCases() []discoveryAdapterCase {
	return []discoveryAdapterCase{
		{
			name: "projects", method: http.MethodGet, path: "/open_api/v3.0/project/list/",
			success: `{"code":0,"message":"OK","request_id":"project-request","data":{"list":[{"project_id":9007199254740993,"advertiser_id":1000000000000001,"name":"Fixture project"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchProjects(testRequestContext(t, "marketing"), portmarketing.ProjectDiscoveryRequest{
					DiscoveryScope: discoveryScope(), Fields: []string{"project_id", "name"},
					Name: "Fixture", LandingType: "SHOP", MarketingGoal: "VIDEO_AND_IMAGE",
					DeliveryMode: "PROCEDURAL", Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "project-request", true)
					if result.Response["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["project_id"].(interface{ String() string }).String() != discoveryAdapterHighID {
						t.Fatalf("high project ID changed: %#v", result.Response)
					}
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if !strings.Contains(call.Query.Get("fields"), "project_id") ||
					!strings.Contains(call.Query.Get("filtering"), "Fixture") {
					t.Fatalf("project SDK query changed: %#v", call.Query)
				}
			},
		},
		{
			name: "promotions", method: http.MethodGet, path: "/open_api/v3.0/promotion/list/",
			success: `{"code":0,"message":"OK","request_id":"promotion-request","data":{"list":[{"promotion_id":9007199254740993,"project_id":9007199254740993,"advertiser_id":1000000000000001,"promotion_name":"Fixture promotion"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchPromotions(testRequestContext(t, "marketing"), portmarketing.PromotionDiscoveryRequest{
					DiscoveryScope: discoveryScope(), Fields: []string{"promotion_id", "promotion_name"},
					Name: "Fixture", ProjectID: discoveryAdapterHighID,
					PromotionIDs: []string{discoveryAdapterHighID}, Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "promotion-request", true)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if !strings.Contains(call.Query.Get("filtering"), discoveryAdapterHighID) {
					t.Fatalf("high promotion IDs changed: %#v", call.Query)
				}
			},
		},
		{
			name: "DPA meta", method: http.MethodGet, path: "/open_api/2/dpa/meta/get/",
			success: `{"code":0,"message":"OK","request_id":"dpa-meta-request","data":{"list":[]}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchDPA(testRequestContext(t, "marketing"), portmarketing.DPADiscoveryRequest{
					DiscoveryScope: discoveryScope(), Mode: "meta", PlatformID: discoveryAdapterHighID,
					Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "dpa-meta-request", false)
				}
				return err
			},
			assertCall: assertHighPlatformID,
		},
		{
			name: "DPA dictionary", method: http.MethodGet, path: "/open_api/2/dpa/dict/get/",
			success: `{"code":0,"message":"OK","request_id":"dpa-dict-request","data":{"list":[]}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchDPA(testRequestContext(t, "marketing"), portmarketing.DPADiscoveryRequest{
					DiscoveryScope: discoveryScope(), Mode: "dict", PlatformID: discoveryAdapterHighID,
					Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "dpa-dict-request", false)
				}
				return err
			},
			assertCall: assertHighPlatformID,
		},
		{
			name: "DPA EBP detail", method: http.MethodGet,
			path:    "/open_api/v3.0/dpa/ebp/product/detail/get/",
			success: `{"code":0,"message":"OK","request_id":"dpa-ebp-request","data":{"list":[],"page_info":{"page":1,"page_size":20,"total_page":0,"total_number":0}}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchDPA(testRequestContext(t, "marketing"), portmarketing.DPADiscoveryRequest{
					DiscoveryScope: discoveryScope(), Mode: "ebp-detail", PlatformID: discoveryAdapterHighID,
					UniqueProductID: discoveryAdapterHighID, Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "dpa-ebp-request", true)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if call.Query.Get("platform_id") != discoveryAdapterHighID ||
					!strings.Contains(call.Query.Get("filtering"), discoveryAdapterHighID) ||
					call.Query.Get("account_type") != "EBP" {
					t.Fatalf("DPA EBP SDK query changed: %#v", call.Query)
				}
			},
		},
		{
			name: "DPA asset detail", method: http.MethodPost,
			path:    "/open_api/2/dpa/asset_v2/detail/read/",
			success: `{"code":0,"message":"OK","request_id":"dpa-asset-request","data":{"asset_list":[]}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchDPA(testRequestContext(t, "marketing"), portmarketing.DPADiscoveryRequest{
					DiscoveryScope: discoveryScope(), Mode: "asset-detail", PlatformID: discoveryAdapterHighID,
					UniqueProductID: discoveryAdapterHighID, Page: 1, PageSize: 20,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "dpa-asset-request", false)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if !strings.Contains(call.Body, discoveryAdapterHighID) {
					t.Fatalf("high DPA product ID changed: %s", call.Body)
				}
			},
		},
		{
			name: "events", method: http.MethodGet,
			path:    "/open_api/2/tools/event/all_assets/list/",
			success: `{"code":0,"message":"OK","request_id":"event-request","data":{"asset_list":[{"asset_id":9007199254740993,"asset_name":"Fixture asset","asset_type":"THIRD_EXTERNAL"}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchEvents(testRequestContext(t, "marketing"), portmarketing.EventDiscoveryRequest{
					DiscoveryScope: discoveryScope(), AssetType: "THIRD_EXTERNAL",
					AssetIDs: []string{discoveryAdapterHighID}, Page: 1, PageSize: 100,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "event-request", true)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if !strings.Contains(call.Query.Get("filtering"), discoveryAdapterHighID) {
					t.Fatalf("high event asset ID changed: %#v", call.Query)
				}
			},
		},
		{
			name: "deep bids", method: http.MethodGet,
			path:    "/open_api/v3.0/event_manager/deep_bid_type/get/",
			success: `{"code":0,"message":"OK","request_id":"deep-bid-request","data":{"deep_bid_type":["NET_ORDER_ROI"],"deep_bid_type_detail":[]}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchDeepBids(testRequestContext(t, "marketing"), portmarketing.DeepBidDiscoveryRequest{
					DiscoveryScope: discoveryScope(), AssetID: discoveryAdapterHighID,
					ExternalAction: "AD_CONVERT_TYPE_APP_ORDER", DeepExternalAction: "AD_CONVERT_TYPE_APP_PAY",
					DeliveryMode: "PROCEDURAL", LandingType: "SHOP", AdType: "ALL",
					MarketingGoal: "VIDEO_AND_IMAGE", ProductSetting: "SINGLE", ValueOptimizedType: "ACTION",
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "deep-bid-request", false)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if call.Query.Get("asset_id") != discoveryAdapterHighID ||
					call.Query.Get("external_action") != "AD_CONVERT_TYPE_APP_ORDER" ||
					call.Query.Get("deep_external_action") != "AD_CONVERT_TYPE_APP_PAY" {
					t.Fatalf("deep-bid SDK query changed: %#v", call.Query)
				}
			},
		},
		{
			name: "goals", method: http.MethodGet,
			path:    "/open_api/v3.0/event_manager/optimized_goal/get_v2/",
			success: `{"code":0,"message":"OK","request_id":"goal-request","data":{"asset_ids":[9007199254740993],"goals":[{"external_action":"AD_CONVERT_TYPE_APP_ORDER","optimization_name":"净成交","history_back":false,"twenty_four_hour_back":false}]}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchGoals(testRequestContext(t, "marketing"), portmarketing.GoalDiscoveryRequest{
					DiscoveryScope: discoveryScope(), LandingType: "SHOP", AdType: "ALL",
					AssetType: "THIRDPARTY", MarketingGoal: "VIDEO_AND_IMAGE",
					DeliveryMode: "PROCEDURAL", DeliveryType: "NORMAL",
					AssetID: discoveryAdapterHighID, IncludeAsset: true,
				})
				if err == nil {
					assertDiscoveryEnvelope(t, result, "goal-request", false)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if call.Query.Get("asset_id") != discoveryAdapterHighID || call.Query.Get("landing_type") != "SHOP" ||
					call.Query.Get("ad_type") != "ALL" {
					t.Fatalf("goal SDK query changed: %#v", call.Query)
				}
			},
		},
		{
			name: "administrative regions", method: http.MethodGet,
			path:    "/open_api/2/tools/admin/info/",
			success: `{"code":0,"message":"OK","request_id":"admin-request","data":{"districts":[{"name":"北京市","code":"11","sub_districts":[]}],"version":"V2_3_2"}}`,
			run: func(t *testing.T, adapter MarketingDiscoveryAdapter) error {
				result, err := adapter.FetchAdminInfo(testRequestContext(t, "marketing"), portmarketing.AdminDiscoveryRequest{
					DiscoveryScope: discoveryScope(), Codes: []string{"CN"},
				})
				if err == nil && (result.Code != 0 || result.RequestID != "admin-request" || len(result.Nodes) != 1 ||
					result.Nodes[0].Name != "北京市" || result.Nodes[0].Code != "11") {
					t.Fatalf("admin mapping changed: %#v", result)
				}
				return err
			},
			assertCall: func(t *testing.T, call discoveryAdapterCall) {
				if !strings.Contains(call.Query.Get("codes"), "CN") || call.Query.Get("language") != "ZH_CN_GOV" ||
					call.Query.Get("sub_district") != "ONE_LEVEL" || call.Query.Get("version") != "V2_3_2" {
					t.Fatalf("admin SDK query changed: %#v", call.Query)
				}
			},
		},
	}
}

func TestMarketingDiscoveryAdapterUsesEveryGeneratedSDKService(t *testing.T) {
	cases := discoveryAdapterCases()
	responses := map[string]string{}
	for _, test := range cases {
		responses[test.path] = test.success
	}
	transport := &discoveryAdapterTransport{response: func(call discoveryAdapterCall) (int, string) {
		return http.StatusOK, responses[call.Path]
	}}
	adapter := newDiscoveryAdapter(t, transport)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := len(transport.calls)
			if err := test.run(t, adapter); err != nil {
				t.Fatal(err)
			}
			if len(transport.calls) != before+1 {
				t.Fatalf("generated SDK calls = %d, want 1", len(transport.calls)-before)
			}
			call := transport.calls[len(transport.calls)-1]
			if call.Path != test.path || call.Method != test.method || call.Host != BusinessHost ||
				call.Token != discoveryAdapterToken {
				t.Fatalf("generated SDK service changed: %#v", call)
			}
			if call.Query.Get("advertiser_id") != discoveryAdapterAdvertiserID &&
				call.Query.Get("account_id") != discoveryAdapterAdvertiserID && call.Method != http.MethodPost {
				t.Fatalf("advertiser missing from generated SDK request: %#v", call)
			}
			test.assertCall(t, call)
		})
	}
	if len(transport.calls) != len(cases) {
		t.Fatalf("generated SDK matrix calls = %d, want %d", len(transport.calls), len(cases))
	}
}

func TestMarketingDiscoveryAdapterRejectsBusinessErrorsFromEveryService(t *testing.T) {
	for _, test := range discoveryAdapterCases() {
		t.Run(test.name, func(t *testing.T) {
			transport := &discoveryAdapterTransport{response: func(discoveryAdapterCall) (int, string) {
				return http.StatusOK, `{"code":40103,"message":"synthetic authorization failure","request_id":"business-error-request"}`
			}}
			err := test.run(t, newDiscoveryAdapter(t, transport))
			var envelope *EnvelopeError
			if !errors.As(err, &envelope) || envelope.Code != 40103 ||
				envelope.RequestID != "business-error-request" || len(transport.calls) != 1 {
				t.Fatalf("business error was not preserved: err=%v envelope=%#v calls=%d", err, envelope, len(transport.calls))
			}
		})
	}
}

func TestMarketingDiscoveryAdapterRejectsMissingDataFromEveryService(t *testing.T) {
	for _, test := range discoveryAdapterCases() {
		t.Run(test.name, func(t *testing.T) {
			transport := &discoveryAdapterTransport{response: func(discoveryAdapterCall) (int, string) {
				return http.StatusOK, `{"code":0,"message":"OK","request_id":"missing-data-request"}`
			}}
			err := test.run(t, newDiscoveryAdapter(t, transport))
			if err == nil || !strings.Contains(err.Error(), "missing required data") || len(transport.calls) != 1 {
				t.Fatalf("missing data was accepted: err=%v calls=%d", err, len(transport.calls))
			}
		})
	}
}

func assertDiscoveryEnvelope(
	t *testing.T,
	result domainmarketing.DiscoveryEnvelope,
	requestID string,
	wantPage bool,
) {
	t.Helper()
	if result.Code != 0 || result.Message != "OK" || result.RequestID != requestID || result.Response["code"] == nil {
		t.Fatalf("diagnostic envelope changed: %#v", result)
	}
	if wantPage && result.PageInfo == nil {
		t.Fatalf("page_info missing: %#v", result)
	}
	if !wantPage && result.PageInfo != nil {
		t.Fatalf("unexpected page_info: %#v", result.PageInfo)
	}
}

func assertHighPlatformID(t *testing.T, call discoveryAdapterCall) {
	t.Helper()
	if call.Query.Get("platform_id") != discoveryAdapterHighID {
		t.Fatalf("high platform ID changed: %#v", call.Query)
	}
}
