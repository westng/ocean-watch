package officialmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

// This runs every frozen mapping through the production JSON/SSE wire path.
// The server advertises the original captured schemas, including documented
// corrections; Core's independent matrix covers scope, projection and paging.
func TestAll156OfficialMappingsProtocolMatrix(t *testing.T) {
	queries, err := contracts.ApprovedOfficialQueries()
	if err != nil || len(queries) != 156 {
		t.Fatal("frozen inventory unavailable")
	}
	execution := map[string]contracts.ApprovedOfficialQuery{}
	for _, query := range queries {
		if _, exists := execution[query.ExecutionQueryID]; !exists {
			execution[query.ExecutionQueryID] = query
		}
	}
	names := make([]string, 0, len(execution))
	for name := range execution {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, jsonResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "JSON", false: "SSE"}[jsonResponse], func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "catalog-fixture", Version: "1"}, nil)
			for name, query := range execution {
				server.AddTool(&mcp.Tool{Name: name, InputSchema: query.SourceInput}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					var arguments map[string]any
					decoder := json.NewDecoder(strings.NewReader(string(request.Params.Arguments)))
					decoder.UseNumber()
					if decoder.Decode(&arguments) != nil || ValidateSchemaArguments(query.ExecutionInput, arguments) != nil {
						t.Errorf("invalid frozen execution arguments for %s", name)
					}
					return &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"code":0,"data":{"advertiser_id":9007199254740993,"amount":1234567890.123456789}}`)}, nil
				})
			}
			local := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: jsonResponse}))
			t.Cleanup(local.Close)
			target, _ := url.Parse(local.URL)
			wire := &clientTestForwarder{target: target, t: t}
			control := &fixtureReadControl{}
			client := newClient(control, wire)
			t.Cleanup(func() { _ = client.Close() })
			ctx, _, _, err := requestcontrol.PrepareCommandContext(context.Background(), 512)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			for _, query := range queries {
				t.Run(query.QueryID, func(t *testing.T) {
					index := sort.SearchStrings(names, query.ExecutionQueryID)
					start := index / 16 * 16
					profile := names[start:min(start+16, len(names))]
					var schema map[string]any
					decoder := json.NewDecoder(strings.NewReader(string(query.ExecutionInput)))
					decoder.UseNumber()
					if decoder.Decode(&schema) != nil {
						t.Fatal("invalid execution schema")
					}
					input := protocolMatrixInput(schema, "").(map[string]any)
					arguments := map[string]json.RawMessage{}
					for name, value := range input {
						arguments[name], _ = json.Marshal(value)
					}
					response, err := client.Read(ctx, port.Request{QueryID: query.ExecutionQueryID, Profile: profile, Lease: port.Lease{AuthorizationID: "synthetic-grant", Revision: 1, Token: "synthetic-token"}, ControlKey: fixtureAdvertiserID, Arguments: arguments, ApprovedInput: query.ExecutionInput, SourceInput: query.SourceInput})
					if err != nil {
						t.Fatalf("frozen protocol mapping failed: %v", err)
					}
					data := response.Data.(map[string]any)
					if data["advertiser_id"].(json.Number).String() != fixtureAdvertiserID || data["amount"].(json.Number).String() != "1234567890.123456789" {
						t.Fatal("raw numeric precision changed")
					}
				})
			}
			if wire.calls.Load() != 156 || control.reads.Load() != 156 || control.inFlight.Load() != 0 || control.serial.Load() != 0 {
				t.Fatal("catalog matrix lost calls or leaked read admission")
			}
		})
	}
}

func protocolMatrixInput(schema map[string]any, name string) any {
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	switch schema["type"] {
	case "object":
		value := map[string]any{}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, name := range required {
			key, _ := name.(string)
			child, _ := properties[key].(map[string]any)
			value[key] = protocolMatrixInput(child, key)
		}
		return value
	case "array":
		items, _ := schema["items"].(map[string]any)
		return []any{protocolMatrixInput(items, name)}
	case "integer", "number":
		if contracts.OfficialIdentifierField(name) {
			return json.Number(fixtureAdvertiserID)
		}
		if value, exists := schema["minimum"]; exists {
			return value
		}
		return json.Number("1")
	case "boolean":
		return false
	default:
		if schema["format"] == "datetime" {
			return "2026-10-31 00:00:00"
		}
		if schema["format"] == "date" || strings.Contains(name, "date") || strings.Contains(name, "time") {
			return "2026-10-31"
		}
		return "synthetic-fixture"
	}
}

func TestPrivateOfficialArgumentsCannotSerializeCredentials(t *testing.T) {
	encoded, err := json.Marshal(port.Request{Arguments: map[string]json.RawMessage{"APP-Access-Token": json.RawMessage(`"synthetic-private-token"`)}, Lease: port.Lease{Token: "synthetic-private-token"}})
	if err != nil || strings.Contains(string(encoded), "synthetic-private-token") || strings.Contains(string(encoded), "APP-Access-Token") {
		t.Fatal("private arguments became serializable")
	}
}
