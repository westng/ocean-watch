package configuration

import "testing"

func TestCurrentRejectsRemovedConfigurationShapes(t *testing.T) {
	for name, raw := range map[string]map[string]any{
		"missing schema": {"channels": map[string]any{}},
		"old schema":     {"config_schema_version": 1, "channels": map[string]any{}},
		"root api":       {"config_schema_version": 2, "channels": map[string]any{}, "api": map[string]any{}},
		"root oauth":     {"config_schema_version": 2, "channels": map[string]any{}, "oauth": map[string]any{}},
		"embedded token": {"config_schema_version": 2, "channels": map[string]any{"marketing": map[string]any{"api": map[string]any{"access_token": "fixture"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Current(raw); err == nil {
				t.Fatal("expected removed configuration shape to fail")
			}
		})
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

func TestRuntimeRejectsMissingSelectedChannel(t *testing.T) {
	raw := map[string]any{"config_schema_version": 2, "channels": map[string]any{}}
	if _, _, err := Runtime(raw, "qianchuan", "qianchuan_materials"); err == nil {
		t.Fatal("expected missing selected channel to fail")
	}
}
