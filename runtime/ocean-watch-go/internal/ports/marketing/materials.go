package marketing

import (
	"context"
	"time"

	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
)

type LibraryVideoRequest struct {
	AdvertiserID string
	AccessToken  string
	VideoIDs     []string
	MaterialIDs  []string
	Signatures   []string
	StartTime    string
	EndTime      string
	Page         int
	PageSize     int
}

type AdVideoRequest struct {
	AdvertiserID string
	AccessToken  string
	VideoIDs     []string
}

type CoverSuggestionRequest struct {
	AdvertiserID string
	AccessToken  string
	VideoID      string
	Attempts     int
	Wait         time.Duration
}

type CreatorAuthorizationRequest struct {
	AdvertiserID string
	AccessToken  string
	AwemeIDs     []string
	ItemIDs      []string
	Page         int
	PageSize     int
}

type CreatorHomepageRequest struct {
	AdvertiserID string
	AccessToken  string
	AwemeID      string
	Page         int
	PageSize     int
}

type LibraryImageRequest struct {
	AdvertiserID string
	AccessToken  string
	ImageIDs     []string
	MaterialIDs  []string
	Page         int
	PageSize     int
}

type AdImageRequest struct {
	AdvertiserID string
	AccessToken  string
	ImageIDs     []string
}

type ProductRequest struct {
	AdvertiserID string
	AccessToken  string
	ProductID    string
	Name         string
	Page         int
	PageSize     int
}

type MaterialReader interface {
	FetchLibraryVideos(context.Context, LibraryVideoRequest) (domainmarketing.VideoPage, error)
	FetchAdVideos(context.Context, AdVideoRequest) (domainmarketing.VideoBatch, error)
	FetchCoverSuggestions(context.Context, CoverSuggestionRequest) (domainmarketing.CoverSuggestion, error)
	FetchCreatorAuthorizations(context.Context, CreatorAuthorizationRequest) (domainmarketing.CreatorAuthorizationPage, error)
	FetchCreatorHomepage(context.Context, CreatorHomepageRequest) (domainmarketing.CreatorHomepagePage, error)
	FetchLibraryImages(context.Context, LibraryImageRequest) (domainmarketing.ImagePage, error)
	FetchAdImages(context.Context, AdImageRequest) (domainmarketing.ImageBatch, error)
	FetchProducts(context.Context, ProductRequest) (domainmarketing.ProductPage, error)
}
