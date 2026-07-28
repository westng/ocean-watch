package oceanengine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
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
		OAuthHost + "/open_api/oauth2/advertiser/get/#",
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
