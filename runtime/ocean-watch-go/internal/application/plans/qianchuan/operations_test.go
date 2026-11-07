package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/sqlite"
	applicationops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	dq "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	templates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

// Synthetic trust root exists only in tests. Production cannot select it.
type operationTestApproval struct {
	now    func() time.Time
	reject bool
	last   approval.Evidence
	calls  int
}

func (*operationTestApproval) ProviderID() string  { return "fixture" }
func (*operationTestApproval) AdapterID() string   { return "fixture" }
func (*operationTestApproval) HostVersion() string { return "fixture" }
func (a *operationTestApproval) Request(_ context.Context, r approval.Request) error {
	a.calls++
	if a.reject {
		return approval.ErrNotApproved
	}
	a.last = approval.Evidence{ApprovalID: fmt.Sprintf("ap_fixture_%d", a.calls), ProviderID: "fixture", AdapterID: "fixture", HostVersion: "fixture", OperationID: r.OperationID, OperationVersion: r.Version, SnapshotDigest: r.SnapshotDigest, ScopeDigest: r.ScopeDigest, SourceReference: fmt.Sprintf("event_fixture_%d", a.calls), Nonce: strings.Repeat(fmt.Sprintf("%x", a.calls), 64), IssuedAt: a.now(), ExpiresAt: r.ExpiresAt}
	return nil
}
func (a *operationTestApproval) Receive(context.Context, approval.Request) (approval.Evidence, error) {
	return a.last, nil
}
func (a *operationTestApproval) Discard(approval.Request) { a.last = approval.Evidence{} }
func (a *operationTestApproval) VerifyEvidence(_ context.Context, e approval.Evidence) (approval.EvidenceBinding, error) {
	if e != a.last {
		return approval.EvidenceBinding{}, approval.ErrUntrustedEvidence
	}
	nonce, err := ops.NonceDigest(e.Nonce)
	if err != nil {
		return approval.EvidenceBinding{}, err
	}
	digest, err := ops.EvidenceDigest(ops.ApprovalRecord{ID: e.ApprovalID, OperationID: e.OperationID, Version: e.OperationVersion, SnapshotDigest: e.SnapshotDigest, ScopeDigest: e.ScopeDigest, ProviderID: e.ProviderID, AdapterID: e.AdapterID, HostVersion: e.HostVersion, NonceHash: nonce, SourceReference: e.SourceReference, IssuedAt: e.IssuedAt, ExpiresAt: e.ExpiresAt, Status: ops.ApprovalValid})
	return approval.EvidenceBinding{Decision: "approved", EvidenceHash: digest, NonceHash: nonce}, err
}

type operationTestIO struct {
	batchStateReader
	details      map[string]dq.PlanDetail
	calls        int
	lose         bool
	hide         bool
	failAt       int
	works        []VerifiedWork
	appendTarget string
	wrongBudget  bool
}

func (x *operationTestIO) FetchAuthorizedCreators(context.Context, port.AuthorizedCreatorPageRequest) (dq.AuthorizedCreatorPage, error) {
	rows := []dq.AuthorizedCreator{}
	for _, w := range x.works {
		rows = append(rows, w.Creator)
	}
	return dq.AuthorizedCreatorPage{Rows: rows, PageInfo: dq.PageInfo{Page: 1, TotalPages: 1, TotalNumber: len(rows)}}, nil
}
func (x *operationTestIO) FetchCreatorVideos(_ context.Context, r port.CreatorVideoPageRequest) (dq.CreatorVideoPage, error) {
	rows := []dq.CreatorVideo{}
	for _, w := range x.works {
		if w.Creator.AwemeID == r.AwemeID {
			rows = append(rows, w.Material)
		}
	}
	return dq.CreatorVideoPage{Rows: rows}, nil
}

func (x *operationTestIO) FetchPlanDetail(_ context.Context, r port.PlanDetailRequest) (dq.PlanDetail, error) {
	if x.hide {
		return dq.PlanDetail{}, errors.New("synthetic readback unavailable")
	}
	d, ok := x.details[r.AdID]
	if !ok {
		return d, errors.New("no synthetic plan")
	}
	return d, nil
}
func (x *operationTestIO) CreatePlan(_ context.Context, r port.CreatePlanRequest) (port.WriteResult, error) {
	x.calls++
	if x.failAt == x.calls {
		return port.WriteResult{}, &domainplans.DispatchFailure{State: domainplans.DispatchNotSent, Cause: errors.New("fixture not sent")}
	}
	var p struct {
		Name     string        `json:"name"`
		Creator  json.Number   `json:"aweme_id"`
		Products []json.Number `json:"product_ids"`
		Settings struct {
			Budget domain.Decimal `json:"budget"`
			ROI    domain.Decimal `json:"roi2_goal"`
			Bid    string         `json:"smart_bid_type"`
		} `json:"delivery_setting"`
	}
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return port.WriteResult{}, err
	}
	id := fmt.Sprintf("200000000000%04d", x.calls)
	products := []dq.PlanProduct{}
	for _, v := range p.Products {
		products = append(products, dq.PlanProduct{ProductID: string(v)})
	}
	if x.wrongBudget {
		p.Settings.Budget = domain.MustDecimal("1")
	}
	x.details[id] = dq.PlanDetail{AdID: id, AwemeID: string(p.Creator), Name: p.Name, Status: "ENABLE", Products: products, Budget: &p.Settings.Budget, ROI2Goal: &p.Settings.ROI, SmartBidType: p.Settings.Bid}
	x.apply(r.Payload)
	if x.lose {
		return port.WriteResult{}, errors.New("synthetic lost response")
	}
	return port.WriteResult{ObjectID: id, RequestID: "request_fixture"}, nil
}
func (x *operationTestIO) AddMaterials(_ context.Context, r port.MaterialWriteRequest) (port.WriteResult, error) {
	x.calls++
	x.apply(r.Payload)
	return port.WriteResult{ObjectID: r.AdID, RequestID: "request_fixture"}, nil
}
func (*operationTestIO) DeleteMaterials(context.Context, port.DeleteMaterialsRequest) (port.WriteResult, error) {
	panic("unexpected delete")
}
func (*operationTestIO) UpdatePlan(context.Context, port.MutationRequest) (port.WriteResult, error) {
	panic("unexpected update")
}
func (x *operationTestIO) apply(raw json.RawMessage) {
	var p struct {
		Creatives []struct {
			Videos []struct {
				ID json.Number `json:"aweme_item_id"`
			} `json:"video_material"`
		} `json:"multi_product_creative_list"`
	}
	_ = json.Unmarshal(raw, &p)
	for _, c := range p.Creatives {
		for _, v := range c.Videos {
			id := string(v.ID)
			x.materials[id] = dq.PlanMaterial{MaterialID: "material-" + id, AwemeItemID: id, MaterialType: "VIDEO", MaterialSelectType: "CUSTOM", MaterialStatus: "DELIVERY_OK"}
		}
	}
}
func (x *operationTestIO) FindCurrentPlans(ctx context.Context, r CurrentPlanRequest) (CurrentPlanResult, error) {
	if x.appendTarget != "" {
		out := CurrentPlanResult{Matches: map[string][]ExistingPlan{}, Policies: map[string]PlanMatchPolicy{}}
		digest, _ := PlanBindingDigest(nil)
		for _, target := range r.Targets {
			out.Matches[target.GroupID] = []ExistingPlan{{AdID: x.appendTarget, Name: "fixture-existing", AwemeID: target.AwemeID, ProductIDs: target.ProductIDs}}
			out.Policies[target.GroupID] = PlanMatchPolicy{Status: "unbound_existing", BindingDigest: digest}
		}
		return out, nil
	}
	return commandNoPlanFinder{}.FindCurrentPlans(ctx, r)
}
func (x *operationTestIO) ScanCurrentPlans(context.Context, CurrentPlanScanRequest) (CurrentPlanInventory, error) {
	if x.hide {
		return CurrentPlanInventory{}, errors.New("synthetic inventory failure")
	}
	out := CurrentPlanInventory{}
	for _, d := range x.details {
		out.Plans = append(out.Plans, dq.Plan{AdID: d.AdID, Name: d.Name})
	}
	return out, nil
}

type operationFixture struct {
	service   OperationService
	io        *operationTestIO
	approval  *operationTestApproval
	store     *sqlite.Store
	path      string
	now       time.Time
	preflight string
	config    map[string]any
}

func newOperationFixture(t *testing.T, count int) *operationFixture {
	t.Helper()
	f := &operationFixture{now: time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC), preflight: "qianchuan-preflight-20261107t010000-abcdef123456", config: commandProductConfig()}
	f.path = filepath.Join(t.TempDir(), "private", "state.db")
	store, err := sqlite.Initialize(context.Background(), sqlite.Options{Path: f.path})
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	t.Cleanup(func() { _ = f.store.Close() })
	f.io = &operationTestIO{batchStateReader: batchStateReader{materials: map[string]dq.PlanMaterial{}}, details: map[string]dq.PlanDetail{}}
	exported, err := templates.ExportQianchuanPlanPayload(f.config, templates.QianchuanTemplateProduct, "qcpt_command", "")
	if err != nil {
		t.Fatal(err)
	}
	works := batchVerifiedWorks(count)
	for i := range works {
		works[i].Creator.AwemeID = fmt.Sprintf("400000000000%04d", i+1)
	}
	f.io.works = works
	request := BatchRequest{AdvertiserID: exported.AdvertiserID, TemplateID: exported.TemplateID, TemplateName: exported.DisplayName, ProductName: exported.ProductName, ProductShortName: exported.ProductShortName, PlanNameTemplate: exported.PlanNameTemplate, TemplatePayload: exported.Payload, Works: works, ReadAccessToken: batchToken, SelectionPolicy: PlanSelectionUniqueExisting}
	batch := BatchService{Reader: f.io, Writer: f.io, Reconciler: f.io, Bindings: &memoryPlanBindingStore{}, Now: func() time.Time { return f.now }, Guard: sharedplans.GuardedExecutor{Locks: commandLocker{}, Now: func() time.Time { return f.now }, RuntimeVersion: "1.1.0-alpha.1"}}
	preview, err := batch.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	prepared, _, err := prepareBatchSnapshot(request, preview, f.now)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := batchPreflightJournal(prepared, f.now)
	if err != nil {
		t.Fatal(err)
	}
	commands := CommandService{Config: commandConfigReader{config: f.config}, Tokens: &commandTokenProvider{}, Journals: &commandJournalStore{journals: map[string]domainplans.Journal{f.preflight: journal}}, Batch: batch, Locks: commandLocker{}, Now: func() time.Time { return f.now }}
	f.approval = &operationTestApproval{now: func() time.Time { return f.now }}
	verifier, _ := approval.NewVerifier(f.approval)
	instance, _ := policy.New(policy.Config{Mode: "execute", Toolsets: []string{"core", "qianchuan"}})
	f.service = OperationService{Commands: commands, Core: applicationops.Service{Repository: store, Snapshots: store, Provider: f.approval, Evidence: f.approval, Verifier: verifier, Now: func() time.Time { return f.now }}, Recovery: store, Policy: instance, RuntimeVersion: "1.1.0-alpha.1"}
	return f
}
func (f *operationFixture) prepare(t *testing.T) OperationReference {
	t.Helper()
	v, err := f.service.Prepare(context.Background(), f.preflight)
	if err != nil {
		t.Fatal(err)
	}
	if f.io.calls != 0 {
		t.Fatal("prepare dispatched")
	}
	return v.OperationReference
}
func (f *operationFixture) confirm(t *testing.T, r OperationReference) string {
	t.Helper()
	a, err := f.service.Confirm(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return a.ApprovalID
}

func TestOperationConfirmedCreateReadbackAndReplay(t *testing.T) {
	f := newOperationFixture(t, 1)
	ref := f.prepare(t)
	if _, err := f.service.Execute(context.Background(), ref, "ap_fake"); err == nil || f.io.calls != 0 {
		t.Fatal("fake approval dispatched")
	}
	id := f.confirm(t, ref)
	f.now = f.now.Add(time.Minute)
	view, err := f.service.Execute(context.Background(), ref, id)
	if err != nil || view.Status != ops.StatusSucceeded || f.io.calls != 1 || !view.Items[0].Applied {
		t.Fatalf("execution: %+v %v calls=%d", view, err, f.io.calls)
	}
	if _, err := f.service.Execute(context.Background(), ref, id); err == nil || f.io.calls != 1 {
		t.Fatal("approval replayed")
	}
	if _, err := f.service.Reconcile(context.Background(), ref); err != nil || f.io.calls != 1 {
		t.Fatal("terminal recovery wrote")
	}
}
func TestOperationUnknownSurvivesRestartAndReconcilesWithoutWrite(t *testing.T) {
	f := newOperationFixture(t, 1)
	ref := f.prepare(t)
	id := f.confirm(t, ref)
	f.io.lose = true
	f.io.hide = true
	view, err := f.service.Execute(context.Background(), ref, id)
	if err == nil || view.Status != ops.StatusUnknown || f.io.calls != 1 {
		t.Fatalf("unknown: %+v %v", view, err)
	}
	_ = f.store.Close()
	f.store, err = sqlite.Open(context.Background(), sqlite.Options{Path: f.path})
	if err != nil {
		t.Fatal(err)
	}
	f.service.Core.Repository = f.store
	f.service.Core.Snapshots = f.store
	f.service.Recovery = f.store
	f.now = f.now.Add(time.Second)
	f.io.hide = false
	view, err = f.service.Reconcile(context.Background(), ref)
	if err != nil || view.Status != ops.StatusSucceeded || f.io.calls != 1 {
		t.Fatalf("recovery: %+v %v", view, err)
	}
}
func TestOperationPartialFailureDoesNotResumeUnsentWork(t *testing.T) {
	f := newOperationFixture(t, 2)
	ref := f.prepare(t)
	id := f.confirm(t, ref)
	f.io.failAt = 2
	view, err := f.service.Execute(context.Background(), ref, id)
	if err == nil || view.Status != ops.StatusPartiallySucceeded || f.io.calls != 2 {
		t.Fatalf("partial: %+v %v", view, err)
	}
	f.now = f.now.Add(time.Second)
	view, err = f.service.Reconcile(context.Background(), ref)
	if err != nil || view.Status != ops.StatusPartiallySucceeded || f.io.calls != 2 {
		t.Fatalf("partial recovery: %+v %v", view, err)
	}
}
func TestOperationCreateReadbackMustMatchBudget(t *testing.T) {
	f := newOperationFixture(t, 1)
	ref := f.prepare(t)
	approvalID := f.confirm(t, ref)
	f.io.wrongBudget = true
	v, err := f.service.Execute(context.Background(), ref, approvalID)
	if !errors.Is(err, ops.ErrDispatchUnknown) || v.Status != ops.StatusUnknown || v.Items[0].Applied {
		t.Fatalf("wrong budget reported applied: %+v %v", v, err)
	}
}

func TestOperationAppendAndMaterialDrift(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint(drift), func(t *testing.T) {
			f := newOperationFixture(t, 1)
			f.io.appendTarget = batchPlanID
			f.io.details[batchPlanID] = dq.PlanDetail{AdID: batchPlanID, Name: "fixture-existing", AwemeID: batchCreatorID, Status: "ENABLE", Products: []dq.PlanProduct{{ProductID: batchProductID}}}
			store := f.service.Commands.Journals.(*commandJournalStore)
			p, err := decodeBatchPreflight(store.journals[f.preflight], f.now)
			if err != nil {
				t.Fatal(err)
			}
			for id, g := range p.Groups {
				g.ExpectedAction = "would_append"
				g.ExpectedAdID = batchPlanID
				p.Groups[id] = g
			}
			store.journals[f.preflight], err = batchPreflightJournal(p, f.now)
			if err != nil {
				t.Fatal(err)
			}
			ref := f.prepare(t)
			approvalID := f.confirm(t, ref)
			if drift {
				f.io.materials[batchWorkID(1)] = dq.PlanMaterial{MaterialID: "fixture", AwemeItemID: batchWorkID(1), MaterialType: "VIDEO", MaterialStatus: "DELIVERY_OK", MaterialSelectType: "CUSTOM"}
			}
			v, err := f.service.Execute(context.Background(), ref, approvalID)
			if drift {
				if err == nil || f.io.calls != 0 {
					t.Fatalf("material drift dispatched: %+v %v", v, err)
				}
			} else if err != nil || v.Status != ops.StatusSucceeded || f.io.calls != 1 {
				t.Fatalf("append failed: %+v %v", v, err)
			}
		})
	}
}

func TestOperationUnresolvedGroupBlocksNewOperation(t *testing.T) {
	f := newOperationFixture(t, 1)
	first := f.prepare(t)
	a := f.confirm(t, first)
	f.io.lose = true
	f.io.hide = true
	if _, err := f.service.Execute(context.Background(), first, a); err == nil {
		t.Fatal("expected unresolved readback")
	}
	store := f.service.Commands.Journals.(*commandJournalStore)
	store.journals["qianchuan-preflight-20261107t010000-654321abcdef"] = store.journals[f.preflight]
	f.preflight = "qianchuan-preflight-20261107t010000-654321abcdef"
	v, err := f.service.Prepare(context.Background(), f.preflight)
	if err != nil {
		t.Fatal(err)
	}
	a = f.confirm(t, v.OperationReference)
	if _, err = f.service.Execute(context.Background(), v.OperationReference, a); !errors.Is(err, ops.ErrDispatchUnknown) || f.io.calls != 1 {
		t.Fatalf("new operation bypassed unknown: %v calls=%d", err, f.io.calls)
	}
}

func TestOperationPrepareIdempotentAndInspectionHasNoOfficialReads(t *testing.T) {
	f := newOperationFixture(t, 1)
	ref := f.prepare(t)
	f.service.Commands.Tokens = nil
	second, err := f.service.Prepare(context.Background(), f.preflight)
	if err != nil || second.OperationReference != ref {
		t.Fatalf("idempotent prepare: %+v %v", second, err)
	}
	if _, err = f.service.Get(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestOperationRejectionExpiryDriftAndPolicyPreventWrites(t *testing.T) {
	for _, scenario := range []string{"reject", "expired", "template", "digest", "policy", "runtime", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOperationFixture(t, 1)
			ref := f.prepare(t)
			if scenario == "reject" {
				f.approval.reject = true
				if _, err := f.service.Confirm(context.Background(), ref); err == nil {
					t.Fatal("rejection ignored")
				}
				return
			}
			id := f.confirm(t, ref)
			ctx := context.Background()
			switch scenario {
			case "expired":
				f.now = f.now.Add(6 * time.Minute)
			case "template":
				f.config["qianchuan_product_templates"].(map[string]any)["qcpt_command"].(map[string]any)["delivery_setting"].(map[string]any)["budget"] = 6000
			case "digest":
				ref.SnapshotDigest = "sha256:" + strings.Repeat("0", 64)
			case "policy":
				f.service.Policy = policy.Default()
			case "runtime":
				f.service.RuntimeVersion = "1.1.0-alpha.2"
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := f.service.Execute(ctx, ref, id); err == nil || f.io.calls != 0 {
				t.Fatalf("%s dispatched or was accepted: %v", scenario, err)
			}
		})
	}
}
