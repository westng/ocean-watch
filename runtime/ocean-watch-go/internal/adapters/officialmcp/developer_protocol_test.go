package officialmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

type developerForwarder struct {
	target *url.URL
	t      *testing.T
}

func (forward developerForwarder) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "open.oceanengine.com" || request.Header.Get("Access-Token") != "" || request.Header.Get("APP-Access-Token") != "" {
		forward.t.Error("developer connection acquired a business credential")
	}
	clone := request.Clone(request.Context())
	u := *request.URL
	u.Host, u.Scheme = forward.target.Host, forward.target.Scheme
	clone.URL, clone.Host = &u, u.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func developerProtocolFixture(t *testing.T, handler mcp.ToolHandler) (*DeveloperClient, docs.Metadata) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "docs-fixture", Version: "1"}, nil)
	for _, name := range developerNames {
		input, _ := contracts.DeveloperDocsInput(name)
		server.AddTool(&mcp.Tool{Name: name, InputSchema: input}, handler)
	}
	local := httptest.NewServer(mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(local.Close)
	target, _ := url.Parse(local.URL)
	return &DeveloperClient{base: developerForwarder{target: target, t: t}}, docs.Metadata{AppID: "9007199254740993", DeveloperID: "9007199254740995"}
}

func TestDeveloperSSEReadMappingsRedactionAndUTF8Bound(t *testing.T) {
	var calls atomic.Int64
	client, metadata := developerProtocolFixture(t, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		if strings.Contains(string(request.Params.Arguments), "900719925474099") {
			t.Error("protected metadata entered tool arguments")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "9007199254740993 9007199254740995 " + strings.Repeat("千川", 15000)}}}, nil
	})
	if err := client.Validate(context.Background(), metadata); err != nil || calls.Load() != 0 {
		t.Fatalf("schema-only validation dispatched a docs query: %v", err)
	}
	for _, request := range []docs.Request{{Kind: "search", Query: "千川预算"}, {Kind: "schema", APIPath: "/open_api/v1.0/qianchuan/account/balance/get/"}, {Kind: "example", APIPath: "/open_api/v1.0/qianchuan/account/balance/get/", Language: "java"}} {
		result, err := client.Query(context.Background(), metadata, request)
		if err != nil || !result.Truncated || len(result.Snippets) != 1 || !utf8.ValidString(result.Snippets[0]) || len(result.Snippets[0]) > 64<<10 {
			t.Fatalf("SSE result bound failed for %s: %v", request.Kind, err)
		}
		if strings.Contains(result.Snippets[0], metadata.AppID) || strings.Contains(result.Snippets[0], metadata.DeveloperID) {
			t.Fatal("protected metadata escaped redaction")
		}
	}
	if calls.Load() != 3 {
		t.Fatal("fixed mappings dispatched unexpected calls")
	}
}

func TestDeveloperSSECancelAndRemoteRejectionAreControlled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	client, metadata := developerProtocolFixture(t, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		close(started)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil, errors.New("synthetic-private-url")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.Query(ctx, metadata, docs.Request{Kind: "search", Query: "千川"})
		done <- err
	}()
	select {
	case <-started:
		cancel()
	case <-ctx.Done():
		t.Fatal("documentation query did not dispatch")
	}
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, docs.ErrConnection) || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("cancellation lost controlled failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled SSE query did not release its session")
	}
	client, metadata = developerProtocolFixture(t, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "synthetic-private-url"}}}, nil
	})
	_, err := client.Query(context.Background(), metadata, docs.Request{Kind: "search", Query: "千川"})
	var controlled *docs.Failure
	if !errors.As(err, &controlled) || controlled.Stage != "remote_query_rejected" || strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("remote rejection became a successful document")
	}
}
