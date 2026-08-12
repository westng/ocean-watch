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

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	marketingPlanAdvertiserID = "1000000000000001"
	marketingPlanProjectID    = "9007199254740991"
	marketingPlanPromotionID  = "9007199254740993"
	marketingPlanToken        = "TEST_MARKETING_PLAN_TOKEN_DO_NOT_USE"
)

type marketingPlanCall struct {
	Method string
	Path   string
	Query  url.Values
	Token  string
	Body   string
}

type marketingPlanTransport struct {
	calls    []marketingPlanCall
	response func(marketingPlanCall) (int, string, error)
}

func (transport *marketingPlanTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(payload)
	}
	call := marketingPlanCall{
		Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(),
		Token: request.Header.Get("Access-Token"), Body: body,
	}
	transport.calls = append(transport.calls, call)
	status, payload, err := http.StatusOK, `{"code":40400,"message":"unexpected synthetic route"}`, error(nil)
	if transport.response != nil {
		status, payload, err = transport.response(call)
	}
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    request,
	}, nil
}

func newMarketingPlanAdapter(t *testing.T, transport http.RoundTripper) MarketingPlanAdapter {
	t.Helper()
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	return MarketingPlanAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
			Jitter: func(delay time.Duration) time.Duration { return delay },
		},
	}
}

func TestMarketingPlanAdapterUsesGeneratedCreateServices(t *testing.T) {
	transport := &marketingPlanTransport{}
	transport.response = func(call marketingPlanCall) (int, string, error) {
		switch call.Path {
		case "/open_api/v3.0/project/create/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"project-request","data":{"project_id":9007199254740991}}`, nil
		case "/open_api/v3.0/promotion/create/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"promotion-request","data":{"promotion_id":9007199254740993}}`, nil
		default:
			return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
		}
	}
	adapter := newMarketingPlanAdapter(t, transport)
	ctx := testRequestContext(t, "marketing")
	project, err := adapter.CreateProject(ctx, portmarketing.ProjectCreateRequest{
		AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
		Payload: marketingProjectPayload(marketingPlanAdvertiserID, "stable-project"),
	})
	if err != nil {
		t.Fatal(err)
	}
	promotion, err := adapter.CreatePromotion(ctx, portmarketing.PromotionCreateRequest{
		AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
		ProjectID: marketingPlanProjectID,
		Payload:   marketingPromotionPayload(marketingPlanAdvertiserID, marketingPlanProjectID, "stable-promotion"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.ObjectID != marketingPlanProjectID || project.RequestID != "project-request" ||
		promotion.ObjectID != marketingPlanPromotionID || promotion.RequestID != "promotion-request" ||
		len(transport.calls) != 2 {
		t.Fatalf("create mapping changed: project=%#v promotion=%#v calls=%#v", project, promotion, transport.calls)
	}
	for _, call := range transport.calls {
		if call.Method != http.MethodPost || call.Token != marketingPlanToken {
			t.Fatalf("generated SDK request changed: %#v", call)
		}
		var body map[string]any
		decoder := json.NewDecoder(strings.NewReader(call.Body))
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["advertiser_id"].(json.Number).String() != marketingPlanAdvertiserID {
			t.Fatalf("high advertiser ID changed: %s", call.Body)
		}
	}
	if !strings.Contains(transport.calls[1].Body, marketingPlanProjectID) {
		t.Fatalf("promotion did not receive the transaction project ID: %s", transport.calls[1].Body)
	}
}

func TestMarketingPlanAdapterClassifiesWriteDispatchBoundary(t *testing.T) {
	t.Run("payload blocked before transport", func(t *testing.T) {
		transport := &marketingPlanTransport{}
		adapter := newMarketingPlanAdapter(t, transport)
		_, err := adapter.CreateProject(testRequestContext(t, "marketing"), portmarketing.ProjectCreateRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Payload: marketingProjectPayload("1000000000000002", "wrong-scope"),
		})
		assertWriteState(t, err, domainplans.DispatchNotSent)
		if len(transport.calls) != 0 {
			t.Fatal("invalid payload reached the transport")
		}
	})

	t.Run("governor blocked before transport", func(t *testing.T) {
		transport := &marketingPlanTransport{}
		adapter := newMarketingPlanAdapter(t, transport)
		ctx, _, _ := controlledTestRequestContext(t, "marketing", testAuthorizationID, 0)
		_, err := adapter.CreateProject(ctx, portmarketing.ProjectCreateRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Payload: marketingProjectPayload(marketingPlanAdvertiserID, "blocked-project"),
		})
		assertWriteState(t, err, domainplans.DispatchNotSent)
		if len(transport.calls) != 0 {
			t.Fatal("request budget failure reached the transport")
		}
	})

	t.Run("disconnect after transport", func(t *testing.T) {
		transport := &marketingPlanTransport{response: func(marketingPlanCall) (int, string, error) {
			return 0, "", errors.New("synthetic connection reset")
		}}
		adapter := newMarketingPlanAdapter(t, transport)
		_, err := adapter.CreateProject(testRequestContext(t, "marketing"), portmarketing.ProjectCreateRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Payload: marketingProjectPayload(marketingPlanAdvertiserID, "unknown-project"),
		})
		assertWriteState(t, err, domainplans.DispatchUnknown)
		if len(transport.calls) != 1 {
			t.Fatalf("unknown write call count = %d", len(transport.calls))
		}
	})

	t.Run("business rejection acknowledged", func(t *testing.T) {
		transport := &marketingPlanTransport{response: func(marketingPlanCall) (int, string, error) {
			return http.StatusOK, `{"code":40000,"message":"fixture rejection","request_id":"rejected"}`, nil
		}}
		adapter := newMarketingPlanAdapter(t, transport)
		_, err := adapter.CreateProject(testRequestContext(t, "marketing"), portmarketing.ProjectCreateRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Payload: marketingProjectPayload(marketingPlanAdvertiserID, "rejected-project"),
		})
		assertWriteState(t, err, domainplans.DispatchAcknowledged)
		if len(transport.calls) != 1 {
			t.Fatalf("business rejection call count = %d", len(transport.calls))
		}
	})

	t.Run("malformed success remains unknown", func(t *testing.T) {
		transport := &marketingPlanTransport{response: func(marketingPlanCall) (int, string, error) {
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"missing-id","data":{}}`, nil
		}}
		adapter := newMarketingPlanAdapter(t, transport)
		_, err := adapter.CreateProject(testRequestContext(t, "marketing"), portmarketing.ProjectCreateRequest{
			AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
			Payload: marketingProjectPayload(marketingPlanAdvertiserID, "missing-id-project"),
		})
		assertWriteState(t, err, domainplans.DispatchUnknown)
	})
}

func TestMarketingPlanAdapterReconciliationUsesPageLocalRetryAndExactKeys(t *testing.T) {
	projectSequence := []int{}
	pageTwoAttempts := 0
	transport := &marketingPlanTransport{}
	transport.response = func(call marketingPlanCall) (int, string, error) {
		switch call.Path {
		case "/open_api/v3.0/project/list/":
			page, _ := strconv.Atoi(call.Query.Get("page"))
			projectSequence = append(projectSequence, page)
			if page == 2 && pageTwoAttempts < 2 {
				pageTwoAttempts++
				return http.StatusOK, `{"code":40100,"message":"rate limited","request_id":"retry"}`, nil
			}
			name := "stable-project"
			if page == 2 {
				name = "stable-project-fuzzy"
			}
			return http.StatusOK, projectPageFixture(page, 3, 3, int64(9007199254740990)+int64(page), name), nil
		case "/open_api/v3.0/promotion/list/":
			return http.StatusOK, `{"code":0,"message":"OK","request_id":"promotion-page","data":{"list":[{"promotion_id":9007199254740993,"project_id":9007199254740991,"promotion_name":"stable-promotion"},{"promotion_id":9007199254740994,"project_id":9007199254740992,"promotion_name":"stable-promotion"}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":2}}}`, nil
		default:
			return http.StatusNotFound, `{"code":40400,"message":"unexpected"}`, nil
		}
	}
	adapter := newMarketingPlanAdapter(t, transport)
	ctx := testRequestContext(t, "marketing")
	projects, err := adapter.FindProjects(ctx, portmarketing.ProjectReconciliationRequest{
		AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken, Name: "stable-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectSequence, []int{1, 2, 2, 2, 3}) ||
		!reflect.DeepEqual(projects, []string{"9007199254740991", "9007199254740993"}) {
		t.Fatalf("project reconciliation changed: sequence=%v candidates=%v", projectSequence, projects)
	}
	promotions, err := adapter.FindPromotions(ctx, portmarketing.PromotionReconciliationRequest{
		AdvertiserID: marketingPlanAdvertiserID, AccessToken: marketingPlanToken,
		ProjectID: marketingPlanProjectID, Name: "stable-promotion",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(promotions, []string{marketingPlanPromotionID}) {
		t.Fatalf("promotion reconciliation crossed project scope: %v", promotions)
	}
	for _, call := range transport.calls {
		if call.Path == "/open_api/v3.0/project/list/" || call.Path == "/open_api/v3.0/promotion/list/" {
			if call.Method != http.MethodGet || call.Token != marketingPlanToken || call.Query.Get("page_size") != "100" {
				t.Fatalf("reconciliation SDK request changed: %#v", call)
			}
		}
	}
}

func assertWriteState(t *testing.T, err error, expected domainplans.DispatchState) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s write failure", expected)
	}
	var failure *domainplans.DispatchFailure
	if !errors.As(err, &failure) || failure.State != expected {
		t.Fatalf("write state = %#v, want %s", failure, expected)
	}
}

func marketingProjectPayload(advertiserID, name string) json.RawMessage {
	return json.RawMessage(`{"advertiser_id":` + advertiserID + `,"name":` + strconv.Quote(name) + `}`)
}

func marketingPromotionPayload(advertiserID, projectID, name string) json.RawMessage {
	return json.RawMessage(`{"advertiser_id":` + advertiserID + `,"project_id":` + projectID + `,"name":` + strconv.Quote(name) + `}`)
}

func projectPageFixture(page, totalPages, totalNumber int, projectID int64, name string) string {
	return `{"code":0,"message":"OK","request_id":"project-page-` + strconv.Itoa(page) +
		`","data":{"list":[{"project_id":` + strconv.FormatInt(projectID, 10) + `,"name":` + strconv.Quote(name) +
		`}],"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":` +
		strconv.Itoa(totalPages) + `,"total_number":` + strconv.Itoa(totalNumber) + `}}}`
}
