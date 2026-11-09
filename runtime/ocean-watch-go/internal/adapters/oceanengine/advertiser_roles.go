package oceanengine

import (
	"context"
	"errors"
	"net/url"
	"strconv"

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
	role := effectiveAccountRole(account)
	if issue := nonAdvertisingRole(channel, role, account.AccountID); issue != nil {
		return []string{}, issue, nil
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
		rows, err := adapter.listAgent(ctx, channel, accessToken, sourceID)
		var envelope *EnvelopeError
		if errors.As(err, &envelope) && envelope.Code == 40002 {
			return []string{}, &domain.AccountDiscoveryIssue{
				AccountID: account.AccountID, Role: role, Code: "40002", Reason: "app_permission_missing",
			}, nil
		}
		return rows, nil, err
	case "PLATFORM_ROLE_SHOP_ACCOUNT":
		rows, err := adapter.listShop(ctx, channel, accessToken, sourceID)
		return rows, nil, err
	default:
		return []string{}, &domain.AccountDiscoveryIssue{
			AccountID: account.AccountID, Role: "UNKNOWN", Code: "unsupported_account_role", Reason: "account_role_expansion_not_supported",
		}, nil
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
	return adapter.collectAccountIDs(ctx, client, accessToken, customerAccountsEndpoint,
		url.Values{"cc_account_id": {strconv.FormatInt(accountID, 10)}, "account_source": {accountSource(channel)}},
		[]string{"list"}, []string{"advertiser_id", "account_id"})
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
	return adapter.collectAccountIDs(ctx, client, accessToken, enterpriseAccountsEndpoint,
		url.Values{"enterprise_organization_id": {strconv.FormatInt(accountID, 10)}, "account_source": {accountSource(channel)}},
		[]string{"account_list", "list"}, []string{"account_id", "advertiser_id"})
}

func accountSource(channel string) string {
	if channel == "qianchuan" {
		return "QIANCHUAN"
	}
	return "AD"
}

func (adapter AdvertiserDiscoveryAdapter) collectAccountIDs(ctx context.Context, client *Client, token, endpoint string, query url.Values, listKeys, idKeys []string) ([]string, error) {
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[string]{
		Retry: adapter.readRetry(), Classify: ClassifyReadError, Key: identityString,
		Fetch: func(ctx context.Context, page int) (pagination.Page[string], error) {
			query.Set("page", strconv.Itoa(page))
			query.Set("page_size", strconv.Itoa(advertiserPageSize))
			response, err := client.accountData(ctx, endpoint, token, query)
			if err != nil {
				return pagination.Page[string]{}, err
			}
			data, ok := response.(map[string]any)
			if !ok {
				return pagination.Page[string]{}, discoveryFailure(endpoint, "INVALID_ACCOUNT_LIST", errors.New("account page data is not an object"))
			}
			items, err := discoveryRows(data, listKeys...)
			if err != nil {
				return pagination.Page[string]{}, discoveryFailure(endpoint, "INVALID_ACCOUNT_LIST", err)
			}
			ids := make([]string, 0, len(items))
			for _, item := range items {
				row, ok := item.(map[string]any)
				if !ok {
					return pagination.Page[string]{}, discoveryFailure(endpoint, "INVALID_ACCOUNT_LIST", errors.New("invalid account page row"))
				}
				id := discoveryID(row, idKeys...)
				if id == "" {
					return pagination.Page[string]{}, discoveryFailure(endpoint, "INVALID_ADVERTISER_ID", errors.New("missing advertiser identity"))
				}
				ids = append(ids, id)
			}
			result, err := discoveryPage(data, page, ids)
			if err != nil {
				return pagination.Page[string]{}, discoveryFailure(endpoint, "INVALID_PAGINATION", err)
			}
			return result, nil
		},
	})
	if err != nil {
		return nil, discoveryFailure(endpoint, "INVALID_PAGINATION", err)
	}
	return rows, nil
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
