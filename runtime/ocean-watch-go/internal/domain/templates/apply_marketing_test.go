package templates

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyMarketingPlanTemplateMatchesBusinessBindings(t *testing.T) {
	config := marketingApplyFixture()
	original := cloneMap(config)
	effective, err := ApplyMarketingPlanTemplate(config, MarketingPlanTemplateSelection{
		Name: "巨量营销-1001-商品甲-9001-混剪素材", AdvertiserID: "1001", Channel: "marketing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, original) {
		t.Fatal("template application mutated the source config")
	}
	if mapOrEmpty(effective["account"])["advertiser_id"] != "1001" ||
		mapOrEmpty(effective["defaults"])["product_id"] != "9001" {
		t.Fatalf("business bindings were not applied: %#v", effective)
	}
	if !reflect.DeepEqual(effective["titles"], []any{"这是第一条测试标题", "这是第二条测试标题"}) {
		t.Fatalf("copy materials were not applied: %#v", effective["titles"])
	}
	if !reflect.DeepEqual(mapOrEmpty(effective["resolved_ids"])["city_ids"], []any{11, 12}) ||
		mapOrEmpty(effective["resolved_ids"])["unique_product_id"] != "9001" {
		t.Fatalf("default and business overrides were not deep-merged: %#v", effective["resolved_ids"])
	}
	selected := mapOrEmpty(effective["_selected_plan_template"])
	if selected["name"] != "巨量营销-1001-商品甲-9001-混剪素材" || selected["legacy"] != false {
		t.Fatalf("selected template metadata changed: %#v", selected)
	}
}

func TestApplyMarketingPlanTemplateRejectsUnsafeSelection(t *testing.T) {
	config := marketingApplyFixture()
	name := "巨量营销-1001-商品甲-9001-混剪素材"
	tests := []struct {
		name      string
		mutate    func(map[string]any)
		selection MarketingPlanTemplateSelection
		contains  string
	}{
		{
			name: "explicit template required", selection: MarketingPlanTemplateSelection{},
			contains: "explicit plan template",
		},
		{
			name: "advertiser binding", selection: MarketingPlanTemplateSelection{Name: name, AdvertiserID: "2002", Channel: "marketing"},
			contains: "bound to advertiser 1001",
		},
		{
			name: "channel binding", selection: MarketingPlanTemplateSelection{Name: name, AdvertiserID: "1001", Channel: "qianchuan"},
			contains: "bound to channel marketing",
		},
		{
			name: "product binding", selection: MarketingPlanTemplateSelection{Name: name, AdvertiserID: "1001", Channel: "marketing"},
			mutate: func(value map[string]any) {
				template := mapOrEmpty(mapOrEmpty(value["plan_templates"])[name])
				resolved := mapOrEmpty(mapOrEmpty(template["overrides"])["resolved_ids"])
				resolved["unique_product_id"] = "different-product"
			},
			contains: "binds product 9001",
		},
		{
			name: "fixed material IDs", selection: MarketingPlanTemplateSelection{Name: name, AdvertiserID: "1001", Channel: "marketing"},
			mutate: func(value map[string]any) {
				template := mapOrEmpty(mapOrEmpty(value["plan_templates"])[name])
				template["overrides"] = map[string]any{"materials": map[string]any{"video_ids": []any{}}}
			},
			contains: "cannot store runtime material IDs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneMap(config)
			if test.mutate != nil {
				test.mutate(candidate)
			}
			_, err := ApplyMarketingPlanTemplate(candidate, test.selection)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("got %v, want error containing %q", err, test.contains)
			}
		})
	}
}

func TestApplyMarketingPlanTemplateCanReturnCreationBaseExplicitly(t *testing.T) {
	effective, err := ApplyMarketingPlanTemplate(marketingApplyFixture(), MarketingPlanTemplateSelection{
		AllowNoTemplate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective["_selected_plan_template"] != nil {
		t.Fatalf("creation base was presented as a business template: %#v", effective)
	}
	if !reflect.DeepEqual(effective["titles"], []any{}) || !reflect.DeepEqual(effective["materials"], map[string]any{}) {
		t.Fatalf("creation base was not applied: %#v", effective)
	}
}

func marketingApplyFixture() map[string]any {
	name := "巨量营销-1001-商品甲-9001-混剪素材"
	return map[string]any{
		"plan_template_schema_version": 6,
		"default_channel":              "marketing",
		"account":                      map[string]any{"channel": "marketing", "advertiser_id": "1001"},
		"default_plan_template": map[string]any{
			"defaults": map[string]any{
				"daily_budget": 300,
				"product_info": map[string]any{"product_image_type": "DPA", "product_image_fields": []any{"images_url"}},
			},
			"materials":    map[string]any{},
			"resolved_ids": map[string]any{"city_ids": []any{11, 12}},
			"links":        map[string]any{}, "tracking_urls": map[string]any{}, "titles": []any{},
		},
		"plan_templates": map[string]any{
			name: map[string]any{
				"display_name": name,
				"bindings": map[string]any{
					"channel": "marketing", "advertiser_id": "1001", "platform": "平台",
					"traffic_source": "CID", "product_id": "9001", "product_name": "商品甲",
				},
				"copy_materials": map[string]any{"titles": []any{"这是第一条测试标题", "这是第二条测试标题"}},
				"material_strategy": map[string]any{
					"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5,
				},
				"overrides": map[string]any{"resolved_ids": map[string]any{"unique_product_id": "9001"}},
			},
		},
	}
}
