package oceanengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

func TestQianchuanHTTPReadsOverlapWhileWritesAndThirdReadWait(t *testing.T) {
	entered := make(chan string, 4)
	finish := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		entered <- request.Header.Get("X-Fixture-Request")
		_, _ = io.WriteString(w, `{"code":0}`+strings.Repeat(" ", qianchuanEnvelopePeek))
		w.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-finish:
		}
	}))
	defer server.Close()
	defer close(finish)
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{QianchuanAuthorizationConcurrency: 10})
	if err != nil {
		t.Fatal(err)
	}
	transport := &governedTransport{base: server.Client().Transport, channel: "qianchuan", governor: governor,
		sharedQianchuanControl: filesystem.QianchuanRequestController{Root: t.TempDir(), MinimumInterval: time.Millisecond}}
	ctx, budget, _ := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 10)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ctx, err = requestcontrol.WithAdvertiser(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	request := func(ctx context.Context, method, marker string) *http.Request {
		t.Helper()
		value, err := http.NewRequestWithContext(ctx, method, server.URL+"/open_api/v1.0/qianchuan/uni_promotion/list/", nil)
		if err != nil {
			t.Fatal(err)
		}
		value.Header.Set("X-Fixture-Request", marker)
		return value
	}
	first, err := transport.RoundTrip(request(ctx, http.MethodGet, "first"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	second, err := transport.RoundTrip(request(ctx, http.MethodGet, "second"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if <-entered != "first" || <-entered != "second" {
		t.Fatal("two HTTP reads did not overlap before either response closed")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		blocked, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
		_, err := transport.RoundTrip(request(blocked, method, "blocked"))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s crossed open read leases: %v", method, err)
		}
	}
	if budget.Snapshot().Used != 2 {
		t.Fatal("capacity/lock waits were counted as HTTP dispatches")
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := transport.RoundTrip(request(ctx, http.MethodGet, "third"))
	if err != nil {
		t.Fatalf("cancelled wait leaked shared capacity: %v", err)
	}
	defer third.Body.Close()
	if <-entered != "third" || budget.Snapshot().Used != 3 {
		t.Fatal("a blocked write reached HTTP or a read lease was leaked")
	}
}

type delayedQianchuanAdmission struct{ delay time.Duration }

func (controller delayedQianchuanAdmission) Acquire(ctx context.Context, _ string) (func(error, *http.Response) error, error) {
	timer := time.NewTimer(controller.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return func(error, *http.Response) error { return nil }, nil
	}
}

func TestQianchuanDispatchTimeoutStartsAfterAdmission(t *testing.T) {
	governor, err := requestcontrol.NewGovernor(requestcontrol.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	client := newClient("qianchuan", ClientProfile{Name: ProfileBusiness, Host: BusinessHost, Scheme: OfficialScheme},
		TimeoutStandard, 60*time.Millisecond, DefaultMaxResponse, governor,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			called = true
			if request.Context().Err() != nil {
				t.Fatal("request reached dispatch with an expired network budget")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), ContentLength: -1}, nil
		}), delayedQianchuanAdmission{delay: 180 * time.Millisecond})
	ctx, budget, _ := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 1)
	ctx, err = requestcontrol.WithAdvertiser(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_promotion/list/", nil)
	response, err := client.sdk.ApiClient.GetConfig().HTTPClient.Do(request)
	if err != nil {
		t.Fatalf("local admission consumed the network timeout: %v", err)
	}
	defer response.Body.Close()
	if !called || budget.Snapshot().Used != 1 {
		t.Fatal("admitted request did not dispatch exactly once")
	}
}

func TestQianchuanQueuedCancellationRemainsBeforeDispatch(t *testing.T) {
	governor, _ := requestcontrol.NewGovernor(requestcontrol.Limits{})
	called := false
	client := newClient("qianchuan", ClientProfile{Name: ProfileBusiness, Host: BusinessHost, Scheme: OfficialScheme},
		TimeoutStandard, 60*time.Millisecond, DefaultMaxResponse, governor,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected dispatch")
		}), delayedQianchuanAdmission{delay: 180 * time.Millisecond})
	ctx, budget, _ := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 1)
	ctx, _ = requestcontrol.WithAdvertiser(ctx, "123")
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_promotion/list/", nil)
	_, err := client.sdk.ApiClient.GetConfig().HTTPClient.Do(request)
	var failure *SDKRequestError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &failure) || failure.Dispatched() || called || budget.Snapshot().Used != 0 {
		t.Fatalf("queued cancellation crossed the dispatch boundary: %v used=%d called=%t", err, budget.Snapshot().Used, called)
	}
}

type contextBlockedResponseBody struct{ ctx context.Context }

func (body contextBlockedResponseBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (body contextBlockedResponseBody) Close() error { return nil }

func TestQianchuanResponseBodyTimeoutKeepsDispatchDiagnostics(t *testing.T) {
	governor, _ := requestcontrol.NewGovernor(requestcontrol.Limits{})
	client := newClient("qianchuan", ClientProfile{Name: ProfileBusiness, Host: BusinessHost, Scheme: OfficialScheme},
		TimeoutStandard, 60*time.Millisecond, DefaultMaxResponse, governor,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: contextBlockedResponseBody{ctx: request.Context()}, ContentLength: -1}, nil
		}), delayedQianchuanAdmission{})
	ctx, budget, _ := controlledTestRequestContext(t, "qianchuan", testAuthorizationID, 1)
	ctx, _ = requestcontrol.WithAdvertiser(ctx, "123")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_promotion/list/", nil)
	response, err := client.sdk.ApiClient.GetConfig().HTTPClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	started := time.Now()
	_, err = io.ReadAll(response.Body)
	var failure interface {
		Dispatched() bool
		EndpointFamily() string
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &failure) || !failure.Dispatched() ||
		failure.EndpointFamily() != "/v1.0/qianchuan/uni_promotion/list" || ctx.Err() != nil ||
		budget.Snapshot().Used != 1 || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("response-body timeout lost its bounded network budget or dispatch evidence: %v", err)
	}
}
