package qianchuan

import (
	"context"

	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
)

type ProductPageRequest struct {
	AdvertiserID   string
	AccessToken    string
	ProductIDs     []string
	ProductName    string
	Tab            string
	AwemeID        string
	OnlyUnpromoted bool
	OrderField     string
	OrderType      string
	Platform       string
	Page           int
	PageSize       int
}

type PlanPageRequest struct {
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

type PlanDetailRequest struct {
	AdvertiserID string
	AccessToken  string
	AdID         string
}

type MaterialPageRequest struct {
	AdvertiserID   string
	AccessToken    string
	AdID           string
	MaterialType   string
	MaterialStatus string
	Page           int
	PageSize       int
}

type AuthorizedCreatorPageRequest struct {
	AdvertiserID  string
	AccessToken   string
	SearchKeyword string
	MarketingGoal string
	Scene         string
	Page          int
	PageSize      int
}

type CreatorVideoPageRequest struct {
	AdvertiserID string
	AccessToken  string
	AwemeID      string
	ProductID    string
	AwemeItemIDs []string
	Cursor       *int64
	Count        int
}

type Reader interface {
	FetchProducts(context.Context, ProductPageRequest) (domainqianchuan.ProductPage, error)
	FetchPlans(context.Context, PlanPageRequest) (domainqianchuan.PlanPage, error)
	FetchPlanDetail(context.Context, PlanDetailRequest) (domainqianchuan.PlanDetail, error)
	FetchPlanMaterials(context.Context, MaterialPageRequest) (domainqianchuan.MaterialPage, error)
	FetchAuthorizedCreators(context.Context, AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error)
	FetchCreatorVideos(context.Context, CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error)
}
