package templates

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestListAllTemplatesPreservesChannelContracts(t *testing.T) {
	result, err := List(templateTestConfig(t), "all", false)
	if err != nil {
		t.Fatal(err)
	}
	summary := result["summary"].(map[string]any)
	if summary["business_template_count"] != 3 || summary["default_skeleton_count"] != 3 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	channels := result["channels"].(map[string]any)
	marketing := channels["marketing"].(map[string]any)
	marketingRows := marketing["templates"].([]any)
	marketingRow := marketingRows[0].(map[string]any)
	if marketingRow["template_type"] != "混剪素材" || marketingRow["copy_title_count"] != 2 {
		t.Fatalf("unexpected Marketing row: %#v", marketingRow)
	}
	if _, exists := marketingRow["copy_materials"]; exists {
		t.Fatal("compact Marketing row leaked copy_materials")
	}
	qianchuan := channels["qianchuan"].(map[string]any)
	qianchuanRows := qianchuan["templates"].([]any)
	product := qianchuanRows[0].(map[string]any)
	live := qianchuanRows[1].(map[string]any)
	if product["template_kind"] != "product" || product["product_count"] != 2 {
		t.Fatalf("unexpected product row: %#v", product)
	}
	if live["template_kind"] != "live" || live["aweme_id"] != "9988776655" {
		t.Fatalf("unexpected live row: %#v", live)
	}
	payload, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"daily_budget":5000.0`)) {
		t.Fatalf("live budget lost Python float JSON semantics: %s", payload)
	}
}

func TestListDetailsAndShowPreserveFullTemplates(t *testing.T) {
	config := templateTestConfig(t)
	listed, err := List(config, "all", true)
	if err != nil {
		t.Fatal(err)
	}
	channels := listed["channels"].(map[string]any)
	marketing := channels["marketing"].(map[string]any)["templates"].([]any)[0].(map[string]any)
	if _, exists := marketing["copy_materials"]; !exists || marketing["channel"] != "marketing" {
		t.Fatalf("Marketing details are incomplete: %#v", marketing)
	}
	qianchuan := channels["qianchuan"].(map[string]any)["templates"].([]any)
	if qianchuan[0].(map[string]any)["delivery_setting"] == nil || qianchuan[1].(map[string]any)["creative_setting"] == nil {
		t.Fatalf("Qianchuan details are incomplete: %#v", qianchuan)
	}
	shown, err := Show(config, "qianchuan", "qclt_example")
	if err != nil {
		t.Fatal(err)
	}
	if shown["ready_for_plan_creation"] != true || shown["template"].(map[string]any)["template_kind"] != "live" {
		t.Fatalf("unexpected shown template: %#v", shown)
	}
}

func TestShowQianchuanPreservesProductErrorWhenBothKindsMiss(t *testing.T) {
	_, err := Show(templateTestConfig(t), "qianchuan", "missing")
	if err == nil || err.Error() != "Qianchuan product template not found" {
		t.Fatalf("got %v, want product-template not-found error", err)
	}
	if !reflect.DeepEqual(errorDetails(err), map[string]any{"selector": "missing"}) {
		t.Fatalf("unexpected error details: %#v", errorDetails(err))
	}
}

func TestLegacyMarketingAndQianchuanProductMigrationAreReadOnly(t *testing.T) {
	config := templateTestConfig(t)
	config["plan_template_schema_version"] = json.Number("1")
	config["plan_templates"] = map[string]any{
		"legacy": map[string]any{
			"platform": "平台", "traffic_source": "CID", "product_id": "3001",
			"product_label": "旧商品", "titles": []any{"旧标题"},
		},
	}
	config["account"] = map[string]any{"channel": "marketing", "advertiser_id": json.Number("1000000000000001")}
	productTemplates := config[qianchuanProductTemplatesKey].(map[string]any)
	product := productTemplates["qcpt_example"].(map[string]any)
	product["display_name"] = "旧名称"
	config[qianchuanProductSchemaVersionKey] = json.Number("2")
	before := cloneMap(config)
	result, err := List(config, "all", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, before) {
		t.Fatal("template query mutated input config")
	}
	channels := result["channels"].(map[string]any)
	marketing := channels["marketing"].(map[string]any)["templates"].([]any)[0].(map[string]any)
	if marketing["legacy"] != true || marketing["material_source_type"] != "ACCOUNT_UPLOAD" {
		t.Fatalf("legacy Marketing template was not normalized: %#v", marketing)
	}
	productRow := channels["qianchuan"].(map[string]any)["templates"].([]any)[0].(map[string]any)
	if productRow["display_name"] != "巨量千川-2000000000000001-示例商品-8000000000000001/8000000000000002-商品全域" {
		t.Fatalf("product name was not migrated: %#v", productRow)
	}
}

func templateTestConfig(t *testing.T) map[string]any {
	t.Helper()
	const payload = `{
  "plan_template_schema_version": 6,
  "default_channel": "marketing",
  "default_plan_template": {
    "defaults": {
      "daily_budget": 300,
      "roi_goal": 1.5,
      "gender": "NONE",
      "ages": [],
      "product_info": {"product_image_type": "DPA", "product_image_fields": ["images_url"]}
    },
    "resolved_ids": {"city_ids": [1, 2], "city_names": ["甲市", "乙市"]}
  },
  "plan_templates": {
    "巨量营销-1000000000000001-示例商品-3001-混剪素材": {
      "display_name": "巨量营销-1000000000000001-示例商品-3001-混剪素材",
      "bindings": {
        "advertiser_id": "1000000000000001",
        "platform": "平台",
        "traffic_source": "CID",
        "product_id": "3001",
        "product_name": "示例商品"
      },
      "material_strategy": {
        "source_type": "ACCOUNT_UPLOAD",
        "selection_mode": "MANUAL",
        "max_materials_per_unit": 5
      },
      "copy_materials": {"titles": ["示例标题一", "示例标题二"]},
      "overrides": {}
    }
  },
  "qianchuan_product_template_schema_version": 8,
  "default_qianchuan_product_template": {
    "template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN",
    "business_usable": false,
    "bindings": {
      "channel": "qianchuan",
      "advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
      "product_name": "REPLACE_WITH_PRODUCT_NAME",
      "product_short_name": "REPLACE_WITH_PRODUCT_SHORT_NAME",
      "product_ids": []
    },
    "plan_name_template": "{month_day}-{creator_name}-{product_short_name}-{type}-{business}",
    "delivery_setting": {
      "smart_bid_type": "SMART_BID_CUSTOM",
      "roi2_goal": 1.7,
      "qcpx_mode": "QCPX_MODE_ON",
      "budget": 5000,
      "video_schedule_type": "SCHEDULE_FROM_NOW",
      "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"
    },
    "material_strategy": {"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false}
  },
  "qianchuan_product_templates": {
    "qcpt_example": {
      "template_id": "qcpt_example",
      "display_name": "巨量千川-2000000000000001-示例商品-8000000000000001/8000000000000002-商品全域",
      "template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN",
      "status": "active",
      "bindings": {
        "channel": "qianchuan",
        "advertiser_id": "2000000000000001",
        "product_name": "示例商品",
        "product_short_name": "示例",
        "product_ids": ["8000000000000001", "8000000000000002"]
      },
      "plan_name_template": "{product_name}-{creator_name}-{datetime}",
      "delivery_setting": {
        "smart_bid_type": "SMART_BID_CUSTOM",
        "roi2_goal": 1.7,
        "qcpx_mode": "QCPX_MODE_ON",
        "budget": 5000,
        "video_schedule_type": "SCHEDULE_FROM_NOW",
        "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"
      },
      "material_strategy": {"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false}
    }
  },
  "qianchuan_live_template_schema_version": 1,
  "default_qianchuan_live_template": {
    "template_type": "QIANCHUAN_LIVE_ALL_DOMAIN",
    "business_usable": false,
    "bindings": {
      "channel": "qianchuan",
      "advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
      "creator_name": "REPLACE_WITH_CREATOR_NAME",
      "aweme_id": "REPLACE_WITH_AWEME_ID"
    },
    "delivery_setting": {
      "smart_bid_type": "SMART_BID_CONSERVATIVE",
      "budget": 5000,
      "live_schedule_type": "SCHEDULE_FROM_NOW",
      "daily_delivery_time": 8.5
    },
    "creative_setting": {"smart_select_material": true},
    "material_strategy": {"source_type": "LIVE_SMART_SELECTION", "persist_material_ids": false}
  },
  "qianchuan_live_templates": {
    "qclt_example": {
      "template_id": "qclt_example",
      "display_name": "巨量千川-2000000000000001-示例达人-9988776655-直播全域",
      "template_type": "QIANCHUAN_LIVE_ALL_DOMAIN",
      "status": "active",
      "bindings": {
        "channel": "qianchuan",
        "advertiser_id": "2000000000000001",
        "creator_name": "示例达人",
        "aweme_id": "9988776655"
      },
      "delivery_setting": {
        "smart_bid_type": "SMART_BID_CONSERVATIVE",
        "budget": 5000,
        "live_schedule_type": "SCHEDULE_FROM_NOW",
        "daily_delivery_time": 8.5
      },
      "creative_setting": {"smart_select_material": true},
      "material_strategy": {"source_type": "LIVE_SMART_SELECTION", "persist_material_ids": false}
    }
  }
}`
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.UseNumber()
	var config map[string]any
	if err := decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	return config
}
