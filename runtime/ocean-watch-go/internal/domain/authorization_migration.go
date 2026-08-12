package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var legacyAuthorizationTokenFields = []string{
	"access_token",
	"refresh_token",
	"access_token_expires_at",
	"refresh_token_expires_at",
	"last_token_update_at",
}

type LegacyAuthorizationPlan struct {
	State                  map[string]any
	Credential             map[string]any
	Result                 map[string]any
	CommitAuthorization    bool
	WriteAuthorizationSlot bool
}

func PrepareLegacyMarketingAuthorization(
	current map[string]any,
	legacy map[string]any,
	authorizationID string,
) (LegacyAuthorizationPlan, error) {
	if strings.TrimSpace(authorizationID) == "" || strings.ContainsAny(authorizationID, "\x00\r\n/") {
		return LegacyAuthorizationPlan{}, errors.New("authorization_id is invalid")
	}
	state := cloneAuthorizationMap(current)
	if len(legacy) == 0 {
		return LegacyAuthorizationPlan{
			State:  state,
			Result: map[string]any{"migrated": false, "reason": "legacy_credentials_not_found"},
		}, nil
	}
	hasApp := !authorizationMissing(legacy["app_id"]) && !authorizationMissing(legacy["secret"])
	hasToken := !authorizationMissing(legacy["access_token"]) || !authorizationMissing(legacy["refresh_token"])
	if !hasToken {
		return LegacyAuthorizationPlan{
			State: state,
			Result: map[string]any{
				"migrated": hasApp, "channel": "marketing", "authorization_migrated": false,
				"reason": "legacy_token_not_found",
			},
		}, nil
	}
	authorizations, err := authorizationMigrationObject(state, "authorizations")
	if err != nil {
		return LegacyAuthorizationPlan{}, err
	}
	if _, exists := authorizations[authorizationID]; exists {
		return LegacyAuthorizationPlan{
			State: state,
			Result: map[string]any{
				"migrated": false, "reason": "legacy_authorization_already_migrated",
				"authorization_id": authorizationID,
			},
		}, nil
	}
	if len(authorizations) != 0 {
		return LegacyAuthorizationPlan{
			State:  state,
			Result: map[string]any{"migrated": false, "reason": "marketing_authorizations_exist"},
		}, nil
	}

	accounts, resolved, err := legacyAuthorizedAccounts(legacy["oauth_authorized_accounts"])
	if err != nil {
		return LegacyAuthorizationPlan{}, err
	}
	declared, err := legacyAdvertiserIDs(legacy["authorized_advertiser_ids"])
	if err != nil {
		return LegacyAuthorizationPlan{}, err
	}
	pending := false
	for _, advertiserID := range declared {
		if !resolved[advertiserID] {
			pending = true
			break
		}
	}
	credential := map[string]any{}
	for _, field := range legacyAuthorizationTokenFields {
		if value, exists := legacy[field]; exists && !authorizationMissing(value) {
			credential[field] = cloneAuthorizationValue(value)
		}
	}
	authorizations[authorizationID] = map[string]any{
		"token_revision":                  1,
		"authorized_accounts":             accounts,
		"advertiser_ids":                  []any{},
		"last_authorized_account_sync_at": cloneAuthorizationValue(legacy["last_authorized_account_sync_at"]),
		"pending_account_sync":            pending,
	}
	state["authorizations"] = authorizations
	if err := rebuildAuthorizationMigrationIndexes(state); err != nil {
		return LegacyAuthorizationPlan{}, err
	}
	generation := 0
	if state["generation"] != nil {
		generation, err = authorizationNonNegativeInteger(state["generation"])
		if err != nil {
			return LegacyAuthorizationPlan{}, errors.New("authorization generation must be non-negative")
		}
	}
	state["generation"] = generation + 1
	return LegacyAuthorizationPlan{
		State: state, Credential: credential, CommitAuthorization: true, WriteAuthorizationSlot: true,
		Result: map[string]any{
			"migrated": true, "channel": "marketing", "authorization_id": authorizationID,
			"pending_account_sync": pending,
		},
	}, nil
}

func legacyAuthorizedAccounts(value any) ([]any, map[string]bool, error) {
	rows := authorizationList(value)
	if value != nil && rows == nil {
		return nil, nil, errors.New("oauth_authorized_accounts must be a list")
	}
	result := make([]any, 0, len(rows))
	resolved := map[string]bool{}
	allowed := map[string]bool{
		"account_name": true, "account_role": true, "account_type": true,
		"account_string_id": true, "shop_id": true, "advertiser_name": true, "is_valid": true,
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, errors.New("OAuth authorized account must be an object")
		}
		accountValue := row["account_id"]
		if authorizationMissing(accountValue) {
			accountValue = row["advertiser_id"]
		}
		if authorizationMissing(accountValue) {
			continue
		}
		accountID := fmt.Sprint(accountValue)
		if !authorizationDecimalID(accountID) {
			return nil, nil, fmt.Errorf("account_id must match 0|[1-9][0-9]*: %s", accountID)
		}
		copied := map[string]any{}
		for key, item := range row {
			if allowed[key] {
				copied[key] = cloneAuthorizationValue(item)
			}
		}
		advertisers := []any{}
		role := strings.TrimSpace(fmt.Sprint(row["account_role"]))
		if role == "" || role == "<nil>" {
			role = strings.TrimSpace(fmt.Sprint(row["account_type"]))
		}
		if role == "ADVERTISER" {
			advertisers = append(advertisers, accountID)
			resolved[accountID] = true
		}
		copied["account_id"] = accountID
		copied["advertiser_ids"] = advertisers
		result = append(result, copied)
	}
	return result, resolved, nil
}

func legacyAdvertiserIDs(value any) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	values := authorizationList(value)
	if values == nil {
		return nil, errors.New("authorized_advertiser_ids must be a list")
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		advertiserID := fmt.Sprint(raw)
		if !authorizationDecimalID(advertiserID) {
			return nil, fmt.Errorf("advertiser_id must match 0|[1-9][0-9]*: %s", advertiserID)
		}
		result = append(result, advertiserID)
	}
	return uniqueAuthorizationStrings(result), nil
}

func rebuildAuthorizationMigrationIndexes(state map[string]any) error {
	authorizations, err := authorizationMigrationObject(state, "authorizations")
	if err != nil {
		return err
	}
	accountIndex := map[string]any{}
	advertiserIndex := map[string]any{}
	ids := make([]string, 0, len(authorizations))
	for authorizationID := range authorizations {
		ids = append(ids, authorizationID)
	}
	sort.Strings(ids)
	for _, authorizationID := range ids {
		metadata, ok := authorizations[authorizationID].(map[string]any)
		if !ok {
			return fmt.Errorf("authorization %s must be an object", authorizationID)
		}
		active := []string{}
		for _, raw := range authorizationList(metadata["authorized_accounts"]) {
			account, ok := raw.(map[string]any)
			if !ok {
				return errors.New("authorized account must be an object")
			}
			accountID := fmt.Sprint(account["account_id"])
			if !authorizationDecimalID(accountID) {
				return fmt.Errorf("account_id must match 0|[1-9][0-9]*: %s", accountID)
			}
			if owner := strings.TrimSpace(fmt.Sprint(accountIndex[accountID])); owner != "" && owner != "<nil>" && owner != authorizationID {
				return fmt.Errorf("authorized account %s belongs to multiple authorizations", accountID)
			}
			accountIndex[accountID] = authorizationID
			advertisers, err := authorizationDecimalStrings(account["advertiser_ids"], "advertiser_id")
			if err != nil {
				return err
			}
			active = append(active, advertisers...)
		}
		active = uniqueAuthorizationStrings(active)
		metadata["advertiser_ids"] = authorizationStringsToAny(active)
		for _, advertiserID := range active {
			owners := authorizationStrings(advertiserIndex[advertiserID])
			owners = uniqueAuthorizationStrings(append(owners, authorizationID))
			advertiserIndex[advertiserID] = authorizationStringsToAny(owners)
		}
	}
	state["account_index"] = accountIndex
	state["advertiser_index"] = advertiserIndex
	return nil
}

func authorizationMigrationObject(parent map[string]any, key string) (map[string]any, error) {
	if parent[key] == nil {
		created := map[string]any{}
		parent[key] = created
		return created, nil
	}
	result, ok := parent[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return result, nil
}

func authorizationNonNegativeInteger(value any) (int, error) {
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Int64()
		if err != nil || parsed < 0 {
			return 0, errors.New("value must be non-negative")
		}
		return int(parsed), nil
	}
	parsed, err := strconvAuthorizationInteger(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("value must be non-negative")
	}
	return parsed, nil
}

func strconvAuthorizationInteger(value any) (int, error) {
	return strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
}

func authorizationStringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func authorizationMissing(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		trimmed := strings.TrimSpace(text)
		return trimmed == "" || strings.HasPrefix(trimmed, "REPLACE_WITH")
	}
	return false
}

func cloneAuthorizationMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return cloneAuthorizationValue(value).(map[string]any)
}

func cloneAuthorizationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneAuthorizationValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneAuthorizationValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
