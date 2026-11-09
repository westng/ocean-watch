package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestMarketingDiscoveryMatchesStudioXMixedRolesAndResponseForms(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	transport.respond = func(request *http.Request, _ int) (int, string) {
		if request.URL.Path != "/open_api/oauth2/advertiser/get/" && request.Header.Get("Access-Token") != "fixture-access" {
			t.Error("account request lost its authorization header")
		}
		switch request.URL.Path {
		case "/open_api/oauth2/advertiser/get/":
			return 200, `{"code":0,"data":{"list":[
				{"account_id":"9001","advertiser_id":"9007199254740993","account_role":"ADVERTISER","is_valid":1},
				{"account_id":"9002","account_role":"CUSTOMER_OPERATOR","account_type":"ADVERTISER"},
				{"account_id":"9003","account_role":"PLATFORM_ROLE_ENTERPRISE_BP_ADMIN"},
				{"account_id":"9004","account_role":"PLATFORM_ROLE_SHOP_ACCOUNT"},
				{"account_id":"9005","account_role":"PLATFORM_ROLE_QIANCHUAN_AGENT"},
				{"account_id":"9006","account_type":"PLATFORM_ROLE_NEW"},
				{"account_string_id":"fixture-non-decimal-sec-uid","account_role":"PLATFORM_ROLE_AWEME"},
				{"account_id":"9007","account_role":"ADVERTISER","advertiser_id":"9999","is_valid":"false"},
				{"account_id":"9008","account_role":"PLATFORM_ROLE_STAR"}
			]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			if request.URL.Query().Get("account_source") != "AD" || request.URL.Query().Get("cc_account_id") != "9002" {
				t.Error("marketing customer-center parameters changed")
			}
			if request.URL.Query().Get("page") == "1" {
				return 200, `{"code":0,"data":{"list":[{"advertiser_id":"1002"}],"page_info":{"total_page":2,"page":null,"total_number":null}}}`
			}
			return 200, `{"code":0,"data":{"list":[{"account_id":"1003"}],"page_info":{"page":"2","total_page":"2","total_number":"2"}}}`
		case "/open_api/2/ebp/advertiser/list/":
			if request.URL.Query().Get("account_source") != "AD" || request.URL.Query().Get("enterprise_organization_id") != "9003" {
				t.Error("marketing enterprise parameters changed")
			}
			return 200, `{"code":0,"data":{"account_list":[{"advertiser_id":"1004"},{"account_id":"1002"}],"page_info":{"total_page":1}}}`
		case "/open_api/2/advertiser/info/":
			if request.URL.Query().Get("advertiser_ids") != "[9007199254740993,1002,1003,1004]" {
				t.Error("advertiser IDs lost precision or role deduplication")
			}
			return 200, `{"code":0,"data":{"list":[{"advertiser_id":"9007199254740993"},{"id":"1004"},{"id":1003},{"id":1002}]}}`
		default:
			t.Error("marketing called another channel's discovery endpoint")
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
	if !reflect.DeepEqual(snapshot.AdvertiserIDs, []string{"9007199254740993", "1002", "1003", "1004"}) || len(snapshot.Accounts) != 5 || len(snapshot.DiscoveryIssues) != 5 {
		t.Fatalf("unexpected marketing snapshot: %#v", snapshot)
	}
	if len(transport.calls) != 5 || len(snapshot.Accounts[4].AdvertiserIDs) != 0 {
		t.Fatal("inactive roots or non-advertiser roles affected advertising scope")
	}
	for _, account := range snapshot.Accounts {
		if account.AccountID == "9004" || account.AccountID == "9005" || account.AccountID == "9008" {
			t.Fatal("a different channel's root entered the marketing account mappings")
		}
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "fixture-non-decimal-sec-uid") || strings.Contains(string(encoded), "PLATFORM_ROLE_NEW") {
		t.Fatal("unmapped raw account identifiers or unknown role text escaped the discovery boundary")
	}
}

func TestAccountDataKeepsEndpointAndAuthorizationBoundaries(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}, respond: func(*http.Request, int) (int, string) {
		t.Error("an invalid account request was dispatched")
		return 200, `{"code":0,"data":{"list":[]}}`
	}}
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Client("marketing", ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.accountData(testRequestContext(t, "marketing"), "/arbitrary/path/", "fixture-access", nil); err == nil {
		t.Fatal("arbitrary account endpoint was accepted")
	}
	if _, err := client.accountData(context.Background(), authorizedAccountsEndpoint, "fixture-access", nil); err == nil {
		t.Fatal("account discovery bypassed authorization scope or request budget")
	}
	if len(transport.calls) != 0 {
		t.Fatal("account discovery bypassed its request guards")
	}
}

func TestMarketingDiscoverySparsePaginationStillRejectsIncompleteData(t *testing.T) {
	for _, test := range []struct {
		name, first, second string
	}{
		{"missing total pages", `{"list":[{"advertiser_id":1001}],"page_info":{}}`, ""},
		{"changed total pages", `{"list":[{"advertiser_id":1001}],"page_info":{"total_page":2}}`, `{"list":[{"advertiser_id":1002}],"page_info":{"total_page":3}}`},
		{"duplicate across sparse pages", `{"list":[{"advertiser_id":1001}],"page_info":{"total_page":2}}`, `{"list":[{"advertiser_id":1001}],"page_info":{"total_page":2}}`},
		{"empty sparse page", `{"list":[],"page_info":{"total_page":2}}`, ""},
		{"wrong requested page", `{"list":[{"advertiser_id":1001}],"page_info":{"page":2,"total_page":1}}`, ""},
		{"invalid advertiser identity", `{"list":[{"advertiser_id":"not-an-id"}],"page_info":{"total_page":1}}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &discoveryTransport{perRoute: map[string]int{}}
			transport.respond = func(request *http.Request, _ int) (int, string) {
				if request.URL.Path == "/open_api/oauth2/advertiser/get/" {
					return 200, `{"code":0,"data":{"list":[{"account_id":9002,"account_role":"CUSTOMER_ADMIN"}]}}`
				}
				body := test.first
				if request.URL.Query().Get("page") == "2" {
					body = test.second
				}
				return 200, `{"code":0,"data":` + body + `}`
			}
			factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = (AdvertiserDiscoveryAdapter{Factory: factory}).Discover(testRequestContext(t, "marketing"), "marketing", "fixture-access")
			if err == nil {
				t.Fatal("incomplete account data became a successful snapshot")
			}
			var contextual interface{ AuthorizationFailureContext() (string, string) }
			if !errors.As(err, &contextual) {
				t.Fatal("account failure lost safe diagnostic context")
			}
			endpoint, reason := contextual.AuthorizationFailureContext()
			if endpoint != customerAccountsEndpoint || reason == "" {
				t.Fatal("account failure did not identify its endpoint and validation")
			}
		})
	}
}

func TestMarketingDiscoveryPreservesSafeOfficialErrors(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}, respond: func(*http.Request, int) (int, string) {
		return 200, `{"code":40002,"message":"secret=fixture-private-secret","request_id":"request-fixture"}`
	}}
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (AdvertiserDiscoveryAdapter{Factory: factory}).Discover(testRequestContext(t, "marketing"), "marketing", "fixture-access")
	var official *EnvelopeError
	if !errors.As(err, &official) || official.Code != 40002 || official.RequestID != "request-fixture" || strings.Contains(err.Error(), "fixture-private-secret") {
		t.Fatal("official failure metadata was lost or its raw message was exposed")
	}
}
