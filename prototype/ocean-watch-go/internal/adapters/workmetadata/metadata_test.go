package workmetadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

func TestDouyinMetadataResolverUsesOnlyPublicLinkAndReturnsHints(t *testing.T) {
	var received *http.Request
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":200,"data":{"video":{"video_info_id":"6000000000000001"},"author":{"uid":"4000000000000001","unique_id":"visible-1","nickname":"第三方达人"},"product":{"product_info_id":"5000000000000001","product_info_name":"fixture"}}}`)
	}))
	defer server.Close()
	resolver := DouyinMetadataResolver{Endpoint: server.URL + "/metadata?version=1", Client: server.Client()}
	result, err := resolver.Resolve(context.Background(), "https://v.douyin.com/public-link")
	if err != nil {
		t.Fatal(err)
	}
	if result.AwemeItemID != "6000000000000001" || result.OwnerHint == nil ||
		result.OwnerHint.AwemeID != "4000000000000001" || result.ProductHint == nil ||
		result.ProductHint.ProductID != "5000000000000001" || result.CreatorName != "第三方达人" {
		t.Fatalf("metadata hints changed: %#v", result)
	}
	if received == nil || received.Method != http.MethodGet ||
		received.URL.Query().Get("url") != "https://v.douyin.com/public-link" ||
		received.URL.Query().Get("version") != "1" || received.Header.Get("Authorization") != "" {
		t.Fatalf("metadata request leaked or changed fields: %#v", received)
	}
}

func TestDouyinMetadataResolverKeepsNumericHintWithoutVisibleID(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":200,"data":{"video":{"video_info_id":"6000000000000001"},"author":{"uid":"4000000000000001","nickname":"第三方达人"}}}`)
	}))
	defer server.Close()

	result, err := (DouyinMetadataResolver{Endpoint: server.URL, Client: server.Client()}).Resolve(
		context.Background(),
		"https://v.douyin.com/public-link",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.OwnerHint == nil || result.OwnerHint.AwemeID != "4000000000000001" ||
		result.OwnerHint.AwemeShowID != "" {
		t.Fatalf("numeric-only metadata hint was discarded: %#v", result)
	}
}

func TestDouyinMetadataResolverFallsBackWithoutExposingEndpoint(t *testing.T) {
	endpointSecret := "private-endpoint-marker"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, endpointSecret, http.StatusServiceUnavailable)
	}))
	defer server.Close()
	fallback := metadataFallbackStub{result: domain.ResolvedWorkLink{
		InputURL:     "https://www.douyin.com/video/6000000000000001",
		ResolvedURL:  "https://www.douyin.com/video/6000000000000001",
		CanonicalURL: "https://www.douyin.com/video/6000000000000001",
		AwemeItemID:  "6000000000000001",
	}}
	result, err := (DouyinMetadataResolver{
		Endpoint: server.URL + "/" + endpointSecret, Client: server.Client(), Fallback: fallback,
	}).Resolve(context.Background(), "https://www.douyin.com/video/6000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if result.AwemeItemID != "6000000000000001" || result.HintWarning == nil ||
		result.HintWarning.Code != "metadata_query_failed" ||
		strings.Contains(result.HintWarning.Message, endpointSecret) {
		t.Fatalf("metadata fallback or redaction changed: %#v", result)
	}
}

func TestDouyinMetadataResolverRejectsRedirectAndMismatchedWork(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetCalls++
		_, _ = io.WriteString(writer, `{"code":200}`)
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(writer, request, target.URL, http.StatusFound)
			return
		}
		_, _ = io.WriteString(writer, `{"code":200,"data":{"video":{"video_info_id":"6000000000000002"}}}`)
	}))
	defer server.Close()
	fallback := metadataFallbackStub{result: domain.ResolvedWorkLink{
		AwemeItemID: "6000000000000001", InputURL: "https://www.douyin.com/video/6000000000000001",
		ResolvedURL:  "https://www.douyin.com/video/6000000000000001",
		CanonicalURL: "https://www.douyin.com/video/6000000000000001",
	}}
	for _, path := range []string{"/redirect", "/mismatch"} {
		result, err := (DouyinMetadataResolver{
			Endpoint: server.URL + path, Client: server.Client(), Fallback: fallback,
		}).Resolve(context.Background(), "https://www.douyin.com/video/6000000000000001")
		if err != nil || result.HintWarning == nil {
			t.Fatalf("%s did not safely fall back: result=%#v err=%v", path, result, err)
		}
	}
	if targetCalls != 0 {
		t.Fatalf("metadata resolver followed %d redirects", targetCalls)
	}
}

type metadataFallbackStub struct {
	result domain.ResolvedWorkLink
	err    error
}

func (stub metadataFallbackStub) Resolve(context.Context, string) (domain.ResolvedWorkLink, error) {
	return stub.result, stub.err
}
