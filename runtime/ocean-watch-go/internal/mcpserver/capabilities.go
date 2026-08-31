package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
)

const capabilitiesInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{"channel":{"type":"string","enum":["all","marketing","qianchuan","shared"]}}
}`

const capabilitiesOutputSchema = `{
  "type":"object","additionalProperties":false,"required":["ok","runtime_version","commands"],
  "properties":{
    "ok":{"const":true},"runtime_version":{"type":"string","minLength":1,"maxLength":128},
    "commands":{"type":"array","items":{"type":"object","additionalProperties":false,
      "required":["id","channel","effect","requires_submit","description","primary_surface","route"],
      "properties":{
		"id":{"type":"string","minLength":1,"maxLength":128},"domain":{"type":"string","minLength":1,"maxLength":64},"action":{"type":"string","minLength":1,"maxLength":64},
		"channel":{"type":"string","enum":["marketing","qianchuan","shared"]},
		"effect":{"type":"string","enum":["local_read","local_write","public_read","authorization_write","official_read","online_write"]},
		"requires_submit":{"type":"boolean"},"description":{"type":"string","minLength":1,"maxLength":256},
		"primary_surface":{"type":"string","enum":["cli","mcp"]},"route":{"type":"string","minLength":1,"maxLength":256}
      }
    }}
  }
}`

type capabilityOutput struct {
	OK             bool             `json:"ok"`
	RuntimeVersion string           `json:"runtime_version"`
	Commands       []capabilityItem `json:"commands"`
}

type capabilityItem struct {
	ID             string `json:"id"`
	Domain         string `json:"domain"`
	Action         string `json:"action"`
	Channel        string `json:"channel"`
	Effect         string `json:"effect"`
	RequiresSubmit bool   `json:"requires_submit"`
	Description    string `json:"description"`
	PrimarySurface string `json:"primary_surface"`
	Route          string `json:"route"`
}

func capabilitiesHandler(version string) mcp.ToolHandler {
	return func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channel := "all"
		if len(request.Params.Arguments) > 0 {
			var input struct {
				Channel string `json:"channel"`
			}
			if err := decodeStrict(request.Params.Arguments, &input); err != nil {
				return nil, err
			}
			if strings.TrimSpace(input.Channel) != "" {
				channel = input.Channel
			}
		}
		items := make([]capabilityItem, 0, len(contracts.DefaultCapabilityRegistry.All()))
		for _, spec := range contracts.DefaultCapabilityRegistry.All() {
			item := capabilitySpecOutput(spec)
			if channel == "all" || item.Channel == channel || item.Channel == "shared" {
				items = append(items, item)
			}
		}
		output := capabilityOutput{OK: true, RuntimeVersion: version, Commands: items}
		return resultFor(output, false), nil
	}
}

func capabilitySpecOutput(spec contracts.CapabilitySpec) capabilityItem {
	item := capabilityItem{
		ID: spec.ID, Channel: spec.Channel, Effect: string(spec.Effect), RequiresSubmit: spec.RequiresSubmit,
		Description: spec.CommandDescription, PrimarySurface: string(spec.PrimarySurface), Route: spec.Route(),
	}
	if item.Description == "" {
		item.Description = "Invoke " + spec.MCPTool
	}
	if spec.CLICommand != "" {
		parts := strings.SplitN(spec.CLICommand, " ", 2)
		item.Domain, item.Action = parts[0], parts[1]
	}
	return item
}
