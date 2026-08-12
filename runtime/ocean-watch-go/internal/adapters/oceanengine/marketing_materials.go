package oceanengine

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

type MarketingMaterialsAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

var errCoverGenerationRunning = errors.New("video cover generation is still running")

func (adapter MarketingMaterialsAdapter) FetchLibraryVideos(
	ctx context.Context,
	request portmarketing.LibraryVideoRequest,
) (domainmarketing.VideoPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.VideoPage{}, err
	}
	materialIDs, err := parseIDs(request.MaterialIDs, "material_id")
	if err != nil {
		return domainmarketing.VideoPage{}, err
	}
	filtering := models.FileVideoGetV2Filtering{
		VideoIds: request.VideoIDs, MaterialIds: materialIDs, Signatures: request.Signatures,
	}
	if request.StartTime != "" {
		filtering.StartTime = stringPointer(request.StartTime)
	}
	if request.EndTime != "" {
		filtering.EndTime = stringPointer(request.EndTime)
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.FileVideoGetV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.FileVideoGetV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardFileVideoGet(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.VideoPage{}, err
	}
	rows := make([]domainmarketing.VideoAsset, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapLibraryVideo(item))
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.VideoPage{}, err
	}
	return domainmarketing.VideoPage{Rows: rows, PageInfo: pageInfo,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchAdVideos(
	ctx context.Context,
	request portmarketing.AdVideoRequest,
) (domainmarketing.VideoBatch, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.VideoBatch{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.FileVideoAdGetV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.FileVideoAdGetV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				VideoIds(request.VideoIDs).Execute()
			if guardErr := guardFileVideoAdGet(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.VideoBatch{}, err
	}
	rows := make([]domainmarketing.VideoAsset, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapAdVideo(item))
	}
	return domainmarketing.VideoBatch{Rows: rows, RequestID: stringValue(result.RequestId),
		Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchCoverSuggestions(
	ctx context.Context,
	request portmarketing.CoverSuggestionRequest,
) (domainmarketing.CoverSuggestion, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.CoverSuggestion{}, err
	}
	var last *models.ToolsVideoCoverSuggestV2Response
	policy, classifier := coverReadPolicy(adapter.Retry, request)
	result, err := platformretry.Do(
		ctx, policy, classifier,
		func(ctx context.Context, _ int) (*models.ToolsVideoCoverSuggestV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.ToolsVideoCoverSuggestV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).VideoId(request.VideoID).Execute()
			if guardErr := guardVideoCoverSuggest(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			last = response
			if strings.EqualFold(stringValue(response.Data.Status), "RUNNING") {
				return nil, errCoverGenerationRunning
			}
			return response, nil
		},
	)
	if errors.Is(err, errCoverGenerationRunning) && last != nil {
		result, err = last, nil
	}
	if err != nil {
		return domainmarketing.CoverSuggestion{}, err
	}
	rows := make([]domainmarketing.CoverAsset, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		if item == nil {
			continue
		}
		rows = append(rows, domainmarketing.CoverAsset{
			ID: stringValue(item.Id), URL: stringValue(item.Url),
			Width: cloneInt64(item.Width), Height: cloneInt64(item.Height),
		})
	}
	return domainmarketing.CoverSuggestion{Status: stringValue(result.Data.Status), Rows: rows,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchCreatorAuthorizations(
	ctx context.Context,
	request portmarketing.CreatorAuthorizationRequest,
) (domainmarketing.CreatorAuthorizationPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.CreatorAuthorizationPage{}, err
	}
	itemIDs, err := parseIDs(request.ItemIDs, "item_id")
	if err != nil {
		return domainmarketing.CreatorAuthorizationPage{}, err
	}
	filtering := models.ToolsAwemeAuthListV2Filtering{
		AuthType: []*models.ToolsAwemeAuthListV2FilteringAuthType{
			models.VIDEO_ITEM_ToolsAwemeAuthListV2FilteringAuthType.Ptr(),
		},
		AuthStatus: []*models.ToolsAwemeAuthListV2FilteringAuthStatus{
			models.AUTHRIZED_ToolsAwemeAuthListV2FilteringAuthStatus.Ptr(),
		},
		AwemeIds: request.AwemeIDs, ItemIds: itemIDs,
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ToolsAwemeAuthListV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.ToolsAwemeAuthListV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardAwemeAuthorization(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.CreatorAuthorizationPage{}, err
	}
	rows := make([]domainmarketing.CreatorAuthorization, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapCreatorAuthorization(item))
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.CreatorAuthorizationPage{}, err
	}
	return domainmarketing.CreatorAuthorizationPage{Rows: rows, PageInfo: pageInfo,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchCreatorHomepage(
	ctx context.Context,
	request portmarketing.CreatorHomepageRequest,
) (domainmarketing.CreatorHomepagePage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.CreatorHomepagePage{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.FileVideoAwemeGetV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.FileVideoAwemeGetV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).AwemeId(request.AwemeID).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardFileVideoAweme(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.CreatorHomepagePage{}, err
	}
	rows := make([]domainmarketing.CreatorVideo, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapHomepageVideo(item))
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.CreatorHomepagePage{}, err
	}
	return domainmarketing.CreatorHomepagePage{Rows: rows, PageInfo: pageInfo,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchLibraryImages(
	ctx context.Context,
	request portmarketing.LibraryImageRequest,
) (domainmarketing.ImagePage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.ImagePage{}, err
	}
	materialIDs, err := parseIDs(request.MaterialIDs, "material_id")
	if err != nil {
		return domainmarketing.ImagePage{}, err
	}
	filtering := models.FileImageGetV2Filtering{ImageIds: request.ImageIDs, MaterialIds: materialIDs}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.FileImageGetV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.FileImageGetV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardFileImageGet(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.ImagePage{}, err
	}
	rows := make([]domainmarketing.ImageAsset, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapLibraryImage(item))
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.ImagePage{}, err
	}
	return domainmarketing.ImagePage{Rows: rows, PageInfo: pageInfo,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchAdImages(
	ctx context.Context,
	request portmarketing.AdImageRequest,
) (domainmarketing.ImageBatch, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.ImageBatch{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.FileImageAdGetV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.FileImageAdGetV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				ImageIds(request.ImageIDs).Execute()
			if guardErr := guardFileImageAdGet(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.ImageBatch{}, err
	}
	rows := make([]domainmarketing.ImageAsset, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		rows = append(rows, mapAdImage(item))
	}
	return domainmarketing.ImageBatch{Rows: rows, RequestID: stringValue(result.RequestId),
		Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) FetchProducts(
	ctx context.Context,
	request portmarketing.ProductRequest,
) (domainmarketing.ProductPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainmarketing.ProductPage{}, err
	}
	filtering := models.DpaClueProductListV2Filtering{}
	if request.ProductID != "" {
		productID, parseErr := parsePositiveID(request.ProductID, "product_id")
		if parseErr != nil {
			return domainmarketing.ProductPage{}, parseErr
		}
		filtering.ProductIds = []int64{productID}
	}
	if request.Name != "" {
		filtering.ProductName = stringPointer(request.Name)
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.DpaClueProductListV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.DpaClueProductListV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Filtering(filtering).Execute()
			if guardErr := guardDPAProducts(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.ProductPage{}, err
	}
	rows := make([]domainmarketing.DPAProduct, 0, len(result.Data.Products))
	for _, item := range result.Data.Products {
		rows = append(rows, mapDPAProduct(item))
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.ProductPage{}, err
	}
	return domainmarketing.ProductPage{Rows: rows, PageInfo: pageInfo,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message)}, nil
}

func (adapter MarketingMaterialsAdapter) client(advertiserID, accessToken string) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Ocean Engine client factory is required")
	}
	parsed, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, errors.New("Marketing material access token is required")
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	return client, parsed, err
}

func guardFileVideoGet(response *models.FileVideoGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardFileVideoAdGet(response *models.FileVideoAdGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardVideoCoverSuggest(response *models.ToolsVideoCoverSuggestV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardAwemeAuthorization(response *models.ToolsAwemeAuthListV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardFileVideoAweme(response *models.FileVideoAwemeGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardFileImageGet(response *models.FileImageGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardFileImageAdGet(response *models.FileImageAdGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardDPAProducts(response *models.DpaClueProductListV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func classifyCoverRead(err error) (bool, time.Duration) {
	if errors.Is(err, errCoverGenerationRunning) {
		return true, time.Second
	}
	return ClassifyReadError(err)
}

func coverReadPolicy(
	base platformretry.Policy,
	request portmarketing.CoverSuggestionRequest,
) (platformretry.Policy, platformretry.Classifier) {
	policy := readPolicy(base)
	classifier := platformretry.Classifier(classifyCoverRead)
	if request.Attempts <= 0 {
		return policy, classifier
	}
	policy.Delays = make([]time.Duration, max(0, request.Attempts-1))
	for index := range policy.Delays {
		policy.Delays[index] = request.Wait
	}
	policy.Jitter = nil
	classifier = func(err error) (bool, time.Duration) {
		if errors.Is(err, errCoverGenerationRunning) {
			return true, 0
		}
		return ClassifyReadError(err)
	}
	return policy, classifier
}

func mapLibraryVideo(item *models.FileVideoGetV2ResponseDataListInner) domainmarketing.VideoAsset {
	if item == nil {
		return domainmarketing.VideoAsset{}
	}
	return domainmarketing.VideoAsset{
		ID: stringValue(item.Id), MaterialID: optionalID(item.MaterialId), Filename: stringValue(item.Filename),
		CreateTime: stringValue(item.CreateTime), Width: cloneInt64(item.Width), Height: cloneInt64(item.Height),
		Duration: cloneFloat64(item.Duration), BitRate: float64FromInt64(item.BitRate), Format: stringValue(item.Format),
		Source: stringValue(item.Source), Signature: stringValue(item.Signature), PosterURL: stringValue(item.PosterUrl),
		URL: stringValue(item.Url), Size: cloneInt64(item.Size), Labels: cloneStrings(item.Labels),
		OrganizationTags: cloneStrings(item.OrganizationTags), StarAuthorID: stringValue(item.StarAuthorId),
	}
}

func mapAdVideo(item *models.FileVideoAdGetV2ResponseDataListInner) domainmarketing.VideoAsset {
	if item == nil {
		return domainmarketing.VideoAsset{}
	}
	return domainmarketing.VideoAsset{ID: stringValue(item.Id), MaterialID: optionalID(item.MaterialId),
		Width: cloneInt64(item.Width), Height: cloneInt64(item.Height), Duration: cloneFloat64(item.Duration),
		BitRate: cloneFloat64(item.BitRate), Format: stringValue(item.Format), Signature: stringValue(item.Signature),
		PosterURL: stringValue(item.PosterUrl), URL: stringValue(item.Url), Size: cloneInt64(item.Size)}
}

func mapCreatorAuthorization(item *models.ToolsAwemeAuthListV2ResponseDataListInner) domainmarketing.CreatorAuthorization {
	if item == nil {
		return domainmarketing.CreatorAuthorization{}
	}
	warnings := make([]string, 0, len(item.WarningTypes))
	for _, value := range item.WarningTypes {
		if value != nil {
			warnings = append(warnings, string(*value))
		}
	}
	row := domainmarketing.CreatorAuthorization{
		AwemeID: stringValue(item.AwemeId), AwemeName: stringValue(item.AwemeName), OpenID: stringValue(item.OpenId),
		AuthType: enumValue(item.AuthType), AuthStatus: enumValue(item.AuthStatus),
		StartTime: stringValue(item.StartTime), EndTime: stringValue(item.EndTime),
		AutoExpireDate: stringValue(item.AuthAutoExpireDate), WarningTypes: warnings,
		WarningContent: cloneStrings(item.WarningContent), HomepageVisibility: cloneBool(item.HasVideoHpVisibilityLimit),
	}
	if video := item.VideoInfo; video != nil {
		row.Video = domainmarketing.CreatorVideo{
			ItemID: positiveIDValue(video.ItemId), MaterialID: optionalID(video.Mid), VideoID: stringValue(video.VideoId),
			VideoCoverID: stringValue(video.VideoCoverId), VideoCoverURL: stringValue(video.VideoCoverUrl),
			ImageMode: enumValue(video.ImageMode), Title: stringValue(video.Title), Duration: cloneFloat64(video.Duration),
			PlayURL: stringValue(video.AwemePlayUrl),
		}
	}
	return row
}

func mapHomepageVideo(item *models.FileVideoAwemeGetV2ResponseDataListInner) domainmarketing.CreatorVideo {
	if item == nil {
		return domainmarketing.CreatorVideo{}
	}
	return domainmarketing.CreatorVideo{
		ItemID: optionalID(item.ItemId), MaterialID: optionalID(item.Mid), VideoID: stringValue(item.VideoId),
		VideoCoverID: stringValue(item.VideoCoverId), VideoCoverURL: stringValue(item.VideoCoverUrl),
		ImageMode: enumValue(item.ImageMode), Title: stringValue(item.Title), Duration: cloneFloat64(item.Duration),
		PlayURL: stringValue(item.AwemePlayUrl),
	}
}

func mapLibraryImage(item *models.FileImageGetV2ResponseDataListInner) domainmarketing.ImageAsset {
	if item == nil {
		return domainmarketing.ImageAsset{}
	}
	return domainmarketing.ImageAsset{ID: stringValue(item.Id), MaterialID: optionalID(item.MaterialId),
		Filename: stringValue(item.Filename), CreateTime: stringValue(item.CreateTime),
		Width: cloneInt64(item.Width), Height: cloneInt64(item.Height), Format: stringValue(item.Format),
		Signature: stringValue(item.Signature), URL: stringValue(item.Url), Size: cloneInt64(item.Size), AIGC: cloneBool(item.Aigc)}
}

func mapAdImage(item *models.FileImageAdGetV2ResponseDataListInner) domainmarketing.ImageAsset {
	if item == nil {
		return domainmarketing.ImageAsset{}
	}
	return domainmarketing.ImageAsset{ID: stringValue(item.Id), MaterialID: optionalID(item.MaterialId),
		Width: cloneInt64(item.Width), Height: cloneInt64(item.Height), Format: stringValue(item.Format),
		Signature: stringValue(item.Signature), URL: stringValue(item.Url), Size: cloneInt64(item.Size)}
}

func mapDPAProduct(item *models.DpaClueProductListV2ResponseDataProductsInner) domainmarketing.DPAProduct {
	if item == nil {
		return domainmarketing.DPAProduct{}
	}
	images := make([]domainmarketing.ProductImage, 0, len(item.ImagesUrl))
	for _, image := range item.ImagesUrl {
		if image != nil {
			images = append(images, domainmarketing.ProductImage{URL: stringValue(image.Url)})
		}
	}
	row := domainmarketing.DPAProduct{
		ProductID: optionalID(item.ProductId), OuterID: stringValue(item.OuterId), Name: stringValue(item.Name),
		Title: stringValue(item.Title), Description: stringValue(item.Description), Feature: stringValue(item.Feature),
		ImageURL: stringValue(item.ImageUrl), ImagesURL: images, VideoURL: stringValue(item.VideoUrl),
		Status: enumValue(item.Status), AuditStatus: enumValue(item.AuditStatus),
		CompletionStatus: enumValue(item.CompletionStatus), OnlineTime: stringValue(item.OnlineTime),
		OfflineTime: stringValue(item.OfflineTime), Bought: cloneInt64(item.Bought), Comments: cloneInt64(item.Comments),
		HasVideo: cloneBool(item.HasVideo), Tags: cloneStrings(item.Tags),
	}
	if category := item.Category; category != nil {
		row.Category = &domainmarketing.ProductCategory{
			FirstID: optionalID(category.FirstCategoryId), FirstName: stringValue(category.FirstCategoryName),
			SecondID: optionalID(category.SecondCategoryId), SecondName: stringValue(category.SecondCategoryName),
			ThirdID: optionalID(category.ThirdCategoryId), ThirdName: stringValue(category.ThirdCategoryName),
			FourthID: optionalID(category.FourthCategoryId), FourthName: stringValue(category.FourthCategoryName),
		}
	}
	return row
}

func mapMarketingPageInfo(expectedPage, expectedSize int, page, pageSize, totalPages, totalNumber *int64) (domainmarketing.PageInfo, error) {
	if page == nil || pageSize == nil || totalPages == nil || totalNumber == nil ||
		*page < 0 || *pageSize < 0 || *totalPages < 0 || *totalNumber < 0 ||
		*page > int64(maxInt()) || *pageSize > int64(maxInt()) || *totalPages > int64(maxInt()) || *totalNumber > int64(maxInt()) {
		return domainmarketing.PageInfo{}, errors.New("Marketing material response contains malformed page_info")
	}
	if int(*page) != expectedPage || int(*pageSize) != expectedSize {
		return domainmarketing.PageInfo{}, errors.New("Marketing material response page_info does not match the request")
	}
	return domainmarketing.PageInfo{Page: int(*page), PageSize: int(*pageSize),
		TotalPages: int(*totalPages), TotalNumber: int(*totalNumber)}, nil
}

func positiveIDValue(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func float64FromInt64(value *int64) *float64 {
	if value == nil {
		return nil
	}
	converted := float64(*value)
	return &converted
}
