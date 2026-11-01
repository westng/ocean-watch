package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	docs "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/developerdocs"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
)

const developerOutputSchema = `{"type":"object","required":["source","kind","snippets","truncated"],"properties":{"source":{"const":"official_developer_mcp"},"kind":{"enum":["search","schema","example"]},"language":{"type":"string"},"snippets":{"type":"array","items":{"type":"string"}},"truncated":{"type":"boolean"}},"additionalProperties":false}`

func (runtime Runtime) addDeveloperTools(server *mcp.Server) {
	if runtime.Policy == nil || runtime.Policy.Check([]string{"developer_docs"}, "read") != nil {
		return
	}
	for _, tool := range []struct {
		id, name, kind, description string
		schema                      json.RawMessage
	}{
		{"qianchuan.developer_docs_search", "search_oceanengine_api_docs", "search", "仅在维护本插件、查找官方 API 资料时调用。读取公开接口文档，不发业务请求，也不读取业务 OAuth。", objectSchema(map[string]any{"query": stringSchema()}, "query")},
		{"qianchuan.developer_docs_schema", "get_oceanengine_api_schema", "schema", "读取一个精确官方 /open_api/ 路径的 OpenAPI 文档。检索创建或更新接口的资料不会执行该接口。", objectSchema(map[string]any{"api_path": stringSchema()}, "api_path")},
		{"qianchuan.developer_docs_example", "get_oceanengine_sdk_example", "example", "按用户明确选择的 Go 或 Java SDK 语言读取精确接口示例。须提供 language；不执行示例或业务写入。", objectSchema(map[string]any{"api_path": stringSchema(), "language": enumSchema("go", "java")}, "api_path", "language")},
	} {
		no, yes := false, true
		kind := tool.kind
		handler := func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			started, id := runtime.now(), runtime.requestID()
			var input docs.Request
			input.Kind = kind
			if decodeStrict(r.Params.Arguments, &input) != nil {
				return runtime.failureResult(started, id, r.Params.Name, invalidArgumentFailure()), nil
			}
			if runtime.DeveloperDocs == nil {
				return runtime.failureResult(started, id, r.Params.Name, toolFailure{Code: "DEVELOPER_DOCS_CONFIGURATION_REQUIRED", Message: "可选开发资料尚未配置。", Details: map[string]any{}}), nil
			}
			result, err := runtime.DeveloperDocs.Query(ctx, input)
			if err != nil {
				code := "DEVELOPER_DOCS_CONNECTION_FAILED"
				switch {
				case errors.Is(err, docs.ErrMetadata):
					code = "DEVELOPER_DOCS_CONFIGURATION_REQUIRED"
				case errors.Is(err, docs.ErrSchema):
					code = "DEVELOPER_DOCS_SCHEMA_REVIEW_REQUIRED"
				case errors.Is(err, policy.ErrDenied):
					code = "POLICY_DENIED"
				}
				return runtime.failureResult(started, id, r.Params.Name, toolFailure{Code: code, Message: code, Details: map[string]any{}}), nil
			}
			return runtime.successResult(started, id, r.Params.Name, result), nil
		}
		addCapabilityTool(server, tool.id, &mcp.Tool{Name: tool.name, Description: tool.description, InputSchema: tool.schema, OutputSchema: json.RawMessage(developerOutputSchema), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &no, OpenWorldHint: &yes, IdempotentHint: true}}, runtime.handler(handler))
	}
}
