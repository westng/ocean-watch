package templates

import (
	"context"
	"reflect"
	"testing"
)

type lifecycleStoreSpy struct {
	config   map[string]any
	reads    int
	casCalls int
	revision string
	written  map[string]any
}

const applicationMarketingTemplateName = "巨量营销-1-product-2-混剪素材"

func (store *lifecycleStoreSpy) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, nil
}

func (store *lifecycleStoreSpy) ReadWithRevision(context.Context) (map[string]any, string, error) {
	store.reads++
	return store.config, store.revision, nil
}

func (store *lifecycleStoreSpy) CompareAndSwap(_ context.Context, revision string, updated map[string]any) error {
	store.casCalls++
	store.revision = revision
	store.written = updated
	return nil
}

func TestValidateIsReadOnlyAndPreservesPath(t *testing.T) {
	store := &lifecycleStoreSpy{config: map[string]any{}}
	result, err := (Lifecycle{Store: store, Path: "/synthetic/config.json"}).Validate(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || store.casCalls != 0 {
		t.Fatalf("unexpected store calls: reads=%d cas=%d", store.reads, store.casCalls)
	}
	if result["config"] != "/synthetic/config.json" || result["ok"] != false {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestDeleteDryRunDoesNotWrite(t *testing.T) {
	config := applicationTemplateFixture()
	store := &lifecycleStoreSpy{config: config, revision: "r1"}
	result, err := (Lifecycle{Store: store, Path: "/synthetic/config.json"}).Delete(
		context.Background(), "marketing", applicationMarketingTemplateName, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || store.casCalls != 0 || result["changed"] != false {
		t.Fatalf("dry-run wrote state: store=%#v result=%#v", store, result)
	}
	if _, exists := config["plan_templates"].(map[string]any)[applicationMarketingTemplateName]; !exists {
		t.Fatal("dry-run mutated source config")
	}
}

func TestDeleteSubmitUsesCapturedRevision(t *testing.T) {
	store := &lifecycleStoreSpy{config: applicationTemplateFixture(), revision: "r1"}
	result, err := (Lifecycle{Store: store, Path: "/synthetic/config.json"}).Delete(
		context.Background(), "marketing", applicationMarketingTemplateName, false, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.casCalls != 1 || store.revision != "r1" || result["changed"] != true {
		t.Fatalf("submit did not use CAS: store=%#v result=%#v", store, result)
	}
	if _, exists := store.written["plan_templates"].(map[string]any)[applicationMarketingTemplateName]; exists {
		t.Fatal("submitted config retained deleted template")
	}
}

func TestCurrentQianchuanMigrationSkipsCAS(t *testing.T) {
	config := map[string]any{
		"qianchuan_product_template_schema_version": 4,
		"default_qianchuan_product_template": map[string]any{
			"template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN", "business_usable": false,
			"bindings":          map[string]any{"channel": "qianchuan", "advertiser_id": "REPLACE_WITH_ADVERTISER_ID", "product_name": "REPLACE_WITH_PRODUCT_NAME", "product_ids": []any{}},
			"delivery_setting":  map[string]any{"smart_bid_type": "SMART_BID_CUSTOM", "roi2_goal": 1.7, "qcpx_mode": "QCPX_MODE_ON", "budget": 5000, "video_schedule_type": "SCHEDULE_FROM_NOW", "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"},
			"material_strategy": map[string]any{"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false},
		},
		"qianchuan_product_templates": map[string]any{},
	}
	store := &lifecycleStoreSpy{config: config, revision: "r1"}
	result, err := (Lifecycle{Store: store}).MigrateQianchuanProduct(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.casCalls != 0 || result["migrated"] != false {
		t.Fatalf("idempotent migration wrote state: store=%#v result=%#v", store, result)
	}
}

func TestSetCopyWritesNormalizedResult(t *testing.T) {
	store := &lifecycleStoreSpy{config: applicationTemplateFixture(), revision: "r1"}
	result, err := (Lifecycle{Store: store, Path: "/synthetic/config.json"}).SetCopy(
		context.Background(), applicationMarketingTemplateName, []string{" 新标题示例一 ", "新标题示例一", "新标题示例二"}, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.casCalls != 1 || result["command"] != "set-copy" || result["changed"] != true {
		t.Fatalf("unexpected set-copy result: store=%#v result=%#v", store, result)
	}
	templates := result["templates"].([]any)
	copyMaterials := templates[0].(map[string]any)["copy_materials"].(map[string]any)
	if !reflect.DeepEqual(copyMaterials["titles"], []any{"新标题示例一", "新标题示例二"}) {
		t.Fatalf("titles were not normalized: %#v", copyMaterials)
	}
}

func applicationTemplateFixture() map[string]any {
	return map[string]any{
		"plan_template_schema_version": 5,
		"default_plan_template": map[string]any{
			"defaults": map[string]any{
				"daily_budget": 300, "roi_goal": 1.5, "gender": "NONE", "ages": []any{},
				"product_info": map[string]any{"product_image_type": "DPA", "product_image_fields": []any{"images_url"}},
			},
			"resolved_ids": map[string]any{"city_ids": []any{}, "city_names": []any{}},
		},
		"plan_templates": map[string]any{
			applicationMarketingTemplateName: map[string]any{
				"display_name":      applicationMarketingTemplateName,
				"bindings":          map[string]any{"channel": "marketing", "advertiser_id": "1", "platform": "p", "traffic_source": "t", "product_id": "2", "product_name": "product"},
				"material_strategy": map[string]any{"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5},
				"copy_materials":    map[string]any{"titles": []any{"示例标题一"}},
				"overrides":         map[string]any{},
			},
		},
	}
}
