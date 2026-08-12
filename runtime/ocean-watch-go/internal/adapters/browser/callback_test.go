package browser

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type callbackSession struct {
	calls int
	err   error
}

func (session *callbackSession) Exchange(context.Context, string, string) (domain.OAuthToken, error) {
	session.calls++
	return domain.OAuthToken{}, session.err
}

func TestOAuthCallbackStateHTTPBoundary(t *testing.T) {
	session := new(callbackSession)
	handler := Handler(session)
	request := httptest.NewRequest(http.MethodGet, CallbackPath+"?state=AD.fixture&auth_code=fixture-code", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || session.calls != 1 {
		t.Fatalf("status=%d calls=%d", response.Code, session.calls)
	}

	session.err = errors.New("rejected")
	request = httptest.NewRequest(http.MethodGet, CallbackPath+"?state=bad&auth_code=fixture-code", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || session.calls != 2 {
		t.Fatalf("status=%d calls=%d", response.Code, session.calls)
	}
	if body := response.Body.String(); body == "" || body == session.err.Error() {
		t.Fatalf("unsafe callback error body: %q", body)
	}
}

func TestOAuthCallbackStateLoopbackAndPortOccupation(t *testing.T) {
	server := CallbackServer{Address: "0.0.0.0:0", Session: new(callbackSession)}
	if _, err := server.Listen(); err == nil {
		t.Fatal("non-loopback callback listener was accepted")
	}

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server.Address = occupied.Addr().String()
	if _, err := server.Listen(); err == nil {
		t.Fatal("occupied callback port was accepted")
	}
}
