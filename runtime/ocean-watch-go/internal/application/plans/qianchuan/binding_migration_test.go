package qianchuan

import (
	"context"
	"testing"
	"time"
)

func TestBindingMigrationRequiresExactConfirmationAndWritesOnlyOnSubmit(t *testing.T) {
	identity, groupID := fixtureBindingIdentity(t)
	command := BindingAuditCommand{
		AdvertiserID: identity.AdvertiserID, AuthAccountID: "fixture-auth",
		TemplateID: identity.TemplateID, CreatorID: identity.CreatorID, CreatorVisibleID: batchVisibleID,
		ProductIDs: identity.ProductIDs, PlanType: identity.PlanType, Business: identity.Business,
		BusinessDate: "2026-08-18",
	}
	tokens := &commandTokenProvider{}
	finder := &bindingMigrationFinder{candidate: ExistingPlan{
		AdID: batchPlanID, Name: "fixture-plan", Status: "ENABLE",
		AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
	}}
	bindings := &memoryPlanBindingStore{}
	locks := &batchLocker{}
	service := BindingMigrationService{
		Tokens: tokens, Reconciler: finder, Bindings: bindings, Locks: locks,
		Now: func() time.Time { return time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC) },
	}
	audit, err := service.Audit(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if audit.GroupID != groupID || audit.Status != "legacy_binding_required" ||
		len(audit.Candidates) != 1 || finder.lastTarget.BusinessDate != command.BusinessDate {
		t.Fatalf("binding audit=%#v target=%#v", audit, finder.lastTarget)
	}
	if _, err := service.Bind(context.Background(), BindPlanCommand{
		BindingAuditCommand: command, GroupID: "qcg_invalid", AdID: batchPlanID,
	}); err == nil {
		t.Fatal("mismatched group confirmation was accepted")
	}
	preview, err := service.Bind(context.Background(), BindPlanCommand{
		BindingAuditCommand: command, GroupID: groupID, AdID: batchPlanID,
	})
	if err != nil || preview.Mode != "dry_run" || preview.Status != "would_bind" ||
		len(bindings.bindings) != 0 || locks.calls != 0 {
		t.Fatalf("binding dry-run crossed write boundary: result=%#v bindings=%#v locks=%d err=%v", preview, bindings.bindings, locks.calls, err)
	}
	bound, err := service.Bind(context.Background(), BindPlanCommand{
		BindingAuditCommand: command, GroupID: groupID, AdID: batchPlanID, Submit: true,
	})
	if err != nil || bound.Mode != "submit" || bound.Status != "bound" || bound.Binding == nil ||
		len(bindings.bindings) != 1 || locks.calls != 1 || locks.releases != 1 {
		t.Fatalf("binding submit=%#v bindings=%#v locks=%#v err=%v", bound, bindings.bindings, locks, err)
	}
	if tokens.calls != 3 {
		t.Fatalf("binding migration token resolutions=%d want=3", tokens.calls)
	}
}

func TestBindingMigrationRejectsUnlistedAdIDWithoutWriting(t *testing.T) {
	identity, groupID := fixtureBindingIdentity(t)
	bindings := &memoryPlanBindingStore{}
	service := BindingMigrationService{
		Tokens: &commandTokenProvider{},
		Reconciler: &bindingMigrationFinder{candidate: ExistingPlan{
			AdID: batchPlanID, AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
		}},
		Bindings: bindings, Locks: &batchLocker{},
	}
	_, err := service.Bind(context.Background(), BindPlanCommand{
		BindingAuditCommand: BindingAuditCommand{
			AdvertiserID: identity.AdvertiserID, TemplateID: identity.TemplateID,
			CreatorID: identity.CreatorID, ProductIDs: identity.ProductIDs,
			BusinessDate: "2026-08-18",
		},
		GroupID: groupID, AdID: "2000000000000002", Submit: true,
	})
	if err == nil || len(bindings.bindings) != 0 {
		t.Fatalf("unlisted ad_id was bound: err=%v bindings=%#v", err, bindings.bindings)
	}
}

type bindingMigrationFinder struct {
	candidate  ExistingPlan
	lastTarget CreatorTarget
}

func (finder *bindingMigrationFinder) FindCurrentPlans(_ context.Context, request CurrentPlanRequest) (CurrentPlanResult, error) {
	finder.lastTarget = request.Targets[0]
	groupID := finder.lastTarget.GroupID
	return CurrentPlanResult{
		StartTime: finder.lastTarget.BusinessDate + " 00:00:00",
		EndTime:   finder.lastTarget.BusinessDate + " 23:59:59", PageCount: 1,
		Matches: map[string][]ExistingPlan{groupID: {}},
		Policies: map[string]PlanMatchPolicy{groupID: {
			Status: "legacy_binding_required", Candidates: []ExistingPlan{finder.candidate},
		}},
	}, nil
}
