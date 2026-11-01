package officialmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRawCaptureHandlesSplitMultilineSSEAndUnrelatedMessages(t *testing.T) {
	wire := ":keep-alive\r\n\r\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\r\n\r\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{}}\r\n\r\n" +
		"data: {\"jsonrpc\":\"2.0\",\r\n" +
		"data: \"id\":7,\"result\":{\"structuredContent\":{\"code\":0,\"id\":9007199254740993}}}\r\n\r\n"
	capture := &responseCapture{}
	body := &captureBody{ReadCloser: io.NopCloser(strings.NewReader(wire)), capture: capture, expectedID: []byte("7"), sse: true}
	buffer := make([]byte, 7)
	for {
		_, err := body.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if body.Close() != nil {
		t.Fatal("body close failed")
	}
	raw, _, err := capture.read()
	if err != nil || !bytes.Contains(raw, []byte(fixtureAdvertiserID)) {
		t.Fatalf("SSE precision capture failed: %v", err)
	}
}

func TestRawCaptureBoundsPayloadAndRejectsDuplicateResponses(t *testing.T) {
	capture := &responseCapture{}
	body := &captureBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", maxProbeResponseBytes+1))), capture: capture, expectedID: []byte("1")}
	_, err := io.Copy(io.Discard, body)
	_ = body.Close()
	if !errors.Is(err, ErrProbeInvalidResponse) {
		t.Fatalf("oversized response=%v", err)
	}
	capture = &responseCapture{}
	wire := []byte(`{"id":1,"result":{"code":0}}`)
	capture.accept(wire, []byte("1"))
	capture.accept(wire, []byte("1"))
	if _, _, err := capture.read(); !errors.Is(err, ErrProbeInvalidResponse) {
		t.Fatal("duplicate response accepted")
	}
}

func TestFixedEndpointRejectsRedirectTargetsAndCredentialURLs(t *testing.T) {
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("rejected endpoint was dispatched")
		return nil, nil
	})
	transport := &fixedTokenTransport{base: base, token: "synthetic-fixture-token", counts: &probeCounters{}}
	for _, target := range []string{
		"https://example.invalid/qianchuan/mcp",
		"http://open.oceanengine.com/qianchuan/mcp",
		"https://open.oceanengine.com/qianchuan/mcp?access_token=synthetic-fixture-token",
		"https://open.oceanengine.com/qianchuan/mcp#fragment",
		"https://user:synthetic-fixture-token@open.oceanengine.com/qianchuan/mcp",
		"https://open.oceanengine.com/qianchuan/%6dcp",
	} {
		request, err := http.NewRequest(http.MethodPost, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transport.RoundTrip(request); !errors.Is(err, ErrProbeEndpoint) {
			t.Fatalf("unsafe endpoint result=%v", err)
		}
	}
}

func TestProbeIncompleteDiscoveryDoesNotEnableSubset(t *testing.T) {
	probe, control, _ := fixtureProbe(t, true, nil, func(server *mcp.Server) {
		for _, name := range []string{"fixture_extra_a", "fixture_extra_b"} {
			server.AddTool(&mcp.Tool{Name: name, InputSchema: json.RawMessage(fixtureInputSchema)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return nil, ErrProbeToolUnavailable
			})
		}
	})
	ctx, cancel := context.WithTimeout(probeTestContext(t), 5*time.Second)
	defer cancel()
	discovery, err := probe.Inspect(ctx)
	if !errors.Is(err, ErrProbeIncomplete) || discovery.Complete {
		t.Fatalf("discovery=%#v %v", discovery, err)
	}
	if _, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, "unverified"); !errors.Is(err, ErrProbeToolUnavailable) {
		t.Fatalf("incomplete discovery enabled a query: %v", err)
	}
	if control.reads.Load() != 0 || probe.Counts().ToolCall != 0 {
		t.Fatal("incomplete discovery dispatched")
	}
}

func TestProbeCancellationAfterDispatchReleasesSemanticReadSlot(t *testing.T) {
	started := make(chan struct{})
	probe, control, _ := fixtureProbe(t, false, nil, func(server *mcp.Server) {
		server.AddTool(&mcp.Tool{Name: probeBalanceQuery, InputSchema: json.RawMessage(fixtureInputSchema)}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
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
	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	done := make(chan error, 1)
	go func() {
		_, err := probe.ReadAdvertiser(callCtx, probeBalanceQuery, fixtureAdvertiserID, digest)
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("fixture call never started")
	}
	cancelCall()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || control.inFlight.Load() != 0 || control.reads.Load() != 1 || probe.Counts().ToolCall != 1 {
			t.Fatalf("cancelled call=%v active=%d reads=%d calls=%d", err, control.inFlight.Load(), control.reads.Load(), probe.Counts().ToolCall)
		}
	case <-ctx.Done():
		t.Fatal("cancellation did not return")
	}
}

func TestProbeInspectionReturnsOwnedSchemaCopies(t *testing.T) {
	probe, _, _ := fixtureProbe(t, true, nil)
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
			for i := range tool.InputSchema {
				tool.InputSchema[i] = 'x'
			}
		}
	}
	if _, err := probe.ReadAdvertiser(ctx, probeBalanceQuery, fixtureAdvertiserID, digest); err != nil {
		t.Fatalf("caller corrupted stored schema: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
