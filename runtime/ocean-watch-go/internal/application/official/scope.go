package official

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

func (service Service) resolveScope(state map[string]any, query contracts.ApprovedOfficialQuery, request contracts.OfficialQueryRequest, arguments map[string]json.RawMessage) (domain.AuthorizationBinding, error) {
	accountID := request.AuthAccountID
	authorizationID := ""
	if request.Scope.Kind == "authorization" {
		authorizationID = request.Scope.ID
	} else if request.Scope.Kind != "advertiser" && request.Scope.Kind != "public_catalog" && accountID == "" {
		accountID = request.Scope.ID
	}
	binding, domainErr := domain.ResolveAuthorization("qianchuan", state, scopeAdvertiser(request.Scope), accountID, authorizationID, false)
	if domainErr != nil {
		switch domainErr.Code {
		case "authorization_ambiguous":
			return binding, failure("AUTHORIZATION_AMBIGUOUS", "scope", false)
		case "authorization_pending_sync":
			return binding, failure("ACCOUNT_SYNC_REQUIRED", "scope", false)
		default:
			return binding, failure("SCOPE_DENIED", "scope", false)
		}
	}
	if service.Policy.CheckAuthorization("qianchuan", binding.AuthorizationID) != nil {
		return binding, failure("SCOPE_DENIED", "scope", false)
	}
	metadata := configuration.Object(configuration.Object(state["authorizations"])[binding.AuthorizationID])
	accounts := configuration.List(metadata["authorized_accounts"])
	if request.Scope.Kind != "advertiser" && request.Scope.Kind != "authorization" && request.Scope.Kind != "public_catalog" {
		if !contains(binding.AccountIDs, request.Scope.ID) || !scopeRoleKnown(accounts, request.Scope.Kind, request.Scope.ID) {
			return binding, failure("ACCOUNT_SCOPE_UNVERIFIED", "scope", false)
		}
	}
	for _, rule := range query.SubjectRules {
		raw, present := arguments[strings.Split(rule.Path, ".")[0]]
		if !present {
			continue
		}
		var root any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if decoder.Decode(&root) != nil {
			return binding, failure("INVALID_PARAMETERS", "scope", false)
		}
		parts := strings.Split(rule.Path, ".")[1:]
		values := subjectValues(root, parts)
		for _, value := range values {
			id := subjectID(value)
			if id == "" {
				return binding, failure("SCOPE_DENIED", "scope", false)
			}
			kind := rule.Kind
			if kind == "account" && contains(binding.AdvertiserIDs, id) {
				kind = "advertiser"
			}
			if kind == "advertiser" {
				if request.Scope.Kind == "advertiser" && id != request.Scope.ID || !contains(binding.AdvertiserIDs, id) {
					return binding, failure("SCOPE_DENIED", "scope", false)
				}
				if accountID != "" && !advertiserBelongsToAccount(accounts, accountID, id) {
					return binding, failure("SCOPE_DENIED", "scope", false)
				}
			} else if !contains(binding.AccountIDs, id) || !scopeRoleKnown(accounts, kind, id) {
				return binding, failure("ACCOUNT_SCOPE_UNVERIFIED", "scope", false)
			}
			allowed := service.Policy.CheckSubject("qianchuan", kind, id) == nil
			if kind == "advertiser" && request.Scope.Kind != "advertiser" && request.Scope.Kind != "authorization" && advertiserBelongsToAccount(accounts, request.Scope.ID, id) {
				allowed = service.Policy.CheckDescendant("qianchuan", request.Scope.Kind, request.Scope.ID, "advertiser", id) == nil
			}
			if !allowed {
				return binding, failure("SCOPE_DENIED", "scope", false)
			}
		}
	}
	return binding, nil
}

func subjectValues(value any, path []string) []any {
	if len(path) == 0 {
		if values, ok := value.([]any); ok {
			return values
		}
		return []any{value}
	}
	if path[0] == "*" {
		result := []any{}
		if values, ok := value.([]any); ok {
			for _, value := range values {
				result = append(result, subjectValues(value, path[1:])...)
			}
		}
		return result
	}
	if object, ok := value.(map[string]any); ok {
		if child, present := object[path[0]]; present {
			return subjectValues(child, path[1:])
		}
	}
	return nil
}

func subjectID(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func scopeRoleKnown(accounts []any, kind, id string) bool {
	for _, value := range accounts {
		account := configuration.Object(value)
		if fmt.Sprint(account["account_id"]) != id {
			continue
		}
		role, _ := account["account_role"].(string)
		if role == "" {
			role, _ = account["account_type"].(string)
		}
		switch kind {
		case "agent":
			return contains([]string{"AGENT", "CHILD_AGENT", "AGENT_SYSTEM_ACCOUNT", "PLATFORM_ROLE_QIANCHUAN_AGENT"}, role)
		case "shop":
			return role == "PLATFORM_ROLE_SHOP_ACCOUNT"
		case "customer_center":
			return role == "CUSTOMER_ADMIN" || role == "CUSTOMER_OPERATOR"
		case "organization":
			return role == "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN" || role == "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR"
		case "account":
			return role != ""
		}
	}
	return false
}

func advertiserBelongsToAccount(accounts []any, accountID, advertiserID string) bool {
	for _, value := range accounts {
		account := configuration.Object(value)
		if fmt.Sprint(account["account_id"]) != accountID {
			continue
		}
		if accountID == advertiserID {
			return true
		}
		for _, id := range configuration.List(account["advertiser_ids"]) {
			if fmt.Sprint(id) == advertiserID {
				return true
			}
		}
	}
	return false
}

func verifyRequestedPlans(query contracts.ApprovedOfficialQuery, arguments map[string]json.RawMessage, data any) error {
	wanted := map[string]bool{}
	for _, name := range []string{"ad_id", "ad_ids"} {
		if encoded := arguments[name]; len(encoded) > 0 {
			decoder := json.NewDecoder(strings.NewReader(string(encoded)))
			decoder.UseNumber()
			var value any
			if decoder.Decode(&value) != nil {
				return failure("INVALID_PARAMETERS", "scope", false)
			}
			for _, id := range subjectValues(value, nil) {
				wanted[subjectID(id)] = true
			}
		}
	}
	if len(wanted) == 0 || data == nil {
		return nil
	}
	var verify func(any) error
	verify = func(value any) error {
		switch typed := value.(type) {
		case map[string]any:
			for name, child := range typed {
				if name == "ad_id" || name == "ad_ids" {
					for _, id := range subjectValues(child, nil) {
						if id != nil && !wanted[subjectID(id)] {
							return failure("PLAN_SCOPE_MISMATCH", "projection", true)
						}
					}
				}
				if err := verify(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := verify(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if query.QueryID == "qianchuan_ad_detail_get_v1" || query.QueryID == "qianchuan_uni_promotion_ad_detail_v1" {
		id, exists := readValue(data, []string{"ad_id"})
		if !exists || !wanted[subjectID(id)] {
			return failure("PLAN_SCOPE_MISMATCH", "projection", true)
		}
	}
	return verify(data)
}

func (service Service) verifyResponseScope(value any, scope contracts.OfficialQueryScope, authorized []string, accounts []any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "advertiser_id" || key == "advertiser_ids" {
				for _, value := range subjectValues(child, nil) {
					if value == nil {
						continue
					}
					id := subjectID(value)
					allowed := service.Policy.CheckSubject("qianchuan", "advertiser", id) == nil
					if scope.Kind != "advertiser" && scope.Kind != "authorization" && scope.Kind != "public_catalog" {
						if !advertiserBelongsToAccount(accounts, scope.ID, id) {
							return failure("RESPONSE_SCOPE_MISMATCH", "projection", true)
						}
						allowed = service.Policy.CheckDescendant("qianchuan", scope.Kind, scope.ID, "advertiser", id) == nil
					}
					if id == "" || !contains(authorized, id) || scope.Kind == "advertiser" && id != scope.ID || !allowed {
						return failure("RESPONSE_SCOPE_MISMATCH", "projection", true)
					}
				}
			}
			if contracts.OfficialSubjectKind(key) == scope.Kind && scope.Kind != "advertiser" && scope.Kind != "authorization" && scope.Kind != "public_catalog" {
				for _, value := range subjectValues(child, nil) {
					if value != nil && subjectID(value) != scope.ID {
						return failure("RESPONSE_SCOPE_MISMATCH", "projection", true)
					}
				}
			}
			if err := service.verifyResponseScope(child, scope, authorized, accounts); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := service.verifyResponseScope(child, scope, authorized, accounts); err != nil {
				return err
			}
		}
	}
	return nil
}
