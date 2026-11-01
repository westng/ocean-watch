package officialmcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

const fixtureAdvertiserID = "9007199254740993"
const fixtureInputSchema = `{"type":"object","properties":{"advertiser_id":{"type":"integer","minimum":1}},"required":["advertiser_id"],"additionalProperties":false}`

func TestProbeStreamableProtocolPreservesRawNumbersAndReusesSession(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "JSON", false: "SSE"}[jsonResponse], func(t *testing.T) {
			probe, control, called := fixtureProbe(t, jsonResponse, nil)
			ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
			defer cancel()
			discovery, err := probe.Inspect(ctx)
			if err != nil || !discovery.Complete || len(discovery.Tools) != 3 {
				t.Fatalf("discovery=%#v err=%v", discovery, err)
			}
			var digest string
			for _, tool := range discovery.Tools {
				if tool.Name == probeBalanceQuery {
					digest = tool.InputSourceDigest
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				observation, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest)
				if err != nil || !observation.ProtocolSucceeded || !observation.BusinessSucceeded {
					t.Fatalf("read=%#v err=%v", observation, err)
				}
				data, ok := observation.Payload["data"].(map[string]any)
				if !ok {
					t.Fatal("missing data object")
				}
				for field, expected := range map[string]string{"advertiser_id": fixtureAdvertiserID, "amount": "1234567890.123456789"} {
					value, ok := data[field].(json.Number)
					if !ok || value.String() != expected {
						t.Fatalf("%s lost precision: %T", field, data[field])
					}
				}
				serialized, err := json.Marshal(observation)
				if err != nil || strings.Contains(string(serialized), fixtureAdvertiserID) || strings.Contains(string(serialized), "amount") {
					t.Fatal("raw probe response entered its public serialization")
				}
			}
			counts := probe.Counts()
			if counts.Initialize != 1 || counts.ToolList != 3 || counts.ToolCall != 2 || called.Load() != 2 {
				t.Fatalf("counts=%#v server_calls=%d", counts, called.Load())
			}
			if control.reads.Load() != 2 || control.serial.Load() != 0 || control.inFlight.Load() != 0 {
				t.Fatalf("semantic read slots: reads=%d serial=%d active=%d", control.reads.Load(), control.serial.Load(), control.inFlight.Load())
			}
		})
	}
}

func TestProbeRejectsUnreviewedScopeAndSchemaBeforeDispatch(t *testing.T) {
	probe, control, called := fixtureProbe(t, true, nil)
	ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
	defer cancel()
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	digest := discovery.Tools[0].InputSourceDigest
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	for name, input := range map[string]struct {
		query, id, digest string
		want              error
	}{
		"unreviewed":     {probeBalanceQuery, fixtureAdvertiserID, "", ErrProbeSchema},
		"changed schema": {probeBalanceQuery, fixtureAdvertiserID, "different", ErrProbeSchema},
		"scope alias":    {probeBalanceQuery, "0" + fixtureAdvertiserID, digest, ErrProbeScope},
		"missing scope":  {probeBalanceQuery, "", digest, ErrProbeScope},
		"candidate list": {probeListQuery, fixtureAdvertiserID, digest, ErrProbeToolUnavailable},
		"unknown tool":   {"unknown_remote_tool", fixtureAdvertiserID, digest, ErrProbeToolUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := probe.ReadAdvertiser(ctx, input.query, input.id, input.digest); !errors.Is(err, input.want) {
				t.Fatalf("err=%v want=%v", err, input.want)
			}
		})
	}
	if control.reads.Load() != 0 || called.Load() != 0 || probe.Counts().ToolCall != 0 {
		t.Fatal("rejected request acquired a read slot or dispatched")
	}
}

func TestProbeSeparatesProtocolAndBusinessFailure(t *testing.T) {
	for name, result := range map[string]*mcp.CallToolResult{
		"MCP isError": {IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "synthetic secret must not appear"}}},
		"API code":    {StructuredContent: json.RawMessage(`{"code":40001,"message":"synthetic secret must not appear","data":null}`)},
	} {
		t.Run(name, func(t *testing.T) {
			probe, control, _ := fixtureProbe(t, true, result)
			ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
			defer cancel()
			discovery, err := probe.Inspect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var digest string
			for _, tool := range discovery.Tools {
				if tool.Name == probeBalanceQuery {
					digest = tool.InputSourceDigest
				}
			}
			observation, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest)
			if !errors.Is(err, ErrProbeBusiness) || !observation.ProtocolSucceeded ||
				observation.BusinessSucceeded || strings.Contains(err.Error(), "synthetic secret") || control.inFlight.Load() != 0 {
				t.Fatalf("unsafe failure=%#v %v active=%d", observation, err, control.inFlight.Load())
			}
		})
	}
}

func TestProbeCancellationReleasesReadSlot(t *testing.T) {
	probe, control, _ := fixtureProbe(t, true, nil)
	ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
	defer cancel()
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	cancelled, cancelCall := context.WithCancel(ctx)
	cancelCall()
	_, err = probe.ReadAdvertiser(cancelled, probeBalanceQuery, fixtureAdvertiserID, digest)
	if !errors.Is(err, context.Canceled) || control.inFlight.Load() != 0 {
		t.Fatalf("cancellation=%v active=%d", err, control.inFlight.Load())
	}
}

func TestProbeCaptureSupportsTextJSONWithoutFloatConversion(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"{\"code\":0,\"data\":{\"id\":9007199254740993}}"}]}`)
	payload, failed, err := decodeProbePayload(raw)
	if err != nil || failed {
		t.Fatalf("text payload failed: %v", err)
	}
	data := payload["data"].(map[string]any)
	if data["id"].(json.Number).String() != fixtureAdvertiserID {
		t.Fatal("text ID lost precision")
	}
}

func TestProbeBusinessFailureKeepsOnlyAllowlistedParameterHints(t *testing.T) {
	probe, _, _ := fixtureProbe(t, true, &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: `{"code":40000,"message":"invalid filtering / page_size: synthetic_secret_must_not_appear","data":null}`},
	}})
	ctx := probeTestContext(t)
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	observation, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest)
	if !errors.Is(err, ErrProbeBusiness) || !observation.ProtocolSucceeded || observation.BusinessSucceeded ||
		observation.APICode != "40000" || observation.PayloadFormat != "json_text" ||
		strings.Join(observation.ErrorParameterHints, ",") != "filtering,page_size" {
		t.Fatalf("failure metadata changed: %#v %v", observation, err)
	}
	raw, _ := json.Marshal(observation)
	if strings.Contains(string(raw), "synthetic_secret") || strings.Contains(string(raw), "message") || observation.Payload != nil {
		t.Fatal("raw business error entered public metadata")
	}
}

func TestProbeRejectsReturnedSubjectMismatch(t *testing.T) {
	probe, control, _ := fixtureProbe(t, true, &mcp.CallToolResult{
		StructuredContent: json.RawMessage(`{"code":0,"data":{"advertiser_id":123456789}}`),
	})
	ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
	defer cancel()
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	observation, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest)
	if !errors.Is(err, ErrProbeScope) || observation.BusinessSucceeded || control.inFlight.Load() != 0 {
		t.Fatalf("wrong response subject accepted: %v", err)
	}
}

func fixtureProbe(t *testing.T, jsonResponse bool, result *mcp.CallToolResult, edits ...func(*mcp.Server)) (*Probe, *fixtureReadControl, *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	server := mcp.NewServer(&mcp.Implementation{Name: "isolated-official-mcp-fixture", Version: "1"}, &mcp.ServerOptions{PageSize: 1})
	called := &atomic.Int64{}
	for _, name := range []string{probeBalanceQuery, probeQuotaQuery, probeListQuery} {
		server.AddTool(&mcp.Tool{Name: name, InputSchema: json.RawMessage(fixtureInputSchema)}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var arguments map[string]json.RawMessage
			if json.Unmarshal(request.Params.Arguments, &arguments) != nil || string(arguments["advertiser_id"]) != fixtureAdvertiserID || len(arguments) != 1 {
				t.Error("scope or arguments changed on the wire")
			}
			called.Add(1)
			if result != nil {
				return result, nil
			}
			return &mcp.CallToolResult{
				StructuredContent: json.RawMessage(`{"code":0,"data":{"advertiser_id":9007199254740993,"amount":1234567890.123456789}}`),
			}, nil
		})
	}
	for _, edit := range edits {
		edit(server)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: jsonResponse})
	local := httptest.NewServer(handler)
	t.Cleanup(local.Close)
	target, err := url.Parse(local.URL)
	if err != nil {
		t.Fatal(err)
	}
	control := &fixtureReadControl{}
	base := fixtureForwarder{target: target, t: t}
	probe, err := newProbe(ctx, "synthetic-fixture-token", control, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if probe.Close() != nil {
			t.Error("fixture close failed")
		}
	})
	return probe, control, called
}

func probeTestContext(t *testing.T) context.Context {
	t.Helper()
	budget, err := requestcontrol.NewBudget(8)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := requestcontrol.WithBudget(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestProbeRequestBudgetRejectsBeforeBusinessDispatch(t *testing.T) {
	probe, control, called := fixtureProbe(t, true, nil)
	ctx := probeTestContext(t)
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	if _, err := probe.ReadAdvertiser(context.Background(), probeBalanceQuery, fixtureAdvertiserID, digest); !errors.Is(err, ErrProbeBudget) {
		t.Fatalf("missing budget accepted: %v", err)
	}
	if control.reads.Load() != 0 {
		t.Fatal("missing budget acquired a read slot")
	}
	budget, err := requestcontrol.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = requestcontrol.WithBudget(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest); !errors.Is(err, ErrProbeBudget) {
		t.Fatalf("exhausted budget accepted: %v", err)
	}
	if called.Load() != 1 || probe.Counts().ToolCall != 1 || budget.Snapshot().Used != 1 || control.inFlight.Load() != 0 {
		t.Fatal("budget rejection dispatched or leaked a semantic read slot")
	}
}

type fixtureForwarder struct {
	target *url.URL
	t      *testing.T
}

func (forward fixtureForwarder) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != probeEndpoint || request.Header.Get("Access-Token") != "synthetic-fixture-token" {
		forward.t.Error("fixed endpoint or server credential injection changed")
	}
	var selected []string
	if json.Unmarshal([]byte(request.Header.Get("Tool-Range")), &selected) != nil || len(selected) != 3 {
		forward.t.Error("Tool-Range is not the documented JSON array")
	}
	if request.Method == http.MethodPost {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(data), "synthetic-fixture-token") {
			forward.t.Error("credential entered an MCP message")
		}
	}
	clone := request.Clone(request.Context())
	copyURL := *request.URL
	copyURL.Scheme = forward.target.Scheme
	copyURL.Host = forward.target.Host
	clone.URL = &copyURL
	clone.Host = forward.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

type fixtureReadControl struct{ reads, serial, inFlight, rateFeedback atomic.Int64 }

func (control *fixtureReadControl) Acquire(ctx context.Context, id string) (func(error, *http.Response) error, error) {
	control.serial.Add(1)
	return control.AcquireRead(ctx, id)
}
func (control *fixtureReadControl) AcquireRead(ctx context.Context, id string) (func(error, *http.Response) error, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if id != fixtureAdvertiserID {
		return nil, ErrProbeScope
	}
	control.reads.Add(1)
	control.inFlight.Add(1)
	return func(err error, _ *http.Response) error {
		if err != nil && strings.Contains(err.Error(), "40100") {
			control.rateFeedback.Add(1)
		}
		control.inFlight.Add(-1)
		return nil
	}, nil
}

func TestProbeBusinessRateLimitFeedsExistingSharedCooldown(t *testing.T) {
	probe, control, _ := fixtureProbe(t, true, &mcp.CallToolResult{StructuredContent: json.RawMessage(
		`{"code":40100,"message":"synthetic_secret_must_not_appear","data":null}`,
	)})
	ctx := probeTestContext(t)
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	for _, tool := range discovery.Tools {
		if tool.Name == probeBalanceQuery {
			digest = tool.InputSourceDigest
		}
	}
	observation, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest)
	if !errors.Is(err, ErrProbeRateLimited) || !errors.Is(err, ErrProbeBusiness) || !observation.RateLimited ||
		!observation.ProtocolSucceeded || observation.BusinessSucceeded || observation.APICode != "40100" ||
		control.rateFeedback.Load() != 1 || control.inFlight.Load() != 0 || probe.Counts().ToolCall != 1 {
		t.Fatalf("business rate feedback lost: %#v %v", observation, err)
	}
	if strings.Contains(err.Error(), "synthetic_secret") {
		t.Fatal("server message entered shared cooldown")
	}
}
