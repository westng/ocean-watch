package oceanengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

type qianchuanReadCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type qianchuanReadTransport struct {
	calls              []qianchuanReadCall
	planPageTwoAttempt int
	detailAttempt      int
}

func (transport *qianchuanReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := qianchuanReadCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.calls = append(transport.calls, call)
	body := `{"code":40400,"message":"unexpected fixture route"}`
	switch request.URL.Path {
	case "/open_api/v1.0/qianchuan/uni_promotion/product/get/":
		page, _ := strconv.Atoi(call.Query.Get("page"))
		body = `{"code":0,"message":"OK","request_id":"product-request","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"total_page":1,"total_number":1},"product_list":[{"id":3000000000000001,"name":"Fixture product"}]}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/list/":
		page, _ := strconv.Atoi(call.Query.Get("page"))
		if page == 2 && transport.planPageTwoAttempt < 2 {
			codes := []int{40100, 51010}
			code := codes[transport.planPageTwoAttempt]
			transport.planPageTwoAttempt++
			body = `{"code":` + strconv.Itoa(code) + `,"message":"synthetic transient","request_id":"plan-retry"}`
			break
		}
		planID := 2000000000000000 + page
		body = `{"code":0,"message":"OK","request_id":"plan-page-` + strconv.Itoa(page) + `","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":3,"total_num":3},"ad_list":[{"ad_info":{"id":` + strconv.Itoa(planID) + `,"name":"Fixture plan"},"stats_info":{"stat_cost":99999999.99}}]}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/ad/detail/":
		transport.detailAttempt++
		if transport.detailAttempt == 1 {
			body = `{"code":50000,"message":"temporary RPC timeout","request_id":"detail-retry"}`
			break
		}
		body = `{"code":0,"message":"OK","request_id":"detail-request","data":{"ad_id":2000000000000001,"name":"Fixture plan"}}`
	case "/open_api/v1.0/qianchuan/uni_promotion/ad/material/get/":
		page, _ := strconv.Atoi(call.Query.Get("page"))
		body = `{"code":0,"message":"OK","request_id":"material-request","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"total_page":1,"total_number":1},"ad_material_infos":[{"material_info":{"material_type":"VIDEO","video_material":{"material_id":5000000000000001,"aweme_item_id":5000000000000002,"video_id":"video-fixture","title":"Fixture material"}},"stats_info":{"stat_cost":88888888.88}}]}}`
	case "/open_api/v1.0/qianchuan/file/video/aweme/get/":
		body = `{"code":0,"message":"OK","request_id":"video-request","data":{"page_info":{"count":1,"has_more":0,"cursor":0},"video_list":[{"aweme_item_id":6000000000000001,"image_mode":"VIDEO_VERTICAL","title":"Fixture work"}]}}`
	}
	return qianchuanFixtureResponse(request, body), nil
}

func qianchuanFixtureResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
		Request: request,
	}
}

func qianchuanReadFixture(t *testing.T) (*qianchuanReadTransport, QianchuanReadAdapter) {
	t.Helper()
	transport := &qianchuanReadTransport{}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return transport, QianchuanReadAdapter{Factory: factory, Retry: platformretry.Policy{
		Delays: []time.Duration{0, 0},
		Sleep:  func(context.Context, time.Duration) error { return nil },
	}}
}

func TestQianchuanReadGeneratedServiceContracts(t *testing.T) {
	transport, adapter := qianchuanReadFixture(t)
	ctx := testRequestContext(t, "qianchuan")
	token := "TEST_ACCESS_TOKEN_DO_NOT_USE"
	if _, err := adapter.FetchProducts(ctx, portqianchuan.ProductPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token,
		ProductIDs: []string{"3000000000000001"}, ProductName: "Fixture", Tab: "ALL",
		AwemeID: "4000000000000001", OnlyUnpromoted: true, OrderField: "AUDIT_TIME",
		OrderType: "DESC", Platform: "QIANCHUAN", Page: 1, PageSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.FetchPlanMaterials(ctx, portqianchuan.MaterialPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token, AdID: "2000000000000001",
		MaterialType: "VIDEO", MaterialStatus: "ALL", Page: 1, PageSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.FetchCreatorVideos(ctx, portqianchuan.CreatorVideoPageRequest{
		AdvertiserID: "1000000000000001", AccessToken: token,
		AwemeID: "4000000000000001", ProductID: "3000000000000001",
		AwemeItemIDs: []string{"6000000000000001", "6000000000000002"}, Count: 50,
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 3 {
		t.Fatalf("generated service call count = %d", len(transport.calls))
	}
	for index, call := range transport.calls {
		expectedHost := BusinessHost
		if index == 2 {
			expectedHost = OAuthHost
		}
		if call.Method != http.MethodGet || call.Host != expectedHost || call.Token != token {
			t.Fatalf("Qianchuan read escaped generated-service boundary: %#v", call)
		}
	}
	product := transport.calls[0]
	if product.Path != "/open_api/v1.0/qianchuan/uni_promotion/product/get/" ||
		product.Query.Get("advertiser_id") != "1000000000000001" || product.Query.Get("aweme_id") != "4000000000000001" ||
		product.Query.Get("order_field") != "AUDIT_TIME" || product.Query.Get("order_type") != "DESC" ||
		product.Query.Get("platfrom") != "QIANCHUAN" || product.Query.Get("page") != "1" || product.Query.Get("page_size") != "100" {
		t.Fatalf("product request parameters changed: %#v", product)
	}
	var productFilter map[string]any
	if err := json.Unmarshal([]byte(product.Query.Get("filtering")), &productFilter); err != nil ||
		!reflect.DeepEqual(productFilter["product_ids"], []any{float64(3000000000000001)}) ||
		productFilter["product_name"] != "Fixture" || productFilter["tab"] != "ALL" ||
		productFilter["create_roi2_limit_product"] != true {
		t.Fatalf("product filtering changed: value=%q decoded=%#v err=%v", product.Query.Get("filtering"), productFilter, err)
	}
	material := transport.calls[1]
	if material.Path != "/open_api/v1.0/qianchuan/uni_promotion/ad/material/get/" ||
		material.Query.Get("advertiser_id") != "1000000000000001" || material.Query.Get("ad_id") != "2000000000000001" ||
		material.Query.Get("page") != "1" || material.Query.Get("page_size") != "100" || material.Query.Get("fields") != "" {
		t.Fatalf("material request parameters changed or requested finance fields: %#v", material)
	}
	var materialFilter map[string]any
	if err := json.Unmarshal([]byte(material.Query.Get("filtering")), &materialFilter); err != nil ||
		materialFilter["material_type"] != "VIDEO" || materialFilter["material_status"] != "ALL" {
		t.Fatalf("material filtering changed: value=%q decoded=%#v err=%v", material.Query.Get("filtering"), materialFilter, err)
	}
	video := transport.calls[2]
	if video.Path != "/open_api/v1.0/qianchuan/file/video/aweme/get/" ||
		video.Query.Get("advertiser_id") != "1000000000000001" ||
		video.Query.Get("aweme_id") != "4000000000000001" || video.Query.Get("count") != "50" {
		t.Fatalf("creator-video request parameters changed: %#v", video)
	}
	var videoFilter map[string]any
	if err := json.Unmarshal([]byte(video.Query.Get("filtering")), &videoFilter); err != nil ||
		!reflect.DeepEqual(videoFilter["aweme_item_ids"], []any{float64(6000000000000001), float64(6000000000000002)}) ||
		videoFilter["product_id"] != float64(3000000000000001) {
		t.Fatalf("creator-video filtering changed: value=%q decoded=%#v err=%v", video.Query.Get("filtering"), videoFilter, err)
	}
}

type qianchuanTransportTokens struct{}

func (qianchuanTransportTokens) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: testAuthorizationID,
		AccessToken: "TEST_ACCESS_TOKEN_DO_NOT_USE",
	}, nil
}

func TestQianchuanPlanListRetriesOnlyCurrentPageAndDropsStatsInfo(t *testing.T) {
	transport, adapter := qianchuanReadFixture(t)
	result, err := (applicationqianchuan.Service{
		Tokens: qianchuanTransportTokens{}, Reader: adapter,
		Now: func() time.Time { return time.Date(2026, 7, 25, 1, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)) },
	}).ListPlans(testRequestContext(t, "qianchuan"), applicationqianchuan.PlanListQuery{
		CredentialScope: applicationqianchuan.CredentialScope{AdvertiserID: "1000000000000001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := make([]int, 0, len(transport.calls))
	for _, call := range transport.calls {
		if call.Path != "/open_api/v1.0/qianchuan/uni_promotion/list/" || call.Host != BusinessHost ||
			call.Token != "TEST_ACCESS_TOKEN_DO_NOT_USE" || call.Query.Get("start_time") != "2026-07-25 00:00:00" ||
			call.Query.Get("end_time") != "2026-07-25 23:59:59" || call.Query.Get("marketing_goal") != "VIDEO_PROM_GOODS" ||
			call.Query.Get("adlab_scene") != "UNI_PROJECT" || call.Query.Get("need_compensate_info") != "true" ||
			call.Query.Get("order_field") != "create_time" || call.Query.Get("order_type") != "DESC" ||
			call.Query.Get("page_size") != "100" {
			t.Fatalf("plan-list contract changed: %#v", call)
		}
		var fields []string
		if err := json.Unmarshal([]byte(call.Query.Get("fields")), &fields); err != nil || !reflect.DeepEqual(fields, []string{"stat_cost"}) {
			t.Fatalf("plan-list minimum SDK fields changed: value=%q decoded=%v err=%v", call.Query.Get("fields"), fields, err)
		}
		sequence = append(sequence, mustAtoi(t, call.Query.Get("page")))
	}
	if !reflect.DeepEqual(sequence, []int{1, 2, 2, 2, 3}) {
		t.Fatalf("plan-list page sequence = %v", sequence)
	}
	if result.PlanCount != 3 || result.PageCount != 3 {
		t.Fatalf("plan-list result incomplete: %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "stats_info") || strings.Contains(string(encoded), "stat_cost") || strings.Contains(string(encoded), "99999999") {
		t.Fatalf("plan-list finance escaped adapter boundary: %s", encoded)
	}
}

func TestQianchuanPlanDetailRetriesTemporaryRPCTimeout(t *testing.T) {
	transport, adapter := qianchuanReadFixture(t)
	result, err := adapter.FetchPlanDetail(testRequestContext(t, "qianchuan"), portqianchuan.PlanDetailRequest{
		AdvertiserID: "1000000000000001", AccessToken: "TEST_ACCESS_TOKEN_DO_NOT_USE", AdID: "2000000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AdID != "2000000000000001" || len(transport.calls) != 2 {
		t.Fatalf("detail retry result=%#v calls=%d", result, len(transport.calls))
	}
	for _, call := range transport.calls {
		if call.Path != "/open_api/v1.0/qianchuan/uni_promotion/ad/detail/" || call.Host != BusinessHost ||
			call.Query.Get("advertiser_id") != "1000000000000001" || call.Query.Get("ad_id") != "2000000000000001" {
			t.Fatalf("detail retry changed candidate request: %#v", call)
		}
	}
}

func TestQianchuanAdapterRejectsMismatchedResponsePage(t *testing.T) {
	page := int64(2)
	totalPages := int64(2)
	totalNumber := int64(2)
	if _, err := mapInt64PageInfo(1, &page, &totalPages, &totalNumber); err == nil {
		t.Fatal("mismatched product response page was accepted")
	}
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
