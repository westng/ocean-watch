package officialmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

// Synthetic contract retains the observed optional integer/string-enum
// conflict. No live scope, response, name, or financial value is a fixture.
const fixturePlanSchema = `{
	"type":"object",
	"properties":{
		"advertiser_id":{"type":"integer","format":"int64"},
		"start_time":{"type":"string","format":"datetime"},
		"end_time":{"type":"string","format":"datetime"},
		"marketing_goal":{"type":"string","enum":["LIVE_PROM_GOODS","VIDEO_PROM_GOODS"]},
		"adlab_scene":{"type":"string","enum":["OVERALL_PROJECT","UNI_PROJECT"]},
		"data_period":{"type":"string","enum":["ALL_DATA","OVER_ALL_DATA","UNI_DATA"]},
		"filtering":{"type":"object","properties":{"status":{"type":"string"},"having_cost":{"type":"string"},"smart_bid_type":{"type":"string"}}},
		"fields":{"type":"array","items":{"type":"string","enum":["stat_cost"]}},
		"order_type":{"type":"string","enum":["ASC","DESC"]},
		"order_field":{"type":"string","enum":["create_time","stat_cost"]},
		"page":{"type":"integer","minimum":1},
		"page_size":{"type":"integer","enum":["10","20","50","100","200"]}
	},
	"required":["advertiser_id","start_time","end_time","marketing_goal"],
	"additionalProperties":false
}`

func TestProbePlanPagesKeepOriginalSchemaAndExactIDs(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		t.Run(map[bool]string{true: "JSON", false: "SSE"}[jsonResponse], func(t *testing.T) {
			probe, control, _ := fixtureProbe(t, jsonResponse, nil, func(server *mcp.Server) {
				server.AddTool(&mcp.Tool{Name: probeListQuery, InputSchema: json.RawMessage(fixturePlanSchema)}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					var arguments map[string]json.RawMessage
					if json.Unmarshal(request.Params.Arguments, &arguments) != nil || string(arguments["advertiser_id"]) != fixtureAdvertiserID {
						t.Error("plan advertiser lost precision")
					}
					if _, supplied := arguments["page_size"]; supplied {
						t.Error("conflicting page_size was sent")
					}
					if string(arguments["marketing_goal"]) != `"VIDEO_PROM_GOODS"` || string(arguments["adlab_scene"]) != `"UNI_PROJECT"` ||
						string(arguments["data_period"]) != `"ALL_DATA"` || string(arguments["fields"]) != `["stat_cost"]` {
						t.Error("fixed plan scene or fields changed")
					}
					page, _ := strconv.Atoi(string(arguments["page"]))
					return fixturePlanResult(page, 30, 10), nil
				})
			})
			ctx := probeTestContext(t)
			digest := fixturePlanDigest(t, probe, ctx)
			for page := int32(1); page <= 2; page++ {
				observation, err := probe.ReadPlanPage(ctx, fixtureAdvertiserID, digest, fixturePlanInput(page))
				if err != nil || !observation.BusinessSucceeded || !observation.HasMore || observation.Returned != 10 ||
					observation.PageSize != 10 || observation.Page != page {
					t.Fatalf("bounded page=%#v err=%v", observation, err)
				}
				items := observation.Payload["data"].(map[string]any)["ad_list"].([]any)
				firstID := items[0].(map[string]any)["ad_info"].(map[string]any)["id"].(json.Number).String()
				if firstID != strconv.FormatInt(9007199254740993+int64(page-1)*10, 10) {
					t.Fatal("plan ID lost precision")
				}
				cost := items[0].(map[string]any)["stats_info"].(map[string]any)["stat_cost"].(json.Number)
				if cost.String() != "12345000.123456789" {
					t.Fatal("selected stat_cost lost precision")
				}
				encoded, err := json.Marshal(observation)
				if err != nil || strings.Contains(string(encoded), firstID) || strings.Contains(string(encoded), "stat_cost") ||
					strings.Contains(string(encoded), "total_num") || strings.Contains(string(encoded), "total_page") {
					t.Fatal("private production fields entered observation serialization")
				}
			}
			budget, _ := requestcontrol.BudgetFrom(ctx)
			if probe.Counts().ToolCall != 2 || budget.Snapshot().Used != 2 || control.inFlight.Load() != 0 || control.serial.Load() != 0 {
				t.Fatal("bounded pages bypassed shared read or command budget")
			}
		})
	}
}

func TestProbePlanInputRejectsBeforeRead(t *testing.T) {
	probe, control, called := fixtureProbe(t, true, nil, func(server *mcp.Server) {
		server.AddTool(&mcp.Tool{Name: probeListQuery, InputSchema: json.RawMessage(fixturePlanSchema)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			t.Error("rejected page reached upstream")
			return fixturePlanResult(1, 0, 0), nil
		})
	})
	ctx := probeTestContext(t)
	digest := fixturePlanDigest(t, probe, ctx)
	for _, input := range []ProbePlanPageParams{
		{StartTime: "invalid", EndTime: "2026-10-31 23:59:59", Page: 1},
		{StartTime: "2026-10-31 00:00:00", EndTime: "2026-11-02 00:00:00", Page: 1},
		{StartTime: "2026-10-31 01:00:00", EndTime: "2026-10-31 00:00:00", Page: 1},
		fixturePlanInput(0), fixturePlanInput(3),
	} {
		if _, err := probe.ReadPlanPage(ctx, fixtureAdvertiserID, digest, input); !errors.Is(err, ErrProbePaging) {
			t.Fatalf("invalid paging accepted: %v", err)
		}
	}
	if _, err := probe.ReadPlanPage(ctx, "0"+fixtureAdvertiserID, digest, fixturePlanInput(1)); !errors.Is(err, ErrProbeScope) {
		t.Fatal("aliased advertiser accepted")
	}
	if _, err := probe.ReadPlanPage(ctx, fixtureAdvertiserID, "unreviewed", fixturePlanInput(1)); !errors.Is(err, ErrProbeSchema) {
		t.Fatal("unreviewed list schema accepted")
	}
	budget, _ := requestcontrol.BudgetFrom(ctx)
	if control.reads.Load() != 0 || called.Load() != 0 || probe.Counts().ToolCall != 0 || budget.Snapshot().Used != 0 {
		t.Fatal("invalid plan input dispatched")
	}
}

func TestProbeConflictingPageSizeCannotBeSilentlyNormalized(t *testing.T) {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(fixturePlanSchema), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"advertiser_id": int64(9007199254740993), "start_time": "2026-10-31 00:00:00", "end_time": "2026-10-31 23:59:59", "marketing_goal": "VIDEO_PROM_GOODS"}
	if err := resolved.Validate(base); err != nil {
		t.Fatalf("omitting optional conflict must remain schema-valid: %v", err)
	}
	for _, size := range []any{int64(10), "10"} {
		base["page_size"] = size
		if err := resolved.Validate(base); err == nil {
			t.Fatalf("conflicting size %T accepted", size)
		}
	}
}

func TestProbePlanPageValidatesReturnedDefaultAndCompleteness(t *testing.T) {
	for name, test := range map[string]struct {
		page, total, count int
		mutate             func(map[string]any)
		valid, more        bool
	}{
		"empty":        {page: 1, total: 0, count: 0, valid: true},
		"last partial": {page: 2, total: 11, count: 1, valid: true},
		"wrong size": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			data["page_info"].(map[string]any)["page_size"] = 20
		}},
		"missing rows": {page: 1, total: 30, count: 1},
		"wrong page": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			data["page_info"].(map[string]any)["page"] = 2
		}},
		"wrong scene": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			data["ad_list"].([]any)[0].(map[string]any)["ad_info"].(map[string]any)["adlab_scene"] = "OVERALL_PROJECT"
		}},
		"duplicate ID": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			items := data["ad_list"].([]any)
			items[1] = items[0]
		}},
		"missing requested cost": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			stats := data["ad_list"].([]any)[0].(map[string]any)["stats_info"].(map[string]any)
			delete(stats, "stat_cost")
		}},
		"null requested cost": {page: 1, total: 30, count: 10, mutate: func(data map[string]any) {
			data["ad_list"].([]any)[0].(map[string]any)["stats_info"].(map[string]any)["stat_cost"] = nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			probe, control, _ := fixtureProbe(t, true, nil, func(server *mcp.Server) {
				server.AddTool(&mcp.Tool{Name: probeListQuery, InputSchema: json.RawMessage(fixturePlanSchema)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					result := fixturePlanResult(test.page, test.total, test.count)
					if test.mutate != nil {
						var envelope map[string]any
						decoder := json.NewDecoder(strings.NewReader(string(result.StructuredContent.(json.RawMessage))))
						decoder.UseNumber()
						if err := decoder.Decode(&envelope); err != nil {
							t.Fatal(err)
						}
						test.mutate(envelope["data"].(map[string]any))
						raw, _ := json.Marshal(envelope)
						result.StructuredContent = json.RawMessage(raw)
					}
					return result, nil
				})
			})
			ctx := probeTestContext(t)
			digest := fixturePlanDigest(t, probe, ctx)
			result, err := probe.ReadPlanPage(ctx, fixtureAdvertiserID, digest, fixturePlanInput(int32(test.page)))
			if test.valid {
				if err != nil || !result.BusinessSucceeded || result.HasMore != test.more {
					t.Fatalf("valid page rejected: %#v %v", result, err)
				}
			} else if !errors.Is(err, ErrProbePaging) || result.BusinessSucceeded || result.Payload != nil {
				t.Fatalf("invalid page accepted: %#v %v", result, err)
			}
			if control.inFlight.Load() != 0 {
				t.Fatal("response rejection leaked read slot")
			}
		})
	}
}

func fixturePlanInput(page int32) ProbePlanPageParams {
	return ProbePlanPageParams{StartTime: "2026-10-31 00:00:00", EndTime: "2026-10-31 23:59:59", Page: page}
}

func fixturePlanDigest(t *testing.T, probe *Probe, ctx context.Context) string {
	t.Helper()
	discovery, err := probe.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range discovery.Tools {
		if tool.Name == probeListQuery {
			return tool.InputSourceDigest
		}
	}
	t.Fatal("fixture list schema absent")
	return ""
}

func fixturePlanResult(page, total, count int) *mcp.CallToolResult {
	items := []any{}
	for item := 0; item < count; item++ {
		items = append(items, map[string]any{
			"ad_info": map[string]any{
				"id":             json.Number(strconv.FormatInt(9007199254740993+int64(page-1)*10+int64(item), 10)),
				"marketing_goal": "VIDEO_PROM_GOODS", "adlab_scene": "UNI_PROJECT", "smart_bid_type": "SMART_BID_CUSTOM",
			},
			"stats_info": map[string]any{"stat_cost": json.Number("12345000.123456789")},
		})
	}
	totalPages := total / 10
	if total%10 != 0 {
		totalPages++
	}
	raw, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{
		"ad_list": items, "page_info": map[string]any{"page": page, "page_size": 10, "total_page": totalPages, "total_num": total},
	}})
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(raw)}
}
