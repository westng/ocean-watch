package oceanengine

import (
	"context"
	"errors"

	"github.com/oceanengine/ad_open_sdk_go/models"
)

// ServiceAnchor compiles the exact official generated services used by the
// concrete adapters. It is never called in production; adapters build requests
// from these service methods and map SDK types immediately.
func ServiceAnchor(ctx context.Context, client *Client) error {
	if ctx == nil || client == nil {
		return errors.New("SDK service anchor requires context and client")
	}
	_ = client.sdk.Oauth2AccessTokenApi().Post(ctx)
	_ = client.sdk.Oauth2RefreshTokenApi().Post(ctx)
	_ = client.sdk.Oauth2AdvertiserGetApi().Get(ctx)
	_ = client.sdk.CustomerCenterAdvertiserListV2Api().Get(ctx)
	_ = client.sdk.EbpAdvertiserListV2Api().Get(ctx)
	_ = client.sdk.AdvertiserInfoV2Api().Get(ctx)
	_ = client.sdk.AgentAdvertiserSelectV2Api().Get(ctx)
	_ = client.sdk.QianchuanShopAdvertiserListV10Api().Get(ctx)
	_ = client.sdk.ReportCustomGetV30Api().Get(ctx)
	_ = client.sdk.QianchuanReportAllPromotionGetV10Api().Get(ctx)
	_ = client.sdk.QianchuanReportUniPromotionGetV10Api().Get(ctx)
	_ = client.sdk.QianchuanReportUniPromotionDimensionDataRoomGetV10Api().Get(ctx)
	_ = client.sdk.QianchuanReportUniPromotionDimensionDataAuthorGetV10Api().Get(ctx)
	_ = client.sdk.QianchuanUniPromotionListV10Api().Get(ctx)
	_ = client.sdk.QianchuanUniPromotionAdDetailV10Api().Get(ctx)
	_ = client.sdk.QianchuanUniAwemeAdCreateV10Api().Post(ctx)
	_ = client.sdk.QianchuanUniPromotionAdMaterialAddV10Api().Post(ctx)
	_ = client.sdk.QianchuanUniPromotionAdMaterialDeleteV10Api().Post(ctx)
	_ = client.sdk.QianchuanUniPromotionAdStatusUpdateV10Api().Post(ctx)
	_ = client.sdk.QianchuanUniPromotionAdBudgetUpdateV10Api().Post(ctx)
	_ = client.sdk.QianchuanUniPromotionAdRoi2GoalUpdateV10Api().Post(ctx)
	_ = models.Oauth2RefreshTokenRequest{}
	return nil
}
