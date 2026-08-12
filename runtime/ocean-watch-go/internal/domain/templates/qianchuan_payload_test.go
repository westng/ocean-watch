package templates

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestExportQianchuanProductPayloadUsesOnlyOfficialFields(t *testing.T) {
	exported, err := ExportQianchuanPlanPayload(
		templateTestConfig(t), QianchuanTemplateProduct, "qcpt_example", "测试计划",
	)
	if err != nil {
		t.Fatal(err)
	}
	if exported.AdvertiserID != "2000000000000001" || exported.ProductName != "示例商品" ||
		!exported.Active || !reflect.DeepEqual(exported.ProductIDs, []string{"8000000000000001", "8000000000000002"}) {
		t.Fatalf("unexpected export metadata: %#v", exported)
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(exported.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedKeys(payload), []string{
		"advertiser_id", "delivery_setting", "marketing_goal", "name", "product_ids",
	}) {
		t.Fatalf("unexpected product payload fields: %#v", payload)
	}
	if payload["advertiser_id"] != json.Number("2000000000000001") || payload["marketing_goal"] != "VIDEO_PROM_GOODS" || payload["name"] != "测试计划" {
		t.Fatalf("unexpected product payload: %#v", payload)
	}
	if !reflect.DeepEqual(payload["product_ids"], []any{json.Number("8000000000000001"), json.Number("8000000000000002")}) {
		t.Fatalf("product IDs lost integer precision: %#v", payload["product_ids"])
	}
}

func TestExportQianchuanLivePayloadUsesOnlyOfficialFields(t *testing.T) {
	exported, err := ExportQianchuanPlanPayload(
		templateTestConfig(t), QianchuanTemplateLive, "qclt_example", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(exported.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedKeys(payload), []string{
		"advertiser_id", "aweme_id", "creative_setting", "delivery_setting", "marketing_goal",
	}) {
		t.Fatalf("unexpected live payload fields: %#v", payload)
	}
	if payload["aweme_id"] != json.Number("9988776655") || payload["marketing_goal"] != "LIVE_PROM_GOODS" {
		t.Fatalf("unexpected live payload: %#v", payload)
	}
	if _, exists := payload["name"]; exists {
		t.Fatal("live payload unexpectedly includes name")
	}
}

func TestExportQianchuanPlanPayloadRejectsAmbiguousSource(t *testing.T) {
	if _, err := ExportQianchuanPlanPayload(templateTestConfig(t), "other", "qcpt_example", ""); err == nil {
		t.Fatal("unsupported template kind was accepted")
	}
	if _, err := ExportQianchuanPlanPayload(templateTestConfig(t), QianchuanTemplateLive, "qclt_example", "forbidden"); err == nil {
		t.Fatal("live plan name was accepted")
	}
}
