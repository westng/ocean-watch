package qianchuan

import (
	"context"
	"errors"
	"fmt"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func blockConflictingPlanSelections(discovery *CurrentPlanResult) {
	owners := map[string][]string{}
	for groupID, plans := range discovery.Matches {
		if len(plans) == 1 {
			owners[plans[0].AdID] = append(owners[plans[0].AdID], groupID)
		}
	}
	for _, groups := range owners {
		if len(groups) < 2 {
			continue
		}
		for _, groupID := range groups {
			policy := discovery.Policies[groupID]
			policy.Status = "plan_group_conflict"
			policy.Candidates = append([]ExistingPlan(nil), discovery.Matches[groupID]...)
			discovery.Policies[groupID] = policy
			discovery.Matches[groupID] = []ExistingPlan{}
		}
	}
}

func blockBatchPlanConflicts(result *BatchResult, works []VerifiedWork) {
	owners := map[string]map[string]bool{}
	for _, group := range result.Results {
		if group.AdID != "" && !batchGroupFailed(group.Status) {
			if owners[group.AdID] == nil {
				owners[group.AdID] = map[string]bool{}
			}
			owners[group.AdID][group.GroupID] = true
		}
	}
	for index := range result.Results {
		if len(owners[result.Results[index].AdID]) > 1 {
			markPlanGroupConflict(&result.Results[index])
		}
	}
	rebuildBatchPresentation(result, works)
}

func markPlanGroupConflict(group *BatchGroupResult) {
	group.Status, group.ErrorCode = "plan_group_conflict", "PLAN_GROUP_CONFLICT"
	group.Error = "the same plan was selected by multiple complete group identities"
	group.CompletedItemIDs, group.Writes = []string{}, []BatchWrite{}
}

func blockRoutedPlanConflicts(batches []RoutedBatchResult) {
	owners := map[string]map[string]bool{}
	for _, batch := range batches {
		if batch.Result == nil {
			continue
		}
		for _, group := range batch.Result.Results {
			if group.AdID == "" || batchGroupFailed(group.Status) {
				continue
			}
			key := batch.Template.AdvertiserID + "\x00" + group.AdID
			if owners[key] == nil {
				owners[key] = map[string]bool{}
			}
			owners[key][group.GroupID] = true
		}
	}
	for _, batch := range batches {
		if batch.Result == nil || batch.Result.snapshot == nil {
			continue
		}
		works := []VerifiedWork{}
		for index := range batch.Result.Results {
			group := &batch.Result.Results[index]
			key := batch.Template.AdvertiserID + "\x00" + group.AdID
			if len(owners[key]) > 1 {
				markPlanGroupConflict(group)
				prepared := batch.Result.snapshot.Groups[group.GroupID]
				prepared.SubmitEligible, prepared.ExpectedAction, prepared.ErrorCode = false, group.Status, group.ErrorCode
				prepared.ExpectedPresentItemIDs, prepared.ExpectedWriteItemIDs = nil, nil
				batch.Result.snapshot.Groups[group.GroupID] = prepared
			}
			works = append(works, batch.Result.snapshot.Groups[group.GroupID].Works...)
		}
		rebuildBatchPresentation(&batch.Result.BatchResult, works)
	}
}

// Called only after a confirmed append/noop under the advertiser write lock.
// Read back identity independently of the preflight cache before saving a claim.
func (service BatchService) adoptExistingPlan(
	ctx context.Context, request normalizedBatchRequest, group batchGroup, adID, accessToken string,
) error {
	if service.Reader == nil || service.Bindings == nil {
		return errors.New("Qianchuan existing-plan binding dependencies are incomplete")
	}
	detail, err := runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.PlanDetail, error) {
		return service.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: accessToken, AdID: adID,
		})
	})
	if err != nil {
		return fmt.Errorf("verify adopted Qianchuan plan: %w", err)
	}
	if detail.AdID != adID || detail.AwemeID != group.identity.CreatorID ||
		isDeletedPlan(detail.Status, detail.OptStatus) ||
		(detail.MarketingGoal != "" && detail.MarketingGoal != "VIDEO_PROM_GOODS") ||
		!sameStringSet(planProductIDs(detail.Products), group.identity.ProductIDs) {
		return errors.New("adopted Qianchuan plan no longer matches the complete group identity")
	}
	binding, err := NewPlanBinding(group.identity, group.groupID, request.businessDate, adID, service.now())
	if err != nil {
		return err
	}
	return service.Bindings.Put(ctx, binding)
}
