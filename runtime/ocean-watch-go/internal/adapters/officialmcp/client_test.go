package officialmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

const probeBudgetQuery = "qianchuan_account_budget_get_v1"

type clientTestForwarder struct {
	target              *url.URL
	init, listed, calls atomic.Int64
	t                   *testing.T
}

func (f *clientTestForwarder) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != probeEndpoint || r.Header.Get("Access-Token") == "" {
		f.t.Error("endpoint or credential injection invalid")
	}
	if r.Method == http.MethodPost {
		body, _ := r.GetBody()
		var wire struct {
			Method string `json:"method"`
		}
		if body != nil {
			_ = json.NewDecoder(body).Decode(&wire)
			_ = body.Close()
		}
		switch wire.Method {
		case "initialize":
			f.init.Add(1)
		case "tools/list":
			f.listed.Add(1)
		case "tools/call":
			f.calls.Add(1)
		}
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = f.target.Scheme, f.target.Host
	clone.URL = &u
	clone.Host = u.Host
	return http.DefaultTransport.RoundTrip(clone)
}
func formalFixture(t *testing.T, jsonResponse bool, result *mcp.CallToolResult) (*Client, *fixtureReadControl, *clientTestForwarder) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "formal-fixture", Version: "1"}, nil)
	for _, query := range []string{probeBalanceQuery, probeBudgetQuery, probeQuotaQuery} {
		server.AddTool(&mcp.Tool{Name: query, InputSchema: json.RawMessage(fixtureInputSchema)}, func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if result != nil {
				return result, nil
			}
			return &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"code":0,"data":{"advertiser_id":9007199254740993,"amount":1234567890.123456789}}`)}, nil
		})
	}
	local := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: jsonResponse}))
	t.Cleanup(local.Close)
	target, _ := url.Parse(local.URL)
	forward := &clientTestForwarder{target: target, t: t}
	control := &fixtureReadControl{}
	client := newClient(control, forward)
	t.Cleanup(func() { _ = client.Close() })
	return client, control, forward
}
func formalRequest() port.Request {
	return port.Request{QueryID: probeBalanceQuery, Profile: []string{probeBalanceQuery, probeBudgetQuery, probeQuotaQuery}, Lease: port.Lease{AuthorizationID: "fixture-grant", Revision: 1, Token: "fixture-token"}, ControlKey: fixtureAdvertiserID, Arguments: map[string]json.RawMessage{"advertiser_id": json.RawMessage(fixtureAdvertiserID)}, ApprovedInput: json.RawMessage(fixtureInputSchema)}
}

func TestFormalClientProtocolPrecisionReuseAndTokenRetirement(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "JSON", false: "SSE"}[jsonResponse], func(t *testing.T) {
			client, control, wire := formalFixture(t, jsonResponse, nil)
			ctx := probeTestContext(t)
			request := formalRequest()
			for i := 0; i < 2; i++ {
				response, err := client.Read(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				data := response.Data.(map[string]any)
				if data["amount"].(json.Number).String() != "1234567890.123456789" || data["advertiser_id"].(json.Number).String() != fixtureAdvertiserID {
					t.Fatal("precision lost")
				}
				encoded, _ := json.Marshal(response)
				if strings.Contains(string(encoded), "amount") {
					t.Fatal("raw data became serializable")
				}
			}
			if wire.init.Load() != 1 || wire.calls.Load() != 2 || control.serial.Load() != 0 || control.inFlight.Load() != 0 {
				t.Fatal("session or semantic read control failed")
			}
			request.Lease.Revision = 2
			request.Lease.Token = "new-fixture-token"
			if _, err := client.Read(ctx, request); err != nil {
				t.Fatal(err)
			}
			if wire.init.Load() != 2 {
				t.Fatal("new token reused old session")
			}
			request.Lease.Revision = 1
			if _, err := client.Read(ctx, request); err == nil {
				t.Fatal("stale token revision accepted")
			}
		})
	}
}
func TestFormalClientSchemaTTLAndSessionBound(t *testing.T) {
	client, _, wire := formalFixture(t, true, nil)
	now := time.Now()
	client.now = func() time.Time { return now }
	request := formalRequest()
	ctx := probeTestContext(t)
	if _, err := client.Read(ctx, request); err != nil {
		t.Fatal(err)
	}
	now = now.Add(clientSchemaTTL + time.Second)
	if _, err := client.Read(ctx, request); err != nil {
		t.Fatal(err)
	}
	if wire.init.Load() != 2 {
		t.Fatal("TTL reused cached tools/list")
	}
	client.mu.Lock()
	for _, entry := range client.entries {
		entry.active = 1
	}
	client.mu.Unlock()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(ctx, request); err == nil {
		t.Fatal("closed client accepted a call")
	}
}
func TestFormalClientBlocksSchemaAndBudgetBeforeDispatch(t *testing.T) {
	client, control, wire := formalFixture(t, true, nil)
	ctx := probeTestContext(t)
	request := formalRequest()
	request.ApprovedInput = json.RawMessage(strings.Replace(fixtureInputSchema, `"integer"`, `"string"`, 1))
	if _, err := client.Read(ctx, request); err == nil {
		t.Fatal("type change accepted")
	}
	if wire.calls.Load() != 0 || control.reads.Load() != 0 {
		t.Fatal("schema rejection dispatched")
	}
	request = formalRequest()
	if _, err := client.Read(context.Background(), request); err == nil || wire.calls.Load() != 0 {
		t.Fatal("unbudgeted call dispatched")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := client.Read(cancelled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call: %v", err)
	}
	if control.inFlight.Load() != 0 {
		t.Fatal("cancellation leaked slot")
	}
}
func TestFormalClientRateLimitFeedbackAndSafeError(t *testing.T) {
	client, control, _ := formalFixture(t, true, &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"code":40100,"message":"fixture-secret-never-output","data":null}`)})
	_, err := client.Read(probeTestContext(t), formalRequest())
	if err == nil || strings.Contains(err.Error(), "fixture-secret") || control.rateFeedback.Load() != 1 || control.inFlight.Load() != 0 {
		t.Fatalf("unsafe rate-limit handling %v", err)
	}
}
func TestSchemaCompatibilityDoesNotChangeApprovedContract(t *testing.T) {
	base := json.RawMessage(`{"type":"object","properties":{"advertiser_id":{"type":"integer","description":"账户 ID"}},"required":["advertiser_id"]}`)
	compatible := json.RawMessage(`{"type":"object","title":"new metadata","properties":{"advertiser_id":{"type":"integer","description":"账户  ID"},"new_field":{"type":"string"}},"required":["advertiser_id"]}`)
	if _, err := CheckSchemaCompatibility(base, compatible); err != nil {
		t.Fatal("optional metadata addition blocked")
	}
	for _, edited := range []string{strings.Replace(string(base), "账户 ID", "金额单位元", 1), strings.Replace(string(base), "integer", "string", 1), strings.Replace(string(compatible), `["advertiser_id"]`, `["advertiser_id","new_field"]`, 1)} {
		if _, err := CheckSchemaCompatibility(base, json.RawMessage(edited)); err == nil {
			t.Fatal("semantic/type/required change accepted")
		}
	}
}
