package officialmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

const clientSchemaTTL = 5 * time.Minute
const maximumClientSessions = 8

type clientEntry struct {
	ready         chan struct{}
	session       *mcp.ClientSession
	tools         map[string]official.Tool
	counts        probeCounters
	authorization string
	revision      int
	created       time.Time
	active        int
	retired       bool
	closing       bool
	done          chan struct{}
	err           error
}

// Client owns bounded sessions; permissions remain in Application. A schema
// refresh creates a session rather than consuming the SDK's cached tools/list.
type Client struct {
	control   requestcontrol.SharedController
	base      http.RoundTripper
	mu        sync.Mutex
	entries   map[string]*clientEntry
	all       map[*clientEntry]bool
	revisions map[string]int
	closed    bool
	now       func() time.Time
	lifetime  context.Context
	cancel    context.CancelFunc
	closers   sync.WaitGroup
}

func NewClient(control requestcontrol.SharedController) *Client {
	return newClient(control, http.DefaultTransport)
}

func newClient(control requestcontrol.SharedController, base http.RoundTripper) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{control: control, base: base, entries: map[string]*clientEntry{}, all: map[*clientEntry]bool{}, revisions: map[string]int{}, now: time.Now, lifetime: ctx, cancel: cancel}
}

func clientFailure(code, stage string, dispatched bool) error {
	return &official.Failure{Code: code, Stage: stage, Dispatched: dispatched}
}

func canonicalProfile(profile []string) ([]string, error) {
	if len(profile) == 0 || len(profile) > 16 {
		return nil, clientFailure("INVALID_QUERY_PROFILE", "policy", false)
	}
	catalog, err := contracts.QianchuanOfficialCandidates()
	if err != nil {
		return nil, clientFailure("CONTRACT_INVALID", "policy", false)
	}
	copy := append([]string(nil), profile...)
	sort.Strings(copy)
	for i, name := range copy {
		if _, ok := catalog.Find(name); !ok || i > 0 && name == copy[i-1] {
			return nil, clientFailure("INVALID_QUERY_PROFILE", "policy", false)
		}
	}
	return copy, nil
}

func (client *Client) connect(ctx context.Context, lease official.Lease, profile []string, entry *clientEntry) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(client.lifetime, cancel)
	defer stop()
	defer cancel()
	if lease.Token == "" || strings.ContainsAny(lease.Token, "\r\n\x00") || lease.AuthorizationID == "" || lease.Revision < 1 || client.control == nil || client.base == nil {
		return clientFailure("AUTHORIZATION_INVALID", "authorization", false)
	}
	rangeJSON, _ := json.Marshal(profile)
	httpClient := &http.Client{Transport: &fixedTokenTransport{base: client.base, token: lease.Token, toolRange: string(rangeJSON), counts: &entry.counts}, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProbeEndpoint }}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "ocean-watch-official-query", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}, Logger: slog.New(slog.DiscardHandler)})
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: probeEndpoint, HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return clientFailure("OFFICIAL_CONNECTION_FAILED", "initialize", false)
	}
	tools, err := discoverClientTools(ctx, session, profile)
	if err != nil {
		_ = session.Close()
		return err
	}
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		_ = session.Close()
		return clientFailure("OFFICIAL_CLIENT_CLOSED", "initialize", false)
	}
	entry.session = session
	entry.tools = tools
	client.mu.Unlock()
	return nil
}

func discoverClientTools(ctx context.Context, session *mcp.ClientSession, profile []string) (map[string]official.Tool, error) {
	wanted := map[string]bool{}
	for _, name := range profile {
		wanted[name] = true
	}
	tools := map[string]official.Tool{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < maxProbeToolPages; page++ {
		callCtx, capture := captureContext(ctx)
		if _, err := session.ListTools(callCtx, &mcp.ListToolsParams{Cursor: cursor}); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, clientFailure("OFFICIAL_DISCOVERY_FAILED", "tools_list", false)
		}
		raw, _, err := capture.read()
		if err != nil {
			return nil, clientFailure("INVALID_RESPONSE", "tools_list", false)
		}
		var wire struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Input       json.RawMessage `json:"inputSchema"`
				Output      json.RawMessage `json:"outputSchema"`
			} `json:"tools"`
			Cursor string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &wire) != nil {
			return nil, clientFailure("INVALID_RESPONSE", "tools_list", false)
		}
		for _, tool := range wire.Tools {
			if !wanted[tool.Name] {
				continue
			}
			if _, exists := tools[tool.Name]; exists {
				return nil, clientFailure("INVALID_RESPONSE", "tools_list", false)
			}
			if len(tool.Input) == 0 || !json.Valid(tool.Input) {
				return nil, clientFailure("INVALID_RESPONSE", "tools_list", false)
			}
			sum := sha256.Sum256(tool.Input)
			tools[tool.Name] = official.Tool{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.Input...), OutputSchema: append(json.RawMessage(nil), tool.Output...), SourceDigest: hex.EncodeToString(sum[:])}
		}
		if wire.Cursor == "" {
			return tools, nil
		}
		if len(wire.Cursor) > 4096 || seen[wire.Cursor] {
			return nil, clientFailure("DISCOVERY_INCOMPLETE", "tools_list", false)
		}
		seen[wire.Cursor] = true
		cursor = wire.Cursor
	}
	return nil, clientFailure("DISCOVERY_INCOMPLETE", "tools_list", false)
}

func (client *Client) acquire(ctx context.Context, lease official.Lease, profile []string) (*clientEntry, func(), error) {
	profile, err := canonicalProfile(profile)
	if err != nil {
		return nil, nil, err
	}
	if lease.Token == "" || lease.AuthorizationID == "" || lease.Revision < 1 {
		return nil, nil, clientFailure("AUTHORIZATION_INVALID", "authorization", false)
	}
	profileJSON, _ := json.Marshal(profile)
	keyHash := sha256.Sum256([]byte(lease.AuthorizationID + "\x00" + lease.Token + "\x00" + string(profileJSON)))
	key := hex.EncodeToString(keyHash[:])
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return nil, nil, clientFailure("OFFICIAL_CLIENT_CLOSED", "initialize", false)
	}
	if lease.Revision < client.revisions[lease.AuthorizationID] {
		client.mu.Unlock()
		return nil, nil, clientFailure("AUTHORIZATION_STALE", "authorization", false)
	}
	if lease.Revision > client.revisions[lease.AuthorizationID] {
		client.revisions[lease.AuthorizationID] = lease.Revision
		for candidate, e := range client.entries {
			if e.authorization == lease.AuthorizationID && e.revision != lease.Revision {
				client.retireLocked(candidate, e)
			}
		}
	}
	entry := client.entries[key]
	if entry != nil && client.now().Sub(entry.created) >= clientSchemaTTL {
		client.retireLocked(key, entry)
		entry = nil
	}
	creator := entry == nil
	if creator {
		if len(client.all) >= maximumClientSessions {
			var oldestKey string
			var oldest *clientEntry
			for candidate, e := range client.entries {
				if e.active == 0 && (oldest == nil || e.created.Before(oldest.created)) {
					oldestKey, oldest = candidate, e
				}
			}
			if oldest == nil {
				client.mu.Unlock()
				return nil, nil, clientFailure("OFFICIAL_SESSION_BUDGET", "initialize", false)
			}
			client.retireLocked(oldestKey, oldest)
			done := oldest.done
			client.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-done:
			}
			return client.acquire(ctx, lease, profile)
		}
		if len(client.all) >= maximumClientSessions {
			client.mu.Unlock()
			return nil, nil, clientFailure("OFFICIAL_SESSION_BUDGET", "initialize", false)
		}
		entry = &clientEntry{ready: make(chan struct{}), done: make(chan struct{}), authorization: lease.AuthorizationID, revision: lease.Revision, created: client.now()}
		client.entries[key] = entry
		client.all[entry] = true
	}
	entry.active++
	client.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			client.mu.Lock()
			defer client.mu.Unlock()
			entry.active--
			client.closeIdleLocked(entry)
		})
	}
	if creator {
		err := client.connect(ctx, lease, profile, entry)
		client.mu.Lock()
		entry.err = err
		close(entry.ready)
		if err != nil {
			client.retireLocked(key, entry)
		}
		client.mu.Unlock()
	} else {
		select {
		case <-ctx.Done():
			release()
			return nil, nil, ctx.Err()
		case <-entry.ready:
		}
	}
	if entry.err != nil {
		release()
		return nil, nil, entry.err
	}
	return entry, release, nil
}

func (client *Client) retireLocked(key string, entry *clientEntry) {
	delete(client.entries, key)
	entry.retired = true
	client.closeIdleLocked(entry)
}

// Closing a remote session may perform I/O. Retired sessions continue to count
// against the pool limit until closure, but never hold the pool mutex during I/O.
func (client *Client) closeIdleLocked(entry *clientEntry) {
	if !entry.retired || entry.active != 0 || entry.closing {
		return
	}
	if entry.session == nil {
		delete(client.all, entry)
		close(entry.done)
		entry.closing = true
		return
	}
	entry.closing = true
	client.closers.Add(1)
	go func() {
		defer client.closers.Done()
		_ = entry.session.Close()
		client.mu.Lock()
		delete(client.all, entry)
		close(entry.done)
		client.mu.Unlock()
	}()
}

// Discover is for explicitly authorized integration/schema capture, not a Host
// route. It never calls a business tool or changes the compiled query catalog.
func (client *Client) Discover(ctx context.Context, lease official.Lease, profile []string) ([]official.Tool, error) {
	entry, release, err := client.acquire(ctx, lease, profile)
	if err != nil {
		return nil, err
	}
	defer release()
	tools := []official.Tool{}
	for _, name := range profile {
		if tool, ok := entry.tools[name]; ok {
			tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
			tool.OutputSchema = append(json.RawMessage(nil), tool.OutputSchema...)
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

func (client *Client) Read(ctx context.Context, request official.Request) (response official.Response, resultErr error) {
	if ctx == nil {
		return response, clientFailure("INVALID_ARGUMENT", "request", false)
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(client.lifetime, cancel)
	defer stop()
	defer cancel()
	if _, ok := requestcontrol.BudgetFrom(ctx); !ok {
		return response, clientFailure("REQUEST_BUDGET_REQUIRED", "budget", false)
	}
	if !containsProfile(request.Profile, request.QueryID) {
		return response, clientFailure("INVALID_QUERY_PROFILE", "policy", false)
	}
	entry, releaseSession, err := client.acquire(ctx, request.Lease, request.Profile)
	if err != nil {
		return response, err
	}
	defer releaseSession()
	tool, ok := entry.tools[request.QueryID]
	if !ok {
		return response, clientFailure("OFFICIAL_TOOL_UNAVAILABLE", "tools_list", false)
	}
	baseline := request.SourceInput
	if len(baseline) == 0 {
		baseline = request.ApprovedInput
	}
	compatibility, err := CheckSchemaCompatibility(baseline, tool.InputSchema)
	if err != nil {
		return response, clientFailure("SCHEMA_REVIEW_REQUIRED", "schema", false)
	}
	if len(request.ApprovedOutput) > 0 && string(request.ApprovedOutput) != "null" {
		if _, err := CheckSchemaCompatibility(request.ApprovedOutput, tool.OutputSchema); err != nil {
			return response, clientFailure("SCHEMA_REVIEW_REQUIRED", "output_schema", false)
		}
	}
	argumentsJSON, err := json.Marshal(request.Arguments)
	if err != nil {
		return response, clientFailure("INVALID_PARAMETERS", "parameters", false)
	}
	decoder := json.NewDecoder(bytes.NewReader(argumentsJSON))
	decoder.UseNumber()
	var arguments map[string]any
	if decoder.Decode(&arguments) != nil || ValidateSchemaArguments(request.ApprovedInput, arguments) != nil {
		return response, clientFailure("INVALID_PARAMETERS", "parameters", false)
	}
	var release func(error, *http.Response) error
	if reads, ok := client.control.(requestcontrol.SharedReadController); ok {
		release, err = reads.AcquireRead(ctx, request.ControlKey)
	} else {
		release, err = client.control.Acquire(ctx, request.ControlKey)
	}
	if err != nil {
		if ctx.Err() != nil {
			return response, ctx.Err()
		}
		return response, clientFailure("READ_ADMISSION_FAILED", "admission", false)
	}
	if err := requestcontrol.ReserveAttempt(ctx); err != nil {
		_ = release(err, nil)
		return response, clientFailure("REQUEST_BUDGET_EXHAUSTED", "budget", false)
	}
	var httpResponse *http.Response
	defer func() {
		if err := release(resultErr, httpResponse); err != nil {
			response = official.Response{}
			resultErr = clientFailure("READ_FEEDBACK_FAILED", "admission", true)
		}
	}()
	callCtx, capture := captureContext(ctx)
	_, callErr := entry.session.CallTool(callCtx, &mcp.CallToolParams{Name: request.QueryID, Arguments: json.RawMessage(argumentsJSON)})
	raw, httpResponse, captureErr := capture.read()
	if callErr != nil {
		client.invalidate(entry)
		if ctx.Err() != nil {
			return response, ctx.Err()
		}
		return response, clientFailure("OFFICIAL_REQUEST_FAILED", "tools_call", true)
	}
	if captureErr != nil {
		return response, clientFailure("INVALID_RESPONSE", "tools_call", true)
	}
	envelope, isError, err := decodeProbePayload(raw)
	if err != nil {
		return response, clientFailure("INVALID_RESPONSE", "decode", true)
	}
	if isError {
		return response, clientFailure("OFFICIAL_TOOL_ERROR", "tools_call", true)
	}
	code, ok := envelope["code"].(json.Number)
	if !ok {
		return response, clientFailure("INVALID_RESPONSE", "decode", true)
	}
	if code.String() != "0" {
		return response, &official.Failure{Code: businessFailureCode(code.String()), APICode: code.String(), Stage: "business", Dispatched: true}
	}
	data, ok := envelope["data"]
	if !ok {
		return response, clientFailure("INVALID_RESPONSE", "decode", true)
	}
	return official.Response{Data: data, SourceDigest: tool.SourceDigest, Compatibility: compatibility, APICode: "0"}, nil
}

func businessFailureCode(code string) string {
	switch code {
	case "40100", "40110", "40130":
		return "OFFICIAL_RATE_LIMIT_" + code
	case "40103", "40107":
		return "REAUTHORIZATION_REQUIRED"
	default:
		return "OFFICIAL_API_ERROR"
	}
}

func containsProfile(profile []string, name string) bool {
	for _, candidate := range profile {
		if candidate == name {
			return true
		}
	}
	return false
}

func (client *Client) invalidate(entry *clientEntry) {
	client.mu.Lock()
	defer client.mu.Unlock()
	for key, candidate := range client.entries {
		if candidate == entry {
			client.retireLocked(key, entry)
			return
		}
	}
}

func (client *Client) Close() error {
	client.cancel()
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		client.closers.Wait()
		return nil
	}
	client.closed = true
	closing := []*clientEntry{}
	for entry := range client.all {
		entry.retired = true
		if entry.session != nil && !entry.closing {
			entry.closing = true
			client.closers.Add(1)
			closing = append(closing, entry)
		}
	}
	client.entries = map[string]*clientEntry{}
	client.mu.Unlock()
	for _, entry := range closing {
		_ = entry.session.Close()
		client.mu.Lock()
		delete(client.all, entry)
		close(entry.done)
		client.mu.Unlock()
		client.closers.Done()
	}
	client.closers.Wait()
	return nil
}

var _ official.Backend = (*Client)(nil)
