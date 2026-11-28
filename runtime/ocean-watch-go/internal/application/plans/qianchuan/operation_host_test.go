//go:build isolatedhost

package qianchuan_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	opsapp "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	qc "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/mcpserver"
)

func connectOperationHost(t *testing.T, service qc.OperationService) (*mcp.ClientSession, func()) {
	t.Helper()
	runtime := mcpserver.Runtime{Policy: &service.Policy, OperationFactory: func(context.Context, string) (qc.OperationService, func() error, error) {
		return service, func() error { return nil }, nil
	}}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server, err := runtime.NewServer("isolated").Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "isolated-host", Version: "test"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	closeHost := sync.OnceFunc(func() { client.Close(); server.Close() })
	t.Cleanup(closeHost)
	return client, closeHost
}

func hostCall(t *testing.T, client *mcp.ClientSession, name string, args any, wantError string, output any) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(name, err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != (wantError != "") {
		t.Fatalf("%s: unexpected response %s", name, data)
	}
	if wantError != "" {
		var failure struct{ Error struct{ Code string } }
		if err := json.Unmarshal(data, &failure); err != nil || failure.Error.Code != wantError {
			t.Fatalf("%s: want %s, got %s", name, wantError, data)
		}
		return
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatal(name, err)
	}
}

func TestIsolatedOperationHost(t *testing.T) {
	for _, scenario := range []string{"reject", "approve", "reconfirm", "recover"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := qc.NewHostFixture(t)
			client, closeHost := connectOperationHost(t, fixture.HostService())
			var view qc.OperationView
			hostCall(t, client, "prepare_qianchuan_operation", map[string]string{"preflight_id": fixture.PreflightID()}, "", &view)
			ref := view.OperationReference
			if ref.OperationID == "" || !strings.Contains(view.Summary, "计划预算（元）") || view.Status != ops.StatusPreflightReady {
				t.Fatalf("prepare: %+v", view)
			}
			execute := struct {
				qc.OperationReference
				ApprovalID string `json:"approval_id"`
			}{ref, "ap_unissued"}
			if scenario == "reject" {
				fixture.RejectConfirmation()
				hostCall(t, client, "request_operation_confirmation", ref, "OPERATION_NOT_APPROVED", nil)
				hostCall(t, client, "execute_operation", execute, "OPERATION_BLOCKED", nil)
				if fixture.Dispatches() != 0 {
					t.Fatal("rejection dispatched")
				}
				return
			}
			var approved opsapp.ConfirmationResult
			hostCall(t, client, "request_operation_confirmation", ref, "", &approved)
			if approved.ApprovalID == "" || approved.OperationID != ref.OperationID || approved.Version != ref.Version || approved.SnapshotDigest != ref.SnapshotDigest || fixture.Dispatches() != 0 {
				t.Fatal("approval reference or dispatch count changed")
			}
			execute.ApprovalID = approved.ApprovalID
			if scenario == "reconfirm" {
				// A caller that lost the first response uses the same operation
				// reference to request another explicit human decision.
				var replacement opsapp.ConfirmationResult
				hostCall(t, client, "request_operation_confirmation", ref, "", &replacement)
				if replacement.ApprovalID == approved.ApprovalID || replacement.ApprovalID == "" {
					t.Fatal("reconfirmation did not replace the approval reference")
				}
				hostCall(t, client, "execute_operation", execute, "OPERATION_BLOCKED", nil)
				if fixture.Dispatches() != 0 {
					t.Fatal("superseded approval dispatched")
				}
				execute.ApprovalID = replacement.ApprovalID
			}
			if scenario == "recover" {
				fixture.LoseWriteResponse()
				hostCall(t, client, "execute_operation", execute, "OPERATION_RECONCILIATION_REQUIRED", nil)
				closeHost()
				fixture.Reopen(t)
				client, _ = connectOperationHost(t, fixture.HostService())
				hostCall(t, client, "get_operation", ref, "", &view)
				if view.Status != ops.StatusUnknown || fixture.Dispatches() != 1 {
					t.Fatalf("reopened operation: %+v", view)
				}
				hostCall(t, client, "reconcile_operation", ref, "", &view)
			} else {
				hostCall(t, client, "execute_operation", execute, "", &view)
			}
			if view.OperationReference != ref || view.Status != ops.StatusSucceeded || len(view.Items) != 1 || !view.Items[0].Applied {
				t.Fatalf("readback: %+v", view)
			}
			hostCall(t, client, "execute_operation", execute, "OPERATION_BLOCKED", nil)
			if fixture.Dispatches() != 1 {
				t.Fatalf("writes after replay: %d", fixture.Dispatches())
			}
		})
	}
}
