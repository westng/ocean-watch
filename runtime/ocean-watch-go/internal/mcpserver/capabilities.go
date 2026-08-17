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
      "required":["domain","action","channel","effect","requires_submit","description"],
      "properties":{
        "domain":{"type":"string","minLength":1,"maxLength":64},"action":{"type":"string","minLength":1,"maxLength":64},
        "channel":{"type":"string","enum":["marketing","qianchuan","shared"]},
		"effect":{"type":"string","enum":["local_read","local_write","public_read","authorization_write","official_read","online_write"]},
        "requires_submit":{"type":"boolean"},"description":{"type":"string","minLength":1,"maxLength":256}
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
	Domain         string `json:"domain"`
	Action         string `json:"action"`
	Channel        string `json:"channel"`
	Effect         string `json:"effect"`
	RequiresSubmit bool   `json:"requires_submit"`
	Description    string `json:"description"`
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
		items := make([]capabilityItem, 0, len(contracts.Commands))
		for _, command := range contracts.Commands {
			item := commandCapability(command)
			if channel == "all" || item.Channel == channel || item.Channel == "shared" {
				items = append(items, item)
			}
		}
		output := capabilityOutput{OK: true, RuntimeVersion: version, Commands: items}
		return resultFor(output, false), nil
	}
}

func commandCapability(command contracts.Command) capabilityItem {
	capability := contracts.CapabilityFor(command)
	return capabilityItem{
		Domain: command.Domain, Action: command.Action, Channel: capability.Channel,
		Effect: string(capability.Effect), RequiresSubmit: capability.RequiresSubmit,
		Description: command.Description,
	}
}
