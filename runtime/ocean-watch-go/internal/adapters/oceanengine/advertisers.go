package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	accounts, issues, err := adapter.listAuthorizedAccounts(ctx, channel, accessToken)
	if err != nil {
		return domain.AdvertiserSnapshot{}, err
	}
	candidates := []string{}
	for index := range accounts {
		account := &accounts[index]
		if account.IsValid != nil && !*account.IsValid {
			account.AdvertiserIDs = nil
			continue
		}
		advertiserIDs, issue, err := adapter.expandAccount(ctx, channel, accessToken, *account)
		if err != nil {
			return domain.AdvertiserSnapshot{}, discoveryFailure(roleEndpoint(*account), "", err)
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
) ([]domain.AuthorizedAccount, []domain.AccountDiscoveryIssue, error) {
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return nil, nil, err
	}
	response, err := platformretry.Do(
		ctx, adapter.readRetry(), ClassifyReadError,
		func(ctx context.Context, _ int) (any, error) {
			return client.accountData(ctx, authorizedAccountsEndpoint, accessToken, nil)
		},
	)
	if err != nil {
		return nil, nil, err
	}
	rows, err := discoveryRows(response, "list")
	if err != nil {
		return nil, nil, discoveryFailure(authorizedAccountsEndpoint, "INVALID_ACCOUNT_LIST", err)
	}
	accounts := make([]domain.AuthorizedAccount, 0, len(rows))
	issues := []domain.AccountDiscoveryIssue{}
	seen := map[string]struct{}{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, discoveryFailure(authorizedAccountsEndpoint, "INVALID_ACCOUNT_LIST", errors.New("invalid authorized account row"))
		}
		account, issue, err := mapAuthorizedAccount(channel, row)
		if err != nil {
			return nil, nil, discoveryFailure(authorizedAccountsEndpoint, "INVALID_ACCOUNT_ID", err)
		}
		if issue != nil {
			issues = append(issues, *issue)
		}
		if account == nil {
			continue
		}
		if _, exists := seen[account.AccountID]; exists {
			return nil, nil, discoveryFailure(authorizedAccountsEndpoint, "DUPLICATE_ACCOUNT_ID", errors.New("duplicated authorized account identity"))
		}
		seen[account.AccountID] = struct{}{}
		accounts = append(accounts, *account)
	}
	return accounts, issues, nil
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
		for _, id := range chunk {
			if _, err := parsePositiveID(id, "advertiser_id"); err != nil {
				return nil, discoveryFailure(advertiserInfoEndpoint, "INVALID_ADVERTISER_ID", err)
			}
		}
		response, err := platformretry.Do(
			ctx, adapter.readRetry(), ClassifyReadError,
			func(ctx context.Context, _ int) (any, error) {
				return client.accountData(ctx, advertiserInfoEndpoint, accessToken, url.Values{"advertiser_ids": {encodedDiscoveryIDs(chunk)}})
			},
		)
		if err != nil {
			return nil, err
		}
		rows, err := discoveryRows(response, "list", "data")
		if err != nil {
			return nil, discoveryFailure(advertiserInfoEndpoint, "INVALID_ADVERTISER_INFO", err)
		}
		batch := map[string]bool{}
		for _, id := range chunk {
			batch[id] = true
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return nil, discoveryFailure(advertiserInfoEndpoint, "INVALID_ADVERTISER_INFO", errors.New("invalid advertiser detail row"))
			}
			value := discoveryID(row, "id", "advertiser_id")
			if value == "" || !batch[value] {
				return nil, discoveryFailure(advertiserInfoEndpoint, "INVALID_ADVERTISER_INFO", errors.New("advertiser detail does not belong to the requested batch"))
			}
			if _, duplicate := seen[value]; duplicate {
				return nil, discoveryFailure(advertiserInfoEndpoint, "DUPLICATE_ADVERTISER_ID", errors.New("duplicated advertiser detail identity"))
			}
			seen[value] = struct{}{}
			verified = append(verified, value)
		}
	}
	if len(verified) != len(candidates) {
		return nil, discoveryFailure(advertiserInfoEndpoint, "INCOMPLETE_ADVERTISER_INFO", errors.New("advertiser verification is incomplete"))
	}
	for _, candidate := range candidates {
		if _, exists := seen[candidate]; !exists {
			return nil, discoveryFailure(advertiserInfoEndpoint, "INCOMPLETE_ADVERTISER_INFO", errors.New("advertiser verification omitted a candidate"))
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

func pageFromPointers[T any](
	expected int,
	page, totalPages, totalNumber *int64,
	rows []T,
) (pagination.Page[T], error) {
	info := map[string]any{}
	for key, value := range map[string]*int64{"page": page, "total_page": totalPages, "total_number": totalNumber} {
		if value != nil {
			info[key] = json.Number(strconv.FormatInt(*value, 10))
		}
	}
	identities := make([]string, len(rows))
	result, err := discoveryPage(map[string]any{"page_info": info}, expected, identities)
	if err != nil {
		return pagination.Page[T]{}, err
	}
	return pagination.Page[T]{Number: result.Number, TotalPages: result.TotalPages, TotalNumber: result.TotalNumber, TotalNumberUnknown: result.TotalNumberUnknown, Rows: rows}, nil
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
