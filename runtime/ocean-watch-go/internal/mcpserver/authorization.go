package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const authorizationStartSchema = `{"type":"object","additionalProperties":false,"required":["channel"],"properties":{"channel":{"type":"string","enum":["marketing","qianchuan","star_map"]},"advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"authorization_id":{"type":"string","pattern":"^[A-Za-z0-9_-]{1,128}$"},"rebind_existing":{"type":"boolean"}}}`
const authorizationSyncSchema = `{"type":"object","additionalProperties":false,"required":["channel","authorization_id"],"properties":{"channel":{"type":"string","enum":["marketing","qianchuan","star_map"]},"advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"authorization_id":{"type":"string","pattern":"^[A-Za-z0-9_-]{1,128}$"},"rebind_existing":{"type":"boolean"}}}`
const authorizationSessionSchema = `{"type":"object","additionalProperties":false,"required":["channel","session_id"],"properties":{"channel":{"type":"string","enum":["marketing","qianchuan","star_map"]},"session_id":{"type":"string","pattern":"^[a-f0-9]{48}$"}}}`
const authorizationSuccessSchema = `{"type":"object","additionalProperties":false,"required":["ok","request_id","session"],"properties":{"ok":{"const":true},"request_id":{"type":"string"},"session":{"type":"object","additionalProperties":false,"required":["session_id","channel","operation","status","expires_at","authorized_account_count","authorized_advertiser_count","can_resume","sync_required"],"properties":{
"session_id":{"type":"string","pattern":"^[a-f0-9]{48}$"},"channel":{"type":"string","enum":["marketing","qianchuan","star_map"]},"operation":{"enum":["authorize","reauthorize","sync_accounts"]},"status":{"enum":["awaiting_user","awaiting_callback","authorizing","syncing","completed","failed","cancelled","expired"]},"authorization_url":{"type":"string","pattern":"^(https://[^\\s]+|http://127\\.0\\.0\\.1:[0-9]+/oauth/confirm\\?session=[a-f0-9]{48})$"},"expires_at":{"type":"string"},"advertiser_id":{"type":"string"},"authorization_id":{"type":"string"},"authorized_account_count":{"type":"integer","minimum":0},"authorized_advertiser_count":{"type":"integer","minimum":0},"can_resume":{"type":"boolean"},"sync_required":{"type":"boolean"},"error_code":{"type":"string"}}}}}`
const authorizationOutputSchema = objectOneOf + authorizationSuccessSchema + `,` + errorOutputSchema + `]}`

var authorizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var authorizationSessionIDPattern = regexp.MustCompile(`^[a-f0-9]{48}$`)

func (runtime Runtime) addAuthorizationTools(server *mcp.Server) {
	for _, spec := range []struct {
		capability, name, title, description, schema string
		readOnly                                     bool
	}{
		{"shared.authorization_start", "start_authorization", "恢复官方授权", "用户要求授权或查询/预检返回 REAUTHORIZATION_REQUIRED 时使用。必须指定渠道，保留原请求参数。已配置 OAuth App 时直接返回官方授权 URL；请用户打开并完成官方授权，然后查询会话。凭据与账户同步完成后才可恢复原查询/预检。不要要求用户执行 Shell 或粘贴秘密。", authorizationStartSchema, false},
		{"shared.authorization_sync_start", "start_account_sync", "恢复授权账户同步", "用户要求同步账户，或授权会话已保存但 sync_required=true 时使用。返回本机确认页，用户确认后同步精确授权；不会创建投放。完成后重跑原请求。", authorizationSyncSchema, false},
		{"shared.authorization_session_get", "get_authorization_session", "查询授权恢复状态", "查询本 Runtime 的授权恢复会话。等待用户浏览器操作时可稍后查询；completed 且 can_resume=true 后，用原来的模板、链接及账户重跑查询或预检；failed/cancelled/expired 时停止自动重试。", authorizationSessionSchema, true},
		{"shared.authorization_session_cancel", "cancel_authorization_session", "取消授权恢复", "用户取消本次授权恢复时调用。停止等待与后续步骤，不删除已经保存的授权或账户快照，也不能撤销已派发的请求。", authorizationSessionSchema, false},
	} {
		destructive, openWorld := false, !spec.readOnly
		addCapabilityTool(server, spec.capability, &mcp.Tool{Name: spec.name, Description: spec.description,
			Annotations: &mcp.ToolAnnotations{Title: spec.title, ReadOnlyHint: spec.readOnly, IdempotentHint: spec.readOnly, DestructiveHint: &destructive, OpenWorldHint: &openWorld},
			InputSchema: json.RawMessage(spec.schema), OutputSchema: json.RawMessage(authorizationOutputSchema),
		}, runtime.handler(runtime.authorizationHandler(spec.name)))
	}
}

func (runtime Runtime) authorizationHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started, requestID := runtime.now(), runtime.requestID()
		var input struct {
			Channel         string `json:"channel"`
			AdvertiserID    string `json:"advertiser_id"`
			AuthorizationID string `json:"authorization_id"`
			RebindExisting  bool   `json:"rebind_existing"`
			SessionID       string `json:"session_id"`
		}
		var fields map[string]json.RawMessage
		invalid := len(request.Params.Arguments) > 4096 || decodeStrict(request.Params.Arguments, &input) != nil || json.Unmarshal(request.Params.Arguments, &fields) != nil
		for field := range fields {
			allowed := field == "channel"
			switch name {
			case "start_authorization":
				allowed = allowed || field == "advertiser_id" || field == "authorization_id" || field == "rebind_existing"
			case "start_account_sync":
				allowed = allowed || field == "advertiser_id" || field == "authorization_id" || field == "rebind_existing"
			default:
				allowed = allowed || field == "session_id"
			}
			invalid = invalid || !allowed
		}
		invalid = invalid || (input.Channel != "marketing" && input.Channel != "qianchuan" && input.Channel != "star_map")
		if _, exists := fields["advertiser_id"]; exists {
			invalid = invalid || domain.ValidateDecimalID(input.AdvertiserID, "advertiser_id") != nil
		}
		if _, exists := fields["authorization_id"]; exists {
			invalid = invalid || !authorizationIDPattern.MatchString(input.AuthorizationID)
		}
		if name == "start_account_sync" {
			invalid = invalid || !authorizationIDPattern.MatchString(input.AuthorizationID)
		}
		if name == "get_authorization_session" || name == "cancel_authorization_session" {
			invalid = invalid || !authorizationSessionIDPattern.MatchString(input.SessionID)
		}
		if invalid {
			return runtime.failureResult(started, requestID, name, toolFailure{Code: "INVALID_ARGUMENT", Message: "invalid authorization request", Details: map[string]any{}}), nil
		}
		if runtime.AuthorizationRecovery == nil {
			return runtime.failureResult(started, requestID, name, toolFailure{Code: "AUTHORIZATION_RECOVERY_UNAVAILABLE", Message: "authorization recovery is unavailable in this Runtime", Details: map[string]any{}}), nil
		}
		var view auth.RecoverySession
		var err error
		switch name {
		case "start_authorization":
			action := "authorize"
			if input.AuthorizationID != "" {
				action = "reauthorize"
			}
			view, err = runtime.AuthorizationRecovery.Start(ctx, auth.RecoveryRequest{Channel: input.Channel, AdvertiserID: input.AdvertiserID, AuthorizationID: input.AuthorizationID, Operation: action, RebindExisting: input.RebindExisting})
		case "start_account_sync":
			view, err = runtime.AuthorizationRecovery.Start(ctx, auth.RecoveryRequest{Channel: input.Channel, AdvertiserID: input.AdvertiserID, AuthorizationID: input.AuthorizationID, Operation: "sync_accounts", RebindExisting: input.RebindExisting})
		case "get_authorization_session":
			view, err = runtime.AuthorizationRecovery.Get(ctx, input.Channel, input.SessionID)
		case "cancel_authorization_session":
			view, err = runtime.AuthorizationRecovery.Cancel(ctx, input.Channel, input.SessionID)
		}
		if err != nil {
			code := "AUTHORIZATION_RECOVERY_UNAVAILABLE"
			var domainErr *domain.Error
			if errors.As(err, &domainErr) {
				switch domainErr.Code {
				case "authorization_busy":
					code = "AUTHORIZATION_BUSY"
				case "authorization_session_not_found":
					code = "AUTHORIZATION_SESSION_NOT_FOUND"
				case "invalid_arguments":
					code = "INVALID_ARGUMENT"
				case "authorization_ambiguous":
					code = "AUTHORIZATION_AMBIGUOUS"
				case "authorization_not_found":
					code = "AUTHORIZATION_NOT_FOUND"
				case "authorization_identity_mismatch":
					code = "AUTHORIZATION_IDENTITY_MISMATCH"
				case "oauth_app_missing":
					code = "OAUTH_APP_MISSING"
				}
			}
			return runtime.failureResult(started, requestID, name, toolFailure{Code: code, Message: "local authorization interaction could not start; inspect the session or start a new one", Details: map[string]any{"channel": input.Channel}}), nil
		}
		runtime.log(started, requestID, name, "ok", "")
		diagnostics := view.FailureDiagnostics()
		issueCount := view.DiscoveryIssueCount
		// Optional protocol metadata preserves the existing structured tool
		// contract, allowing an already-open chat to adopt compatible fixes.
		view.FailureStage, view.FailureReason, view.OfficialRequestID = "", "", ""
		view.OfficialCode, view.HTTPStatus = 0, 0
		view.FailureEndpoint, view.FailureValidation = "", ""
		view.DiscoveryIssueCount = 0
		result := resultFor(struct {
			OK        bool                 `json:"ok"`
			RequestID string               `json:"request_id"`
			Session   auth.RecoverySession `json:"session"`
		}{true, requestID, view}, false)
		if len(diagnostics) > 0 {
			result.Meta = mcp.Meta{"ocean_watch.authorization_failure": diagnostics}
			encoded, _ := json.Marshal(diagnostics)
			result.Content = append(result.Content, &mcp.TextContent{Text: "授权失败诊断：" + string(encoded)})
		}
		if issueCount > 0 && issueCount <= 1_000_000 {
			if result.Meta == nil {
				result.Meta = mcp.Meta{}
			}
			result.Meta["ocean_watch.account_discovery"] = map[string]any{"issue_count": issueCount, "complete": false}
			result.Content = append(result.Content, &mcp.TextContent{Text: "账户同步已保存已校验范围，但存在未展开的账户角色；不能据此声称全部角色已覆盖。"})
		}
		return result, nil
	}
}

func authorizationFailure(channel string, syncRequired bool) toolFailure {
	code, tool, message := "REAUTHORIZATION_REQUIRED", "start_authorization", "official authorization must be renewed in the browser"
	if syncRequired {
		code, tool, message = "ACCOUNT_SYNC_REQUIRED", "start_account_sync", "saved authorization requires account synchronization"
	}
	return toolFailure{Code: code, Message: message, Details: map[string]any{"channel": channel, "recovery_tool": tool, "resume_original_request": true}}
}
