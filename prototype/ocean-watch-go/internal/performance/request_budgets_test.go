package performance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	applicationaccounts "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/accounts"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	applicationqianchuanplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/qianchuan"
	applicationqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/qianchuan"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/cli"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	platformrequestcontrol "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	fixtureMarketingAdvertiser = "1000000000000001"
	fixtureQianchuanAdvertiser = "1000000000000002"
)

func TestRequestBudgets(t *testing.T) {
	t.Run("accounts list uses zero network attempts", testAccountsListUsesZeroNetworkAttempts)
	t.Run("two account report never lists plans", testTwoAccountReportNeverListsPlans)
	t.Run("failed page retries only itself", testFailedPageRetriesOnlyItself)
	t.Run("55 link authorization scan", testFiftyFiveLinkAuthorizationScan)
}

type batchVerificationBudgetReader struct {
	authorizedCalls     int
	ownershipBatchSizes []int
	productBatchSizes   []int
}

func (*batchVerificationBudgetReader) FetchProducts(
	context.Context,
	portqianchuan.ProductPageRequest,
) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, nil
}

func (*batchVerificationBudgetReader) FetchPlans(
	context.Context,
	portqianchuan.PlanPageRequest,
) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, nil
}

func (*batchVerificationBudgetReader) FetchPlanDetail(
	context.Context,
	portqianchuan.PlanDetailRequest,
) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, nil
}

func (*batchVerificationBudgetReader) FetchPlanMaterials(
	context.Context,
	portqianchuan.MaterialPageRequest,
) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, nil
}

func (reader *batchVerificationBudgetReader) FetchAuthorizedCreators(
	_ context.Context,
	request portqianchuan.AuthorizedCreatorPageRequest,
) (domainqianchuan.AuthorizedCreatorPage, error) {
	reader.authorizedCalls++
	if request.SearchKeyword != "" || request.Page != 1 || request.PageSize != 100 {
		return domainqianchuan.AuthorizedCreatorPage{}, errors.New("55-link fixture escaped one broad creator scan")
	}
	return domainqianchuan.AuthorizedCreatorPage{
		Rows: []domainqianchuan.AuthorizedCreator{{
			AwemeID: "4000000000000001", VisibleID: "fixture-visible", Name: "fixture-creator",
		}},
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1},
	}, nil
}

func (reader *batchVerificationBudgetReader) FetchCreatorVideos(
	_ context.Context,
	request portqianchuan.CreatorVideoPageRequest,
) (domainqianchuan.CreatorVideoPage, error) {
	if request.AwemeID != "4000000000000001" ||
		len(request.AwemeItemIDs) > applicationqianchuanplans.WorkQueryBatchSize {
		return domainqianchuan.CreatorVideoPage{}, errors.New("55-link fixture exceeded the official work-query boundary")
	}
	if request.ProductID == "" {
		reader.ownershipBatchSizes = append(reader.ownershipBatchSizes, len(request.AwemeItemIDs))
	} else {
		reader.productBatchSizes = append(reader.productBatchSizes, len(request.AwemeItemIDs))
	}
	rows := make([]domainqianchuan.CreatorVideo, 0, len(request.AwemeItemIDs))
	for _, itemID := range request.AwemeItemIDs {
		rows = append(rows, domainqianchuan.CreatorVideo{
			AwemeItemID: itemID,
			ImageMode:   "VIDEO_LARGE",
			MaterialID:  "material-" + itemID,
			Title:       "title-" + itemID,
		})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

func testFiftyFiveLinkAuthorizationScan(t *testing.T) {
	reader := &batchVerificationBudgetReader{}
	works := make([]applicationqianchuanplans.WorkInput, 0, 55)
	for index := 1; index <= 55; index++ {
		itemID := strconv.FormatInt(6000000000000000+int64(index), 10)
		works = append(works, applicationqianchuanplans.WorkInput{
			InputIndex:  index - 1,
			InputURL:    "https://www.douyin.com/video/" + itemID,
			AwemeItemID: itemID,
		})
	}
	result, err := (applicationqianchuanplans.WorkVerifier{Reader: reader}).Verify(
		context.Background(),
		applicationqianchuanplans.WorkVerificationRequest{
			AdvertiserID: fixtureQianchuanAdvertiser,
			AccessToken:  "TEST_QIANCHUAN_BATCH_BUDGET_TOKEN_DO_NOT_USE",
			ProductIDs:   []string{"5000000000000001"},
			Works:        works,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizedCreatorScanCount != 1 || reader.authorizedCalls != 1 {
		t.Fatalf("55-link broad creator scans = result:%d reader:%d, want 1",
			result.AuthorizedCreatorScanCount, reader.authorizedCalls)
	}
	if !reflect.DeepEqual(reader.ownershipBatchSizes, []int{50, 5}) ||
		!reflect.DeepEqual(reader.productBatchSizes, []int{50, 5}) {
		t.Fatalf("55-link query batches = ownership:%v product:%v, want [50 5] for each",
			reader.ownershipBatchSizes, reader.productBatchSizes)
	}
	if result.OwnershipQueryCount != 2 || result.ProductQueryCount != 2 ||
		len(result.Matched) != 55 || len(result.Skipped) != 0 {
		t.Fatalf("55-link request budget or classification changed: %#v", result)
	}
}

func testAccountsListUsesZeroNetworkAttempts(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := `{"managed_account_schema_version":1,"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"Fixture","enabled":true}],"qianchuan":[]}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var observed bool
	runner := cli.Runner{
		Routes:   application.DefaultRouteManifest(),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Cwd:      root,
		UserHome: root,
		Getenv:   func(string) string { return "" },
		RequestObserver: func(command string, budget platformrequestcontrol.BudgetSnapshot, metrics platformrequestcontrol.MetricsSnapshot) {
			observed = true
			if command != "accounts list" {
				t.Fatalf("observed command = %q", command)
			}
			if budget != (platformrequestcontrol.BudgetSnapshot{}) {
				t.Fatalf("local command request budget changed: %#v", budget)
			}
			if metrics != (platformrequestcontrol.MetricsSnapshot{}) {
				t.Fatalf("local command performed network work: %#v", metrics)
			}
		},
	}
	if exitCode := runner.Execute(context.Background(), []string{"accounts", "list", "--config", configPath}); exitCode != 0 {
		t.Fatalf("accounts list exit code = %d", exitCode)
	}
	if !observed {
		t.Fatal("request observer did not receive the local command snapshot")
	}
}

type accountStore struct {
	book domain.AccountBook
}

func (store accountStore) Read(context.Context) (domain.AccountBook, error) {
	return store.book, nil
}

type tokenProvider struct{}

func (tokenProvider) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	return authapplication.TokenLease{
		Channel:         query.Channel,
		AuthorizationID: "fixture-" + query.Channel + "-authorization",
		AccessToken:     "fixture-" + query.Channel + "-access",
	}, nil
}

type recordedCall struct {
	path  string
	query string
}

type reportTransport struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (transport *reportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.calls = append(transport.calls, recordedCall{path: request.URL.Path, query: request.URL.RawQuery})
	transport.mu.Unlock()
	body := `{"code":40400,"message":"unexpected fixture endpoint"}`
	switch request.URL.Path {
	case "/open_api/v3.0/report/custom/get/":
		body = `{"code":0,"message":"OK","request_id":"marketing-report","data":{"rows":[],"total_metrics":{"stat_cost":"12.34","in_app_order_count":"2","in_app_order_gmv":"30.00","in_app_order_roi":"2.43","in_app_order_net_count_1h":"1","in_app_order_net_gmv_1h":"20.00","in_app_order_net_roi_1h":"1.62"}}}`
	case "/open_api/v1.0/qianchuan/report/uni_promotion/get/":
		body = `{"code":0,"message":"OK","request_id":"qianchuan-report","data":{"stat_cost":193.95,"total_pay_order_count_for_roi2":1,"total_pay_order_gmv_include_coupon_for_roi2":99,"total_prepay_and_pay_order_roi2":0.5104}}`
	}
	return jsonResponse(request, body), nil
}

func testTwoAccountReportNeverListsPlans(t *testing.T) {
	transport := &reportTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	book := domain.NewAccountBook()
	book.Accounts[domain.Marketing] = []domain.ManagedAccount{{
		Channel: domain.Marketing, AdvertiserID: fixtureMarketingAdvertiser, Name: "Marketing", Enabled: true,
	}}
	book.Accounts[domain.Qianchuan] = []domain.ManagedAccount{{
		Channel: domain.Qianchuan, AdvertiserID: fixtureQianchuanAdvertiser, Name: "Qianchuan", Enabled: true,
	}}
	ctx, budget, metrics, err := platformrequestcontrol.PrepareCommandContext(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	result, reportErr := (applicationaccounts.Reporter{
		Store:      accountStore{book: book},
		Tokens:     tokenProvider{},
		Marketing:  oceanengine.MarketingAccountReportAdapter{Factory: factory},
		Qianchuan:  oceanengine.QianchuanAccountReportAdapter{Factory: factory},
		Concurrent: 2,
	}).Report(ctx, applicationaccounts.Query{StartDate: "2026-07-26", EndDate: "2026-07-26"})
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	if !result.OK || result.Summary.SuccessfulAccountCount != 2 {
		t.Fatalf("two-account report failed: %#v", result.Summary)
	}
	transport.mu.Lock()
	calls := append([]recordedCall(nil), transport.calls...)
	transport.mu.Unlock()
	paths := make([]string, 0, len(calls))
	for _, call := range calls {
		paths = append(paths, call.path)
		if strings.Contains(call.path, "/qianchuan/uni_promotion/list/") {
			t.Fatalf("account report called the Qianchuan plan list: %#v", call)
		}
	}
	wantPaths := []string{
		"/open_api/v1.0/qianchuan/report/uni_promotion/get/",
		"/open_api/v3.0/report/custom/get/",
	}
	sortedStrings(paths)
	sortedStrings(wantPaths)
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("account report paths = %v, want %v", paths, wantPaths)
	}
	if budget.Snapshot().Used != 2 || metrics.Snapshot().Attempts != 2 {
		t.Fatalf("account report request count changed: budget=%#v metrics=%#v", budget.Snapshot(), metrics.Snapshot())
	}
}

type planTransport struct {
	mu              sync.Mutex
	calls           []int
	pageTwoFailures int
}

func (transport *planTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	page, err := strconv.Atoi(request.URL.Query().Get("page"))
	if err != nil {
		return nil, err
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, page)
	if page == 2 && transport.pageTwoFailures < 2 {
		code := []int{40100, 51010}[transport.pageTwoFailures]
		transport.pageTwoFailures++
		transport.mu.Unlock()
		return jsonResponse(request, `{"code":`+strconv.Itoa(code)+`,"message":"synthetic transient"}`), nil
	}
	transport.mu.Unlock()
	planID := 2000000000000000 + page
	body := `{"code":0,"message":"OK","request_id":"page-` + strconv.Itoa(page) + `","data":{"page_info":{"page":` + strconv.Itoa(page) + `,"page_size":100,"total_page":3,"total_num":3},"ad_list":[{"ad_info":{"id":` + strconv.Itoa(planID) + `,"name":"Fixture plan"}}]}}`
	return jsonResponse(request, body), nil
}

func testFailedPageRetriesOnlyItself(t *testing.T) {
	transport := &planTransport{}
	factory, err := oceanengine.NewClientFactory(oceanengine.FactoryOptions{
		TransportFactory: func(oceanengine.HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, budget, metrics, err := platformrequestcontrol.PrepareCommandContext(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (applicationqianchuan.Service{
		Tokens: tokenProvider{},
		Reader: oceanengine.QianchuanReadAdapter{
			Factory: factory,
			Retry: platformretry.Policy{
				Delays: []time.Duration{0, 0},
				Sleep:  func(context.Context, time.Duration) error { return nil },
			},
		},
		Now: func() time.Time {
			return time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
		},
	}).ListPlans(ctx, applicationqianchuan.PlanListQuery{
		CredentialScope: applicationqianchuan.CredentialScope{AdvertiserID: fixtureQianchuanAdvertiser},
	})
	if err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	sequence := append([]int(nil), transport.calls...)
	transport.mu.Unlock()
	if !reflect.DeepEqual(sequence, []int{1, 2, 2, 2, 3}) {
		t.Fatalf("plan-list page sequence = %v", sequence)
	}
	if result.PlanCount != 3 || result.DataPeriod.StartDate != "2026-07-26" || result.DataPeriod.EndDate != "2026-07-26" {
		t.Fatalf("current-day plan result changed: %#v", result)
	}
	if budget.Snapshot().Used != 5 || metrics.Snapshot().Attempts != 5 {
		t.Fatalf("page retries were not charged exactly once: budget=%#v metrics=%#v", budget.Snapshot(), metrics.Snapshot())
	}
}

func BenchmarkRequestBudgets(b *testing.B) {
	root := b.TempDir()
	configPath := filepath.Join(root, "config.json")
	config := `{"managed_account_schema_version":1,"managed_accounts":{"marketing":[{"advertiser_id":"1000000000000001","name":"Fixture","enabled":true}],"qianchuan":[]}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		b.Fatal(err)
	}
	runner := cli.Runner{
		Routes:   application.DefaultRouteManifest(),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Cwd:      root,
		UserHome: root,
		Getenv:   func(string) string { return "" },
	}
	b.ResetTimer()
	for range b.N {
		if exitCode := runner.Execute(context.Background(), []string{"accounts", "list", "--config", configPath}); exitCode != 0 {
			b.Fatalf("accounts list exit code = %d", exitCode)
		}
	}
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewBufferString(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}

func sortedStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && values[current] < values[current-1]; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}
