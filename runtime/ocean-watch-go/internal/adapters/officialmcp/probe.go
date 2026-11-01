// Package officialmcp contains the inactive P0.5 connection prototype.
// No Host tool or production Application route uses it. A passed prototype
// does not enable catalog candidates or replace the P1 shared policy.
package officialmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

const (
	probeEndpoint     = "https://open.oceanengine.com/qianchuan/mcp"
	probeBalanceQuery = "qianchuan_account_balance_get_v1"
	probeQuotaQuery   = "qianchuan_ad_quota_get_v1"
	probeListQuery    = "qianchuan_uni_promotion_list_v1"
	maxProbeToolPages = 4
)

var (
	ErrProbeEndpoint        = errors.New("official MCP probe endpoint rejected")
	ErrProbeTransport       = errors.New("official MCP probe transport failed")
	ErrProbeInvalidResponse = errors.New("official MCP probe response is invalid")
	ErrProbeSchema          = errors.New("official MCP probe requires a verified input schema")
	ErrProbeScope           = errors.New("official MCP probe advertiser scope is invalid")
	ErrProbeToolUnavailable = errors.New("official MCP probe tool is unavailable")
	ErrProbeIncomplete      = errors.New("official MCP probe tool discovery is incomplete")
	ErrProbeBusiness        = errors.New("official MCP probe business query failed")
	ErrProbeBudget          = errors.New("official MCP probe request budget rejected")
	ErrProbeRateLimited     = fmt.Errorf("%w: system rate limit 40100", ErrProbeBusiness)
)

type Probe struct {
	session *mcp.ClientSession
	control requestcontrol.SharedController
	counts  probeCounters
	mu      sync.Mutex
	tools   map[string]ProbeTool
}

type probeCounters struct {
	initialize atomic.Int64
	list       atomic.Int64
	call       atomic.Int64
}

type ProbeCounts struct {
	Initialize int64 `json:"initialize"`
	ToolList   int64 `json:"tools_list"`
	ToolCall   int64 `json:"tools_call"`
}

type ProbeTool struct {
	Name              string          `json:"name"`
	InputSchema       json.RawMessage `json:"-"`
	OutputSchema      json.RawMessage `json:"-"`
	InputSourceDigest string          `json:"input_source_digest"`
}

type ProbeDiscovery struct {
	Tools           []ProbeTool `json:"tools"`
	Complete        bool        `json:"complete"`
	UnexpectedTools int         `json:"unexpected_tools"`
}

// Payload is private to the probe/Adapter. Only a later verified Application
// projection may expose business facts to a Host. No raw response is persisted.
type ProbeObservation struct {
	QueryID             string         `json:"query_id"`
	ProtocolSucceeded   bool           `json:"protocol_succeeded"`
	BusinessSucceeded   bool           `json:"business_succeeded"`
	APICode             string         `json:"api_code"`
	PayloadFormat       string         `json:"payload_format,omitempty"`
	ErrorParameterHints []string       `json:"error_parameter_hints,omitempty"`
	RateLimited         bool           `json:"rate_limited,omitempty"`
	Payload             map[string]any `json:"-"`
}

// The caller obtains the token from the existing TokenManager/TokenLease.
// Do not call this in plugin development against real state without the user's
// explicit business scope. There is no credential loading or refresh here.
func NewProbe(ctx context.Context, accessToken string, control requestcontrol.SharedController) (*Probe, error) {
	return newProbe(ctx, accessToken, control, http.DefaultTransport)
}

func newProbe(ctx context.Context, accessToken string, control requestcontrol.SharedController, base http.RoundTripper) (*Probe, error) {
	if accessToken == "" || strings.ContainsAny(accessToken, "\r\n\x00") || control == nil || base == nil {
		return nil, ErrProbeScope
	}
	probe := &Probe{control: control, tools: map[string]ProbeTool{}}
	toolRange, _ := json.Marshal([]string{probeBalanceQuery, probeQuotaQuery, probeListQuery})
	httpClient := &http.Client{
		Transport:     &fixedTokenTransport{base: base, token: accessToken, toolRange: string(toolRange), counts: &probe.counts},
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProbeEndpoint },
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ocean-watch-official-probe", Version: "p0.5"}, &mcp.ClientOptions{
		Capabilities:   &mcp.ClientCapabilities{},
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: probeEndpoint, HTTPClient: httpClient,
		MaxRetries: -1, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, safeProbeError(ctx)
	}
	probe.session = session
	return probe, nil
}

func (probe *Probe) Close() error {
	if probe.session.Close() != nil {
		return ErrProbeTransport
	}
	return nil
}

func (probe *Probe) Counts() ProbeCounts {
	return ProbeCounts{Initialize: probe.counts.initialize.Load(), ToolList: probe.counts.list.Load(), ToolCall: probe.counts.call.Load()}
}

func (probe *Probe) Inspect(ctx context.Context) (ProbeDiscovery, error) {
	probe.mu.Lock()
	probe.tools = map[string]ProbeTool{}
	probe.mu.Unlock()
	discovery := ProbeDiscovery{Tools: []ProbeTool{}}
	selected := map[string]ProbeTool{}
	cursor, seenCursors := "", map[string]bool{}
	for page := 0; page < maxProbeToolPages; page++ {
		callCtx, capture := captureContext(ctx)
		_, err := probe.session.ListTools(callCtx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return discovery, safeProbeError(ctx)
		}
		raw, _, err := capture.read()
		if err != nil {
			return discovery, err
		}
		// ProbeTool's private schemas are decoded explicitly from the raw wire.
		var wire struct {
			Tools []struct {
				Name         string          `json:"name"`
				InputSchema  json.RawMessage `json:"inputSchema"`
				OutputSchema json.RawMessage `json:"outputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &wire) != nil {
			return discovery, ErrProbeInvalidResponse
		}
		for _, tool := range wire.Tools {
			if !probeQuery(tool.Name) {
				discovery.UnexpectedTools++
				continue
			}
			if _, duplicate := selected[tool.Name]; duplicate {
				return discovery, ErrProbeInvalidResponse
			}
			sum := sha256.Sum256(tool.InputSchema)
			converted := ProbeTool{Name: tool.Name, InputSchema: append(json.RawMessage{}, tool.InputSchema...), OutputSchema: append(json.RawMessage{}, tool.OutputSchema...), InputSourceDigest: hex.EncodeToString(sum[:])}
			selected[tool.Name] = converted
			copy := converted
			copy.InputSchema = append(json.RawMessage{}, converted.InputSchema...)
			copy.OutputSchema = append(json.RawMessage{}, converted.OutputSchema...)
			discovery.Tools = append(discovery.Tools, copy)
		}
		if wire.NextCursor == "" {
			discovery.Complete = true
			probe.mu.Lock()
			probe.tools = selected
			probe.mu.Unlock()
			return discovery, nil
		}
		if seenCursors[wire.NextCursor] || len(wire.NextCursor) > 4096 {
			return discovery, ErrProbeIncomplete
		}
		seenCursors[wire.NextCursor] = true
		cursor = wire.NextCursor
	}
	return discovery, ErrProbeIncomplete
}

// The simple reads accept only an advertiser. The bounded plan-page probe has
// its own narrow entry. Every call requires a reviewed current source schema.
func (probe *Probe) ReadAdvertiser(ctx context.Context, queryID, advertiserID, approvedInputSourceDigest string) (ProbeObservation, error) {
	observation := ProbeObservation{QueryID: queryID}
	if queryID != probeBalanceQuery && queryID != probeQuotaQuery {
		return observation, ErrProbeToolUnavailable
	}
	id, err := strconv.ParseInt(advertiserID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != advertiserID {
		return observation, ErrProbeScope
	}
	return probe.readQuery(ctx, queryID, advertiserID, approvedInputSourceDigest, map[string]any{"advertiser_id": id})
}

func (probe *Probe) readQuery(ctx context.Context, queryID, advertiserID, approvedInputSourceDigest string, arguments map[string]any) (observation ProbeObservation, resultErr error) {
	observation = ProbeObservation{QueryID: queryID}
	if !probeQuery(queryID) {
		return observation, ErrProbeToolUnavailable
	}
	probe.mu.Lock()
	tool, exists := probe.tools[queryID]
	probe.mu.Unlock()
	if !exists {
		return observation, ErrProbeToolUnavailable
	}
	if approvedInputSourceDigest == "" || approvedInputSourceDigest != tool.InputSourceDigest {
		return observation, ErrProbeSchema
	}
	var schema jsonschema.Schema
	if json.Unmarshal(tool.InputSchema, &schema) != nil || schema.Type != "object" || schema.Properties["advertiser_id"] == nil {
		return observation, ErrProbeSchema
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{
		Loader: func(*url.URL) (*jsonschema.Schema, error) { return nil, ErrProbeSchema },
	})
	if err != nil || resolved.Validate(arguments) != nil {
		return observation, ErrProbeSchema
	}
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return observation, ErrProbeSchema
	}
	if _, ok := requestcontrol.BudgetFrom(ctx); !ok {
		return observation, ErrProbeBudget
	}
	var release func(error, *http.Response) error
	if reads, ok := probe.control.(requestcontrol.SharedReadController); ok {
		release, err = reads.AcquireRead(ctx, advertiserID)
	} else {
		release, err = probe.control.Acquire(ctx, advertiserID)
	}
	if err != nil {
		return observation, safeProbeError(ctx)
	}
	if err := requestcontrol.ReserveAttempt(ctx); err != nil {
		_ = release(err, nil)
		return observation, ErrProbeBudget
	}
	var response *http.Response
	defer func() {
		// Decode the business envelope before feedback: an HTTP 200 may carry
		// the documented 40100 system limit. Only safe typed failures reach
		// the existing SDK/MCP shared cooldown, never a server message.
		if releaseErr := release(resultErr, response); releaseErr != nil {
			observation.BusinessSucceeded = false
			observation.Payload = nil
			resultErr = safeProbeError(ctx)
		}
	}()
	callCtx, capture := captureContext(ctx)
	_, callErr := probe.session.CallTool(callCtx, &mcp.CallToolParams{
		Name: queryID, Arguments: json.RawMessage(encodedArguments),
	})
	raw, response, captureErr := capture.read()
	if callErr != nil {
		return observation, safeProbeError(ctx)
	}
	if captureErr != nil {
		return observation, captureErr
	}
	payload, isError, err := decodeProbePayload(raw)
	observation.ProtocolSucceeded = err == nil
	if err != nil {
		return observation, err
	}
	if isError {
		observation.PayloadFormat = "mcp_error"
		return observation, ErrProbeBusiness
	}
	var format struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &format) != nil {
		return observation, ErrProbeInvalidResponse
	}
	observation.PayloadFormat = "json_text"
	if len(format.Structured) > 0 && !bytes.Equal(bytes.TrimSpace(format.Structured), []byte("null")) {
		observation.PayloadFormat = "structured_json"
	}
	code, ok := payload["code"].(json.Number)
	if !ok {
		return observation, ErrProbeInvalidResponse
	}
	observation.APICode = code.String()
	if code.String() != "0" {
		if code.String() == "40100" {
			observation.RateLimited = true
			return observation, ErrProbeRateLimited
		}
		message, _ := payload["message"].(string)
		words := map[string]bool{}
		for _, word := range strings.FieldsFunc(strings.ToLower(message), func(char rune) bool {
			return char != '_' && (char < 'a' || char > 'z') && (char < '0' || char > '9')
		}) {
			words[word] = true
		}
		for _, field := range []string{"advertiser_id", "start_time", "end_time", "marketing_goal", "fields", "filtering", "page_size", "page", "adlab_scene", "data_period", "order_type", "order_field"} {
			if words[field] {
				observation.ErrorParameterHints = append(observation.ErrorParameterHints, field)
			}
		}
		return observation, ErrProbeBusiness
	}
	if data, ok := payload["data"].(map[string]any); ok {
		if subject, present := data["advertiser_id"]; present && subject != nil {
			var returnedID string
			switch subject := subject.(type) {
			case json.Number:
				returnedID = subject.String()
			case string:
				returnedID = subject
			default:
				return observation, ErrProbeInvalidResponse
			}
			if returnedID != advertiserID {
				return observation, ErrProbeScope
			}
		}
	}
	observation.BusinessSucceeded = true
	observation.Payload = payload
	return observation, nil
}

func probeQuery(queryID string) bool {
	return queryID == probeBalanceQuery || queryID == probeQuotaQuery || queryID == probeListQuery
}

func safeProbeError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrProbeTransport
}

func decodeProbePayload(raw json.RawMessage) (map[string]any, bool, error) {
	var result struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, false, ErrProbeInvalidResponse
	}
	if result.IsError {
		return nil, true, nil
	}
	payload := result.Structured
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		for _, content := range result.Content {
			if content.Type == "text" && json.Valid([]byte(content.Text)) {
				if len(payload) != 0 && !bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
					return nil, result.IsError, ErrProbeInvalidResponse
				}
				payload = []byte(content.Text)
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		return nil, result.IsError, ErrProbeInvalidResponse
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, result.IsError, ErrProbeInvalidResponse
	}
	return object, result.IsError, nil
}
