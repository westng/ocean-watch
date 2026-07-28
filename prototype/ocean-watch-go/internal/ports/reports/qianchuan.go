package reports

import (
	"context"

	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/reports"
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

type PlanSchemaRequest struct {
	AdvertiserID string
	AccessToken  string
	Topic        string
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
