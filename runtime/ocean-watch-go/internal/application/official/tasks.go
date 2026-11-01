package official

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

type ManagedAccountReader interface {
	Read(context.Context) (domain.AccountBook, error)
}
type MaterialLocator interface {
	Resolve(context.Context, string, string, []MaterialWork) ([]string, error)
}

type Tasks struct {
	Queries   Service
	Accounts  ManagedAccountReader
	Materials MaterialLocator
}

type TaskResult struct {
	Task              string                           `json:"task"`
	Source            string                           `json:"source"`
	Status            string                           `json:"status"`
	AdvertiserID      string                           `json:"advertiser_id,omitempty"`
	PlanID            string                           `json:"plan_id,omitempty"`
	Scene             string                           `json:"scene,omitempty"`
	Facts             []contracts.OfficialQueryResult  `json:"facts"`
	Failures          []contracts.OfficialQueryFailure `json:"failures"`
	Skipped           []TaskSkipped                    `json:"skipped"`
	HasMore           bool                             `json:"has_more"`
	Cursor            string                           `json:"cursor,omitempty"`
	MaterialLabels    []MaterialLabel                  `json:"material_labels,omitempty"`
	TotalAccounts     int                              `json:"total_accounts,omitempty"`
	ProcessedAccounts int                              `json:"processed_accounts,omitempty"`
}

type TaskSkipped struct {
	QueryID string `json:"query_id"`
	Reason  string `json:"reason"`
}
type MaterialLabel struct {
	MaterialID  string `json:"material_id"`
	Original    *bool  `json:"original"`
	Inefficient *bool  `json:"inefficient"`
}

type HealthRequest struct {
	AdvertiserID    string   `json:"advertiser_id,omitempty"`
	AuthAccountID   string   `json:"auth_account_id,omitempty"`
	ManagedAccounts bool     `json:"managed_accounts,omitempty"`
	Checks          []string `json:"checks,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	Cursor          string   `json:"cursor,omitempty"`
}

type PlanDiagnosisRequest struct {
	AdvertiserID  string `json:"advertiser_id"`
	AuthAccountID string `json:"auth_account_id,omitempty"`
	PlanID        string `json:"plan_id"`
	Scene         string `json:"scene,omitempty"`
}

type MaterialWork struct {
	URL             string `json:"url"`
	ProductID       string `json:"product_id,omitempty"`
	CreatorUID      string `json:"creator_uid,omitempty"`
	CreatorDouyinID string `json:"creator_douyin_id,omitempty"`
	CreatorName     string `json:"creator_name,omitempty"`
}
type MaterialQualityRequest struct {
	AdvertiserID  string         `json:"advertiser_id"`
	AuthAccountID string         `json:"auth_account_id,omitempty"`
	MaterialIDs   []string       `json:"material_ids,omitempty"`
	Works         []MaterialWork `json:"works,omitempty"`
}

type SuggestionRequest struct {
	AdvertiserID  string                     `json:"advertiser_id"`
	AuthAccountID string                     `json:"auth_account_id,omitempty"`
	Kind          string                     `json:"kind"`
	Scene         string                     `json:"scene"`
	PlanID        string                     `json:"plan_id,omitempty"`
	Parameters    map[string]json.RawMessage `json:"parameters,omitempty"`
}

func taskResult(task, advertiser string) TaskResult {
	return TaskResult{Task: task, Source: "official_mcp", Status: "ok", AdvertiserID: advertiser, Facts: []contracts.OfficialQueryResult{}, Failures: []contracts.OfficialQueryFailure{}, Skipped: []TaskSkipped{}}
}

func (tasks Tasks) appendFact(ctx context.Context, result *TaskResult, advertiser, authAccount, queryID string, parameters map[string]json.RawMessage, profile []string) {
	if parameters == nil {
		parameters = map[string]json.RawMessage{}
	}
	fact, err := tasks.Queries.query(ctx, contracts.OfficialQueryRequest{QueryID: queryID, Scope: contracts.OfficialQueryScope{Kind: "advertiser", ID: advertiser}, AuthAccountID: authAccount, Parameters: parameters, Limit: 100}, profile)
	if err != nil {
		code, stage, dispatched := "QUERY_FAILED", "query", false
		authorizationID := ""
		apiCode := ""
		requiredFields := []string(nil)
		var failure *port.Failure
		if errors.As(err, &failure) {
			code, stage, dispatched = failure.Code, failure.Stage, failure.Dispatched
			authorizationID = failure.AuthorizationID
			apiCode = failure.APICode
			requiredFields = append(requiredFields, failure.RequiredFields...)
		}
		if ctx.Err() != nil {
			code = "CANCELLED"
		}
		result.Failures = append(result.Failures, contracts.OfficialQueryFailure{QueryID: queryID, Code: code, Stage: stage, Dispatched: dispatched, Scope: &contracts.OfficialQueryScope{Kind: "advertiser", ID: advertiser}, AuthorizationID: authorizationID, APICode: apiCode, RequiredFields: requiredFields})
		result.Status = "partial"
		return
	}
	if !fact.Completeness.Complete {
		result.Status = "partial"
	}
	if !healthFactKnown(fact) {
		result.Status = "partial"
		result.Failures = append(result.Failures, contracts.OfficialQueryFailure{QueryID: queryID, Code: "REQUIRED_OUTPUT_MISSING", Stage: "projection", Dispatched: true, Scope: &contracts.OfficialQueryScope{Kind: "advertiser", ID: advertiser}})
	}
	result.Facts = append(result.Facts, fact)
}

func (tasks Tasks) Health(ctx context.Context, request HealthRequest) (TaskResult, error) {
	result := taskResult("account_health", request.AdvertiserID)
	if ctx == nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if tasks.Queries.Policy.CheckCapability("qianchuan.account_health") != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if request.ManagedAccounts == (request.AdvertiserID != "") {
		return result, failure("EXACT_ACCOUNT_SCOPE_REQUIRED", "scope", false)
	}
	if len(request.Cursor) > 4096 || !request.ManagedAccounts && !validTaskID(request.AdvertiserID) || request.ManagedAccounts && request.AuthAccountID != "" {
		return result, failure("INVALID_ARGUMENT", "scope", false)
	}
	if request.Limit == 0 {
		request.Limit = 5
	}
	if request.Limit < 1 || request.Limit > 20 {
		return result, failure("INVALID_ARGUMENT", "scope", false)
	}
	checks := request.Checks
	if len(checks) == 0 {
		checks = []string{"balance", "budget", "quota"}
	}
	checkQueries := map[string]string{"balance": "qianchuan_account_balance_get_v1", "budget": "qianchuan_account_budget_get_v1", "quota": "qianchuan_ad_quota_get_v1"}
	profile := []string{}
	seen := map[string]bool{}
	for _, check := range checks {
		query, ok := checkQueries[check]
		if !ok || seen[check] {
			return result, failure("INVALID_ARGUMENT", "checks", false)
		}
		seen[check] = true
		profile = append(profile, query)
	}
	accounts := []domain.ManagedAccount{{AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID}}
	if request.ManagedAccounts {
		if tasks.Accounts == nil {
			return result, failure("BUSINESS_CONFIGURATION_REQUIRED", "scope", false)
		}
		book, err := tasks.Accounts.Read(ctx)
		if err != nil {
			return result, failure("MANAGED_ACCOUNTS_UNAVAILABLE", "scope", false)
		}
		accounts = []domain.ManagedAccount{}
		for _, account := range book.Accounts[domain.Qianchuan] {
			if account.Enabled {
				accounts = append(accounts, account)
			}
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].AdvertiserID < accounts[j].AdvertiserID })
	}
	binding, err := tasks.healthBinding(ctx, accounts, checks, request.Limit)
	if err != nil {
		return result, err
	}
	offset, err := decodeTaskCursor(binding, request.Cursor, len(accounts))
	if err != nil || offset > len(accounts) {
		return result, failure("INVALID_CURSOR", "scope", false)
	}
	end := min(offset+request.Limit, len(accounts))
	result.TotalAccounts = len(accounts)
	result.ProcessedAccounts = end - offset
	result.HasMore = end < len(accounts)
	if result.HasMore {
		result.Cursor = encodeTaskCursor(binding, end)
		result.Status = "partial"
	}
	for _, account := range accounts[offset:end] {
		for _, query := range profile {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			tasks.appendFact(ctx, &result, account.AdvertiserID, account.AuthAccountID, query, nil, profile)
		}
	}
	return finalizeTaskResult(result), nil
}

func (tasks Tasks) healthBinding(ctx context.Context, accounts []domain.ManagedAccount, checks []string, limit int) (string, error) {
	identities := [][3]string{}
	if len(accounts) > 0 {
		if tasks.Queries.Authorizations == nil {
			return "", failure("BUSINESS_CONFIGURATION_REQUIRED", "scope", false)
		}
		state, err := tasks.Queries.Authorizations.LoadChannel(ctx, "qianchuan")
		if err != nil {
			return "", failure("ACCOUNT_SYNC_REQUIRED", "scope", false)
		}
		for _, account := range accounts {
			grant := "unavailable"
			if resolved, err := domain.ResolveAuthorization("qianchuan", state, account.AdvertiserID, account.AuthAccountID, "", false); err == nil {
				grant = resolved.AuthorizationID
			}
			identities = append(identities, [3]string{account.AdvertiserID, account.AuthAccountID, grant})
		}
	}
	payload, _ := json.Marshal([]any{"account_health", identities, checks, limit, tasks.Queries.Policy.Snapshot(), contracts.OfficialQueryContractVersion})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func finalizeTaskResult(result TaskResult) TaskResult {
	if len(result.Facts) == 0 {
		if len(result.Failures) > 0 {
			result.Status = "failed"
		} else if !result.HasMore {
			result.Status = "no_data"
		}
	}
	return result
}

func (tasks Tasks) Diagnose(ctx context.Context, request PlanDiagnosisRequest) (TaskResult, error) {
	result := taskResult("plan_diagnosis", request.AdvertiserID)
	if ctx == nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	result.PlanID = request.PlanID
	if tasks.Queries.Policy.CheckCapability("qianchuan.plan_diagnose") != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if !validTaskID(request.PlanID) {
		return result, failure("INVALID_ARGUMENT", "plan", false)
	}
	if request.Scene != "" && request.Scene != "uni" && request.Scene != "overall" && request.Scene != "legacy" {
		return result, failure("INVALID_ARGUMENT", "scene", false)
	}
	parameters := map[string]json.RawMessage{"ad_id": raw(request.PlanID)}
	detailQuery := "qianchuan_uni_promotion_ad_detail_v1"
	if request.Scene == "legacy" {
		detailQuery = "qianchuan_ad_detail_get_v1"
	}
	tasks.appendFact(ctx, &result, request.AdvertiserID, request.AuthAccountID, detailQuery, parameters, []string{detailQuery})
	if len(result.Facts) == 0 {
		return finalizeTaskResult(result), nil
	}
	detail := result.Facts[0]
	if len(detail.Items) != 1 || cellValue(detail.Items[0]["ad_id"]) != request.PlanID {
		return result, failure("PLAN_SCOPE_MISMATCH", "scene", true)
	}
	scene := ""
	if detailQuery == "qianchuan_ad_detail_get_v1" {
		lab := cellValue(detail.Items[0]["lab_ad_type"])
		if lab == "NOT_LAB_AD" || lab == "LAB_AD" {
			scene = "legacy"
		}
	} else {
		switch cellValue(detail.Items[0]["adlab_scene"]) {
		case "0", "UNI_PROJECT":
			scene = "uni"
		case "1", "OVERALL_PROJECT":
			scene = "overall"
		}
	}
	if scene == "" || request.Scene != "" && scene != request.Scene {
		return result, failure("SCENE_NOT_VERIFIED", "scene", true)
	}
	result.Scene = scene
	if scene == "legacy" {
		queries := []string{"qianchuan_ad_reject_reason_v1", "qianchuan_ad_learing_status_get_v1", "qianchuan_ad_compensate_status_get_v1"}
		for _, query := range queries {
			tasks.appendFact(ctx, &result, request.AdvertiserID, request.AuthAccountID, query, map[string]json.RawMessage{"ad_ids": raw([]string{request.PlanID})}, queries)
		}
	} else {
		query := "qianchuan_uni_promotion_ad_suggestion_v1"
		tasks.appendFact(ctx, &result, request.AdvertiserID, request.AuthAccountID, query, parameters, []string{query})
		result.Skipped = append(result.Skipped, TaskSkipped{QueryID: "qianchuan_ad_learing_status_get_v1", Reason: "legacy_only"}, TaskSkipped{QueryID: "qianchuan_ad_compensate_status_get_v1", Reason: "use_current_plan_detail_compensate_info"})
	}
	return finalizeTaskResult(result), nil
}

func (tasks Tasks) Quality(ctx context.Context, request MaterialQualityRequest) (TaskResult, error) {
	result := taskResult("material_quality", request.AdvertiserID)
	if ctx == nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	if tasks.Queries.Policy.CheckCapability("qianchuan.material_quality") != nil || tasks.Queries.Policy.CheckSubject("qianchuan", "advertiser", request.AdvertiserID) != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if (len(request.MaterialIDs) == 0) == (len(request.Works) == 0) || len(request.MaterialIDs)+len(request.Works) > 100 {
		return result, failure("EXACT_MATERIAL_SCOPE_REQUIRED", "scope", false)
	}
	ids := append([]string(nil), request.MaterialIDs...)
	if len(request.Works) > 0 {
		if tasks.Materials == nil {
			return result, failure("MATERIAL_LOCATOR_UNAVAILABLE", "identity", false)
		}
		resolved, err := tasks.Materials.Resolve(ctx, request.AdvertiserID, request.AuthAccountID, request.Works)
		if err != nil {
			return result, err
		}
		ids = append(ids, resolved...)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validTaskID(id) || seen[id] {
			return result, failure("INVALID_ARGUMENT", "materials", false)
		}
		seen[id] = true
	}
	profile := []string{"qianchuan_file_video_original_get_v1", "qianchuan_file_video_efficiency_get_v1"}
	for _, query := range profile {
		tasks.appendFact(ctx, &result, request.AdvertiserID, request.AuthAccountID, query, map[string]json.RawMessage{"material_ids": raw(ids)}, profile)
	}
	original, originalKnown := labelSet(result.Facts, profile[0], "original_material_ids")
	inefficient, inefficientKnown := labelSet(result.Facts, profile[1], "inefficient_material_ids")
	for _, labels := range []map[string]bool{original, inefficient} {
		for id := range labels {
			if !seen[id] {
				return result, failure("MATERIAL_SCOPE_MISMATCH", "labels", true)
			}
		}
	}
	for _, id := range ids {
		label := MaterialLabel{MaterialID: id}
		if originalKnown {
			value := original[id]
			label.Original = &value
		}
		if inefficientKnown {
			value := inefficient[id]
			label.Inefficient = &value
		}
		result.MaterialLabels = append(result.MaterialLabels, label)
	}
	return finalizeTaskResult(result), nil
}

func (tasks Tasks) Suggestions(ctx context.Context, request SuggestionRequest) (TaskResult, error) {
	result := taskResult("delivery_suggestions", request.AdvertiserID)
	if ctx == nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	result.PlanID = request.PlanID
	result.Scene = request.Scene
	if tasks.Queries.Policy.CheckCapability("qianchuan.delivery_suggestions") != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if request.Scene != "uni" && request.Scene != "overall" && request.Scene != "legacy" {
		return result, failure("EXACT_DELIVERY_SCENE_REQUIRED", "scene", false)
	}
	query := ""
	switch request.Kind {
	case "budget":
		if request.Scene == "legacy" {
			query = "qianchuan_suggest_budget_v1"
		} else {
			query = "qianchuan_uni_aweme_suggest_budget_v1"
		}
	case "roi":
		if request.Scene != "legacy" {
			return result, failure("SCENE_NOT_SUPPORTED", "scene", false)
		}
		query = "qianchuan_suggest_roi_goal_v1"
	case "bid":
		if request.Scene != "legacy" {
			return result, failure("SCENE_NOT_SUPPORTED", "scene", false)
		}
		query = "qianchuan_suggest_bid_v1"
	case "audit":
		if request.Scene == "legacy" {
			query = "qianchuan_ad_reject_reason_v1"
		} else {
			query = "qianchuan_uni_promotion_ad_suggestion_v1"
		}
	default:
		return result, failure("INVALID_ARGUMENT", "suggestion", false)
	}
	parameters := map[string]json.RawMessage{}
	for name, value := range request.Parameters {
		parameters[name] = append(json.RawMessage(nil), value...)
	}
	if request.PlanID != "" {
		if !validTaskID(request.PlanID) {
			return result, failure("INVALID_ARGUMENT", "plan", false)
		}
		detailQuery := "qianchuan_uni_promotion_ad_detail_v1"
		if request.Scene == "legacy" {
			detailQuery = "qianchuan_ad_detail_get_v1"
		}
		fact, err := tasks.Queries.query(ctx, contracts.OfficialQueryRequest{QueryID: detailQuery, Scope: contracts.OfficialQueryScope{Kind: "advertiser", ID: request.AdvertiserID}, AuthAccountID: request.AuthAccountID, Parameters: map[string]json.RawMessage{"ad_id": raw(request.PlanID)}, Limit: 1}, []string{detailQuery})
		if err != nil {
			return result, err
		}
		if len(fact.Items) != 1 || cellValue(fact.Items[0]["ad_id"]) != request.PlanID {
			return result, failure("PLAN_SCOPE_MISMATCH", "scene", true)
		}
		item := fact.Items[0]
		actualScene := ""
		if detailQuery == "qianchuan_ad_detail_get_v1" {
			lab := cellValue(item["lab_ad_type"])
			if lab == "NOT_LAB_AD" || lab == "LAB_AD" {
				actualScene = "legacy"
			}
		} else {
			if cellValue(item["adlab_scene"]) == "0" {
				actualScene = "uni"
			}
			if cellValue(item["adlab_scene"]) == "1" {
				actualScene = "overall"
			}
		}
		if actualScene != request.Scene {
			return result, failure("SCENE_NOT_VERIFIED", "scene", true)
		}
		result.Facts = append(result.Facts, fact)
		for _, name := range []string{"aweme_id", "marketing_goal", "campaign_scene", "marketing_scene", "external_action", "deep_external_action", "deep_bid_type"} {
			if cell := item[name]; cell != nil {
				if supplied, ok := parameters[name]; ok {
					var value string
					if json.Unmarshal(supplied, &value) != nil || value != *cell {
						return result, failure("PLAN_PARAMETER_CONFLICT", "parameters", false)
					}
				} else {
					parameters[name] = raw(*cell)
				}
			}
		}
		if parameters["product_ids"] == nil {
			for _, name := range []string{"product_ids", "product_infos.product_id"} {
				if cell := item[name]; cell != nil {
					var ids []string
					if json.Unmarshal([]byte(*cell), &ids) == nil && len(ids) > 0 {
						parameters["product_ids"] = raw(ids)
						break
					}
				}
			}
		}
		// Only parameters present in the selected frozen contract are inferred.
		contract, _, _ := contracts.ApprovedOfficialQueryByID(query)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(contract.HostInput, &schema)
		for name := range parameters {
			if _, known := schema.Properties[name]; !known {
				if _, supplied := request.Parameters[name]; supplied {
					return result, failure("UNKNOWN_PARAMETER", "parameters", false)
				}
				delete(parameters, name)
			}
		}
		if query == "qianchuan_ad_reject_reason_v1" {
			parameters["ad_ids"] = raw([]string{request.PlanID})
		} else if query == "qianchuan_uni_aweme_suggest_budget_v1" || query == "qianchuan_uni_promotion_ad_suggestion_v1" {
			parameters["ad_id"] = raw(request.PlanID)
		}
	}
	tasks.appendFact(ctx, &result, request.AdvertiserID, request.AuthAccountID, query, parameters, []string{query})
	return finalizeTaskResult(result), nil
}

func healthFactKnown(fact contracts.OfficialQueryResult) bool {
	if len(fact.Items) != 1 {
		return fact.QueryID != "qianchuan_account_balance_get_v1" && fact.QueryID != "qianchuan_account_budget_get_v1" && fact.QueryID != "qianchuan_ad_quota_get_v1"
	}
	item := fact.Items[0]
	switch fact.QueryID {
	case "qianchuan_account_balance_get_v1":
		return item["account_valid"] != nil
	case "qianchuan_account_budget_get_v1":
		mode := cellValue(item["budget_mode"])
		return mode == "INFINITE" || mode == "SPECIFIED" && item["budget"] != nil
	case "qianchuan_ad_quota_get_v1":
		return item["quota_feed.quota_info.total_num"] != nil && item["quota_feed.delivery_info.total_num"] != nil
	default:
		return true
	}
}

func raw(value any) json.RawMessage { encoded, _ := json.Marshal(value); return encoded }
func cellValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func validTaskID(id string) bool {
	return integerToken.MatchString(id) && id != "" && id[0] != '0' && len(id) <= 19 && (len(id) < 19 || id <= "9223372036854775807")
}
func labelSet(facts []contracts.OfficialQueryResult, query, field string) (map[string]bool, bool) {
	result := map[string]bool{}
	for _, fact := range facts {
		if fact.QueryID != query {
			continue
		}
		if !fact.Completeness.Complete {
			return result, false
		}
		for _, item := range fact.Items {
			value := cellValue(item[field])
			if value != "" {
				result[value] = true
			}
		}
		return result, true
	}
	return result, false
}
