package oceanengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

type QianchuanReadAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

func (adapter QianchuanReadAdapter) FetchProducts(
	ctx context.Context,
	request portqianchuan.ProductPageRequest,
) (domainqianchuan.ProductPage, error) {
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	pageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	productIDs, err := parseIDs(request.ProductIDs, "product_id")
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	filtering := models.QianchuanUniPromotionProductGetV10Filtering{ProductIds: productIDs}
	if request.ProductName != "" {
		filtering.ProductName = stringPointer(request.ProductName)
	}
	if request.Tab != "" {
		value := models.QianchuanUniPromotionProductGetV10FilteringTab(request.Tab)
		filtering.Tab = &value
	}
	if request.OnlyUnpromoted {
		filtering.CreateRoi2LimitProduct = boolPointer(true)
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanUniPromotionProductGetV10Response, error) {
			builder := client.sdk.QianchuanUniPromotionProductGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				OrderField(models.QianchuanUniPromotionProductGetV10OrderField(request.OrderField)).
				OrderType(models.QianchuanUniPromotionProductGetV10OrderType(request.OrderType)).
				Page(page).PageSize(pageSize)
			if request.AwemeID != "" {
				awemeID, parseErr := parsePositiveID(request.AwemeID, "aweme_id")
				if parseErr != nil {
					return nil, parseErr
				}
				builder = builder.AwemeId(awemeID)
			}
			if request.Platform != "" {
				builder = builder.Platfrom(models.QianchuanUniPromotionProductGetV10Platfrom(request.Platform))
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardProductResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	rows := make([]domainqianchuan.Product, 0, len(result.Data.ProductList))
	for _, item := range result.Data.ProductList {
		row, mapErr := mapQianchuanProduct(item)
		if mapErr != nil {
			return domainqianchuan.ProductPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapInt64PageInfo(
		request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage,
		result.Data.PageInfo.TotalNumber,
	)
	if err != nil {
		return domainqianchuan.ProductPage{}, err
	}
	return domainqianchuan.ProductPage{Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId)}, nil
}

func (adapter QianchuanReadAdapter) FetchPlans(
	ctx context.Context,
	request portqianchuan.PlanPageRequest,
) (domainqianchuan.PlanPage, error) {
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainqianchuan.PlanPage{}, err
	}
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.PlanPage{}, err
	}
	status := models.QianchuanUniPromotionListV10FilteringStatus(request.Status)
	filtering := models.QianchuanUniPromotionListV10Filtering{Status: &status}
	fields := []*models.QianchuanUniPromotionListV10Fields{
		models.STAT_COST_QianchuanUniPromotionListV10Fields.Ptr(),
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanUniPromotionListV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionListV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				StartTime(request.StartTime).EndTime(request.EndTime).
				MarketingGoal(models.QianchuanUniPromotionListV10MarketingGoal(request.MarketingGoal)).
				Fields(fields).Filtering(filtering).NeedCompensateInfo(request.NeedCompensateInfo).
				OrderType(models.DESC_QianchuanUniPromotionListV10OrderType).
				OrderField(models.CREATE_TIME_QianchuanUniPromotionListV10OrderField).
				Page(page).PageSize(models.QianchuanUniPromotionListV10PageSize(request.PageSize)).
				AdlabScene(models.QianchuanUniPromotionListV10AdlabScene(request.AdlabScene)).Execute()
			if guardErr := guardPlanListResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.PlanPage{}, err
	}
	rows := make([]domainqianchuan.Plan, 0, len(result.Data.AdList))
	for _, item := range result.Data.AdList {
		row, mapErr := mapQianchuanPlan(item)
		if mapErr != nil {
			return domainqianchuan.PlanPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapPlanPageInfo(request.Page, result.Data.PageInfo)
	if err != nil {
		return domainqianchuan.PlanPage{}, err
	}
	return domainqianchuan.PlanPage{Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId)}, nil
}

func (adapter QianchuanReadAdapter) FetchPlanDetail(
	ctx context.Context,
	request portqianchuan.PlanDetailRequest,
) (domainqianchuan.PlanDetail, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.PlanDetail{}, err
	}
	adID, err := parsePositiveID(request.AdID, "ad_id")
	if err != nil {
		return domainqianchuan.PlanDetail{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanUniPromotionAdDetailV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdDetailV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).AdId(adID).Execute()
			if guardErr := guardPlanDetailResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.PlanDetail{}, err
	}
	return mapQianchuanPlanDetail(result.Data)
}

func (adapter QianchuanReadAdapter) FetchPlanMaterials(
	ctx context.Context,
	request portqianchuan.MaterialPageRequest,
) (domainqianchuan.MaterialPage, error) {
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainqianchuan.MaterialPage{}, err
	}
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.MaterialPage{}, err
	}
	adID, err := parsePositiveID(request.AdID, "ad_id")
	if err != nil {
		return domainqianchuan.MaterialPage{}, err
	}
	materialStatus := models.QianchuanUniPromotionAdMaterialGetV10FilteringMaterialStatus(request.MaterialStatus)
	filtering := models.QianchuanUniPromotionAdMaterialGetV10Filtering{
		MaterialType:   models.QianchuanUniPromotionAdMaterialGetV10FilteringMaterialType(request.MaterialType),
		MaterialStatus: &materialStatus,
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanUniPromotionAdMaterialGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdMaterialGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).AdId(adID).
				Filtering(filtering).
				Page(page).PageSize(models.QianchuanUniPromotionAdMaterialGetV10PageSize(request.PageSize)).Execute()
			if guardErr := guardPlanMaterialsResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.MaterialPage{}, err
	}
	rows := make([]domainqianchuan.PlanMaterial, 0, len(result.Data.AdMaterialInfos))
	for _, item := range result.Data.AdMaterialInfos {
		row, mapErr := mapQianchuanPlanMaterial(item)
		if mapErr != nil {
			return domainqianchuan.MaterialPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapMaterialPageInfo(request.Page, result.Data.PageInfo)
	if err != nil {
		return domainqianchuan.MaterialPage{}, err
	}
	return domainqianchuan.MaterialPage{Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId)}, nil
}

func (adapter QianchuanReadAdapter) FetchAuthorizedCreators(
	ctx context.Context,
	request portqianchuan.AuthorizedCreatorPageRequest,
) (domainqianchuan.AuthorizedCreatorPage, error) {
	client, advertiserID, err := adapter.clientFor(ProfileBusiness, request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.AuthorizedCreatorPage{}, err
	}
	marketingGoal := models.QianchuanUniAwemeAuthorizedGetV10FilteringMarketingGoal(request.MarketingGoal)
	scene := models.QianchuanUniAwemeAuthorizedGetV10FilteringScene(request.Scene)
	filtering := models.QianchuanUniAwemeAuthorizedGetV10Filtering{
		MarketingGoal: &marketingGoal, Scene: &scene,
	}
	if request.SearchKeyword != "" {
		filtering.SearchKeyWords = stringPointer(request.SearchKeyword)
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanUniAwemeAuthorizedGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanUniAwemeAuthorizedGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardAuthorizedCreatorsResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.AuthorizedCreatorPage{}, err
	}
	rows := make([]domainqianchuan.AuthorizedCreator, 0, len(result.Data.AwemeIdList))
	for _, item := range result.Data.AwemeIdList {
		row, mapErr := mapAuthorizedCreator(item)
		if mapErr != nil {
			return domainqianchuan.AuthorizedCreatorPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapAuthorizedCreatorPageInfo(request.Page, len(rows), result.Data.PageInfo)
	if err != nil {
		return domainqianchuan.AuthorizedCreatorPage{}, err
	}
	return domainqianchuan.AuthorizedCreatorPage{
		Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId),
	}, nil
}

func (adapter QianchuanReadAdapter) FetchCreatorVideos(
	ctx context.Context,
	request portqianchuan.CreatorVideoPageRequest,
) (domainqianchuan.CreatorVideoPage, error) {
	client, advertiserID, err := adapter.clientFor(ProfileQianchuanVideo, request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainqianchuan.CreatorVideoPage{}, err
	}
	awemeID, err := parsePositiveID(request.AwemeID, "aweme_id")
	if err != nil {
		return domainqianchuan.CreatorVideoPage{}, err
	}
	filtering := models.QianchuanFileVideoAwemeGetV10Filtering{}
	if strings.TrimSpace(request.ProductID) != "" {
		productID, parseErr := parsePositiveID(request.ProductID, "product_id")
		if parseErr != nil {
			return domainqianchuan.CreatorVideoPage{}, parseErr
		}
		filtering.ProductId = &productID
	}
	if len(request.AwemeItemIDs) > 0 {
		filtering.AwemeItemIds, err = parseUniqueQianchuanIDs(request.AwemeItemIDs, "aweme_item_id", 50)
		if err != nil {
			return domainqianchuan.CreatorVideoPage{}, err
		}
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanFileVideoAwemeGetV10Response, error) {
			builder := client.sdk.QianchuanFileVideoAwemeGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).AwemeId(awemeID).
				Filtering(filtering).Count(int64(request.Count))
			if request.Cursor != nil {
				builder = builder.Cursor(*request.Cursor)
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardCreatorVideosResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainqianchuan.CreatorVideoPage{}, err
	}
	rows := make([]domainqianchuan.CreatorVideo, 0, len(result.Data.VideoList))
	for _, item := range result.Data.VideoList {
		row, mapErr := mapCreatorVideo(item)
		if mapErr != nil {
			return domainqianchuan.CreatorVideoPage{}, mapErr
		}
		rows = append(rows, row)
	}
	hasMore, nextCursor, err := mapCreatorVideoPageInfo(result.Data.PageInfo)
	if err != nil {
		return domainqianchuan.CreatorVideoPage{}, err
	}
	return domainqianchuan.CreatorVideoPage{
		Rows: rows, NextCursor: nextCursor, HasMore: hasMore, RequestID: stringValue(result.RequestId),
	}, nil
}

func (adapter QianchuanReadAdapter) client(advertiserID, accessToken string) (*Client, int64, error) {
	return adapter.clientFor(ProfileBusiness, advertiserID, accessToken)
}

func (adapter QianchuanReadAdapter) clientFor(
	profile HostProfile,
	advertiserID string,
	accessToken string,
) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Qianchuan SDK client factory is required")
	}
	parsed, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, errors.New("Qianchuan access token is required")
	}
	client, err := adapter.Factory.Client("qianchuan", profile, TimeoutStandard)
	return client, parsed, err
}

func guardProductResponse(response *models.QianchuanUniPromotionProductGetV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func guardPlanListResponse(response *models.QianchuanUniPromotionListV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func guardPlanDetailResponse(response *models.QianchuanUniPromotionAdDetailV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardPlanMaterialsResponse(response *models.QianchuanUniPromotionAdMaterialGetV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func guardAuthorizedCreatorsResponse(response *models.QianchuanUniAwemeAuthorizedGetV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardCreatorVideosResponse(response *models.QianchuanFileVideoAwemeGetV10Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func mapQianchuanProduct(item *models.QianchuanUniPromotionProductGetV10ResponseDataProductListInner) (domainqianchuan.Product, error) {
	if item == nil || item.Id == nil || *item.Id <= 0 {
		return domainqianchuan.Product{}, errors.New("Qianchuan product response contains an invalid product ID")
	}
	images := make([]domainqianchuan.ProductImage, 0, len(item.SquareImageList))
	for _, image := range item.SquareImageList {
		if image == nil {
			return domainqianchuan.Product{}, errors.New("Qianchuan product response contains a null square image")
		}
		images = append(images, domainqianchuan.ProductImage{URL: stringValue(image.ImgUrl)})
	}
	return domainqianchuan.Product{
		ProductID: strconv.FormatInt(*item.Id, 10), Name: stringValue(item.Name), Image: stringValue(item.Img),
		CategoryName: stringValue(item.CategoryName), ChannelID: optionalID(item.ChannelId),
		ChannelType: enumValue(item.ChannelType), SellNumber: item.SellNum, StockNumber: item.StockNum,
		AuditTime: stringValue(item.AuditTime), SquareImages: images,
		Tags: cloneStrings(item.Tag), GrayReasons: cloneStrings(item.GrayReason),
	}, nil
}

func mapQianchuanPlan(item *models.QianchuanUniPromotionListV10ResponseDataAdListInner) (domainqianchuan.Plan, error) {
	if item == nil || item.AdInfo == nil || item.AdInfo.Id == nil || *item.AdInfo.Id <= 0 {
		return domainqianchuan.Plan{}, errors.New("Qianchuan plan response contains an invalid plan ID")
	}
	info := item.AdInfo
	budget, err := optionalDecimal(info.Budget)
	if err != nil {
		return domainqianchuan.Plan{}, errors.New("Qianchuan plan response contains an invalid budget")
	}
	roi, err := optionalDecimal(info.Roi2Goal)
	if err != nil {
		return domainqianchuan.Plan{}, errors.New("Qianchuan plan response contains an invalid ROI goal")
	}
	creators := make([]domainqianchuan.Creator, 0, len(item.RoomInfo))
	for _, room := range item.RoomInfo {
		if room == nil {
			return domainqianchuan.Plan{}, errors.New("Qianchuan plan response contains a null creator")
		}
		creators = append(creators, domainqianchuan.Creator{
			VisibleID: stringValue(room.AnchorId), Name: stringValue(room.AnchorName), Avatar: stringValue(room.AnchorAvatar),
		})
	}
	products := make([]domainqianchuan.PlanProduct, 0, len(item.ProductInfo))
	for _, product := range item.ProductInfo {
		if product == nil {
			return domainqianchuan.Plan{}, errors.New("Qianchuan plan response contains a null product")
		}
		products = append(products, domainqianchuan.PlanProduct{
			ProductID: optionalID(product.ProductId), ProductName: stringValue(product.ProductName),
			ProductImage: stringValue(product.ProductImage), Reasons: cloneStrings(product.RecommendReasons),
		})
	}
	var guarantee *domainqianchuan.CostGuarantee
	if info.CompensateInfo != nil {
		guarantee = &domainqianchuan.CostGuarantee{Status: enumValue(info.CompensateInfo.Status),
			CompensateStatus: enumValue(info.CompensateInfo.CompensateStatus), Reason: stringValue(info.CompensateInfo.Reason)}
	}
	return domainqianchuan.Plan{
		AdID: strconv.FormatInt(*info.Id, 10), Name: stringValue(info.Name), Status: enumValue(info.Status),
		OptStatus: enumValue(info.OptStatus), CreateTime: stringValue(info.CreateTime), ModifyTime: stringValue(info.ModifyTime),
		StartTime: stringValue(info.StartTime), EndTime: stringValue(info.EndTime), MarketingGoal: enumValue(info.MarketingGoal),
		AdlabScene: enumValue(info.AdlabScene), Creators: creators, Products: products, Budget: budget,
		BudgetMode: enumValue(info.BudgetMode), SmartBidType: enumValue(info.SmartBidType), ROI2Goal: roi, Guarantee: guarantee,
	}, nil
}

func mapQianchuanPlanDetail(item *models.QianchuanUniPromotionAdDetailV10ResponseData) (domainqianchuan.PlanDetail, error) {
	if item == nil || item.AdId == nil || *item.AdId <= 0 {
		return domainqianchuan.PlanDetail{}, errors.New("Qianchuan plan detail contains an invalid plan ID")
	}
	creators := make([]domainqianchuan.Creator, 0, len(item.RoomInfo))
	for _, room := range item.RoomInfo {
		if room == nil {
			return domainqianchuan.PlanDetail{}, errors.New("Qianchuan plan detail contains a null creator")
		}
		creators = append(creators, domainqianchuan.Creator{AwemeID: optionalID(room.AnchorId),
			Name: stringValue(room.AnchorName), Avatar: stringValue(room.AnchorAvatar)})
	}
	products := make([]domainqianchuan.PlanProduct, 0, len(item.ProductInfos))
	for _, product := range item.ProductInfos {
		if product == nil {
			return domainqianchuan.PlanDetail{}, errors.New("Qianchuan plan detail contains a null product")
		}
		products = append(products, domainqianchuan.PlanProduct{ProductID: optionalID(product.ProductId),
			ChannelID: optionalID(product.ChannelId), ChannelType: enumValue(product.ChannelType)})
	}
	var budget, roi *domain.Decimal
	budgetMode, smartBidType := "", ""
	if item.DeliverySetting != nil {
		var err error
		budget, err = optionalDecimal(item.DeliverySetting.Budget)
		if err != nil {
			return domainqianchuan.PlanDetail{}, errors.New("Qianchuan plan detail contains an invalid budget")
		}
		roi, err = optionalDecimal(item.DeliverySetting.Roi2Goal)
		if err != nil {
			return domainqianchuan.PlanDetail{}, errors.New("Qianchuan plan detail contains an invalid ROI goal")
		}
		budgetMode = enumValue(item.DeliverySetting.BudgetMode)
		smartBidType = enumValue(item.DeliverySetting.SmartBidType)
	}
	return domainqianchuan.PlanDetail{
		AdID: strconv.FormatInt(*item.AdId, 10), Name: stringValue(item.Name), Status: enumValue(item.Status),
		OptStatus: enumValue(item.OptStatus), CreateTime: stringValue(item.CreateTime), ModifyTime: stringValue(item.ModifyTime),
		MarketingGoal: enumValue(item.MarketingGoal), AwemeID: optionalID(item.AwemeId), Creators: creators,
		Products: products, Budget: budget, BudgetMode: budgetMode, SmartBidType: smartBidType, ROI2Goal: roi,
	}, nil
}

func mapQianchuanPlanMaterial(item *models.QianchuanUniPromotionAdMaterialGetV10ResponseDataAdMaterialInfosInner) (domainqianchuan.PlanMaterial, error) {
	if item == nil || item.MaterialInfo == nil || item.MaterialInfo.VideoMaterial == nil {
		return domainqianchuan.PlanMaterial{}, errors.New("Qianchuan plan material response contains an invalid video material")
	}
	video := item.MaterialInfo.VideoMaterial
	row := domainqianchuan.PlanMaterial{
		MaterialID: optionalID(video.MaterialId), AwemeItemID: optionalID(video.AwemeItemId), VideoID: stringValue(video.VideoId),
		Title: stringValue(video.Title), URL: stringValue(video.Url), MaterialType: enumValue(item.MaterialInfo.MaterialType),
		MaterialSelectType: enumValue(item.MaterialSelectType), MaterialStatus: enumValue(item.MaterialStatus),
		AuditStatus: enumValue(item.AuditStatus), Source: enumValue(video.Source), Duration: video.VideoDuration,
		Deleted: item.IsDelete, DeliveryReasons: cloneStrings(item.DeliveryNotReason),
	}
	var err error
	row.AwemeIDs, err = positiveIDStrings(item.AwemeIdList, "material aweme_id")
	if err != nil {
		return domainqianchuan.PlanMaterial{}, err
	}
	row.ProductIDs, err = positiveIDStrings(item.ProductIdList, "material product_id")
	if err != nil {
		return domainqianchuan.PlanMaterial{}, err
	}
	if row.MaterialID == "" && row.AwemeItemID == "" && row.VideoID == "" {
		return domainqianchuan.PlanMaterial{}, errors.New("Qianchuan plan material response contains no stable material identity")
	}
	return row, nil
}

func mapAuthorizedCreator(
	item *models.QianchuanUniAwemeAuthorizedGetV10ResponseDataAwemeIdListInner,
) (domainqianchuan.AuthorizedCreator, error) {
	if item == nil || item.AwemeId == nil || *item.AwemeId <= 0 {
		return domainqianchuan.AuthorizedCreator{}, errors.New("Qianchuan authorized creator response contains an invalid aweme_id")
	}
	authTypes := make([]string, 0, len(item.AuthType))
	for _, value := range item.AuthType {
		if value == nil || strings.TrimSpace(string(*value)) == "" {
			return domainqianchuan.AuthorizedCreator{}, errors.New("Qianchuan authorized creator response contains an invalid auth_type")
		}
		authTypes = append(authTypes, string(*value))
	}
	disableReasons := make([]string, 0, len(item.ProductDisableReasons))
	for _, value := range item.ProductDisableReasons {
		if value == nil || strings.TrimSpace(string(*value)) == "" {
			return domainqianchuan.AuthorizedCreator{}, errors.New("Qianchuan authorized creator response contains an invalid product disable reason")
		}
		disableReasons = append(disableReasons, string(*value))
	}
	return domainqianchuan.AuthorizedCreator{
		AwemeID: strconv.FormatInt(*item.AwemeId, 10), VisibleID: strings.TrimSpace(stringValue(item.AwemeShowId)),
		Name: strings.TrimSpace(stringValue(item.AwemeName)), Avatar: stringValue(item.AwemeAvatar),
		AuthTypes: authTypes, HasAuthorized: item.HasAuthorized,
		ProductPromotionDisabled: item.IsProductUniPromDisabled, ProductDisableReasons: disableReasons,
		ProductPromotionApply: enumValue(item.ProductUniPromApplyType),
		CanControlPromotion:   item.CanControlUniprom, CanApplyPromotion: item.CanApplyUniprom,
		HasShopPermission: item.HasShopPermission, HasLivePermission: item.HasLivePermission,
	}, nil
}

func mapCreatorVideo(
	item *models.QianchuanFileVideoAwemeGetV10ResponseDataVideoListInner,
) (domainqianchuan.CreatorVideo, error) {
	if item == nil {
		return domainqianchuan.CreatorVideo{}, errors.New("Qianchuan creator video response contains a null row")
	}
	row := domainqianchuan.CreatorVideo{
		AwemeItemID: optionalID(item.AwemeItemId), ImageMode: enumValue(item.ImageMode),
		VideoID: stringValue(item.VideoId), MaterialID: optionalID(item.MaterialId),
		Title: stringValue(item.Title), VideoCoverURL: stringValue(item.VideoCoverUrl), URL: stringValue(item.Url),
		Width: item.Width, Height: item.Height, Duration: item.Duration,
		ViewCount: item.ViewCnt, LikeCount: item.LikeCnt, ShareCount: item.ShareCnt,
		CommentCount: item.CommentCnt, IsAICreated: item.IsAiCreate,
	}
	if item.IsRecommend != nil {
		value := int64(*item.IsRecommend)
		if value != 0 && value != 1 {
			return domainqianchuan.CreatorVideo{}, errors.New("Qianchuan creator video response contains an invalid is_recommend value")
		}
		row.IsRecommend = &value
	}
	if row.AwemeItemID == "" && row.VideoID == "" && row.MaterialID == "" && row.URL == "" {
		return domainqianchuan.CreatorVideo{}, errors.New("Qianchuan creator video response contains no stable material identity")
	}
	return row, nil
}

func mapInt64PageInfo(expected int, page, totalPages, totalNumber *int64) (domainqianchuan.PageInfo, error) {
	if page == nil || totalPages == nil || totalNumber == nil || *page < 0 || *totalPages < 0 || *totalNumber < 0 {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan response contains malformed page_info")
	}
	if *page > int64(maxInt()) || *totalPages > int64(maxInt()) || *totalNumber > int64(maxInt()) {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan response page_info exceeds platform integer range")
	}
	if int(*page) != expected {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan response page_info does not match the requested page")
	}
	return domainqianchuan.PageInfo{Page: int(*page), TotalPages: int(*totalPages), TotalNumber: int(*totalNumber)}, nil
}

func mapPlanPageInfo(expected int, info *models.QianchuanUniPromotionListV10ResponseDataPageInfo) (domainqianchuan.PageInfo, error) {
	if info == nil || info.TotalPage == nil || info.TotalNum == nil {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan plan response contains malformed page_info")
	}
	page := info.Page
	return mapInt64PageInfo(expected, &page, info.TotalPage, info.TotalNum)
}

func mapMaterialPageInfo(expected int, info *models.QianchuanUniPromotionAdMaterialGetV10ResponseDataPageInfo) (domainqianchuan.PageInfo, error) {
	if info == nil || info.Page == nil || info.TotalPage == nil || info.TotalNumber == nil || *info.Page < 0 || *info.TotalPage < 0 || *info.TotalNumber < 0 {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan material response contains malformed page_info")
	}
	if int64(*info.Page) > int64(maxInt()) || int64(*info.TotalPage) > int64(maxInt()) || *info.TotalNumber > int64(maxInt()) {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan material page_info exceeds platform integer range")
	}
	if int(*info.Page) != expected {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan material page_info does not match the requested page")
	}
	return domainqianchuan.PageInfo{Page: int(*info.Page), TotalPages: int(*info.TotalPage), TotalNumber: int(*info.TotalNumber)}, nil
}

func mapAuthorizedCreatorPageInfo(
	expected int,
	rowCount int,
	info models.QianchuanUniAwemeAuthorizedGetV10ResponseDataPageInfo,
) (domainqianchuan.PageInfo, error) {
	if info.TotalPage == nil || *info.TotalPage < 0 || info.Page < 0 {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan authorized creator response contains malformed page_info")
	}
	if *info.TotalPage == 0 {
		if expected != 1 || rowCount != 0 {
			return domainqianchuan.PageInfo{}, errors.New("Qianchuan authorized creator response contains contradictory empty page_info")
		}
		return domainqianchuan.PageInfo{Page: 1, TotalPages: 0, TotalNumber: 0}, nil
	}
	if info.Page != int64(expected) {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan authorized creator response page does not match the requested page")
	}
	totalNumber := int64(rowCount)
	if info.TotalNumber != nil {
		totalNumber = *info.TotalNumber
	} else if info.TotolNumer != nil {
		totalNumber = *info.TotolNumer
	} else if *info.TotalPage > 1 {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan authorized creator response is missing total_number")
	}
	if totalNumber < 0 || info.Page > int64(maxInt()) || *info.TotalPage > int64(maxInt()) || totalNumber > int64(maxInt()) {
		return domainqianchuan.PageInfo{}, errors.New("Qianchuan authorized creator page_info exceeds platform integer range")
	}
	return domainqianchuan.PageInfo{
		Page: int(info.Page), TotalPages: int(*info.TotalPage), TotalNumber: int(totalNumber),
	}, nil
}

func mapCreatorVideoPageInfo(
	info *models.QianchuanFileVideoAwemeGetV10ResponseDataPageInfo,
) (bool, *int64, error) {
	if info == nil || info.HasMore == nil {
		return false, nil, errors.New("Qianchuan creator video response contains malformed page_info")
	}
	hasMoreValue := int64(*info.HasMore)
	if hasMoreValue != 0 && hasMoreValue != 1 {
		return false, nil, errors.New("Qianchuan creator video response contains an invalid has_more value")
	}
	hasMore := hasMoreValue == 1
	if hasMore && (info.Cursor == nil || *info.Cursor < 0) {
		return false, nil, errors.New("Qianchuan creator video response is missing a valid next cursor")
	}
	if info.Cursor != nil && *info.Cursor < 0 {
		return false, nil, errors.New("Qianchuan creator video response contains a negative cursor")
	}
	return hasMore, info.Cursor, nil
}

func optionalDecimal(value *float64) (*domain.Decimal, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := domain.DecimalFromFloat64(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseIDs(values []string, field string) ([]int64, error) {
	result := make([]int64, 0, len(values))
	for index, value := range values {
		parsed, err := parsePositiveID(value, fmt.Sprintf("%s[%d]", field, index))
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func positiveIDStrings(values []int64, field string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("%s must be positive", field)
		}
		result = append(result, strconv.FormatInt(value, 10))
	}
	return result, nil
}

func optionalID(value *int64) string {
	if value == nil || *value <= 0 {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func enumValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func cloneStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string(nil), values...)
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func maxInt() int                        { return int(^uint(0) >> 1) }
