package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func TestRoutedPreflightToolAcceptsPerRowMappingsAndPreservesChildSnapshots(t *testing.T) {
	const private = "PRIVATE_TOKEN_RAW_URL_ERROR"
	template := applicationqianchuan.BatchTemplateSummary{TemplateID: "qcpt_fixture", Name: "fixture", AdvertiserID: "1001", ProductIDs: []string{"5001"}}
	child := applicationqianchuan.BatchCommandResult{BatchResult: applicationqianchuan.BatchResult{
		Mode: "dry_run", Channel: "qianchuan", Template: template, Counts: map[string]int{"matched_works": 1},
		Presentation: domain.NewQianchuanBatchPresentation(nil, []string{"skipped", "query_failures", "failed_results"}),
	}, PreflightID: "qianchuan-preflight-20261030t010000-abcdef123456", ExpiresAt: "2026-10-30T01:30:00Z", Warnings: []string{private}}
	service := &preflightServiceStub{batchResult: applicationqianchuan.BatchCommandResult{
		Routed: true, BatchResult: applicationqianchuan.BatchResult{Mode: "dry_run", Channel: "qianchuan", Counts: map[string]int{"routed_batches": 2, "pending_works": 1}},
		Batches: []applicationqianchuan.RoutedBatchResult{
			{InputIndexes: []int{0}, Template: template, Status: "preflight_complete", Result: &child},
			{InputIndexes: []int{1}, Template: template, Status: "preflight_failed", FailureStage: "authorization", Cause: domain.NewError("reauthorization_required", private, 1, nil)},
		},
		Pending: []applicationqianchuan.RoutingPending{{InputIndex: 2, Reason: "routing_ambiguous", Candidates: []applicationqianchuan.RoutingCandidate{{TemplateID: "qcpt_fixture", AdvertiserID: "1001", ProductIDs: []string{"5001"}}}}},
	}}
	session := connectTestServer(t, Runtime{QianchuanPreflights: service, RequestID: func() string { return "routing-test" }})
	defer session.Close()
	response, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "preflight_qianchuan_works", Arguments: map[string]any{
		"items": []map[string]any{
			{"work_url": "https://v.douyin.com/first/", "product_name": "血橙", "advertiser_id": "1001", "creator_douyin_id": "creator-visible", "creator_name": "用于命名的昵称"},
			{"work_url": "https://v.douyin.com/second/", "product_id": "5002", "plan_template": "qcpt_second"},
			{"work_url": "https://v.douyin.com/third/"},
		},
	}})
	if err != nil || response.IsError {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	output := decodeStructured[routedPreflightOutput](t, response)
	if !output.Routed || len(output.Batches) != 2 || output.Batches[0].Preflight.PreflightID != child.PreflightID || output.Batches[1].Failure == nil || len(output.Pending) != 1 {
		t.Fatalf("routed output lost child results: %#v", output)
	}
	if failure := output.Batches[1].Failure.Error; failure.Code != "REAUTHORIZATION_REQUIRED" || failure.Details["recovery_tool"] != "start_authorization" {
		t.Fatalf("child recovery instruction was lost: %#v", failure)
	}
	if service.lastCommand.PlanTemplate != "" || service.lastCommand.Items[0].ProductName != "血橙" || service.lastCommand.Items[1].PlanTemplate != "qcpt_second" || service.lastCommand.Submit {
		t.Fatalf("mapping hints were not passed intact: %#v", service.lastCommand)
	}
	encoded, _ := json.Marshal(output)
	if bytes.Contains(encoded, []byte(private)) || bytes.Contains(encoded, []byte("v.douyin.com")) {
		t.Fatalf("private data escaped projection: %s", encoded)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(preflightOutputSchema), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(value); err != nil {
		t.Fatalf("routed response violates published schema: %v", err)
	}
}

func TestMixedPreflightRejectsInvalidIdentifiersAndWriteFields(t *testing.T) {
	for _, raw := range []string{
		`{"items":[{"work_url":"https://v.douyin.com/a/","product_id":"0"}]}`,
		`{"items":[{"work_url":"https://v.douyin.com/a/","advertiser_id":"not-an-id"}]}`,
		`{"items":[{"work_url":"https://v.douyin.com/a/","creator_uid":"1.5"}]}`,
		`{"items":[{"work_url":"https://v.douyin.com/a/","submit":true}]}`,
	} {
		if _, failure := decodePreflightInput(json.RawMessage(raw)); failure == nil {
			t.Fatalf("invalid mixed input accepted: %s", raw)
		}
	}
}
