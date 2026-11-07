package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	ops "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func (s OperationService) readback(ctx context.Context, snapshot ops.Snapshot, p operationPayload, w operationWrite, returnedID, token string, lease ops.ExecutionLease) error {
	reader := s.Commands.Batch.Reader
	if reader == nil || s.Commands.Batch.Bindings == nil {
		return ErrOperationUnsupported
	}
	group := p.Preflight.Groups[w.GroupID]
	target := w.TargetID
	if w.Action == "create" {
		target = returnedID
		if target == "" {
			scanner, ok := s.Commands.Batch.Reconciler.(CurrentPlanScanner)
			if !ok {
				return ErrOperationUnsupported
			}
			inventory, err := scanner.ScanCurrentPlans(ctx, CurrentPlanScanRequest{AdvertiserID: snapshot.SubjectID, AccessToken: token, BusinessDate: p.Preflight.BusinessDate})
			if err != nil {
				return err
			}
			candidates := []string{}
			for _, plan := range inventory.Plans {
				if plan.Name == w.PlanName {
					candidates = append(candidates, plan.AdID)
				}
			}
			if len(candidates) != 1 {
				return ops.ErrDispatchUnknown
			}
			target = candidates[0]
		}
	}
	if !validPositiveID(target) {
		return ops.ErrDispatchUnknown
	}
	detail, err := reader.FetchPlanDetail(ctx, port.PlanDetailRequest{AdvertiserID: snapshot.SubjectID, AccessToken: token, AdID: target})
	if err != nil {
		return err
	}
	if detail.AdID != target || detail.AwemeID != group.Identity.CreatorID || isDeletedPlan(detail.Status, detail.OptStatus) || !sameStringSet(planProductIDs(detail.Products), group.Identity.ProductIDs) || w.Action == "create" && detail.Name != w.PlanName {
		return ops.ErrDispatchUnknown
	}
	if detail.MarketingGoal != "" && detail.MarketingGoal != "VIDEO_PROM_GOODS" {
		return ops.ErrDispatchUnknown
	}
	if w.Action == "create" {
		var payload struct {
			Settings struct {
				Budget json.Number `json:"budget"`
				ROI    json.Number `json:"roi2_goal"`
				Bid    string      `json:"smart_bid_type"`
			} `json:"delivery_setting"`
		}
		if json.Unmarshal(w.Payload, &payload) != nil {
			return ops.ErrDispatchUnknown
		}
		budget, err := domain.ParseDecimal(string(payload.Settings.Budget))
		if err != nil || payload.Settings.Budget == "" || detail.Budget == nil || budget.Compare(*detail.Budget) != 0 || detail.SmartBidType != payload.Settings.Bid {
			return ops.ErrDispatchUnknown
		}
		if payload.Settings.ROI != "" {
			roi, err := domain.ParseDecimal(string(payload.Settings.ROI))
			if err != nil || detail.ROI2Goal == nil || roi.Compare(*detail.ROI2Goal) != 0 {
				return ops.ErrDispatchUnknown
			}
		}
	}
	materials, err := s.Commands.Batch.planMaterialsWithPool(ctx, snapshot.SubjectID, token, target, nil)
	if err != nil {
		return err
	}
	for _, id := range w.ItemIDs {
		if _, ok := materials.activeIDs[id]; !ok {
			return ops.ErrDispatchUnknown
		}
	}
	binding, err := NewPlanBinding(group.Identity, w.GroupID, p.Preflight.BusinessDate, target, s.Commands.now())
	if err != nil {
		return err
	}
	if err = s.Commands.Batch.Bindings.Put(ctx, binding); err != nil {
		return err
	}
	evidence, _ := json.Marshal(map[string]any{"plan_id": target, "creator_id": detail.AwemeID, "product_ids": group.Identity.ProductIDs, "item_ids": w.ItemIDs, "budget": detail.Budget, "roi2_goal": detail.ROI2Goal, "smart_bid_type": detail.SmartBidType})
	digest, err := ops.Digest(evidence)
	if err != nil {
		return err
	}
	return s.Recovery.SaveReadback(ctx, ops.Readback{Lease: lease, ItemID: w.ItemID, ObjectID: target, EvidenceDigest: digest, Now: s.Commands.now()})
}

// Reconcile has no path to Writer. A restart recovers facts; undispatched work
// requires a fresh preflight and approval instead of reusing a consumed grant.
func (s OperationService) Reconcile(ctx context.Context, ref OperationReference) (OperationView, error) {
	snapshot, p, err := s.load(ctx, ref, contracts.QueryModePlan)
	if err != nil {
		return OperationView{}, err
	}
	if snapshot.RuntimeVersion != s.RuntimeVersion {
		return OperationView{}, ErrOperationChanged
	}
	current, err := s.Core.Repository.GetOperationState(ctx, ref.OperationID, ref.Version)
	if err != nil {
		return OperationView{}, err
	}
	if current.Status == ops.StatusSucceeded || current.Status == ops.StatusFailed {
		return s.Get(ctx, ref)
	}
	if current.Status != ops.StatusExecuting && current.Status != ops.StatusUnknown && current.Status != ops.StatusPartiallySucceeded && current.Status != ops.StatusReconciling {
		return OperationView{}, ops.ErrOperationNotExecutable
	}
	token, scoped, err := s.Commands.readLease(ctx, snapshot.SubjectID, p.Preflight.AuthAccountID)
	if err != nil {
		return OperationView{}, err
	}
	if token.AuthorizationID != p.AuthorizationID {
		return OperationView{}, ErrOperationChanged
	}
	scope := domainplans.WriteScope{Channel: domainplans.ChannelQianchuan, AdvertiserID: snapshot.SubjectID, LockFamily: domainplans.LockQianchuanWorks}
	err = sharedplans.WithAdvertiserLock(scoped, s.Commands.Locks, scope, func(locked context.Context) error {
		owner, err := operationOwner()
		if err != nil {
			return err
		}
		lease, err := s.Core.TakeOverExecution(locked, ref.OperationID, ref.Version, owner, s.Commands.now(), executionTTL)
		if err != nil {
			return err
		}
		records, readErr := s.Recovery.ReadDispatches(locked, ref.OperationID, ref.Version)
		if readErr == nil {
			byID := map[string]operationWrite{}
			for _, w := range p.Writes {
				byID[w.ItemID] = w
			}
			for _, r := range records {
				if r.Applied || r.State == ops.DispatchNotSent {
					continue
				}
				lease, err = s.Core.Repository.RenewExecutionLease(locked, lease, s.Commands.now(), executionTTL)
				if err != nil {
					readErr = err
					break
				}
				if err = s.readback(locked, snapshot, p, byID[r.ItemID], r.ObjectID, token.AccessToken, lease); err != nil {
					readErr = errors.Join(readErr, err)
				}
			}
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(locked), 5*time.Second)
		defer cancel()
		_, finishErr := s.Recovery.FinishExecution(cleanup, lease, s.Commands.now())
		return errors.Join(readErr, finishErr)
	})
	view, readErr := s.Get(ctx, ref)
	return view, errors.Join(err, readErr)
}
