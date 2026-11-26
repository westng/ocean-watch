package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
)

func TestCapabilityResponsesConformToAdvertisedOutputSchema(t *testing.T) {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(capabilitiesOutputSchema), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{"all", "marketing", "qianchuan", "star_map", "shared"} {
		t.Run(channel, func(t *testing.T) {
			arguments, err := json.Marshal(map[string]string{"channel": channel})
			if err != nil {
				t.Fatal(err)
			}
			result, err := capabilitiesHandler("test")(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Name: "get_capabilities", Arguments: arguments},
			})
			if err != nil || result.IsError {
				t.Fatalf("capability query failed: result=%v err=%v", result, err)
			}
			value := decodeStructured[map[string]any](t, result)
			if err := resolved.Validate(value); err != nil {
				t.Fatalf("capability response violates its MCP output schema: %v", err)
			}
		})
	}
}

func TestPreviewCatalogDoesNotAdvertiseLegacyWritesAsEnabled(t *testing.T) {
	instance, err := policy.New(policy.Config{Mode: "execute", Toolsets: []string{"core", "marketing", "qianchuan", "reports"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := capabilitiesHandler("3.0.0-alpha.1", &instance)(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_capabilities", Arguments: json.RawMessage(`{"channel":"all"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	output := decodeStructured[capabilityOutput](t, result)
	legacy, operation := 0, false
	for _, item := range output.Commands {
		if item.Effect != "online_write" {
			continue
		}
		if item.ID == "qianchuan.operation_execute" {
			operation = item.Enabled
			continue
		}
		legacy++
		if item.Enabled || item.UnavailableReason != "legacy_write_unavailable_in_preview" {
			t.Fatalf("alpha advertised a legacy write: %+v", item)
		}
	}
	if legacy == 0 || !operation {
		t.Fatal("catalog did not distinguish supported trusted execution")
	}
}
