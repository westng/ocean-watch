package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const (
	CurrentPlanPageSize = 100
	CurrentPlanMaxPages = 100
	// Identity work follows the bounded inventory. Windows bound queued tasks,
	// rather than rejecting an otherwise complete large-account inventory.
	CurrentPlanIdentityBatchSize  = 200
	CurrentPlanMaxIdentityDetails = CurrentPlanPageSize * CurrentPlanMaxPages
	PlanSelectionBoundOnly        = "bound_only"
	PlanSelectionUniqueExisting   = "unique_existing"
)

type CreatorTarget struct {
	AwemeID      string
	VisibleID    string
	ProductIDs   []string
	PlanName     string
	GroupID      string
	BusinessDate string
	Identity     domainqianchuan.PlanGroupIdentity
}

type CurrentPlanRequest struct {
	AdvertiserID    string
	AccessToken     string
	Targets         []CreatorTarget
	ReadPool        *ReadPool
	Inventory       *CurrentPlanInventory
	SelectionPolicy string
}

type CurrentPlanScanRequest struct {
	AdvertiserID string
	AccessToken  string
	BusinessDate string
	ReadPool     *ReadPool
}

type CurrentPlanInventory struct {
	StartTime  string
	EndTime    string
	PageCount  int
	RequestIDs []string
	Plans      []domainqianchuan.Plan
}

type ExistingPlan struct {
	AdID       string   `json:"ad_id"`
	Name       string   `json:"name,omitempty"`
	Status     string   `json:"status,omitempty"`
	OptStatus  string   `json:"opt_status,omitempty"`
	AwemeID    string   `json:"aweme_id"`
	ProductIDs []string `json:"product_ids"`
}

type CurrentPlanResult struct {
	StartTime  string                     `json:"start_time"`
	EndTime    string                     `json:"end_time"`
	PageCount  int                        `json:"page_count"`
	RequestIDs []string                   `json:"request_ids"`
	Matches    map[string][]ExistingPlan  `json:"matches"`
	Policies   map[string]PlanMatchPolicy `json:"policies,omitempty"`
}

type PlanMatchPolicy struct {
	Status        string         `json:"status"`
	Candidates    []ExistingPlan `json:"candidates"`
	BindingDigest string         `json:"binding_digest"`
}

type CurrentPlanFinder interface {
	FindCurrentPlans(context.Context, CurrentPlanRequest) (CurrentPlanResult, error)
}

type CurrentPlanScanner interface {
	ScanCurrentPlans(context.Context, CurrentPlanScanRequest) (CurrentPlanInventory, error)
}

type PlanInventoryError struct {
	Reason string
	cause  error
}

func (err *PlanInventoryError) Error() string {
	return "Qianchuan plan inventory failed: " + err.Reason
}

func (err *PlanInventoryError) Unwrap() error {
	return err.cause
}

func planInventoryError(reason string, err error) error {
	return &PlanInventoryError{Reason: reason, cause: err}
}

type CurrentDayReconciler struct {
	Reader   portqianchuan.Reader
	Bindings PlanBindingReader
	Now      func() time.Time
	MaxPages int
}

func (reconciler CurrentDayReconciler) FindCurrentPlans(
	ctx context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	if ctx == nil {
		return CurrentPlanResult{}, errors.New("Qianchuan reconciliation context is required")
	}
	if reconciler.Reader == nil {
		return CurrentPlanResult{}, errors.New("Qianchuan reconciliation reader is required")
	}
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AccessToken = strings.TrimSpace(request.AccessToken)
	if !validPositiveID(request.AdvertiserID) {
		return CurrentPlanResult{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.AccessToken == "" {
		return CurrentPlanResult{}, errors.New("Qianchuan reconciliation access token is required")
	}
	if request.SelectionPolicy == "" {
		request.SelectionPolicy = PlanSelectionBoundOnly
	}
	if request.SelectionPolicy != PlanSelectionBoundOnly && request.SelectionPolicy != PlanSelectionUniqueExisting {
		return CurrentPlanResult{}, errors.New("Qianchuan plan selection policy is invalid")
	}
	targets, err := normalizeCreatorTargets(request.Targets)
	if err != nil {
		return CurrentPlanResult{}, err
	}
	inventory := request.Inventory
	if inventory == nil {
		businessDate := ""
		for _, target := range targets {
			if target.GroupID == "" {
				continue
			}
			if businessDate != "" && businessDate != target.BusinessDate {
				return CurrentPlanResult{}, errors.New("Qianchuan reconciliation targets span multiple business dates")
			}
			businessDate = target.BusinessDate
		}
		scanned, scanErr := reconciler.ScanCurrentPlans(ctx, CurrentPlanScanRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
			BusinessDate: businessDate, ReadPool: request.ReadPool,
		})
		if scanErr != nil {
			return CurrentPlanResult{}, scanErr
		}
		inventory = &scanned
	}
	if inventory == nil || strings.TrimSpace(inventory.StartTime) == "" ||
		strings.TrimSpace(inventory.EndTime) == "" || inventory.PageCount < 1 {
		return CurrentPlanResult{}, errors.New("Qianchuan current-plan inventory is incomplete")
	}
	if len(inventory.Plans) > CurrentPlanMaxIdentityDetails {
		return CurrentPlanResult{}, planInventoryError("identity_safety_cap", errors.New("plan identity work exceeds the inventory safety cap"))
	}
	matches := make(map[string][]ExistingPlan, len(targets))
	rawMatches := make(map[string][]ExistingPlan, len(targets))
	for _, target := range targets {
		matches[target.key] = []ExistingPlan{}
		rawMatches[target.key] = []ExistingPlan{}
		if target.GroupID != "" && target.Identity.AdvertiserID != request.AdvertiserID {
			return CurrentPlanResult{}, errors.New("Qianchuan reconciliation group advertiser does not match request")
		}
	}
	bindings := map[string]PlanBinding{}
	candidates := selectListCandidates(inventory.Plans, targets)
	if request.SelectionPolicy == PlanSelectionUniqueExisting {
		bindings, err = reconciler.scopedPlanBindings(ctx, request, targets)
		if err != nil {
			return CurrentPlanResult{}, err
		}
		candidates = selectIdentityCandidates(inventory.Plans, targets)
		filtered := make([]listCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			needed := make([]normalizedCreatorTarget, 0, len(candidate.targets))
			for _, target := range candidate.targets {
				binding, bound := bindings[target.key]
				if !bound || (BindingMatchesIdentity(binding, target.Identity, target.BusinessDate) && candidate.plan.AdID == binding.AdID) {
					needed = append(needed, target)
				}
			}
			if len(needed) != 0 {
				candidate.targets = needed
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	type candidateDetail struct {
		detail domainqianchuan.PlanDetail
		err    error
	}
	for start := 0; start < len(candidates); start += CurrentPlanIdentityBatchSize {
		if err := ctx.Err(); err != nil {
			return CurrentPlanResult{}, err
		}
		window := candidates[start:min(start+CurrentPlanIdentityBatchSize, len(candidates))]
		details := parallelOrdered(ctx, request.ReadPool, len(window), func(ctx context.Context, index int) candidateDetail {
			candidate := window[index]
			detail, fetchErr := runReadOnce(
				ctx, request.ReadPool, planDetailReadKey(request.AdvertiserID, candidate.plan.AdID),
				func(ctx context.Context) (domainqianchuan.PlanDetail, error) {
					return runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.PlanDetail, error) {
						return reconciler.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
							AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken, AdID: candidate.plan.AdID,
						})
					})
				},
			)
			return candidateDetail{detail: detail, err: fetchErr}
		})
		for index, candidate := range window {
			detail, fetchErr := details[index].detail, details[index].err
			if fetchErr != nil {
				return CurrentPlanResult{}, fmt.Errorf("confirm Qianchuan plan %s: %w", candidate.plan.AdID, fetchErr)
			}
			if detail.AdID != candidate.plan.AdID {
				return CurrentPlanResult{}, errors.New("Qianchuan plan detail returned a mismatched ad_id")
			}
			if isDeletedPlan(detail.Status, detail.OptStatus) {
				continue
			}
			if request.SelectionPolicy == PlanSelectionUniqueExisting {
				if detail.MarketingGoal != "" && detail.MarketingGoal != "VIDEO_PROM_GOODS" {
					continue
				}
				if !validPositiveID(detail.AwemeID) || len(detail.Products) == 0 {
					return CurrentPlanResult{}, planInventoryError("identity_incomplete", errors.New("plan detail did not establish creator and product identities"))
				}
				for _, product := range detail.Products {
					if !validPositiveID(product.ProductID) {
						return CurrentPlanResult{}, planInventoryError("identity_incomplete", errors.New("plan detail returned an invalid product identity"))
					}
				}
			}
			for _, target := range candidate.targets {
				products := planProductIDs(detail.Products)
				productMatch := hasProductIntersection(detail.Products, target.ProductIDs)
				if target.GroupID != "" {
					productMatch = sameStringSet(products, target.ProductIDs)
				}
				if detail.AwemeID != target.AwemeID ||
					(target.PlanName != "" && detail.Name != target.PlanName) || !productMatch {
					continue
				}
				rawMatches[target.key] = append(rawMatches[target.key], ExistingPlan{
					AdID: detail.AdID, Name: detail.Name, Status: detail.Status,
					OptStatus: detail.OptStatus, AwemeID: detail.AwemeID,
					ProductIDs: products,
				})
			}
		}
	}
	policies := map[string]PlanMatchPolicy{}
	for _, target := range targets {
		candidates := rawMatches[target.key]
		if target.GroupID == "" {
			matches[target.key] = candidates
			continue
		}
		if reconciler.Bindings == nil {
			return CurrentPlanResult{}, errors.New("Qianchuan plan binding reader is required")
		}
		binding, exists := bindings[target.key]
		if request.SelectionPolicy == PlanSelectionBoundOnly {
			var bindingErr error
			binding, exists, bindingErr = reconciler.Bindings.Get(ctx, target.BusinessDate, target.GroupID)
			if bindingErr != nil {
				return CurrentPlanResult{}, fmt.Errorf("read Qianchuan plan binding: %w", bindingErr)
			}
		}
		if !exists {
			bindingDigest, digestErr := PlanBindingDigest(nil)
			if digestErr != nil {
				return CurrentPlanResult{}, fmt.Errorf("digest absent Qianchuan plan binding: %w", digestErr)
			}
			status := "would_create"
			if len(candidates) != 0 {
				status = "legacy_binding_required"
			}
			if request.SelectionPolicy == PlanSelectionUniqueExisting {
				switch len(candidates) {
				case 0:
				case 1:
					claimed, claimErr := reconciler.planClaimedByAnotherGroup(ctx, request, target, candidates[0].AdID)
					if claimErr != nil {
						return CurrentPlanResult{}, claimErr
					}
					status = "unbound_existing"
					if claimed {
						status = "plan_group_conflict"
					} else {
						matches[target.key] = []ExistingPlan{candidates[0]}
					}
				default:
					status = "plan_selection_required"
				}
			}
			policies[target.key] = PlanMatchPolicy{
				Status: status, Candidates: append([]ExistingPlan(nil), candidates...), BindingDigest: bindingDigest,
			}
			continue
		}
		bindingDigest, digestErr := PlanBindingDigest(&binding)
		if digestErr != nil {
			return CurrentPlanResult{}, fmt.Errorf("digest Qianchuan plan binding: %w", digestErr)
		}
		policy := PlanMatchPolicy{
			Status: "binding_drift", Candidates: append([]ExistingPlan(nil), candidates...), BindingDigest: bindingDigest,
		}
		if BindingMatchesIdentity(binding, target.Identity, target.BusinessDate) {
			for _, candidate := range candidates {
				if candidate.AdID == binding.AdID {
					if request.SelectionPolicy == PlanSelectionUniqueExisting {
						claimed, claimErr := reconciler.planClaimedByAnotherGroup(ctx, request, target, candidate.AdID)
						if claimErr != nil {
							return CurrentPlanResult{}, claimErr
						}
						if claimed {
							policy.Status = "plan_group_conflict"
							break
						}
					}
					matches[target.key] = []ExistingPlan{candidate}
					policy.Status = "bound"
					break
				}
			}
		}
		policies[target.key] = policy
	}
	return CurrentPlanResult{
		StartTime: inventory.StartTime, EndTime: inventory.EndTime, PageCount: inventory.PageCount,
		RequestIDs: append([]string(nil), inventory.RequestIDs...), Matches: matches, Policies: policies,
	}, nil
}

func (reconciler CurrentDayReconciler) scopedPlanBindings(ctx context.Context, request CurrentPlanRequest, targets []normalizedCreatorTarget) (map[string]PlanBinding, error) {
	result := map[string]PlanBinding{}
	byDay := map[string]map[string]bool{}
	for _, target := range targets {
		if target.GroupID == "" {
			continue
		}
		if byDay[target.BusinessDate] == nil {
			byDay[target.BusinessDate] = map[string]bool{}
		}
		byDay[target.BusinessDate][target.key] = true
	}
	if len(byDay) != 0 && reconciler.Bindings == nil {
		return nil, errors.New("Qianchuan plan binding reader is required")
	}
	for day, groups := range byDay {
		bindings, err := runReadOnce(ctx, request.ReadPool, "plan-claims\x00"+request.AdvertiserID+"\x00"+day,
			func(ctx context.Context) ([]PlanBinding, error) { return reconciler.Bindings.List(ctx) })
		if err != nil {
			return nil, fmt.Errorf("read Qianchuan plan bindings: %w", err)
		}
		for _, binding := range bindings {
			if binding.AdvertiserID == request.AdvertiserID && binding.BusinessDate == day && groups[binding.GroupID] {
				if err := ValidatePlanBinding(binding); err != nil {
					return nil, err
				}
				result[binding.GroupID] = binding
			}
		}
	}
	return result, nil
}

func (reconciler CurrentDayReconciler) planClaimedByAnotherGroup(
	ctx context.Context, request CurrentPlanRequest, target normalizedCreatorTarget, adID string,
) (bool, error) {
	bindings, err := runReadOnce(ctx, request.ReadPool, "plan-claims\x00"+request.AdvertiserID+"\x00"+target.BusinessDate,
		func(ctx context.Context) ([]PlanBinding, error) { return reconciler.Bindings.List(ctx) })
	if err != nil {
		return false, fmt.Errorf("read Qianchuan plan claims: %w", err)
	}
	for _, binding := range bindings {
		if binding.AdvertiserID == request.AdvertiserID && binding.BusinessDate == target.BusinessDate &&
			binding.AdID == adID && binding.GroupID != target.GroupID {
			return true, nil
		}
	}
	return false, nil
}

func planDetailReadKey(advertiserID, adID string) string {
	return "plan-detail\x00" + advertiserID + "\x00" + adID
}

func (reconciler CurrentDayReconciler) ScanCurrentPlans(
	ctx context.Context,
	request CurrentPlanScanRequest,
) (CurrentPlanInventory, error) {
	if ctx == nil {
		return CurrentPlanInventory{}, errors.New("Qianchuan reconciliation context is required")
	}
	if reconciler.Reader == nil {
		return CurrentPlanInventory{}, errors.New("Qianchuan reconciliation reader is required")
	}
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AccessToken = strings.TrimSpace(request.AccessToken)
	if !validPositiveID(request.AdvertiserID) {
		return CurrentPlanInventory{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.AccessToken == "" {
		return CurrentPlanInventory{}, errors.New("Qianchuan reconciliation access token is required")
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return CurrentPlanInventory{}, fmt.Errorf("load Asia/Shanghai timezone: %w", err)
	}
	day := strings.TrimSpace(request.BusinessDate)
	if day != "" {
		if err := ValidateBusinessDate(day); err != nil {
			return CurrentPlanInventory{}, err
		}
	} else {
		now := time.Now()
		if reconciler.Now != nil {
			now = reconciler.Now()
		}
		day = now.In(location).Format("2006-01-02")
	}
	key := fmt.Sprintf("plan-inventory\x00%s\x00%s\x00ALL\x00VIDEO_PROM_GOODS\x00UNI_PROJECT\x00%d", request.AdvertiserID, day, reconciler.MaxPages)
	return runReadOnce(ctx, request.ReadPool, key, func(ctx context.Context) (CurrentPlanInventory, error) {
		return reconciler.scanCurrentPlans(ctx, request, day)
	})
}

func (reconciler CurrentDayReconciler) scanCurrentPlans(ctx context.Context, request CurrentPlanScanRequest, day string) (CurrentPlanInventory, error) {
	startTime, endTime := day+" 00:00:00", day+" 23:59:59"
	maxPages := reconciler.MaxPages
	if maxPages == 0 {
		maxPages = CurrentPlanMaxPages
	}
	fetchPage := func(ctx context.Context, page int) (domainqianchuan.PlanPage, error) {
		return runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.PlanPage, error) {
			return reconciler.Reader.FetchPlans(ctx, portqianchuan.PlanPageRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
				StartTime: startTime, EndTime: endTime, Status: "ALL",
				MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT",
				NeedCompensateInfo: true, Page: page, PageSize: CurrentPlanPageSize,
			})
		})
	}
	first, err := fetchPage(ctx, 1)
	if err != nil {
		return CurrentPlanInventory{}, planInventoryError("official_query", err)
	}
	if err := validateCurrentPlanPage(first, 1, -1, -1); err != nil {
		return CurrentPlanInventory{}, planInventoryError("pagination_metadata", err)
	}
	if first.PageInfo.TotalPages > maxPages {
		return CurrentPlanInventory{}, planInventoryError("safety_cap", errors.New("pagination exceeds the safety cap"))
	}
	type planPageResult struct {
		page domainqianchuan.PlanPage
		err  error
	}
	remaining := parallelOrdered(ctx, request.ReadPool, max(0, first.PageInfo.TotalPages-1), func(ctx context.Context, index int) planPageResult {
		page := index + 2
		fetched, fetchErr := fetchPage(ctx, page)
		return planPageResult{page: fetched, err: fetchErr}
	})
	pages := make([]domainqianchuan.PlanPage, 1, max(1, first.PageInfo.TotalPages))
	pages[0] = first
	for index, fetched := range remaining {
		page := index + 2
		if fetched.err != nil {
			return CurrentPlanInventory{}, planInventoryError("official_query", fetched.err)
		}
		if err := validateCurrentPlanPage(
			fetched.page, page, first.PageInfo.TotalPages, first.PageInfo.TotalNumber,
		); err != nil {
			return CurrentPlanInventory{}, planInventoryError("pagination_changed", err)
		}
		pages = append(pages, fetched.page)
	}
	plans := make([]domainqianchuan.Plan, 0, first.PageInfo.TotalNumber)
	requestIDs := make([]string, 0, len(pages))
	seenPlanIDs := map[string]struct{}{}
	for _, page := range pages {
		if page.RequestID != "" {
			requestIDs = append(requestIDs, page.RequestID)
		}
		for _, plan := range page.Rows {
			if strings.TrimSpace(plan.AdID) == "" {
				return CurrentPlanInventory{}, planInventoryError("invalid_plan", errors.New("plan has no unique key"))
			}
			if _, duplicate := seenPlanIDs[plan.AdID]; duplicate {
				return CurrentPlanInventory{}, planInventoryError("duplicate_plan", errors.New("duplicate plan unique key"))
			}
			seenPlanIDs[plan.AdID] = struct{}{}
			plans = append(plans, plan)
		}
	}
	if len(plans) != first.PageInfo.TotalNumber {
		return CurrentPlanInventory{}, planInventoryError("count_mismatch", errors.New("pagination row count differs from declared total"))
	}
	return CurrentPlanInventory{
		StartTime: startTime, EndTime: endTime, PageCount: len(pages),
		RequestIDs: requestIDs, Plans: plans,
	}, nil
}

func validateCurrentPlanPage(page domainqianchuan.PlanPage, number, totalPages, totalNumber int) error {
	info := page.PageInfo
	if info.Page != number || info.TotalPages < 0 || info.TotalNumber < 0 {
		return fmt.Errorf("page %d returned invalid pagination metadata", number)
	}
	if info.TotalPages == 0 {
		if number != 1 || info.TotalNumber != 0 || len(page.Rows) != 0 {
			return fmt.Errorf("page %d returned contradictory empty pagination metadata", number)
		}
		return nil
	}
	if number > info.TotalPages {
		return fmt.Errorf("page %d exceeds declared total pages %d", number, info.TotalPages)
	}
	if totalPages >= 0 && (info.TotalPages != totalPages || info.TotalNumber != totalNumber) {
		return fmt.Errorf("page %d changed declared pagination totals", number)
	}
	return nil
}

type normalizedCreatorTarget struct {
	CreatorTarget
	key      string
	aliases  map[string]struct{}
	products map[string]struct{}
}

func normalizeCreatorTargets(values []CreatorTarget) ([]normalizedCreatorTarget, error) {
	if len(values) == 0 {
		return nil, errors.New("Qianchuan reconciliation requires at least one creator target")
	}
	result := make([]normalizedCreatorTarget, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value.AwemeID = strings.TrimSpace(value.AwemeID)
		value.VisibleID = strings.TrimSpace(value.VisibleID)
		value.PlanName = strings.TrimSpace(value.PlanName)
		if !validPositiveID(value.AwemeID) {
			return nil, errors.New("aweme_id must be a positive decimal ID")
		}
		key := value.AwemeID
		if strings.TrimSpace(value.GroupID) != "" {
			value.GroupID = strings.TrimSpace(value.GroupID)
			value.BusinessDate = strings.TrimSpace(value.BusinessDate)
			canonicalID, groupErr := domainqianchuan.GroupID(value.Identity)
			if groupErr != nil || canonicalID != value.GroupID || value.Identity.CreatorID != value.AwemeID {
				return nil, errors.New("Qianchuan reconciliation group identity is invalid")
			}
			if _, dateErr := BindingKey(value.GroupID, value.BusinessDate); dateErr != nil {
				return nil, dateErr
			}
			key = value.GroupID
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("Qianchuan reconciliation creator targets must be unique")
		}
		seen[key] = struct{}{}
		products, productIDs, err := normalizeIDSet(value.ProductIDs, "product_id", 30)
		if err != nil {
			return nil, err
		}
		value.ProductIDs = productIDs
		aliases := map[string]struct{}{value.AwemeID: {}}
		if value.VisibleID != "" {
			aliases[value.VisibleID] = struct{}{}
		}
		result = append(result, normalizedCreatorTarget{
			CreatorTarget: value, key: key, aliases: aliases, products: products,
		})
	}
	return result, nil
}

type listCandidate struct {
	plan    domainqianchuan.Plan
	targets []normalizedCreatorTarget
}

func selectListCandidates(
	plans []domainqianchuan.Plan,
	targets []normalizedCreatorTarget,
) []listCandidate {
	result := []listCandidate{}
	for _, plan := range plans {
		if isDeletedPlan(plan.Status, plan.OptStatus) {
			continue
		}
		candidateTargets := []normalizedCreatorTarget{}
		for _, target := range targets {
			if target.PlanName != "" && plan.Name != "" && plan.Name != target.PlanName {
				continue
			}
			if planHasCreatorAlias(plan.Creators, target.aliases) {
				candidateTargets = append(candidateTargets, target)
			}
		}
		if len(candidateTargets) != 0 {
			result = append(result, listCandidate{plan: plan, targets: candidateTargets})
		}
	}
	return result
}

// Full-domain list responses may expose a mutable visible Douyin handle rather
// than the numeric UID. Confirm every plan that could have this product identity
// from its detail, so an absent/stale handle cannot become a false create proof.
func selectIdentityCandidates(plans []domainqianchuan.Plan, targets []normalizedCreatorTarget) []listCandidate {
	result := []listCandidate{}
	for _, plan := range plans {
		if isDeletedPlan(plan.Status, plan.OptStatus) ||
			(plan.MarketingGoal != "" && plan.MarketingGoal != "VIDEO_PROM_GOODS") ||
			(plan.AdlabScene != "" && plan.AdlabScene != "UNI_PROJECT") {
			continue
		}
		candidateTargets := []normalizedCreatorTarget{}
		for _, target := range targets {
			if target.PlanName != "" && plan.Name != "" && plan.Name != target.PlanName {
				continue
			}
			possible := true
			if target.GroupID != "" {
				for _, product := range plan.Products {
					if product.ProductID != "" {
						if _, wanted := target.products[product.ProductID]; !wanted {
							possible = false
							break
						}
					}
				}
			}
			if possible {
				candidateTargets = append(candidateTargets, target)
			}
		}
		if len(candidateTargets) > 0 {
			result = append(result, listCandidate{plan: plan, targets: candidateTargets})
		}
	}
	return result
}

func planHasCreatorAlias(creators []domainqianchuan.Creator, aliases map[string]struct{}) bool {
	for _, creator := range creators {
		for _, value := range []string{creator.VisibleID, creator.AwemeID} {
			if _, match := aliases[strings.TrimSpace(value)]; match {
				return true
			}
		}
	}
	return false
}

func hasProductIntersection(products []domainqianchuan.PlanProduct, targets []string) bool {
	wanted := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		wanted[target] = struct{}{}
	}
	for _, product := range products {
		if _, match := wanted[product.ProductID]; match {
			return true
		}
	}
	return false
}

func planProductIDs(products []domainqianchuan.PlanProduct) []string {
	result := make([]string, 0, len(products))
	seen := map[string]struct{}{}
	for _, product := range products {
		if product.ProductID == "" {
			continue
		}
		if _, duplicate := seen[product.ProductID]; duplicate {
			continue
		}
		seen[product.ProductID] = struct{}{}
		result = append(result, product.ProductID)
	}
	return result
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	if len(seen) != len(right) {
		return false
	}
	for _, value := range right {
		if _, exists := seen[value]; !exists {
			return false
		}
	}
	return true
}

func isDeletedPlan(status, optStatus string) bool {
	for _, value := range []string{status, optStatus} {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "DELETE", "DELETED":
			return true
		}
	}
	return false
}
