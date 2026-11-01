package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

var workflowTime = time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)

func TestPreflightCreatorProgressDoesNotWaitForAnotherUID(t *testing.T) {
	const slowUID = "4000000000000002"
	slowStarted, releaseSlow, fastMaterials := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSlow) }) }
	defer release()
	reader := newWorkflowReader()
	reader.creators["slow-visible"] = slowUID
	reader.authorization = func(ctx context.Context, request portqianchuan.AuthorizedCreatorPageRequest) error {
		if request.SearchKeyword != "slow-visible" {
			return nil
		}
		close(slowStarted)
		select {
		case <-releaseSlow:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	reader.materialRead = func(request portqianchuan.MaterialPageRequest) { close(fastMaterials) }
	reader.addPlan(batchPlanID, batchCreatorID)
	service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{
		workflowLink(0, "6000000000000001", slowUID, "slow-visible"),
		workflowLink(1, "6000000000000002", batchCreatorID, batchVisibleID),
	})
	type outcome struct {
		result BatchCommandResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.BatchWorks(ctx, BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL, links.result.Resolved[1].InputURL)})
		done <- outcome{result, err}
	}()
	select {
	case <-slowStarted:
	case <-ctx.Done():
		t.Fatal("slow UID was not queried")
	}
	select {
	case <-fastMaterials:
	case <-ctx.Done():
		t.Fatal("ready UID waited for the slow UID before its material diff")
	}
	release()
	value := <-done
	if value.err != nil || len(value.result.Results) != 2 || value.result.Results[0].AwemeID != slowUID || value.result.Results[0].Status != "would_create" || value.result.Results[1].Status != "would_append" {
		t.Fatalf("creator decisions/order=%#v err=%v", value.result.Results, value.err)
	}
	if links.calls != 1 || writer.addCalls != 0 || reader.count("plans") != 1 || reader.count("detail:"+batchPlanID) != 1 {
		t.Fatal("preflight repeated metadata/inventory/details or wrote materials")
	}
}

func TestRoutedPreflightSharesFactsOnlyWithinOneAccountGrant(t *testing.T) {
	for _, separateGrants := range []bool{false, true} {
		t.Run(fmt.Sprint(separateGrants), func(t *testing.T) {
			reader := newWorkflowReader()
			service, links, _ := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
			links.result.Skipped = []domain.SkippedWorkLink{{InputIndex: 1, AwemeItemID: "6000000000000001", InputURL: links.result.Resolved[0].InputURL, Reason: "duplicate_input"}}
			config := commandProductConfig()
			config["qianchuan_product_templates"].(map[string]any)["qcpt_second"] = routingTestTemplate("qcpt_second", batchAdvertiserID, batchProductID, "测试商品")
			service.Config = commandConfigReader{config: config}
			tokens := &routingTestTokens{}
			service.Tokens = tokens
			items := []domainqianchuan.BatchItem{
				{InputIndex: 0, WorkURL: links.result.Resolved[0].InputURL, PlanTemplate: "qcpt_command"},
				{InputIndex: 1, WorkURL: links.result.Resolved[0].InputURL, PlanTemplate: "qcpt_second"},
			}
			if separateGrants {
				items[0].AuthAccountID, items[1].AuthAccountID = "grant-a", "grant-b"
			}
			result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: items})
			if err != nil || len(result.Batches) != 2 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			for _, batch := range result.Batches {
				if batch.Result == nil || len(batch.Result.Results) != 1 || batch.Result.Results[0].Status != "would_create" || batch.Result.PreflightID == "" {
					t.Fatalf("shared input was lost in a template batch: %#v", batch)
				}
			}
			want := 1
			if separateGrants {
				want = 2
			}
			if len(tokens.queries) != want || reader.count("plans") != want || reader.count("authorization:"+batchVisibleID) != want || reader.count("videos:") != want || reader.count("videos:"+batchProductID) != want {
				t.Fatalf("read sharing crossed a grant or repeated facts: token=%d counts=%v", len(tokens.queries), reader.counts)
			}
			if result.Batches[0].Result.PreflightID == result.Batches[1].Result.PreflightID {
				t.Fatal("templates shared a submit snapshot")
			}
		})
	}
}

func TestCreatorPreflightFailureKeepsItsCauseAndBlocksSubmission(t *testing.T) {
	reader := newWorkflowReader()
	reader.addPlan(batchPlanID, batchCreatorID)
	detail := reader.details[batchPlanID]
	detail.AwemeID = ""
	reader.details[batchPlanID] = detail
	service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL)})
	if err != nil || len(result.Results) != 1 || len(result.FailedResults) != 1 || result.Results[0].Status != "failed" {
		t.Fatalf("incomplete plan identity escaped the creator boundary: result=%#v err=%v", result, err)
	}
	var stage *PreflightStageError
	var inventory *PlanInventoryError
	if !errors.As(result.FailedResults[0].Failure, &stage) || stage.Stage != "plan_reconciliation" ||
		!errors.As(result.FailedResults[0].Failure, &inventory) || inventory.Reason != "identity_incomplete" {
		t.Fatal("creator failure lost its stage or underlying identity reason")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "cause") || strings.Contains(string(encoded), "plan detail did not establish") {
		t.Fatal("private failure cause was serialized")
	}
	summary, err := service.GetBatchPreflight(context.Background(), result.PreflightID)
	if err != nil || summary.ReadyForSubmit || summary.EligibleWorks != 0 || writer.addCalls != 0 || writer.createCalls != 0 {
		t.Fatalf("incomplete identity became an executable snapshot: summary=%#v err=%v", summary, err)
	}
}

func TestSingleTemplatePreflightLimitsAccountWorkBeforeHTTPAdmission(t *testing.T) {
	reader := newWorkflowReader()
	for index := range 12 {
		reader.addPlan(fmt.Sprint(2000000000000000+index), "4000000000000002")
	}
	var active, peak atomic.Int64
	reader.detailRead = func(ctx context.Context, _ portqianchuan.PlanDetailRequest) error {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
		}
		select {
		case <-time.After(5 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Concurrency: 8, Items: batchCommandItems(links.result.Resolved[0].InputURL)})
	if err != nil || len(result.Results) != 1 || result.Results[0].Status != "would_create" || peak.Load() != 2 || active.Load() != 0 || writer.createCalls != 0 {
		t.Fatalf("single-template account work escaped admission capacity: peak=%d active=%d result=%#v err=%v", peak.Load(), active.Load(), result, err)
	}
}

func TestUniqueExistingPlanPreflightAndConfirmedAdoption(t *testing.T) {
	reader := newWorkflowReader()
	reader.addPlan(batchPlanID, batchCreatorID)
	service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
	bindings := service.Batch.Bindings.(*memoryPlanBindingStore)
	preview, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL), IncludePayloads: true})
	if err != nil || len(preview.Results) != 1 || preview.Results[0].Status != "would_append" || preview.Results[0].AdID != batchPlanID || len(bindings.bindings) != 0 || writer.addCalls != 0 {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	summary, err := service.GetBatchPreflight(context.Background(), preview.PreflightID)
	if err != nil || summary.SchemaVersion != 3 || !summary.ReadyForSubmit {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	beforeAuthorization := reader.count("authorization:" + batchVisibleID)
	submitted, err := service.BatchWorks(context.Background(), BatchWorksCommand{PreflightID: preview.PreflightID, Submit: true})
	if err != nil || len(submitted.Results) != 1 || submitted.Results[0].Status != "appended" || writer.addCalls != 1 || writer.createCalls != 0 || len(bindings.bindings) != 1 || links.calls != 1 || reader.count("authorization:"+batchVisibleID) != beforeAuthorization {
		t.Fatalf("confirmed adoption=%#v writer=%#v bindings=%v err=%v", submitted, writer, bindings, err)
	}
	if reader.count("detail:"+batchPlanID) != 3 {
		t.Fatal("adoption omitted fresh post-write identity verification")
	}
	_, err = service.BatchWorks(context.Background(), BatchWorksCommand{PreflightID: preview.PreflightID, Submit: true})
	if err != nil || writer.addCalls != 1 {
		t.Fatal("a repeated snapshot replayed an append")
	}
	second, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL)})
	if err != nil || second.Results[0].Status != "already_present" || writer.addCalls != 1 {
		t.Fatalf("repeat preflight=%#v err=%v", second, err)
	}
}

func TestExistingPlanSubmitRejectsChangedTargetsOrMissingBindingWriter(t *testing.T) {
	for _, scenario := range []string{"multiple", "changed_uid", "changed_materials", "missing_binding_writer"} {
		t.Run(scenario, func(t *testing.T) {
			reader := newWorkflowReader()
			reader.addPlan(batchPlanID, batchCreatorID)
			service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
			preview, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL)})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "multiple":
				reader.addPlan("2000000000000002", batchCreatorID)
			case "changed_uid":
				value := reader.details[batchPlanID]
				value.AwemeID = "4000000000000002"
				reader.details[batchPlanID] = value
			case "changed_materials":
				reader.materials[batchPlanID] = []domainqianchuan.PlanMaterial{{AwemeItemID: "6000000000000001", MaterialID: "fixture-existing"}}
			case "missing_binding_writer":
				service.Batch.Bindings = nil
			}
			result, err := service.BatchWorks(context.Background(), BatchWorksCommand{PreflightID: preview.PreflightID, Submit: true})
			want := "preflight_changed"
			if scenario == "missing_binding_writer" {
				want = "binding_failed"
			}
			if err != nil || len(result.Results) != 1 || result.Results[0].Status != want || writer.addCalls != 0 || writer.createCalls != 0 {
				t.Fatalf("unsafe submission: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestExistingUnboundPlanRequiresExactSnapshotEvenForDirectSubmit(t *testing.T) {
	reader := newWorkflowReader()
	reader.addPlan(batchPlanID, batchCreatorID)
	service, _, writer := workflowService(reader, nil)
	service.Batch.Guard.Credentials = &batchCredentialProvider{}
	result, err := service.Batch.Execute(context.Background(), BatchRequest{AdvertiserID: batchAdvertiserID, Submit: true, TemplateID: "fixture-template", TemplateName: "fixture-template", ProductName: "fixture-product", TemplatePayload: batchTemplatePayload(), Works: batchVerifiedWorks(1), SelectionPolicy: PlanSelectionUniqueExisting})
	if err != nil || len(result.Results) != 1 || result.Results[0].Status != "preflight_required" || writer.addCalls != 0 {
		t.Fatalf("direct submit bypassed target confirmation: %#v err=%v", result, err)
	}
}

func TestPreflightCancellationWaitsForAllReadWorkers(t *testing.T) {
	reader := newWorkflowReader()
	reader.creators["second-visible"] = "4000000000000002"
	started, cancelled, release := make(chan string, 2), make(chan string, 2), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	reader.authorization = func(ctx context.Context, request portqianchuan.AuthorizedCreatorPageRequest) error {
		started <- request.SearchKeyword
		<-ctx.Done()
		cancelled <- request.SearchKeyword
		<-release
		return ctx.Err()
	}
	service, links, _ := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID), workflowLink(1, "6000000000000002", "4000000000000002", "second-visible")})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.BatchWorks(ctx, BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL, links.result.Resolved[1].InputURL)})
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("read workers did not start")
		}
	}
	cancel()
	for range 2 {
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("read workers did not receive cancellation")
		}
	}
	select {
	case err := <-done:
		t.Fatalf("command returned while read cleanup was still running: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause was lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("command did not join cancelled read workers")
	}
}

func TestBoundPlanDoesNotWaitForUnrelatedIdentityDetails(t *testing.T) {
	reader := newWorkflowReader()
	reader.addPlan(batchPlanID, batchCreatorID)
	reader.plans = append(reader.plans, domainqianchuan.Plan{AdID: "2000000000000002", Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}}})
	service, links, _ := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID)})
	identity, _ := domainqianchuan.NewPlanGroupIdentity(batchAdvertiserID, "qcpt_command", batchCreatorID, []string{batchProductID}, "", "")
	groupID, _ := domainqianchuan.GroupID(identity)
	binding, _ := NewPlanBinding(identity, groupID, "2026-11-01", batchPlanID, workflowTime)
	_ = service.Batch.Bindings.Put(context.Background(), binding)
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL)})
	if err != nil || result.Results[0].Status != "would_append" || reader.count("detail:2000000000000002") != 0 {
		t.Fatalf("bound group waited for an irrelevant/broken candidate: %#v err=%v", result, err)
	}
}

func TestRoutedPlanConflictsAreBlockedBeforeSnapshotsAreSaved(t *testing.T) {
	reader := newWorkflowReader()
	reader.addPlan(batchPlanID, batchCreatorID)
	service, links, writer := workflowService(reader, []domain.ResolvedWorkLink{workflowLink(0, "6000000000000001", batchCreatorID, batchVisibleID), workflowLink(1, "6000000000000002", batchCreatorID, batchVisibleID)})
	config := commandProductConfig()
	config["qianchuan_product_templates"].(map[string]any)["qcpt_second"] = routingTestTemplate("qcpt_second", batchAdvertiserID, batchProductID, "测试商品")
	service.Config = commandConfigReader{config: config}
	store := &conflictSnapshotStore{}
	service.Journals = store
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{Items: []domainqianchuan.BatchItem{
		{InputIndex: 0, WorkURL: links.result.Resolved[0].InputURL, PlanTemplate: "qcpt_command"},
		{InputIndex: 1, WorkURL: links.result.Resolved[1].InputURL, PlanTemplate: "qcpt_second"},
	}})
	if err != nil || len(result.Batches) != 2 || store.saved != 2 || writer.addCalls != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, batch := range result.Batches {
		if batch.Result.Results[0].Status != "plan_group_conflict" || len(batch.Result.Results[0].Writes) != 0 {
			t.Fatal("two groups selected the same physical plan")
		}
		summary, err := service.GetBatchPreflight(context.Background(), batch.Result.PreflightID)
		if err != nil || summary.ReadyForSubmit {
			t.Fatalf("conflicting snapshot is executable: %#v err=%v", summary, err)
		}
	}
}

func TestUniquePlanIdentityCoverageNeverTreatsUncertaintyAsAbsence(t *testing.T) {
	identity, groupID := fixtureBindingIdentity(t)
	target := CreatorTarget{AwemeID: batchCreatorID, VisibleID: batchVisibleID, ProductIDs: identity.ProductIDs, GroupID: groupID, BusinessDate: "2026-11-01", Identity: identity}
	for _, scenario := range []string{"changed_handle", "other_uid", "different_product", "wrong_scene", "deleted", "multiple", "missing_uid", "missing_products", "detail_failure", "cap"} {
		t.Run(scenario, func(t *testing.T) {
			reader := newWorkflowReader()
			reader.addPlan(batchPlanID, batchCreatorID)
			switch scenario {
			case "changed_handle":
				reader.plans[0].Creators = []domainqianchuan.Creator{{VisibleID: "obsolete-handle"}}
			case "other_uid":
				value := reader.details[batchPlanID]
				value.AwemeID = "4000000000000002"
				reader.details[batchPlanID] = value
			case "different_product":
				value := reader.details[batchPlanID]
				value.Products = []domainqianchuan.PlanProduct{{ProductID: routeProductB}}
				reader.details[batchPlanID] = value
			case "wrong_scene":
				reader.plans[0].AdlabScene = "OTHER_SCENE"
			case "deleted":
				reader.plans[0].Status = "DELETED"
			case "multiple":
				reader.addPlan("2000000000000002", batchCreatorID)
			case "missing_uid":
				value := reader.details[batchPlanID]
				value.AwemeID = ""
				reader.details[batchPlanID] = value
			case "missing_products":
				value := reader.details[batchPlanID]
				value.Products = nil
				reader.details[batchPlanID] = value
			case "detail_failure":
				delete(reader.details, batchPlanID)
			case "cap":
				for index := 2; index <= CurrentPlanMaxIdentityDetails+1; index++ {
					reader.addPlan(fmt.Sprint(2000000000000000+index), batchCreatorID)
				}
			}
			result, err := (CurrentDayReconciler{Reader: reader, Bindings: &memoryPlanBindingStore{}}).FindCurrentPlans(context.Background(), CurrentPlanRequest{AdvertiserID: batchAdvertiserID, AccessToken: "fixture-token", Targets: []CreatorTarget{target}, SelectionPolicy: PlanSelectionUniqueExisting, Inventory: &CurrentPlanInventory{StartTime: "2026-11-01 00:00:00", EndTime: "2026-11-01 23:59:59", PageCount: 1, Plans: reader.plans}})
			switch scenario {
			case "missing_uid", "missing_products", "detail_failure", "cap":
				if err == nil {
					t.Fatalf("incomplete identity became a create decision: %#v", result)
				}
			case "multiple":
				if err != nil || result.Policies[groupID].Status != "plan_selection_required" || len(result.Matches[groupID]) != 0 {
					t.Fatalf("multiple candidates=%#v err=%v", result, err)
				}
			case "changed_handle":
				if err != nil || result.Policies[groupID].Status != "unbound_existing" || len(result.Matches[groupID]) != 1 {
					t.Fatalf("mutable handle hid numeric UID: %#v err=%v", result, err)
				}
			default:
				if err != nil || result.Policies[groupID].Status != "would_create" {
					t.Fatalf("nonmatching identity=%#v err=%v", result, err)
				}
			}
		})
	}
}

func TestLargePlanIdentityScanCoversLaterWindowsAndStopsOnFailure(t *testing.T) {
	for _, failFirstWindow := range []bool{false, true} {
		t.Run(fmt.Sprint(failFirstWindow), func(t *testing.T) {
			reader := newWorkflowReader()
			count := CurrentPlanIdentityBatchSize + 5
			for index := range count {
				uid := "4000000000000002"
				if index == count-1 {
					uid = batchCreatorID
				}
				reader.addPlan(fmt.Sprint(2000000000000000+index), uid)
			}
			if failFirstWindow {
				delete(reader.details, reader.plans[0].AdID)
			}
			identity, err := domainqianchuan.NewPlanGroupIdentity(batchAdvertiserID, "qcpt_command", batchCreatorID, []string{batchProductID}, "", "")
			if err != nil {
				t.Fatal(err)
			}
			groupID, _ := domainqianchuan.GroupID(identity)
			pool, _ := NewReadPool(8)
			result, err := (CurrentDayReconciler{Reader: reader, Bindings: &memoryPlanBindingStore{}}).FindCurrentPlans(context.Background(), CurrentPlanRequest{
				AdvertiserID: batchAdvertiserID, AccessToken: "fixture-token", ReadPool: pool,
				SelectionPolicy: PlanSelectionUniqueExisting,
				Targets:         []CreatorTarget{{AwemeID: batchCreatorID, GroupID: groupID, BusinessDate: "2026-11-01", Identity: identity, ProductIDs: []string{batchProductID}}},
				Inventory:       &CurrentPlanInventory{StartTime: "2026-11-01 00:00:00", EndTime: "2026-11-01 23:59:59", PageCount: 3, Plans: reader.plans},
			})
			if failFirstWindow {
				if err == nil || reader.count("detail:"+reader.plans[count-1].AdID) != 0 {
					t.Fatal("failed first window advanced to a create decision or fetched later windows")
				}
				return
			}
			if err != nil || result.Policies[groupID].Status != "unbound_existing" || len(result.Matches[groupID]) != 1 || result.Matches[groupID][0].AdID != reader.plans[count-1].AdID {
				t.Fatalf("later-window match was rejected or became a false create: result=%#v err=%v", result, err)
			}
			for _, plan := range reader.plans {
				if reader.count("detail:"+plan.AdID) != 1 {
					t.Fatal("identity scan omitted or repeated a plan")
				}
			}
		})
	}
}

func TestV2SnapshotKeepsBoundOnlyPolicyAndV3RequiresExplicitPolicy(t *testing.T) {
	work := batchVerifiedWorks(1)[0]
	groupID := batchTestGroupID(t, "fixture-template", work)
	absent, _ := PlanBindingDigest(nil)
	request := BatchRequest{AdvertiserID: batchAdvertiserID, TemplateID: "fixture-template", TemplateName: "fixture-template", ProductName: "fixture-product", TemplatePayload: batchTemplatePayload(), Works: []VerifiedWork{work}, SelectionPolicy: PlanSelectionUniqueExisting}
	snapshot, _, err := prepareBatchSnapshot(request, BatchResult{BusinessDate: "2026-11-01", Results: []BatchGroupResult{{GroupID: groupID, AwemeID: batchCreatorID, ProductIDs: []string{batchProductID}, InputItemIDs: []string{work.AwemeItemID}, AdID: batchPlanID, Status: "would_append", BindingDigest: absent}}}, workflowTime)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{2, 3} {
		copy := snapshot
		copy.SchemaVersion = version
		if version == 2 {
			copy.SelectionPolicy = ""
		}
		journal, err := batchPreflightJournal(copy, workflowTime)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeBatchPreflight(journal, workflowTime.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		batch, err := decoded.batchRequest(request, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := PlanSelectionUniqueExisting
		if version == 2 {
			want = PlanSelectionBoundOnly
		}
		if batch.SelectionPolicy != want {
			t.Fatalf("schema %d silently changed selection policy", version)
		}
	}
	snapshot.SelectionPolicy = ""
	journal, _ := batchPreflightJournal(snapshot, workflowTime)
	if _, err := decodeBatchPreflight(journal, workflowTime.Add(time.Minute)); err == nil {
		t.Fatal("V3 accepted an absent selection policy")
	}
}

type workflowReader struct {
	commandReader
	mu            sync.Mutex
	counts        map[string]int
	creators      map[string]string
	plans         []domainqianchuan.Plan
	details       map[string]domainqianchuan.PlanDetail
	materials     map[string][]domainqianchuan.PlanMaterial
	authorization func(context.Context, portqianchuan.AuthorizedCreatorPageRequest) error
	materialRead  func(portqianchuan.MaterialPageRequest)
	detailRead    func(context.Context, portqianchuan.PlanDetailRequest) error
}

func newWorkflowReader() *workflowReader {
	return &workflowReader{counts: map[string]int{}, creators: map[string]string{batchVisibleID: batchCreatorID}, details: map[string]domainqianchuan.PlanDetail{}, materials: map[string][]domainqianchuan.PlanMaterial{}}
}
func (reader *workflowReader) count(key string) int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.counts[key]
}
func (reader *workflowReader) addPlan(adID, uid string) {
	reader.plans = append(reader.plans, domainqianchuan.Plan{AdID: adID, Status: "ENABLE", MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT", Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}}})
	reader.details[adID] = domainqianchuan.PlanDetail{AdID: adID, Status: "ENABLE", MarketingGoal: "VIDEO_PROM_GOODS", AwemeID: uid, Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}}}
}
func (reader *workflowReader) FetchPlans(_ context.Context, request portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.counts["plans"]++
	if request.StartTime != "2026-11-01 00:00:00" || request.MarketingGoal != "VIDEO_PROM_GOODS" || request.AdlabScene != "UNI_PROJECT" {
		return domainqianchuan.PlanPage{}, errors.New("incorrect plan inventory scope")
	}
	return domainqianchuan.PlanPage{Rows: append([]domainqianchuan.Plan(nil), reader.plans...), PageInfo: domainqianchuan.PageInfo{Page: request.Page, TotalPages: 1, TotalNumber: len(reader.plans)}}, nil
}
func (reader *workflowReader) FetchPlanDetail(ctx context.Context, request portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	if reader.detailRead != nil {
		if err := reader.detailRead(ctx, request); err != nil {
			return domainqianchuan.PlanDetail{}, err
		}
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.counts["detail:"+request.AdID]++
	value, ok := reader.details[request.AdID]
	if !ok {
		return value, errors.New("fixture detail unavailable")
	}
	return value, nil
}
func (reader *workflowReader) FetchPlanMaterials(_ context.Context, request portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.counts["materials:"+request.AdID]++
	if reader.materialRead != nil {
		reader.materialRead(request)
	}
	rows := append([]domainqianchuan.PlanMaterial(nil), reader.materials[request.AdID]...)
	return domainqianchuan.MaterialPage{Rows: rows, PageInfo: domainqianchuan.PageInfo{Page: request.Page, TotalPages: 1, TotalNumber: len(rows)}}, nil
}
func (reader *workflowReader) FetchAuthorizedCreators(ctx context.Context, request portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	if reader.authorization != nil {
		if err := reader.authorization(ctx, request); err != nil {
			return domainqianchuan.AuthorizedCreatorPage{}, err
		}
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.counts["authorization:"+request.SearchKeyword]++
	uid := reader.creators[request.SearchKeyword]
	return domainqianchuan.AuthorizedCreatorPage{Rows: []domainqianchuan.AuthorizedCreator{{AwemeID: uid, VisibleID: request.SearchKeyword, Name: "fixture-creator"}}, PageInfo: domainqianchuan.PageInfo{Page: request.Page, TotalPages: 1, TotalNumber: 1}}, nil
}
func (reader *workflowReader) FetchCreatorVideos(_ context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.counts["videos:"+request.ProductID]++
	rows := []domainqianchuan.CreatorVideo{}
	for _, id := range request.AwemeItemIDs {
		rows = append(rows, domainqianchuan.CreatorVideo{AwemeItemID: id, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + id, Title: "fixture-title"})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

type workflowWriter struct {
	commandWriter
	reader   *workflowReader
	addCalls int
}

func (writer *workflowWriter) AddMaterials(_ context.Context, request portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	writer.addCalls++
	var payload struct {
		Creatives []struct {
			Videos []struct {
				ID json.Number `json:"aweme_item_id"`
			} `json:"video_material"`
		} `json:"multi_product_creative_list"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(request.Payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return portqianchuan.WriteResult{}, err
	}
	writer.reader.mu.Lock()
	defer writer.reader.mu.Unlock()
	for _, creative := range payload.Creatives {
		for _, video := range creative.Videos {
			writer.reader.materials[request.AdID] = append(writer.reader.materials[request.AdID], domainqianchuan.PlanMaterial{AwemeItemID: string(video.ID), MaterialID: "material-" + string(video.ID)})
		}
	}
	return portqianchuan.WriteResult{RequestID: "fixture-append"}, nil
}

func workflowService(reader *workflowReader, rows []domain.ResolvedWorkLink) (CommandService, *commandLinkResolver, *workflowWriter) {
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{Resolved: rows}}
	bindings := &memoryPlanBindingStore{}
	writer := &workflowWriter{reader: reader}
	service := CommandService{Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{}, Links: links, Verifier: WorkVerifier{Reader: reader}, Locks: commandLocker{}, Journals: &commandJournalStore{}, Now: func() time.Time { return workflowTime }, Batch: BatchService{Guard: sharedplans.GuardedExecutor{Locks: commandLocker{}}, Reader: reader, Writer: writer, Reconciler: CurrentDayReconciler{Reader: reader, Bindings: bindings, Now: func() time.Time { return workflowTime }}, Bindings: bindings, Now: func() time.Time { return workflowTime }}}
	return service, links, writer
}
func workflowLink(index int, itemID, uid, showID string) domain.ResolvedWorkLink {
	return domain.ResolvedWorkLink{InputIndex: index, InputURL: "https://www.douyin.com/video/" + itemID, AwemeItemID: itemID, CreatorName: "fixture-creator", OwnerHint: &domain.WorkOwnerHint{AwemeID: uid, AwemeShowID: showID}}
}

type conflictSnapshotStore struct {
	commandJournalStore
	saved int
}

func (store *conflictSnapshotStore) Save(ctx context.Context, id string, journal domainplans.Journal) error {
	var snapshot preparedBatchSnapshot
	if err := json.Unmarshal(journal.Extra["snapshot"], &snapshot); err != nil {
		return err
	}
	for _, group := range snapshot.Groups {
		if group.SubmitEligible || group.ExpectedAction != "plan_group_conflict" {
			return errors.New("an executable conflict snapshot was saved")
		}
	}
	store.saved++
	return store.commandJournalStore.Save(ctx, id, journal)
}
