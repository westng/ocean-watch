package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/bootstrap"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/mcpserver"
)

func TestSavedOperationHasEquivalentCoreCLIAndMCPViewsWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "ads-plan-monitor", "state")
	store, err := sqlite.Initialize(ctx, sqlite.Options{Path: filepath.Join(stateRoot, "ocean-watch.db")})
	if err != nil {
		t.Fatal(err)
	}
	writePayload := json.RawMessage(`{"advertiser_id":1001,"ad_id":2001}`)
	writeDigest, _ := ops.Digest(writePayload)
	item := ops.ConfirmationItem{ItemID: "item_fixture", GroupID: "group_fixture", Action: "append", TargetID: "2001", PayloadDigest: writeDigest, ExpectedDigest: writeDigest, Changes: []ops.ConfirmationChange{{Field: "素材数量", Before: "0", After: "1"}}}
	payload, _ := json.Marshal(map[string]any{"contract": "qianchuan_batch_execution_v1", "preflight_id": "qianchuan-preflight-20261107t010000-abcdef123456", "preflight": map[string]any{"schema_version": 3, "advertiser_id": "1001"}, "authorization_id": "fixture", "items": []ops.ConfirmationItem{item}, "writes": []map[string]any{{"item_id": item.ItemID, "group_id": item.GroupID, "action": item.Action, "target_id": item.TargetID, "plan_name": "fixture", "item_ids": []string{"3001"}, "payload": writePayload}}})
	digest, _ := ops.Digest(payload)
	now := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	snapshot := ops.Snapshot{OperationID: "op_transport_fixture", Version: 1, Kind: "qianchuan_batch_execution_v1", Channel: "qianchuan", SubjectID: "1001", RuntimeVersion: "fixture", SchemaVersion: 1, InputDigest: digest, SnapshotDigest: digest, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Payload: payload}
	if err = store.SavePreflight(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	instance := policy.Default()
	factory := func(ctx context.Context, action string) (qianchuan.OperationService, func() error, error) {
		return bootstrap.NewQianchuanOperations(ctx, bootstrap.QianchuanOptions{Policy: &instance, StateRoot: stateRoot, RuntimeVersion: "fixture"}, action)
	}
	service, close, err := factory(ctx, "get")
	if err != nil {
		t.Fatal(err)
	}
	ref := qianchuan.OperationReference{OperationID: snapshot.OperationID, Version: 1, SnapshotDigest: digest}
	direct, err := service.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	_ = close()
	var stdout bytes.Buffer
	runner := cli.Runner{Routes: application.DefaultRouteManifest(), Policy: &instance, RuntimeVersion: "fixture", Stdout: &stdout, UserHome: t.TempDir(), Cwd: root, Getenv: func(key string) string {
		if key == "OCEAN_WATCH_HOME" || key == "CODEX_HOME" {
			return root
		}
		return ""
	}}
	if code := runner.Execute(ctx, []string{"operations", "get", "--operation-id", ref.OperationID, "--version", strconv.Itoa(ref.Version), "--snapshot-digest", ref.SnapshotDigest}); code != 0 {
		t.Fatalf("CLI failed: %s", stdout.String())
	}
	var fromCLI qianchuan.OperationView
	if err = json.Unmarshal(stdout.Bytes(), &fromCLI); err != nil {
		t.Fatal(err)
	}
	server := (mcpserver.Runtime{Policy: &instance, OperationFactory: factory}).NewServer("fixture")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	connection, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "fixture"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_operation", Arguments: ref})
	if err != nil || result.IsError {
		t.Fatalf("MCP failed: %+v %v", result, err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var fromMCP qianchuan.OperationView
	if err = json.Unmarshal(raw, &fromMCP); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(direct, fromCLI) || !reflect.DeepEqual(direct, fromMCP) {
		t.Fatalf("transport operation views differ: core=%+v cli=%+v mcp=%+v", direct, fromCLI, fromMCP)
	}
}
