package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
