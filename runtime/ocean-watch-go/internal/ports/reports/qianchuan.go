package reports

import (
	"context"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
)

type MaterialFilters struct {
	MaterialIDs  []string
	MaterialType string
	MaterialMode []string
	VideoSource  []string
}

type MaterialPageRequest struct {
	AdvertiserID string
	AccessToken  string
	StartDate    string
	EndDate      string
	Fields       []string
	Filters      MaterialFilters
	OrderField   string
	OrderType    string
	Page         int
	PageSize     int
}

type SchemaRequest struct {
	AdvertiserID string
	AccessToken  string
	Topics       []string
	DataPeriod   string
}

type PlanSchemaRequest struct {
	AdvertiserID string
	AccessToken  string
	Topic        string
}

type ReportFilter struct {
	Field    string
	Operator int64
	Values   []string
}

type DataPageRequest struct {
	AdvertiserID string
	AccessToken  string
	Topic        string
	Dimensions   []string
	Metrics      []string
	Filters      []ReportFilter
	StartTime    string
	EndTime      string
	OrderField   string
	OrderType    int64
	DataPeriod   string
	Page         int
	PageSize     int
}

type PlanMetricPageRequest struct {
	AdvertiserID string
	AccessToken  string
	Topic        string
	Dimensions   []string
	Metrics      []string
	StartTime    string
	EndTime      string
	OrderField   string
	OrderType    int64
	Page         int
	PageSize     int
}

type AggregateRequest struct {
	AdvertiserID  string
	AccessToken   string
	StartTime     string
	EndTime       string
	Fields        []string
	AdlabScene    string
	DataPeriod    string
	MarketingGoal string
	OrderPlatform string
}

type DimensionPageRequest struct {
	AdvertiserID  string
	AccessToken   string
	DimensionID   string
	StartTime     string
	EndTime       string
	Dimension     string
	Metrics       []string
	MarketingGoal string
	OrderPlatform string
	SmartBidType  string
	OrderField    string
	OrderType     string
	Page          int
	PageSize      int
}

type PlanMetadataPageRequest struct {
	AdvertiserID       string
	AccessToken        string
	StartTime          string
	EndTime            string
	Status             string
	MarketingGoal      string
	AdlabScene         string
	NeedCompensateInfo bool
	Page               int
	PageSize           int
}

type QianchuanReader interface {
	FetchMaterialPage(context.Context, MaterialPageRequest) (domainreports.MaterialPage, error)
	FetchPlanSchema(context.Context, PlanSchemaRequest) (domainreports.PlanSchema, error)
	FetchPlanMetricPage(context.Context, PlanMetricPageRequest) (domainreports.PlanMetricPage, error)
	FetchPlanMetadataPage(context.Context, PlanMetadataPageRequest) (domainqianchuan.PlanPage, error)
}

type QianchuanUnifiedReader interface {
	FetchSchemas(context.Context, SchemaRequest) ([]domainreports.QianchuanSchema, error)
	FetchDataPage(context.Context, DataPageRequest) (domainreports.QianchuanReportPage, error)
	FetchAllPromotion(context.Context, AggregateRequest) (domainreports.QianchuanAggregate, error)
	FetchUniPromotion(context.Context, AggregateRequest) (domainreports.QianchuanAggregate, error)
	FetchRoomDimensionPage(context.Context, DimensionPageRequest) (domainreports.QianchuanDimensionPage, error)
	FetchAuthorDimensionPage(context.Context, DimensionPageRequest) (domainreports.QianchuanDimensionPage, error)
}
