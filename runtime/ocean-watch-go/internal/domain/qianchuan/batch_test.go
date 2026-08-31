package qianchuan

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestPlanGroupIdentityCanonicalGolden(t *testing.T) {
	identity, err := NewPlanGroupIdentity(
		"1000000000000001", "qcpt_test", "4000000000000001",
		[]string{"8000000000000002", "8000000000000001", "8000000000000001"}, "随手po", "刘岛",
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalPlanGroupIdentity(identity)
	if err != nil || string(canonical) != `{"advertiser_id":"1000000000000001","template_id":"qcpt_test","creator_id":"4000000000000001","product_ids":["8000000000000001","8000000000000002"],"plan_type":"随手po","business":"刘岛"}` {
		t.Fatalf("canonical=%s err=%v", canonical, err)
	}
	groupID, err := GroupID(identity)
	if err != nil || groupID != "qcg_90d6380dc1b4f2c066a99d081cd5488094630ab6abc3a689708f04837c8d6be8" {
		t.Fatalf("group_id=%s err=%v", groupID, err)
	}
}

func TestTwentyFiveRowFixtureKeepsPlanTypesSeparate(t *testing.T) {
	payload, err := os.ReadFile("testdata/batch-items-25.json")
	if err != nil {
		t.Fatal(err)
	}
	var items []VerifiedBatchItem
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatal(err)
	}
	groups, err := GroupVerifiedItems("1000000000000001", "qcpt_fixture", items)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, group := range groups {
		counts[group.Identity.PlanType] += len(group.Items)
	}
	if len(items) != 25 || len(groups) != 2 || counts["随手po"] != 22 || counts["真人口播营销"] != 3 {
		t.Fatalf("items=%d groups=%d counts=%v", len(items), len(groups), counts)
	}
}

func TestGroupIdentitySeparatesTypeBusinessAndProducts(t *testing.T) {
	base := func(planType, business string, products []string) PlanGroupIdentity {
		identity, err := NewPlanGroupIdentity("a", "t", "c", products, planType, business)
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	ids := map[string]string{}
	for name, identity := range map[string]PlanGroupIdentity{
		"base":       base("随手po", "刘岛", []string{"p1", "p2"}),
		"reordered":  base("随手po", "刘岛", []string{"p2", "p1", "p1"}),
		"type":       base("真人口播营销", "刘岛", []string{"p1", "p2"}),
		"business":   base("随手po", "另一个商务", []string{"p1", "p2"}),
		"products":   base("随手po", "刘岛", []string{"p1"}),
		"empty_type": base("", "刘岛", []string{"p1", "p2"}),
	} {
		id, err := GroupID(identity)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	if ids["base"] != ids["reordered"] {
		t.Fatal("product order changed group identity")
	}
	for _, name := range []string{"type", "business", "products", "empty_type"} {
		if ids["base"] == ids[name] {
			t.Fatalf("%s was merged into base", name)
		}
	}
}

func TestGroupIDIsStableWhenInputOrderChanges(t *testing.T) {
	items := []VerifiedBatchItem{
		{BatchItem: BatchItem{InputIndex: 0, PlanType: "随手po", Business: "刘岛"}, WorkID: "w1", CreatorID: "c1", ProductIDs: []string{"p1"}},
		{BatchItem: BatchItem{InputIndex: 1, PlanType: "随手po", Business: "刘岛"}, WorkID: "w2", CreatorID: "c1", ProductIDs: []string{"p1"}},
	}
	first, err := GroupVerifiedItems("a", "t", items)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GroupVerifiedItems("a", "t", []VerifiedBatchItem{items[1], items[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].GroupID != second[0].GroupID {
		t.Fatalf("reordered input changed group id: first=%#v second=%#v", first, second)
	}
}

func TestDeduplicateVerifiedItemsReportsConflicts(t *testing.T) {
	item := VerifiedBatchItem{BatchItem: BatchItem{InputIndex: 0, WorkURL: "u", PlanType: "随手po", Business: "刘岛"}, WorkID: "w", CreatorID: "c", ProductIDs: []string{"p2", "p1"}}
	result, duplicates, err := DeduplicateVerifiedItems([]VerifiedBatchItem{item, item})
	if err != nil || len(result) != 1 || !reflect.DeepEqual(duplicates, []int{0}) {
		t.Fatalf("duplicate result=%#v duplicates=%v err=%v", result, duplicates, err)
	}
	conflict := item
	conflict.InputIndex = 1
	conflict.Business = "其他"
	_, _, err = DeduplicateVerifiedItems([]VerifiedBatchItem{item, conflict})
	if !errors.Is(err, ErrDuplicateItemConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}
