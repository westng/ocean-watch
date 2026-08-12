package materials

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	fixtureAdvertiserID = "1000000000000001"
	fixtureToken        = "TEST_MARKETING_ACCESS_TOKEN_DO_NOT_USE"
)

type marketingTokenSpy struct {
	queries []authapplication.TokenQuery
}

func (spy *marketingTokenSpy) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	spy.queries = append(spy.queries, query)
	return authapplication.TokenLease{
		Channel: "marketing", AuthorizationID: "fixture-authorization", AccessToken: fixtureToken,
	}, nil
}

type marketingReaderStub struct {
	videoRequests         []portmarketing.LibraryVideoRequest
	adVideoRequests       []portmarketing.AdVideoRequest
	coverRequests         []portmarketing.CoverSuggestionRequest
	authorizationRequests []portmarketing.CreatorAuthorizationRequest
	homepageRequests      []portmarketing.CreatorHomepageRequest
	imageRequests         []portmarketing.LibraryImageRequest
	adImageRequests       []portmarketing.AdImageRequest
	productRequests       []portmarketing.ProductRequest
	videos                func(portmarketing.LibraryVideoRequest) (domainmarketing.VideoPage, error)
	adVideos              func(portmarketing.AdVideoRequest) (domainmarketing.VideoBatch, error)
	covers                func(portmarketing.CoverSuggestionRequest) (domainmarketing.CoverSuggestion, error)
	authorizations        func(portmarketing.CreatorAuthorizationRequest) (domainmarketing.CreatorAuthorizationPage, error)
	homepage              func(portmarketing.CreatorHomepageRequest) (domainmarketing.CreatorHomepagePage, error)
	images                func(portmarketing.LibraryImageRequest) (domainmarketing.ImagePage, error)
	adImages              func(portmarketing.AdImageRequest) (domainmarketing.ImageBatch, error)
	products              func(portmarketing.ProductRequest) (domainmarketing.ProductPage, error)
}

func (stub *marketingReaderStub) FetchLibraryVideos(_ context.Context, request portmarketing.LibraryVideoRequest) (domainmarketing.VideoPage, error) {
	stub.videoRequests = append(stub.videoRequests, request)
	if stub.videos == nil {
		return domainmarketing.VideoPage{}, errors.New("unexpected library video request")
	}
	return stub.videos(request)
}

func (stub *marketingReaderStub) FetchAdVideos(_ context.Context, request portmarketing.AdVideoRequest) (domainmarketing.VideoBatch, error) {
	stub.adVideoRequests = append(stub.adVideoRequests, request)
	if stub.adVideos == nil {
		return domainmarketing.VideoBatch{}, errors.New("unexpected ad video request")
	}
	return stub.adVideos(request)
}

func (stub *marketingReaderStub) FetchCoverSuggestions(_ context.Context, request portmarketing.CoverSuggestionRequest) (domainmarketing.CoverSuggestion, error) {
	stub.coverRequests = append(stub.coverRequests, request)
	if stub.covers == nil {
		return domainmarketing.CoverSuggestion{}, errors.New("unexpected cover request")
	}
	return stub.covers(request)
}

func (stub *marketingReaderStub) FetchCreatorAuthorizations(_ context.Context, request portmarketing.CreatorAuthorizationRequest) (domainmarketing.CreatorAuthorizationPage, error) {
	stub.authorizationRequests = append(stub.authorizationRequests, request)
	if stub.authorizations == nil {
		return domainmarketing.CreatorAuthorizationPage{}, errors.New("unexpected creator authorization request")
	}
	return stub.authorizations(request)
}

func (stub *marketingReaderStub) FetchCreatorHomepage(_ context.Context, request portmarketing.CreatorHomepageRequest) (domainmarketing.CreatorHomepagePage, error) {
	stub.homepageRequests = append(stub.homepageRequests, request)
	if stub.homepage == nil {
		return domainmarketing.CreatorHomepagePage{}, errors.New("unexpected creator homepage request")
	}
	return stub.homepage(request)
}

func (stub *marketingReaderStub) FetchLibraryImages(_ context.Context, request portmarketing.LibraryImageRequest) (domainmarketing.ImagePage, error) {
	stub.imageRequests = append(stub.imageRequests, request)
	if stub.images == nil {
		return domainmarketing.ImagePage{}, errors.New("unexpected library image request")
	}
	return stub.images(request)
}

func (stub *marketingReaderStub) FetchAdImages(_ context.Context, request portmarketing.AdImageRequest) (domainmarketing.ImageBatch, error) {
	stub.adImageRequests = append(stub.adImageRequests, request)
	if stub.adImages == nil {
		return domainmarketing.ImageBatch{}, errors.New("unexpected ad image request")
	}
	return stub.adImages(request)
}

func (stub *marketingReaderStub) FetchProducts(_ context.Context, request portmarketing.ProductRequest) (domainmarketing.ProductPage, error) {
	stub.productRequests = append(stub.productRequests, request)
	if stub.products == nil {
		return domainmarketing.ProductPage{}, errors.New("unexpected product request")
	}
	return stub.products(request)
}

func TestMarketingMaterialValidationStopsBeforeTokenResolution(t *testing.T) {
	tests := []struct {
		name string
		run  func(Service) error
	}{
		{name: "mixed-video-filters", run: func(service Service) error {
			_, err := service.QueryVideos(context.Background(), VideoQuery{
				CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID},
				VideoIDs:        []string{"video-fixture"}, MaterialIDs: []string{"5000000000000001"},
			})
			return err
		}},
		{name: "homepage-without-exact-creator", run: func(service Service) error {
			_, err := service.QueryCreator(context.Background(), CreatorQuery{
				CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Source: "homepage",
			})
			return err
		}},
		{name: "mixed-image-filters", run: func(service Service) error {
			_, err := service.QueryImages(context.Background(), ImageQuery{
				CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Mode: "library-get",
				ImageIDs: []string{"image-fixture"}, MaterialIDs: []string{"5000000000000001"},
			})
			return err
		}},
		{name: "unapproved-product-path", run: func(service Service) error {
			_, err := service.QueryProducts(context.Background(), ProductQuery{
				CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Path: "/2/dpa/other/list/",
			})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := &marketingTokenSpy{}
			reader := &marketingReaderStub{}
			if err := test.run(Service{Tokens: tokens, Reader: reader}); err == nil {
				t.Fatal("invalid material query was accepted")
			}
			if len(tokens.queries) != 0 || reader.callCount() != 0 {
				t.Fatalf("invalid query crossed token or adapter boundary: tokens=%d calls=%d", len(tokens.queries), reader.callCount())
			}
		})
	}
}

func TestMarketingVideoPaginationAndDiagnostics(t *testing.T) {
	tokens := &marketingTokenSpy{}
	reader := &marketingReaderStub{videos: func(request portmarketing.LibraryVideoRequest) (domainmarketing.VideoPage, error) {
		return domainmarketing.VideoPage{
			Rows: []domainmarketing.VideoAsset{{
				ID: "video-" + string(rune('0'+request.Page)), MaterialID: "900719925474099" + string(rune('0'+request.Page)),
				Filename: "Fixture " + string(rune('0'+request.Page)),
			}},
			PageInfo:  domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 2, TotalNumber: 2},
			RequestID: "request-" + string(rune('0'+request.Page)), Message: "OK",
		}, nil
	}}
	result, err := (Service{Tokens: tokens, Reader: reader}).QueryVideos(context.Background(), VideoQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID, AuthAccountID: "9000000000000001"},
		Date:            "2026-07-25", PageSize: 1, FetchAll: true, Filename: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.videoRequests) != 2 || reader.videoRequests[0].Page != 1 || reader.videoRequests[1].Page != 2 {
		t.Fatalf("video page sequence = %#v", reader.videoRequests)
	}
	if result.Endpoint != LibraryVideoEndpoint || result.MatchedCount != 2 || len(result.SelectedVideos) != 2 ||
		result.SelectedVideos[1].MaterialID != "9007199254740992" ||
		!reflect.DeepEqual(result.RequestIDs, []string{"request-1", "request-2"}) {
		t.Fatalf("video result changed: %#v", result)
	}
	if !reflect.DeepEqual(tokens.queries, []authapplication.TokenQuery{{
		Channel: "marketing", AdvertiserID: fixtureAdvertiserID, AuthAccountID: "9000000000000001",
	}}) {
		t.Fatalf("token scope changed: %#v", tokens.queries)
	}
	if filtering, ok := result.Params["filtering"].(map[string]any); !ok || filtering["start_time"] != "2026-07-25" || filtering["end_time"] != "2026-07-25" {
		t.Fatalf("video date filtering changed: %#v", result.Params)
	}
}

func TestMarketingVideoExplicitDateBoundsOverrideShortcutIndependently(t *testing.T) {
	reader := &marketingReaderStub{videos: func(request portmarketing.LibraryVideoRequest) (domainmarketing.VideoPage, error) {
		if request.StartTime != "2026-07-20" || request.EndTime != "2026-07-25" {
			t.Fatalf("date precedence changed: %#v", request)
		}
		return domainmarketing.VideoPage{
			Rows: []domainmarketing.VideoAsset{},
			PageInfo: domainmarketing.PageInfo{
				Page: request.Page, PageSize: request.PageSize, TotalPages: 0, TotalNumber: 0,
			},
		}, nil
	}}
	_, err := (Service{
		Tokens: &marketingTokenSpy{}, Reader: reader,
		Now: func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) },
	}).QueryVideos(context.Background(), VideoQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID},
		Date:            "today", StartTime: "2026-07-20 00:00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarketingCoverPollingSettingsReachAdapterUnchanged(t *testing.T) {
	reader := &marketingReaderStub{covers: func(request portmarketing.CoverSuggestionRequest) (domainmarketing.CoverSuggestion, error) {
		if request.Attempts != 4 || request.Wait != 0 {
			t.Fatalf("cover polling settings changed at the application boundary: %#v", request)
		}
		return domainmarketing.CoverSuggestion{
			Status: "SUCCESS", Rows: []domainmarketing.CoverAsset{{ID: "cover-fixture"}},
		}, nil
	}}
	result, err := (Service{Tokens: &marketingTokenSpy{}, Reader: reader}).QueryVideos(
		context.Background(), VideoQuery{
			CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID},
			Mode:            "cover-suggest", VideoIDs: []string{"video-fixture"},
			CoverAttempts: 4, CoverWait: 0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedCoverID != "cover-fixture" || len(reader.coverRequests) != 1 {
		t.Fatalf("custom cover query changed: %#v requests=%#v", result, reader.coverRequests)
	}
}

func TestMarketingSinglePageQueriesFailClosedOnContradictoryTotals(t *testing.T) {
	reader := &marketingReaderStub{
		images: func(request portmarketing.LibraryImageRequest) (domainmarketing.ImagePage, error) {
			return domainmarketing.ImagePage{
				Rows:     []domainmarketing.ImageAsset{{ID: "image-fixture"}},
				PageInfo: domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 1, TotalNumber: 2},
			}, nil
		},
		products: func(request portmarketing.ProductRequest) (domainmarketing.ProductPage, error) {
			return domainmarketing.ProductPage{
				Rows:     []domainmarketing.DPAProduct{{ProductID: "3000000000000001"}},
				PageInfo: domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 1, TotalNumber: 2},
			}, nil
		},
	}
	service := Service{Tokens: &marketingTokenSpy{}, Reader: reader}
	if result, err := service.QueryImages(context.Background(), ImageQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Mode: "library-get",
	}); err == nil || result.Response.Data.List != nil {
		t.Fatalf("contradictory image page returned result=%#v err=%v", result, err)
	}
	if result, err := service.QueryProducts(context.Background(), ProductQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID},
	}); err == nil || result.Response.Data.List != nil {
		t.Fatalf("contradictory product page returned result=%#v err=%v", result, err)
	}
}

func TestMarketingCreatorAuthorizationFilteringAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 25, 8, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	reader := &marketingReaderStub{authorizations: func(request portmarketing.CreatorAuthorizationRequest) (domainmarketing.CreatorAuthorizationPage, error) {
		var rows []domainmarketing.CreatorAuthorization
		if request.Page == 1 {
			rows = []domainmarketing.CreatorAuthorization{
				creatorAuthorization("creator-a", "5000000000000001", "2026-07-30 08:00:00", "cover-a"),
				creatorAuthorization("creator-other", "5000000000000002", "2026-07-30 08:00:00", "cover-b"),
			}
		} else {
			rows = []domainmarketing.CreatorAuthorization{
				creatorAuthorization("creator-a", "5000000000000003", "2026-07-25 12:00:00", ""),
			}
		}
		return domainmarketing.CreatorAuthorizationPage{
			Rows:      rows,
			PageInfo:  domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 2, TotalNumber: 3},
			RequestID: "auth-request-" + string(rune('0'+request.Page)), Message: "OK",
		}, nil
	}}
	result, err := (Service{Tokens: &marketingTokenSpy{}, Reader: reader, Now: func() time.Time { return now }}).QueryCreator(
		context.Background(), CreatorQuery{
			CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Source: "authorized",
			AwemeIDs: []string{"creator-a"}, MinimumRemainingDays: 1, IncludeUnusable: true, PageSize: 2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.authorizationRequests) != 2 || result.PageCount != 2 || result.CandidateCount != 2 {
		t.Fatalf("creator pagination/filtering changed: requests=%#v result=%#v", reader.authorizationRequests, result)
	}
	if result.Candidates[0].AuthorizationStatus != "VALID" || !result.Candidates[0].Usable ||
		result.Candidates[0].SourceKey == nil || !strings.Contains(result.Candidates[0].SourceKey.Canonical, "5000000000000001") {
		t.Fatalf("usable authorized candidate changed: %#v", result.Candidates[0])
	}
	if result.Candidates[1].Usable || !reflect.DeepEqual(result.Candidates[1].UnusableReasons, []string{
		"missing_video_cover_id", "authorization_expires_too_soon",
	}) {
		t.Fatalf("unusable authorization reasons changed: %#v", result.Candidates[1])
	}
	for _, request := range reader.authorizationRequests {
		if !reflect.DeepEqual(request.AwemeIDs, []string{"creator-a"}) || request.AccessToken != fixtureToken {
			t.Fatalf("creator request changed: %#v", request)
		}
	}
}

func TestMarketingHomepageDoesNotClaimAuthorization(t *testing.T) {
	reader := &marketingReaderStub{homepage: func(request portmarketing.CreatorHomepageRequest) (domainmarketing.CreatorHomepagePage, error) {
		return domainmarketing.CreatorHomepagePage{
			Rows: []domainmarketing.CreatorVideo{{
				ItemID: "5000000000000001", VideoID: "video-homepage", VideoCoverID: "cover-homepage", Title: "Homepage fixture",
			}},
			PageInfo:  domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 1, TotalNumber: 1},
			RequestID: "homepage-request", Message: "OK",
		}, nil
	}}
	result, err := (Service{Tokens: &marketingTokenSpy{}, Reader: reader}).QueryCreator(context.Background(), CreatorQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Source: "homepage",
		AwemeIDs: []string{"creator-homepage"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceType != CreatorHomepageSource || len(result.Candidates) != 1 {
		t.Fatalf("homepage result changed: %#v", result)
	}
	candidate := result.Candidates[0]
	if candidate.AuthorizationStatus != "" || candidate.RawAuthorizationStatus != "" || candidate.AuthorizationType != "" ||
		candidate.SourceKey != nil || candidate.CreatorID != "creator-homepage" || !candidate.Usable {
		t.Fatalf("homepage candidate claimed authorization semantics: %#v", candidate)
	}
}

func TestMarketingProductKeepsCompatibilityOnlyPlatformID(t *testing.T) {
	reader := &marketingReaderStub{products: func(request portmarketing.ProductRequest) (domainmarketing.ProductPage, error) {
		return domainmarketing.ProductPage{
			Rows:      []domainmarketing.DPAProduct{{ProductID: request.ProductID, Name: request.Name}},
			PageInfo:  domainmarketing.PageInfo{Page: request.Page, PageSize: request.PageSize, TotalPages: 1, TotalNumber: 1},
			RequestID: "product-request", Message: "OK",
		}, nil
	}}
	result, err := (Service{Tokens: &marketingTokenSpy{}, Reader: reader}).QueryProducts(context.Background(), ProductQuery{
		CredentialScope: CredentialScope{AdvertiserID: fixtureAdvertiserID}, Path: ProductEndpoint,
		ProductPlatformID: "platform-compatibility-only", ProductID: "3000000000000001", Name: "Fixture product",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.productRequests) != 1 || result.Params["product_platform_id"] != "platform-compatibility-only" {
		t.Fatalf("product compatibility output changed: request=%#v result=%#v", reader.productRequests, result)
	}
	request := reader.productRequests[0]
	if request.ProductID != "3000000000000001" || request.Name != "Fixture product" {
		t.Fatalf("official product request changed: %#v", request)
	}
}

func creatorAuthorization(awemeID, itemID, endTime, coverID string) domainmarketing.CreatorAuthorization {
	return domainmarketing.CreatorAuthorization{
		AwemeID: awemeID, AwemeName: "Fixture creator", OpenID: "open-" + awemeID,
		AuthType: OfficialVideoItemAuthType, AuthStatus: OfficialAuthorizedStatus,
		StartTime: "2026-07-20 08:00:00", EndTime: endTime,
		Video: domainmarketing.CreatorVideo{
			ItemID: itemID, MaterialID: "9007199254740993", VideoID: "video-" + itemID,
			VideoCoverID: coverID, Title: "Fixture work",
		},
	}
}

func (stub *marketingReaderStub) callCount() int {
	return len(stub.videoRequests) + len(stub.adVideoRequests) + len(stub.coverRequests) +
		len(stub.authorizationRequests) + len(stub.homepageRequests) + len(stub.imageRequests) +
		len(stub.adImageRequests) + len(stub.productRequests)
}
