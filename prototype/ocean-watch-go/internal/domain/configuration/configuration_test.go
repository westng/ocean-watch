package configuration

import "testing"

func TestMigrateChannelsPreservesUnknownFieldsAndStripsCredentials(t *testing.T) {
	raw := map[string]any{
		"future": map[string]any{"preserved": true},
		"api": map[string]any{
			"base_url":     "https://api.oceanengine.com/open_api",
			"access_token": "TEST_ACCESS_TOKEN_DO_NOT_USE",
			"developer_id": "TEST_DEVELOPER_ID",
			"auth_code":    "TEST_AUTH_CODE",
		},
		"oauth":   map[string]any{"redirect_uri": "http://127.0.0.1:8787/oauth/callback"},
		"account": map[string]any{"advertiser_id": "1000000000000001"},
		"plan_templates": map[string]any{
			"fixture": map[string]any{"bindings": map[string]any{"advertiser_id": "1000000000000001"}},
		},
	}
	migrated, err := MigrateChannels(raw)
	if err != nil {
		t.Fatal(err)
	}
	if migrated["api"] != nil || migrated["oauth"] != nil {
		t.Fatalf("legacy roots survived: %#v", migrated)
	}
	if Value(migrated, "channels.marketing.api.access_token") != nil {
		t.Fatal("credential leaked into channel config")
	}
	if Value(migrated, "channels.marketing.api.developer_id") != nil ||
		Value(migrated, "channels.marketing.api.auth_code") != nil {
		t.Fatal("sensitive compatibility field leaked into channel config")
	}
	if Value(migrated, "future.preserved") != true {
		t.Fatal("unknown field was lost")
	}
	if Value(migrated, "plan_templates.fixture.bindings.channel") != "marketing" {
		t.Fatal("template channel was not filled")
	}
	if raw["api"].(map[string]any)["access_token"] == nil {
		t.Fatal("input was mutated")
	}
}

func TestRuntimeNeverFallsBackAcrossChannels(t *testing.T) {
	raw := map[string]any{
		"config_schema_version": 2,
		"default_channel":       "qianchuan",
		"channels": map[string]any{
			"marketing": map[string]any{"api": map[string]any{"marketing_only": true}},
			"qianchuan": map[string]any{"api": map[string]any{"qianchuan_only": true}},
		},
		"account": map[string]any{"channel": "qianchuan"},
	}
	runtimeConfig, channel, err := Runtime(raw, "", "qianchuan_materials")
	if err != nil {
		t.Fatal(err)
	}
	if channel.ID != "qianchuan" || Value(runtimeConfig, "api.marketing_only") != nil || Value(runtimeConfig, "api.qianchuan_only") != true {
		t.Fatalf("cross-channel runtime merge: %#v", runtimeConfig)
	}
}

func TestExtractLegacyCredentialsRejectsConflicts(t *testing.T) {
	raw := map[string]any{
		"api": map[string]any{"app_id": "fixture-a"},
		"channels": map[string]any{
			"marketing": map[string]any{"api": map[string]any{"app_id": "fixture-b"}},
		},
	}
	if _, err := ExtractLegacyCredentials(raw, "marketing"); err == nil {
		t.Fatal("expected conflicting credentials to fail")
	}
}
