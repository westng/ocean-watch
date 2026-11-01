package qianchuan

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type creatorPreflight struct {
	verification WorkVerificationResult
	batch        BatchResult
	timing       BatchTiming
	verifiedAt   time.Time
}

// Each creator advances as soon as its own official evidence is complete.
// Account inventory and immutable reads are coalesced by the operation pool.
// This pipeline is read-only; submitted groups keep the account write lock.
func (service CommandService) preflightCreators(
	ctx context.Context, batch BatchRequest, request WorkVerificationRequest,
	awaitInventory func(context.Context) (*CurrentPlanInventory, error),
) (BatchResult, WorkVerificationResult, BatchTiming, float64, error) {
	if service.Verifier.Reader == nil || service.Batch.Reader == nil || service.Batch.Reconciler == nil {
		return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, errors.New("Qianchuan preflight read dependencies are incomplete")
	}
	works, skipped, err := normalizeWorkInputs(request.Works)
	if err != nil {
		return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, err
	}
	batch.Submit, batch.Works = false, nil
	batch.Skipped = append(batch.Skipped, skipped...)
	result, err := service.Batch.Execute(ctx, batch)
	if err != nil {
		return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, err
	}
	batch.BusinessDate = result.BusinessDate
	verification := WorkVerificationResult{Skipped: skipped, ResolvedOwnerHints: map[string]OwnerHint{}}
	groups := partitionCreatorWorks(works)
	started := service.now()
	type completion struct {
		index int
		value creatorPreflight
	}
	completed := make(chan completion, len(groups))
	for index, group := range groups {
		go func() {
			query := request
			query.Works = group
			verified, verifyErr := service.Verifier.Verify(ctx, query)
			value := creatorPreflight{verification: verified, verifiedAt: service.now()}
			child := batch
			child.Works, child.Skipped, child.QueryFailures = verified.Matched, verified.Skipped, verified.QueryFailures
			if verifyErr != nil {
				// No ownership/product proof exists for this creator's works.
				child.Works = nil
				for _, work := range group {
					child.Skipped = append(child.Skipped, skippedFromWork(work, "work_verification_failed", "作品官方校验未完成", nil))
				}
				value.verification.Skipped = child.Skipped
				value.batch, _ = service.Batch.Execute(ctx, child)
				value.batch.ExitCode = 1
			} else {
				var executeErr error
				if awaitInventory != nil && len(child.Works) != 0 {
					child.PlanInventory, executeErr = awaitInventory(ctx)
					executeErr = preflightStageError("plan_inventory", executeErr)
				}
				if executeErr == nil {
					value.batch, executeErr = service.Batch.Execute(WithBatchTiming(ctx, &value.timing), child)
				}
				if executeErr != nil {
					value.batch = failedCreatorPreflight(child, executeErr)
				}
			}
			completed <- completion{index: index, value: value}
		}()
	}
	ordered := make([]creatorPreflight, len(groups))
	for range groups {
		done := <-completed
		ordered[done.index] = done.value
	}
	if ctx.Err() != nil {
		return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, ctx.Err()
	}
	lastVerification := started
	timing := BatchTiming{}
	for _, value := range ordered {
		mergeWorkVerification(&verification, value.verification)
		result.Results = append(result.Results, value.batch.Results...)
		result.Skipped = append(result.Skipped, value.batch.Skipped...)
		result.QueryFailures = append(result.QueryFailures, value.batch.QueryFailures...)
		if value.batch.ExitCode != 0 {
			result.ExitCode = 1
		}
		if value.verifiedAt.After(lastVerification) {
			lastVerification = value.verifiedAt
		}
		// Concurrent durations are not additive runtime. Report the longest
		// creator stage; overall runtime is measured by the command boundary.
		timing.GroupReconciliationSeconds = max(timing.GroupReconciliationSeconds, value.timing.GroupReconciliationSeconds)
		timing.MaterialDiffSeconds = max(timing.MaterialDiffSeconds, value.timing.MaterialDiffSeconds)
	}
	groupWorks := map[string]bool{}
	for _, group := range result.Results {
		for _, id := range group.InputItemIDs {
			groupWorks[id] = true
		}
	}
	matched := make([]VerifiedWork, 0, len(verification.Matched))
	for _, work := range verification.Matched {
		if groupWorks[work.AwemeItemID] {
			matched = append(matched, work)
		}
	}
	verification.Matched = matched
	sort.SliceStable(verification.Matched, func(i, j int) bool { return verification.Matched[i].InputIndex < verification.Matched[j].InputIndex })
	// Visible-handle recovery can join two input partitions to the same UID.
	// Rebuild that uncommon case from the combined facts instead of publishing
	// duplicate group IDs. Coalesced reads avoid repeating official requests.
	if duplicateBatchGroups(result.Results) {
		if len(result.FailedResults) != 0 || hasFailedBatchGroup(result.Results) {
			return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, errors.New("creator recovery joined incomplete plan groups")
		}
		combined := batch
		combined.Works, combined.Skipped, combined.QueryFailures = verification.Matched, result.Skipped, result.QueryFailures
		result, err = service.Batch.Execute(ctx, combined)
		if err != nil {
			return BatchResult{}, WorkVerificationResult{}, BatchTiming{}, 0, err
		}
	}
	blockBatchPlanConflicts(&result, verification.Matched)
	rebuildBatchPresentation(&result, verification.Matched)
	return result, verification, timing, durationSeconds(started, lastVerification), nil
}

func partitionCreatorWorks(works []WorkInput) [][]WorkInput {
	groups := [][]WorkInput{}
	indexes := map[string]int{}
	for _, work := range works {
		key := "work\x00" + work.AwemeItemID
		if work.OwnerHint != nil {
			if uid := strings.TrimSpace(work.OwnerHint.AwemeID); validPositiveID(uid) {
				key = "uid\x00" + uid
			} else if showID := strings.TrimSpace(work.OwnerHint.AwemeShowID); showID != "" {
				key = "show\x00" + showID
			}
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], work)
	}
	return groups
}

func failedCreatorPreflight(request BatchRequest, err error) BatchResult {
	normalized, normalizeErr := normalizeBatchRequest(request)
	if normalizeErr != nil {
		works := request.Works
		request.Works = nil
		for _, work := range works {
			request.Skipped = append(request.Skipped, SkippedWork{InputIndex: work.InputIndex, AwemeItemID: work.AwemeItemID, Reason: "group_preflight_failed", Message: "作品投放分组校验未完成"})
		}
		normalized, _ = normalizeBatchRequest(request)
		normalized.businessDate = request.BusinessDate
		result := normalized.emptyResult("dry_run")
		result.ExitCode = 1
		return result
	}
	normalized.businessDate = request.BusinessDate
	result := normalized.emptyResult("dry_run")
	for _, group := range normalized.groups {
		row := newBatchGroupResult(group, group.identity.ProductIDs)
		row.Failure = err
		row.Status, row.ErrorCode, row.Error = "failed", "GROUP_PREFLIGHT_FAILED", "creator preflight did not complete"
		result.Results = append(result.Results, row)
	}
	result.ExitCode = 1
	rebuildBatchPresentation(&result, request.Works)
	return result
}

func mergeWorkVerification(target *WorkVerificationResult, source WorkVerificationResult) {
	target.Matched = append(target.Matched, source.Matched...)
	target.Skipped = append(target.Skipped, source.Skipped...)
	target.QueryFailures = append(target.QueryFailures, source.QueryFailures...)
	for _, creator := range source.Creators {
		target.Creators = appendCreatorUnique(target.Creators, creator)
	}
	for _, creator := range source.DisabledCreators {
		target.DisabledCreators = appendCreatorUnique(target.DisabledCreators, creator)
	}
	for itemID, hint := range source.ResolvedOwnerHints {
		target.ResolvedOwnerHints[itemID] = hint
	}
	target.AuthorizedCreatorScanCount += source.AuthorizedCreatorScanCount
	target.AuthorizedCreatorPageCount += source.AuthorizedCreatorPageCount
	target.OwnershipQueryCount += source.OwnershipQueryCount
	target.ProductQueryCount += source.ProductQueryCount
	target.OwnerHintSummary.Supplied += source.OwnerHintSummary.Supplied
	target.OwnerHintSummary.Eligible += source.OwnerHintSummary.Eligible
	target.OwnerHintSummary.Verified += source.OwnerHintSummary.Verified
	target.OwnerHintSummary.Stale += source.OwnerHintSummary.Stale
	target.OwnerHintSummary.AuthorizedHintQueryCount += source.OwnerHintSummary.AuthorizedHintQueryCount
	target.OwnerHintSummary.AuthorizedHintFailureCount += source.OwnerHintSummary.AuthorizedHintFailureCount
	target.OwnerHintSummary.OfficialVideoQueryCount += source.OwnerHintSummary.OfficialVideoQueryCount
}

func duplicateBatchGroups(groups []BatchGroupResult) bool {
	seen := map[string]bool{}
	for _, group := range groups {
		if seen[group.GroupID] {
			return true
		}
		seen[group.GroupID] = true
	}
	return false
}

func hasFailedBatchGroup(groups []BatchGroupResult) bool {
	for _, group := range groups {
		if batchGroupFailed(group.Status) {
			return true
		}
	}
	return false
}

func rebuildBatchPresentation(result *BatchResult, works []VerifiedWork) {
	byID := map[string]VerifiedWork{}
	for _, work := range works {
		byID[work.AwemeItemID] = work
	}
	counts := map[string]int{"creator_groups": len(result.Results), "matched_works": len(works), "skipped_works": len(result.Skipped)}
	result.FailedResults = []BatchGroupResult{}
	type orderedRow struct {
		index int
		row   map[string]any
	}
	ordered := []orderedRow{}
	for _, groupResult := range result.Results {
		counts[groupResult.Status]++
		group := batchGroup{groupID: groupResult.GroupID}
		for _, itemID := range groupResult.InputItemIDs {
			if work, ok := byID[itemID]; ok {
				group.works = append(group.works, work)
				group.creator = work.Creator
			}
		}
		if batchGroupFailed(groupResult.Status) {
			result.FailedResults = append(result.FailedResults, groupResult)
			result.ExitCode = 1
		}
		for _, work := range group.works {
			for _, row := range batchPresentationRows(batchGroup{works: []VerifiedWork{work}}, groupResult) {
				ordered = append(ordered, orderedRow{index: work.InputIndex, row: row})
			}
		}
	}
	result.Counts = counts
	sort.SliceStable(result.Skipped, func(i, j int) bool { return result.Skipped[i].InputIndex < result.Skipped[j].InputIndex })
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].index < ordered[j].index })
	rows := make([]map[string]any, 0, len(ordered))
	for _, row := range ordered {
		rows = append(rows, row.row)
	}
	result.Presentation = domain.NewQianchuanBatchPresentation(rows, []string{"skipped", "query_failures", "failed_results"})
}
