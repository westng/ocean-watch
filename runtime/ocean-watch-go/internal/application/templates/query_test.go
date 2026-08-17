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

func (store *countingConfigStore) ReadWithRevision(context.Context) (map[string]any, string, error) {
	store.reads++
	return store.config, "revision", nil
}

func TestListReadsConfigExactlyOnce(t *testing.T) {
	store := &countingConfigStore{config: map[string]any{}}
	query := Query{Store: store}
	result, err := query.List(context.Background(), "all", false)
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatalf("config read %d times, want 1", store.reads)
	}
	if _, exists := result["config"]; exists {
		t.Fatalf("application result leaked a config path: %#v", result)
	}
}

func TestShowReadsConfigExactlyOnce(t *testing.T) {
	store := &countingConfigStore{config: map[string]any{
		"plan_template_schema_version": 6,
		"default_plan_template":        map[string]any{},
		"plan_templates": map[string]any{
			"template": map[string]any{
				"display_name": "template",
				"bindings": map[string]any{
					"channel": "marketing", "advertiser_id": "1", "platform": "p",
					"traffic_source": "t", "product_id": "2", "product_name": "product",
				},
				"material_strategy": map[string]any{
					"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": 5,
				},
				"copy_materials": map[string]any{},
				"overrides":      map[string]any{},
			},
		},
	}}
	query := Query{Store: store}
	if _, err := query.Show(context.Background(), "marketing", "template"); err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatalf("config read %d times, want 1", store.reads)
	}
}

func TestVersionedQueriesReadConfigExactlyOnce(t *testing.T) {
	store := &countingConfigStore{config: map[string]any{}}
	query := Query{Store: store, VersionedStore: store}
	result, err := query.ListVersioned(context.Background(), "all", false)
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 || result.StateVersion != "revision" {
		t.Fatalf("unexpected versioned read: reads=%d result=%#v", store.reads, result)
	}
}
