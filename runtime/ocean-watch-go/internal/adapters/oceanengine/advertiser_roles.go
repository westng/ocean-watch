package oceanengine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/pagination"
)

func (adapter AdvertiserDiscoveryAdapter) expandAccount(
	ctx context.Context,
	channel string,
	accessToken string,
	account domain.AuthorizedAccount,
) ([]string, *domain.AccountDiscoveryIssue, error) {
	role := account.AccountRole
	if role == "" {
		role = account.AccountType
	}
	sourceID, err := parsePositiveID(account.AccountID, "authorized account_id")
	if err != nil {
		return nil, nil, err
	}
	switch role {
	case "ADVERTISER":
		advertiserID := account.AccountID
		if len(account.AdvertiserIDs) == 1 {
			advertiserID = account.AdvertiserIDs[0]
		}
		return []string{advertiserID}, nil, nil
	case "PLATFORM_ROLE_AWEME":
		return []string{}, &domain.AccountDiscoveryIssue{
			AccountID: account.AccountID, Role: role,
			Code: "non_advertiser_role", Reason: "account_role_has_no_advertiser_expansion",
		}, nil
	case "PLATFORM_ROLE_STAR":
		return []string{account.AccountID}, nil, nil
	case "PLATFORM_ROLE_STAR_AGENT", "PLATFORM_ROLE_STAR_MCN", "PLATFORM_ROLE_STAR_ISV":
		return []string{}, &domain.AccountDiscoveryIssue{
			AccountID: account.AccountID, Role: role,
			Code: "star_account_expansion_not_supported", Reason: "star_account_role_requires_explicit_account_selection",
		}, nil
	case "CUSTOMER_ADMIN", "CUSTOMER_OPERATOR":
		rows, err := adapter.listCustomerCenter(ctx, channel, accessToken, sourceID)
		return rows, nil, err
	case "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR":
		rows, err := adapter.listEBP(ctx, channel, accessToken, sourceID)
		return rows, nil, err
	case "PLATFORM_ROLE_QIANCHUAN_AGENT":
		if channel != "qianchuan" {
			return nil, nil, fmt.Errorf("role %s is invalid for channel %s", role, channel)
		}
		rows, err := adapter.listAgent(ctx, channel, accessToken, sourceID)
		var envelope *EnvelopeError
		if errors.As(err, &envelope) && envelope.Code == 40002 {
			return []string{}, &domain.AccountDiscoveryIssue{
				AccountID: account.AccountID, Role: role, Code: "40002", Reason: "app_permission_missing",
			}, nil
		}
		return rows, nil, err
	case "PLATFORM_ROLE_SHOP_ACCOUNT":
		if channel != "qianchuan" {
			return nil, nil, fmt.Errorf("role %s is invalid for channel %s", role, channel)
		}
		rows, err := adapter.listShop(ctx, channel, accessToken, sourceID)
		return rows, nil, err
	default:
		return nil, nil, fmt.Errorf("authorized account role %s is not supported for complete discovery", role)
	}
}

func (adapter AdvertiserDiscoveryAdapter) listCustomerCenter(
	ctx context.Context,
	channel, accessToken string,
	accountID int64,
) ([]string, error) {
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	accountSource := models.AD_CustomerCenterAdvertiserListV2AccountSource
	if channel == "qianchuan" {
		accountSource = models.QIANCHUAN_CustomerCenterAdvertiserListV2AccountSource
	}
	return pagination.CollectPages(ctx, pagination.PageOptions[string]{
		Retry: adapter.readRetry(), Classify: ClassifyReadError, Key: identityString,
		Fetch: func(ctx context.Context, page int) (pagination.Page[string], error) {
			response, httpResponse, sdkErr := client.sdk.CustomerCenterAdvertiserListV2Api().Get(ctx).
				AccessToken(accessToken).AccountSource(accountSource).CcAccountId(accountID).
				Page(int64(page)).PageSize(advertiserPageSize).Execute()
			if response == nil {
				return pagination.Page[string]{}, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
			}
			if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
				return pagination.Page[string]{}, err
			}
			rows := make([]string, 0, len(response.Data.List))
			for _, row := range response.Data.List {
				if row == nil {
					return pagination.Page[string]{}, errors.New("customer center response contains a null row")
				}
				value := ""
				if row.AdvertiserId != nil {
					value = strconv.FormatInt(*row.AdvertiserId, 10)
				} else if row.AccountId != nil {
					value = strings.TrimSpace(*row.AccountId)
				}
				if _, err := parsePositiveID(value, "customer center advertiser_id"); err != nil {
					return pagination.Page[string]{}, err
				}
				rows = append(rows, value)
			}
			if response.Data.PageInfo == nil {
				return pagination.Page[string]{}, errors.New("customer center response is missing page_info")
			}
			return pageFromPointers(page, response.Data.PageInfo.Page, response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber, rows)
		},
	})
}

func (adapter AdvertiserDiscoveryAdapter) listEBP(
	ctx context.Context,
	channel, accessToken string,
	accountID int64,
) ([]string, error) {
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	accountSource := models.AD_EbpAdvertiserListV2AccountSource
	if channel == "qianchuan" {
		accountSource = models.QIANCHUAN_EbpAdvertiserListV2AccountSource
	}
	return pagination.CollectPages(ctx, pagination.PageOptions[string]{
		Retry: adapter.readRetry(), Classify: ClassifyReadError, Key: identityString,
		Fetch: func(ctx context.Context, page int) (pagination.Page[string], error) {
			response, httpResponse, sdkErr := client.sdk.EbpAdvertiserListV2Api().Get(ctx).
				AccessToken(accessToken).EnterpriseOrganizationId(accountID).AccountSource(accountSource).
				Page(int64(page)).PageSize(advertiserPageSize).Execute()
			if response == nil {
				return pagination.Page[string]{}, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
			}
			if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
				return pagination.Page[string]{}, err
			}
			rows := make([]string, 0, len(response.Data.AccountList))
			for _, row := range response.Data.AccountList {
				if row == nil || row.AccountId == nil || *row.AccountId <= 0 {
					return pagination.Page[string]{}, errors.New("EBP response contains an invalid advertiser ID")
				}
				rows = append(rows, strconv.FormatInt(*row.AccountId, 10))
			}
			if response.Data.PageInfo == nil {
				return pagination.Page[string]{}, errors.New("EBP response is missing page_info")
			}
			return pageFromPointers(page, response.Data.PageInfo.Page, response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber, rows)
		},
	})
}

func (adapter AdvertiserDiscoveryAdapter) listAgent(
	ctx context.Context,
	channel, accessToken string,
	accountID int64,
) ([]string, error) {
	client, err := adapter.Factory.Client(channel, ProfileAgent, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	return pagination.CollectPages(ctx, pagination.PageOptions[string]{
		Retry: adapter.readRetry(), Classify: ClassifyReadError, Key: identityString,
		Fetch: func(ctx context.Context, page int) (pagination.Page[string], error) {
			response, httpResponse, sdkErr := client.sdk.AgentAdvertiserSelectV2Api().Get(ctx).
				AccessToken(accessToken).AdvertiserId(accountID).Page(int64(page)).PageSize(advertiserPageSize).Execute()
			if response == nil {
				return pagination.Page[string]{}, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
			}
			if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
				return pagination.Page[string]{}, err
			}
			if response.Data.PageInfo == nil {
				return pagination.Page[string]{}, errors.New("agent advertiser response is missing page_info")
			}
			values := response.Data.List
			if len(values) == 0 {
				values = response.Data.AdvertiserIds
			}
			rows, err := stringIDs(values, "agent advertiser_id")
			if err != nil {
				return pagination.Page[string]{}, err
			}
			return pageFromPointers(page, response.Data.PageInfo.Page, response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber, rows)
		},
	})
}

func (adapter AdvertiserDiscoveryAdapter) listShop(
	ctx context.Context,
	channel, accessToken string,
	shopID int64,
) ([]string, error) {
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	return pagination.CollectPages(ctx, pagination.PageOptions[string]{
		Retry: adapter.readRetry(), Classify: ClassifyReadError, Key: identityString,
		Fetch: func(ctx context.Context, page int) (pagination.Page[string], error) {
			permission := models.QC_AWEME_QianchuanShopAdvertiserListV10Permission
			response, httpResponse, sdkErr := client.sdk.QianchuanShopAdvertiserListV10Api().Get(ctx).
				AccessToken(accessToken).ShopId(shopID).Permission([]*models.QianchuanShopAdvertiserListV10Permission{&permission}).
				Page(int64(page)).PageSize(advertiserPageSize).Execute()
			if response == nil {
				return pagination.Page[string]{}, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
			}
			if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
				return pagination.Page[string]{}, err
			}
			if response.Data.PageInfo == nil {
				return pagination.Page[string]{}, errors.New("shop advertiser response is missing page_info")
			}
			values := response.Data.List
			if len(values) == 0 {
				values = make([]int64, 0, len(response.Data.AdvIdList))
				for _, row := range response.Data.AdvIdList {
					if row == nil || row.AdvId == nil {
						return pagination.Page[string]{}, errors.New("shop advertiser response contains an invalid advertiser ID")
					}
					values = append(values, *row.AdvId)
				}
			}
			rows, err := stringIDs(values, "shop advertiser_id")
			if err != nil {
				return pagination.Page[string]{}, err
			}
			return pageFromPointers(page, response.Data.PageInfo.Page, response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber, rows)
		},
	})
}
