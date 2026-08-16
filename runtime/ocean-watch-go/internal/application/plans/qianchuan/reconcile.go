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
)

type CreatorTarget struct {
	AwemeID    string
	VisibleID  string
	ProductIDs []string
	PlanName   string
}

type CurrentPlanRequest struct {
	AdvertiserID string
	AccessToken  string
	Targets      []CreatorTarget
	ReadPool     *ReadPool
	Inventory    *CurrentPlanInventory
}

type CurrentPlanScanRequest struct {
	AdvertiserID string
	AccessToken  string
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
	StartTime  string                    `json:"start_time"`
	EndTime    string                    `json:"end_time"`
	PageCount  int                       `json:"page_count"`
	RequestIDs []string                  `json:"request_ids"`
	Matches    map[string][]ExistingPlan `json:"matches"`
}

type CurrentPlanFinder interface {
	FindCurrentPlans(context.Context, CurrentPlanRequest) (CurrentPlanResult, error)
}

type CurrentPlanScanner interface {
	ScanCurrentPlans(context.Context, CurrentPlanScanRequest) (CurrentPlanInventory, error)
}

type CurrentDayReconciler struct {
	Reader   portqianchuan.Reader
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
	targets, err := normalizeCreatorTargets(request.Targets)
	if err != nil {
		return CurrentPlanResult{}, err
	}
	inventory := request.Inventory
	if inventory == nil {
		scanned, scanErr := reconciler.ScanCurrentPlans(ctx, CurrentPlanScanRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken, ReadPool: request.ReadPool,
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
	matches := make(map[string][]ExistingPlan, len(targets))
	for _, target := range targets {
		matches[target.AwemeID] = []ExistingPlan{}
	}
	candidates := selectListCandidates(inventory.Plans, targets)
	type candidateDetail struct {
		detail domainqianchuan.PlanDetail
		err    error
	}
	details := parallelOrdered(ctx, request.ReadPool, len(candidates), func(ctx context.Context, index int) candidateDetail {
		candidate := candidates[index]
		detail, fetchErr := runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.PlanDetail, error) {
			return reconciler.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken, AdID: candidate.plan.AdID,
			})
		})
		return candidateDetail{detail: detail, err: fetchErr}
	})
	for index, candidate := range candidates {
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
		for _, target := range candidate.targets {
			if detail.AwemeID != target.AwemeID ||
				(target.PlanName != "" && detail.Name != target.PlanName) ||
				!hasProductIntersection(detail.Products, target.ProductIDs) {
				continue
			}
			matches[target.AwemeID] = append(matches[target.AwemeID], ExistingPlan{
				AdID: detail.AdID, Name: detail.Name, Status: detail.Status,
				OptStatus: detail.OptStatus, AwemeID: detail.AwemeID,
				ProductIDs: planProductIDs(detail.Products),
			})
		}
	}
	return CurrentPlanResult{
		StartTime: inventory.StartTime, EndTime: inventory.EndTime, PageCount: inventory.PageCount,
		RequestIDs: append([]string(nil), inventory.RequestIDs...), Matches: matches,
	}, nil
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
	now := time.Now()
	if reconciler.Now != nil {
		now = reconciler.Now()
	}
	day := now.In(location).Format("2006-01-02")
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
		return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: %w", err)
	}
	if err := validateCurrentPlanPage(first, 1, -1, -1); err != nil {
		return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: %w", err)
	}
	if first.PageInfo.TotalPages > maxPages {
		return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: pagination exceeds the safety cap of %d pages", maxPages)
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
			return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: fetch page %d: %w", page, fetched.err)
		}
		if err := validateCurrentPlanPage(
			fetched.page, page, first.PageInfo.TotalPages, first.PageInfo.TotalNumber,
		); err != nil {
			return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: %w", err)
		}
		pages = append(pages, fetched.page)
	}
	plans := make([]domainqianchuan.Plan, 0, first.PageInfo.TotalNumber)
	requestIDs := make([]string, 0, len(pages))
	seenPlanIDs := map[string]struct{}{}
	for pageIndex, page := range pages {
		if page.RequestID != "" {
			requestIDs = append(requestIDs, page.RequestID)
		}
		for _, plan := range page.Rows {
			if strings.TrimSpace(plan.AdID) == "" {
				return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: page %d returned an empty unique key", pageIndex+1)
			}
			if _, duplicate := seenPlanIDs[plan.AdID]; duplicate {
				return CurrentPlanInventory{}, fmt.Errorf("scan current-day Qianchuan plans: page %d returned duplicate unique key %q", pageIndex+1, plan.AdID)
			}
			seenPlanIDs[plan.AdID] = struct{}{}
			plans = append(plans, plan)
		}
	}
	if len(plans) != first.PageInfo.TotalNumber {
		return CurrentPlanInventory{}, fmt.Errorf(
			"scan current-day Qianchuan plans: pagination returned %d unique rows but declared %d",
			len(plans), first.PageInfo.TotalNumber,
		)
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
		if _, duplicate := seen[value.AwemeID]; duplicate {
			return nil, errors.New("Qianchuan reconciliation creator targets must be unique")
		}
		seen[value.AwemeID] = struct{}{}
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
			CreatorTarget: value, aliases: aliases, products: products,
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

func isDeletedPlan(status, optStatus string) bool {
	for _, value := range []string{status, optStatus} {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "DELETE", "DELETED":
			return true
		}
	}
	return false
}
