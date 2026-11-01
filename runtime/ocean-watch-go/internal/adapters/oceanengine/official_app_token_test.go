package oceanengine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

type appCredentialFixture struct{}

func (appCredentialFixture) Read(context.Context, string) (map[string]any, error) {
	return map[string]any{"app_id": "100", "secret": "synthetic-secret"}, nil
}
func (appCredentialFixture) Write(context.Context, string, map[string]any) (string, error) {
	panic("unexpected write")
}

func TestAppTokenUsesFixedAuthenticationProfileAndCoalesces(t *testing.T) {
	var calls atomic.Int32
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(profile HostProfile) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			if profile != ProfileAppOAuth || r.Method != "POST" || r.URL.Host != BusinessHost || !strings.HasSuffix(r.URL.Path, "/oauth2/app_access_token/") {
				t.Error("App authentication used a business read/write route")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"access_token":"synthetic-app-token","expires_in":7200}}`)), Request: r}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &OfficialAppTokens{Credentials: appCredentialFixture{}, Factory: factory}
	ctx, _, _, _ := requestcontrol.PrepareCommandContext(context.Background(), 4)
	if _, err := provider.EnsureApp(ctx, "101"); err == nil || calls.Load() != 0 {
		t.Fatal("foreign App dispatched authentication")
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			lease, err := provider.EnsureApp(ctx, "100")
			if err != nil || lease.AppID != "100" || lease.Token != "synthetic-app-token" {
				t.Error("App token preparation failed")
			}
		})
	}
	wait.Wait()
	if calls.Load() != 1 {
		t.Fatal("App token authentication was not coalesced")
	}
}
