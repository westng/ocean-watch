package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	reports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func validateToolOutput(t *testing.T, tool *mcp.Tool, result *mcp.CallToolResult) {
	t.Helper()
	raw, err := json.Marshal(tool.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(decodeStructured[map[string]any](t, result)); err != nil {
		t.Fatalf("%s output violates advertised schema: %v", tool.Name, err)
	}
}

func TestReportSchemaDiscoversTopicsWithoutCredentials(t *testing.T) {
	session := connectTestServer(t, Runtime{})
	defer session.Close()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_qianchuan_report_schema", Arguments: map[string]any{"advertiser_id": "1001"}})
	if err != nil || result.IsError {
		t.Fatalf("topic discovery failed: %v %v", result, err)
	}
	output := decodeStructured[struct {
		Source string
		Result struct {
			Topics []string `json:"available_topics"`
		}
	}](t, result)
	if output.Source != "local_contract" || !slices.Contains(output.Result.Topics, "OVERALL_ROI_PRODUCT_AWEME") || slices.Contains(output.Result.Topics, "AUTHOR") {
		t.Fatalf("invalid topic discovery: %+v", output)
	}
}

func TestReportFailuresPreserveRecoveryAndSafeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{domain.NewError("reauthorization_required", "private", 1, nil), "REAUTHORIZATION_REQUIRED"},
		{&oceanengine.EnvelopeError{Code: 40000, HTTPStatus: 200, Message: "private"}, "OFFICIAL_API_ERROR"},
		{domain.NewError("report_fields_unsupported", "private", 2, map[string]any{"unsupported_fields": []string{"author_id"}}), "REPORT_FIELDS_UNSUPPORTED"},
	} {
		failure := mapOfficialError(tc.err)
		if failure.Code != tc.code || failure.Message == "private" {
			t.Fatalf("lost report failure: %+v", failure)
		}
		if tc.code == "OFFICIAL_API_ERROR" && failure.Details["api_code"] != "40000" {
			t.Fatal("official error code was lost")
		}
	}
}

func TestEveryToolAcceptsStructuredFailure(t *testing.T) {
	instance, err := policy.New(policy.Config{Mode: "execute", Toolsets: []string{"core", "marketing", "qianchuan", "reports", "developer_docs"}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{Policy: &instance}
	session := connectTestServer(t, runtime)
	defer session.Close()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			result := runtime.failureResult(runtime.now(), "fixture-request", tool.Name, invalidArgumentFailure())
			validateToolOutput(t, tool, result)
		})
	}
}

type taskReportsFixture struct{ failure error }

func (f taskReportsFixture) QianchuanSchema(_ context.Context, q reports.QianchuanSchemaQuery) (reports.QianchuanSchemaResult, error) {
	return reports.QianchuanSchemaResult{AdvertiserID: q.AdvertiserID, Mode: "fixture"}, f.failure
}

func (f taskReportsFixture) QianchuanCustom(_ context.Context, q reports.QianchuanCustomQuery) (reports.QianchuanCustomResult, error) {
	return reports.QianchuanCustomResult{AdvertiserID: q.AdvertiserID, DataTopic: q.DataTopic, Mode: "fixture"}, f.failure
}

func TestUnifiedReportToolsSuccessAndFailureContracts(t *testing.T) {
	for _, failure := range []error{nil, errors.New("fixture upstream failure")} {
		runtime := Runtime{UnifiedReports: taskReportsFixture{failure: failure}}
		session := connectTestServer(t, runtime)
		t.Cleanup(func() { session.Close() })
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string]map[string]any{
			"get_qianchuan_report_schema": {"advertiser_id": "1001", "topics": []string{"SITE_PROMOTION_PRODUCT_AD"}},
			"report_qianchuan_custom":     {"advertiser_id": "1001", "start_date": "2026-11-27", "end_date": "2026-11-27", "data_topic": "SITE_PROMOTION_PRODUCT_AD", "dimensions": []string{"ad_id"}, "metrics": []string{"stat_cost"}},
		}
		for _, tool := range listed.Tools {
			args, ok := inputs[tool.Name]
			if !ok {
				continue
			}
			t.Run(tool.Name, func(t *testing.T) {
				result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
				if err != nil || result.IsError != (failure != nil) {
					t.Fatalf("call failed: %v, %v", result, err)
				}
				validateToolOutput(t, tool, result)
			})
		}
	}
}
