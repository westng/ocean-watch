package reports

import (
	"context"
	"errors"
	"fmt"
	"strings"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

const (
	QianchuanAllPromotionEndpoint = "/v1.0/qianchuan/report/all_promotion/get/"
	QianchuanUniPromotionEndpoint = "/v1.0/qianchuan/report/uni_promotion/get/"
	QianchuanSchemaEndpoint       = "/v1.0/qianchuan/report/uni_promotion/config/get/"
	QianchuanDataEndpoint         = "/v1.0/qianchuan/report/uni_promotion/data/get/"
	QianchuanRoomEndpoint         = "/v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/"
	QianchuanAuthorEndpoint       = "/v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/"
	QianchuanProductTopic         = "SITE_PROMOTION_PRODUCT_PRODUCT"
	QianchuanOverallProductTopic  = "OVERALL_ROI_PRODUCT_PRODUCT"
	QianchuanUnifiedMaxPages      = 500
	QianchuanDimensionMaxPageSize = 100
)

var QianchuanUnifiedPageSizes = map[int]bool{10: true, 20: true, 50: true, 100: true, 200: true}

var DefaultQianchuanAllPromotionFields = []string{
	"stat_cost_for_roi2",
	"total_pay_order_count_for_roi2", "total_pay_order_gmv_include_coupon_for_roi2",
	"total_prepay_and_pay_order_roi2", "total_order_settle_amount_for_roi2_1h",
	"total_prepay_and_pay_settle_roi2_1h",
}

var DefaultQianchuanUniPromotionFields = []string{
	"stat_cost", "total_pay_order_count_for_roi2",
	"total_pay_order_gmv_include_coupon_for_roi2", "total_prepay_and_pay_order_roi2",
}

var DefaultQianchuanProductMetrics = []string{
	"stat_cost", "stat_cost_for_roi2", "stat_cost_for_overall_roi2",
	"total_pay_order_count_for_roi2", "total_pay_order_gmv_include_coupon_for_roi2",
	"total_prepay_and_pay_order_roi2", "total_prepay_and_pay_settle_overall_roi2_1h",
}

var DefaultQianchuanDimensionMetrics = []string{
	"stat_cost", "stat_cost_for_roi2", "total_pay_order_count_for_roi2",
	"total_pay_order_gmv_include_coupon_for_roi2", "total_prepay_and_pay_order_roi2",
	"total_order_settle_amount_for_roi2_1h", "total_prepay_and_pay_settle_roi2_1h",
}

type QianchuanSchemaQuery struct {
	CredentialScope
	Topics     []string
	DataPeriod string
}

type QianchuanSchemaResult struct {
	Mode         string                          `json:"mode"`
	Endpoint     string                          `json:"endpoint"`
	AdvertiserID string                          `json:"advertiser_id"`
	DataPeriod   string                          `json:"data_period,omitempty"`
	Schemas      []domainreports.QianchuanSchema `json:"schemas"`
	RequestIDs   []string                        `json:"request_ids"`
}

type QianchuanAggregateQuery struct {
	CredentialScope
	StartDate     string
	EndDate       string
	Fields        []string
	AdlabScene    string
	DataPeriod    string
	MarketingGoal string
	OrderPlatform string
}

type QianchuanAggregateResult struct {
	Mode          string         `json:"mode"`
	Endpoint      string         `json:"endpoint"`
	AdvertiserID  string         `json:"advertiser_id"`
	DateRange     DateRange      `json:"date_range"`
	Fields        []string       `json:"fields"`
	AdlabScene    string         `json:"adlab_scene,omitempty"`
	DataPeriod    string         `json:"data_period,omitempty"`
	MarketingGoal string         `json:"marketing_goal"`
	OrderPlatform string         `json:"order_platform"`
	Data          map[string]any `json:"data"`
	RequestIDs    []string       `json:"request_ids"`
}

type QianchuanFilter struct {
	Field    string   `json:"field"`
	Operator int64    `json:"operator"`
	Values   []string `json:"values"`
}

type QianchuanCustomQuery struct {
	CredentialScope
	StartDate  string
	EndDate    string
	DataTopic  string
	Dimensions []string
	Metrics    []string
	Filters    []QianchuanFilter
	DataPeriod string
	OrderField string
	OrderType  string
	PageSize   int
	MaxPages   int
	Top        int
}

type QianchuanCustomResult struct {
	Mode           string                             `json:"mode"`
	Endpoint       string                             `json:"endpoint"`
	AdvertiserID   string                             `json:"advertiser_id"`
	DateRange      DateRange                          `json:"date_range"`
	DataTopic      string                             `json:"data_topic"`
	Dimensions     []string                           `json:"dimensions"`
	Metrics        []string                           `json:"metrics"`
	Filters        []QianchuanFilter                  `json:"filters"`
	DataPeriod     string                             `json:"data_period,omitempty"`
	Rows           []domainreports.QianchuanReportRow `json:"rows"`
	DisplayedCount int                                `json:"displayed_count"`
	TotalRowCount  int                                `json:"total_row_count"`
	PageCount      int                                `json:"page_count"`
	RequestIDs     []string                           `json:"request_ids"`
	Truncated      bool                               `json:"truncated"`
}

type QianchuanDimensionQuery struct {
	CredentialScope
	DimensionID   string
	StartDate     string
	EndDate       string
	Dimension     string
	Metrics       []string
	MarketingGoal string
	OrderPlatform string
	SmartBidType  string
	OrderField    string
	OrderType     string
	PageSize      int
	MaxPages      int
	Top           int
}

type QianchuanDimensionResult struct {
	Mode           string                                `json:"mode"`
	Endpoint       string                                `json:"endpoint"`
	AdvertiserID   string                                `json:"advertiser_id"`
	DimensionID    string                                `json:"dimension_id"`
	DateRange      DateRange                             `json:"date_range"`
	Dimension      string                                `json:"dimension"`
	Metrics        []string                              `json:"metrics"`
	OrderPlatform  string                                `json:"order_platform"`
	SmartBidType   string                                `json:"smart_bid_type,omitempty"`
	Rows           []domainreports.QianchuanDimensionRow `json:"rows"`
	DisplayedCount int                                   `json:"displayed_count"`
	TotalRowCount  int                                   `json:"total_row_count"`
	PageCount      int                                   `json:"page_count"`
	RequestIDs     []string                              `json:"request_ids"`
	Truncated      bool                                  `json:"truncated"`
}

func (service Service) QianchuanSchema(ctx context.Context, query QianchuanSchemaQuery) (QianchuanSchemaResult, error) {
	reader, lease, ctx, err := service.unifiedContext(ctx, query.CredentialScope)
	if err != nil {
		return QianchuanSchemaResult{}, err
	}
	topics := uniqueNonEmpty(query.Topics)
	if len(topics) == 0 {
		return QianchuanSchemaResult{}, errors.New("at least one data topic is required")
	}
	query.DataPeriod = strings.TrimSpace(query.DataPeriod)
	if !validQianchuanDataPeriod(query.DataPeriod) {
		return QianchuanSchemaResult{}, errors.New("data_period is not supported")
	}
	schemas, err := reader.FetchSchemas(ctx, portreports.SchemaRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		Topics: topics, DataPeriod: query.DataPeriod,
	})
	if err != nil {
		return QianchuanSchemaResult{}, err
	}
	requestIDs := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		requestIDs = append(requestIDs, schema.RequestID)
	}
	requestIDs = uniqueNonEmpty(requestIDs)
	return QianchuanSchemaResult{
		Mode: "qianchuan_unified_report_schema", Endpoint: QianchuanSchemaEndpoint,
		AdvertiserID: query.AdvertiserID, DataPeriod: query.DataPeriod,
		Schemas: schemas, RequestIDs: requestIDs,
	}, nil
}

func (service Service) QianchuanAllPromotion(ctx context.Context, query QianchuanAggregateQuery) (QianchuanAggregateResult, error) {
	return service.qianchuanAggregate(ctx, query, true)
}

func (service Service) QianchuanUniPromotion(ctx context.Context, query QianchuanAggregateQuery) (QianchuanAggregateResult, error) {
	return service.qianchuanAggregate(ctx, query, false)
}

func (service Service) qianchuanAggregate(ctx context.Context, query QianchuanAggregateQuery, all bool) (QianchuanAggregateResult, error) {
	query, err := service.normalizeAggregateQuery(query)
	if err != nil {
		return QianchuanAggregateResult{}, err
	}
	reader, lease, ctx, err := service.unifiedContext(ctx, query.CredentialScope)
	if err != nil {
		return QianchuanAggregateResult{}, err
	}
	start, end := query.StartDate, query.EndDate
	endpoint, mode := QianchuanUniPromotionEndpoint, "qianchuan_uni_account_report"
	request := portreports.AggregateRequest{
		AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
		StartTime: start + " 00:00:00", EndTime: end + " 23:59:59", Fields: query.Fields,
		AdlabScene: query.AdlabScene, DataPeriod: query.DataPeriod,
		MarketingGoal: query.MarketingGoal, OrderPlatform: query.OrderPlatform,
	}
	var aggregate domainreports.QianchuanAggregate
	if all {
		endpoint, mode = QianchuanAllPromotionEndpoint, "qianchuan_all_promotion_account_report"
		aggregate, err = reader.FetchAllPromotion(ctx, request)
	} else {
		aggregate, err = reader.FetchUniPromotion(ctx, request)
	}
	if err != nil {
		return QianchuanAggregateResult{}, err
	}
	requestIDs := []string{}
	if aggregate.RequestID != "" {
		requestIDs = append(requestIDs, aggregate.RequestID)
	}
	return QianchuanAggregateResult{
		Mode: mode, Endpoint: endpoint, AdvertiserID: query.AdvertiserID,
		DateRange: DateRange{StartDate: start, EndDate: end}, Fields: query.Fields,
		AdlabScene: query.AdlabScene, DataPeriod: query.DataPeriod,
		MarketingGoal: query.MarketingGoal, OrderPlatform: query.OrderPlatform,
		Data: aggregate.Values, RequestIDs: requestIDs,
	}, nil
}

func (service Service) QianchuanCustom(ctx context.Context, query QianchuanCustomQuery) (QianchuanCustomResult, error) {
	query, err := service.normalizeCustomQianchuanQuery(query)
	if err != nil {
		return QianchuanCustomResult{}, err
	}
	reader, lease, ctx, err := service.unifiedContext(ctx, query.CredentialScope)
	if err != nil {
		return QianchuanCustomResult{}, err
	}
	filters := make([]portreports.ReportFilter, len(query.Filters))
	for index, filter := range query.Filters {
		filters[index] = portreports.ReportFilter{
			Field: filter.Field, Operator: filter.Operator, Values: append([]string(nil), filter.Values...),
		}
	}
	rows := []domainreports.QianchuanReportRow{}
	requestIDs := []string{}
	pageCount := 0
	totalPages, totalNumber := -1, -1
	startTime, endTime := reportTimes(query.StartDate, query.EndDate)
	for page := 1; page <= query.MaxPages; page++ {
		result, fetchErr := reader.FetchDataPage(ctx, portreports.DataPageRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
			Topic: query.DataTopic, Dimensions: query.Dimensions, Metrics: query.Metrics,
			Filters: filters, StartTime: startTime, EndTime: endTime,
			OrderField: query.OrderField, OrderType: qianchuanOrderType(query.OrderType),
			DataPeriod: query.DataPeriod, Page: page, PageSize: query.PageSize,
		})
		if fetchErr != nil {
			return QianchuanCustomResult{}, fetchErr
		}
		if page == 1 {
			totalPages, totalNumber = result.PageInfo.TotalPages, result.PageInfo.TotalNumber
		} else if result.PageInfo.TotalPages != totalPages || result.PageInfo.TotalNumber != totalNumber {
			return QianchuanCustomResult{}, errors.New("Qianchuan report pagination changed during traversal")
		}
		rows = append(rows, result.Rows...)
		pageCount++
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		if page >= totalPages {
			break
		}
		if page == query.MaxPages {
			return QianchuanCustomResult{}, errors.New("Qianchuan report reached the configured page cap")
		}
	}
	if len(rows) != totalNumber {
		return QianchuanCustomResult{}, errors.New("Qianchuan report row count contradicts pagination metadata")
	}
	displayed := rows
	if query.Top != 0 && len(displayed) > query.Top {
		displayed = displayed[:query.Top]
	}
	return QianchuanCustomResult{
		Mode: "qianchuan_unified_report", Endpoint: QianchuanDataEndpoint,
		AdvertiserID: query.AdvertiserID, DateRange: DateRange{query.StartDate, query.EndDate},
		DataTopic: query.DataTopic, Dimensions: query.Dimensions, Metrics: query.Metrics,
		Filters: query.Filters, DataPeriod: query.DataPeriod, Rows: displayed,
		DisplayedCount: len(displayed), TotalRowCount: len(rows), PageCount: pageCount,
		RequestIDs: requestIDs, Truncated: len(displayed) != len(rows),
	}, nil
}

func (service Service) QianchuanRoom(ctx context.Context, query QianchuanDimensionQuery) (QianchuanDimensionResult, error) {
	return service.qianchuanDimension(ctx, query, true)
}

func (service Service) QianchuanAuthor(ctx context.Context, query QianchuanDimensionQuery) (QianchuanDimensionResult, error) {
	return service.qianchuanDimension(ctx, query, false)
}

func (service Service) qianchuanDimension(ctx context.Context, query QianchuanDimensionQuery, room bool) (QianchuanDimensionResult, error) {
	query, err := service.normalizeDimensionQuery(query)
	if err != nil {
		return QianchuanDimensionResult{}, err
	}
	reader, lease, ctx, err := service.unifiedContext(ctx, query.CredentialScope)
	if err != nil {
		return QianchuanDimensionResult{}, err
	}
	rows := []domainreports.QianchuanDimensionRow{}
	requestIDs := []string{}
	pageCount, totalPages, totalNumber := 0, -1, -1
	startTime, endTime := reportTimes(query.StartDate, query.EndDate)
	endpoint, mode := QianchuanAuthorEndpoint, "qianchuan_author_dimension_report"
	for page := 1; page <= query.MaxPages; page++ {
		request := portreports.DimensionPageRequest{
			AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
			DimensionID: query.DimensionID, StartTime: startTime, EndTime: endTime,
			Dimension: query.Dimension, Metrics: query.Metrics, MarketingGoal: query.MarketingGoal,
			OrderPlatform: query.OrderPlatform, SmartBidType: query.SmartBidType,
			OrderField: query.OrderField, OrderType: query.OrderType, Page: page, PageSize: query.PageSize,
		}
		var result domainreports.QianchuanDimensionPage
		if room {
			endpoint, mode = QianchuanRoomEndpoint, "qianchuan_room_dimension_report"
			result, err = reader.FetchRoomDimensionPage(ctx, request)
		} else {
			result, err = reader.FetchAuthorDimensionPage(ctx, request)
		}
		if err != nil {
			return QianchuanDimensionResult{}, err
		}
		if page == 1 {
			totalPages, totalNumber = result.PageInfo.TotalPages, result.PageInfo.TotalNumber
		} else if result.PageInfo.TotalPages != totalPages || result.PageInfo.TotalNumber != totalNumber {
			return QianchuanDimensionResult{}, errors.New("Qianchuan dimension report pagination changed during traversal")
		}
		rows = append(rows, result.Rows...)
		pageCount++
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		if page >= totalPages {
			break
		}
		if page == query.MaxPages {
			return QianchuanDimensionResult{}, errors.New("Qianchuan dimension report reached the configured page cap")
		}
	}
	if len(rows) != totalNumber {
		return QianchuanDimensionResult{}, errors.New("Qianchuan dimension report row count contradicts pagination metadata")
	}
	displayed := rows
	if query.Top != 0 && len(displayed) > query.Top {
		displayed = displayed[:query.Top]
	}
	return QianchuanDimensionResult{
		Mode: mode, Endpoint: endpoint, AdvertiserID: query.AdvertiserID,
		DimensionID: query.DimensionID, DateRange: DateRange{query.StartDate, query.EndDate},
		Dimension: query.Dimension, Metrics: query.Metrics,
		OrderPlatform: query.OrderPlatform, SmartBidType: query.SmartBidType, Rows: displayed,
		DisplayedCount: len(displayed), TotalRowCount: len(rows), PageCount: pageCount,
		RequestIDs: requestIDs, Truncated: len(displayed) != len(rows),
	}, nil
}

func (service Service) unifiedContext(ctx context.Context, scope CredentialScope) (portreports.QianchuanUnifiedReader, authapplication.TokenLease, context.Context, error) {
	if err := validateScope(scope); err != nil {
		return nil, authapplication.TokenLease{}, ctx, err
	}
	reader := service.UnifiedReader
	if reader == nil {
		reader, _ = service.Reader.(portreports.QianchuanUnifiedReader)
	}
	if reader == nil {
		return nil, authapplication.TokenLease{}, ctx, errors.New("Qianchuan unified report reader is unavailable")
	}
	lease, err := service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "qianchuan", AdvertiserID: scope.AdvertiserID, AuthAccountID: scope.AuthAccountID,
	})
	if err != nil {
		return nil, authapplication.TokenLease{}, ctx, err
	}
	ctx, err = authapplication.WithAdvertiserTokenLease(ctx, lease, scope.AdvertiserID)
	return reader, lease, ctx, err
}

func (service Service) normalizeAggregateQuery(query QianchuanAggregateQuery) (QianchuanAggregateQuery, error) {
	start, end, err := service.dateRange(query.StartDate, query.EndDate)
	if err != nil {
		return query, err
	}
	query.StartDate, query.EndDate = start, end
	query.Fields = uniqueNonEmpty(query.Fields)
	if len(query.Fields) == 0 {
		return query, errors.New("at least one field is required")
	}
	query.MarketingGoal = defaultString(strings.TrimSpace(query.MarketingGoal), "ALL")
	query.OrderPlatform = defaultString(strings.TrimSpace(query.OrderPlatform), "QIANCHUAN")
	query.AdlabScene = defaultString(strings.TrimSpace(query.AdlabScene), "OVERALL_PROJECT")
	if query.AdlabScene != "OVERALL_PROJECT" && query.AdlabScene != "UNI_PROJECT" {
		return query, errors.New("adlab_scene must be OVERALL_PROJECT or UNI_PROJECT")
	}
	query.DataPeriod = strings.TrimSpace(query.DataPeriod)
	if !validQianchuanDataPeriod(query.DataPeriod) {
		return query, errors.New("data_period is not supported")
	}
	if query.AdlabScene != "OVERALL_PROJECT" && query.DataPeriod != "" {
		return query, errors.New("data_period is supported only for OVERALL_PROJECT")
	}
	return query, validateScope(query.CredentialScope)
}

func (service Service) normalizeCustomQianchuanQuery(query QianchuanCustomQuery) (QianchuanCustomQuery, error) {
	start, end, err := service.dateRange(query.StartDate, query.EndDate)
	if err != nil {
		return query, err
	}
	query.StartDate, query.EndDate = start, end
	query.DataTopic = strings.TrimSpace(query.DataTopic)
	query.Dimensions, query.Metrics = uniqueNonEmpty(query.Dimensions), uniqueNonEmpty(query.Metrics)
	if query.DataTopic == "" || len(query.Dimensions) == 0 || len(query.Metrics) == 0 {
		return query, errors.New("data_topic, dimensions, and metrics are required")
	}
	query.OrderField = defaultString(strings.TrimSpace(query.OrderField), "stat_cost")
	query.OrderType = defaultString(strings.ToUpper(strings.TrimSpace(query.OrderType)), "DESC")
	if query.OrderType != "ASC" && query.OrderType != "DESC" {
		return query, errors.New("order_type must be ASC or DESC")
	}
	if query.PageSize == 0 {
		query.PageSize = 100
	}
	if !QianchuanUnifiedPageSizes[query.PageSize] {
		return query, errors.New("page_size must be one of 10, 20, 50, 100, or 200")
	}
	if query.MaxPages == 0 {
		query.MaxPages = 100
	}
	if query.MaxPages < 1 || query.MaxPages > QianchuanUnifiedMaxPages || query.Top < 0 {
		return query, errors.New("max_pages must be 1..500 and top must be non-negative")
	}
	if !validQianchuanDataPeriod(query.DataPeriod) {
		return query, errors.New("data_period is not supported")
	}
	for index, filter := range query.Filters {
		if strings.TrimSpace(filter.Field) == "" || filter.Operator != 7 || len(filter.Values) == 0 {
			return query, fmt.Errorf("filter[%d] requires a field, operator 7, and values", index)
		}
	}
	return query, validateScope(query.CredentialScope)
}

func (service Service) normalizeDimensionQuery(query QianchuanDimensionQuery) (QianchuanDimensionQuery, error) {
	start, end, err := service.dateRange(query.StartDate, query.EndDate)
	if err != nil {
		return query, err
	}
	query.StartDate, query.EndDate = start, end
	if !positiveID(query.DimensionID) {
		return query, errors.New("dimension_id must be a positive integer")
	}
	query.Metrics = uniqueNonEmpty(query.Metrics)
	if len(query.Metrics) == 0 {
		return query, errors.New("at least one metric is required")
	}
	if query.Dimension != "TIME_GRANULARITY_DAILY" && query.Dimension != "TIME_GRANULARITY_HOURLY" {
		return query, errors.New("dimension must be daily or hourly")
	}
	query.MarketingGoal = defaultString(strings.TrimSpace(query.MarketingGoal), "ALL")
	query.OrderPlatform = defaultString(strings.TrimSpace(query.OrderPlatform), "QIANCHUAN")
	if query.OrderPlatform != "ALL" && query.OrderPlatform != "QIANCHUAN" && query.OrderPlatform != "ECP_AWEME" {
		return query, errors.New("order_platform must be ALL, QIANCHUAN, or ECP_AWEME")
	}
	query.SmartBidType = strings.TrimSpace(query.SmartBidType)
	if query.SmartBidType != "" && query.SmartBidType != "SMART_BID_CUSTOM" && query.SmartBidType != "SMART_BID_CONSERVATIVE" {
		return query, errors.New("smart_bid_type must be SMART_BID_CUSTOM or SMART_BID_CONSERVATIVE")
	}
	query.OrderType = defaultString(strings.ToUpper(strings.TrimSpace(query.OrderType)), "DESC")
	if query.OrderField == "" || (query.OrderType != "ASC" && query.OrderType != "DESC") {
		return query, errors.New("order field and ASC or DESC order type are required")
	}
	if query.PageSize == 0 {
		query.PageSize = 100
	}
	if query.PageSize < 1 || query.PageSize > QianchuanDimensionMaxPageSize {
		return query, errors.New("page_size must be between 1 and 100")
	}
	if query.MaxPages == 0 {
		query.MaxPages = 100
	}
	if query.MaxPages < 1 || query.MaxPages > QianchuanUnifiedMaxPages || query.Top < 0 {
		return query, errors.New("max_pages must be 1..500 and top must be non-negative")
	}
	return query, validateScope(query.CredentialScope)
}

func qianchuanOrderType(value string) int64 {
	if value == "ASC" {
		return 1
	}
	return 2
}

func validQianchuanDataPeriod(value string) bool {
	return value == "" || value == "ALL_DATA" || value == "OVER_ALL_DATA" || value == "UNI_DATA"
}
