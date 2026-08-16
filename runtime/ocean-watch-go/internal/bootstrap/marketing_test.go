package bootstrap

import (
	"context"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

func TestNewMarketingRuntimeSharesInjectedTokenProvider(t *testing.T) {
	tokens := bootstrapTokens{}
	materialReader := bootstrapMarketingMaterialReader{}
	reportReader := bootstrapMarketingReportReader{}
	runtime, err := NewMarketingRuntime(MarketingOptions{
		StateRoot: t.TempDir(), CredentialStore: bootstrapCredentials{}, Tokens: tokens,
		MaterialReader: materialReader, ReportReader: reportReader,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Auth.Credentials == nil || runtime.Auth.Authorizations == nil ||
		runtime.Materials.Tokens == nil || runtime.Materials.Reader == nil ||
		runtime.Reports.Tokens == nil || runtime.Reports.Reader == nil {
		t.Fatalf("shared Marketing runtime was not fully assembled: %#v", runtime)
	}
	if runtime.Materials.Tokens != runtime.Reports.Tokens {
		t.Fatal("Marketing services did not share one token provider")
	}
}

type bootstrapMarketingMaterialReader struct{}

func (bootstrapMarketingMaterialReader) FetchLibraryVideos(context.Context, portmarketing.LibraryVideoRequest) (domainmarketing.VideoPage, error) {
	return domainmarketing.VideoPage{}, nil
}
func (bootstrapMarketingMaterialReader) FetchAdVideos(context.Context, portmarketing.AdVideoRequest) (domainmarketing.VideoBatch, error) {
	return domainmarketing.VideoBatch{}, nil
}
func (bootstrapMarketingMaterialReader) FetchCoverSuggestions(context.Context, portmarketing.CoverSuggestionRequest) (domainmarketing.CoverSuggestion, error) {
	return domainmarketing.CoverSuggestion{}, nil
}
func (bootstrapMarketingMaterialReader) FetchCreatorAuthorizations(context.Context, portmarketing.CreatorAuthorizationRequest) (domainmarketing.CreatorAuthorizationPage, error) {
	return domainmarketing.CreatorAuthorizationPage{}, nil
}
func (bootstrapMarketingMaterialReader) FetchCreatorHomepage(context.Context, portmarketing.CreatorHomepageRequest) (domainmarketing.CreatorHomepagePage, error) {
	return domainmarketing.CreatorHomepagePage{}, nil
}
func (bootstrapMarketingMaterialReader) FetchLibraryImages(context.Context, portmarketing.LibraryImageRequest) (domainmarketing.ImagePage, error) {
	return domainmarketing.ImagePage{}, nil
}
func (bootstrapMarketingMaterialReader) FetchAdImages(context.Context, portmarketing.AdImageRequest) (domainmarketing.ImageBatch, error) {
	return domainmarketing.ImageBatch{}, nil
}
func (bootstrapMarketingMaterialReader) FetchProducts(context.Context, portmarketing.ProductRequest) (domainmarketing.ProductPage, error) {
	return domainmarketing.ProductPage{}, nil
}

type bootstrapMarketingReportReader struct{}

func (bootstrapMarketingReportReader) FetchSchema(context.Context, portreports.MarketingSchemaRequest) (domainreports.MarketingSchema, error) {
	return domainreports.MarketingSchema{}, nil
}
func (bootstrapMarketingReportReader) FetchReportPage(context.Context, portreports.MarketingReportPageRequest) (domainreports.MarketingReportPage, error) {
	return domainreports.MarketingReportPage{}, nil
}
func (bootstrapMarketingReportReader) FetchPromotionPage(context.Context, portreports.MarketingPromotionPageRequest) (domainreports.MarketingPromotionPage, error) {
	return domainreports.MarketingPromotionPage{}, nil
}

var _ auth.TokenProvider = bootstrapTokens{}
