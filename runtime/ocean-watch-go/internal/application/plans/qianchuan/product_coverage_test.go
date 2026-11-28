package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	dq "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/state"
)

type coverageReadbacks struct {
	state.OperationRecovery
	saved []ops.Readback
}

func (r *coverageReadbacks) SaveReadback(_ context.Context, row ops.Readback) error {
	r.saved = append(r.saved, row)
	return nil
}

type coverageReader struct {
	batchStateReader
	rows     []dq.PlanMaterial
	products []string
}

func (r *coverageReader) FetchPlanMaterials(context.Context, port.MaterialPageRequest) (dq.MaterialPage, error) {
	return dq.MaterialPage{Rows: r.rows, PageInfo: dq.PageInfo{Page: 1, TotalPages: 1, TotalNumber: len(r.rows)}}, nil
}
func (r *coverageReader) FetchPlanDetail(context.Context, port.PlanDetailRequest) (dq.PlanDetail, error) {
	d := dq.PlanDetail{AdID: batchPlanID, AwemeID: batchCreatorID, Status: "ENABLE"}
	for _, id := range r.products {
		d.Products = append(d.Products, dq.PlanProduct{ProductID: id})
	}
	return d, nil
}
func TestProductCoveragePreflightAndReadback(t *testing.T) {
	ctx := context.Background()
	products := []string{batchProductID, routeProductB}
	works := batchVerifiedWorks(1)
	works[0].MatchedProductIDs = products
	id := works[0].AwemeItemID
	rowA := dq.PlanMaterial{AwemeItemID: id, MaterialID: "first", ProductIDs: products[:1]}
	rowB := dq.PlanMaterial{AwemeItemID: id, MaterialID: "second", ProductIDs: products[1:]}
	reader := &coverageReader{rows: []dq.PlanMaterial{rowA}, products: products}
	digest, _ := PlanBindingDigest(nil)
	service := BatchService{Reader: reader, Bindings: &memoryPlanBindingStore{}, Reconciler: batchPreflightFinder{bindingDigest: digest, matches: map[string][]ExistingPlan{batchCreatorID: {{AdID: batchPlanID, AwemeID: batchCreatorID, ProductIDs: products}}}}}
	req := BatchRequest{AdvertiserID: batchAdvertiserID, ReadAccessToken: batchToken, TemplateID: "fixture", TemplateName: "fixture", ProductName: "fixture", TemplatePayload: json.RawMessage(strings.Replace(string(batchTemplatePayload()), "["+batchProductID+"]", "["+strings.Join(products, ",")+"]", 1)), Works: works, IncludePayloads: true}
	run := func(request BatchRequest) (BatchResult, BatchGroupResult) {
		t.Helper()
		r, err := service.Execute(ctx, request)
		if err != nil || len(r.Results) != 1 {
			t.Fatalf("preflight: %#v, %v", r, err)
		}
		return r, r.Results[0]
	}
	preview, g := run(req)
	if g.Status != "would_append" || len(g.AlreadyPresent) != 0 || len(g.Writes) != 1 {
		t.Fatalf("partial coverage: %#v", g)
	}
	var payload struct {
		Creatives []struct {
			ProductID json.Number `json:"product_id"`
			Videos    []struct {
				ID json.Number `json:"aweme_item_id"`
			} `json:"video_material"`
		} `json:"multi_product_creative_list"`
	}
	if err := json.Unmarshal(g.Writes[0].Payload, &payload); err != nil || len(payload.Creatives) != 1 || payload.Creatives[0].ProductID.String() != routeProductB || len(payload.Creatives[0].Videos) != 1 || payload.Creatives[0].Videos[0].ID.String() != id {
		t.Fatalf("incorrect delta: %s", g.Writes[0].Payload)
	}
	snapshot, _, err := prepareBatchSnapshot(req, preview, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	recovery := &coverageReadbacks{}
	operation := OperationService{Commands: CommandService{Batch: service}, Recovery: recovery}
	readback := func() error {
		write := operationWrite{ItemID: "append-1", GroupID: g.GroupID, Action: "append", TargetID: batchPlanID, ItemIDs: []string{id}}
		return operation.readback(ctx, ops.Snapshot{SubjectID: batchAdvertiserID}, operationPayload{Preflight: snapshot}, write, "", batchToken, ops.ExecutionLease{})
	}
	if err := readback(); !errors.Is(err, ops.ErrDispatchUnknown) || len(recovery.saved) != 0 {
		t.Fatalf("partial readback: %v", err)
	}
	reader.rows = []dq.PlanMaterial{rowB}
	replay, err := snapshot.batchRequest(req, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	replay.Submit = false
	replay.PreparedGroups = nil
	replay.ReadAccessToken = batchToken
	if _, g := run(replay); g.Status != "preflight_changed" {
		t.Fatalf("replay accepted swapped product coverage: %#v", g)
	}
	reader.rows = []dq.PlanMaterial{rowA, rowB}
	if _, g := run(req); g.Status != "already_present" || !reflect.DeepEqual(g.AlreadyPresent, []string{id}) || len(g.Writes) != 0 {
		t.Fatalf("combined rows: %#v", g)
	}
	if err := readback(); err != nil || len(recovery.saved) != 1 || recovery.saved[0].ObjectID != batchPlanID || recovery.saved[0].EvidenceDigest == "" {
		t.Fatalf("full readback: %+v, %v", recovery.saved, err)
	}
	rowB.MaterialStatus = "DELETED"
	reader.rows = []dq.PlanMaterial{rowA, rowB}
	if err := readback(); !errors.Is(err, ops.ErrDispatchUnknown) || len(recovery.saved) != 1 {
		t.Fatalf("readback counted a deleted product association: %v", err)
	}
	reader.rows = []dq.PlanMaterial{{AwemeItemID: id}}
	if _, g := run(req); g.Status != "failed" || len(g.Writes) != 0 {
		t.Fatalf("unscoped multi-product row: %#v", g)
	}
}
