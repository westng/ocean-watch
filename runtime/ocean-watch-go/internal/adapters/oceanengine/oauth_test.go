package oceanengine

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type oauthTransportFixture struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []map[string]any
	response string
}

func (transport *oauthTransportFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if len(payload) != 0 {
		if err := json.Unmarshal(payload, &body); err != nil {
			return nil, err
		}
	}
	transport.mu.Lock()
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	transport.bodies = append(transport.bodies, body)
	response := transport.response
	transport.mu.Unlock()
	return &http.Response{
		StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(response)), ContentLength: int64(len(response)),
		Request: request,
	}, nil
}

func TestOAuthAdapterUsesGeneratedServicesAndOfficialOAuthProfile(t *testing.T) {
	transport := &oauthTransportFixture{response: `{
		"code":0,"message":"OK","request_id":"fixture-request",
		"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":3600,"refresh_token_expires_in":7200}
	}`}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(profile HostProfile) http.RoundTripper {
			if profile != ProfileOAuth {
				t.Fatalf("OAuth adapter requested profile %s", profile)
			}
			return transport
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := OAuthAdapter{Factory: factory}
	token, err := adapter.ExchangeCode(
		testRequestContext(t, "marketing"), "marketing",
		domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-code",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "fixture-access" || token.RefreshToken != "fixture-refresh" ||
		token.AccessTokenTTL != time.Hour || token.RefreshTokenTTL != 2*time.Hour {
		t.Fatalf("unexpected token mapping: %#v", token)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(transport.requests))
	}
	request := transport.requests[0]
	if request.Method != http.MethodPost || request.URL.Host != OAuthHost ||
		request.URL.Path != "/open_api/oauth2/access_token/" {
		t.Fatalf("unexpected OAuth request: %s %s", request.Method, request.URL)
	}
	if transport.bodies[0]["auth_code"] != "fixture-code" || transport.bodies[0]["app_id"] != float64(123) {
		t.Fatalf("unexpected OAuth request body: %#v", transport.bodies[0])
	}

	transport.response = `{"code":0,"data":{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":60,"refresh_token_expires_in":120}}`
	token, err = adapter.RefreshToken(
		testRequestContext(t, "marketing"), "marketing",
		domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-refresh",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "rotated-access" || token.AccessTokenTTL != time.Minute {
		t.Fatalf("unexpected refresh mapping: %#v", token)
	}
	request = transport.requests[1]
	if request.URL.Path != "/open_api/oauth2/refresh_token/" ||
		transport.bodies[1]["refresh_token"] != "fixture-refresh" {
		t.Fatalf("unexpected refresh request: %s %#v", request.URL, transport.bodies[1])
	}
}

func TestOAuthAdapterRejectsBusinessErrorAndIncompleteSuccess(t *testing.T) {
	transport := &oauthTransportFixture{response: `{"code":40100,"message":"access_token=TEST_TOKEN_DO_NOT_LOG"}`}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := OAuthAdapter{Factory: factory}
	_, err = adapter.RefreshToken(
		testRequestContext(t, "qianchuan"), "qianchuan",
		domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-refresh",
	)
	if err == nil || strings.Contains(err.Error(), "TEST_TOKEN_DO_NOT_LOG") {
		t.Fatalf("business error was not safely rejected: %v", err)
	}
	transport.response = `{"code":0,"data":{}}`
	_, err = adapter.RefreshToken(
		testRequestContext(t, "qianchuan"), "qianchuan",
		domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-refresh",
	)
	if err == nil {
		t.Fatal("incomplete OAuth success was accepted")
	}
}

func TestQianchuanOAuthRefreshDoesNotRequireAdvertiserScope(t *testing.T) {
	transport := &oauthTransportFixture{response: `{
		"code":0,"message":"OK","request_id":"fixture-refresh",
		"data":{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":3600}
	}`}
	shared := &sharedControlFixture{acquired: make(chan struct{}, 1)}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory:       func(HostProfile) http.RoundTripper { return transport },
		SharedQianchuanControl: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, budget, metrics := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 1)
	_, err = (OAuthAdapter{Factory: factory}).RefreshToken(
		ctx, "qianchuan",
		domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-refresh",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 || budget.Snapshot().Used != 1 || metrics.Snapshot().Attempts != 1 {
		t.Fatalf("OAuth refresh was not dispatched exactly once: requests=%d budget=%#v metrics=%#v",
			len(transport.requests), budget.Snapshot(), metrics.Snapshot())
	}
	select {
	case <-shared.acquired:
		t.Fatal("OAuth refresh entered advertiser-scoped Qianchuan request control")
	default:
	}
}
