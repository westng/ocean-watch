package oceanengine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

func TestAuthorizedUserUsesOfficialIdentityServiceWithoutAdvertiserScope(t *testing.T) {
	for _, channel := range []string{"marketing", "qianchuan"} {
		t.Run(channel, func(t *testing.T) {
			transport := &discoveryTransport{perRoute: map[string]int{}}
			transport.respond = func(request *http.Request, _ int) (int, string) {
				if request.Method != http.MethodGet || request.URL.Host != BusinessHost || request.URL.Path != "/open_api/2/user/info/" || request.Header.Get("Access-Token") != "fixture-access" {
					t.Errorf("unexpected official identity request: %s %s", request.Method, request.URL)
				}
				return 200, `{"code":0,"request_id":"user-fixture","data":{"id":7001,"app_id":123,"email":"private-fixture@example.test","advertiser_ids":[1001]}}`
			}
			shared := &sharedControlFixture{acquired: make(chan struct{}, 1)}
			factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }, SharedQianchuanControl: shared})
			if err != nil {
				t.Fatal(err)
			}
			ctx, budget, metrics := controlledTestRequestContext(t, channel, "oauth-bootstrap", 1)
			user, err := (OAuthAdapter{Factory: factory}).GetAuthorizedUser(ctx, channel, "fixture-access")
			if err != nil || user.ID != "7001" || user.AppID != "123" {
				t.Fatalf("official user identity was not read: %+v, %v", user, err)
			}
			if budget.Snapshot().Used != 1 || metrics.Snapshot().Attempts != 1 || len(shared.acquired) != 0 {
				t.Fatal("identity lookup did not use authorization-wide request control")
			}
		})
	}
}

func TestAuthorizedUserRejectsInvalidIdentityAndPreservesSafeFailureMetadata(t *testing.T) {
	for _, test := range []struct{ name, response, reason string }{
		{"missing UID", `{"code":0,"request_id":"user-fixture","data":{}}`, "INVALID_USER_INFO_RESPONSE"},
		{"zero UID", `{"code":0,"request_id":"user-fixture","data":{"id":0}}`, "INVALID_USER_INFO_RESPONSE"},
		{"official failure", `{"code":40001,"message":"access_token=fixture-private-token","request_id":"user-fixture"}`, "OFFICIAL_API_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &discoveryTransport{perRoute: map[string]int{}, respond: func(*http.Request, int) (int, string) { return 200, test.response }}
			factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
			if err != nil {
				t.Fatal(err)
			}
			user, err := (OAuthAdapter{Factory: factory}).GetAuthorizedUser(testRequestContext(t, "marketing"), "marketing", "fixture-access")
			var classified interface{ AuthorizationFailureReason() string }
			if err == nil || !errors.As(err, &classified) || classified.AuthorizationFailureReason() != test.reason || user.ID != "" || strings.Contains(err.Error(), "fixture-private-token") {
				t.Fatalf("invalid identity response was not safely rejected: %+v, %v", user, err)
			}
			var metadata interface{ AuthorizationFailureMetadata() (int64, string, int) }
			if !errors.As(err, &metadata) {
				t.Fatal("official failure omitted trace metadata")
			}
			_, requestID, status := metadata.AuthorizationFailureMetadata()
			if requestID != "user-fixture" || status != 200 {
				t.Fatal("official identity trace metadata was lost")
			}
		})
	}
}

func TestAuthorizedUserRequiresAuthorizationScope(t *testing.T) {
	transport := &discoveryTransport{perRoute: map[string]int{}}
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (OAuthAdapter{Factory: factory}).GetAuthorizedUser(context.Background(), "qianchuan", "fixture-access")
	if !errors.Is(err, requestcontrol.ErrAuthorizationScopeMissing) || len(transport.calls) != 0 {
		t.Fatal("unscoped identity lookup was dispatched")
	}
}
