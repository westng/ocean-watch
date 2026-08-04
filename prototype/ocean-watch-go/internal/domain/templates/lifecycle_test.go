package templates

import (
	"reflect"
	"testing"
)

const marketingFixtureName = "巨量营销-1000000000000001-示例商品-3001-混剪素材"

func TestValidateCurrentTemplateFixture(t *testing.T) {
	config := templateTestConfig(t)
	before := cloneMap(config)
	result, err := Validate(config, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["mode"] != "template_validation" {
		t.Fatalf("unexpected validation envelope: %#v", result)
	}
	channels := result["channels"].([]any)
	marketing := channels[0].(map[string]any)
	qianchuan := channels[1].(map[string]any)
	if marketing["schema_version"] != 6 || marketing["supported_schema_version"] != 6 || marketing["valid"] != true {
		t.Fatalf("unexpected Marketing validation: %#v", marketing)
	}
	if !reflect.DeepEqual(marketing["errors"], []any{}) || !reflect.DeepEqual(marketing["templates"], []any{
		map[string]any{"template": marketingFixtureName, "valid": true, "errors": []any{}},
	}) {
		t.Fatalf("unexpected Marketing validation rows: %#v", marketing)
	}
	if qianchuan["selected_template_kind"] != nil || qianchuan["valid"] != true {
		t.Fatalf("unexpected Qianchuan validation: %#v", qianchuan)
	}
	if !reflect.DeepEqual(qianchuan["schema_versions"], map[string]any{"product": 5, "live": 1}) {
		t.Fatalf("unexpected Qianchuan schema versions: %#v", qianchuan)
	}
	if !reflect.DeepEqual(config, before) {
		t.Fatal("validation mutated the input config")
	}
}

func TestValidateCollectsTemplateAndSchemaErrors(t *testing.T) {
	config := templateTestConfig(t)
	config["plan_template_schema_version"] = true
	template := config["plan_templates"].(map[string]any)[marketingFixtureName].(map[string]any)
	template["material_strategy"] = map[string]any{"source_type": "INVALID"}
	result, err := Validate(config, "marketing", marketingFixtureName)
	if err != nil {
		t.Fatal(err)
	}
	channel := result["channels"].([]any)[0].(map[string]any)
	if channel["valid"] != false || channel["schema_version"] != nil {
		t.Fatalf("invalid Marketing config passed validation: %#v", channel)
	}
	if !reflect.DeepEqual(channel["errors"], []any{"plan_template_schema_version must be a positive integer"}) {
		t.Fatalf("schema error was not preserved: %#v", channel["errors"])
	}
	templateErrors := channel["templates"].([]any)[0].(map[string]any)["errors"]
	if !reflect.DeepEqual(templateErrors, []any{"plan_template_schema_version must be an integer"}) {
		t.Fatalf("template errors were not preserved: %#v", templateErrors)
	}
}

func TestDeleteMarketingProtectsBothReferenceKinds(t *testing.T) {
	config := templateTestConfig(t)
	dependentName := "巨量营销-1000000000000001-另一商品-3002-混剪素材"
	dependent := cloneMap(config["plan_templates"].(map[string]any)[marketingFixtureName].(map[string]any))
	dependent["display_name"] = dependentName
	dependent["bindings"].(map[string]any)["product_id"] = "3002"
	dependent["bindings"].(map[string]any)["product_name"] = "另一商品"
	dependent["created_from"] = map[string]any{"template": marketingFixtureName}
	dependent["copy_materials"].(map[string]any)["copied_from_template"] = marketingFixtureName
	config["plan_templates"].(map[string]any)[dependentName] = dependent

	_, _, err := Delete(config, "marketing", marketingFixtureName, false)
	if err == nil || err.Error() != "template is referenced by other templates; pass --force to delete" {
		t.Fatalf("unexpected reference error: %v", err)
	}
	want := []any{map[string]any{
		"template":   dependentName,
		"references": []any{"created_from.template", "copy_materials.copied_from_template"},
	}}
	if !reflect.DeepEqual(errorDetails(err)["dependents"], want) {
		t.Fatalf("unexpected dependents: %#v", errorDetails(err))
	}
	updated, deletion, err := Delete(config, "marketing", marketingFixtureName, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := updated["plan_templates"].(map[string]any)[marketingFixtureName]; exists {
		t.Fatal("forced deletion retained target template")
	}
	if !reflect.DeepEqual(deletion["dependents"], want) {
		t.Fatalf("forced deletion lost diagnostics: %#v", deletion)
	}
}

func TestDeleteQianchuanResolvesProductBeforeLive(t *testing.T) {
	config := templateTestConfig(t)
	productConfig, productDeletion, err := Delete(config, "qianchuan", "qcpt_example", false)
	if err != nil {
		t.Fatal(err)
	}
	if productDeletion["template_kind"] != "product" || productDeletion["template"] != "qcpt_example" {
		t.Fatalf("unexpected product deletion: %#v", productDeletion)
	}
	if len(productConfig[qianchuanProductTemplatesKey].(map[string]any)) != 0 {
		t.Fatal("product template was not removed")
	}
	liveConfig, liveDeletion, err := Delete(config, "qianchuan", "qclt_example", false)
	if err != nil {
		t.Fatal(err)
	}
	if liveDeletion["template_kind"] != "live" || liveDeletion["template"] != "qclt_example" {
		t.Fatalf("unexpected live deletion: %#v", liveDeletion)
	}
	if len(liveConfig[qianchuanLiveTemplatesKey].(map[string]any)) != 0 {
		t.Fatal("live template was not removed")
	}
}

func TestSetCopyNormalizesTitlesAndTracksSource(t *testing.T) {
	config := templateTestConfig(t)
	updated, err := SetCopy(config, marketingFixtureName, []string{" 全新示例标题一 ", "全新示例标题一", "全新示例标题二"}, "")
	if err != nil {
		t.Fatal(err)
	}
	copyMaterials := updated["plan_templates"].(map[string]any)[marketingFixtureName].(map[string]any)["copy_materials"].(map[string]any)
	if !reflect.DeepEqual(copyMaterials, map[string]any{"titles": []any{"全新示例标题一", "全新示例标题二"}}) {
		t.Fatalf("unexpected normalized copy: %#v", copyMaterials)
	}
	fromSelf, err := SetCopy(config, marketingFixtureName, nil, marketingFixtureName)
	if err != nil {
		t.Fatal(err)
	}
	copyMaterials = fromSelf["plan_templates"].(map[string]any)[marketingFixtureName].(map[string]any)["copy_materials"].(map[string]any)
	if copyMaterials["copied_from_template"] != marketingFixtureName {
		t.Fatalf("copy source was not tracked: %#v", copyMaterials)
	}
	if _, err := SetCopy(config, marketingFixtureName, []string{"短"}, ""); err == nil {
		t.Fatal("invalid copy title was accepted")
	}
}

func TestCurrentSchemaMigrationContracts(t *testing.T) {
	config := templateTestConfig(t)
	marketing, legacyError, err := MigrateMarketing(config, false)
	if err != nil || legacyError != nil {
		t.Fatalf("current Marketing migration failed: %v, %v", legacyError, err)
	}
	if !reflect.DeepEqual(config, marketing) {
		t.Fatal("current Marketing migration changed semantic content")
	}
	productConfig, productResult, productChanged, err := MigrateQianchuanProduct(config)
	if err != nil {
		t.Fatal(err)
	}
	if productChanged || productResult["migrated"] != false || !semanticEqual(config, productConfig) {
		t.Fatalf("current product migration was not idempotent: %#v", productResult)
	}
	liveConfig, liveResult, liveChanged, err := MigrateQianchuanLive(config)
	if err != nil {
		t.Fatal(err)
	}
	if liveChanged || liveResult["migrated"] != false || !semanticEqual(config, liveConfig) {
		t.Fatalf("current live migration was not idempotent: %#v", liveResult)
	}
}

func TestUserNamesSurviveMarketingV5AndQianchuanV4Migration(t *testing.T) {
	config := templateTestConfig(t)
	marketingTemplates := config["plan_templates"].(map[string]any)
	marketingTemplate := marketingTemplates[marketingFixtureName].(map[string]any)
	delete(marketingTemplates, marketingFixtureName)
	marketingTemplate["display_name"] = "用户旧营销模板"
	marketingTemplates["用户旧营销模板"] = marketingTemplate
	config["plan_template_schema_version"] = 5

	migratedMarketing, legacyError, err := MigrateMarketing(config, false)
	if err != nil || legacyError != nil {
		t.Fatalf("Marketing v5 migration failed: %v, %v", legacyError, err)
	}
	if migratedMarketing["plan_template_schema_version"] != marketingSchemaVersion ||
		mapOrEmpty(migratedMarketing["plan_templates"])["用户旧营销模板"] == nil {
		t.Fatalf("Marketing user name changed during migration: %#v", migratedMarketing["plan_templates"])
	}

	productTemplates := config[qianchuanProductTemplatesKey].(map[string]any)
	productTemplate := productTemplates["qcpt_example"].(map[string]any)
	productTemplate["display_name"] = "用户旧千川模板"
	delete(productTemplate, "plan_name_template")
	config[qianchuanProductSchemaVersionKey] = 4

	migratedProduct, _, changed, err := MigrateQianchuanProduct(config)
	if err != nil {
		t.Fatal(err)
	}
	migratedTemplate := mapOrEmpty(migratedProduct[qianchuanProductTemplatesKey])["qcpt_example"].(map[string]any)
	if !changed || migratedTemplate["display_name"] != "用户旧千川模板" ||
		migratedTemplate["plan_name_template"] != qianchuanProductDefaultPlanName {
		t.Fatalf("Qianchuan v4 migration changed user name or behavior: %#v", migratedTemplate)
	}
}
