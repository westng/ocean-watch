package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/reports"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/reports"
)

const (
	MarketingSchemaEndpoint    = "/v3.0/report/custom/config/get/"
	MarketingReportEndpoint    = "/v3.0/report/custom/get/"
	MarketingPromotionEndpoint = "/v3.0/promotion/list/"
	MarketingMaterialTopic     = "MATERIAL_DATA"
	MarketingPlanTopic         = "UNI_PROJECT_DATA"
	MarketingPageSize          = 100
	MarketingMaxPages          = 500
)

var MarketingPromotionFields = []string{
	"promotion_id", "project_id", "advertiser_id", "promotion_name", "status",
	"status_first", "status_second", "opt_status", "source", "promotion_materials",
	"promotion_create_time", "promotion_modify_time",
}

var DefaultMarketingDimensions = []string{"material_id", "cdp_promotion_id", "cdp_promotion_name"}

var DefaultMarketingMetrics = []string{
	"stat_cost", "show_cnt", "click_cnt", "ctr", "cpc_platform", "cpm_platform",
	"convert_cnt", "conversion_cost", "conversion_rate", "total_play",
	"play_duration_3s", "play_over_rate", "in_app_order", "in_app_order_gmv",
	"in_app_order_roi",
}

var MarketingPlanIDDimensions = []string{"project_id", "cdp_project_id"}

var MarketingPlanNameDimensions = []string{"project_name", "cdp_project_name"}

var DefaultMarketingPlanMetrics = []string{
	"stat_cost", "show_cnt", "click_cnt", "ctr", "convert_cnt", "conversion_cost",
	"conversion_rate", "in_app_order_count", "in_app_order_gmv", "in_app_order_roi",
}

var MarketingPlanPresentationColumns = []domain.PresentationColumn{
	{Field: "rank", Label: "排名"},
	{Field: "project_id", Label: "项目ID"},
	{Field: "project_name", Label: "项目名称"},
	{Field: "stat_cost", Label: "消耗"},
	{Field: "show_cnt", Label: "展现"},
	{Field: "click_cnt", Label: "点击"},
	{Field: "ctr", Label: "点击率"},
	{Field: "convert_cnt", Label: "转化"},
	{Field: "conversion_cost", Label: "转化成本"},
	{Field: "conversion_rate", Label: "转化率"},
	{Field: "in_app_order_count", Label: "订单"},
	{Field: "in_app_order_gmv", Label: "GMV"},
	{Field: "in_app_order_roi", Label: "ROI"},
}

var validMarketingTopics = map[string]struct{}{
	"BASIC_DATA": {}, "BIDWORD_DATA": {}, "CREATIVE_DATA": {}, "DMP_DATA": {},
	"DPA_VIDEO_DATA": {}, "MATERIAL_BOOST_DATA": {}, "MATERIAL_DATA": {},
	"ONE_KEY_BOOST_DATA": {}, "PRODUCT_DATA": {}, "QUERY_DATA": {},
	"STD_BASIC_DATA": {}, "STD_BIDWORD_DATA": {}, "STD_DMP_DATA": {},
	"STD_MATERIAL_DATA": {}, "STD_PRODUCT_DATA": {}, "STD_QUERY_DATA": {},
	"UNI_PROJECT_DATA": {}, "UNI_PROJECT_MATERIAL_DATA": {}, "VIDEO_DUARATION_DATA": {},
}

type MarketingService struct {
	Tokens TokenProvider
	Reader portreports.MarketingReader
	Now    func() time.Time
}

type MarketingSchemaQuery struct {
	CredentialScope
	DataTopics []string
	Full       bool
}

type MarketingSchemaDimension struct {
	Field    string `json:"field"`
	Name     string `json:"name"`
	SortAble *bool  `json:"sort_able"`
}

type MarketingSchemaMetric struct {
	Field string `json:"field"`
	Name  string `json:"name"`
}

type MarketingSchemaTopic struct {
	DataTopic      string                     `json:"data_topic"`
	DimensionCount int                        `json:"dimension_count"`
	MetricCount    int                        `json:"metric_count"`
	Dimensions     []MarketingSchemaDimension `json:"dimensions"`
	Metrics        []MarketingSchemaMetric    `json:"metrics"`
}

type MarketingSchemaResult struct {
	Endpoint        string         `json:"endpoint"`
	Params          map[string]any `json:"params"`
	ResponseCode    int64          `json:"response_code"`
	ResponseMessage string         `json:"response_message,omitempty"`
	RequestID       string         `json:"request_id,omitempty"`
	Topics          any            `json:"topics"`
	Response        map[string]any `json:"response,omitempty"`
}

type MarketingFilter struct {
	Field    string   `json:"field"`
	Type     int64    `json:"type"`
	Operator int64    `json:"operator"`
	Values   []string `json:"values"`
}

type MarketingCustomQuery struct {
	CredentialScope
	DataTopic  string
	Dimensions []string
	Metrics    []string
	Filters    []MarketingFilter
	StartTime  string
	EndTime    string
	OrderField string
	OrderType  string
	Page       int
	PageSize   int
}

type MarketingPageInfo struct {
	Page        int `json:"page"`
	PageSize    int `json:"page_size"`
	TotalNumber int `json:"total_number"`
	TotalPage   int `json:"total_page"`
}

type MarketingCustomResult struct {
	Endpoint        string                             `json:"endpoint"`
	Params          map[string]any                     `json:"params"`
	ResponseCode    int64                              `json:"response_code"`
	ResponseMessage string                             `json:"response_message,omitempty"`
	RequestID       string                             `json:"request_id,omitempty"`
	PageInfo        MarketingPageInfo                  `json:"page_info"`
	TotalMetrics    map[string]string                  `json:"total_metrics"`
	RowCount        int                                `json:"row_count"`
	Rows            []domainreports.MarketingReportRow `json:"rows"`
	FlatRows        []map[string]any                   `json:"flat_rows"`
}

type MarketingPlanQuery struct {
	CredentialScope
	StartDate string
	EndDate   string
	Metrics   []string
	PageSize  int
	MaxPages  int
	Top       int
}

type MarketingPlanContract struct {
	Dimensions              []string `json:"dimensions"`
	Metrics                 []string `json:"metrics"`
	AvailableDimensionCount int      `json:"available_dimension_count"`
	AvailableMetricCount    int      `json:"available_metric_count"`
	OmittedDefaultMetrics   []string `json:"omitted_default_metrics"`
}

type MarketingPlanSummary struct {
	TotalSpend     domain.Decimal  `json:"total_spend"`
	TotalGMV       *domain.Decimal `json:"total_gmv"`
	TotalOrders    *int64          `json:"total_orders"`
	WeightedROI    *domain.Decimal `json:"weighted_roi"`
	PlansWithSpend int             `json:"plans_with_spend"`
}

type MarketingPlanResult struct {
	Mode            string                `json:"mode"`
	ConfigEndpoint  string                `json:"config_endpoint"`
	ReportEndpoint  string                `json:"report_endpoint"`
	AdvertiserID    string                `json:"advertiser_id"`
	DateRange       DateRange             `json:"date_range"`
	Contract        MarketingPlanContract `json:"contract"`
	ConfigRequestID string                `json:"config_request_id,omitempty"`
	Presentation    domain.Presentation   `json:"presentation"`
	Summary         MarketingPlanSummary  `json:"summary"`
	RowCount        int                   `json:"row_count"`
	DisplayedCount  int                   `json:"displayed_count"`
	Rows            []map[string]any      `json:"rows"`
	PageCount       int                   `json:"page_count"`
	RequestIDs      []string              `json:"request_ids"`
	Truncated       bool                  `json:"truncated"`
}

type MarketingMaterialQuery struct {
	CredentialScope
	StartDate                   string
	EndDate                     string
	DataTopic                   string
	Dimensions                  []string
	Metrics                     []string
	IncludeExtraReportMaterials bool
	ProjectID                   string
	PromotionIDs                []string
	ActiveOnly                  bool
	PromotionPage               int
	PromotionPageSize           int
	ReportPage                  int
	ReportPageSize              int
	SinglePage                  bool
	OrderField                  string
	OrderType                   string
}

type MarketingMaterialResult struct {
	Mode                           string            `json:"mode"`
	StatusHandling                 string            `json:"status_handling"`
	DateRange                      DateRange         `json:"date_range"`
	PromotionEndpoint              string            `json:"promotion_endpoint"`
	MaterialReportEndpoint         string            `json:"material_report_endpoint"`
	PromotionRequestID             any               `json:"promotion_request_id"`
	PromotionRequestIDs            []string          `json:"promotion_request_ids"`
	MaterialReportRequestID        any               `json:"material_report_request_id"`
	MaterialReportRequestIDs       []string          `json:"material_report_request_ids"`
	PromotionResponseCode          int64             `json:"promotion_response_code"`
	PromotionResponseCodes         []int64           `json:"promotion_response_codes"`
	PromotionResponseMessage       any               `json:"promotion_response_message"`
	PromotionResponseMessages      []string          `json:"promotion_response_messages"`
	MaterialReportResponseCode     any               `json:"material_report_response_code"`
	MaterialReportResponseCodes    []int64           `json:"material_report_response_codes"`
	MaterialReportResponseMessage  any               `json:"material_report_response_message"`
	MaterialReportResponseMessages []string          `json:"material_report_response_messages"`
	PromotionCount                 int               `json:"promotion_count"`
	SelectedPromotionCount         int               `json:"selected_promotion_count"`
	ActiveLikePromotionCount       int               `json:"active_like_promotion_count"`
	MaterialCount                  int               `json:"material_count"`
	RowCount                       int               `json:"row_count"`
	PromotionPageInfo              MarketingPageInfo `json:"promotion_page_info"`
	ReportPageInfo                 any               `json:"report_page_info"`
	ReportTotalMetrics             any               `json:"report_total_metrics"`
	ReportScope                    string            `json:"report_scope"`
	PromotionParams                map[string]any    `json:"promotion_params"`
	MaterialReportParams           any               `json:"material_report_params"`
	Rows                           []map[string]any  `json:"rows"`
	ExcludedPromotions             []map[string]any  `json:"excluded_promotions"`
}

func ValidMarketingDataTopic(value string) bool {
	_, ok := validMarketingTopics[strings.TrimSpace(value)]
	return ok
}

func (service MarketingService) Schema(
	ctx context.Context,
	query MarketingSchemaQuery,
) (MarketingSchemaResult, error) {
	query.DataTopics = uniqueReportValues(query.DataTopics)
	if err := validateMarketingScope(query.CredentialScope); err != nil {
		return MarketingSchemaResult{}, err
	}
	if len(query.DataTopics) == 0 {
		query.DataTopics = []string{MarketingMaterialTopic}
	}
	if len(query.DataTopics) > 10 {
		return MarketingSchemaResult{}, errors.New("data_topics accepts at most 10 values")
	}
	for _, topic := range query.DataTopics {
		if !ValidMarketingDataTopic(topic) {
			return MarketingSchemaResult{}, fmt.Errorf("unsupported Marketing report data topic %q", topic)
		}
	}
	lease, err := service.marketingToken(ctx, query.CredentialScope)
	if err != nil {
		return MarketingSchemaResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return MarketingSchemaResult{}, err
	}
	schema, err := service.Reader.FetchSchema(ctx, portreports.MarketingSchemaRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		DataTopics: append([]string(nil), query.DataTopics...),
	})
	if err != nil {
		return MarketingSchemaResult{}, err
	}
	if err := validateSchemaTopics(schema.Topics, query.DataTopics); err != nil {
		return MarketingSchemaResult{}, err
	}
	var topics any
	if query.Full {
		topics = append([]domainreports.MarketingTopic(nil), schema.Topics...)
	} else {
		compact := make([]MarketingSchemaTopic, 0, len(schema.Topics))
		for _, topic := range schema.Topics {
			dimensions := make([]MarketingSchemaDimension, 0, len(topic.Dimensions))
			for _, field := range topic.Dimensions {
				dimensions = append(dimensions, MarketingSchemaDimension{
					Field: field.Field, Name: field.Name, SortAble: field.SortAble,
				})
			}
			metrics := make([]MarketingSchemaMetric, 0, len(topic.Metrics))
			for _, field := range topic.Metrics {
				metrics = append(metrics, MarketingSchemaMetric{Field: field.Field, Name: field.Name})
			}
			compact = append(compact, MarketingSchemaTopic{
				DataTopic: topic.DataTopic, DimensionCount: len(dimensions), MetricCount: len(metrics),
				Dimensions: dimensions, Metrics: metrics,
			})
		}
		topics = compact
	}
	result := MarketingSchemaResult{
		Endpoint: MarketingSchemaEndpoint,
		Params: map[string]any{
			"advertiser_id": query.AdvertiserID,
			"data_topics":   append([]string(nil), query.DataTopics...),
		},
		ResponseCode: 0, ResponseMessage: schema.Message, RequestID: schema.RequestID, Topics: topics,
	}
	if query.Full {
		result.Response = cloneAnyMap(schema.Response)
	}
	return result, nil
}

func (service MarketingService) Custom(
	ctx context.Context,
	query MarketingCustomQuery,
) (MarketingCustomResult, error) {
	query, err := service.normalizeCustomQuery(query)
	if err != nil {
		return MarketingCustomResult{}, err
	}
	lease, err := service.marketingToken(ctx, query.CredentialScope)
	if err != nil {
		return MarketingCustomResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return MarketingCustomResult{}, err
	}
	filters := make([]portreports.MarketingFilter, len(query.Filters))
	for index, filter := range query.Filters {
		filters[index] = portreports.MarketingFilter{
			Field: filter.Field, Type: filter.Type, Operator: filter.Operator,
			Values: append([]string(nil), filter.Values...),
		}
	}
	page, err := service.Reader.FetchReportPage(ctx, portreports.MarketingReportPageRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		DataTopic: query.DataTopic, Dimensions: query.Dimensions, Metrics: query.Metrics,
		Filters: filters, StartTime: query.StartTime, EndTime: query.EndTime,
		OrderField: query.OrderField, OrderType: query.OrderType,
		Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		return MarketingCustomResult{}, err
	}
	flatRows := make([]map[string]any, len(page.Rows))
	for index, row := range page.Rows {
		flatRows[index] = flattenMarketingRow(row)
	}
	params := marketingReportParams(query, filters)
	return MarketingCustomResult{
		Endpoint: MarketingReportEndpoint, Params: params, ResponseCode: 0,
		ResponseMessage: page.Message, RequestID: page.RequestID,
		PageInfo: MarketingPageInfo{Page: page.Page, PageSize: page.PageSize,
			TotalNumber: page.TotalNumber, TotalPage: page.TotalPages},
		TotalMetrics: cloneStringValues(page.TotalMetrics), RowCount: len(page.Rows),
		Rows: append([]domainreports.MarketingReportRow(nil), page.Rows...), FlatRows: flatRows,
	}, nil
}

func (service MarketingService) Plans(
	ctx context.Context,
	query MarketingPlanQuery,
) (MarketingPlanResult, error) {
	query, err := service.normalizePlanQuery(query)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	lease, err := service.marketingToken(ctx, query.CredentialScope)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	schema, err := service.Reader.FetchSchema(ctx, portreports.MarketingSchemaRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		DataTopics: []string{MarketingPlanTopic},
	})
	if err != nil {
		return MarketingPlanResult{}, err
	}
	if err := validateSchemaTopics(schema.Topics, []string{MarketingPlanTopic}); err != nil {
		return MarketingPlanResult{}, err
	}
	contract, err := selectMarketingPlanContract(schema.Topics[0], query.Metrics)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	startTime, endTime := query.StartDate+" 00:00:00", query.EndDate+" 23:59:59"
	requestIDs := []string{}
	pageCount := 0
	idDimension := contract.Dimensions[0]
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[domainreports.MarketingReportRow]{
		MaxPages: query.MaxPages,
		Key: func(row domainreports.MarketingReportRow) string {
			return strings.TrimSpace(row.Dimensions[idDimension])
		},
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainreports.MarketingReportRow], error) {
			result, fetchErr := service.Reader.FetchReportPage(ctx, portreports.MarketingReportPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				DataTopic: MarketingPlanTopic, Dimensions: contract.Dimensions,
				Metrics: contract.Metrics, Filters: []portreports.MarketingFilter{},
				StartTime: startTime, EndTime: endTime, OrderField: "stat_cost", OrderType: "DESC",
				Page: page, PageSize: query.PageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainreports.MarketingReportRow]{}, fetchErr
			}
			pageCount++
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			return pagination.Page[domainreports.MarketingReportRow]{
				Number: result.Page, TotalPages: result.TotalPages,
				TotalNumber: result.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return MarketingPlanResult{}, err
	}
	parsed, err := parseMarketingPlanRows(rows, contract)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	sort.SliceStable(parsed, func(left, right int) bool {
		return parsed[left].Spend.Compare(parsed[right].Spend) > 0
	})
	summary, err := summarizeMarketingPlans(parsed, contract.Metrics)
	if err != nil {
		return MarketingPlanResult{}, err
	}
	allRows := make([]map[string]any, len(parsed))
	presentationRows := make([]map[string]any, len(parsed))
	for index, row := range parsed {
		allRows[index] = cloneAnyMap(row.Flat)
		presentationRows[index] = row.presentation(index + 1)
	}
	if query.Top != 0 && len(allRows) > query.Top {
		allRows = allRows[:query.Top]
		presentationRows = presentationRows[:query.Top]
	}
	presentation := domain.Presentation{
		Format: "markdown_table", Required: true, AllowColumnOmission: false,
		AllowColumnReordering: false,
		Columns:               append([]domain.PresentationColumn(nil), MarketingPlanPresentationColumns...),
		Rows:                  presentationRows,
		RenderedMarkdown:      renderMarketingPlanTable(presentationRows),
	}
	return MarketingPlanResult{
		Mode: "marketing_plan_report", ConfigEndpoint: MarketingSchemaEndpoint,
		ReportEndpoint: MarketingReportEndpoint, AdvertiserID: query.AdvertiserID,
		DateRange: DateRange{StartDate: query.StartDate, EndDate: query.EndDate},
		Contract:  contract, ConfigRequestID: schema.RequestID, Presentation: presentation,
		Summary: summary, RowCount: len(parsed), DisplayedCount: len(allRows), Rows: allRows,
		PageCount: pageCount, RequestIDs: requestIDs, Truncated: false,
	}, nil
}

func (service MarketingService) Materials(
	ctx context.Context,
	query MarketingMaterialQuery,
) (MarketingMaterialResult, error) {
	query, err := service.normalizeMaterialQuery(query)
	if err != nil {
		return MarketingMaterialResult{}, err
	}
	lease, err := service.marketingToken(ctx, query.CredentialScope)
	if err != nil {
		return MarketingMaterialResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return MarketingMaterialResult{}, err
	}

	promotionRequestIDs := []string{}
	promotionResponseCodes := []int64{}
	promotionResponseMessages := []string{}
	var promotionFirstRequestID string
	var promotionLast domainreports.MarketingPromotionPage
	promotions, err := pagination.CollectPages(ctx, pagination.PageOptions[domainreports.MarketingPromotion]{
		MaxPages: MarketingMaxPages, StartPage: query.PromotionPage, SinglePage: query.SinglePage,
		Key: func(row domainreports.MarketingPromotion) string { return strings.TrimSpace(row.PromotionID) },
		Fetch: func(ctx context.Context, page int) (pagination.Page[domainreports.MarketingPromotion], error) {
			result, fetchErr := service.Reader.FetchPromotionPage(ctx, portreports.MarketingPromotionPageRequest{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
				ProjectID: query.ProjectID, PromotionIDs: append([]string(nil), query.PromotionIDs...),
				Page: page, PageSize: query.PromotionPageSize,
			})
			if fetchErr != nil {
				return pagination.Page[domainreports.MarketingPromotion]{}, fetchErr
			}
			promotionLast = result
			promotionResponseCodes = append(promotionResponseCodes, 0)
			if result.RequestID != "" {
				if promotionFirstRequestID == "" {
					promotionFirstRequestID = result.RequestID
				}
				promotionRequestIDs = append(promotionRequestIDs, result.RequestID)
			}
			if result.Message != "" {
				promotionResponseMessages = append(promotionResponseMessages, result.Message)
			}
			return pagination.Page[domainreports.MarketingPromotion]{
				Number: result.Page, TotalPages: result.TotalPages,
				TotalNumber: result.TotalNumber, Rows: result.Rows,
			}, nil
		},
	})
	if err != nil {
		return MarketingMaterialResult{}, err
	}

	activeCount := 0
	selected := make([]domainreports.MarketingPromotion, 0, len(promotions))
	excluded := []map[string]any{}
	for _, promotion := range promotions {
		active := marketingPromotionActive(promotion)
		if active {
			activeCount++
		}
		if query.ActiveOnly && !active {
			excluded = append(excluded, marketingExcludedPromotion(promotion))
			continue
		}
		selected = append(selected, promotion)
	}
	materialRows := extractMarketingMaterialRows(selected)

	reportRequestIDs := []string{}
	reportResponseCodes := []int64{}
	reportResponseMessages := []string{}
	var reportFirstRequestID string
	var reportLast domainreports.MarketingReportPage
	reportRows := []domainreports.MarketingReportRow{}
	var materialReportParams any
	if len(materialRows) != 0 {
		filters := marketingMaterialFilters(materialRows, query.IncludeExtraReportMaterials)
		customQuery := MarketingCustomQuery{
			CredentialScope: query.CredentialScope, DataTopic: query.DataTopic,
			Dimensions: query.Dimensions, Metrics: query.Metrics,
			StartTime: query.StartDate + " 00:00:00", EndTime: query.EndDate + " 23:59:59",
			OrderField: query.OrderField, OrderType: query.OrderType,
			Page: query.ReportPage, PageSize: query.ReportPageSize,
		}
		portFilters := make([]portreports.MarketingFilter, len(filters))
		for index, filter := range filters {
			portFilters[index] = portreports.MarketingFilter{
				Field: filter.Field, Type: filter.Type, Operator: filter.Operator,
				Values: append([]string(nil), filter.Values...),
			}
		}
		materialReportParams = marketingReportParams(customQuery, portFilters)
		reportRows, err = pagination.CollectPages(ctx, pagination.PageOptions[domainreports.MarketingReportRow]{
			MaxPages: MarketingMaxPages, StartPage: query.ReportPage, SinglePage: query.SinglePage,
			Key: marketingReportRowKey,
			Fetch: func(ctx context.Context, page int) (pagination.Page[domainreports.MarketingReportRow], error) {
				result, fetchErr := service.Reader.FetchReportPage(ctx, portreports.MarketingReportPageRequest{
					AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
					DataTopic: query.DataTopic, Dimensions: append([]string(nil), query.Dimensions...),
					Metrics: append([]string(nil), query.Metrics...), Filters: portFilters,
					StartTime: customQuery.StartTime, EndTime: customQuery.EndTime,
					OrderField: query.OrderField, OrderType: query.OrderType,
					Page: page, PageSize: query.ReportPageSize,
				})
				if fetchErr != nil {
					return pagination.Page[domainreports.MarketingReportRow]{}, fetchErr
				}
				reportLast = result
				reportResponseCodes = append(reportResponseCodes, 0)
				if result.RequestID != "" {
					if reportFirstRequestID == "" {
						reportFirstRequestID = result.RequestID
					}
					reportRequestIDs = append(reportRequestIDs, result.RequestID)
				}
				if result.Message != "" {
					reportResponseMessages = append(reportResponseMessages, result.Message)
				}
				return pagination.Page[domainreports.MarketingReportRow]{
					Number: result.Page, TotalPages: result.TotalPages,
					TotalNumber: result.TotalNumber, Rows: result.Rows,
				}, nil
			},
		})
		if err != nil {
			return MarketingMaterialResult{}, err
		}
	}

	joined := joinMarketingMaterialRows(materialRows, reportRows)
	promotionFiltering := map[string]any{}
	if query.ProjectID != "" {
		promotionFiltering["project_id"] = query.ProjectID
	}
	if len(query.PromotionIDs) != 0 {
		promotionFiltering["ids"] = append([]string(nil), query.PromotionIDs...)
	}
	var filtering any
	if len(promotionFiltering) != 0 {
		filtering = promotionFiltering
	}
	promotionParams := map[string]any{
		"advertiser_id": query.AdvertiserID, "filtering": filtering,
		"fields": append([]string(nil), MarketingPromotionFields...),
		"page":   query.PromotionPage, "page_size": query.PromotionPageSize,
	}
	result := MarketingMaterialResult{
		Mode: "unit_materials_report", StatusHandling: "record_only",
		DateRange:         DateRange{StartDate: query.StartDate, EndDate: query.EndDate},
		PromotionEndpoint: MarketingPromotionEndpoint, MaterialReportEndpoint: MarketingReportEndpoint,
		PromotionRequestID:       nullableMarketingString(promotionFirstRequestID),
		PromotionRequestIDs:      promotionRequestIDs,
		MaterialReportRequestID:  nullableMarketingString(reportFirstRequestID),
		MaterialReportRequestIDs: reportRequestIDs,
		PromotionResponseCode:    0, PromotionResponseCodes: promotionResponseCodes,
		PromotionResponseMessage:   nullableMarketingString(promotionLast.Message),
		PromotionResponseMessages:  promotionResponseMessages,
		MaterialReportResponseCode: nil, MaterialReportResponseCodes: reportResponseCodes,
		MaterialReportResponseMessage: nil, MaterialReportResponseMessages: reportResponseMessages,
		PromotionCount: len(promotions), SelectedPromotionCount: len(selected),
		ActiveLikePromotionCount: activeCount, MaterialCount: len(materialRows), RowCount: len(joined),
		PromotionPageInfo: MarketingPageInfo{
			Page: promotionLast.Page, PageSize: promotionLast.PageSize,
			TotalNumber: promotionLast.TotalNumber, TotalPage: promotionLast.TotalPages,
		},
		ReportPageInfo: nil, ReportTotalMetrics: nil,
		ReportScope:     "promotion_and_extracted_material_ids",
		PromotionParams: promotionParams, MaterialReportParams: materialReportParams,
		Rows: joined, ExcludedPromotions: excluded,
	}
	if query.ActiveOnly {
		result.StatusHandling = "active_only"
	}
	if query.IncludeExtraReportMaterials {
		result.ReportScope = "promotion_ids_only_with_extra_report_materials"
	}
	if len(materialRows) != 0 {
		result.MaterialReportResponseCode = int64(0)
		result.MaterialReportResponseMessage = nullableMarketingString(reportLast.Message)
		result.ReportPageInfo = MarketingPageInfo{
			Page: reportLast.Page, PageSize: reportLast.PageSize,
			TotalNumber: reportLast.TotalNumber, TotalPage: reportLast.TotalPages,
		}
		result.ReportTotalMetrics = cloneStringValues(reportLast.TotalMetrics)
	}
	return result, nil
}

var marketingInactiveSecondReasons = map[string]bool{
	"AUDIT": true, "AUDIT_DENY": true, "OFFLINE_BALANCE": true,
	"PROJECT_OFFLINE_BUDGET": true, "PROJECT_OFFLINE": true,
	"PROMOTION_DISABLE": true, "PROMOTION_DELETE": true, "TIME_DONE": true,
}

var marketingInactiveStatuses = map[string]bool{
	"AUDIT": true, "AUDIT_DENY": true, "DELETE": true,
	"DISABLE": true, "DONE": true, "NOT_START": true,
}

func marketingPromotionActive(value domainreports.MarketingPromotion) bool {
	if value.PromotionOptStatus != "ENABLE" || marketingInactiveStatuses[value.PromotionStatus] {
		return false
	}
	for _, reason := range value.PromotionStatusSecond {
		if marketingInactiveSecondReasons[reason] {
			return false
		}
	}
	return true
}

func marketingExcludedPromotion(value domainreports.MarketingPromotion) map[string]any {
	return map[string]any{
		"promotion_id": value.PromotionID, "promotion_name": value.PromotionName,
		"status": value.PromotionStatus, "status_first": value.PromotionStatusFirst,
		"status_second": append([]string(nil), value.PromotionStatusSecond...),
		"opt_status":    value.PromotionOptStatus,
	}
}

func extractMarketingMaterialRows(promotions []domainreports.MarketingPromotion) []map[string]any {
	rows := []map[string]any{}
	for _, promotion := range promotions {
		for _, material := range promotion.Materials {
			if strings.TrimSpace(material.MaterialID) == "" {
				continue
			}
			rows = append(rows, map[string]any{
				"project_id": promotion.ProjectID, "promotion_id": promotion.PromotionID,
				"promotion_name": promotion.PromotionName, "promotion_status": promotion.PromotionStatus,
				"promotion_status_first":  promotion.PromotionStatusFirst,
				"promotion_status_second": strings.Join(promotion.PromotionStatusSecond, ","),
				"promotion_opt_status":    promotion.PromotionOptStatus,
				"material_id":             material.MaterialID, "video_id": nullableMarketingString(material.VideoID),
				"video_cover_id":  nullableMarketingString(material.VideoCoverID),
				"material_status": material.MaterialStatus, "material_opt_status": material.MaterialOptStatus,
				"image_mode": material.ImageMode, "material_create_time": nullableMarketingString(material.MaterialCreateTime),
			})
		}
	}
	return rows
}

func marketingMaterialFilters(rows []map[string]any, includeExtra bool) []MarketingFilter {
	materialIDs := []string{}
	promotionIDs := []string{}
	for _, row := range rows {
		materialIDs = append(materialIDs, fmt.Sprint(row["material_id"]))
		promotionIDs = append(promotionIDs, fmt.Sprint(row["promotion_id"]))
	}
	materialIDs = sortedMarketingIDs(uniqueReportValues(materialIDs))
	promotionIDs = sortedMarketingIDs(uniqueReportValues(promotionIDs))
	filters := []MarketingFilter{}
	if !includeExtra && len(materialIDs) != 0 {
		filters = append(filters, MarketingFilter{Field: "material_id", Type: 2, Operator: 1, Values: materialIDs})
	}
	if len(promotionIDs) != 0 {
		filters = append(filters, MarketingFilter{Field: "cdp_promotion_id", Type: 2, Operator: 1, Values: promotionIDs})
	}
	return filters
}

func sortedMarketingIDs(values []string) []string {
	sort.Slice(values, func(left, right int) bool {
		leftValue := strings.TrimLeft(values[left], "0")
		rightValue := strings.TrimLeft(values[right], "0")
		if len(leftValue) != len(rightValue) {
			return len(leftValue) < len(rightValue)
		}
		return leftValue < rightValue
	})
	return values
}

func marketingReportRowKey(row domainreports.MarketingReportRow) string {
	keys := make([]string, 0, len(row.Dimensions))
	for key := range row.Dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+row.Dimensions[key])
	}
	return strings.Join(parts, "\x1f")
}

func joinMarketingMaterialRows(
	materials []map[string]any,
	reports []domainreports.MarketingReportRow,
) []map[string]any {
	byKey := map[string]domainreports.MarketingReportRow{}
	for _, row := range reports {
		materialID := row.Dimensions["material_id"]
		promotionID := row.Dimensions["cdp_promotion_id"]
		if promotionID == "" {
			promotionID = row.Dimensions["promotion_id"]
		}
		key := materialID + "\x1f" + promotionID
		byKey[key] = row
		if promotionID == "" {
			if _, exists := byKey[materialID+"\x1f"]; !exists {
				byKey[materialID+"\x1f"] = row
			}
		}
	}
	joined := make([]map[string]any, 0, len(materials))
	for _, material := range materials {
		row := cloneAnyMap(material)
		materialID := fmt.Sprint(material["material_id"])
		promotionID := fmt.Sprint(material["promotion_id"])
		report, ok := byKey[materialID+"\x1f"+promotionID]
		if !ok {
			report, ok = byKey[materialID+"\x1f"]
		}
		if ok {
			for key, value := range report.Dimensions {
				if _, exists := row[key]; !exists {
					row[key] = value
				}
			}
			for key, value := range report.Metrics {
				if _, exists := row[key]; !exists {
					row[key] = value
				}
			}
		}
		row["has_report_data"] = ok
		joined = append(joined, row)
	}
	return joined
}

func nullableMarketingString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type parsedMarketingPlan struct {
	ID      string
	Name    string
	Flat    map[string]any
	Metrics map[string]*domain.Decimal
	Spend   domain.Decimal
}

func (row parsedMarketingPlan) presentation(rank int) map[string]any {
	result := map[string]any{"rank": rank, "project_id": row.ID, "project_name": nil}
	if row.Name != "" {
		result["project_name"] = row.Name
	}
	for _, field := range DefaultMarketingPlanMetrics {
		result[field] = nil
		if value := row.Metrics[field]; value != nil {
			result[field] = normalizeMarketingPlanMetric(field, *value)
		}
	}
	return result
}

func (service MarketingService) normalizePlanQuery(query MarketingPlanQuery) (MarketingPlanQuery, error) {
	if err := validateMarketingScope(query.CredentialScope); err != nil {
		return MarketingPlanQuery{}, err
	}
	today := time.Now()
	if service.Now != nil {
		today = service.Now()
	}
	todayText := today.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	query.StartDate = defaultReportString(strings.TrimSpace(query.StartDate), todayText)
	query.EndDate = defaultReportString(strings.TrimSpace(query.EndDate), todayText)
	start, err := time.Parse("2006-01-02", query.StartDate)
	if err != nil {
		return MarketingPlanQuery{}, errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", query.EndDate)
	if err != nil {
		return MarketingPlanQuery{}, errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return MarketingPlanQuery{}, errors.New("start_date cannot be after end_date")
	}
	query.Metrics = uniqueReportValues(query.Metrics)
	if query.PageSize == 0 {
		query.PageSize = MarketingPageSize
	}
	if query.PageSize < 1 || query.PageSize > MarketingPageSize {
		return MarketingPlanQuery{}, errors.New("page_size must be between 1 and 100")
	}
	if query.MaxPages == 0 {
		query.MaxPages = 100
	}
	if query.MaxPages < 1 || query.MaxPages > MarketingMaxPages {
		return MarketingPlanQuery{}, errors.New("max_pages must be between 1 and 500")
	}
	if query.Top < 0 {
		return MarketingPlanQuery{}, errors.New("top must be zero or a positive integer")
	}
	return query, nil
}

func (service MarketingService) normalizeMaterialQuery(
	query MarketingMaterialQuery,
) (MarketingMaterialQuery, error) {
	if err := validateMarketingScope(query.CredentialScope); err != nil {
		return MarketingMaterialQuery{}, err
	}
	today := time.Now()
	if service.Now != nil {
		today = service.Now()
	}
	todayText := today.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	query.StartDate = defaultReportString(strings.TrimSpace(query.StartDate), todayText)
	query.EndDate = defaultReportString(strings.TrimSpace(query.EndDate), todayText)
	start, err := time.Parse("2006-01-02", query.StartDate)
	if err != nil {
		return MarketingMaterialQuery{}, errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", query.EndDate)
	if err != nil {
		return MarketingMaterialQuery{}, errors.New("start_date and end_date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return MarketingMaterialQuery{}, errors.New("start_date cannot be after end_date")
	}
	query.DataTopic = defaultReportString(strings.TrimSpace(query.DataTopic), MarketingMaterialTopic)
	if !ValidMarketingDataTopic(query.DataTopic) {
		return MarketingMaterialQuery{}, fmt.Errorf("unsupported Marketing report data topic %q", query.DataTopic)
	}
	query.Dimensions = uniqueReportValues(query.Dimensions)
	if len(query.Dimensions) == 0 {
		query.Dimensions = append([]string(nil), DefaultMarketingDimensions...)
	}
	query.Metrics = uniqueReportValues(query.Metrics)
	if len(query.Metrics) == 0 {
		query.Metrics = append([]string(nil), DefaultMarketingMetrics...)
	}
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	if query.ProjectID != "" {
		if err := domainValidateReportID(query.ProjectID, "project_id"); err != nil {
			return MarketingMaterialQuery{}, err
		}
	}
	query.PromotionIDs = uniqueReportValues(query.PromotionIDs)
	for index, promotionID := range query.PromotionIDs {
		if err := domainValidateReportID(promotionID, fmt.Sprintf("promotion_id[%d]", index)); err != nil {
			return MarketingMaterialQuery{}, err
		}
	}
	if query.PromotionPage == 0 {
		query.PromotionPage = 1
	}
	if query.PromotionPageSize == 0 {
		query.PromotionPageSize = 20
	}
	if query.ReportPage == 0 {
		query.ReportPage = 1
	}
	if query.ReportPageSize == 0 {
		query.ReportPageSize = MarketingPageSize
	}
	if query.PromotionPage < 1 || query.ReportPage < 1 {
		return MarketingMaterialQuery{}, errors.New("promotion_page and report_page must be positive")
	}
	if query.PromotionPageSize < 1 || query.PromotionPageSize > MarketingPageSize ||
		query.ReportPageSize < 1 || query.ReportPageSize > MarketingPageSize {
		return MarketingMaterialQuery{}, errors.New("promotion_page_size and report_page_size must be between 1 and 100")
	}
	query.OrderField = defaultReportString(strings.TrimSpace(query.OrderField), "stat_cost")
	query.OrderType = defaultReportString(strings.ToUpper(strings.TrimSpace(query.OrderType)), "DESC")
	if query.OrderType != "ASC" && query.OrderType != "DESC" {
		return MarketingMaterialQuery{}, errors.New("order_type must be ASC or DESC")
	}
	return query, nil
}

func selectMarketingPlanContract(
	topic domainreports.MarketingTopic,
	requestedMetrics []string,
) (MarketingPlanContract, error) {
	dimensions := map[string]bool{}
	for _, field := range topic.Dimensions {
		dimensions[field.Field] = true
	}
	metrics := map[string]bool{}
	for _, field := range topic.Metrics {
		metrics[field.Field] = true
	}
	idDimension := firstAvailable(MarketingPlanIDDimensions, dimensions)
	if idDimension == "" {
		return MarketingPlanContract{}, fmt.Errorf(
			"UNI_PROJECT_DATA has no supported project ID dimension; available_dimensions=%v",
			sortedKeys(dimensions),
		)
	}
	selectedDimensions := []string{idDimension}
	if name := firstAvailable(MarketingPlanNameDimensions, dimensions); name != "" {
		selectedDimensions = append(selectedDimensions, name)
	}
	explicit := len(requestedMetrics) != 0
	requested := requestedMetrics
	if !explicit {
		requested = append([]string(nil), DefaultMarketingPlanMetrics...)
	}
	missing := []string{}
	selected := []string{}
	for _, field := range requested {
		if metrics[field] {
			selected = append(selected, field)
		} else {
			missing = append(missing, field)
		}
	}
	if explicit && len(missing) != 0 {
		return MarketingPlanContract{}, fmt.Errorf(
			"requested Marketing report metrics are unavailable: missing_metrics=%v available_metrics=%v",
			missing, sortedKeys(metrics),
		)
	}
	if !containsReportValue(selected, "stat_cost") {
		return MarketingPlanContract{}, errors.New("UNI_PROJECT_DATA does not expose stat_cost")
	}
	return MarketingPlanContract{
		Dimensions: selectedDimensions, Metrics: selected,
		AvailableDimensionCount: len(dimensions), AvailableMetricCount: len(metrics),
		OmittedDefaultMetrics: missing,
	}, nil
}

func parseMarketingPlanRows(
	rows []domainreports.MarketingReportRow,
	contract MarketingPlanContract,
) ([]parsedMarketingPlan, error) {
	result := make([]parsedMarketingPlan, 0, len(rows))
	idDimension := contract.Dimensions[0]
	nameDimension := ""
	if len(contract.Dimensions) > 1 {
		nameDimension = contract.Dimensions[1]
	}
	for _, row := range rows {
		id := strings.TrimSpace(row.Dimensions[idDimension])
		if err := domainValidateReportID(id, idDimension); err != nil {
			return nil, err
		}
		flat := flattenMarketingRow(row)
		metrics := make(map[string]*domain.Decimal, len(contract.Metrics))
		for _, field := range contract.Metrics {
			text, exists := row.Metrics[field]
			if !exists || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("Marketing plan report omitted selected metric %s for %s", field, id)
			}
			value, err := domain.ParseDecimal(text)
			if err != nil {
				return nil, fmt.Errorf("Marketing plan report returned a non-numeric metric %s for %s", field, id)
			}
			metrics[field] = &value
		}
		result = append(result, parsedMarketingPlan{
			ID: id, Name: strings.TrimSpace(row.Dimensions[nameDimension]), Flat: flat,
			Metrics: metrics, Spend: *metrics["stat_cost"],
		})
	}
	return result, nil
}

func summarizeMarketingPlans(
	rows []parsedMarketingPlan,
	selectedMetrics []string,
) (MarketingPlanSummary, error) {
	available := map[string]bool{}
	for _, field := range selectedMetrics {
		available[field] = true
	}
	var spend, gmv, orders domain.Decimal
	result := MarketingPlanSummary{}
	for _, row := range rows {
		spend = spend.Add(*row.Metrics["stat_cost"])
		if row.Spend.Sign() > 0 {
			result.PlansWithSpend++
		}
		if available["in_app_order_gmv"] {
			gmv = gmv.Add(*row.Metrics["in_app_order_gmv"])
		}
		if available["in_app_order_count"] {
			orders = orders.Add(*row.Metrics["in_app_order_count"])
		}
	}
	result.TotalSpend = spend.Round(2)
	if available["in_app_order_gmv"] {
		value := gmv.Round(2)
		result.TotalGMV = &value
		if spend.Sign() != 0 {
			roi, err := gmv.Divide(spend)
			if err != nil {
				return MarketingPlanSummary{}, err
			}
			roi = roi.Round(4)
			result.WeightedROI = &roi
		}
	}
	if available["in_app_order_count"] {
		value, err := orders.Int64Exact()
		if err != nil {
			return MarketingPlanSummary{}, errors.New("Marketing plan report returned a fractional order count")
		}
		result.TotalOrders = &value
	}
	return result, nil
}

func normalizeMarketingPlanMetric(field string, value domain.Decimal) any {
	switch field {
	case "show_cnt", "click_cnt", "convert_cnt", "in_app_order_count":
		if integer, err := value.Int64Exact(); err == nil {
			return integer
		}
		return value
	case "stat_cost", "conversion_cost", "in_app_order_gmv":
		return value.Round(2)
	default:
		return value.Round(4)
	}
}

func renderMarketingPlanTable(rows []map[string]any) string {
	labels, separators := []string{}, []string{}
	for _, column := range MarketingPlanPresentationColumns {
		labels = append(labels, column.Label)
		if column.Field == "rank" {
			separators = append(separators, "---:")
		} else {
			separators = append(separators, "---")
		}
	}
	lines := []string{
		"| " + strings.Join(labels, " | ") + " |",
		"| " + strings.Join(separators, " | ") + " |",
	}
	for _, row := range rows {
		values := make([]string, 0, len(MarketingPlanPresentationColumns))
		for _, column := range MarketingPlanPresentationColumns {
			values = append(values, marketingPlanPresentationValue(column.Field, row[column.Field]))
		}
		lines = append(lines, "| "+strings.Join(values, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func marketingPlanPresentationValue(field string, value any) string {
	if value == nil {
		return "—"
	}
	if decimal, ok := value.(domain.Decimal); ok {
		switch field {
		case "stat_cost", "conversion_cost", "in_app_order_gmv":
			return "¥" + marketingCommaMoney(decimal.StringFixed(2))
		default:
			return domain.EscapeMarkdownValue(decimal.String())
		}
	}
	return domain.EscapeMarkdownValue(value)
}

func marketingCommaMoney(value string) string {
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

func firstAvailable(candidates []string, available map[string]bool) string {
	for _, candidate := range candidates {
		if available[candidate] {
			return candidate
		}
	}
	return ""
}

func containsReportValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (service MarketingService) normalizeCustomQuery(query MarketingCustomQuery) (MarketingCustomQuery, error) {
	if err := validateMarketingScope(query.CredentialScope); err != nil {
		return MarketingCustomQuery{}, err
	}
	query.DataTopic = defaultReportString(strings.TrimSpace(query.DataTopic), MarketingMaterialTopic)
	if !ValidMarketingDataTopic(query.DataTopic) {
		return MarketingCustomQuery{}, fmt.Errorf("unsupported Marketing report data topic %q", query.DataTopic)
	}
	query.Dimensions = uniqueReportValues(query.Dimensions)
	if len(query.Dimensions) == 0 {
		query.Dimensions = append([]string(nil), DefaultMarketingDimensions...)
	}
	query.Metrics = uniqueReportValues(query.Metrics)
	if len(query.Metrics) == 0 {
		query.Metrics = append([]string(nil), DefaultMarketingMetrics...)
	}
	if len(query.Dimensions) == 0 || len(query.Metrics) == 0 {
		return MarketingCustomQuery{}, errors.New("dimensions and metrics must not be empty")
	}
	for index := range query.Filters {
		query.Filters[index].Field = strings.TrimSpace(query.Filters[index].Field)
		query.Filters[index].Values = uniqueReportValues(query.Filters[index].Values)
		if query.Filters[index].Field == "" || len(query.Filters[index].Values) == 0 {
			return MarketingCustomQuery{}, fmt.Errorf("filter[%d] requires a field and values", index)
		}
	}
	start, end, err := service.marketingTimes(query.StartTime, query.EndTime)
	if err != nil {
		return MarketingCustomQuery{}, err
	}
	query.StartTime, query.EndTime = start, end
	query.OrderField = defaultReportString(strings.TrimSpace(query.OrderField), "stat_cost")
	query.OrderType = defaultReportString(strings.ToUpper(strings.TrimSpace(query.OrderType)), "DESC")
	if query.OrderType != "ASC" && query.OrderType != "DESC" {
		return MarketingCustomQuery{}, errors.New("order_type must be ASC or DESC")
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Page < 1 {
		return MarketingCustomQuery{}, errors.New("page must be positive")
	}
	if query.PageSize == 0 {
		query.PageSize = MarketingPageSize
	}
	if query.PageSize < 1 || query.PageSize > MarketingPageSize {
		return MarketingCustomQuery{}, errors.New("page_size must be between 1 and 100")
	}
	return query, nil
}

func (service MarketingService) marketingToken(
	ctx context.Context,
	scope CredentialScope,
) (authapplication.TokenLease, error) {
	if service.Tokens == nil || service.Reader == nil {
		return authapplication.TokenLease{}, errors.New("Marketing report dependencies are incomplete")
	}
	return service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "marketing", AdvertiserID: scope.AdvertiserID, AuthAccountID: scope.AuthAccountID,
	})
}

func (service MarketingService) marketingTimes(startTime, endTime string) (string, string, error) {
	today := time.Now()
	if service.Now != nil {
		today = service.Now()
	}
	todayText := today.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	startTime = normalizeMarketingTime(startTime, todayText, false)
	endTime = normalizeMarketingTime(endTime, todayText, true)
	start, err := time.Parse("2006-01-02 15:04:05", startTime)
	if err != nil {
		return "", "", errors.New("start_time and end_time must use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
	}
	end, err := time.Parse("2006-01-02 15:04:05", endTime)
	if err != nil {
		return "", "", errors.New("start_time and end_time must use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
	}
	if start.After(end) {
		return "", "", errors.New("start_time cannot be after end_time")
	}
	return startTime, endTime, nil
}

func normalizeMarketingTime(value, defaultDate string, end bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultDate
	}
	if len(value) == 10 {
		if end {
			return value + " 23:59:59"
		}
		return value + " 00:00:00"
	}
	return value
}

func validateMarketingScope(scope CredentialScope) error {
	if err := domainValidateReportID(scope.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	if scope.AuthAccountID != "" {
		return domainValidateReportID(scope.AuthAccountID, "auth_account_id")
	}
	return nil
}

func domainValidateReportID(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must be a positive integer", field)
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("%s must be a positive integer", field)
		}
	}
	if strings.TrimLeft(value, "0") == "" {
		return fmt.Errorf("%s must be a positive integer", field)
	}
	return nil
}

func validateSchemaTopics(topics []domainreports.MarketingTopic, requested []string) error {
	available := make(map[string]bool, len(topics))
	for _, topic := range topics {
		if topic.DataTopic == "" || available[topic.DataTopic] {
			return errors.New("Marketing report schema returned duplicate or empty data_topic")
		}
		available[topic.DataTopic] = true
		if err := validateSchemaFields(topic.Dimensions, "dimension", topic.DataTopic); err != nil {
			return err
		}
		if err := validateSchemaFields(topic.Metrics, "metric", topic.DataTopic); err != nil {
			return err
		}
	}
	for _, topic := range requested {
		if !available[topic] {
			return fmt.Errorf("Marketing report schema omitted requested data topic %s", topic)
		}
	}
	return nil
}

func validateSchemaFields(fields []domainreports.MarketingField, kind, topic string) error {
	seen := map[string]bool{}
	for _, field := range fields {
		if strings.TrimSpace(field.Field) == "" || seen[field.Field] {
			return fmt.Errorf("Marketing report schema topic %s returned duplicate or empty %s", topic, kind)
		}
		seen[field.Field] = true
	}
	return nil
}

func marketingReportParams(
	query MarketingCustomQuery,
	filters []portreports.MarketingFilter,
) map[string]any {
	return map[string]any{
		"advertiser_id": query.AdvertiserID, "data_topic": query.DataTopic,
		"dimensions": append([]string(nil), query.Dimensions...),
		"metrics":    append([]string(nil), query.Metrics...), "filters": filters,
		"start_time": query.StartTime, "end_time": query.EndTime,
		"order_by": []map[string]any{{"field": query.OrderField, "type": query.OrderType}},
		"page":     query.Page, "page_size": query.PageSize,
	}
}

func flattenMarketingRow(row domainreports.MarketingReportRow) map[string]any {
	result := make(map[string]any, len(row.Dimensions)+len(row.Metrics))
	for key, value := range row.Dimensions {
		result[key] = value
	}
	for key, value := range row.Metrics {
		result[key] = value
	}
	return result
}

func uniqueReportValues(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func cloneStringValues(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func cloneAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func defaultReportString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
