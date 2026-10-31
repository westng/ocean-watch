package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

type discoveryCall struct {
	Host  string
	Path  string
	Query url.Values
}

type discoveryTransport struct {
	mu       sync.Mutex
	calls    []discoveryCall
	respond  func(*http.Request, int) (int, string)
	perRoute map[string]int
}

func (transport *discoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	key := request.URL.Host + request.URL.Path + "?" + request.URL.RawQuery
	transport.perRoute[key]++
	attempt := transport.perRoute[key]
	transport.calls = append(transport.calls, discoveryCall{
		Host: request.URL.Host, Path: request.URL.Path, Query: request.URL.Query(),
	})
	status, body := transport.respond(request, attempt)
	transport.mu.Unlock()
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
		Request: request,
	}, nil
}

func TestQianchuanAccountDiscoveryWithSharedRequestControl(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, _ int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[
				{"account_id":9001,"account_role":"ADVERTISER","account_type":"CUSTOMER_ADMIN","advertiser_id":1001,"is_valid":true},
				{"account_id":9002,"account_role":"CUSTOMER_OPERATOR","account_type":"ADVERTISER","is_valid":true},
				{"account_id":9003,"account_role":"PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR","account_type":"ADVERTISER","is_valid":true},
				{"account_id":9004,"account_type":"PLATFORM_ROLE_QIANCHUAN_AGENT","is_valid":true},
				{"account_id":9007,"account_role":"PLATFORM_ROLE_AWEME","advertiser_id":7777,"is_valid":true},
				{"account_id":0,"account_string_id":"9005","account_role":"PLATFORM_ROLE_SHOP_ACCOUNT","is_valid":true},
				{"account_id":9006,"account_role":"ADVERTISER","advertiser_id":1001,"is_valid":false}
			]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			return 200, `{"code":0,"data":{"list":[{"advertiser_id":1002}],"page_info":{"page":1,"total_number":1,"total_page":1}}}`
		case "/open_api/2/ebp/advertiser/list/":
			return 200, `{"code":0,"data":{"account_list":[{"account_id":1003}],"page_info":{"page":1,"total_number":1,"total_page":1}}}`
		case "/open_api/2/agent/advertiser/select/":
			return 200, `{"code":0,"data":{"list":[1004],"page_info":{"page":1,"total_number":1,"total_page":1}}}`
		case "/open_api/v1.0/qianchuan/shop/advertiser/list/":
			return 200, `{"code":0,"data":{"list":[1005],"adv_id_list":[{"adv_id":1005}],"page_info":{"page":1,"total_number":1,"total_page":1}}}`
		case "/open_api/2/advertiser/info/":
			return 200, `{"code":0,"data":[{"id":1001},{"id":1002},{"id":1003},{"id":1004},{"id":1005}]}`
		default:
			t.Errorf("unexpected discovery endpoint: %s", request.URL.Path)
			return 404, `{}`
		}
	}
	shared := &sharedControlFixture{acquired: make(chan struct{}, 8)}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory:       func(HostProfile) http.RoundTripper { return transport },
		SharedQianchuanControl: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, budget, metrics := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 6)
	adapter := AdvertiserDiscoveryAdapter{Factory: factory}
	snapshot, err := adapter.Discover(ctx, "qianchuan", "fixture-access")
	if err != nil {
		t.Fatalf("account discovery without a selected advertiser failed: %v (missing advertiser scope: %t)",
			err, errors.Is(err, requestcontrol.ErrAdvertiserScopeMissing))
	}
	if len(snapshot.Accounts) != 7 || !reflect.DeepEqual(snapshot.AdvertiserIDs, []string{"1001", "1002", "1003", "1004", "1005"}) || len(snapshot.Accounts[4].AdvertiserIDs) != 0 || len(snapshot.Accounts[6].AdvertiserIDs) != 0 {
		t.Fatalf("incomplete account snapshot: %#v", snapshot)
	}
	if len(snapshot.DiscoveryIssues) != 1 || snapshot.DiscoveryIssues[0].AccountID != "9007" || snapshot.DiscoveryIssues[0].Role != "PLATFORM_ROLE_AWEME" || snapshot.DiscoveryIssues[0].Code != "non_advertiser_role" {
		t.Fatalf("non-advertiser account did not retain its discovery notice: %#v", snapshot.DiscoveryIssues)
	}
	if len(transport.calls) != 6 || budget.Snapshot().Used != 6 || metrics.Snapshot().Attempts != 6 {
		t.Fatalf("discovery request accounting changed: calls=%d budget=%#v metrics=%#v",
			len(transport.calls), budget.Snapshot(), metrics.Snapshot())
	}
	if len(shared.acquired) != 0 {
		t.Fatal("authorization-wide account discovery acquired an advertiser-specific controller")
	}
	if transport.calls[0].Host != BusinessHost || transport.calls[1].Query.Get("cc_account_id") != "9002" || transport.calls[2].Query.Get("enterprise_organization_id") != "9003" || transport.calls[4].Query.Get("shop_id") != "9005" {
		t.Fatalf("role expansion used the wrong root identity: %#v", transport.calls)
	}
	if transport.calls[1].Query.Get("account_source") != "QIANCHUAN" || transport.calls[2].Query.Get("account_source") != "QIANCHUAN" || transport.calls[1].Query.Get("page_size") != "100" || !strings.Contains(transport.calls[4].Query.Get("permission"), "QC_AWEME") {
		t.Fatalf("role expansion omitted the channel or pagination parameters: %#v", transport.calls)
	}
}

func TestAdvertiserDiscoveryVerifiesDeduplicatedRoleAccountsInBatches(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	var batchSizes []int
	transport.respond = func(request *http.Request, _ int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[{"account_id":0,"account_string_id":"","advertiser_id":1001,"account_role":"ADVERTISER"},{"account_id":9002,"account_role":"CUSTOMER_OPERATOR"}]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			rows := make([]string, 51)
			for index := range rows {
				rows[index] = fmt.Sprintf(`{"advertiser_id":%d}`, 1001+index)
			}
			return 200, fmt.Sprintf(`{"code":0,"data":{"list":[%s],"page_info":{"page":1,"total_number":51,"total_page":1}}}`, strings.Join(rows, ","))
		case "/open_api/2/advertiser/info/":
			var ids []int64
			if err := json.Unmarshal([]byte(request.URL.Query().Get("advertiser_ids")), &ids); err != nil || len(ids) > 50 {
				t.Errorf("invalid verification batch: %v, %v", ids, err)
				return 400, `{}`
			}
			batchSizes = append(batchSizes, len(ids))
			rows := make([]string, len(ids))
			for index, id := range ids {
				rows[len(ids)-1-index] = fmt.Sprintf(`{"id":%d}`, id)
			}
			return 200, fmt.Sprintf(`{"code":0,"data":[%s]}`, strings.Join(rows, ","))
		default:
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (AdvertiserDiscoveryAdapter{Factory: factory}).Discover(testRequestContext(t, "marketing"), "marketing", "fixture-access")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.AdvertiserIDs) != 51 || snapshot.AdvertiserIDs[0] != "1001" || snapshot.AdvertiserIDs[50] != "1051" || !reflect.DeepEqual(batchSizes, []int{50, 1}) {
		t.Fatalf("overlapping role accounts were not fully verified: ids=%v batches=%v", snapshot.AdvertiserIDs, batchSizes)
	}
}

func TestAdvertiserDiscoveryUsesGeneratedRoleServicesAndRetriesCurrentPage(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, attempt int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[
				{"account_id":9001,"account_type":"ADVERTISER","advertiser_id":1001,"is_valid":true},
				{"account_id":9002,"account_type":"CUSTOMER_ADMIN","is_valid":true},
				{"account_id":9003,"account_type":"PLATFORM_ROLE_ENTERPRISE_BP_ADMIN","is_valid":true}
			]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			page := request.URL.Query().Get("page")
			if page == "2" && attempt == 1 {
				return 200, `{"code":40100,"message":"rate limited","request_id":"fixture-rate-limit"}`
			}
			advertiserID := 1002
			if page == "2" {
				advertiserID = 1003
			}
			return 200, fmt.Sprintf(`{"code":0,"data":{"list":[{"advertiser_id":%d}],"page_info":{"page":%s,"page_size":100,"total_number":2,"total_page":2}}}`, advertiserID, page)
		case "/open_api/2/ebp/advertiser/list/":
			return 200, `{"code":0,"data":{"account_list":[{"account_id":1004}],"page_info":{"page":1,"page_size":100,"total_number":1,"total_page":1}}}`
		case "/open_api/2/advertiser/info/":
			return 200, `{"code":0,"data":[{"id":1001},{"id":1002},{"id":1003},{"id":1004}]}`
		default:
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := AdvertiserDiscoveryAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
	}
	snapshot, err := adapter.Discover(testRequestContext(t, "marketing"), "marketing", "fixture-access")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.AdvertiserIDs, []string{"1001", "1002", "1003", "1004"}) {
		t.Fatalf("advertiser IDs = %v", snapshot.AdvertiserIDs)
	}
	paths := make([]string, 0, len(transport.calls))
	for _, call := range transport.calls {
		paths = append(paths, call.Host+call.Path+"#"+call.Query.Get("page"))
	}
	want := []string{
		BusinessHost + "/open_api/oauth2/advertiser/get/#",
		BusinessHost + "/open_api/2/customer_center/advertiser/list/#1",
		BusinessHost + "/open_api/2/customer_center/advertiser/list/#2",
		BusinessHost + "/open_api/2/customer_center/advertiser/list/#2",
		BusinessHost + "/open_api/2/ebp/advertiser/list/#1",
		BusinessHost + "/open_api/2/advertiser/info/#",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("request sequence:\n got %v\nwant %v", paths, want)
	}
	if transport.calls[1].Query.Get("account_source") != "AD" ||
		transport.calls[4].Query.Get("account_source") != "AD" {
		t.Fatalf("marketing account sources were not explicit: %#v", transport.calls)
	}
}

func TestStarMapDiscoveryFiltersStarAccountsAndRetriesReadFailures(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, attempt int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			if attempt == 1 {
				return 200, `{"code":40100,"message":"rate limited","request_id":"fixture-rate-limit"}`
			}
			return 200, `{"code":0,"data":{"list":[
				{"account_id":3001,"account_type":"PLATFORM_ROLE_STAR","account_name":"星图账户","is_valid":true},
				{"account_id":3002,"account_type":"ADVERTISER","is_valid":true}
			]}}`
		case "/open_api/2/star/info/":
			return 200, `{"code":0,"data":{"info_list":[{"start_id":3001,"start_name":"星图账户","status":"ENABLE"}]}}`
		default:
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := AdvertiserDiscoveryAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
	}
	snapshot, err := adapter.Discover(
		testRequestContext(t, "star_map"), "star_map", "fixture-access",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.AdvertiserIDs, []string{"3001"}) || len(snapshot.Accounts) != 1 {
		t.Fatalf("unexpected star map snapshot: %#v", snapshot)
	}
	if len(transport.calls) != 3 || transport.calls[2].Host != BusinessHost ||
		transport.calls[2].Path != "/open_api/2/star/info/" {
		t.Fatalf("unexpected star map request sequence: %#v", transport.calls)
	}
}

func TestAdvertiserDiscoveryUsesAgentAndShopProfiles(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, _ int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[
				{"account_id":9101,"account_type":"PLATFORM_ROLE_QIANCHUAN_AGENT","is_valid":true},
				{"account_id":9102,"account_type":"PLATFORM_ROLE_SHOP_ACCOUNT","is_valid":true}
			]}}`
		case "/open_api/2/agent/advertiser/select/":
			return 200, `{"code":0,"data":{"list":[2001],"page_info":{"page":1,"page_size":100,"total_number":1,"total_page":1}}}`
		case "/open_api/v1.0/qianchuan/shop/advertiser/list/":
			return 200, `{"code":0,"data":{"adv_id_list":[{"adv_id":2002}],"page_info":{"page":1,"page_size":100,"total_number":1,"total_page":1}}}`
		case "/open_api/2/advertiser/info/":
			return 200, `{"code":0,"data":[{"id":2001},{"id":2002}]}`
		default:
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := AdvertiserDiscoveryAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0}, Sleep: func(context.Context, time.Duration) error { return nil },
		},
	}
	snapshot, err := adapter.Discover(testRequestContext(t, "qianchuan"), "qianchuan", "fixture-access")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.AdvertiserIDs, []string{"2001", "2002"}) {
		t.Fatalf("advertiser IDs = %v", snapshot.AdvertiserIDs)
	}
	if len(transport.calls) != 4 {
		t.Fatalf("call count = %d", len(transport.calls))
	}
	if transport.calls[1].Host != OAuthHost || transport.calls[1].Path != "/open_api/2/agent/advertiser/select/" {
		t.Fatalf("agent request used wrong profile: %#v", transport.calls[1])
	}
	if transport.calls[2].Host != BusinessHost || transport.calls[2].Path != "/open_api/v1.0/qianchuan/shop/advertiser/list/" {
		t.Fatalf("shop request used wrong profile: %#v", transport.calls[2])
	}
	if !strings.Contains(transport.calls[2].Query.Get("permission"), "QC_AWEME") {
		t.Fatalf("shop permission was not explicit: %#v", transport.calls[2].Query)
	}
}

func TestAdvertiserDiscoveryAcceptsOfficialEmptyZeroPageMetadata(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, _ int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[{"account_id":9002,"account_type":"CUSTOMER_ADMIN","is_valid":true}]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			return 200, `{"code":0,"data":{"list":[],"page_info":{"total_page":0}}}`
		default:
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (AdvertiserDiscoveryAdapter{Factory: factory}).Discover(
		testRequestContext(t, "marketing"), "marketing", "fixture-access",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.AdvertiserIDs) != 0 || len(snapshot.Accounts) != 1 {
		t.Fatalf("unexpected empty snapshot: %#v", snapshot)
	}
}

func TestAdvertiserDiscoveryRejectsContradictoryZeroPageMetadata(t *testing.T) {
	totalPages := int64(0)
	for _, test := range []struct {
		name        string
		totalNumber *int64
		rows        []string
	}{
		{name: "non-empty-rows", rows: []string{"2001"}},
		{name: "non-zero-total", totalNumber: func() *int64 { value := int64(1); return &value }()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pageFromPointers(1, nil, &totalPages, test.totalNumber, test.rows); err == nil {
				t.Fatal("contradictory zero-page metadata was accepted")
			}
		})
	}
}

func TestAdvertiserDiscoveryFailsClosedOnDuplicateOrIncompleteVerification(t *testing.T) {
	for _, test := range []struct {
		name       string
		roleBody   string
		verifyBody string
	}{
		{
			name:       "duplicate-role-id",
			roleBody:   `{"code":0,"data":{"list":[{"advertiser_id":2001},{"advertiser_id":2001}],"page_info":{"page":1,"page_size":100,"total_number":2,"total_page":1}}}`,
			verifyBody: `{"code":0,"data":[{"id":2001}]}`,
		},
		{
			name:       "incomplete-verification",
			roleBody:   `{"code":0,"data":{"list":[{"advertiser_id":2001},{"advertiser_id":2002}],"page_info":{"page":1,"page_size":100,"total_number":2,"total_page":1}}}`,
			verifyBody: `{"code":0,"data":[{"id":2001}]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &discoveryTransport{perRoute: map[string]int{}}
			transport.respond = func(request *http.Request, _ int) (int, string) {
				switch request.URL.Path {
				case "/open_api/oauth2/advertiser/get/":
					return 200, `{"code":0,"data":{"list":[{"account_id":9002,"account_type":"CUSTOMER_ADMIN","is_valid":true}]}}`
				case "/open_api/2/customer_center/advertiser/list/":
					return 200, test.roleBody
				case "/open_api/2/advertiser/info/":
					return 200, test.verifyBody
				default:
					return 404, `{}`
				}
			}
			factory, err := NewClientFactory(FactoryOptions{
				TransportFactory: func(HostProfile) http.RoundTripper { return transport },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (AdvertiserDiscoveryAdapter{Factory: factory}).Discover(
				testRequestContext(t, "marketing"), "marketing", "fixture-access",
			); err == nil {
				t.Fatal("invalid advertiser snapshot was accepted")
			}
		})
	}
}
