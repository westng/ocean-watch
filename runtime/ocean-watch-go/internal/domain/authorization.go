package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	AppCredentialAccountPrefix           = "oceanengine-app"
	AuthorizationCredentialAccountPrefix = "oceanengine-auth"
)

type AuthorizationSummary struct {
	AuthorizationID          string
	TokenRevision            int
	AccountIDs               []string
	AdvertiserIDs            []string
	PendingAccountSync       bool
	AccountDiscoveryIssues   []any
	AccountDiscoveryComplete bool
}

type AuthorizationState struct {
	AuthorizationCount           int
	AuthorizedAccountCount       int
	AdvertiserIDs                []string
	Generation                   int
	PendingAccountSyncCount      int
	PartialAccountDiscoveryCount int
	Authorizations               []AuthorizationSummary
}

type AuthorizationBinding struct {
	Channel            string
	AuthorizationID    string
	TokenRevision      int
	CredentialAccount  string
	PendingAccountSync bool
	AccountIDs         []string
	AdvertiserIDs      []string
}

func AppCredentialAccount(channel string) (string, error) {
	if err := validateAuthorizationChannel(channel); err != nil {
		return "", err
	}
	return AppCredentialAccountPrefix + "-" + channel, nil
}

func AuthorizationCredentialAccount(channel, authorizationID string, revision int) (string, error) {
	if err := validateAuthorizationChannel(channel); err != nil {
		return "", err
	}
	if strings.TrimSpace(authorizationID) == "" || strings.ContainsAny(authorizationID, "\x00\r\n/") {
		return "", errors.New("authorization_id is invalid")
	}
	if revision < 1 {
		return "", errors.New("token_revision must be positive")
	}
	return fmt.Sprintf("%s-%s-%s-r%d", AuthorizationCredentialAccountPrefix, channel, authorizationID, revision), nil
}

func ResolveAuthorization(
	channel string,
	state map[string]any,
	advertiserID string,
	authAccountID string,
	authorizationID string,
	allowPending bool,
) (AuthorizationBinding, *Error) {
	if err := validateAuthorizationChannel(channel); err != nil {
		return AuthorizationBinding{}, NewError("unknown_channel", err.Error(), 2, map[string]any{"channel": channel})
	}
	authorizations := authorizationObject(state["authorizations"])
	accountIndex := authorizationObject(state["account_index"])
	advertiserIndex := authorizationObject(state["advertiser_index"])

	candidates := []string{}
	switch {
	case authorizationID != "":
		if _, exists := authorizations[authorizationID]; exists {
			candidates = append(candidates, authorizationID)
		}
	case authAccountID != "":
		if !authorizationDecimalID(authAccountID) {
			return AuthorizationBinding{}, NewError("invalid_account_id", "account_id must match 0|[1-9][0-9]*", 2, nil)
		}
		if owner := strings.TrimSpace(fmt.Sprint(accountIndex[authAccountID])); owner != "" && owner != "<nil>" {
			candidates = append(candidates, owner)
		}
	case advertiserID != "":
		if !authorizationDecimalID(advertiserID) {
			return AuthorizationBinding{}, NewError("invalid_advertiser_id", "advertiser_id must match 0|[1-9][0-9]*", 2, nil)
		}
		candidates = append(candidates, authorizationStrings(advertiserIndex[advertiserID])...)
	default:
		for candidate := range authorizations {
			candidates = append(candidates, candidate)
		}
		sort.Strings(candidates)
	}
	candidates = uniqueAuthorizationStrings(candidates)
	if len(candidates) == 0 {
		pending := pendingAuthorizationIDs(authorizations)
		if len(pending) != 0 {
			return AuthorizationBinding{}, NewError(
				"legacy_authorization_pending_sync",
				fmt.Sprintf("%s legacy authorization requires an authorized-account sync", channel),
				1,
				map[string]any{"authorization_ids": pending},
			)
		}
		return AuthorizationBinding{}, NewError(
			"authorization_not_found",
			fmt.Sprintf("no %s authorization covers the requested account", channel),
			1,
			map[string]any{"advertiser_id": optionalAuthorizationString(advertiserID)},
		)
	}
	if len(candidates) > 1 {
		return AuthorizationBinding{}, NewError(
			"authorization_ambiguous",
			fmt.Sprintf("multiple %s authorizations cover advertiser %s", channel, advertiserID),
			1,
			map[string]any{"authorization_ids": candidates},
		)
	}
	selectedID := candidates[0]
	metadata := authorizationObject(authorizations[selectedID])
	pending := authorizationBool(metadata["pending_account_sync"])
	if pending && !allowPending {
		return AuthorizationBinding{}, NewError(
			"legacy_authorization_pending_sync",
			fmt.Sprintf("%s authorization %s requires an authorized-account sync", channel, selectedID),
			1,
			map[string]any{"authorization_ids": []string{selectedID}},
		)
	}
	coveredAdvertisers, err := authorizationDecimalStrings(metadata["advertiser_ids"], "advertiser_id")
	if err != nil {
		return AuthorizationBinding{}, NewError("authorization_state_invalid", err.Error(), 1, nil)
	}
	accountIDs, err := authorizationAccountIDs(metadata["authorized_accounts"])
	if err != nil {
		return AuthorizationBinding{}, NewError("authorization_state_invalid", err.Error(), 1, nil)
	}
	if advertiserID != "" && !(allowPending && pending) {
		advertiserScope := coveredAdvertisers
		if authAccountID != "" {
			advertiserScope, err = authorizationAdvertisersForAccount(metadata["authorized_accounts"], authAccountID)
			if err != nil {
				return AuthorizationBinding{}, NewError("authorization_state_invalid", err.Error(), 1, nil)
			}
		}
		if !authorizationContains(advertiserScope, advertiserID) {
			return AuthorizationBinding{}, NewError(
				"authorized_account_not_found",
				fmt.Sprintf("authorization %s does not cover advertiser %s", selectedID, advertiserID),
				1,
				nil,
			)
		}
	}
	revision := 1
	if value, exists := metadata["token_revision"]; exists && value != nil {
		revision, err = authorizationPositiveInteger(value)
		if err != nil {
			return AuthorizationBinding{}, NewError("authorization_state_invalid", "token_revision must be positive", 1, nil)
		}
	}
	credentialAccount, credentialErr := AuthorizationCredentialAccount(channel, selectedID, revision)
	if credentialErr != nil {
		return AuthorizationBinding{}, NewError("authorization_state_invalid", credentialErr.Error(), 1, nil)
	}
	return AuthorizationBinding{
		Channel: channel, AuthorizationID: selectedID, TokenRevision: revision,
		CredentialAccount: credentialAccount, PendingAccountSync: pending,
		AccountIDs: accountIDs, AdvertiserIDs: coveredAdvertisers,
	}, nil
}

func validateAuthorizationChannel(channel string) error {
	if channel != "marketing" && channel != "qianchuan" {
		return fmt.Errorf("unsupported authorization channel: %s", channel)
	}
	return nil
}

func authorizationObject(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func authorizationStrings(value any) []string {
	result := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, values...)
	case string:
		if strings.TrimSpace(values) != "" {
			result = append(result, values)
		}
	}
	return result
}

func authorizationDecimalStrings(value any, field string) ([]string, error) {
	values := authorizationStrings(value)
	for _, item := range values {
		if !authorizationDecimalID(item) {
			return nil, fmt.Errorf("%s must match 0|[1-9][0-9]*", field)
		}
	}
	return uniqueAuthorizationStrings(values), nil
}

func authorizationAccountIDs(value any) ([]string, error) {
	result := []string{}
	for _, item := range authorizationList(value) {
		row := authorizationObject(item)
		if row["account_id"] == nil {
			continue
		}
		accountID := strings.TrimSpace(fmt.Sprint(row["account_id"]))
		if !authorizationDecimalID(accountID) {
			return nil, errors.New("account_id must match 0|[1-9][0-9]*")
		}
		result = append(result, accountID)
	}
	return uniqueAuthorizationStrings(result), nil
}

func authorizationAdvertisersForAccount(value any, accountID string) ([]string, error) {
	for _, item := range authorizationList(value) {
		row := authorizationObject(item)
		if strings.TrimSpace(fmt.Sprint(row["account_id"])) == accountID {
			return authorizationDecimalStrings(row["advertiser_ids"], "advertiser_id")
		}
	}
	return []string{}, nil
}

func authorizationList(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func authorizationPositiveInteger(value any) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || parsed < 1 {
		return 0, errors.New("value must be positive")
	}
	return parsed, nil
}

func authorizationDecimalID(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func authorizationContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueAuthorizationStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func pendingAuthorizationIDs(authorizations map[string]any) []string {
	result := []string{}
	for authorizationID, raw := range authorizations {
		if authorizationBool(authorizationObject(raw)["pending_account_sync"]) {
			result = append(result, authorizationID)
		}
	}
	sort.Strings(result)
	return result
}

func authorizationBool(value any) bool {
	result, _ := value.(bool)
	return result
}

func optionalAuthorizationString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
