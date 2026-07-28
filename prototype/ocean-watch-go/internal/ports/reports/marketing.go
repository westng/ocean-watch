package reports

import (
	"context"

	domainreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/reports"
)

type MarketingSchemaRequest struct {
	AdvertiserID string
	AccessToken  string
	DataTopics   []string
}

type MarketingFilter struct {
	Field    string   `json:"field"`
	Type     int64    `json:"type"`
	Operator int64    `json:"operator"`
	Values   []string `json:"values"`
}

type MarketingReportPageRequest struct {
	AdvertiserID string
	AccessToken  string
	DataTopic    string
	Dimensions   []string
	Metrics      []string
	Filters      []MarketingFilter
	StartTime    string
	EndTime      string
	OrderField   string
	OrderType    string
	Page         int
	PageSize     int
}

type MarketingPromotionPageRequest struct {
	AdvertiserID string
	AccessToken  string
	ProjectID    string
	PromotionIDs []string
	Page         int
	PageSize     int
}

type MarketingReader interface {
	FetchSchema(context.Context, MarketingSchemaRequest) (domainreports.MarketingSchema, error)
	FetchReportPage(context.Context, MarketingReportPageRequest) (domainreports.MarketingReportPage, error)
	FetchPromotionPage(context.Context, MarketingPromotionPageRequest) (domainreports.MarketingPromotionPage, error)
}
