package sqlite

import (
	"context"
	"testing"
)

func TestLegacyMultipleItemsRoundTripAndInputOrder(t *testing.T) {
	ctx := context.Background()
	store, _ := legacyStore(t)
	mapping := legacyMapping(t, "multi", func(v map[string]any) {
		v["jobs"].(map[string]any)["job-2"] = map[string]any{"status": "promotion_dispatching", "advertiser_id": "456", "request_id": "request-2", "dispatch_key": "dispatch-2", "dispatch_state": "unknown"}
	})
	if len(mapping.Items) != 2 {
		t.Fatal("missing source item mapping")
	}
	if _, _, err := store.ImportLegacyMapping(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	mapping.Items[0], mapping.Items[1] = mapping.Items[1], mapping.Items[0]
	got, created, err := store.ImportLegacyMapping(ctx, mapping)
	if err != nil || created || len(got.Items) != 2 {
		t.Fatalf("created=%v err=%v items=%d", created, err, len(got.Items))
	}
	if got.Items[0].SourceItemID != "job-1" || got.Items[1].SourceItemID != "job-2" {
		t.Fatal("lost stable source item identity")
	}
	if got.Items[0].TargetItemID == got.Items[1].TargetItemID {
		t.Fatal("target item collision")
	}
}
