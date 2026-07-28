package qianchuan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	ProductEndpoint            = "/v1.0/qianchuan/uni_promotion/product/get/"
	PlanListEndpoint           = "/v1.0/qianchuan/uni_promotion/list/"
	PlanDetailEndpoint         = "/v1.0/qianchuan/uni_promotion/ad/detail/"
	PlanMaterialsEndpoint      = "/v1.0/qianchuan/uni_promotion/ad/material/get/"
	AuthorizedCreatorsEndpoint = "/v1.0/qianchuan/uni_aweme/authorized/get/"
	CreatorVideosEndpoint      = "/v1.0/qianchuan/file/video/aweme/get/"
	DefaultPageSize            = 100
	DefaultMaxPages            = 100
	MaxProductPageSize         = 100
	MaxMaterialPageSize        = 100
	MaxCreatorPageSize         = 100
	MaxCreatorVideoCount       = 50
	MaxAllowedPages            = 100
)

var (
	productTabs        = stringSet("ALL", "BREAKTHROUGH_PRODUCT", "GOOD_BOOST", "NEW_PRODUCT", "SEARCH_TREND")
	productOrderFields = stringSet("SELL_NUM", "STOCK", "AUDIT_TIME")
	orderTypes         = stringSet("ASC", "DESC")
	productPlatforms   = stringSet("ECP_AWEME", "QIANCHUAN")
	planStatuses       = stringSet(
		"ADVERTISER_OFFLINE_BUDGET", "ALL", "ALL_INCLUDE_DELETED", "AUDIT", "DELETED",
		"DELIVERY_OK", "DISABLE", "EXTERNAL_URL_DISABLE", "FROZEN", "LIVE_ROOM_OFF",
		"NO_SCHEDULE", "OFFLINE_AUDIT", "OFFLINE_BALANCE", "OFFLINE_BUDGET",
		"QUOTA_DISABLE", "REAUDIT", "ROI2_DISABLE", "SYSTEM_DISABLE", "TIME_DONE", "TIME_NO_REACH",
	)
	planPageSizes     = intSet(10, 20, 50, 100, 200)
	materialPageSizes = intSet(10, 20, 50, 100)
)

type TokenProvider interface {
	Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error)
}

type Service struct {
	Tokens    TokenProvider
	Reader    portqianchuan.Reader
	Templates ProductTemplateResolver
	Now       func() time.Time
}

type ProductTemplateResolver interface {
	ResolveQianchuanProductBinding(context.Context, string) (domaintemplates.QianchuanProductBinding, error)
}

type CredentialScope struct {
	AdvertiserID  string
	AuthAccountID string
}

type ProductQuery struct {
	CredentialScope
	ProductIDs     []string
	ProductName    string
	Tab            string
	AwemeID        string
	OnlyUnpromoted bool
	OrderField     string
	OrderType      string
	Platform       string
	PageSize       int
	MaxPages       int
}

type ProductResult struct {
	Mode         string                    `json:"mode"`
	Endpoint     string                    `json:"endpoint"`
	AdvertiserID string                    `json:"advertiser_id"`
	Filters      ProductFilters            `json:"filters"`
	ProductCount int                       `json:"product_count"`
	Products     []domainqianchuan.Product `json:"products"`
	PageCount    int                       `json:"page_count"`
	RequestIDs   []string                  `json:"request_ids"`
	Truncated    bool                      `json:"truncated"`
}

type ProductFilters struct {
	ProductIDs     []string `json:"product_ids,omitempty"`
	ProductName    string   `json:"product_name,omitempty"`
	Tab            string   `json:"tab"`
	AwemeID        string   `json:"aweme_id,omitempty"`
	OnlyUnpromoted bool     `json:"create_roi2_limit_product,omitempty"`
	OrderField     string   `json:"order_field"`
	OrderType      string   `json:"order_type"`
	Platform       string   `json:"platform,omitempty"`
}

type PlanListQuery struct {
	CredentialScope
	StartDate string
	EndDate   string
	Status    string
	PageSize  int
	MaxPages  int
	Top       int
	Full      bool
}

type PlanListResult struct {
	Mode           string     `json:"mode"`
	Endpoint       string     `json:"endpoint"`
	AdvertiserID   string     `json:"advertiser_id"`
	PlanCount      int        `json:"plan_count"`
	DisplayedCount int        `json:"displayed_count"`
	Plans          any        `json:"plans"`
	PageCount      int        `json:"page_count"`
	DataPeriod     DatePeriod `json:"data_period"`
	RequestIDs     []string   `json:"request_ids"`
	Truncated      bool       `json:"truncated"`
}

type CompactPlan struct {
	AdID          string          `json:"ad_id,omitempty"`
	Name          string          `json:"name,omitempty"`
	Status        string          `json:"status,omitempty"`
	OptStatus     string          `json:"opt_status,omitempty"`
	CreateTime    string          `json:"create_time,omitempty"`
	MarketingGoal string          `json:"marketing_goal,omitempty"`
	CreatorIDs    []string        `json:"creator_ids"`
	Budget        *domain.Decimal `json:"budget,omitempty"`
	SmartBidType  string          `json:"smart_bid_type,omitempty"`
	ROI2Goal      *domain.Decimal `json:"roi2_goal,omitempty"`
}

type DatePeriod struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type PlanDetailResult struct {
	Mode         string                     `json:"mode"`
	Endpoint     string                     `json:"endpoint"`
	AdvertiserID string                     `json:"advertiser_id"`
	AdID         string                     `json:"ad_id"`
	Plan         domainqianchuan.PlanDetail `json:"plan"`
}

type PlanMaterialsQuery struct {
	CredentialScope
	AdID     string
	PageSize int
	MaxPages int
}

type PlanMaterialsResult struct {
	Mode          string                         `json:"mode"`
	Endpoint      string                         `json:"endpoint"`
	AdvertiserID  string                         `json:"advertiser_id"`
	AdID          string                         `json:"ad_id"`
	MaterialCount int                            `json:"material_count"`
	Materials     []domainqianchuan.PlanMaterial `json:"materials"`
	PageCount     int                            `json:"page_count"`
	RequestIDs    []string                       `json:"request_ids"`
	Truncated     bool                           `json:"truncated"`
}

type AuthorizedCreatorQuery struct {
	CredentialScope
	SearchKeyword string
	PageSize      int
	MaxPages      int
}

type AuthorizedCreatorResult struct {
	Endpoint       string                              `json:"endpoint"`
	AdvertiserID   string                              `json:"advertiser_id"`
	SearchKeywords *string                             `json:"search_keywords"`
	Creators       []domainqianchuan.AuthorizedCreator `json:"creators"`
	CreatorCount   int                                 `json:"creator_count"`
	PageCount      int                                 `json:"page_count"`
	RequestIDs     []string                            `json:"request_ids"`
	Truncated      bool                                `json:"truncated"`
}

type CreatorVideoQuery struct {
	PlanTemplate  string
	DouyinID      string
	CreatorName   string
	AuthAccountID string
	PageSize      int
	MaxPages      int
}

type CreatorResolution struct {
	domainqianchuan.AuthorizedCreator
	MatchField           string   `json:"match_field"`
	RequestedDouyinID    string   `json:"requested_douyin_id"`
	RequestedCreatorName *string  `json:"requested_creator_name"`
	CreatorNameMatches   *bool    `json:"creator_name_matches"`
	Endpoint             string   `json:"endpoint"`
	PageCount            int      `json:"page_count"`
	RequestIDs           []string `json:"request_ids"`
}

type CreatorVideoQueryRow struct {
	ProductID    string   `json:"product_id"`
	MatchedCount int      `json:"matched_count"`
	PageCount    int      `json:"page_count"`
	RequestIDs   []string `json:"request_ids"`
	Truncated    bool     `json:"truncated"`
}

type CreatorVideoResult struct {
	Endpoint                  string                         `json:"endpoint"`
	CreatorResolutionEndpoint string                         `json:"creator_resolution_endpoint"`
	AdvertiserID              string                         `json:"advertiser_id"`
	TemplateID                string                         `json:"template_id"`
	TemplateName              string                         `json:"template_name"`
	ProductIDs                []string                       `json:"product_ids"`
	DouyinID                  string                         `json:"douyin_id"`
	AwemeID                   string                         `json:"aweme_id"`
	CreatorName               string                         `json:"creator_name,omitempty"`
	ResolvedCreator           CreatorResolution              `json:"resolved_creator"`
	QueryCount                int                            `json:"query_count"`
	MaterialCount             int                            `json:"material_count"`
	Queries                   []CreatorVideoQueryRow         `json:"queries"`
	Materials                 []domainqianchuan.CreatorVideo `json:"materials"`
}

func (service Service) QueryProducts(ctx context.Context, query ProductQuery, mode string) (ProductResult, error) {
	if err := service.validate(); err != nil {
		return ProductResult{}, err
	}
	query = normalizeProductQuery(query)
	if err := validateProductQuery(query); err != nil {
		return ProductResult{}, err
	}
	if mode != "qianchuan_product_list" && mode != "qianchuan_product_search" {
		return ProductResult{}, errors.New("invalid Qianchuan product query mode")
	}
	if mode == "qianchuan_product_search" && len(query.ProductIDs) == 0 && query.ProductName == "" {
		return ProductResult{}, errors.New("search requires product IDs or a product name")
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return ProductResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return ProductResult{}, err
	}
	requestIDs := []string{}
	pages := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.Product]{
		MaxPages: query.MaxPages,
		Key:      func(row domainqianchuan.Product) string { return row.ProductID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.Product], error) {
			pages++
			result, fetchErr := service.Reader.FetchProducts(ctx, portqianchuan.ProductPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				ProductIDs: query.ProductIDs, ProductName: query.ProductName, Tab: query.Tab,
				AwemeID: query.AwemeID, OnlyUnpromoted: query.OnlyUnpromoted,
				OrderField: query.OrderField, OrderType: query.OrderType, Platform: query.Platform,
				Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainqianchuan.Product]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainqianchuan.Product]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return ProductResult{}, fmt.Errorf("query Qianchuan products: %w", err)
	}
	return ProductResult{
		Mode: mode, Endpoint: ProductEndpoint, AdvertiserID: query.AdvertiserID,
		Filters: ProductFilters{ProductIDs: query.ProductIDs, ProductName: query.ProductName,
			Tab: query.Tab, AwemeID: query.AwemeID, OnlyUnpromoted: query.OnlyUnpromoted,
			OrderField: query.OrderField, OrderType: query.OrderType, Platform: query.Platform},
		ProductCount: len(rows), Products: rows, PageCount: pages, RequestIDs: requestIDs,
		Truncated: false,
	}, nil
}

func (service Service) ListPlans(ctx context.Context, query PlanListQuery) (PlanListResult, error) {
	if err := service.validate(); err != nil {
		return PlanListResult{}, err
	}
	query = service.normalizePlanListQuery(query)
	if err := validatePlanListQuery(query); err != nil {
		return PlanListResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return PlanListResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return PlanListResult{}, err
	}
	requestIDs := []string{}
	pages := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.Plan]{
		MaxPages: query.MaxPages,
		Key:      func(row domainqianchuan.Plan) string { return row.AdID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.Plan], error) {
			pages++
			result, fetchErr := service.Reader.FetchPlans(ctx, portqianchuan.PlanPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				StartTime: query.StartDate + " 00:00:00", EndTime: query.EndDate + " 23:59:59",
				Status: query.Status, MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT",
				NeedCompensateInfo: true, Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainqianchuan.Plan]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainqianchuan.Plan]{Number: result.PageInfo.Page,
				TotalPages: result.PageInfo.TotalPages, TotalNumber: result.PageInfo.TotalNumber,
				Rows: result.Rows}, nil
		},
	})
	if err != nil {
		return PlanListResult{}, fmt.Errorf("query Qianchuan plans: %w", err)
	}
	displayed := rows
	if query.Top > 0 && query.Top < len(rows) {
		displayed = rows[:query.Top]
	}
	var plans any = compactPlans(displayed)
	if query.Full {
		plans = displayed
	}
	return PlanListResult{
		Mode: "qianchuan_plan_list", Endpoint: PlanListEndpoint, AdvertiserID: query.AdvertiserID,
		PlanCount: len(rows), DisplayedCount: len(displayed), Plans: plans, PageCount: pages,
		DataPeriod: DatePeriod{StartDate: query.StartDate, EndDate: query.EndDate},
		RequestIDs: requestIDs, Truncated: false,
	}, nil
}

func (service Service) ShowPlan(ctx context.Context, scope CredentialScope, adID string) (PlanDetailResult, error) {
	if err := service.validate(); err != nil {
		return PlanDetailResult{}, err
	}
	scope = normalizeCredentialScope(scope)
	adID = strings.TrimSpace(adID)
	if err := validatePositiveID(adID, "ad_id"); err != nil {
		return PlanDetailResult{}, err
	}
	lease, err := service.token(ctx, scope)
	if err != nil {
		return PlanDetailResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return PlanDetailResult{}, err
	}
	plan, err := service.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
		AdvertiserID: scope.AdvertiserID, AccessToken: lease.AccessToken, AdID: adID,
	})
	if err != nil {
		return PlanDetailResult{}, fmt.Errorf("query Qianchuan plan detail: %w", err)
	}
	if plan.AdID != adID {
		return PlanDetailResult{}, errors.New("Qianchuan plan detail returned a mismatched ad_id")
	}
	return PlanDetailResult{Mode: "qianchuan_plan_detail", Endpoint: PlanDetailEndpoint,
		AdvertiserID: scope.AdvertiserID, AdID: adID, Plan: plan}, nil
}

func (service Service) ListPlanMaterials(ctx context.Context, query PlanMaterialsQuery) (PlanMaterialsResult, error) {
	if err := service.validate(); err != nil {
		return PlanMaterialsResult{}, err
	}
	query.CredentialScope = normalizeCredentialScope(query.CredentialScope)
	query.PageSize, query.MaxPages = normalizePagination(query.PageSize, query.MaxPages)
	query.AdID = strings.TrimSpace(query.AdID)
	if err := validatePositiveID(query.AdID, "ad_id"); err != nil {
		return PlanMaterialsResult{}, err
	}
	if err := validatePagination(query.PageSize, query.MaxPages, materialPageSizes, MaxMaterialPageSize); err != nil {
		return PlanMaterialsResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return PlanMaterialsResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return PlanMaterialsResult{}, err
	}
	requestIDs := []string{}
	pages := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.PlanMaterial]{
		MaxPages: query.MaxPages,
		Key:      materialKey,
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.PlanMaterial], error) {
			pages++
			result, fetchErr := service.Reader.FetchPlanMaterials(ctx, portqianchuan.MaterialPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken, AdID: query.AdID,
				MaterialType: "VIDEO", MaterialStatus: "ALL", Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainqianchuan.PlanMaterial]{}, fetchErr
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainqianchuan.PlanMaterial]{Number: result.PageInfo.Page,
				TotalPages: result.PageInfo.TotalPages, TotalNumber: result.PageInfo.TotalNumber,
				Rows: result.Rows}, nil
		},
	})
	if err != nil {
		return PlanMaterialsResult{}, fmt.Errorf("query Qianchuan plan materials: %w", err)
	}
	return PlanMaterialsResult{Mode: "qianchuan_plan_materials", Endpoint: PlanMaterialsEndpoint,
		AdvertiserID: query.AdvertiserID, AdID: query.AdID, MaterialCount: len(rows),
		Materials: rows, PageCount: pages, RequestIDs: requestIDs, Truncated: false}, nil
}

func (service Service) ListAuthorizedCreators(
	ctx context.Context,
	query AuthorizedCreatorQuery,
) (AuthorizedCreatorResult, error) {
	if err := service.validate(); err != nil {
		return AuthorizedCreatorResult{}, err
	}
	query.CredentialScope = normalizeCredentialScope(query.CredentialScope)
	query.SearchKeyword = strings.TrimSpace(query.SearchKeyword)
	query.PageSize, query.MaxPages = normalizePagination(query.PageSize, query.MaxPages)
	if err := validateAuthorizedCreatorQuery(query); err != nil {
		return AuthorizedCreatorResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return AuthorizedCreatorResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return AuthorizedCreatorResult{}, err
	}
	rows, pageCount, requestIDs, truncated, err := service.collectAuthorizedCreators(
		ctx, query.CredentialScope, lease.AccessToken, query.SearchKeyword, query.PageSize, query.MaxPages,
	)
	if err != nil {
		return AuthorizedCreatorResult{}, fmt.Errorf("query Qianchuan authorized creators: %w", err)
	}
	var searchKeywords *string
	if query.SearchKeyword != "" {
		value := query.SearchKeyword
		searchKeywords = &value
	}
	return AuthorizedCreatorResult{
		Endpoint: AuthorizedCreatorsEndpoint, AdvertiserID: query.AdvertiserID,
		SearchKeywords: searchKeywords, Creators: rows, CreatorCount: len(rows),
		PageCount: pageCount, RequestIDs: requestIDs, Truncated: truncated,
	}, nil
}

func (service Service) QueryCreatorVideos(
	ctx context.Context,
	query CreatorVideoQuery,
) (CreatorVideoResult, error) {
	if err := service.validate(); err != nil {
		return CreatorVideoResult{}, err
	}
	if service.Templates == nil {
		return CreatorVideoResult{}, errors.New("Qianchuan product template resolver is required")
	}
	query.PlanTemplate = strings.TrimSpace(query.PlanTemplate)
	query.DouyinID = strings.TrimSpace(query.DouyinID)
	query.CreatorName = strings.TrimSpace(query.CreatorName)
	query.AuthAccountID = strings.TrimSpace(query.AuthAccountID)
	if query.PageSize == 0 {
		query.PageSize = MaxCreatorVideoCount
	}
	if query.MaxPages == 0 {
		query.MaxPages = DefaultMaxPages
	}
	if query.PlanTemplate == "" {
		return CreatorVideoResult{}, errors.New("plan_template is required")
	}
	if query.DouyinID == "" {
		return CreatorVideoResult{}, errors.New("douyin_id is required")
	}
	if err := validatePagination(query.PageSize, query.MaxPages, nil, MaxCreatorVideoCount); err != nil {
		return CreatorVideoResult{}, err
	}
	binding, err := service.Templates.ResolveQianchuanProductBinding(ctx, query.PlanTemplate)
	if err != nil {
		return CreatorVideoResult{}, err
	}
	if err := validateProductBinding(binding); err != nil {
		return CreatorVideoResult{}, err
	}
	scope := CredentialScope{AdvertiserID: binding.AdvertiserID, AuthAccountID: query.AuthAccountID}
	lease, err := service.token(ctx, scope)
	if err != nil {
		return CreatorVideoResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return CreatorVideoResult{}, err
	}
	resolved, err := service.resolveAuthorizedCreator(
		ctx, scope, lease.AccessToken, query.DouyinID, query.CreatorName, query.MaxPages,
	)
	if err != nil {
		return CreatorVideoResult{}, err
	}
	merged := make([]domainqianchuan.CreatorVideo, 0)
	mergedIndex := map[string]int{}
	queryRows := make([]CreatorVideoQueryRow, 0, len(binding.ProductIDs))
	for _, productID := range binding.ProductIDs {
		videos, pageCount, requestIDs, fetchErr := service.collectCreatorVideos(
			ctx, scope, lease.AccessToken, resolved.AwemeID, productID, query.PageSize, query.MaxPages,
		)
		if fetchErr != nil {
			return CreatorVideoResult{}, fmt.Errorf("query Qianchuan creator videos for product %s: %w", productID, fetchErr)
		}
		for _, video := range videos {
			key := creatorVideoKey(video)
			index, exists := mergedIndex[key]
			if !exists {
				video.MatchedProductIDs = []string{productID}
				mergedIndex[key] = len(merged)
				merged = append(merged, video)
				continue
			}
			merged[index].MatchedProductIDs = appendUnique(merged[index].MatchedProductIDs, productID)
		}
		queryRows = append(queryRows, CreatorVideoQueryRow{
			ProductID: productID, MatchedCount: len(videos), PageCount: pageCount,
			RequestIDs: requestIDs, Truncated: false,
		})
	}
	creatorName := resolved.Name
	if creatorName == "" {
		creatorName = query.CreatorName
	}
	return CreatorVideoResult{
		Endpoint: CreatorVideosEndpoint, CreatorResolutionEndpoint: AuthorizedCreatorsEndpoint,
		AdvertiserID: binding.AdvertiserID, TemplateID: binding.TemplateID,
		TemplateName: binding.DisplayName, ProductIDs: append([]string(nil), binding.ProductIDs...),
		DouyinID: query.DouyinID, AwemeID: resolved.AwemeID, CreatorName: creatorName,
		ResolvedCreator: resolved, QueryCount: len(queryRows), MaterialCount: len(merged),
		Queries: queryRows, Materials: merged,
	}, nil
}

func (service Service) collectAuthorizedCreators(
	ctx context.Context,
	scope CredentialScope,
	accessToken string,
	searchKeyword string,
	pageSize int,
	maxPages int,
) ([]domainqianchuan.AuthorizedCreator, int, []string, bool, error) {
	rows := make([]domainqianchuan.AuthorizedCreator, 0)
	seen := map[string]struct{}{}
	requestIDs := []string{}
	expectedPages, expectedTotal := -1, -1
	for page := 1; page <= maxPages; page++ {
		result, err := service.Reader.FetchAuthorizedCreators(ctx, portqianchuan.AuthorizedCreatorPageRequest{
			AdvertiserID: scope.AdvertiserID, AccessToken: accessToken, SearchKeyword: searchKeyword,
			MarketingGoal: "VIDEO_PROM_GOODS", Scene: "CREATE", Page: page, PageSize: pageSize,
		})
		if err != nil {
			return nil, page - 1, requestIDs, false, fmt.Errorf("fetch page %d: %w", page, err)
		}
		if result.PageInfo.Page != page || result.PageInfo.TotalPages < 0 || result.PageInfo.TotalNumber < 0 {
			return nil, page, requestIDs, false, fmt.Errorf("page %d returned invalid pagination metadata", page)
		}
		if expectedPages < 0 {
			expectedPages, expectedTotal = result.PageInfo.TotalPages, result.PageInfo.TotalNumber
		} else if result.PageInfo.TotalPages != expectedPages || result.PageInfo.TotalNumber != expectedTotal {
			return nil, page, requestIDs, false, fmt.Errorf("page %d changed declared pagination totals", page)
		}
		if expectedPages == 0 {
			if page != 1 || expectedTotal != 0 || len(result.Rows) != 0 {
				return nil, page, requestIDs, false, errors.New("authorized creator pagination returned contradictory empty metadata")
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return rows, page, requestIDs, false, nil
		}
		if page > expectedPages {
			return nil, page, requestIDs, false, fmt.Errorf("page %d exceeds declared total pages %d", page, expectedPages)
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		for _, row := range result.Rows {
			if row.AwemeID == "" {
				return nil, page, requestIDs, false, fmt.Errorf("page %d returned an empty creator identity", page)
			}
			if _, exists := seen[row.AwemeID]; exists {
				return nil, page, requestIDs, false, fmt.Errorf("page %d returned duplicate creator %q", page, row.AwemeID)
			}
			seen[row.AwemeID] = struct{}{}
			rows = append(rows, row)
		}
		if page == expectedPages {
			if len(rows) != expectedTotal {
				return nil, page, requestIDs, false, fmt.Errorf(
					"authorized creator pagination returned %d unique rows but declared %d", len(rows), expectedTotal,
				)
			}
			return rows, page, requestIDs, false, nil
		}
		if page == maxPages {
			return rows, page, requestIDs, true, nil
		}
	}
	return rows, maxPages, requestIDs, true, nil
}

func (service Service) resolveAuthorizedCreator(
	ctx context.Context,
	scope CredentialScope,
	accessToken string,
	douyinID string,
	creatorName string,
	maxPages int,
) (CreatorResolution, error) {
	rows, pages, requestIDs, truncated, err := service.collectAuthorizedCreators(
		ctx, scope, accessToken, douyinID, MaxCreatorPageSize, maxPages,
	)
	if err != nil {
		return CreatorResolution{}, fmt.Errorf("resolve Qianchuan authorized creator: %w", err)
	}
	if truncated {
		return CreatorResolution{}, domain.NewError(
			"configuration_error", "Qianchuan authorized creator search exceeded max_pages", 2,
			map[string]any{"douyin_id": douyinID, "advertiser_id": scope.AdvertiserID, "page_count": pages},
		)
	}
	matches := make([]CreatorResolution, 0, 1)
	for _, row := range rows {
		matchField := ""
		if row.VisibleID == douyinID {
			matchField = "aweme_show_id"
		} else if isDecimalID(douyinID) && row.AwemeID == douyinID {
			matchField = "aweme_id"
		}
		if matchField == "" {
			continue
		}
		resolved := CreatorResolution{
			AuthorizedCreator: row, MatchField: matchField, RequestedDouyinID: douyinID,
			Endpoint: AuthorizedCreatorsEndpoint, PageCount: pages,
			RequestIDs: append([]string(nil), requestIDs...),
		}
		if creatorName != "" {
			name := creatorName
			matchesName := row.Name == creatorName
			resolved.RequestedCreatorName = &name
			resolved.CreatorNameMatches = &matchesName
		}
		matches = append(matches, resolved)
	}
	if len(matches) == 0 {
		candidates := rows
		if len(candidates) > 10 {
			candidates = candidates[:10]
		}
		return CreatorResolution{}, domain.NewError(
			"configuration_error", "No exact authorized Qianchuan creator matched douyin_id", 2,
			map[string]any{"douyin_id": douyinID, "advertiser_id": scope.AdvertiserID,
				"candidate_count": len(rows), "candidates": candidates, "truncated": false},
		)
	}
	if len(matches) != 1 {
		return CreatorResolution{}, domain.NewError(
			"configuration_error", "douyin_id matched multiple authorized Qianchuan creators", 2,
			map[string]any{"douyin_id": douyinID, "matches": matches},
		)
	}
	return matches[0], nil
}

func (service Service) collectCreatorVideos(
	ctx context.Context,
	scope CredentialScope,
	accessToken string,
	awemeID string,
	productID string,
	pageSize int,
	maxPages int,
) ([]domainqianchuan.CreatorVideo, int, []string, error) {
	rows := make([]domainqianchuan.CreatorVideo, 0)
	seenRows := map[string]struct{}{}
	seenCursors := map[string]struct{}{"": {}}
	requestIDs := []string{}
	cursor := ""
	for page := 1; page <= maxPages; page++ {
		var parsedCursor *int64
		if cursor != "" {
			value, err := strconv.ParseInt(cursor, 10, 64)
			if err != nil || value < 0 {
				return nil, page - 1, requestIDs, errors.New("invalid Qianchuan creator video cursor")
			}
			parsedCursor = &value
		}
		result, err := service.Reader.FetchCreatorVideos(ctx, portqianchuan.CreatorVideoPageRequest{
			AdvertiserID: scope.AdvertiserID, AccessToken: accessToken, AwemeID: awemeID,
			ProductID: productID, Cursor: parsedCursor, Count: pageSize,
		})
		if err != nil {
			return nil, page - 1, requestIDs, fmt.Errorf("fetch cursor page %d: %w", page, err)
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		for _, row := range result.Rows {
			key := creatorVideoKey(row)
			if key == "" {
				return nil, page, requestIDs, fmt.Errorf("cursor page %d returned an empty material identity", page)
			}
			if _, exists := seenRows[key]; exists {
				continue
			}
			seenRows[key] = struct{}{}
			rows = append(rows, row)
		}
		if !result.HasMore {
			return rows, page, requestIDs, nil
		}
		if result.NextCursor == nil || *result.NextCursor < 0 {
			return nil, page, requestIDs, fmt.Errorf("cursor pagination returned an invalid cursor on page %d", page)
		}
		nextCursor := strconv.FormatInt(*result.NextCursor, 10)
		if nextCursor == cursor {
			return nil, page, requestIDs, fmt.Errorf("cursor pagination stalled on page %d", page)
		}
		if _, exists := seenCursors[nextCursor]; exists {
			return nil, page, requestIDs, fmt.Errorf("cursor pagination repeated a prior cursor on page %d", page)
		}
		seenCursors[nextCursor] = struct{}{}
		cursor = nextCursor
	}
	return nil, maxPages, requestIDs, fmt.Errorf("cursor pagination exceeds the safety cap of %d pages", maxPages)
}

func (service Service) validate() error {
	if service.Tokens == nil || service.Reader == nil {
		return errors.New("Qianchuan read service dependencies are incomplete")
	}
	return nil
}

func (service Service) token(ctx context.Context, scope CredentialScope) (authapplication.TokenLease, error) {
	if strings.TrimSpace(scope.AdvertiserID) == "" {
		return authapplication.TokenLease{}, errors.New("advertiser_id is required")
	}
	return service.Tokens.Ensure(ctx, authapplication.TokenQuery{Channel: "qianchuan",
		AdvertiserID: strings.TrimSpace(scope.AdvertiserID), AuthAccountID: strings.TrimSpace(scope.AuthAccountID)})
}

func normalizeProductQuery(query ProductQuery) ProductQuery {
	query.CredentialScope = normalizeCredentialScope(query.CredentialScope)
	for index := range query.ProductIDs {
		query.ProductIDs[index] = strings.TrimSpace(query.ProductIDs[index])
	}
	query.ProductName = strings.TrimSpace(query.ProductName)
	query.AwemeID = strings.TrimSpace(query.AwemeID)
	query.Tab = defaultString(strings.TrimSpace(query.Tab), "ALL")
	query.OrderField = defaultString(strings.TrimSpace(query.OrderField), "AUDIT_TIME")
	query.OrderType = defaultString(strings.TrimSpace(query.OrderType), "DESC")
	query.PageSize, query.MaxPages = normalizePagination(query.PageSize, query.MaxPages)
	return query
}

func (service Service) normalizePlanListQuery(query PlanListQuery) PlanListQuery {
	query.CredentialScope = normalizeCredentialScope(query.CredentialScope)
	query.StartDate = strings.TrimSpace(query.StartDate)
	query.EndDate = strings.TrimSpace(query.EndDate)
	query.PageSize, query.MaxPages = normalizePagination(query.PageSize, query.MaxPages)
	if query.StartDate == "" && query.EndDate == "" {
		now := time.Now()
		if service.Now != nil {
			now = service.Now()
		}
		today := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
		query.StartDate, query.EndDate = today, today
	}
	query.Status = defaultString(strings.TrimSpace(query.Status), "ALL")
	return query
}

func normalizePagination(pageSize, maxPages int) (int, int) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if maxPages == 0 {
		maxPages = DefaultMaxPages
	}
	return pageSize, maxPages
}

func normalizeCredentialScope(scope CredentialScope) CredentialScope {
	scope.AdvertiserID = strings.TrimSpace(scope.AdvertiserID)
	scope.AuthAccountID = strings.TrimSpace(scope.AuthAccountID)
	return scope
}

func validateProductQuery(query ProductQuery) error {
	if err := validatePositiveID(query.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	for index, productID := range query.ProductIDs {
		if err := validatePositiveID(productID, fmt.Sprintf("product_id[%d]", index)); err != nil {
			return err
		}
	}
	if query.AwemeID != "" {
		if err := validatePositiveID(query.AwemeID, "aweme_id"); err != nil {
			return err
		}
	}
	if !productTabs[query.Tab] {
		return errors.New("tab is not a supported Qianchuan product tab")
	}
	if !productOrderFields[query.OrderField] {
		return errors.New("order_field is not supported for Qianchuan products")
	}
	if !orderTypes[query.OrderType] {
		return errors.New("order_type must be ASC or DESC")
	}
	if query.Platform != "" && !productPlatforms[query.Platform] {
		return errors.New("platform must be ECP_AWEME or QIANCHUAN")
	}
	return validatePagination(query.PageSize, query.MaxPages, nil, MaxProductPageSize)
}

func validatePlanListQuery(query PlanListQuery) error {
	if err := validatePositiveID(query.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	if err := validateDatePeriod(query.StartDate, query.EndDate); err != nil {
		return err
	}
	if !planStatuses[query.Status] {
		return errors.New("status is not supported for Qianchuan plans")
	}
	if query.Top < 0 {
		return errors.New("top must be zero or a positive integer")
	}
	return validatePagination(query.PageSize, query.MaxPages, planPageSizes, 200)
}

func validateAuthorizedCreatorQuery(query AuthorizedCreatorQuery) error {
	if err := validatePositiveID(query.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	return validatePagination(query.PageSize, query.MaxPages, nil, MaxCreatorPageSize)
}

func validateProductBinding(binding domaintemplates.QianchuanProductBinding) error {
	if !binding.Active {
		return domain.NewError(
			"configuration_error", "Qianchuan product template is inactive", 2,
			map[string]any{"template_id": binding.TemplateID},
		)
	}
	if strings.TrimSpace(binding.TemplateID) == "" || strings.TrimSpace(binding.DisplayName) == "" {
		return errors.New("Qianchuan product template identity is incomplete")
	}
	if err := validatePositiveID(binding.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	if len(binding.ProductIDs) == 0 || len(binding.ProductIDs) > 30 {
		return errors.New("Qianchuan product template must bind between 1 and 30 products")
	}
	seen := map[string]struct{}{}
	for index, productID := range binding.ProductIDs {
		if err := validatePositiveID(productID, fmt.Sprintf("product_id[%d]", index)); err != nil {
			return err
		}
		if _, exists := seen[productID]; exists {
			return errors.New("Qianchuan product template contains duplicate product IDs")
		}
		seen[productID] = struct{}{}
	}
	return nil
}

func validateDatePeriod(startDate, endDate string) error {
	if startDate == "" || endDate == "" {
		return errors.New("start_date and end_date must both be provided")
	}
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil || start.Format("2006-01-02") != startDate {
		return errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil || end.Format("2006-01-02") != endDate {
		return errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return errors.New("start_date cannot be after end_date")
	}
	return nil
}

func validatePagination(pageSize, maxPages int, allowed map[int]bool, maximumPageSize int) error {
	if pageSize < 1 || pageSize > maximumPageSize {
		return fmt.Errorf("page_size must be between 1 and %d", maximumPageSize)
	}
	if allowed != nil && !allowed[pageSize] {
		return errors.New("page_size is not supported by the Qianchuan endpoint")
	}
	if maxPages < 1 || maxPages > MaxAllowedPages {
		return fmt.Errorf("max_pages must be between 1 and %d", MaxAllowedPages)
	}
	return nil
}

func validatePositiveID(value, field string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive decimal integer within int64 range", field)
	}
	return nil
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func intSet(values ...int) map[int]bool {
	result := make(map[int]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func materialKey(row domainqianchuan.PlanMaterial) string {
	for _, value := range []string{row.MaterialID, row.AwemeItemID, row.VideoID} {
		if value != "" {
			return value
		}
	}
	return ""
}

func creatorVideoKey(row domainqianchuan.CreatorVideo) string {
	for _, value := range []string{row.AwemeItemID, row.VideoID, row.MaterialID, row.URL} {
		if value != "" {
			return value
		}
	}
	return ""
}

func compactPlans(plans []domainqianchuan.Plan) []CompactPlan {
	result := make([]CompactPlan, 0, len(plans))
	for _, plan := range plans {
		creatorIDs := make([]string, 0, len(plan.Creators))
		for _, creator := range plan.Creators {
			identifier := creator.VisibleID
			if identifier == "" {
				identifier = creator.AwemeID
			}
			if identifier != "" {
				creatorIDs = append(creatorIDs, identifier)
			}
		}
		result = append(result, CompactPlan{
			AdID: plan.AdID, Name: plan.Name, Status: plan.Status, OptStatus: plan.OptStatus,
			CreateTime: plan.CreateTime, MarketingGoal: plan.MarketingGoal, CreatorIDs: creatorIDs,
			Budget: plan.Budget, SmartBidType: plan.SmartBidType, ROI2Goal: plan.ROI2Goal,
		})
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func isDecimalID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
