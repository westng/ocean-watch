package marketing

import (
	"context"

	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
)

type DiscoveryScope struct {
	AdvertiserID string
	AccessToken  string
}

type ProjectDiscoveryRequest struct {
	DiscoveryScope
	Fields        []string
	Name          string
	LandingType   string
	MarketingGoal string
	DeliveryMode  string
	Page          int
	PageSize      int
}

type PromotionDiscoveryRequest struct {
	DiscoveryScope
	Fields       []string
	Name         string
	ProjectID    string
	PromotionIDs []string
	Page         int
	PageSize     int
}

type DPADiscoveryRequest struct {
	DiscoveryScope
	Mode            string
	PlatformID      string
	UniqueProductID string
	Page            int
	PageSize        int
}

type EventDiscoveryRequest struct {
	DiscoveryScope
	AssetType string
	AssetIDs  []string
	Page      int
	PageSize  int
}

type DeepBidDiscoveryRequest struct {
	DiscoveryScope
	AssetID            string
	ExternalAction     string
	DeepExternalAction string
	DeliveryMode       string
	LandingType        string
	AdType             string
	MarketingGoal      string
	ProductSetting     string
	ValueOptimizedType string
}

type GoalDiscoveryRequest struct {
	DiscoveryScope
	LandingType   string
	AdType        string
	AssetType     string
	MarketingGoal string
	DeliveryMode  string
	DeliveryType  string
	AssetID       string
	IncludeAsset  bool
}

type AdminDiscoveryRequest struct {
	DiscoveryScope
	Codes []string
}

type DiscoveryReader interface {
	FetchProjects(context.Context, ProjectDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchPromotions(context.Context, PromotionDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchDPA(context.Context, DPADiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchEvents(context.Context, EventDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchDeepBids(context.Context, DeepBidDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchGoals(context.Context, GoalDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	FetchAdminInfo(context.Context, AdminDiscoveryRequest) (domainmarketing.AdminEnvelope, error)
}
