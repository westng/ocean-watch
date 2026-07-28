package marketing

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestBuildMarketingPayloadsMatchesPythonGolden(t *testing.T) {
	config := marketingPayloadFixture()
	payloads, err := BuildPayloads(config, PayloadOptions{MaterialDate: "7.25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads.MissingFields) != 0 {
		t.Fatalf("valid golden fixture was blocked: %#v", payloads.MissingFields)
	}
	projectJSON, promotionJSON, err := payloads.JSON()
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, projectJSON, `{
      "advertiser_id": 1234567890,
      "name": "7.25_test product_1_01",
      "operation": "ENABLE",
      "delivery_mode": "PROCEDURAL",
      "landing_type": "SHOP",
      "asset_type": "THIRDPARTY",
      "marketing_goal": "VIDEO_AND_IMAGE",
      "ad_type": "ALL",
      "related_product": {"product_setting": "SINGLE", "unique_product_id": 9007199254740993},
      "delivery_range": {"inventory_catalog": "UNIVERSAL_SMART"},
      "delivery_setting": {
        "schedule_type": "SCHEDULE_FROM_NOW", "budget_mode": "BUDGET_MODE_DAY",
        "budget": 300, "bid_type": "CUSTOM", "pricing": "PRICING_OCPM",
        "cpa_bid": 100, "roi_goal": 1.5, "deep_bid_type": "NET_ORDER_ROI"
      },
      "optimize_goal": {"external_action": "AD_CONVERT_TYPE_APP_ORDER", "asset_ids": [2001]},
      "audience": {
        "district": "REGION", "region_version": "2.3.2", "city": [11, 12],
        "location_type": "CURRENT", "gender": "NONE", "hide_if_converted": "NO_EXCLUDE"
      },
      "track_url_setting": {
        "track_url": ["https://tracking.test/impression?a=1&b=two%20words"],
        "action_track_url": ["https://tracking.test/click?x=%2Fpath&y=1"]
      }
    }`)
	assertJSONEqual(t, promotionJSON, `{
      "advertiser_id": 1234567890,
      "project_id": "{{project_id}}",
      "name": "单元_test product_7.25_1_01",
      "operation": "ENABLE",
      "source": "test source",
      "promotion_materials": {
        "video_material_list": [
          {"video_id": "video-1", "image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", "video_cover_id": "cover-1"},
          {"video_id": "video-2", "image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", "video_cover_id": "cover-2"}
        ],
        "title_material_list": [
          {"title": "这是第一条测试标题"}, {"title": "这是第二条测试标题"}
        ],
        "external_url_material_list": ["https://landing.test/page?p=1"],
        "open_url_type": "CUSTOM",
        "open_url": "testapp://open?sku=1",
        "component_material_list": [],
        "product_info": {
          "titles": ["商品甲"], "selling_points": ["商品卖点推荐"],
          "product_name_type": "CUSTOM", "product_image_type": "DPA",
          "product_selling_point_type": "CUSTOM", "product_image_fields": ["images_url"]
        },
        "call_to_action_buttons": ["立即购买"]
      },
      "promotion_related_product": [{"unique_product_id": 9007199254740993}],
      "brand_info": {"brand_name_id": 4001}
    }`)
	if !bytes.Contains(projectJSON, []byte(`9007199254740993`)) ||
		bytes.Contains(projectJSON, []byte(`9.007199254740`)) {
		t.Fatalf("large product ID lost precision: %s", projectJSON)
	}
	if got := payloads.Project["track_url_setting"].(map[string]any)["track_url"]; !reflect.DeepEqual(
		got,
		[]any{"https://tracking.test/impression?a=1&b=two%20words"},
	) {
		t.Fatalf("tracking URL changed: %#v", got)
	}
}

func TestBuildMarketingPayloadsSupportsCustomImagesAndOverrides(t *testing.T) {
	config := marketingPayloadFixture()
	productInfo := config["defaults"].(map[string]any)["product_info"].(map[string]any)
	productInfo["product_image_type"] = "CUSTOM"
	delete(productInfo, "product_image_fields")
	config["resolved_ids"].(map[string]any)["product_image_ids"] = []any{"image-1", "image-2"}
	payloads, err := BuildPayloads(config, PayloadOptions{
		AdvertiserID: "1234567890", Budget: 600, CPABid: 120, ROIGoal: 2.25,
		VideoIDs: []string{"runtime-video"}, MaterialDate: "7.26",
		ProjectName: "项目覆盖", PromotionName: "单元覆盖", ProjectID: "2001",
	})
	if err != nil {
		t.Fatal(err)
	}
	product := payloads.Promotion["promotion_materials"].(map[string]any)["product_info"].(map[string]any)
	if !reflect.DeepEqual(product["image_ids"], []any{"image-1", "image-2"}) {
		t.Fatalf("CUSTOM image IDs missing: %#v", product)
	}
	if _, exists := product["product_image_fields"]; exists {
		t.Fatalf("CUSTOM payload leaked DPA fields: %#v", product)
	}
	if payloads.Promotion["project_id"] != json.Number("2001") ||
		payloads.Project["name"] != "项目覆盖" || payloads.Promotion["name"] != "单元覆盖" {
		t.Fatalf("runtime overrides changed: %#v", payloads)
	}
	delivery := payloads.Project["delivery_setting"].(map[string]any)
	if delivery["budget"] != 600 || delivery["cpa_bid"] != 120 || delivery["roi_goal"] != 2.25 {
		t.Fatalf("delivery overrides changed: %#v", delivery)
	}
}

func TestMarketingPayloadMissingFieldsBlocksRuntimeAssetsAndInvalidCopy(t *testing.T) {
	config := marketingPayloadFixture()
	config["materials"] = map[string]any{}
	config["resolved_ids"] = map[string]any{
		"unique_product_id": "9007199254740993",
		"city_ids":          []any{},
	}
	config["tracking_urls"] = map[string]any{
		"track_url": []any{"https://example.com/impression"}, "action_track_url": []any{},
	}
	config["links"] = map[string]any{"landing_page_url": "TODO", "open_url": ""}
	config["titles"] = []any{"太短"}
	config["defaults"].(map[string]any)["product_info"].(map[string]any)["selling_points"] = []any{"太短"}
	payloads, err := BuildPayloads(config, PayloadOptions{MaterialDate: "7.25"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tracking_urls.track_url", "tracking_urls.action_track_url", "resolved_ids.city_ids",
		"materials.video_ids", "links.landing_page_url", "links.open_url", "titles",
		"defaults.product_info.selling_points", "resolved_ids.event_asset_ids",
	}
	if !reflect.DeepEqual(payloads.MissingFields, want) {
		t.Fatalf("blocking field contract changed:\n got %#v\nwant %#v", payloads.MissingFields, want)
	}
}

func TestBuildMarketingPayloadsUsesStableYesterdayAndRejectsInvalidScope(t *testing.T) {
	config := marketingPayloadFixture()
	payloads, err := BuildPayloads(config, PayloadOptions{
		Now: time.Date(2026, 7, 26, 1, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if payloads.Project["name"] != "7.25_test product_1_01" {
		t.Fatalf("default material date changed: %v", payloads.Project["name"])
	}
	if _, err := BuildPayloads(config, PayloadOptions{AdvertiserID: "not-an-id"}); err == nil {
		t.Fatal("invalid advertiser scope was accepted")
	}
	if _, err := BuildPayloads(config, PayloadOptions{ProjectID: "0"}); err == nil {
		t.Fatal("invalid resume project ID was accepted")
	}
}

func marketingPayloadFixture() map[string]any {
	return map[string]any{
		"account": map[string]any{"channel": "marketing", "advertiser_id": "1234567890"},
		"defaults": map[string]any{
			"operation": "ENABLE", "project_name_template": "{material_date}_{product_name}_{group_index}_{suffix}",
			"promotion_name_template": "单元_{product_name}_{material_date}_{index}_{suffix}",
			"product_name":            "test product", "product_id": "product-1", "daily_budget": 300,
			"cpa_bid": 100, "roi_goal": 1.5, "source": "test source", "landing_type": "SHOP",
			"asset_type": "THIRDPARTY", "marketing_goal": "VIDEO_AND_IMAGE", "delivery_mode": "PROCEDURAL",
			"ad_type": "ALL", "gender": "NONE", "ages": []any{}, "location_type": "CURRENT",
			"district": "REGION", "region_version": "2.3.2", "hide_if_converted": "NO_EXCLUDE",
			"schedule_type": "SCHEDULE_FROM_NOW", "budget_mode": "BUDGET_MODE_DAY", "pricing": "PRICING_OCPM",
			"external_action": "AD_CONVERT_TYPE_APP_ORDER", "deep_bid_type": "NET_ORDER_ROI",
			"video_image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", "call_to_action_buttons": []any{"立即购买"},
			"product_info": map[string]any{
				"product_name_type": "CUSTOM", "product_image_type": "DPA",
				"product_image_fields": []any{"images_url"}, "product_selling_point_type": "CUSTOM",
				"titles": []any{"商品甲"}, "selling_points": []any{"商品卖点推荐"},
			},
		},
		"materials": map[string]any{
			"video_ids":       []any{"video-1", "video-2"},
			"video_cover_ids": map[string]any{"video-1": "cover-1", "video-2": "cover-2"},
		},
		"resolved_ids": map[string]any{
			"city_ids": []any{11, 12}, "unique_product_id": "9007199254740993",
			"event_asset_ids": []any{2001}, "brand_info": map[string]any{"brand_name_id": 4001, "cdp_brand_id": nil},
		},
		"tracking_urls": map[string]any{
			"track_url":        []any{"https://tracking.test/impression?a=1&b=two%20words"},
			"action_track_url": []any{"https://tracking.test/click?x=%2Fpath&y=1"},
		},
		"links": map[string]any{
			"landing_page_url": "https://landing.test/page?p=1", "open_url": "testapp://open?sku=1",
		},
		"titles": []any{"这是第一条测试标题", "这是第二条测试标题"},
	}
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	decode := func(payload []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		var result any
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	gotValue := decode(got)
	wantValue := decode([]byte(want))
	if !reflect.DeepEqual(gotValue, wantValue) {
		gotFormatted, _ := json.MarshalIndent(gotValue, "", "  ")
		wantFormatted, _ := json.MarshalIndent(wantValue, "", "  ")
		t.Fatalf("JSON differs\n--- got ---\n%s\n--- want ---\n%s", gotFormatted, wantFormatted)
	}
}
