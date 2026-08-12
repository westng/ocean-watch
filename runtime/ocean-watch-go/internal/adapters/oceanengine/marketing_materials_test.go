package oceanengine

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	marketingFixtureAdvertiserID = "1000000000000001"
	marketingFixtureToken        = "TEST_MARKETING_ACCESS_TOKEN_DO_NOT_USE"
	marketingFixtureHighID       = "9007199254740993"
)

type marketingMaterialCall struct {
	Method string
	Host   string
	Path   string
	Query  url.Values
	Token  string
}

type marketingMaterialTransport struct {
	calls         []marketingMaterialCall
	videoAttempts int
	coverAttempts int
}

func (transport *marketingMaterialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	call := marketingMaterialCall{
		Method: request.Method, Host: request.URL.Host, Path: request.URL.Path,
		Query: request.URL.Query(), Token: request.Header.Get("Access-Token"),
	}
	transport.calls = append(transport.calls, call)
	body := `{"code":40400,"message":"unexpected synthetic route"}`
	switch request.URL.Path {
	case "/open_api/2/file/video/get/":
		transport.videoAttempts++
		if transport.videoAttempts == 1 {
			body = `{"code":40100,"message":"synthetic rate limit","request_id":"video-rate-limit"}`
		} else {
			body = `{"code":0,"message":"OK","request_id":"video-request","data":{"list":[{"id":"video-fixture","material_id":9007199254740993,"filename":"Fixture video","poster_url":"https://example.test/poster.jpg"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
		}
	case "/open_api/2/file/video/ad/get/":
		body = `{"code":0,"message":"OK","request_id":"ad-video-request","data":{"list":[{"id":"video-fixture","material_id":9007199254740993,"poster_url":"https://example.test/poster.jpg"}]}}`
	case "/open_api/2/tools/video_cover/suggest/":
		transport.coverAttempts++
		if transport.coverAttempts == 1 {
			body = `{"code":0,"message":"OK","request_id":"cover-running","data":{"status":"RUNNING","list":[]}}`
		} else {
			body = `{"code":0,"message":"OK","request_id":"cover-request","data":{"status":"SUCCESS","list":[{"id":"cover-fixture","url":"https://example.test/cover.jpg","width":1080,"height":1920}]}}`
		}
	case "/open_api/2/tools/aweme_auth_list/":
		body = `{"code":0,"message":"OK","request_id":"authorization-request","data":{"list":[{"aweme_id":"creator-fixture","aweme_name":"Fixture creator","open_id":"open-fixture","auth_type":"VIDEO_ITEM","auth_status":"AUTHRIZED","start_time":"2026-07-20 08:00:00","end_time":"2026-08-20 08:00:00","video_info":{"item_id":9007199254740993,"mid":9007199254740994,"video_id":"creator-video","video_cover_id":"creator-cover","image_mode":"CREATIVE_IMAGE_MODE_VIDEO_VERTICAL","title":"Fixture work"}}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
	case "/open_api/2/file/video/aweme/get/":
		body = `{"code":0,"message":"OK","request_id":"homepage-request","data":{"list":[{"item_id":9007199254740993,"mid":9007199254740994,"video_id":"homepage-video","video_cover_id":"homepage-cover","image_mode":"CREATIVE_IMAGE_MODE_VIDEO_VERTICAL","title":"Homepage work"}],"page_info":{"page":1,"page_size":100,"total_page":1,"total_number":1}}}`
	case "/open_api/2/file/image/get/":
		body = `{"code":0,"message":"OK","request_id":"image-request","data":{"list":[{"id":"image-fixture","material_id":9007199254740993,"filename":"Fixture image","url":"https://example.test/image.jpg"}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	case "/open_api/2/file/image/ad/get/":
		body = `{"code":0,"message":"OK","request_id":"ad-image-request","data":{"list":[{"id":"image-fixture","material_id":9007199254740993,"url":"https://example.test/image.jpg"}]}}`
	case "/open_api/2/dpa/clue_product/list/":
		body = `{"code":0,"message":"OK","request_id":"product-request","data":{"products":[{"product_id":9007199254740993,"outer_id":"outer-fixture","name":"Fixture product","images_url":[{"url":"https://example.test/product.jpg"}]}],"page_info":{"page":1,"page_size":20,"total_page":1,"total_number":1}}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
	}, nil
}

func TestMarketingGeneratedServiceContracts(t *testing.T) {
	transport := &marketingMaterialTransport{}
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := MarketingMaterialsAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
	}
	ctx := testRequestContext(t, "marketing")
	videoPage, err := adapter.FetchLibraryVideos(ctx, portmarketing.LibraryVideoRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		MaterialIDs: []string{marketingFixtureHighID}, StartTime: "2026-07-25", EndTime: "2026-07-25",
		Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	adVideos, err := adapter.FetchAdVideos(ctx, portmarketing.AdVideoRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		VideoIDs: []string{"video-fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	covers, err := adapter.FetchCoverSuggestions(ctx, portmarketing.CoverSuggestionRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken, VideoID: "video-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizations, err := adapter.FetchCreatorAuthorizations(ctx, portmarketing.CreatorAuthorizationRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		AwemeIDs: []string{"creator-fixture"}, ItemIDs: []string{marketingFixtureHighID}, Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	homepage, err := adapter.FetchCreatorHomepage(ctx, portmarketing.CreatorHomepageRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		AwemeID: "creator-fixture", Page: 1, PageSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	images, err := adapter.FetchLibraryImages(ctx, portmarketing.LibraryImageRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		MaterialIDs: []string{marketingFixtureHighID}, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	adImages, err := adapter.FetchAdImages(ctx, portmarketing.AdImageRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		ImageIDs: []string{"image-fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	products, err := adapter.FetchProducts(ctx, portmarketing.ProductRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		ProductID: marketingFixtureHighID, Name: "Fixture product", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	if videoPage.Rows[0].MaterialID != marketingFixtureHighID || adVideos.Rows[0].MaterialID != marketingFixtureHighID ||
		authorizations.Rows[0].Video.ItemID != marketingFixtureHighID ||
		authorizations.Rows[0].Video.MaterialID != "9007199254740994" ||
		homepage.Rows[0].ItemID != marketingFixtureHighID || images.Rows[0].MaterialID != marketingFixtureHighID ||
		adImages.Rows[0].MaterialID != marketingFixtureHighID || products.Rows[0].ProductID != marketingFixtureHighID {
		t.Fatalf("SDK mapping lost high ID precision: video=%#v ad=%#v auth=%#v homepage=%#v image=%#v adImage=%#v product=%#v",
			videoPage, adVideos, authorizations, homepage, images, adImages, products)
	}
	if transport.videoAttempts != 2 || transport.coverAttempts != 2 || covers.Status != "SUCCESS" || covers.Rows[0].ID != "cover-fixture" {
		t.Fatalf("bounded retry or cover polling changed: video=%d cover=%d result=%#v",
			transport.videoAttempts, transport.coverAttempts, covers)
	}

	wantPaths := []string{
		"/open_api/2/file/video/get/", "/open_api/2/file/video/get/",
		"/open_api/2/file/video/ad/get/",
		"/open_api/2/tools/video_cover/suggest/", "/open_api/2/tools/video_cover/suggest/",
		"/open_api/2/tools/aweme_auth_list/", "/open_api/2/file/video/aweme/get/",
		"/open_api/2/file/image/get/", "/open_api/2/file/image/ad/get/",
		"/open_api/2/dpa/clue_product/list/",
	}
	paths := make([]string, 0, len(transport.calls))
	for _, call := range transport.calls {
		paths = append(paths, call.Path)
		if call.Method != http.MethodGet || call.Host != BusinessHost || call.Token != marketingFixtureToken ||
			call.Query.Get("advertiser_id") != marketingFixtureAdvertiserID {
			t.Fatalf("generated Service transport contract changed: %#v", call)
		}
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("generated Service path sequence = %#v, want %#v", paths, wantPaths)
	}

	authCall := findMarketingMaterialCall(t, transport.calls, "/open_api/2/tools/aweme_auth_list/")
	filtering := authCall.Query.Get("filtering")
	for _, expected := range []string{"VIDEO_ITEM", "AUTHRIZED", "creator-fixture", marketingFixtureHighID} {
		if !strings.Contains(filtering, expected) {
			t.Fatalf("authorization filtering omitted %q: %s", expected, filtering)
		}
	}
	productCall := findMarketingMaterialCall(t, transport.calls, "/open_api/2/dpa/clue_product/list/")
	if strings.Contains(productCall.Query.Encode(), "product_platform_id") || !strings.Contains(productCall.Query.Get("filtering"), marketingFixtureHighID) {
		t.Fatalf("product request sent compatibility-only fields or omitted product ID: %#v", productCall.Query)
	}
}

func TestMarketingGeneratedServiceRejectsBusinessEnvelopeError(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"code":40001,"message":"synthetic business failure","request_id":"error-request","data":{"list":[]}}`
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
		}, nil
	})
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (MarketingMaterialsAdapter{Factory: factory, Retry: platformretry.Policy{Delays: []time.Duration{}}}).FetchAdVideos(
		testRequestContext(t, "marketing"), portmarketing.AdVideoRequest{
			AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken, VideoIDs: []string{"video-fixture"},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "40001") {
		t.Fatalf("HTTP 200 business error was not rejected: %v", err)
	}
}

func TestMarketingCoverPollingHonorsCustomAttemptsAndZeroWait(t *testing.T) {
	attempts := 0
	delays := []time.Duration{}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		body := `{"code":0,"message":"OK","request_id":"cover-running","data":{"status":"RUNNING","list":[]}}`
		if attempts == 4 {
			body = `{"code":0,"message":"OK","request_id":"cover-ready","data":{"status":"SUCCESS","list":[{"id":"cover-fixture"}]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request,
		}, nil
	})
	factory, err := NewClientFactory(FactoryOptions{
		TransportFactory: func(HostProfile) http.RoundTripper { return transport },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := (MarketingMaterialsAdapter{
		Factory: factory,
		Retry: platformretry.Policy{
			Delays: []time.Duration{time.Hour},
			Sleep: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			},
		},
	}).FetchCoverSuggestions(testRequestContext(t, "marketing"), portmarketing.CoverSuggestionRequest{
		AdvertiserID: marketingFixtureAdvertiserID, AccessToken: marketingFixtureToken,
		VideoID: "video-fixture", Attempts: 4, Wait: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 4 || !reflect.DeepEqual(delays, []time.Duration{0, 0, 0}) ||
		result.Status != "SUCCESS" || len(result.Rows) != 1 || result.Rows[0].ID != "cover-fixture" {
		t.Fatalf("custom cover polling changed: attempts=%d delays=%#v result=%#v", attempts, delays, result)
	}
}

func findMarketingMaterialCall(t *testing.T, calls []marketingMaterialCall, path string) marketingMaterialCall {
	t.Helper()
	for _, call := range calls {
		if call.Path == path {
			return call
		}
	}
	t.Fatalf("missing synthetic call for %s", path)
	return marketingMaterialCall{}
}
