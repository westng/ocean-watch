package oceanengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestFactoryPinsEveryOfficialProfileAndDisablesSDKLogs(t *testing.T) {
	factory, err := NewClientFactory(FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[HostProfile]string{
		ProfileBusiness: BusinessHost, ProfileOAuth: OAuthHost,
		ProfileAgent: OAuthHost, ProfileQianchuanVideo: OAuthHost,
	}
	for profile, host := range tests {
		client, clientErr := factory.Client("marketing", profile, TimeoutStandard)
		if clientErr != nil {
			t.Fatal(clientErr)
		}
		configuration := client.sdk.ApiClient.GetConfig()
		if configuration.Host != host || configuration.Scheme != OfficialScheme {
			t.Fatalf("profile %s resolved to unsafe SDK base URL", profile)
		}
		if configuration.LogEnable || configuration.UseLogMw || configuration.Debug {
			t.Fatal("SDK logging must remain disabled")
		}
		if configuration.HTTPClient.Timeout != DefaultRequestTimeout {
			t.Fatal("SDK client timeout is not bounded")
		}
		if client.Profile().MaxResponseBytes != DefaultMaxResponse {
			t.Fatal("SDK response limit does not match the approved default")
		}
	}
}

func TestFactoryRejectsUnapprovedProfilesAndLimits(t *testing.T) {
	if _, err := NewClientFactory(FactoryOptions{MaxResponseBytes: DefaultMaxResponse + 1}); err == nil {
		t.Fatal("unapproved response limit was accepted")
	}
	factory, err := NewClientFactory(FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		channel string
		host    HostProfile
		timeout TimeoutProfile
	}{
		{channel: "other", host: ProfileBusiness, timeout: TimeoutStandard},
		{channel: "marketing", host: "https://example.test", timeout: TimeoutStandard},
		{channel: "marketing", host: ProfileBusiness, timeout: "unbounded"},
	} {
		if _, err := factory.Client(test.channel, test.host, test.timeout); err == nil {
			t.Fatal("unsafe SDK profile was accepted")
		}
	}
}

func TestFactoryCachesByImmutableKeyUnderConcurrency(t *testing.T) {
	var transportCreations atomic.Int32
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper {
			transportCreations.Add(1)
			return roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("not executed")
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const callers = 50
	clients := make(chan *Client, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			client, clientErr := factory.Client("qianchuan", ProfileBusiness, TimeoutStandard)
			if clientErr != nil {
				errorsSeen <- clientErr
				return
			}
			clients <- client
		}()
	}
	wait.Wait()
	close(clients)
	close(errorsSeen)
	for clientErr := range errorsSeen {
		t.Fatal(clientErr)
	}
	var first *Client
	for client := range clients {
		if first == nil {
			first = client
		}
		if client != first {
			t.Fatal("identical factory key produced multiple clients")
		}
	}
	if transportCreations.Load() != 1 {
		t.Fatalf("transport creations = %d, want 1", transportCreations.Load())
	}
	otherChannel, err := factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	if err != nil {
		t.Fatal(err)
	}
	if otherChannel == first {
		t.Fatal("client was reused across channel boundary")
	}
}

func TestSecurityTransportRejectsEscapedHost(t *testing.T) {
	called := false
	transport := &securityTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected call")
		}), host: BusinessHost, scheme: OfficialScheme, maxResponseBytes: 100,
	}
	for _, rawURL := range []string{
		"https://example.test/", "http://api.oceanengine.com/", "https://api.oceanengine.com:8443/",
		"https://user@api.oceanengine.com/",
	} {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
		if _, err := transport.RoundTrip(request); err == nil || called {
			t.Fatalf("escaped host was not blocked before transport: %s", rawURL)
		}
	}
}

func TestSecurityTransportLimitsUnknownLengthResponse(t *testing.T) {
	transport := &securityTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200, ContentLength: -1,
				Body: io.NopCloser(strings.NewReader("123456")),
			}, nil
		}), host: BusinessHost, scheme: OfficialScheme, maxResponseBytes: 5,
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.oceanengine.com/open_api/test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(response.Body)
	if err == nil {
		t.Fatal("oversized streamed body was accepted")
	}
}

func TestSecurityTransportRedactsNestedTransportFailureAndPreservesCancellation(t *testing.T) {
	secretError := errors.New("request failed: access_token=TEST_TOKEN_DO_NOT_LOG")
	transport := &securityTransport{
		base: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, secretError }),
		host: BusinessHost, scheme: OfficialScheme, maxResponseBytes: 100,
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.oceanengine.com/open_api/test", nil)
	_, err := transport.RoundTrip(request)
	if err == nil || strings.Contains(err.Error(), "TEST_TOKEN_DO_NOT_LOG") || !errors.Is(err, secretError) {
		t.Fatal("transport failure was not safely normalized")
	}

	canceledTransport := &securityTransport{
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}), host: BusinessHost, scheme: OfficialScheme, maxResponseBytes: 100,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.oceanengine.com/open_api/test", nil)
	_, err = canceledTransport.RoundTrip(request)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("request cancellation was not preserved")
	}
}

func TestSDKRequestErrorPreservesDispatchBoundary(t *testing.T) {
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	baseCalls := 0
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls++
		return nil, errors.New("synthetic connection reset")
	})
	transport := &securityTransport{
		base: &governedTransport{base: base, channel: "marketing", governor: governor},
		host: BusinessHost, scheme: OfficialScheme, maxResponseBytes: 100,
	}

	blockedContext, _, _ := controlledTestRequestContext(t, "marketing", testAuthorizationID, 0)
	blockedRequest, _ := http.NewRequestWithContext(
		blockedContext, http.MethodPost,
		"https://api.oceanengine.com/open_api/v3.0/project/create/", nil,
	)
	_, blockedErr := transport.RoundTrip(blockedRequest)
	var blocked *SDKRequestError
	if !errors.As(blockedErr, &blocked) || blocked.Dispatched() || baseCalls != 0 ||
		!errors.Is(blockedErr, requestcontrol.ErrRequestBudgetExceeded) {
		t.Fatalf("pre-dispatch boundary changed: err=%v calls=%d", blockedErr, baseCalls)
	}

	dispatchedContext, _, _ := controlledTestRequestContext(t, "marketing", testAuthorizationID, 1)
	dispatchedRequest, _ := http.NewRequestWithContext(
		dispatchedContext, http.MethodPost,
		"https://api.oceanengine.com/open_api/v3.0/project/create/", nil,
	)
	_, dispatchedErr := transport.RoundTrip(dispatchedRequest)
	var dispatched *SDKRequestError
	if !errors.As(dispatchedErr, &dispatched) || !dispatched.Dispatched() || baseCalls != 1 {
		t.Fatalf("post-dispatch boundary changed: err=%v calls=%d", dispatchedErr, baseCalls)
	}
}

func TestClientForbidsRedirects(t *testing.T) {
	factory, err := NewClientFactory(FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := client.sdk.ApiClient.GetConfig().HTTPClient
	redirectRequest, _ := http.NewRequest(http.MethodGet, "https://ad.oceanengine.com/", nil)
	if err := httpClient.CheckRedirect(redirectRequest, nil); err == nil {
		t.Fatal("SDK redirects were accepted")
	}
}

func TestServiceAnchorCompilesOfficialGeneratedServices(t *testing.T) {
	factory, err := NewClientFactory(FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	if err != nil {
		t.Fatal(err)
	}
	if err := ServiceAnchor(context.Background(), client); err != nil {
		t.Fatal(err)
	}
}

type governedBlockingTransport struct {
	started chan string
	release chan struct{}
	calls   atomic.Int32
}

func (transport *governedBlockingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	select {
	case transport.started <- request.Header.Get("X-Fixture-Request"):
	case <-request.Context().Done():
		return nil, request.Context().Err()
	}
	select {
	case <-transport.release:
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
			Request:    request,
		}, nil
	case <-request.Context().Done():
		return nil, request.Context().Err()
	}
}

func governedRequest(t testing.TB, ctx context.Context, marker string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://api.oceanengine.com/open_api/v3.0/project/list/", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Fixture-Request", marker)
	return request
}

func TestGovernedTransportLimitsSameAuthorizationAndCancelsWait(t *testing.T) {
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{
		AuthorizationConcurrency: 1, EndpointConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := &governedBlockingTransport{
		started: make(chan string, 2), release: make(chan struct{}),
	}
	transport := &governedTransport{base: base, channel: "marketing", governor: governor}
	ctx, budget, metrics := controlledTestRequestContext(t, "marketing", "authorization-a", 4)
	firstResult := make(chan error, 1)
	go func() {
		_, callErr := transport.RoundTrip(governedRequest(t, ctx, "first"))
		firstResult <- callErr
	}()
	if marker := <-base.started; marker != "first" {
		t.Fatalf("unexpected first request marker: %s", marker)
	}

	waitContext, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, waitErr := transport.RoundTrip(governedRequest(t, waitContext, "second"))
	if !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("limiter wait did not preserve its deadline: %v", waitErr)
	}
	if base.calls.Load() != 1 || budget.Snapshot().Used != 1 || metrics.Snapshot().Attempts != 1 ||
		metrics.Snapshot().LimiterCancellations != 1 {
		t.Fatalf("canceled wait was counted as HTTP traffic: calls=%d budget=%#v metrics=%#v",
			base.calls.Load(), budget.Snapshot(), metrics.Snapshot())
	}
	close(base.release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
}

func TestGovernedTransportSeparatesChannelsAndAuthorizations(t *testing.T) {
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{
		AuthorizationConcurrency: 1, EndpointConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := &governedBlockingTransport{
		started: make(chan string, 3), release: make(chan struct{}),
	}
	commandContext, _, _, err := requestcontrol.PrepareCommandContext(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		channel, authorization, marker string
	}
	fixtures := []fixture{
		{channel: "marketing", authorization: "authorization-a", marker: "marketing-a"},
		{channel: "marketing", authorization: "authorization-b", marker: "marketing-b"},
		{channel: "qianchuan", authorization: "authorization-a", marker: "qianchuan-a"},
	}
	results := make(chan error, len(fixtures))
	for _, item := range fixtures {
		ctx, scopeErr := requestcontrol.WithAuthorization(commandContext, item.channel, item.authorization)
		if scopeErr != nil {
			t.Fatal(scopeErr)
		}
		request := governedRequest(t, ctx, item.marker)
		transport := &governedTransport{base: base, channel: item.channel, governor: governor}
		go func() {
			_, callErr := transport.RoundTrip(request)
			results <- callErr
		}()
	}
	seen := map[string]bool{}
	for range fixtures {
		select {
		case marker := <-base.started:
			seen[marker] = true
		case <-time.After(time.Second):
			t.Fatal("an isolated request was blocked by another authorization scope")
		}
	}
	for _, item := range fixtures {
		if !seen[item.marker] {
			t.Fatalf("request scope was not isolated: missing=%s seen=%#v", item.marker, seen)
		}
	}
	close(base.release)
	for range fixtures {
		if callErr := <-results; callErr != nil {
			t.Fatal(callErr)
		}
	}
}

func TestGovernedTransportFailsClosedWithoutLeakingToken(t *testing.T) {
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport call")
	})
	transport := &governedTransport{base: base, channel: "marketing", governor: governor}
	secret := "TEST_ACCESS_TOKEN_MUST_NOT_APPEAR"
	ctx, budget, metrics := controlledTestRequestContext(t, "marketing", testAuthorizationID, 0)
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		"https://api.oceanengine.com/open_api/v3.0/project/list/?access_token="+secret,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Access-Token", secret)
	_, err = transport.RoundTrip(request)
	if !errors.Is(err, requestcontrol.ErrRequestBudgetExceeded) || calls.Load() != 0 {
		t.Fatalf("budget rejection reached transport: calls=%d err=%v", calls.Load(), err)
	}
	diagnostics := fmt.Sprintf("%v %#v %#v", err, budget.Snapshot(), metrics.Snapshot())
	if strings.Contains(diagnostics, secret) || metrics.Snapshot().Attempts != 0 {
		t.Fatalf("request controls exposed or counted rejected traffic: %s", diagnostics)
	}

	missingAuthorization, missingBudget, _, err := requestcontrol.PrepareCommandContext(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	request = governedRequest(t, missingAuthorization, "missing-authorization")
	if _, err := transport.RoundTrip(request); !errors.Is(err, requestcontrol.ErrAuthorizationScopeMissing) {
		t.Fatalf("missing authorization did not fail closed: %v", err)
	}
	if calls.Load() != 0 || missingBudget.Snapshot().Used != 0 {
		t.Fatalf("missing authorization reached transport: calls=%d budget=%#v",
			calls.Load(), missingBudget.Snapshot())
	}
}
