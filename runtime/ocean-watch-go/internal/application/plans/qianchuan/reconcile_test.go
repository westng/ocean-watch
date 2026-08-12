package qianchuan_test

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

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	plansqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const (
	fixtureAdvertiserID = "1000000000000001"
	fixtureToken        = "TEST_QIANCHUAN_RECONCILIATION_TOKEN_DO_NOT_USE"
	fixtureAwemeID      = "4000000000000001"
	fixtureVisibleID    = "creator-visible"
	fixtureProductID    = "5000000000000001"
)

type reconciliationCall struct {
	Path  string
	Query url.Values
}

type reconciliationTransport struct {
	calls           []reconciliationCall
	pageTwoAttempts int
}

func (transport *reconciliationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := reconciliationCall{Path: request.URL.Path, Query: request.URL.Query()}
	transport.calls = append(transport.calls, call)
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch call.Path {
	case "/open_api/v1.0/qianchuan/uni_promotion/list/":
		page, _ := strconv.Atoi(call.Query.Get("page"))
		if page == 2 && transport.pageTwoAttempts < 2 {
			transport.pageTwoAttempts++
			code := 40100
			if transport.pageTwoAttempts == 2 {
				code = 51010
			}
			body = `{"code":` + strconv.Itoa(code) + `,"message":"synthetic transient","request_id":"retry"}`
			break
		}
		body = currentPlanPage(page)
	case "/open_api/v1.0/qianchuan/uni_promotion/ad/detail/":
		body = currentPlanDetail(call.Query.Get("ad_id"))
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
		Request: request,
	}, nil
}

func TestCurrentDayPlanReconciliation(t *testing.T) {
	transport := &reconciliationTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := oceanengine.QianchuanReadAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
			Jitter: func(delay time.Duration) time.Duration { return delay },
		},
	}
	reconciler := plansqianchuan.CurrentDayReconciler{
		Reader: reader,
		Now: func() time.Time {
			return time.Date(2026, time.July, 25, 16, 30, 0, 0, time.UTC)
		},
	}
	result, err := reconciler.FindCurrentPlans(qianchuanRequestContext(t), plansqianchuan.CurrentPlanRequest{
		AdvertiserID: fixtureAdvertiserID, AccessToken: fixtureToken,
		Targets: []plansqianchuan.CreatorTarget{{
			AwemeID: fixtureAwemeID, VisibleID: fixtureVisibleID,
			ProductIDs: []string{fixtureProductID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StartTime != "2026-07-26 00:00:00" || result.EndTime != "2026-07-26 23:59:59" || result.PageCount != 3 {
		t.Fatalf("current-day window changed: %#v", result)
	}
	matches := result.Matches[fixtureAwemeID]
	if len(matches) != 1 || matches[0].AdID != "2000000000000001" ||
		matches[0].Status != "DISABLE" || !reflect.DeepEqual(matches[0].ProductIDs, []string{fixtureProductID}) {
		t.Fatalf("current-day exact match changed: %#v", matches)
	}
	pageSequence := []int{}
	detailIDs := []string{}
	for _, call := range transport.calls {
		switch call.Path {
		case "/open_api/v1.0/qianchuan/uni_promotion/list/":
			if call.Query.Get("start_time") != "2026-07-26 00:00:00" ||
				call.Query.Get("end_time") != "2026-07-26 23:59:59" ||
				call.Query.Get("marketing_goal") != "VIDEO_PROM_GOODS" ||
				call.Query.Get("adlab_scene") != "UNI_PROJECT" ||
				call.Query.Get("page_size") != "100" {
				t.Fatalf("current-day list parameters changed: %#v", call.Query)
			}
			page, _ := strconv.Atoi(call.Query.Get("page"))
			pageSequence = append(pageSequence, page)
		case "/open_api/v1.0/qianchuan/uni_promotion/ad/detail/":
			detailIDs = append(detailIDs, call.Query.Get("ad_id"))
		}
	}
	if !reflect.DeepEqual(pageSequence, []int{1, 2, 2, 2, 3}) {
		t.Fatalf("current-day page sequence = %v", pageSequence)
	}
	if !reflect.DeepEqual(detailIDs, []string{
		"2000000000000001", "2000000000000002", "2000000000000003",
	}) {
		t.Fatalf("detail queries escaped candidate set: %v", detailIDs)
	}
}

func currentPlanPage(page int) string {
	rows := map[int]string{
		1: `[{"ad_info":{"id":2000000000000001,"name":"paused","status":"DISABLE","opt_status":"DISABLE"},"room_info":[{"anchor_id":"creator-visible"}]},{"ad_info":{"id":2000000000000099,"name":"not-candidate","status":"DELIVERY_OK","opt_status":"ENABLE"},"room_info":[{"anchor_id":"other-visible"}]}]`,
		2: `[{"ad_info":{"id":2000000000000002,"name":"wrong-numeric","status":"DELIVERY_OK","opt_status":"ENABLE"},"room_info":[{"anchor_id":"creator-visible"}]},{"ad_info":{"id":2000000000000003,"name":"wrong-product","status":"DELIVERY_OK","opt_status":"ENABLE"},"room_info":[{"anchor_id":"creator-visible"}]}]`,
		3: `[{"ad_info":{"id":2000000000000004,"name":"deleted","status":"DELETE","opt_status":"DELETE"},"room_info":[{"anchor_id":"creator-visible"}]}]`,
	}
	return `{"code":0,"message":"OK","request_id":"page-` + strconv.Itoa(page) + `","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":3,"total_num":5},"ad_list":` + rows[page] + `}}`
}

func currentPlanDetail(adID string) string {
	data := map[string]string{
		"2000000000000001": `{"ad_id":2000000000000001,"aweme_id":4000000000000001,"name":"paused","status":"DISABLE","opt_status":"DISABLE","product_infos":[{"product_id":5000000000000001}]}`,
		"2000000000000002": `{"ad_id":2000000000000002,"aweme_id":4000000000000002,"name":"wrong-numeric","status":"DELIVERY_OK","opt_status":"ENABLE","product_infos":[{"product_id":5000000000000001}]}`,
		"2000000000000003": `{"ad_id":2000000000000003,"aweme_id":4000000000000001,"name":"wrong-product","status":"DELIVERY_OK","opt_status":"ENABLE","product_infos":[{"product_id":5000000000000002}]}`,
	}
	return `{"code":0,"message":"OK","request_id":"detail-` + adID + `","data":` + data[adID] + `}`
}

func qianchuanRequestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, _, _, err := requestcontrol.PrepareCommandContext(context.Background(), 256)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = requestcontrol.WithAuthorization(ctx, "qianchuan", "fixture-authorization")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

type createCredentialSpy struct {
	calls int
}

func (spy *createCredentialSpy) AccessToken(
	context.Context,
	domainplans.Channel,
	string,
	string,
) (sharedplans.CredentialLease, error) {
	spy.calls++
	return sharedplans.CredentialLease{
		AuthorizationID: "fixture-authorization", AccessToken: fixtureToken,
	}, nil
}

type createLockSpy struct {
	calls int
}

func (spy *createLockSpy) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	spy.calls++
	return func() error { return nil }, nil
}

type unknownCreateWriter struct {
	calls int
}

func (writer *unknownCreateWriter) CreatePlan(
	context.Context,
	portqianchuan.CreatePlanRequest,
) (portqianchuan.WriteResult, error) {
	writer.calls++
	return portqianchuan.WriteResult{}, &domainplans.DispatchFailure{
		State: domainplans.DispatchUnknown, Cause: errors.New("synthetic response loss"),
	}
}

func (*unknownCreateWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected add")
}

func (*unknownCreateWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected delete")
}

func (*unknownCreateWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected mutation")
}

type createFinderSpy struct {
	calls int
}

func (finder *createFinderSpy) FindCurrentPlans(
	_ context.Context,
	request plansqianchuan.CurrentPlanRequest,
) (plansqianchuan.CurrentPlanResult, error) {
	finder.calls++
	return plansqianchuan.CurrentPlanResult{Matches: map[string][]plansqianchuan.ExistingPlan{
		fixtureAwemeID: {{
			AdID: "2000000000000009", Name: "stable-plan", AwemeID: fixtureAwemeID,
			ProductIDs: []string{fixtureProductID},
		}},
	}}, nil
}

func TestUnknownCreateQueriesCurrentStateWithoutReplay(t *testing.T) {
	credentials := &createCredentialSpy{}
	locks := &createLockSpy{}
	writer := &unknownCreateWriter{}
	finder := &createFinderSpy{}
	executor := plansqianchuan.CreateExecutor{
		Guard:  sharedplans.GuardedExecutor{Credentials: credentials, Locks: locks},
		Writer: writer, Reconciler: finder,
	}
	result, err := executor.Execute(context.Background(), plansqianchuan.CreateRequest{
		AdvertiserID: fixtureAdvertiserID, Submit: true, VisibleID: fixtureVisibleID,
		Payload: qianchuanCreateFixturePayload(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "reconciled" || result.AdID != "2000000000000009" ||
		result.DispatchState != domainplans.DispatchUnknown || result.Reconciliation == nil ||
		result.Reconciliation.State != domainplans.ReconciliationApplied {
		t.Fatalf("unknown create reconciliation changed: %#v", result)
	}
	if writer.calls != 1 || finder.calls != 1 || credentials.calls != 1 || locks.calls != 1 {
		t.Fatalf("unknown create was replayed or skipped guard: writer=%d finder=%d credentials=%d locks=%d", writer.calls, finder.calls, credentials.calls, locks.calls)
	}
}

func TestCreateDryRunNeedsNoCredentialsOrWriter(t *testing.T) {
	result, err := (plansqianchuan.CreateExecutor{}).Execute(context.Background(), plansqianchuan.CreateRequest{
		AdvertiserID: fixtureAdvertiserID, Payload: qianchuanCreateFixturePayload(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "dry_run" || result.Status != "ready" || result.Endpoint != plansqianchuan.CreateEndpoint {
		t.Fatalf("Qianchuan dry-run boundary changed: %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), fixtureToken) {
		t.Fatal("dry-run exposed a credential")
	}
}

func qianchuanCreateFixturePayload() json.RawMessage {
	return json.RawMessage(`{"advertiser_id":1000000000000001,"aweme_id":4000000000000001,"name":"stable-plan","marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`)
}
