package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
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

func TestToolsExposeStrictContractsAndStructuredResults(t *testing.T) {
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
	if len(listedTools.Tools) != 16 || listedTools.Tools[0].Annotations == nil {
		t.Fatalf("unexpected tools: %#v", listedTools.Tools)
	}
	wantAnnotations := map[string]struct {
		readOnly, idempotent, openWorld bool
	}{
		"list_templates":                     {readOnly: true, idempotent: true},
		"get_template":                       {readOnly: true, idempotent: true},
		"preflight_qianchuan_works":          {readOnly: false, idempotent: false, openWorld: true},
		"get_qianchuan_preflight":            {readOnly: true, idempotent: true},
		"list_managed_accounts":              {readOnly: true, idempotent: true},
		"get_qianchuan_authorization":        {readOnly: true, idempotent: true},
		"search_qianchuan_products":          {readOnly: false, idempotent: false, openWorld: true},
		"list_qianchuan_plans":               {readOnly: false, idempotent: false, openWorld: true},
		"get_qianchuan_plan":                 {readOnly: false, idempotent: false, openWorld: true},
		"report_qianchuan_account":           {readOnly: false, idempotent: false, openWorld: true},
		"report_qianchuan_plans":             {readOnly: false, idempotent: false, openWorld: true},
		"get_marketing_authorization":        {readOnly: true, idempotent: true},
		"search_marketing_videos":            {readOnly: false, idempotent: false, openWorld: true},
		"search_marketing_creator_materials": {readOnly: false, idempotent: false, openWorld: true},
		"report_marketing_materials":         {readOnly: false, idempotent: false, openWorld: true},
		"report_marketing_plans":             {readOnly: false, idempotent: false, openWorld: true},
	}
	for _, tool := range listedTools.Tools {
		annotations := tool.Annotations
		want, ok := wantAnnotations[tool.Name]
		if !ok || annotations.ReadOnlyHint != want.readOnly || annotations.IdempotentHint != want.idempotent ||
			annotations.DestructiveHint == nil || *annotations.DestructiveHint ||
			annotations.OpenWorldHint == nil || *annotations.OpenWorldHint != want.openWorld {
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

func TestQianchuanPreflightToolsUseApplicationServiceAndSanitizeOutput(t *testing.T) {
	const secret = "SECRET_TOKEN_COOKIE_RAW_ERROR"
	service := &preflightServiceStub{
		batchResult: applicationqianchuan.BatchCommandResult{
			BatchResult: applicationqianchuan.BatchResult{
				Mode: "dry_run", Channel: "qianchuan",
				Template: applicationqianchuan.BatchTemplateSummary{
					TemplateID: "qcpt_test", Name: "fixture", AdvertiserID: "2000000000000001",
					ProductIDs: []string{"8000000000000001"},
				},
				Counts: map[string]int{"matched_works": 1},
				Results: []applicationqianchuan.BatchGroupResult{{
					AwemeID: "4000000000000001", CreatorName: "fixture", Status: "would_create",
					ProductIDs: []string{"8000000000000001"}, InputItemIDs: []string{"6000000000000001"},
				}},
				Skipped: []applicationqianchuan.SkippedWork{{
					InputIndex: 1, InputURL: "https://v.douyin.com/" + secret, Reason: "invalid", Message: secret,
				}},
				QueryFailures: []applicationqianchuan.WorkQueryFailure{{AwemeID: "4000000000000002", Message: secret}},
				FailedResults: []applicationqianchuan.BatchGroupResult{{AwemeID: "4000000000000003", Status: "failed", Error: secret}},
				Presentation: domain.NewQianchuanBatchPresentation([]map[string]any{{
					"plan_id": "", "creator_nickname": "fixture", "product_id": "8000000000000001",
					"material_id": "6000000000000001", "material_title": "fixture title",
				}}, []string{"skipped", "query_failures", "failed_results"}),
			},
			Performance: applicationqianchuan.BatchPerformance{
				OwnerHintCache: applicationqianchuan.OwnerHintCachePerformance{Warning: map[string]string{"message": secret}},
			},
			PreflightID: "qianchuan-preflight-20260816t040000-abcdef123456",
			ExpiresAt:   "2026-08-16T04:30:00Z",
		},
		summary: applicationqianchuan.BatchPreflightSummary{
			PreflightID: "qianchuan-preflight-20260816t040000-abcdef123456",
			CreatedAt:   "2026-08-16T04:00:00Z", ExpiresAt: "2026-08-16T04:30:00Z",
			AdvertiserID: "2000000000000001", TemplateID: "qcpt_test", TemplateName: "fixture",
			ProductName: "product", ProductShortName: "short", ProductIDs: []string{"8000000000000001"},
			EligibleWorks: 1, Decisions: []applicationqianchuan.BatchPreflightDecision{{CreatorID: "4000000000000001", Action: "create"}},
			ReadyForSubmit: true,
		},
	}
	logs := new(bytes.Buffer)
	runtime := Runtime{
		QianchuanPreflights: service, LogWriter: logs,
		RequestID: func() string { return "request-preflight" },
	}
	session := connectTestServer(t, runtime)
	defer session.Close()

	preflight, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "preflight_qianchuan_works", Arguments: map[string]any{
			"plan_template": "qcpt_test", "work_urls": []string{"https://v.douyin.com/input/"},
		},
	})
	if err != nil || preflight.IsError {
		t.Fatalf("preflight result=%#v err=%v", preflight, err)
	}
	output := decodeStructured[preflightOutput](t, preflight)
	if service.batchCalls != 1 || service.lastCommand.Submit || service.lastCommand.IncludePayloads ||
		service.lastCommand.Concurrency != applicationqianchuan.DefaultBatchConcurrency ||
		!service.budget.Unbounded ||
		output.PreflightID == "" || output.Presentation.RenderedMarkdown == "" {
		t.Fatalf("preflight contract changed: service=%#v output=%#v", service, output)
	}
	encoded, _ := json.Marshal(output)
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("v.douyin.com")) || bytes.Contains(logs.Bytes(), []byte(secret)) {
		t.Fatalf("preflight output or logs leaked private input: output=%s logs=%s", encoded, logs.Bytes())
	}

	get, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_qianchuan_preflight", Arguments: map[string]any{
			"preflight_id": "qianchuan-preflight-20260816t040000-abcdef123456",
		},
	})
	if err != nil || get.IsError {
		t.Fatalf("get preflight result=%#v err=%v", get, err)
	}
	if service.getCalls != 1 || decodeStructured[getPreflightOutput](t, get).Preflight.Decisions[0].Action != "create" {
		t.Fatalf("get preflight contract changed: service=%#v result=%#v", service, get)
	}
	getOutput := decodeStructured[getPreflightOutput](t, get)
	if getOutput.Preflight.PreflightID != service.summary.PreflightID || len(getOutput.Preflight.ProductIDs) != 1 ||
		getOutput.Preflight.EligibleWorks != 1 || !getOutput.Preflight.ReadyForSubmit {
		t.Fatalf("get preflight presenter changed: %#v", getOutput.Preflight)
	}

	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "preflight_qianchuan_works", Arguments: map[string]any{
			"plan_template": "qcpt_test", "work_urls": []string{"https://v.douyin.com/input/"}, "submit": true,
		},
	})
	if err != nil || !invalid.IsError || decodeStructured[errorEnvelope](t, invalid).Error.Code != "INVALID_ARGUMENT" || service.batchCalls != 1 {
		t.Fatalf("unknown preflight argument crossed service boundary: result=%#v err=%v calls=%d", invalid, err, service.batchCalls)
	}
}

func TestQianchuanErrorsDistinguishPreflightExecutionFromSnapshotLookup(t *testing.T) {
	missing := fmt.Errorf("read local state: %w", os.ErrNotExist)
	if failure := mapQianchuanPreflightError(missing); failure.Code != "CONFIG_UNAVAILABLE" {
		t.Fatalf("preflight os.ErrNotExist = %#v", failure)
	}
	if failure := mapQianchuanSnapshotError(missing); failure.Code != "PREFLIGHT_NOT_FOUND" {
		t.Fatalf("snapshot os.ErrNotExist = %#v", failure)
	}
	invalid := fmt.Errorf("decode: %w", applicationqianchuan.ErrBatchPreflightInvalid)
	if failure := mapQianchuanSnapshotError(invalid); failure.Code != "PREFLIGHT_INVALID" {
		t.Fatalf("invalid snapshot = %#v", failure)
	}
	if failure := mapQianchuanSnapshotError(errors.New("temporary local read failure")); failure.Code != "PREFLIGHT_READ_FAILED" || !failure.Retryable {
		t.Fatalf("snapshot local read failure = %#v", failure)
	}
}

type preflightServiceStub struct {
	batchCalls  int
	getCalls    int
	lastCommand applicationqianchuan.BatchWorksCommand
	batchResult applicationqianchuan.BatchCommandResult
	summary     applicationqianchuan.BatchPreflightSummary
	budget      requestcontrol.BudgetSnapshot
}

func (service *preflightServiceStub) BatchWorks(
	ctx context.Context,
	command applicationqianchuan.BatchWorksCommand,
) (applicationqianchuan.BatchCommandResult, error) {
	service.batchCalls++
	service.lastCommand = command
	if budget, ok := requestcontrol.BudgetFrom(ctx); ok {
		service.budget = budget.Snapshot()
	}
	return service.batchResult, nil
}

func (service *preflightServiceStub) GetBatchPreflight(
	_ context.Context,
	_ string,
) (applicationqianchuan.BatchPreflightSummary, error) {
	service.getCalls++
	return service.summary, nil
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
		"LANG": "zh_CN.UTF-8", "PATH": "/usr/bin:/bin",
		"OCEAN_WATCH_PYTHON": "/fixture/python", "OCEAN_WATCH_F2_DOUYIN_COOKIE": "runtime-cookie",
		"HTTPS_PROXY": "http://127.0.0.1:7890", "CODEX_HOME": "/private/state",
		"OCEAN_WATCH_F2_ENTRYPOINT": "/private/resolve.py",
		"ADS_PLAN_MONITOR_CONFIG":   "/private/config.json", "OCEAN_TOKEN": "secret",
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
	if !cleared || environment["LANG"] != "zh_CN.UTF-8" || environment["PATH"] != "/usr/bin:/bin" ||
		environment["OCEAN_WATCH_PYTHON"] != "/fixture/python" ||
		environment["OCEAN_WATCH_F2_DOUYIN_COOKIE"] != "runtime-cookie" ||
		environment["HTTPS_PROXY"] != "http://127.0.0.1:7890" ||
		environment["OCEAN_WATCH_F2_ENTRYPOINT"] != "" ||
		environment["CODEX_HOME"] != "" || environment["ADS_PLAN_MONITOR_CONFIG"] != "" ||
		environment["OCEAN_TOKEN"] != "" {
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

func connectTestServer(t *testing.T, runtime Runtime) *mcp.ClientSession {
	t.Helper()
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
	return session
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
      "display_name":"marketing-test",
      "bindings":{"channel":"marketing","advertiser_id":"1000000000000001","platform":"抖音","traffic_source":"CID","product_id":"3001","product_name":"示例商品"},
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
