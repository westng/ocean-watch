package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
)

type versionedStore struct {
	config   map[string]any
	revision string
	reads    int
}

func (store *versionedStore) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, nil
}

func (store *versionedStore) ReadWithRevision(context.Context) (map[string]any, string, error) {
	store.reads++
	return store.config, store.revision, nil
}

func TestToolsExposeStrictReadOnlyContractsAndStructuredResults(t *testing.T) {
	store := &versionedStore{revision: "revision-1", config: mcpTemplateConfig()}
	logs := new(bytes.Buffer)
	runtime := Runtime{
		Query: applicationQuery(store), LogWriter: logs,
		Now:       func() time.Time { return time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC) },
		RequestID: func() string { return "request-1" },
	}
	server := runtime.NewServer("test")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	listedTools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listedTools.Tools) != 2 || listedTools.Tools[0].Annotations == nil {
		t.Fatalf("unexpected tools: %#v", listedTools.Tools)
	}
	for _, tool := range listedTools.Tools {
		annotations := tool.Annotations
		if !annotations.ReadOnlyHint || !annotations.IdempotentHint || annotations.DestructiveHint == nil || *annotations.DestructiveHint || annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
			t.Fatalf("unsafe annotations for %s: %#v", tool.Name, annotations)
		}
	}

	listResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_templates", Arguments: map[string]any{"channel": "all", "limit": 1},
	})
	if err != nil || listResult.IsError {
		t.Fatalf("list result = %#v, %v", listResult, err)
	}
	listed := decodeStructured[listOutput](t, listResult)
	if listed.RequestID != "request-1" || listed.StateVersion != "revision-1" || listed.TotalCount != 2 || len(listed.Items) != 1 || listed.NextCursor == nil {
		t.Fatalf("unexpected list output: %#v", listed)
	}
	if listed.Items[0].Channel != "marketing" || listed.Items[0].Status != nil || listed.Items[0].AdvertiserID == nil || *listed.Items[0].AdvertiserID != "1000000000000001" {
		t.Fatalf("unexpected marketing item: %#v", listed.Items[0])
	}

	getResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_template", Arguments: map[string]any{"channel": "qianchuan", "template_id": "qcpt_test"},
	})
	if err != nil || getResult.IsError {
		t.Fatalf("get result = %#v, %v", getResult, err)
	}
	detail := decodeStructured[getOutput](t, getResult).Template
	if detail.TemplateID != "qcpt_test" || detail.ProductIDs[0] != "8000000000000001" || detail.ProjectNameTemplate == nil || *detail.ProjectNameTemplate != "{product_name}-{creator_name}-{datetime}" {
		t.Fatalf("unexpected template detail: %#v", detail)
	}
	if bytes.Contains(logs.Bytes(), []byte("1000000000000001")) || bytes.Contains(logs.Bytes(), []byte("8000000000000001")) {
		t.Fatalf("tool logs leaked business IDs: %s", logs.String())
	}
}

func TestToolsRejectUnknownArgumentsAndChangedCursor(t *testing.T) {
	store := &versionedStore{revision: "revision-1", config: mcpTemplateConfig()}
	runtime := Runtime{Query: applicationQuery(store), RequestID: func() string { return "request-2" }}
	server := runtime.NewServer("test")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_template", Arguments: map[string]any{"channel": "qianchuan", "template_id": "qcpt_test", "config": "/tmp/config.json"},
	})
	if err != nil || !invalid.IsError {
		t.Fatalf("invalid result = %#v, %v", invalid, err)
	}
	if failure := decodeStructured[errorEnvelope](t, invalid); failure.Error.Code != "INVALID_ARGUMENT" || len(failure.Error.Details) != 0 {
		t.Fatalf("unexpected invalid-argument result: %#v", failure)
	}

	cursor, err := encodeCursor(cursorPayload{Version: 1, Channel: "all", StateVersion: "old", Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_templates", Arguments: map[string]any{"channel": "all", "limit": 1, "cursor": cursor},
	})
	if err != nil || !changed.IsError {
		t.Fatalf("changed result = %#v, %v", changed, err)
	}
	if failure := decodeStructured[errorEnvelope](t, changed); failure.Error.Code != "STATE_CHANGED" || !failure.Error.Retryable {
		t.Fatalf("unexpected state-changed result: %#v", failure)
	}
}

func TestRetainManagedEnvironmentDropsUnrelatedValues(t *testing.T) {
	environment := map[string]string{
		"LANG": "zh_CN.UTF-8", "CODEX_HOME": "/private/state", "OCEAN_TOKEN": "secret",
	}
	cleared := false
	err := retainManagedEnvironment(
		func(name string) string { return environment[name] },
		func() {
			cleared = true
			environment = map[string]string{}
		},
		func(name, value string) error {
			environment[name] = value
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !cleared || len(environment) != 1 || environment["LANG"] != "zh_CN.UTF-8" {
		t.Fatalf("unexpected retained environment: %#v", environment)
	}
}

func TestManagedConfigErrorsUseStableCodes(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{err: fmt.Errorf("read: %w", filesystem.ErrManagedConfigInvalid), code: "CONFIG_INVALID"},
		{err: fmt.Errorf("read: %w", os.ErrNotExist), code: "CONFIG_UNAVAILABLE"},
		{err: fmt.Errorf("read: %w", os.ErrPermission), code: "LOCAL_ACCESS_DENIED"},
	}
	for _, test := range tests {
		failure := mapQueryError(test.err)
		if failure.Code != test.code || len(failure.Details) != 0 {
			t.Fatalf("mapQueryError(%v) = %#v, want %s", test.err, failure, test.code)
		}
	}
}

func applicationQuery(store *versionedStore) applicationtemplates.Query {
	return applicationtemplates.Query{Store: store, VersionedStore: store}
}

func decodeStructured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func mcpTemplateConfig() map[string]any {
	const payload = `{
  "plan_template_schema_version": 6,
  "default_plan_template": {"defaults":{"daily_budget":300,"roi_goal":1.5,"product_info":{"product_image_type":"DPA","product_image_fields":[]}}},
  "plan_templates": {
    "marketing-test": {
      "bindings":{"advertiser_id":"1000000000000001","platform":"抖音","traffic_source":"CID","product_id":"3001","product_name":"示例商品"},
      "material_strategy":{"source_type":"ACCOUNT_UPLOAD","selection_mode":"MANUAL","max_materials_per_unit":5},
      "copy_materials":{"titles":[]},"overrides":{}
    }
  },
  "qianchuan_product_template_schema_version": 8,
  "qianchuan_product_templates": {
    "qcpt_test": {
      "template_id":"qcpt_test","display_name":"千川测试模板","template_type":"QIANCHUAN_PRODUCT_ALL_DOMAIN","status":"active",
      "bindings":{"channel":"qianchuan","advertiser_id":"2000000000000001","product_name":"示例商品","product_short_name":"示例","product_ids":["8000000000000001"]},
      "plan_name_template":"{product_name}-{creator_name}-{datetime}",
      "delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.7,"qcpx_mode":"QCPX_MODE_ON","budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW","deep_external_action":"AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"},
      "material_strategy":{"source_type":"CREATOR_RUNTIME_QUERY","persist_material_ids":false}
    }
  },
  "qianchuan_live_template_schema_version": 1,
  "qianchuan_live_templates": {}
}`
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.UseNumber()
	var config map[string]any
	if err := decoder.Decode(&config); err != nil {
		panic(err)
	}
	return config
}
