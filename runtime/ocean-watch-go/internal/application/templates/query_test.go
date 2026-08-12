package templates

import (
	"context"
	"testing"
)

type countingConfigStore struct {
	reads  int
	config map[string]any
}

func (store *countingConfigStore) Read(context.Context) (map[string]any, error) {
	store.reads++
	return store.config, nil
}

func TestListReadsConfigExactlyOnce(t *testing.T) {
	store := &countingConfigStore{config: map[string]any{}}
	query := Query{Store: store, Path: "/synthetic/config.json"}
	result, err := query.List(context.Background(), "all", false)
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatalf("config read %d times, want 1", store.reads)
	}
	if result["config"] != "/synthetic/config.json" {
		t.Fatalf("unexpected config path: %#v", result)
	}
}

func TestShowReadsConfigExactlyOnce(t *testing.T) {
	store := &countingConfigStore{config: map[string]any{
		"plan_templates": map[string]any{
			"template": map[string]any{
				"bindings": map[string]any{
					"channel": "marketing", "advertiser_id": "1", "platform": "p",
					"traffic_source": "t", "product_id": "2", "product_name": "product",
				},
				"material_strategy": map[string]any{
					"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5,
				},
				"overrides": map[string]any{},
			},
		},
	}}
	query := Query{Store: store, Path: "/synthetic/config.json"}
	if _, err := query.Show(context.Background(), "marketing", "template"); err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatalf("config read %d times, want 1", store.reads)
	}
}
