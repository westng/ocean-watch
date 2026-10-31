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
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

const advertiserPageSize = 100

type AdvertiserDiscoveryAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

func (adapter AdvertiserDiscoveryAdapter) Discover(
	ctx context.Context,
	channel string,
	accessToken string,
) (domain.AdvertiserSnapshot, error) {
	if ctx == nil || adapter.Factory == nil {
		return domain.AdvertiserSnapshot{}, errors.New("advertiser discovery adapter is incomplete")
	}
	if channel != "marketing" && channel != "qianchuan" && channel != "star_map" {
		return domain.AdvertiserSnapshot{}, fmt.Errorf("unsupported advertiser discovery channel %q", channel)
	}
	if strings.TrimSpace(accessToken) == "" {
		return domain.AdvertiserSnapshot{}, errors.New("advertiser discovery access token is required")
	}
	accounts, err := adapter.listAuthorizedAccounts(ctx, channel, accessToken)
	if err != nil {
		return domain.AdvertiserSnapshot{}, err
	}
	if channel == "star_map" {
		accounts = filterStarMapAccounts(accounts)
	}
	candidates := []string{}
	issues := []domain.AccountDiscoveryIssue{}
	for index := range accounts {
		account := &accounts[index]
		if account.IsValid != nil && !*account.IsValid {
			account.AdvertiserIDs = nil
			continue
		}
		advertiserIDs, issue, err := adapter.expandAccount(ctx, channel, accessToken, *account)
		if err != nil {
			return domain.AdvertiserSnapshot{}, err
		}
		account.AdvertiserIDs = advertiserIDs
		candidates = append(candidates, advertiserIDs...)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	candidates = uniqueStrings(candidates)
	verified, err := adapter.verifyAdvertisers(ctx, channel, accessToken, candidates)
	if err != nil {
		return domain.AdvertiserSnapshot{}, err
	}
	verifiedSet := make(map[string]struct{}, len(verified))
	for _, advertiserID := range verified {
		verifiedSet[advertiserID] = struct{}{}
	}
	for index := range accounts {
		filtered := make([]string, 0, len(accounts[index].AdvertiserIDs))
		for _, advertiserID := range accounts[index].AdvertiserIDs {
			if _, exists := verifiedSet[advertiserID]; exists {
				filtered = append(filtered, advertiserID)
			}
		}
		accounts[index].AdvertiserIDs = filtered
	}
	return domain.AdvertiserSnapshot{
		Accounts: accounts, AdvertiserIDs: verified, DiscoveryIssues: issues,
	}, nil
}

func (adapter AdvertiserDiscoveryAdapter) listAuthorizedAccounts(
	ctx context.Context,
	channel string,
	accessToken string,
) ([]domain.AuthorizedAccount, error) {
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	response, err := platformretry.Do(
		ctx, adapter.readRetry(), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.Oauth2AdvertiserGetResponse, error) {
			response, httpResponse, sdkErr := client.sdk.Oauth2AdvertiserGetApi().Get(ctx).
				AccessToken(accessToken).Execute()
			if response == nil {
				return nil, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
			}
			if err := GuardEnvelope(
				httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
				true, response.Data != nil,
			); err != nil {
				return nil, err
			}
			return response, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list authorized accounts: %w", err)
	}
	accounts := make([]domain.AuthorizedAccount, 0, len(response.Data.List))
	seen := map[string]struct{}{}
	for _, row := range response.Data.List {
		account, err := mapAuthorizedAccount(row)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[account.AccountID]; exists {
			return nil, fmt.Errorf("authorized account %s is duplicated", account.AccountID)
		}
		seen[account.AccountID] = struct{}{}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (adapter AdvertiserDiscoveryAdapter) verifyAdvertisers(
	ctx context.Context,
	channel, accessToken string,
	candidates []string,
) ([]string, error) {
	if len(candidates) == 0 {
		return []string{}, nil
	}
	if channel == "star_map" {
		return adapter.verifyStarAccounts(ctx, accessToken, candidates)
	}
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	verified := []string{}
	seen := map[string]struct{}{}
	for start := 0; start < len(candidates); start += 50 {
		end := start + 50
		if end > len(candidates) {
			end = len(candidates)
		}
		chunk := candidates[start:end]
		ids := make([]int64, len(chunk))
		for index, candidate := range chunk {
			ids[index], err = parsePositiveID(candidate, "advertiser_id")
			if err != nil {
				return nil, err
			}
		}
		response, err := platformretry.Do(
			ctx, adapter.readRetry(), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.AdvertiserInfoV2Response, error) {
				response, httpResponse, sdkErr := client.sdk.AdvertiserInfoV2Api().Get(ctx).
					AccessToken(accessToken).AdvertiserIds(ids).Execute()
				if response == nil {
					return nil, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
				}
				if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
					return nil, err
				}
				return response, nil
			},
		)
		if err != nil {
			return nil, fmt.Errorf("verify advertiser batch %d: %w", start/50+1, err)
		}
		for _, row := range response.Data {
			if row == nil || row.Id == nil || *row.Id <= 0 {
				return nil, errors.New("advertiser info response contains an invalid ID")
			}
			value := strconv.FormatInt(*row.Id, 10)
			if _, duplicate := seen[value]; duplicate {
				return nil, fmt.Errorf("advertiser info response duplicated %s", value)
			}
			seen[value] = struct{}{}
			verified = append(verified, value)
		}
	}
	if len(verified) != len(candidates) {
		return nil, fmt.Errorf("advertiser verification returned %d of %d candidates", len(verified), len(candidates))
	}
	for _, candidate := range candidates {
		if _, exists := seen[candidate]; !exists {
			return nil, fmt.Errorf("advertiser verification omitted %s", candidate)
		}
	}
	return candidates, nil
}

func (adapter AdvertiserDiscoveryAdapter) verifyStarAccounts(
	ctx context.Context,
	accessToken string,
	candidates []string,
) ([]string, error) {
	client, err := adapter.Factory.Client("star_map", ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	verified := []string{}
	seen := map[string]struct{}{}
	for start := 0; start < len(candidates); start += 50 {
		end := start + 50
		if end > len(candidates) {
			end = len(candidates)
		}
		chunk := candidates[start:end]
		ids := make([]int64, len(chunk))
		for index, candidate := range chunk {
			ids[index], err = parsePositiveID(candidate, "star_id")
			if err != nil {
				return nil, err
			}
		}
		response, err := platformretry.Do(
			ctx, adapter.readRetry(), ClassifyReadError,
			func(ctx context.Context, _ int) (*models.StarInfoV2Response, error) {
				response, httpResponse, sdkErr := client.sdk.StarInfoV2Api().Get(ctx).
					AccessToken(accessToken).StarIds(ids).Execute()
				if response == nil {
					return nil, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
				}
				if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
					return nil, err
				}
				return response, nil
			},
		)
		if err != nil {
			return nil, fmt.Errorf("verify star account batch %d: %w", start/50+1, err)
		}
		if response.Data == nil {
			return nil, errors.New("star info response is missing data")
		}
		for _, row := range response.Data.InfoList {
			if row == nil || row.StartId <= 0 {
				return nil, errors.New("star info response contains an invalid star ID")
			}
			value := strconv.FormatInt(row.StartId, 10)
			if _, duplicate := seen[value]; duplicate {
				return nil, fmt.Errorf("star info response duplicated %s", value)
			}
			seen[value] = struct{}{}
			verified = append(verified, value)
		}
	}
	if len(verified) != len(candidates) {
		return nil, fmt.Errorf("star account verification returned %d of %d candidates", len(verified), len(candidates))
	}
	for _, candidate := range candidates {
		if _, exists := seen[candidate]; !exists {
			return nil, fmt.Errorf("star account verification omitted %s", candidate)
		}
	}
	return candidates, nil
}

func (adapter AdvertiserDiscoveryAdapter) readRetry() platformretry.Policy {
	return readPolicy(adapter.Retry)
}

func mapAuthorizedAccount(row *models.Oauth2AdvertiserGetResponseDataListInner) (domain.AuthorizedAccount, error) {
	if row == nil {
		return domain.AuthorizedAccount{}, errors.New("authorized account response contains a null row")
	}
	accountID := ""
	if row.AccountId != nil && *row.AccountId > 0 {
		accountID = strconv.FormatInt(*row.AccountId, 10)
	}
	if accountID == "" && row.AccountStringId != nil {
		candidate := strings.TrimSpace(*row.AccountStringId)
		if _, err := parsePositiveID(candidate, "authorized account_id"); err == nil {
			accountID = candidate
		}
	}
	if accountID == "" && row.AdvertiserId != nil && *row.AdvertiserId > 0 {
		accountID = strconv.FormatInt(*row.AdvertiserId, 10)
	}
	if _, err := parsePositiveID(accountID, "authorized account_id"); err != nil {
		return domain.AuthorizedAccount{}, err
	}
	accountType := ""
	if row.AccountType != nil {
		accountType = string(*row.AccountType)
	}
	accountRole := stringValue(row.AccountRole)
	advertiserIDs := []string(nil)
	if row.AdvertiserId != nil && *row.AdvertiserId > 0 {
		advertiserIDs = []string{strconv.FormatInt(*row.AdvertiserId, 10)}
	}
	account := domain.AuthorizedAccount{
		AccountID: accountID, AccountStringID: stringValue(row.AccountStringId),
		AccountName: stringValue(row.AccountName), AccountRole: accountRole,
		AccountType: accountType, AdvertiserName: stringValue(row.AdvertiserName),
		IsValid: row.IsValid, AdvertiserIDs: advertiserIDs,
	}
	if accountType == "PLATFORM_ROLE_SHOP_ACCOUNT" || accountRole == "PLATFORM_ROLE_SHOP_ACCOUNT" {
		account.ShopID = accountID
	}
	return account, nil
}

func pageFromPointers[T any](
	expected int,
	page, totalPages, totalNumber *int64,
	rows []T,
) (pagination.Page[T], error) {
	maxInt := int64(^uint(0) >> 1)
	if totalPages == nil || *totalPages < 0 || *totalPages > maxInt {
		return pagination.Page[T]{}, errors.New("response contains malformed page_info")
	}
	if *totalPages == 0 && expected == 1 && len(rows) == 0 {
		if page != nil && (*page < 0 || *page > maxInt || *page != 0 && *page != 1) {
			return pagination.Page[T]{}, errors.New("response contains malformed page_info")
		}
		if totalNumber != nil && *totalNumber != 0 {
			return pagination.Page[T]{}, errors.New("response contains malformed page_info")
		}
		return pagination.Page[T]{Number: expected, TotalPages: 0, TotalNumber: 0, Rows: rows}, nil
	}
	if page == nil || totalNumber == nil ||
		*page < 0 || *totalNumber < 0 ||
		*page > maxInt || *totalNumber > maxInt {
		return pagination.Page[T]{}, errors.New("response contains malformed page_info")
	}
	result := pagination.Page[T]{
		Number: int(*page), TotalPages: int(*totalPages), TotalNumber: int(*totalNumber), Rows: rows,
	}
	return result, nil
}

func stringIDs(values []int64, field string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("%s must be positive", field)
		}
		result = append(result, strconv.FormatInt(value, 10))
	}
	return result, nil
}

func parsePositiveID(value, field string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive int64", field)
	}
	return parsed, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func filterStarMapAccounts(accounts []domain.AuthorizedAccount) []domain.AuthorizedAccount {
	result := make([]domain.AuthorizedAccount, 0, len(accounts))
	for _, account := range accounts {
		role := account.AccountRole
		if role == "" {
			role = account.AccountType
		}
		if role == "PLATFORM_ROLE_STAR" || role == "PLATFORM_ROLE_STAR_AGENT" ||
			role == "PLATFORM_ROLE_STAR_MCN" || role == "PLATFORM_ROLE_STAR_ISV" {
			result = append(result, account)
		}
	}
	return result
}

func identityString(value string) string { return value }

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
