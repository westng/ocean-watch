package qianchuan

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func TestPlanBindingMatchesOnlyCompleteIdentityAndBusinessDate(t *testing.T) {
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		"1000000000000001", "qcpt_fixture", "4000000000000001",
		[]string{"5000000000000001"}, "随手po", "测试商务",
	)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewPlanBinding(
		identity, groupID, "2026-08-18", "2000000000000001",
		time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !BindingMatchesIdentity(binding, identity, "2026-08-18") ||
		BindingMatchesIdentity(binding, identity, "2026-08-19") {
		t.Fatal("binding business-date identity changed")
	}
	mutations := []domainqianchuan.PlanGroupIdentity{
		{AdvertiserID: "1000000000000002", TemplateID: identity.TemplateID, CreatorID: identity.CreatorID, ProductIDs: identity.ProductIDs, PlanType: identity.PlanType, Business: identity.Business},
		{AdvertiserID: identity.AdvertiserID, TemplateID: "qcpt_other", CreatorID: identity.CreatorID, ProductIDs: identity.ProductIDs, PlanType: identity.PlanType, Business: identity.Business},
		{AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID, CreatorID: "4000000000000002", ProductIDs: identity.ProductIDs, PlanType: identity.PlanType, Business: identity.Business},
		{AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID, CreatorID: identity.CreatorID, ProductIDs: []string{"5000000000000002"}, PlanType: identity.PlanType, Business: identity.Business},
		{AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID, CreatorID: identity.CreatorID, ProductIDs: identity.ProductIDs, PlanType: "真人口播营销", Business: identity.Business},
		{AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID, CreatorID: identity.CreatorID, ProductIDs: identity.ProductIDs, PlanType: identity.PlanType, Business: "其他商务"},
	}
	for index, changed := range mutations {
		if BindingMatchesIdentity(binding, changed, "2026-08-18") {
			t.Fatalf("identity mutation %d reused binding", index)
		}
	}
}

func TestCurrentDayReconcilerRequiresExplicitBindingAndNeverRedirects(t *testing.T) {
	identity, groupID := fixtureBindingIdentity(t)
	target := CreatorTarget{
		AwemeID: identity.CreatorID, VisibleID: batchVisibleID, ProductIDs: identity.ProductIDs,
		GroupID: groupID, BusinessDate: "2026-08-18", Identity: identity,
	}
	matching := domainqianchuan.Plan{
		AdID: batchPlanID, Name: "fixture-plan", Status: "ENABLE",
		Creators: []domainqianchuan.Creator{{AwemeID: identity.CreatorID, VisibleID: batchVisibleID}},
	}
	detail := domainqianchuan.PlanDetail{
		AdID: batchPlanID, Name: matching.Name, Status: "ENABLE", AwemeID: identity.CreatorID,
		Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}},
	}
	inventory := func(plans ...domainqianchuan.Plan) *CurrentPlanInventory {
		return &CurrentPlanInventory{
			StartTime: "2026-08-18 00:00:00", EndTime: "2026-08-18 23:59:59",
			PageCount: 1, Plans: plans,
		}
	}

	t.Run("unique historical candidate still requires binding", func(t *testing.T) {
		reader := &bindingPolicyReader{details: map[string]domainqianchuan.PlanDetail{batchPlanID: detail}}
		result, err := (CurrentDayReconciler{Reader: reader, Bindings: &memoryPlanBindingStore{}}).FindCurrentPlans(
			context.Background(), CurrentPlanRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: batchToken, Targets: []CreatorTarget{target},
				Inventory: inventory(matching),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		policy := result.Policies[groupID]
		if policy.Status != "legacy_binding_required" || len(policy.Candidates) != 1 ||
			len(result.Matches[groupID]) != 0 {
			t.Fatalf("historical candidate was auto-bound: result=%#v", result)
		}
	})

	t.Run("no candidate permits create", func(t *testing.T) {
		reader := &bindingPolicyReader{details: map[string]domainqianchuan.PlanDetail{}}
		result, err := (CurrentDayReconciler{Reader: reader, Bindings: &memoryPlanBindingStore{}}).FindCurrentPlans(
			context.Background(), CurrentPlanRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: batchToken, Targets: []CreatorTarget{target},
				Inventory: inventory(),
			},
		)
		if err != nil || result.Policies[groupID].Status != "would_create" || len(result.Matches[groupID]) != 0 {
			t.Fatalf("empty inventory policy=%#v err=%v", result, err)
		}
	})

	t.Run("exact daily binding selects only its plan", func(t *testing.T) {
		binding, err := NewPlanBinding(identity, groupID, target.BusinessDate, batchPlanID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		store := &memoryPlanBindingStore{}
		if err := store.Put(context.Background(), binding); err != nil {
			t.Fatal(err)
		}
		reader := &bindingPolicyReader{details: map[string]domainqianchuan.PlanDetail{batchPlanID: detail}}
		result, err := (CurrentDayReconciler{Reader: reader, Bindings: store}).FindCurrentPlans(
			context.Background(), CurrentPlanRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: batchToken, Targets: []CreatorTarget{target},
				Inventory: inventory(matching),
			},
		)
		if err != nil || result.Policies[groupID].Status != "bound" ||
			!reflect.DeepEqual(result.Matches[groupID], []ExistingPlan{{
				AdID: batchPlanID, Name: "fixture-plan", Status: "ENABLE",
				AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
			}}) {
			t.Fatalf("bound plan selection=%#v err=%v", result, err)
		}
	})

	t.Run("drift does not redirect to another exact candidate", func(t *testing.T) {
		binding, err := NewPlanBinding(identity, groupID, target.BusinessDate, batchPlanID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		store := &memoryPlanBindingStore{}
		if err := store.Put(context.Background(), binding); err != nil {
			t.Fatal(err)
		}
		alternate := matching
		alternate.AdID = "2000000000000002"
		alternateDetail := detail
		alternateDetail.AdID = alternate.AdID
		reader := &bindingPolicyReader{details: map[string]domainqianchuan.PlanDetail{alternate.AdID: alternateDetail}}
		result, err := (CurrentDayReconciler{Reader: reader, Bindings: store}).FindCurrentPlans(
			context.Background(), CurrentPlanRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: batchToken, Targets: []CreatorTarget{target},
				Inventory: inventory(alternate),
			},
		)
		if err != nil || result.Policies[groupID].Status != "binding_drift" ||
			len(result.Matches[groupID]) != 0 || len(result.Policies[groupID].Candidates) != 1 {
			t.Fatalf("drift redirected to alternate plan: result=%#v err=%v", result, err)
		}
	})
}

func TestCurrentDayReconcilerDeduplicatesPlanDetailsAcrossGroups(t *testing.T) {
	identities := make([]domainqianchuan.PlanGroupIdentity, 0, 2)
	groupIDs := make([]string, 0, 2)
	store := &memoryPlanBindingStore{}
	for _, planType := range []string{"随手po", "真人口播营销"} {
		identity, err := domainqianchuan.NewPlanGroupIdentity(
			batchAdvertiserID, "fixture-template", batchCreatorID,
			[]string{batchProductID}, planType, "刘岛",
		)
		if err != nil {
			t.Fatal(err)
		}
		groupID, err := domainqianchuan.GroupID(identity)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := NewPlanBinding(identity, groupID, "2026-08-18", batchPlanID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(context.Background(), binding); err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		groupIDs = append(groupIDs, groupID)
	}
	reader := &bindingPolicyReader{details: map[string]domainqianchuan.PlanDetail{
		batchPlanID: {
			AdID: batchPlanID, Name: "fixture-plan", Status: "ENABLE", AwemeID: batchCreatorID,
			Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}},
		},
	}}
	pool, err := NewReadPool(4)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]CreatorTarget, 0, len(identities))
	for index, identity := range identities {
		targets = append(targets, CreatorTarget{
			AwemeID: identity.CreatorID, VisibleID: batchVisibleID, ProductIDs: identity.ProductIDs,
			GroupID: groupIDs[index], BusinessDate: "2026-08-18", Identity: identity,
		})
	}
	result, err := (CurrentDayReconciler{Reader: reader, Bindings: store}).FindCurrentPlans(
		context.Background(), CurrentPlanRequest{
			AdvertiserID: batchAdvertiserID, AccessToken: batchToken, Targets: targets, ReadPool: pool,
			Inventory: &CurrentPlanInventory{
				StartTime: "2026-08-18 00:00:00", EndTime: "2026-08-18 23:59:59", PageCount: 1,
				Plans: []domainqianchuan.Plan{{
					AdID: batchPlanID, Name: "fixture-plan", Status: "ENABLE",
					Creators: []domainqianchuan.Creator{{AwemeID: batchCreatorID, VisibleID: batchVisibleID}},
				}},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.detailCalls.Load() != 1 || result.Policies[groupIDs[0]].Status != "bound" || result.Policies[groupIDs[1]].Status != "bound" {
		t.Fatalf("same-plan details were not deduplicated: calls=%d policies=%#v", reader.detailCalls.Load(), result.Policies)
	}
}

func TestCreatedPlanIsNotBoundWhenOfficialReadbackFails(t *testing.T) {
	bindings := &memoryPlanBindingStore{}
	service := BatchService{
		Guard: sharedplans.GuardedExecutor{
			Credentials: &batchCredentialProvider{}, Locks: &batchLocker{},
		},
		Reader: &commandReader{}, Writer: &commandSuccessfulWriter{},
		Reconciler: commandNoPlanFinder{}, Bindings: bindings,
		Now: func() time.Time { return time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC) },
	}
	result, err := service.Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: batchVerifiedWorks(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].Status != "create_unverified" ||
		len(bindings.bindings) != 0 {
		t.Fatalf("unverified plan created a binding: result=%#v bindings=%#v", result, bindings.bindings)
	}
}

func TestUnknownPlanCreateIsNotBoundWithoutSuccessfulReconciliation(t *testing.T) {
	bindings := &memoryPlanBindingStore{}
	writer := &bindingUnknownCreateWriter{}
	service := BatchService{
		Guard: sharedplans.GuardedExecutor{
			Credentials: &batchCredentialProvider{}, Locks: &batchLocker{},
		},
		Reader: &commandReader{}, Writer: writer,
		Reconciler: commandNoPlanFinder{}, Bindings: bindings,
		Now: func() time.Time { return time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC) },
	}
	result, err := service.Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: batchVerifiedWorks(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || len(result.Results) != 1 || result.Results[0].Status != "create_failed" ||
		len(bindings.bindings) != 0 {
		t.Fatalf("unknown create produced a binding: result=%#v bindings=%#v writes=%d", result, bindings.bindings, writer.calls)
	}
}

func TestCurrentPlanScanUsesExplicitBusinessDate(t *testing.T) {
	reader := &bindingScanDateReader{}
	reconciler := CurrentDayReconciler{Reader: reader, Now: func() time.Time {
		return time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	}}
	result, err := reconciler.ScanCurrentPlans(context.Background(), CurrentPlanScanRequest{
		AdvertiserID: batchAdvertiserID, AccessToken: batchToken, BusinessDate: "2026-08-18",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StartTime != "2026-08-18 00:00:00" || result.EndTime != "2026-08-18 23:59:59" ||
		reader.request.StartTime != result.StartTime || reader.request.EndTime != result.EndTime {
		t.Fatalf("explicit business date was ignored: result=%#v request=%#v", result, reader.request)
	}
}

type bindingPolicyReader struct {
	batchStateReader
	details     map[string]domainqianchuan.PlanDetail
	detailCalls atomic.Int32
}

type bindingScanDateReader struct {
	batchStateReader
	request portqianchuan.PlanPageRequest
}

func (reader *bindingScanDateReader) FetchPlans(_ context.Context, request portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	reader.request = request
	return domainqianchuan.PlanPage{
		Rows:     []domainqianchuan.Plan{},
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 0, TotalNumber: 0},
	}, nil
}

type bindingUnknownCreateWriter struct{ calls int }

func (writer *bindingUnknownCreateWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	writer.calls++
	return portqianchuan.WriteResult{}, &domainplans.DispatchFailure{
		State: domainplans.DispatchUnknown, Cause: errors.New("fixture response loss"),
	}
}

func (*bindingUnknownCreateWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material add")
}

func (*bindingUnknownCreateWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material delete")
}

func (*bindingUnknownCreateWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan mutation")
}

func (reader *bindingPolicyReader) FetchPlanDetail(_ context.Context, request portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	reader.detailCalls.Add(1)
	detail, exists := reader.details[request.AdID]
	if !exists {
		return domainqianchuan.PlanDetail{}, errors.New("fixture plan detail not found")
	}
	return detail, nil
}

func fixtureBindingIdentity(t *testing.T) (domainqianchuan.PlanGroupIdentity, string) {
	t.Helper()
	identity, err := domainqianchuan.NewPlanGroupIdentity(
		batchAdvertiserID, "fixture-template", batchCreatorID,
		[]string{batchProductID}, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	groupID, err := domainqianchuan.GroupID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return identity, groupID
}

var _ portqianchuan.Reader = (*bindingPolicyReader)(nil)
