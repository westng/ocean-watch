package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

func (runtime Runtime) addOperationTools(server *mcp.Server) {
	for _, tool := range []struct{ action, name, description string }{
		{"prepare", "prepare_qianchuan_operation", "将精确千川预检转换为不可变执行预览；返回用户应审阅的摘要，不执行投放。"},
		{"get", "get_operation", "读取精确操作的摘要与持久派发/对账状态，不访问官方接口。"},
		{"confirm", "request_operation_confirmation", "为精确操作打开独立本机确认界面；仅用户在该界面批准后返回审批引用，聊天或自动许可不算批准。"},
		{"execute", "execute_operation", "消费精确版本的审批引用并执行千川新建/追加；只使用已保存载荷，失败或未知后先查询和对账。"},
		{"reconcile", "reconcile_operation", "恢复精确操作的官方结果事实；不会重新派发，未派发项目需重新预检和确认。"},
	} {
		action := tool.action
		props := map[string]any{"operation_id": map[string]any{"type": "string", "pattern": "^op_[A-Za-z0-9_-]{1,128}$"}, "version": map[string]any{"type": "integer", "minimum": 1}, "snapshot_digest": map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"}}
		required := []string{"operation_id", "version", "snapshot_digest"}
		if action == "prepare" {
			props = map[string]any{"preflight_id": map[string]any{"type": "string", "pattern": qianchuanPreflightIDPattern.String()}}
			required = []string{"preflight_id"}
		}
		if action == "execute" {
			props["approval_id"] = map[string]any{"type": "string", "pattern": "^ap_[A-Za-z0-9_.:-]+$"}
			required = append(required, "approval_id")
		}
		output := operationViewSchema()
		if action == "confirm" {
			output = objectSchema(map[string]any{"operation_id": stringSchema(), "version": map[string]any{"type": "integer"}, "snapshot_digest": stringSchema(), "approval_id": stringSchema(), "expires_at": stringSchema()}, "operation_id", "version", "snapshot_digest", "approval_id", "expires_at")
		}
		handler := func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			started, id := runtime.now(), runtime.requestID()
			var input struct {
				qianchuan.OperationReference
				PreflightID string `json:"preflight_id"`
				ApprovalID  string `json:"approval_id"`
			}
			if decodeStrict(r.Params.Arguments, &input) != nil {
				return runtime.failureResult(started, id, r.Params.Name, invalidArgumentFailure()), nil
			}
			if runtime.OperationFactory == nil {
				return runtime.failureResult(started, id, r.Params.Name, toolFailure{Code: "OPERATION_UNAVAILABLE", Message: "操作服务不可用。", Details: map[string]any{}}), nil
			}
			service, close, err := runtime.OperationFactory(ctx, action)
			var result any
			if err == nil {
				defer close()
				switch action {
				case "prepare":
					result, err = service.Prepare(ctx, input.PreflightID)
				case "get":
					result, err = service.Get(ctx, input.OperationReference)
				case "confirm":
					result, err = service.Confirm(ctx, input.OperationReference)
				case "execute":
					result, err = service.Execute(ctx, input.OperationReference, input.ApprovalID)
				case "reconcile":
					result, err = service.Reconcile(ctx, input.OperationReference)
				}
			}
			if err != nil {
				code := "OPERATION_BLOCKED"
				switch {
				case errors.Is(err, qianchuan.ErrOperationChanged):
					code = "OPERATION_CHANGED"
				case errors.Is(err, ops.ErrDispatchUnknown):
					code = "OPERATION_RECONCILIATION_REQUIRED"
				case errors.Is(err, approval.ErrNotApproved):
					code = "OPERATION_NOT_APPROVED"
				case errors.Is(err, approval.ErrVerifierUnavailable):
					code = "APPROVAL_UNAVAILABLE"
				case errors.Is(err, sqlite.ErrIncompatible):
					return runtime.failureResult(started, id, r.Params.Name, operationStateFailure()), nil
				}
				return runtime.failureResult(started, id, r.Params.Name, toolFailure{Code: code, Message: "操作未完成，请读取已保存状态；未知结果先对账，不自动重新提交。", Details: map[string]any{}}), nil
			}
			return runtime.successResult(started, id, r.Params.Name, result), nil
		}
		no, yes := false, true
		annotations := &mcp.ToolAnnotations{ReadOnlyHint: action == "get", DestructiveHint: &no, OpenWorldHint: &yes, IdempotentHint: action == "get"}
		if action == "get" || action == "confirm" {
			annotations.OpenWorldHint = &no
		}
		if action == "execute" {
			annotations.DestructiveHint = &yes
		}
		addCapabilityTool(server, "qianchuan.operation_"+action, &mcp.Tool{Name: tool.name, Description: tool.description, InputSchema: objectSchema(props, required...), OutputSchema: output, Annotations: annotations}, runtime.handler(withBoundedRequestBudget(handler)))
	}
}

func operationStateFailure() toolFailure {
	return toolFailure{Code: "STATE_INCOMPATIBLE", Message: "本地状态格式不兼容；先用 operations upgrade-state 查看迁移预览，确认备份与兼容性后再显式升级。", Details: map[string]any{}}
}

func operationViewSchema() json.RawMessage {
	item := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"item_id", "group_id", "action", "target_id", "dispatch_state", "applied"}, "properties": map[string]any{"item_id": stringSchema(), "group_id": stringSchema(), "action": enumSchema("create", "append"), "target_id": stringSchema(), "dispatch_state": enumSchema("", "intent", "not_sent", "acknowledged", "unknown"), "applied": map[string]any{"type": "boolean"}}}
	return objectSchema(map[string]any{"operation_id": stringSchema(), "version": map[string]any{"type": "integer"}, "snapshot_digest": stringSchema(), "status": stringSchema(), "advertiser_id": stringSchema(), "expires_at": stringSchema(), "summary": stringSchema(), "items": map[string]any{"type": "array", "items": item}}, "operation_id", "version", "snapshot_digest", "status", "advertiser_id", "expires_at", "summary", "items")
}
