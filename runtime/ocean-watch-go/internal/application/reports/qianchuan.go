package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/pagination"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

const (
	QianchuanMaterialEndpoint   = "/v1.0/qianchuan/report/material/get/"
	QianchuanPlanSchemaEndpoint = "/v1.0/qianchuan/report/uni_promotion/config/get/"
	QianchuanPlanDataEndpoint   = "/v1.0/qianchuan/report/uni_promotion/data/get/"
	QianchuanPlanListEndpoint   = "/v1.0/qianchuan/uni_promotion/list/"
	PlanReportTopic             = "SITE_PROMOTION_PRODUCT_AD"
	DefaultPageSize             = 100
	DefaultMaxPages             = 100
	MaxMaterialPageSize         = 100
	MaxPlanPageSize             = 200
	MaxAllowedPages             = 500
)

var DefaultMaterialFields = []string{
	"stat_cost", "show_cnt", "click_cnt", "ctr", "convert_cnt", "convert_rate",
	"cpa_platform", "pay_order_amount", "pay_order_count", "prepay_and_pay_order_roi",
	"total_play", "play_duration_3s_rate", "play_over_rate",
}

var DefaultPlanFields = []string{
	"stat_cost",
	"total_pay_order_count_for_roi2",
	"total_pay_order_gmv_include_coupon_for_roi2",
	"total_prepay_and_pay_order_roi2",
	"total_order_settle_amount_for_roi2_1h",
	"total_order_settle_count_for_roi2_1h",
	"total_prepay_and_pay_settle_roi2_1h",
}

var PlanPresentationColumns = []domain.PresentationColumn{
	{Field: "rank", Label: "排名"},
	{Field: "name", Label: "计划"},
	{Field: "creator_names", Label: "达人"},
	{Field: "product_names", Label: "商品"},
	{Field: "status_label", Label: "状态"},
	{Field: "cost_guarantee_status_label", Label: "成本保障"},
	{Field: "bid_type_label", Label: "出价方式"},
	{Field: "roi_goal", Label: "目标 ROI"},
	{Field: "budget", Label: "日预算"},
	{Field: "stat_cost", Label: "消耗"},
	{Field: "total_pay_order_count_for_roi2", Label: "订单"},
	{Field: "total_pay_order_gmv_include_coupon_for_roi2", Label: "GMV"},
	{Field: "roi", Label: "实际 ROI"},
	{Field: "total_order_settle_amount_for_roi2_1h", Label: "1h 结算金额"},
	{Field: "total_prepay_and_pay_settle_roi2_1h", Label: "1h 结算 ROI"},
}

var PlanPresentationDetails = []domain.PresentationColumn{
	{Field: "budget_mode_label", Label: "预算方式"},
	{Field: "cost_guarantee_result", Label: "成本保障结果"},
	{Field: "cost_guarantee_reason", Label: "成本保障原因"},
}

var statusLabels = map[string]string{
	"DELIVERY_OK": "投放中", "DISABLE": "已暂停", "SYSTEM_DISABLE": "系统暂停",
	"AUDIT": "审核中", "OFFLINE_AUDIT": "审核不通过", "OFFLINE_BALANCE": "账户余额不足",
	"OFFLINE_BUDGET": "超出预算", "TIME_NO_REACH": "未到投放时间",
	"TIME_DONE": "已完成", "DELETED": "已删除",
}

var guaranteeLabels = map[string]string{
	"IN_EFFECT": "生效中", "INVALID": "未生效", "CONFIRMING": "确认中",
	"PAID": "已赔付", "ENDED": "已结束", "DEFAULT": "默认状态",
}

var bidTypeLabels = map[string]string{
	"SMART_BID_CUSTOM": "控成本投放", "SMART_BID_CONSERVATIVE": "放量投放",
}

var budgetModeLabels = map[string]string{
	"BUDGET_MODE_DAY": "日预算", "BUDGET_MODE_TOTAL": "总预算",
	"BUDGET_MODE_INFINITE": "不限预算",
}

type TokenProvider interface {
	Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error)
}

type Service struct {
	Tokens        TokenProvider
	Reader        portreports.QianchuanReader
	UnifiedReader portreports.QianchuanUnifiedReader
	Now           func() time.Time
}

type CredentialScope struct {
	AdvertiserID  string
	AuthAccountID string
}

type DateRange struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type MaterialFilters struct {
	MaterialIDs  []string `json:"material_id,omitempty"`
	MaterialType string   `json:"material_type,omitempty"`
	MaterialMode []string `json:"material_mode,omitempty"`
	VideoSource  []string `json:"video_source,omitempty"`
}

type MaterialQuery struct {
	CredentialScope
	StartDate  string
	EndDate    string
	Fields     []string
	Filters    MaterialFilters
	OrderField string
	OrderType  string
	PageSize   int
	MaxPages   int
	Top        int
}

type MaterialSummary struct {
	TotalSpend          *domain.Decimal `json:"total_spend"`
	TotalPayOrderAmount *domain.Decimal `json:"total_pay_order_amount"`
	TotalPayOrderCount  *int64          `json:"total_pay_order_count"`
	WeightedROI         *domain.Decimal `json:"weighted_roi"`
	MaterialsWithSpend  *int            `json:"materials_with_spend"`
}

type MaterialResult struct {
	Mode           string           `json:"mode"`
	Endpoint       string           `json:"endpoint"`
	AdvertiserID   string           `json:"advertiser_id"`
	DateRange      DateRange        `json:"date_range"`
	Fields         []string         `json:"fields"`
	Filters        MaterialFilters  `json:"filters"`
	Summary        MaterialSummary  `json:"summary"`
	RowCount       int              `json:"row_count"`
	DisplayedCount int              `json:"displayed_count"`
	Rows           []map[string]any `json:"rows"`
	PageCount      int              `json:"page_count"`
	RequestIDs     []string         `json:"request_ids"`
	Truncated      bool             `json:"truncated"`
}

type PlanQuery struct {
	CredentialScope
	StartDate string
	EndDate   string
	Top       int
	Status    string
	MaxPages  int
}

type PlanScope struct {
	MarketingGoal string `json:"marketing_goal"`
	AdlabScene    string `json:"adlab_scene"`
	Status        string `json:"status"`
}

type PlanSummary struct {
	PlanCount            int            `json:"plan_count"`
	PlansWithCost        int            `json:"plans_with_cost"`
	MetadataMissingCount int            `json:"metadata_missing_count"`
	TotalCost            domain.Decimal `json:"total_cost"`
	TotalPayOrderCount   int64          `json:"total_pay_order_count"`
	TotalPayOrderGMV     domain.Decimal `json:"total_pay_order_gmv"`
	TotalPayROI          domain.Decimal `json:"total_pay_roi"`
	TotalSettledAmount1H domain.Decimal `json:"total_settled_amount_1h"`
	TotalSettledROI1H    domain.Decimal `json:"total_settled_roi_1h"`
}

type PlanPageCount struct {
	PlanMetadata int `json:"plan_metadata"`
	ReportData   int `json:"report_data"`
}

type PlanResult struct {
	OK             bool                `json:"ok"`
	Channel        string              `json:"channel"`
	Transport      string              `json:"transport"`
	AdvertiserID   string              `json:"advertiser_id"`
	DateRange      DateRange           `json:"date_range"`
	Scope          PlanScope           `json:"scope"`
	Presentation   domain.Presentation `json:"presentation"`
	Summary        PlanSummary         `json:"summary"`
	Rows           []map[string]any    `json:"rows"`
	DisplayedCount int                 `json:"displayed_count"`
	TotalRowCount  int                 `json:"total_row_count"`
	Truncated      bool                `json:"truncated"`
	PageCount      PlanPageCount       `json:"page_count"`
	RequestIDs     []string            `json:"request_ids"`
	AmountUnit     string              `json:"amount_unit"`
}

func (service Service) MaterialReport(ctx context.Context, query MaterialQuery) (MaterialResult, error) {
	query, err := service.normalizeMaterialQuery(query)
	if err != nil {
		return MaterialResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return MaterialResult{}, err
	}
	ctx, err = authapplication.WithAdvertiserTokenLease(ctx, lease, query.AdvertiserID)
	if err != nil {
		return MaterialResult{}, err
	}
	requestIDs := []string{}
	pageCount := 0
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainreports.MaterialRow]{
		MaxPages: query.MaxPages,
		Key:      func(row domainreports.MaterialRow) string { return row.MaterialID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainreports.MaterialRow], error) {
			result, fetchErr := service.Reader.FetchMaterialPage(ctx, portreports.MaterialPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				StartDate: query.StartDate, EndDate: query.EndDate, Fields: query.Fields,
				Filters: portreports.MaterialFilters{
					MaterialIDs: query.Filters.MaterialIDs, MaterialType: query.Filters.MaterialType,
					MaterialMode: query.Filters.MaterialMode, VideoSource: query.Filters.VideoSource,
				},
				OrderField: query.OrderField, OrderType: query.OrderType,
				Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainreports.MaterialRow]{}, fetchErr
			}
			pageCount++
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainreports.MaterialRow]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return MaterialResult{}, err
	}
	summary, err := summarizeMaterials(rows, query.Fields)
	if err != nil {
		return MaterialResult{}, err
	}
	displayed := rows
	if query.Top != 0 && len(displayed) > query.Top {
		displayed = displayed[:query.Top]
	}
	outputRows := make([]map[string]any, len(displayed))
	for index, row := range displayed {
		outputRows[index] = cloneMap(row.Values)
	}
	return MaterialResult{
		Mode: "qianchuan_material_report", Endpoint: QianchuanMaterialEndpoint,
		AdvertiserID: query.AdvertiserID, DateRange: DateRange{query.StartDate, query.EndDate},
		Fields: append([]string(nil), query.Fields...), Filters: query.Filters, Summary: summary,
		RowCount: len(rows), DisplayedCount: len(outputRows), Rows: outputRows,
		PageCount: pageCount, RequestIDs: requestIDs, Truncated: false,
	}, nil
}

func (service Service) PlanReport(ctx context.Context, query PlanQuery) (PlanResult, error) {
	query, err := service.normalizePlanQuery(query)
	if err != nil {
		return PlanResult{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return PlanResult{}, err
	}
	ctx, err = authapplication.WithAdvertiserTokenLease(ctx, lease, query.AdvertiserID)
	if err != nil {
		return PlanResult{}, err
	}
	schema, err := service.Reader.FetchPlanSchema(ctx, portreports.PlanSchemaRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken, Topic: PlanReportTopic,
	})
	if err != nil {
		return PlanResult{}, err
	}
	if err := validatePlanSchema(schema); err != nil {
		return PlanResult{}, err
	}
	requestIDs := []string{}
	if schema.RequestID != "" {
		requestIDs = append(requestIDs, schema.RequestID)
	}
	startTime, endTime := reportTimes(query.StartDate, query.EndDate)
	metricPageCount := 0
	metricRows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainreports.PlanMetricRow]{
		MaxPages: query.MaxPages,
		Key:      func(row domainreports.PlanMetricRow) string { return row.AdID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainreports.PlanMetricRow], error) {
			result, fetchErr := service.Reader.FetchPlanMetricPage(ctx, portreports.PlanMetricPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				Topic: PlanReportTopic, Dimensions: []string{"ad_id"}, Metrics: DefaultPlanFields,
				StartTime: startTime, EndTime: endTime, OrderField: "stat_cost", OrderType: 2,
				Page: page, PageSize: DefaultPageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainreports.PlanMetricRow]{}, fetchErr
			}
			metricPageCount++
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainreports.PlanMetricRow]{
				Number: result.PageInfo.Page, TotalPages: result.PageInfo.TotalPages,
				TotalNumber: result.PageInfo.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return PlanResult{}, err
	}
	metadataPageCount := 0
	metadataRows := []domainqianchuan.Plan{}
	if len(metricRows) != 0 {
		metadataRows, err = pagination.CollectPages(ctx, pagination.PageOptions[domainqianchuan.Plan]{
			MaxPages: query.MaxPages,
			Key:      func(row domainqianchuan.Plan) string { return row.AdID },
			Fetch: func(ctx context.Context, page int) (pagination.Page[domainqianchuan.Plan], error) {
				result, fetchErr := service.Reader.FetchPlanMetadataPage(ctx, portreports.PlanMetadataPageRequest{
					AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
					StartTime: startTime, EndTime: endTime, Status: "ALL",
					MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT",
					NeedCompensateInfo: true, Page: page, PageSize: DefaultPageSize,
				})
				if fetchErr != nil {
					return pagination.Page[domainqianchuan.Plan]{}, fetchErr
				}
				metadataPageCount++
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
			return PlanResult{}, err
		}
	}
	metadata := make(map[string]domainqianchuan.Plan, len(metadataRows))
	for _, row := range metadataRows {
		metadata[row.AdID] = row
	}
	selected := make([]planPair, 0, len(metricRows))
	for _, metric := range metricRows {
		plan, found := metadata[metric.AdID]
		if !found && query.Status != "ALL" {
			return PlanResult{}, fmt.Errorf("Qianchuan report plan metadata could not be resolved for %s", metric.AdID)
		}
		if query.Status != "ALL" && plan.Status != query.Status {
			continue
		}
		selected = append(selected, planPair{Metric: metric, Metadata: plan, HasMetadata: found})
	}
	sort.SliceStable(selected, func(left, right int) bool {
		return selected[left].Metric.Metrics["stat_cost"].Compare(selected[right].Metric.Metrics["stat_cost"]) > 0
	})
	allRows := make([]map[string]any, len(selected))
	for index, pair := range selected {
		row, normalizeErr := normalizePlanRow(pair)
		if normalizeErr != nil {
			return PlanResult{}, normalizeErr
		}
		row["rank"] = index + 1
		allRows[index] = row
	}
	summary, err := summarizePlans(selected)
	if err != nil {
		return PlanResult{}, err
	}
	displayed := allRows
	if query.Top != 0 && len(displayed) > query.Top {
		displayed = displayed[:query.Top]
	}
	presentation := newPlanPresentation(displayed)
	return PlanResult{
		OK: true, Channel: "qianchuan", Transport: "official_sdk_rest",
		AdvertiserID: query.AdvertiserID, DateRange: DateRange{query.StartDate, query.EndDate},
		Scope:        PlanScope{MarketingGoal: "VIDEO_PROM_GOODS", AdlabScene: "UNI_PROJECT", Status: query.Status},
		Presentation: presentation, Summary: summary, Rows: displayed,
		DisplayedCount: len(displayed), TotalRowCount: len(allRows), Truncated: false,
		PageCount:  PlanPageCount{PlanMetadata: metadataPageCount, ReportData: metricPageCount},
		RequestIDs: requestIDs, AmountUnit: "CNY",
	}, nil
}

type planPair struct {
	Metric      domainreports.PlanMetricRow
	Metadata    domainqianchuan.Plan
	HasMetadata bool
}

func (service Service) normalizeMaterialQuery(query MaterialQuery) (MaterialQuery, error) {
	if err := validateScope(query.CredentialScope); err != nil {
		return MaterialQuery{}, err
	}
	start, end, err := service.dateRange(query.StartDate, query.EndDate)
	if err != nil {
		return MaterialQuery{}, err
	}
	query.StartDate, query.EndDate = start, end
	if len(query.Fields) == 0 {
		query.Fields = append([]string(nil), DefaultMaterialFields...)
	} else {
		query.Fields = uniqueNonEmpty(query.Fields)
	}
	query.OrderField = defaultString(query.OrderField, "stat_cost")
	query.OrderType = defaultString(query.OrderType, "DESC")
	if query.OrderType != "ASC" && query.OrderType != "DESC" {
		return MaterialQuery{}, errors.New("order_type must be ASC or DESC")
	}
	if !contains(query.Fields, query.OrderField) {
		query.Fields = append(query.Fields, query.OrderField)
	}
	if query.PageSize == 0 {
		query.PageSize = DefaultPageSize
	}
	if query.PageSize < 1 || query.PageSize > MaxMaterialPageSize {
		return MaterialQuery{}, errors.New("page_size must be between 1 and 100")
	}
	if query.MaxPages == 0 {
		query.MaxPages = DefaultMaxPages
	}
	if query.MaxPages < 1 || query.MaxPages > MaxAllowedPages {
		return MaterialQuery{}, errors.New("max_pages must be between 1 and 500")
	}
	if query.Top < 0 {
		return MaterialQuery{}, errors.New("top must be zero or a positive integer")
	}
	if query.Filters.MaterialType != "" && query.Filters.MaterialType != "video" &&
		query.Filters.MaterialType != "image" && query.Filters.MaterialType != "carousel" {
		return MaterialQuery{}, errors.New("material_type is not supported")
	}
	for index, value := range query.Filters.MaterialIDs {
		if !positiveID(value) {
			return MaterialQuery{}, fmt.Errorf("material_id[%d] must be a positive integer", index)
		}
	}
	query.Filters.MaterialIDs = uniqueNonEmpty(query.Filters.MaterialIDs)
	query.Filters.MaterialMode = uniqueNonEmpty(query.Filters.MaterialMode)
	query.Filters.VideoSource = uniqueNonEmpty(query.Filters.VideoSource)
	return query, nil
}

func (service Service) normalizePlanQuery(query PlanQuery) (PlanQuery, error) {
	if err := validateScope(query.CredentialScope); err != nil {
		return PlanQuery{}, err
	}
	start, end, err := service.dateRange(query.StartDate, query.EndDate)
	if err != nil {
		return PlanQuery{}, err
	}
	query.StartDate, query.EndDate = start, end
	if query.Top < 0 {
		return PlanQuery{}, errors.New("top must be zero or a positive integer")
	}
	query.Status = defaultString(strings.TrimSpace(query.Status), "ALL")
	if query.MaxPages == 0 {
		query.MaxPages = MaxAllowedPages
	}
	if query.MaxPages < 1 || query.MaxPages > MaxAllowedPages {
		return PlanQuery{}, errors.New("max_pages must be between 1 and 500")
	}
	return query, nil
}

func (service Service) token(ctx context.Context, scope CredentialScope) (authapplication.TokenLease, error) {
	if service.Tokens == nil || service.Reader == nil {
		return authapplication.TokenLease{}, errors.New("Qianchuan report dependencies are incomplete")
	}
	return service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "qianchuan", AdvertiserID: scope.AdvertiserID, AuthAccountID: scope.AuthAccountID,
	})
}

func (service Service) dateRange(startDate, endDate string) (string, string, error) {
	today := time.Now()
	if service.Now != nil {
		today = service.Now()
	}
	todayText := today.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	startDate = defaultString(strings.TrimSpace(startDate), todayText)
	endDate = defaultString(strings.TrimSpace(endDate), todayText)
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return "", "", errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return "", "", errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return "", "", errors.New("start_date cannot be after end_date")
	}
	return startDate, endDate, nil
}

func validateScope(scope CredentialScope) error {
	if !positiveID(scope.AdvertiserID) {
		return errors.New("advertiser_id must be a positive integer")
	}
	return nil
}

func validatePlanSchema(schema domainreports.QianchuanSchema) error {
	if schema.Topic != PlanReportTopic || !contains(schema.Dimensions, "ad_id") {
		return errors.New("Qianchuan plan report schema does not support the required topic and dimension")
	}
	for _, field := range DefaultPlanFields {
		if !contains(schema.Metrics, field) {
			return fmt.Errorf("Qianchuan plan report schema omitted required metric %s", field)
		}
	}
	return nil
}

func summarizeMaterials(rows []domainreports.MaterialRow, selected []string) (MaterialSummary, error) {
	available := make(map[string]bool, len(selected))
	for _, field := range selected {
		available[field] = true
	}
	var spend, gmv, orders domain.Decimal
	materialsWithSpend := 0
	for _, row := range rows {
		if available["stat_cost"] {
			value, err := decimalValue(row.Values["stat_cost"], "stat_cost", true)
			if err != nil {
				return MaterialSummary{}, err
			}
			spend = spend.Add(value)
			if value.Sign() > 0 {
				materialsWithSpend++
			}
		}
		if available["pay_order_amount"] {
			value, err := decimalValue(row.Values["pay_order_amount"], "pay_order_amount", true)
			if err != nil {
				return MaterialSummary{}, err
			}
			gmv = gmv.Add(value)
		}
		if available["pay_order_count"] {
			value, err := decimalValue(row.Values["pay_order_count"], "pay_order_count", true)
			if err != nil {
				return MaterialSummary{}, err
			}
			orders = orders.Add(value)
		}
	}
	result := MaterialSummary{}
	if available["stat_cost"] {
		value := spend
		count := materialsWithSpend
		result.TotalSpend, result.MaterialsWithSpend = &value, &count
	}
	if available["pay_order_amount"] {
		value := gmv
		result.TotalPayOrderAmount = &value
	}
	if available["pay_order_count"] {
		value, err := orders.Int64Exact()
		if err != nil {
			return MaterialSummary{}, errors.New("Qianchuan material report returned a fractional pay_order_count")
		}
		result.TotalPayOrderCount = &value
	}
	if result.TotalSpend != nil && result.TotalPayOrderAmount != nil && spend.Sign() != 0 {
		value, err := gmv.Divide(spend)
		if err != nil {
			return MaterialSummary{}, err
		}
		value = value.Round(12)
		result.WeightedROI = &value
	}
	return result, nil
}

func normalizePlanRow(pair planPair) (map[string]any, error) {
	row := map[string]any{
		"ad_id": pair.Metric.AdID, "metadata_available": pair.HasMetadata,
		"metadata_missing_reason": nil, "name": nil, "status": nil, "status_label": nil,
		"opt_status": nil, "budget": nil, "budget_mode": nil, "budget_mode_label": nil,
		"roi_goal": nil, "bid": nil, "smart_bid_type": nil, "bid_type_label": nil,
		"cost_guarantee_status": nil, "cost_guarantee_status_label": nil,
		"cost_guarantee_result": nil, "cost_guarantee_reason": nil,
		"creator_ids": []string{}, "creator_names": []string{},
		"product_ids": []string{}, "product_names": []string{},
	}
	if !pair.HasMetadata {
		row["metadata_missing_reason"] = "plan_not_returned_by_metadata_api"
	} else {
		plan := pair.Metadata
		row["name"], row["status"], row["status_label"], row["opt_status"] =
			plan.Name, plan.Status, label(statusLabels, plan.Status), plan.OptStatus
		row["budget"], row["budget_mode"], row["budget_mode_label"] =
			plan.Budget, plan.BudgetMode, label(budgetModeLabels, plan.BudgetMode)
		row["roi_goal"], row["bid"], row["smart_bid_type"], row["bid_type_label"] =
			plan.ROI2Goal, plan.ROI2Goal, plan.SmartBidType, label(bidTypeLabels, plan.SmartBidType)
		creatorIDs, creatorNames := []string{}, []string{}
		for _, creator := range plan.Creators {
			if creator.VisibleID != "" {
				creatorIDs = append(creatorIDs, creator.VisibleID)
			}
			if creator.Name != "" {
				creatorNames = append(creatorNames, creator.Name)
			}
		}
		productIDs, productNames := []string{}, []string{}
		for _, product := range plan.Products {
			if product.ProductID != "" {
				productIDs = append(productIDs, product.ProductID)
			}
			if product.ProductName != "" {
				productNames = append(productNames, product.ProductName)
			}
		}
		row["creator_ids"], row["creator_names"] = creatorIDs, creatorNames
		row["product_ids"], row["product_names"] = productIDs, productNames
		if plan.Guarantee != nil {
			row["cost_guarantee_status"] = plan.Guarantee.CompensateStatus
			row["cost_guarantee_status_label"] = label(guaranteeLabels, plan.Guarantee.CompensateStatus)
			row["cost_guarantee_result"] = plan.Guarantee.Status
			row["cost_guarantee_reason"] = plan.Guarantee.Reason
		}
	}
	for _, field := range DefaultPlanFields {
		value, exists := pair.Metric.Metrics[field]
		if !exists {
			return nil, fmt.Errorf("Qianchuan report omitted required metric %s for %s", field, pair.Metric.AdID)
		}
		switch field {
		case "stat_cost", "total_pay_order_gmv_include_coupon_for_roi2", "total_order_settle_amount_for_roi2_1h":
			row[field] = value.Round(2)
		case "total_pay_order_count_for_roi2", "total_order_settle_count_for_roi2_1h":
			count, err := value.Int64Exact()
			if err != nil {
				return nil, fmt.Errorf("Qianchuan report returned a fractional count for %s", field)
			}
			row[field] = count
		default:
			row[field] = value.Round(4)
		}
	}
	row["roi"] = row["total_prepay_and_pay_order_roi2"]
	return row, nil
}

func summarizePlans(rows []planPair) (PlanSummary, error) {
	var cost, gmv, settled, orders domain.Decimal
	result := PlanSummary{PlanCount: len(rows)}
	for _, row := range rows {
		for _, field := range DefaultPlanFields {
			if _, exists := row.Metric.Metrics[field]; !exists {
				return PlanSummary{}, fmt.Errorf("Qianchuan report omitted required metric %s for %s", field, row.Metric.AdID)
			}
		}
		rowCost := row.Metric.Metrics["stat_cost"]
		cost = cost.Add(rowCost)
		gmv = gmv.Add(row.Metric.Metrics["total_pay_order_gmv_include_coupon_for_roi2"])
		settled = settled.Add(row.Metric.Metrics["total_order_settle_amount_for_roi2_1h"])
		orders = orders.Add(row.Metric.Metrics["total_pay_order_count_for_roi2"])
		if rowCost.Sign() > 0 {
			result.PlansWithCost++
		}
		if !row.HasMetadata {
			result.MetadataMissingCount++
		}
	}
	orderCount, err := orders.Int64Exact()
	if err != nil {
		return PlanSummary{}, errors.New("Qianchuan report returned a fractional order count")
	}
	result.TotalCost = cost.Round(2)
	result.TotalPayOrderCount = orderCount
	result.TotalPayOrderGMV = gmv.Round(2)
	result.TotalSettledAmount1H = settled.Round(2)
	if cost.Sign() != 0 {
		result.TotalPayROI, err = gmv.Divide(cost)
		if err != nil {
			return PlanSummary{}, err
		}
		result.TotalPayROI = result.TotalPayROI.Round(4)
		result.TotalSettledROI1H, err = settled.Divide(cost)
		if err != nil {
			return PlanSummary{}, err
		}
		result.TotalSettledROI1H = result.TotalSettledROI1H.Round(4)
	}
	return result, nil
}

func newPlanPresentation(rows []map[string]any) domain.Presentation {
	return domain.Presentation{
		Format: "markdown_table", Required: true, AllowColumnOmission: false,
		AllowColumnReordering: false, Columns: append([]domain.PresentationColumn(nil), PlanPresentationColumns...),
		Rows: rows, RequiredDetails: append([]domain.PresentationColumn(nil), PlanPresentationDetails...),
		RenderedMarkdown: renderPlanTable(rows),
	}
}

func renderPlanTable(rows []map[string]any) string {
	labels, separators := []string{}, []string{}
	for _, column := range PlanPresentationColumns {
		labels = append(labels, column.Label)
		if column.Field == "rank" {
			separators = append(separators, "---:")
		} else {
			separators = append(separators, "---")
		}
	}
	lines := []string{"| " + strings.Join(labels, " | ") + " |", "| " + strings.Join(separators, " | ") + " |"}
	for _, row := range rows {
		values := make([]string, 0, len(PlanPresentationColumns))
		for _, column := range PlanPresentationColumns {
			values = append(values, planPresentationValue(column.Field, row[column.Field]))
		}
		lines = append(lines, "| "+strings.Join(values, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func planPresentationValue(field string, value any) string {
	if value == nil {
		return "—"
	}
	if values, ok := value.([]string); ok {
		if len(values) == 0 {
			return "—"
		}
		return domain.EscapeMarkdownValue(strings.Join(values, "、"))
	}
	if decimal, ok := decimalPointerOrValue(value); ok {
		switch field {
		case "budget", "stat_cost", "total_pay_order_gmv_include_coupon_for_roi2", "total_order_settle_amount_for_roi2_1h":
			return "¥" + commaMoney(decimal.StringFixed(2))
		default:
			return domain.EscapeMarkdownValue(decimal.String())
		}
	}
	text := fmt.Sprint(value)
	if text == "" {
		return "—"
	}
	return domain.EscapeMarkdownValue(text)
}

func decimalPointerOrValue(value any) (domain.Decimal, bool) {
	switch typed := value.(type) {
	case domain.Decimal:
		return typed, true
	case *domain.Decimal:
		if typed != nil {
			return *typed, true
		}
	}
	return domain.Decimal{}, false
}

func commaMoney(value string) string {
	parts := strings.SplitN(value, ".", 2)
	integer := parts[0]
	sign := ""
	if strings.HasPrefix(integer, "-") {
		sign, integer = "-", integer[1:]
	}
	for index := len(integer) - 3; index > 0; index -= 3 {
		integer = integer[:index] + "," + integer[index:]
	}
	if len(parts) == 2 {
		return sign + integer + "." + parts[1]
	}
	return sign + integer
}

func decimalValue(value any, field string, missingAsZero bool) (domain.Decimal, error) {
	if value == nil || fmt.Sprint(value) == "" {
		if missingAsZero {
			return domain.Decimal{}, nil
		}
		return domain.Decimal{}, fmt.Errorf("Qianchuan report omitted required metric %s", field)
	}
	parsed, err := domain.ParseDecimal(fmt.Sprint(value))
	if err != nil {
		return domain.Decimal{}, fmt.Errorf("Qianchuan report returned a non-numeric metric %s", field)
	}
	return parsed, nil
}

func reportTimes(startDate, endDate string) (string, string) {
	return startDate + " 00:00:00", endDate + " 23:59:59"
}

func label(values map[string]string, value string) any {
	if value == "" {
		return nil
	}
	if translated := values[value]; translated != "" {
		return translated
	}
	return value
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func uniqueNonEmpty(values []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func positiveID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed > 0
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
