package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"strings"
	"testing"
)

func TestOperationToolsRejectForgedApprovalAndPayloadBeforeOpeningState(t *testing.T) {
	instance, _ := policy.New(policy.Config{Mode: "execute", Toolsets: []string{"core", "qianchuan"}})
	calls := 0
	runtime := Runtime{Policy: &instance, OperationFactory: func(context.Context, string) (qianchuan.OperationService, func() error, error) {
		calls++
		return qianchuan.OperationService{}, func() error { return nil }, nil
	}}
	server := runtime.NewServer("fixture")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	connection, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	base := map[string]any{"operation_id": "op_fixture", "version": 1, "snapshot_digest": "sha256:" + strings.Repeat("a", 64)}
	for _, field := range []string{"approved", "provider", "nonce", "payload", "submit"} {
		input := map[string]any{}
		for k, v := range base {
			input[k] = v
		}
		input[field] = true
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "request_operation_confirmation", Arguments: input})
		if err == nil && !result.IsError {
			t.Fatalf("accepted forged field %s", field)
		}
	}
	if calls != 0 {
		t.Fatal("invalid approval opened state")
	}
	plan := policy.Default()
	runtime.Policy = &plan
	arguments, _ := json.Marshal(map[string]any{"operation_id": "op_fixture", "version": 1, "snapshot_digest": base["snapshot_digest"], "approval_id": "ap_fake"})
	result, err := runtime.handler(func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Fatal("plan-mode execution reached handler")
		return nil, nil
	})(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "execute_operation", Arguments: arguments}})
	if err != nil || !result.IsError {
		t.Fatal("plan mode did not reject execute")
	}
}

func TestIncompatibleStateDoesNotTriggerPreflightRetry(t *testing.T) {
	for _, failure := range []toolFailure{mapQianchuanPreflightError(sqlite.ErrIncompatible), mapQianchuanSnapshotError(sqlite.ErrIncompatible)} {
		if failure.Code != "STATE_INCOMPATIBLE" || failure.Retryable || !strings.Contains(failure.Message, "upgrade-state") {
			t.Fatalf("incompatible state treated as retriable business failure: %+v", failure)
		}
	}
}
