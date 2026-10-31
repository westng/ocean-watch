package oceanengine

import (
	"errors"
	"net/http"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func TestOAuthExchangeRetainsOnlySafeProviderFailureMetadata(t *testing.T) {
	for _, test := range []struct {
		name, response, reason string
		code                   int64
	}{
		{"rejected", `{"code":40001,"message":"secret=fixture-private-secret","request_id":"request-fixture"}`, "OFFICIAL_API_ERROR", 40001},
		{"missing refresh token", `{"code":0,"request_id":"request-fixture","data":{"access_token":"fixture-private-access"}}`, "INVALID_TOKEN_RESPONSE", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &oauthTransportFixture{response: test.response}
			factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = (OAuthAdapter{Factory: factory}).ExchangeCode(testRequestContext(t, "qianchuan"), "qianchuan", domain.OAuthApp{AppID: "123", Secret: "fixture-secret"}, "fixture-code")
			if err == nil {
				t.Fatal("invalid token response was accepted")
			}
			var reason interface {
				error
				AuthorizationFailureReason() string
			}
			var metadata interface {
				error
				AuthorizationFailureMetadata() (int64, string, int)
			}
			if !errors.As(err, &reason) || reason.AuthorizationFailureReason() != test.reason || !errors.As(err, &metadata) {
				t.Fatal("OAuth adapter lost safe failure classification")
			}
			code, requestID, status := metadata.AuthorizationFailureMetadata()
			if code != test.code || requestID != "request-fixture" || status != 200 {
				t.Fatalf("incorrect official metadata: code=%d request=%q status=%d", code, requestID, status)
			}
		})
	}
}
