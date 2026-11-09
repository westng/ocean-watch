package oceanengine

import (
	"errors"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func effectiveAccountRole(account domain.AuthorizedAccount) string {
	if account.AccountRole != "" {
		return account.AccountRole
	}
	return account.AccountType
}

func safeAccountRole(role string) string {
	switch role {
	case "", "ADVERTISER", "PLATFORM_ROLE_AWEME", "CUSTOMER_ADMIN", "CUSTOMER_OPERATOR",
		"PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR",
		"PLATFORM_ROLE_SHOP_ACCOUNT", "PLATFORM_ROLE_QIANCHUAN_AGENT", "PLATFORM_ROLE_STAR",
		"PLATFORM_ROLE_STAR_AGENT", "PLATFORM_ROLE_STAR_MCN", "PLATFORM_ROLE_STAR_ISV":
		return role
	default:
		return "UNKNOWN"
	}
}

func nonAdvertisingRole(channel, role, accountID string) *domain.AccountDiscoveryIssue {
	issue := &domain.AccountDiscoveryIssue{AccountID: accountID, Role: role, Code: "non_channel_role"}
	switch role {
	case "PLATFORM_ROLE_AWEME":
		issue.Code, issue.Reason = "non_advertiser_role", "account_role_has_no_advertiser_expansion"
	case "PLATFORM_ROLE_SHOP_ACCOUNT", "PLATFORM_ROLE_QIANCHUAN_AGENT":
		if channel == "qianchuan" {
			return nil
		}
		issue.Reason = "account_role_belongs_to_qianchuan"
	case "PLATFORM_ROLE_STAR", "PLATFORM_ROLE_STAR_AGENT", "PLATFORM_ROLE_STAR_MCN", "PLATFORM_ROLE_STAR_ISV":
		if channel == "star_map" {
			return nil
		}
		issue.Reason = "account_role_belongs_to_star_map"
	default:
		return nil
	}
	return issue
}

func mapAuthorizedAccount(channel string, row map[string]any) (*domain.AuthorizedAccount, *domain.AccountDiscoveryIssue, error) {
	accountRole, accountType := safeAccountRole(discoveryText(row["account_role"])), safeAccountRole(discoveryText(row["account_type"]))
	role := accountRole
	if role == "" {
		role = accountType
	}
	if channel == "star_map" && role != "PLATFORM_ROLE_STAR" && role != "PLATFORM_ROLE_STAR_AGENT" && role != "PLATFORM_ROLE_STAR_MCN" && role != "PLATFORM_ROLE_STAR_ISV" {
		return nil, nil, nil
	}
	if issue := nonAdvertisingRole(channel, role, discoveryID(row, "account_id", "account_string_id", "advertiser_id")); issue != nil && issue.Code == "non_channel_role" {
		return nil, issue, nil
	}
	var valid *bool
	if value, present := row["is_valid"]; present && value != nil {
		parsed, ok := value.(bool)
		if !ok {
			switch discoveryText(value) {
			case "1", "true":
				parsed = true
			case "0", "false":
				parsed = false
			default:
				return nil, nil, discoveryFailure(authorizedAccountsEndpoint, "INVALID_ACCOUNT_VALIDITY", errors.New("invalid authorized account validity"))
			}
		}
		valid = &parsed
	}
	accountID := discoveryID(row, "account_id", "account_string_id", "advertiser_id")
	if accountID == "" {
		// Douyin sec_uid is not a decimal advertiser identity. Record its role,
		// never turn it into a decimal mapping or abort other advertising roots.
		if issue := nonAdvertisingRole(channel, role, ""); issue != nil {
			return nil, issue, nil
		}
		if role == "UNKNOWN" || role == "" {
			return nil, &domain.AccountDiscoveryIssue{Role: "UNKNOWN", Code: "unsupported_account_role", Reason: "account_role_expansion_not_supported"}, nil
		}
		if valid != nil && !*valid {
			return nil, nil, nil
		}
		return nil, nil, errors.New("missing decimal authorized account identity")
	}
	account := &domain.AuthorizedAccount{
		AccountID: accountID, AccountStringID: discoveryText(row["account_string_id"]),
		AccountRole: accountRole, AccountType: accountType, IsValid: valid,
		AccountName: discoveryText(row["account_name"]), AdvertiserName: discoveryText(row["advertiser_name"]),
	}
	if id := discoveryID(row, "advertiser_id"); id != "" {
		account.AdvertiserIDs = []string{id}
	}
	if role == "PLATFORM_ROLE_SHOP_ACCOUNT" {
		account.ShopID = accountID
	}
	return account, nil, nil
}

func roleEndpoint(account domain.AuthorizedAccount) string {
	switch effectiveAccountRole(account) {
	case "CUSTOMER_ADMIN", "CUSTOMER_OPERATOR":
		return customerAccountsEndpoint
	case "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR":
		return enterpriseAccountsEndpoint
	case "PLATFORM_ROLE_QIANCHUAN_AGENT":
		return "/2/agent/advertiser/select/"
	case "PLATFORM_ROLE_SHOP_ACCOUNT":
		return "/v1.0/qianchuan/shop/advertiser/list/"
	default:
		return authorizedAccountsEndpoint
	}
}
