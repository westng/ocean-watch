package templates

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildQianchuanProductTemplateIsPureAndDeduplicatesProducts(t *testing.T) {
	config := map[string]any{}
	normalized, sources, err := QianchuanProductCreateSources(config)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildQianchuanProductTemplate(sources[0].Template, QianchuanProductCreateInput{
		TemplateID:       "qcpt_123456789abc",
		TemplateName:     "用户千川模板甲",
		AdvertiserID:     "2000000000000101",
		ProductName:      "千川商品甲官方全称",
		ProductShortName: "千川商品甲",
		ProductIDs:       "8000000000000101/8000000000000102/8000000000000101",
		PlanNameTemplate: "{creator_name}-{product_name}-{date}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidate["bindings"].(map[string]any)["product_ids"], []any{
		"8000000000000101", "8000000000000102",
	}) {
		t.Fatalf("product IDs were not normalized: %#v", candidate)
	}
	if len(normalized[qianchuanProductTemplatesKey].(map[string]any)) != 0 {
		t.Fatal("candidate construction mutated normalized config")
	}
	if candidate["display_name"] != "用户千川模板甲" ||
		candidate["plan_name_template"] != "{creator_name}-{product_name}-{date}" {
		t.Fatalf("custom template and plan names were not preserved: %#v", candidate)
	}
	bindings := candidate["bindings"].(map[string]any)
	if bindings["product_name"] != "千川商品甲官方全称" || bindings["product_short_name"] != "千川商品甲" {
		t.Fatalf("product names were not preserved independently: %#v", bindings)
	}
	updated, err := ApplyQianchuanProductTemplate(normalized, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated[qianchuanProductTemplatesKey].(map[string]any)) != 1 || len(normalized[qianchuanProductTemplatesKey].(map[string]any)) != 0 {
		t.Fatal("candidate application did not isolate the source config")
	}
}

func TestBuildQianchuanProductTemplateRejectsUnknownPlanNamePlaceholder(t *testing.T) {
	_, err := BuildQianchuanProductTemplate(nil, QianchuanProductCreateInput{
		TemplateID: "qcpt_123456789abc", TemplateName: "用户千川模板甲",
		AdvertiserID: "1", ProductName: "商品", ProductIDs: "2",
		PlanNameTemplate: "{product_name}-{unknown_value}",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported placeholders") {
		t.Fatalf("unknown plan-name placeholder was accepted: %v", err)
	}
}

func TestApplyQianchuanProductTemplateRejectsDuplicateDisplayName(t *testing.T) {
	normalized, sources, err := QianchuanProductCreateSources(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := BuildQianchuanProductTemplate(sources[0].Template, QianchuanProductCreateInput{
		TemplateID: "qcpt_123456789abc", TemplateName: "用户千川模板甲", AdvertiserID: "1", ProductName: "商品", ProductIDs: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyQianchuanProductTemplate(normalized, first)
	if err != nil {
		t.Fatal(err)
	}
	second := cloneMap(first)
	second["template_id"] = "qcpt_abcdef123456"
	if _, err := ApplyQianchuanProductTemplate(updated, second); err == nil {
		t.Fatal("duplicate display name was accepted")
	}
}

func TestBuildQianchuanLiveTemplateEnforcesBidFields(t *testing.T) {
	_, sources, err := QianchuanLiveCreateSources(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	source := cloneMap(sources[0].Template)
	delivery := cloneMap(source["delivery_setting"].(map[string]any))
	delivery["smart_bid_type"] = "SMART_BID_CUSTOM"
	delivery["roi2_goal"] = "2.2"
	delete(delivery, "daily_delivery_time")
	source["delivery_setting"] = delivery
	candidate, err := BuildQianchuanLiveTemplate(source, QianchuanLiveCreateInput{
		TemplateID: "qclt_123456789abc", AdvertiserID: "1", CreatorName: "直播账号", AwemeID: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	validatedDelivery := candidate["delivery_setting"].(map[string]any)
	if validatedDelivery["smart_bid_type"] != "SMART_BID_CUSTOM" || validatedDelivery["roi2_goal"] != DecimalFloat64(2.2) {
		t.Fatalf("unexpected live delivery: %#v", validatedDelivery)
	}
	if _, exists := validatedDelivery["daily_delivery_time"]; exists {
		t.Fatal("custom bid retained conservative-only delivery duration")
	}
}
