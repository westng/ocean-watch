package workmetadata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNormalizeDouyinURLRejectsSSRFShapes(t *testing.T) {
	for _, value := range []string{
		"http://v.douyin.com/abc", "https://douyin.com.evil.test/video/1",
		"https://evil.test/?next=https://douyin.com/video/1", "https://user@douyin.com/video/1",
		"https://douyin.com:8443/video/1", "https://127.0.0.1/video/1", "https://douyin.com/video/1#fragment",
	} {
		if _, err := NormalizeDouyinURL(value); err == nil {
			t.Fatalf("unsafe URL was accepted: %s", value)
		}
	}
	for _, value := range []string{
		"https://douyin.com/video/1", "分享 https://v.douyin.com/abc/ 复制打开",
		"https://www.iesdouyin.com/video/2", "https://www.douyin.com:443/video/3",
	} {
		if _, err := NormalizeDouyinURL(value); err != nil {
			t.Fatalf("trusted URL was rejected: %s: %v", value, err)
		}
	}
}

func TestNormalizeDouyinURLUsesOnlyShareCodePathSegment(t *testing.T) {
	tests := map[string]string{
		"time marker":      "https://v.douyin.com/abc-123_X/:3pm",
		"date and command": "分享 https://v.douyin.com/abc-123_X/04/07 oDu:/ 复制打开",
		"query suffix":     "https://v.douyin.com/abc-123_X/?from=copy",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeDouyinURL(input)
			if err != nil {
				t.Fatalf("share URL was rejected: %v", err)
			}
			if want := "https://v.douyin.com/abc-123_X/"; got != want {
				t.Fatalf("NormalizeDouyinURL() = %q, want %q", got, want)
			}
		})
	}
}

func TestNormalizeDouyinURLDoesNotTruncateCanonicalWorkPath(t *testing.T) {
	const input = "https://www.douyin.com/video/123456/?previous_page=copy"
	got, err := NormalizeDouyinURL(input)
	if err != nil {
		t.Fatalf("canonical URL was rejected: %v", err)
	}
	if got != input {
		t.Fatalf("NormalizeDouyinURL() = %q, want %q", got, input)
	}
}

func TestDouyinRedirectResolverRejectsCrossHostRedirectBeforeDispatch(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://evil.test/video/123"}},
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	_, err := (DouyinRedirectResolver{Client: client}).Resolve(context.Background(), "https://v.douyin.com/abc")
	var linkErr *domain.WorkLinkError
	if !errors.As(err, &linkErr) || linkErr.Code != "untrusted_host" || calls != 1 {
		t.Fatalf("cross-host redirect was not safely rejected: calls=%d err=%v", calls, err)
	}
}

func TestDouyinRedirectResolverFollowsAtMostFiveTrustedRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if strings.HasPrefix(request.URL.Path, "/video/") {
			return &http.Response{
				StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("x")), Request: request,
			}, nil
		}
		location := "https://v.douyin.com/hop" + string(rune('0'+calls))
		if calls == 5 {
			location = "https://www.douyin.com/video/123456"
		}
		return &http.Response{
			StatusCode: http.StatusFound, Header: http.Header{"Location": []string{location}},
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	result, err := (DouyinRedirectResolver{Client: client}).Resolve(context.Background(), "https://v.douyin.com/start")
	if err != nil || result.AwemeItemID != "123456" || calls != 6 {
		t.Fatalf("trusted redirect chain failed: calls=%d result=%#v err=%v", calls, result, err)
	}

	calls = 0
	client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://v.douyin.com/hop" + string(rune('a'+calls))}},
			Body:       io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	_, err = (DouyinRedirectResolver{Client: client}).Resolve(context.Background(), "https://v.douyin.com/start")
	var linkErr *domain.WorkLinkError
	if !errors.As(err, &linkErr) || linkErr.Code != "too_many_redirects" || calls != 6 {
		t.Fatalf("redirect limit changed: calls=%d err=%v", calls, err)
	}
}
