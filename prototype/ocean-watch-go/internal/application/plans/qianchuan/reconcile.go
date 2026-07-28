package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
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
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return CurrentPlanResult{}, fmt.Errorf("load Asia/Shanghai timezone: %w", err)
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
	requestIDs := []string{}
	pageCount := 0
	plans, err := pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.Plan]{
		MaxPages: maxPages,
		Key:      func(plan domainqianchuan.Plan) string { return plan.AdID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.Plan], error) {
			pageCount++
			result, fetchErr := reconciler.Reader.FetchPlans(ctx, portqianchuan.PlanPageRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
				StartTime: startTime, EndTime: endTime, Status: "ALL",
				MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT",
				NeedCompensateInfo: true, Page: page, PageSize: CurrentPlanPageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainqianchuan.Plan]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainqianchuan.Plan]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return CurrentPlanResult{}, fmt.Errorf("scan current-day Qianchuan plans: %w", err)
	}
	matches := make(map[string][]ExistingPlan, len(targets))
	for _, target := range targets {
		matches[target.AwemeID] = []ExistingPlan{}
	}
	candidates := selectListCandidates(plans, targets)
	for _, candidate := range candidates {
		detail, fetchErr := reconciler.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken, AdID: candidate.plan.AdID,
		})
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
		StartTime: startTime, EndTime: endTime, PageCount: pageCount,
		RequestIDs: requestIDs, Matches: matches,
	}, nil
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
