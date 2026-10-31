package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/f2config"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const f2StartSchema = `{"type":"object","additionalProperties":false,"properties":{}}`
const f2SessionSchema = `{"type":"object","additionalProperties":false,"required":["session_id"],"properties":{"session_id":{"type":"string","pattern":"^[a-f0-9]{48}$"}}}`
const f2SuccessSchema = `{"type":"object","additionalProperties":false,"required":["ok","request_id","session"],"properties":{"ok":{"const":true},"request_id":{"type":"string"},"session":{"type":"object","additionalProperties":false,"required":["session_id","status","expires_at","credential_backend","credential_saved","can_resume"],"properties":{"session_id":{"type":"string","pattern":"^[a-f0-9]{48}$"},"status":{"enum":["awaiting_user","completed","failed","cancelled","expired"]},"configuration_url":{"type":"string","pattern":"^http://127\\.0\\.0\\.1:[0-9]+/f2/configure\\?session=[a-f0-9]{48}$"},"expires_at":{"type":"string"},"credential_backend":{"enum":["macos-keychain","linux-secret-service","windows-credential-manager"]},"credential_saved":{"type":"boolean"},"can_resume":{"type":"boolean"},"error_code":{"type":"string"}}}}}`
const f2OutputSchema = objectOneOf + f2SuccessSchema + `,` + errorOutputSchema + `]}`

func (runtime Runtime) addF2ConfigurationTools(server *mcp.Server) {
	for _, spec := range []struct {
		capability, name, title, description, schema string
		readOnly                                     bool
	}{
		{"shared.f2_configuration_start", "start_f2_configuration", "配置 F2", "用户说配置 F2、初始化 F2、设置或更新抖音 Cookie 时使用。启动本机临时配置页并返回 URL，请用户打开页面填写 Cookie。不要在聊天、工具参数、Shell 或文件中接收 Cookie。完成后查询会话并恢复原作品解析/预检。", f2StartSchema, false},
		{"shared.f2_configuration_get", "get_f2_configuration_session", "查询 F2 配置状态", "查询本 Runtime 中 F2 配置会话的安全状态。completed 且 credential_saved=true 后可重跑原解析/预检。会话未找到可能是 Runtime 重启或跨宿主调用，错误详情中的 configuration_status 独立检查系统凭据；configured=true 时可重试原请求，不要据此要求重填 Cookie。保存或回读成功均不证明 Cookie 未过期或作品可访问。failed/cancelled/expired 时停止自动重试。", f2SessionSchema, true},
		{"shared.f2_configuration_cancel", "cancel_f2_configuration_session", "取消 F2 配置", "用户取消 F2 配置时调用，关闭本机配置会话。已经保存的系统凭据会保留。", f2SessionSchema, false},
	} {
		destructive, openWorld := false, false
		addCapabilityTool(server, spec.capability, &mcp.Tool{Name: spec.name, Description: spec.description,
			Annotations: &mcp.ToolAnnotations{Title: spec.title, ReadOnlyHint: spec.readOnly, IdempotentHint: spec.readOnly, DestructiveHint: &destructive, OpenWorldHint: &openWorld},
			InputSchema: json.RawMessage(spec.schema), OutputSchema: json.RawMessage(f2OutputSchema)}, runtime.handler(runtime.f2ConfigurationHandler(spec.name)))
	}
}

func (runtime Runtime) f2ConfigurationHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started, requestID := runtime.now(), runtime.requestID()
		var input struct {
			SessionID string `json:"session_id"`
		}
		var fields map[string]json.RawMessage
		invalid := len(request.Params.Arguments) > 256 || decodeStrict(request.Params.Arguments, &input) != nil || json.Unmarshal(request.Params.Arguments, &fields) != nil || fields == nil
		if name == "start_f2_configuration" {
			invalid = invalid || len(fields) != 0
		} else {
			invalid = invalid || len(fields) != 1 || !authorizationSessionIDPattern.MatchString(input.SessionID)
		}
		if invalid {
			return runtime.failureResult(started, requestID, name, toolFailure{Code: "INVALID_ARGUMENT", Message: "F2 configuration accepts only a session ID; enter Cookie in the local page", Details: map[string]any{}}), nil
		}
		if runtime.F2Configuration == nil {
			return runtime.failureResult(started, requestID, name, toolFailure{Code: "F2_CONFIGURATION_UNAVAILABLE", Message: "F2 configuration is unavailable in this Runtime", Details: map[string]any{}}), nil
		}
		var view f2config.Session
		var err error
		switch name {
		case "start_f2_configuration":
			view, err = runtime.F2Configuration.Start(ctx)
		case "get_f2_configuration_session":
			view, err = runtime.F2Configuration.Get(ctx, input.SessionID)
		case "cancel_f2_configuration_session":
			view, err = runtime.F2Configuration.Cancel(ctx, input.SessionID)
		}
		if err != nil {
			code := "F2_CONFIGURATION_UNAVAILABLE"
			message := "本机 F2 配置不可用，请检查系统凭据库或重新启动配置"
			details := map[string]any{}
			var domainErr *domain.Error
			if errors.As(err, &domainErr) {
				switch domainErr.Code {
				case "f2_configuration_busy", "f2_configuration_session_not_found", "f2_credential_unavailable":
					code = strings.ToUpper(domainErr.Code)
				}
			}
			if code == "F2_CONFIGURATION_SESSION_NOT_FOUND" {
				status := runtime.F2Configuration.Status(ctx)
				message = "当前进程未找到该 F2 配置会话；这不表示 Cookie 保存失败，请查看独立的系统凭据状态"
				details["configuration_status"] = status
				if status.Configured {
					details["next_action"] = "retry_original_request"
				} else {
					details["recovery_tool"] = "start_f2_configuration"
				}
			}
			return runtime.failureResult(started, requestID, name, toolFailure{Code: code, Message: message, Details: details}), nil
		}
		runtime.log(started, requestID, name, "ok", "")
		return resultFor(struct {
			OK        bool             `json:"ok"`
			RequestID string           `json:"request_id"`
			Session   f2config.Session `json:"session"`
		}{true, requestID, view}, false), nil
	}
}
