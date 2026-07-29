package templates

import (
	"reflect"
	"testing"
)

func TestBuildMarketingTemplateDefaultCandidate(t *testing.T) {
	config := marketingCreateFixture()
	normalized, sources, err := MarketingCreateSources(config, "ACCOUNT_UPLOAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "default_plan_template" {
		t.Fatalf("unexpected sources: %#v", sources)
	}
	name, candidate, err := BuildMarketingTemplate(normalized, "", nil, MarketingCreateInput{
		AdvertiserID: "1000000000000101", Platform: "示例平台", TrafficSource: "CID",
		ProductID: "9000000000000101", ProductName: "向导商品甲",
		SellingPoints: "向导商品值得推荐", DailyBudget: 600, ROIGoal: 1.8,
		Gender: "GENDER_FEMALE", Ages: []any{"AGE_BETWEEN_24_30", "AGE_BETWEEN_31_40", "AGE_BETWEEN_41_49"},
		MaterialSourceType: "ACCOUNT_UPLOAD", SelectionMode: "MANUAL", MaxMaterials: 5,
		Titles: []any{"这是一条向导测试标题"}, PlanSource: "向导来源",
		LandingPageURL: "https://landing.example.test/a", OpenURL: "exampleapp://product/a",
		TrackURL: "https://track.example.test/impression", ActionTrackURL: "https://track.example.test/click",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "巨量营销-1000000000000101-向导商品甲-9000000000000101-混剪素材" {
		t.Fatalf("unexpected name: %s", name)
	}
	productInfo := mapOrEmpty(mapOrEmpty(mapOrEmpty(candidate["overrides"])["defaults"])["product_info"])
	if productInfo["product_image_type"] != "DPA" || !reflect.DeepEqual(productInfo["product_image_fields"], []any{"images_url"}) {
		t.Fatalf("DPA fields were not preserved: %#v", productInfo)
	}
	if !reflect.DeepEqual(productInfo["selling_points"], []any{"向导商品值得推荐"}) {
		t.Fatalf("selling points were not normalized: %#v", productInfo)
	}
	readiness := MarketingCandidateReadiness(normalized, name, candidate)
	if readiness["ready_for_plan_creation"] != true || !reflect.DeepEqual(readiness["runtime_missing_fields"], []any{"materials.video_ids", "api.access_token"}) {
		t.Fatalf("unexpected readiness: %#v", readiness)
	}
	if !reflect.DeepEqual(config, marketingCreateFixture()) {
		t.Fatal("candidate construction mutated input config")
	}
	updated, err := ApplyMarketingTemplate(normalized, name, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := mapOrEmpty(normalized["plan_templates"])[name]; exists {
		t.Fatal("candidate application mutated source config")
	}
	if _, exists := mapOrEmpty(updated["plan_templates"])[name]; !exists {
		t.Fatal("candidate application did not add template")
	}
}

func TestMarketingCandidateKeepsOpaqueCIDLinksUnchanged(t *testing.T) {
	config := marketingCreateFixture()
	normalized, _, err := MarketingCreateSources(config, "ACCOUNT_UPLOAD")
	if err != nil {
		t.Fatal(err)
	}
	name, candidate, err := BuildMarketingTemplate(normalized, "", nil, MarketingCreateInput{
		AdvertiserID: "1000000000000101", Platform: "京东", TrafficSource: "CID",
		ProductID: "9000000000000101", ProductName: "向导商品甲",
		SellingPoints: "向导商品值得推荐", DailyBudget: 600, ROIGoal: 1.8,
		Gender: "GENDER_FEMALE", Ages: []any{"AGE_BETWEEN_24_30"},
		MaterialSourceType: "ACCOUNT_UPLOAD", SelectionMode: "MANUAL", MaxMaterials: 5,
		Titles: []any{"这是一条向导测试标题"}, PlanSource: "向导来源",
		LandingPageURL: "custom+cid://opaque/path?repeat=1&repeat=2&TODO=保留&projectid=__PROJECT_ID__",
		OpenURL:        "another-scheme://任意路径?x=1&x=2&raw=%7bVALUE%7d",
		TrackURL:       "track-anywhere://path?kind=click&unknown=1&unknown=2",
		ActionTrackURL: "https://example.com/not-inferred?kind=impress&TODO=value",
	})
	if err != nil {
		t.Fatal(err)
	}
	readiness := MarketingCandidateReadiness(normalized, name, candidate)
	if readiness["ready_for_plan_creation"] != true {
		t.Fatalf("opaque CID links were interpreted: %#v", readiness)
	}
	overrides := mapOrEmpty(candidate["overrides"])
	if !reflect.DeepEqual(overrides["links"], map[string]any{
		"landing_page_url": "custom+cid://opaque/path?repeat=1&repeat=2&TODO=保留&projectid=__PROJECT_ID__",
		"open_url":         "another-scheme://任意路径?x=1&x=2&raw=%7bVALUE%7d",
	}) || !reflect.DeepEqual(overrides["tracking_urls"], map[string]any{
		"track_url":        []any{"track-anywhere://path?kind=click&unknown=1&unknown=2"},
		"action_track_url": []any{"https://example.com/not-inferred?kind=impress&TODO=value"},
	}) {
		t.Fatalf("opaque CID links changed in candidate: %#v", overrides)
	}
}

func TestMarketingClonePoliciesClearOwnedFields(t *testing.T) {
	config := marketingCreateFixture()
	sourceName := "巨量营销-1-旧商品-2-混剪素材"
	source := map[string]any{
		"display_name":      sourceName,
		"bindings":          map[string]any{"channel": "marketing", "advertiser_id": "1", "platform": "平台", "traffic_source": "CID", "product_id": "2", "product_name": "旧商品"},
		"copy_materials":    map[string]any{"titles": []any{"这是来源商品文案"}},
		"material_strategy": map[string]any{"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5},
		"overrides": map[string]any{
			"materials":    map[string]any{"video_ids": []any{"video"}},
			"defaults":     map[string]any{"product_info": map[string]any{"product_image_type": "CUSTOM"}},
			"resolved_ids": map[string]any{"event_asset_ids": []any{1}, "product_image_ids": []any{"image"}, "city_ids": []any{11}},
			"links":        map[string]any{"landing_page_url": "https://old.test"},
		},
	}
	_, candidate, err := BuildMarketingTemplate(config, sourceName, source, MarketingCreateInput{
		AdvertiserID: "3", Platform: "平台", TrafficSource: "CID", ProductID: "4", ProductName: "新商品",
		SellingPoints: "新商品值得推荐", DailyBudget: 300, ROIGoal: 1.5, Gender: "NONE", Ages: []any{},
		MaterialSourceType: "ACCOUNT_UPLOAD", SelectionMode: "MANUAL", MaxMaterials: 5,
		Titles: []any{"这是新的商品文案"}, PlanSource: "来源", LandingPageURL: "https://new.test",
		OpenURL: "app://new", TrackURL: "https://new.test/i", ActionTrackURL: "https://new.test/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	createdFrom := mapOrEmpty(candidate["created_from"])
	if createdFrom["policy"] != "cross_advertiser_new_product" {
		t.Fatalf("unexpected policy: %#v", createdFrom)
	}
	cleared := stringValues(createdFrom["cleared_fields"])
	for _, field := range []string{"materials.video_ids", "resolved_ids.event_asset_ids", "resolved_ids.product_image_ids", "links.landing_page_url", "defaults.product_info"} {
		if !containsString(cleared, field) {
			t.Fatalf("missing cleared field %s: %#v", field, cleared)
		}
	}
	overrides := mapOrEmpty(candidate["overrides"])
	if !reflect.DeepEqual(mapOrEmpty(overrides["resolved_ids"])["city_ids"], []any{11}) {
		t.Fatalf("shared city IDs were cleared: %#v", overrides)
	}
	productInfo := mapOrEmpty(mapOrEmpty(overrides["defaults"])["product_info"])
	if productInfo["product_image_type"] != "DPA" || !reflect.DeepEqual(productInfo["product_image_fields"], []any{"images_url"}) {
		t.Fatalf("new product did not reset to DPA: %#v", productInfo)
	}
}

func TestMarketingSameProductKeepsCopyAndProductAssets(t *testing.T) {
	config := marketingCreateFixture()
	sourceName := "巨量营销-1-商品-2-混剪素材"
	source := map[string]any{
		"display_name":      sourceName,
		"bindings":          map[string]any{"channel": "marketing", "advertiser_id": "1", "platform": "平台", "traffic_source": "CID", "product_id": "2", "product_name": "商品"},
		"copy_materials":    map[string]any{"titles": []any{"这是来源商品文案"}},
		"material_strategy": map[string]any{"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5},
		"overrides": map[string]any{
			"defaults":     map[string]any{"product_info": map[string]any{"product_name_type": "CUSTOM", "product_selling_point_type": "CUSTOM", "product_image_type": "CUSTOM", "selling_points": []any{"来源商品值得推荐"}}},
			"resolved_ids": map[string]any{"product_image_ids": []any{"image"}},
		},
	}
	config["plan_templates"] = map[string]any{sourceName: source}
	normalized, sources, err := MarketingCreateSources(config, "")
	if err != nil {
		t.Fatal(err)
	}
	_, candidate, err := BuildMarketingTemplate(normalized, sourceName, sources[1].Template, MarketingCreateInput{
		AdvertiserID: "1", Platform: "平台", TrafficSource: "CID", ProductID: "2", ProductName: "商品",
		SellingPoints: "来源商品值得推荐", DailyBudget: 300, ROIGoal: 1.5, Gender: "NONE", Ages: []any{},
		MaterialSourceType: "ACCOUNT_UPLOAD", SelectionMode: "MANUAL", MaxMaterials: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapOrEmpty(candidate["created_from"])["policy"] != "same_advertiser_same_product" {
		t.Fatalf("unexpected policy: %#v", candidate["created_from"])
	}
	if !reflect.DeepEqual(mapOrEmpty(candidate["copy_materials"])["titles"], []any{"这是来源商品文案"}) {
		t.Fatalf("copy was not inherited: %#v", candidate)
	}
	if !reflect.DeepEqual(mapOrEmpty(mapOrEmpty(candidate["overrides"])["resolved_ids"])["product_image_ids"], []any{"image"}) {
		t.Fatalf("same-product assets were cleared: %#v", candidate)
	}
}

func TestMarketingTextAndDiffContracts(t *testing.T) {
	if _, err := normalizeMarketingRequiredTitles([]any{"太短"}); err == nil {
		t.Fatal("short title was accepted")
	}
	points, err := normalizeMarketingSellingPoints("商品卖点推荐，商品卖点推荐")
	if err != nil || !reflect.DeepEqual(points, []any{"商品卖点推荐"}) {
		t.Fatalf("unexpected selling points: %#v err=%v", points, err)
	}
	if _, err := normalizeMarketingSellingPoints("abc中文"); err == nil {
		t.Fatal("official half-position length was not enforced")
	}
	changes := MarketingTemplateDiff(
		map[string]any{"bindings": map[string]any{"advertiser_id": "1"}},
		map[string]any{"bindings": map[string]any{"advertiser_id": "2"}, "copy_materials": map[string]any{"titles": []any{"标题"}}},
	)
	if !reflect.DeepEqual(changes, []any{
		map[string]any{"field": "bindings.advertiser_id", "before": "1", "after": "2"},
		map[string]any{"field": "copy_materials.titles", "before": nil, "after": []any{"标题"}},
	}) {
		t.Fatalf("unexpected flat diff: %#v", changes)
	}
}

func TestApplyMarketingTemplateRejectsDuplicateAndIncomplete(t *testing.T) {
	config := marketingCreateFixture()
	name := "巨量营销-1-商品-2-混剪素材"
	candidate := map[string]any{
		"display_name":      name,
		"bindings":          map[string]any{"channel": "marketing", "advertiser_id": "1", "platform": "平台", "traffic_source": "CID", "product_id": "2", "product_name": "商品"},
		"copy_materials":    map[string]any{"titles": []any{}},
		"material_strategy": map[string]any{"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5},
		"overrides":         map[string]any{},
	}
	if _, err := ApplyMarketingTemplate(config, name, candidate); err == nil {
		t.Fatal("incomplete candidate was accepted")
	}
	config["plan_templates"] = map[string]any{name: candidate}
	if _, err := ApplyMarketingTemplate(config, name, candidate); err == nil {
		t.Fatal("duplicate candidate was accepted")
	}
}

func marketingCreateFixture() map[string]any {
	return map[string]any{
		"config_schema_version":        2,
		"channels":                     map[string]any{"marketing": map[string]any{"api": map[string]any{"base_url": "https://api.oceanengine.com/open_api"}}},
		"account":                      map[string]any{"channel": "marketing", "advertiser_id": "REPLACE_WITH_ADVERTISER_ID"},
		"plan_template_schema_version": 5,
		"plan_templates":               map[string]any{},
		"default_plan_template": map[string]any{
			"defaults": map[string]any{
				"operation": "ENABLE", "project_name_template": "{material_date}_{product_name}_roi_详情页",
				"promotion_name_template": "自动投放单元_{product_name}_{material_date}_{suffix}",
				"daily_budget":            300, "roi_goal": 1.5, "landing_type": "SHOP", "marketing_goal": "VIDEO_AND_IMAGE",
				"delivery_mode": "PROCEDURAL", "ad_type": "ALL", "gender": "NONE", "ages": []any{},
				"location_type": "CURRENT", "district": "REGION", "region_version": "2.3.2",
				"schedule_type": "SCHEDULE_FROM_NOW", "budget_mode": "BUDGET_MODE_DAY", "pricing": "PRICING_OCPM",
				"video_image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
				"product_info":     map[string]any{"product_name_type": "CUSTOM", "product_image_type": "DPA", "product_image_fields": []any{"images_url"}, "product_selling_point_type": "CUSTOM"},
			},
			"resolved_ids": map[string]any{"city_ids": []any{11, 12}},
		},
		"future_root": "preserved",
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
