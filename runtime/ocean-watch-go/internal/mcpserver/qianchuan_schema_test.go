package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestDecodePreflightItemsPreservesMixedRowsAndRejectsV1Fields(t *testing.T) {
	input, failure := decodePreflightInput(json.RawMessage(`{
		"plan_template":"qcpt_test",
		"items":[
			{"work_url":"https://v.douyin.com/a/","plan_type":"随手po","business":"刘岛"},
			{"work_url":"https://v.douyin.com/b/","plan_type":"真人口播营销","business":"刘岛"}
		]
	}`))
	if failure != nil || len(input.Items) != 2 {
		t.Fatalf("structured preflight items were rejected: input=%#v failure=%#v", input, failure)
	}
	items := preflightBatchItems(input)
	if items[0].InputIndex != 0 || items[1].InputIndex != 1 || items[0].PlanType != "随手po" || items[1].PlanType != "真人口播营销" {
		t.Fatalf("structured rows were not preserved: %#v", items)
	}
	for name, raw := range map[string]string{
		"legacy top-level fields": `{"plan_template":"qcpt_test","work_urls":["https://v.douyin.com/a/"]}`,
		"unknown top-level field": `{"plan_template":"qcpt_test","items":[{"work_url":"https://v.douyin.com/a/"}],"submit":true}`,
		"unknown item field":      `{"plan_template":"qcpt_test","items":[{"work_url":"https://v.douyin.com/a/","unexpected":"x"}]}`,
	} {
		if _, failure := decodePreflightInput(json.RawMessage(raw)); failure == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestPreflightPerformanceSchemaHasOnlyComponentTimings(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(preflightSuccessSchema), &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	performance := properties["performance"].(map[string]any)
	performanceProperties := performance["properties"].(map[string]any)
	for _, legacy := range []string{
		"link_resolution_seconds", "credential_resolution_seconds", "material_resolution_seconds",
		"plan_reconciliation_seconds", "total_seconds",
	} {
		if _, exists := performanceProperties[legacy]; exists {
			t.Fatalf("legacy cumulative performance field is still exposed: %s", legacy)
		}
	}
	for _, current := range []string{"owner_hint_cache", "link_metadata", "stages", "requests"} {
		if _, exists := performanceProperties[current]; !exists {
			t.Fatalf("current performance field is missing: %s", current)
		}
	}
}
