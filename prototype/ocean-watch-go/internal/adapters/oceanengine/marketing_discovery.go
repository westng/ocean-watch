package oceanengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

type MarketingDiscoveryAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

func (adapter MarketingDiscoveryAdapter) FetchProjects(
	ctx context.Context,
	request portmarketing.ProjectDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	filtering := models.ProjectListV30Filtering{}
	if request.Name != "" {
		filtering.Name = stringPointer(request.Name)
	}
	if request.LandingType != "" {
		value := models.ProjectListV30FilteringLandingType(request.LandingType)
		filtering.LandingType = &value
	}
	if request.MarketingGoal != "" {
		value := models.ProjectListV30FilteringMarketingGoal(request.MarketingGoal)
		filtering.MarketingGoal = &value
	}
	if request.DeliveryMode != "" {
		value := models.ProjectListV30FilteringDeliveryMode(request.DeliveryMode)
		filtering.DeliveryMode = &value
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ProjectListV30Response, error) {
			response, httpResponse, sdkErr := client.sdk.ProjectListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Fields(request.Fields).
				Filtering(filtering).Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardProjectDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, &pageInfo)
}

func (adapter MarketingDiscoveryAdapter) FetchPromotions(
	ctx context.Context,
	request portmarketing.PromotionDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	filtering := models.PromotionListV30Filtering{}
	if request.Name != "" {
		filtering.Name = stringPointer(request.Name)
	}
	if request.ProjectID != "" {
		value, parseErr := parsePositiveID(request.ProjectID, "project_id")
		if parseErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, parseErr
		}
		filtering.ProjectId = &value
	}
	filtering.Ids, err = parseDiscoveryIDs(request.PromotionIDs, "promotion_id")
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.PromotionListV30Response, error) {
			response, httpResponse, sdkErr := client.sdk.PromotionListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Fields(request.Fields).Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardPromotionDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, &pageInfo)
}

func (adapter MarketingDiscoveryAdapter) FetchDPA(
	ctx context.Context,
	request portmarketing.DPADiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	switch request.Mode {
	case "meta":
		platformID, err := parsePositiveID(request.PlatformID, "platform_id")
		if err != nil {
			return domainmarketing.DiscoveryEnvelope{}, err
		}
		result, fetchErr := platformretry.Do(
			ctx, readPolicy(adapter.Retry), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.DpaMetaGetV2Response, error) {
				response, httpResponse, sdkErr := client.sdk.DpaMetaGetV2Api().Get(ctx).
					AccessToken(request.AccessToken).AdvertiserId(advertiserID).PlatformId(platformID).Execute()
				if guardErr := guardDPAMeta(response, httpResponse, sdkErr); guardErr != nil {
					return nil, guardErr
				}
				return response, nil
			},
		)
		if fetchErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, fetchErr
		}
		return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
	case "dict":
		platformID, err := parsePositiveID(request.PlatformID, "platform_id")
		if err != nil {
			return domainmarketing.DiscoveryEnvelope{}, err
		}
		result, fetchErr := platformretry.Do(
			ctx, readPolicy(adapter.Retry), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.DpaDictGetV2Response, error) {
				response, httpResponse, sdkErr := client.sdk.DpaDictGetV2Api().Get(ctx).
					AccessToken(request.AccessToken).AdvertiserId(advertiserID).PlatformId(platformID).Execute()
				if guardErr := guardDPADict(response, httpResponse, sdkErr); guardErr != nil {
					return nil, guardErr
				}
				return response, nil
			},
		)
		if fetchErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, fetchErr
		}
		return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
	case "ebp-detail":
		platformID, err := parsePositiveID(request.PlatformID, "platform_id")
		if err != nil {
			return domainmarketing.DiscoveryEnvelope{}, err
		}
		productID := strings.TrimSpace(request.UniqueProductID)
		if _, parseErr := parsePositiveID(productID, "unique_product_id"); parseErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, parseErr
		}
		filtering := models.DpaEbpProductDetailGetV30Filtering{ProductId: &productID}
		result, fetchErr := platformretry.Do(
			ctx, readPolicy(adapter.Retry), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.DpaEbpProductDetailGetV30Response, error) {
				response, httpResponse, sdkErr := client.sdk.DpaEbpProductDetailGetV30Api().Get(ctx).
					AccessToken(request.AccessToken).AccountId(advertiserID).
					AccountType(models.DpaEbpProductDetailGetV30AccountType("EBP")).PlatformId(platformID).
					Filtering(filtering).Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
				if guardErr := guardDPAEbpDetail(response, httpResponse, sdkErr); guardErr != nil {
					return nil, guardErr
				}
				return response, nil
			},
		)
		if fetchErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, fetchErr
		}
		pageInfo, pageErr := mapMarketingPageInfo(request.Page, request.PageSize,
			result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
			result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
		if pageErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, pageErr
		}
		return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, &pageInfo)
	case "asset-detail":
		productID, parseErr := parsePositiveID(request.UniqueProductID, "unique_product_id")
		if parseErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, parseErr
		}
		body := models.DpaAssetV2DetailReadV2Request{
			AdvertiserId: advertiserID, AssetIds: []int64{}, UniqueProductIds: []int64{productID},
		}
		result, fetchErr := platformretry.Do(
			ctx, readPolicy(adapter.Retry), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.DpaAssetV2DetailReadV2Response, error) {
				response, httpResponse, sdkErr := client.sdk.DpaAssetV2DetailReadV2Api().Post(ctx).
					AccessToken(request.AccessToken).DpaAssetV2DetailReadV2Request(body).Execute()
				if guardErr := guardDPAAssetDetail(response, httpResponse, sdkErr); guardErr != nil {
					return nil, guardErr
				}
				return response, nil
			},
		)
		if fetchErr != nil {
			return domainmarketing.DiscoveryEnvelope{}, fetchErr
		}
		return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
	default:
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unsupported DPA discovery mode")
	}
}

func (adapter MarketingDiscoveryAdapter) FetchEvents(
	ctx context.Context,
	request portmarketing.EventDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	pageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	assetIDs, err := parseDiscoveryIDs(request.AssetIDs, "asset_id")
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	assetType := models.ToolsEventAllAssetsListV2FilteringAssetType(request.AssetType)
	filtering := models.ToolsEventAllAssetsListV2Filtering{AssetIds: assetIDs, AssetType: &assetType}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ToolsEventAllAssetsListV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.ToolsEventAllAssetsListV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Page(page).PageSize(pageSize).Execute()
			if guardErr := guardEventDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, &pageInfo)
}

func (adapter MarketingDiscoveryAdapter) FetchDeepBids(
	ctx context.Context,
	request portmarketing.DeepBidDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.EventManagerDeepBidTypeGetV30Response, error) {
			builder := client.sdk.EventManagerDeepBidTypeGetV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				ExternalAction(models.EventManagerDeepBidTypeGetV30ExternalAction(request.ExternalAction))
			if request.AssetID != "" {
				assetID, parseErr := parsePositiveID(request.AssetID, "asset_id")
				if parseErr != nil {
					return nil, parseErr
				}
				builder = builder.AssetId(assetID)
			}
			if request.DeepExternalAction != "" {
				builder = builder.DeepExternalAction(models.EventManagerDeepBidTypeGetV30DeepExternalAction(request.DeepExternalAction))
			}
			if request.DeliveryMode != "" {
				builder = builder.DeliveryMode(models.EventManagerDeepBidTypeGetV30DeliveryMode(request.DeliveryMode))
			}
			if request.LandingType != "" {
				builder = builder.LandingType(models.EventManagerDeepBidTypeGetV30LandingType(request.LandingType))
			}
			if request.AdType != "" {
				builder = builder.AdType(models.EventManagerDeepBidTypeGetV30AdType(request.AdType))
			}
			if request.MarketingGoal != "" {
				builder = builder.MarketingGoal(models.EventManagerDeepBidTypeGetV30MarketingGoal(request.MarketingGoal))
			}
			if request.ProductSetting != "" {
				builder = builder.ProductSetting(models.EventManagerDeepBidTypeGetV30ProductSetting(request.ProductSetting))
			}
			if request.ValueOptimizedType != "" {
				builder = builder.ValueOptimizedType(models.EventManagerDeepBidTypeGetV30ValueOptimizedType(request.ValueOptimizedType))
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardDeepBidDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
}

func (adapter MarketingDiscoveryAdapter) FetchGoals(
	ctx context.Context,
	request portmarketing.GoalDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.EventManagerOptimizedGoalGetV2V30Response, error) {
			builder := client.sdk.EventManagerOptimizedGoalGetV2V30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				LandingType(models.EventManagerOptimizedGoalGetV2V30LandingType(request.LandingType)).
				AdType(models.EventManagerOptimizedGoalGetV2V30AdType(request.AdType))
			if request.AssetType != "" {
				builder = builder.AssetType(models.EventManagerOptimizedGoalGetV2V30AssetType(request.AssetType))
			}
			if request.MarketingGoal != "" {
				builder = builder.MarketingGoal(models.EventManagerOptimizedGoalGetV2V30MarketingGoal(request.MarketingGoal))
			}
			if request.DeliveryMode != "" {
				builder = builder.DeliveryMode(models.EventManagerOptimizedGoalGetV2V30DeliveryMode(request.DeliveryMode))
			}
			if request.DeliveryType != "" {
				builder = builder.DeliveryType(models.EventManagerOptimizedGoalGetV2V30DeliveryType(request.DeliveryType))
			}
			if request.IncludeAsset && request.AssetID != "" {
				assetID, parseErr := parsePositiveID(request.AssetID, "asset_id")
				if parseErr != nil {
					return nil, parseErr
				}
				builder = builder.AssetId(assetID)
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardGoalDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
}

func (adapter MarketingDiscoveryAdapter) FetchAdminInfo(
	ctx context.Context,
	request portmarketing.AdminDiscoveryRequest,
) (domainmarketing.AdminEnvelope, error) {
	client, advertiserID, err := adapter.client(request.DiscoveryScope)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ToolsAdminInfoV2Response, error) {
			response, httpResponse, sdkErr := client.sdk.ToolsAdminInfoV2Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Codes(request.Codes).
				Language(models.ToolsAdminInfoV2Language("ZH_CN_GOV")).
				SubDistrict(models.ToolsAdminInfoV2SubDistrict("ONE_LEVEL")).
				Version(models.ToolsAdminInfoV2Version("V2_3_2")).Execute()
			if guardErr := guardAdminDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	envelope, err := discoveryEnvelope(result.Code, result.Message, result.RequestId, result, nil)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	nodes, err := adminNodes(envelope.Response)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	return domainmarketing.AdminEnvelope{Code: envelope.Code, Message: envelope.Message,
		RequestID: envelope.RequestID, Response: envelope.Response, Nodes: nodes}, nil
}

func (adapter MarketingDiscoveryAdapter) client(scope portmarketing.DiscoveryScope) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Ocean Engine client factory is required")
	}
	advertiserID, err := parsePositiveID(scope.AdvertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(scope.AccessToken) == "" {
		return nil, 0, errors.New("Marketing discovery access token is required")
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	return client, advertiserID, err
}

func discoveryEnvelope(
	code *int64,
	message *string,
	requestID *string,
	response any,
	pageInfo *domainmarketing.PageInfo,
) (domainmarketing.DiscoveryEnvelope, error) {
	if code == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("Marketing discovery response is missing business code")
	}
	decoded, err := diagnosticObject(response)
	if err != nil {
		return domainmarketing.DiscoveryEnvelope{}, err
	}
	return domainmarketing.DiscoveryEnvelope{Code: *code, Message: stringValue(message),
		RequestID: stringValue(requestID), Response: decoded, PageInfo: pageInfo}, nil
}

func diagnosticObject(value any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Marketing discovery SDK response: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Marketing discovery SDK response: %w", err)
	}
	if result == nil {
		return nil, errors.New("Marketing discovery SDK response is not an object")
	}
	return result, nil
}

func adminNodes(response map[string]any) ([]domainmarketing.AdminNode, error) {
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, errors.New("Marketing administrative response is missing data")
	}
	values, ok := data["districts"].([]any)
	if !ok {
		return nil, errors.New("Marketing administrative response is missing districts")
	}
	result := make([]domainmarketing.AdminNode, 0, len(values))
	for _, value := range values {
		node, err := adminNode(value)
		if err != nil {
			return nil, err
		}
		result = append(result, node)
	}
	return result, nil
}

func adminNode(value any) (domainmarketing.AdminNode, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return domainmarketing.AdminNode{}, errors.New("Marketing administrative district is not an object")
	}
	name := strings.TrimSpace(fmt.Sprint(object["name"]))
	code := strings.TrimSpace(fmt.Sprint(object["code"]))
	if code == "" || code == "<nil>" {
		code = strings.TrimSpace(fmt.Sprint(object["geoname_id"]))
	}
	if name == "<nil>" {
		name = ""
	}
	if code == "<nil>" {
		code = ""
	}
	children := []domainmarketing.AdminNode{}
	if values, ok := object["sub_districts"].([]any); ok {
		children = make([]domainmarketing.AdminNode, 0, len(values))
		for _, value := range values {
			child, err := adminNode(value)
			if err != nil {
				return domainmarketing.AdminNode{}, err
			}
			children = append(children, child)
		}
	}
	return domainmarketing.AdminNode{Name: name, Code: code, Children: children}, nil
}

func guardProjectDiscovery(response *models.ProjectListV30Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardPromotionDiscovery(response *models.PromotionListV30Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardDPAMeta(response *models.DpaMetaGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardDPADict(response *models.DpaDictGetV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardDPAEbpDetail(response *models.DpaEbpProductDetailGetV30Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardDPAAssetDetail(response *models.DpaAssetV2DetailReadV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardEventDiscovery(response *models.ToolsEventAllAssetsListV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true,
		response.Data != nil && response.Data.PageInfo != nil)
}

func guardDeepBidDiscovery(response *models.EventManagerDeepBidTypeGetV30Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardGoalDiscovery(response *models.EventManagerOptimizedGoalGetV2V30Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardAdminDiscovery(response *models.ToolsAdminInfoV2Response, httpResponse *http.Response, sdkErr error) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func parseDiscoveryIDs(values []string, field string) ([]int64, error) {
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
