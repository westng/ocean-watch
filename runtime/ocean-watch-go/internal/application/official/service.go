// Package official implements query discovery and execution in shared Core.
package official

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

type Service struct {
	Policy         policy.Instance
	Authorizations auth.AuthorizationStore
	Tokens         auth.TokenProvider
	Backend        port.Backend
	AppTokens      port.AppTokenProvider
	Now            func() time.Time
}

type CatalogQuery struct {
	QueryID string `json:"query_id,omitempty"`
	Group   string `json:"group,omitempty"`
	Keyword string `json:"keyword,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

type CatalogItem struct {
	QueryID               string                              `json:"query_id"`
	Title                 string                              `json:"title"`
	Group                 string                              `json:"group"`
	Status                string                              `json:"status"`
	Enabled               bool                                `json:"enabled"`
	Reason                string                              `json:"reason,omitempty"`
	Toolsets              []string                            `json:"required_toolsets"`
	MinimumMode           contracts.QueryExecutionMode        `json:"minimum_mode"`
	ScopeKinds            []string                            `json:"scope_kinds"`
	ContractVersion       string                              `json:"contract_version"`
	LastKnownAvailability string                              `json:"last_known_availability"`
	InputSchema           json.RawMessage                     `json:"input_schema,omitempty"`
	RestrictedParameters  []string                            `json:"restricted_parameters,omitempty"`
	OutputFields          []contracts.OfficialDocumentField   `json:"output_fields,omitempty"`
	SourceDigest          string                              `json:"source_digest,omitempty"`
	ContractFingerprint   string                              `json:"contract_fingerprint,omitempty"`
	ExecutionQueryID      string                              `json:"execution_query_id,omitempty"`
	MappingEvidence       string                              `json:"mapping_evidence,omitempty"`
	APIPath               string                              `json:"api_path,omitempty"`
	DocumentationURL      string                              `json:"documentation_url,omitempty"`
	RequestFields         []contracts.OfficialDocumentField   `json:"request_fields,omitempty"`
	MissingEvidence       []string                            `json:"missing_evidence,omitempty"`
	PagingKind            string                              `json:"paging_kind,omitempty"`
	PagingVariants        []contracts.OfficialPagingVariant   `json:"paging_variants,omitempty"`
	MaxDateSpanDays       int                                 `json:"max_date_span_days,omitempty"`
	ConditionalRules      []contracts.OfficialConditionalRule `json:"conditional_rules,omitempty"`
	BoundUserFields       []string                            `json:"bound_user_fields,omitempty"`
}

type CatalogResult struct {
	Source         string                       `json:"source"`
	CatalogVersion string                       `json:"catalog_version"`
	Mode           contracts.QueryExecutionMode `json:"mode"`
	Toolsets       []string                     `json:"toolsets"`
	Total          int                          `json:"total"`
	Items          []CatalogItem                `json:"items"`
	HasMore        bool                         `json:"has_more"`
	Cursor         string                       `json:"cursor,omitempty"`
}

func failure(code, stage string, dispatched bool) error {
	return &port.Failure{Code: code, Stage: stage, Dispatched: dispatched}
}

func (service Service) Catalog(_ context.Context, request CatalogQuery) (CatalogResult, error) {
	result := CatalogResult{Source: "local_contract", CatalogVersion: contracts.QianchuanOfficialCatalogVersion, Mode: service.Policy.Mode(), Toolsets: service.Policy.Toolsets(), Items: []CatalogItem{}}
	if service.Policy.CheckCapability("qianchuan.official_capabilities") != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if request.Limit < 0 || request.Limit > 100 || len(request.Keyword) > 128 || len(request.Cursor) > 4096 {
		return result, failure("INVALID_ARGUMENT", "catalog", false)
	}
	if request.Limit == 0 {
		request.Limit = 20
	}
	queries, err := contracts.ApprovedOfficialQueries()
	if err != nil {
		return result, failure("CONTRACT_INVALID", "catalog", false)
	}
	filtered := []contracts.ApprovedOfficialQuery{}
	for _, query := range queries {
		if request.QueryID != "" && query.QueryID != request.QueryID || request.Group != "" && query.Group != request.Group {
			continue
		}
		keyword := strings.ToLower(request.Keyword)
		if keyword != "" && !strings.Contains(strings.ToLower(query.QueryID+" "+query.Title), keyword) {
			continue
		}
		filtered = append(filtered, query)
	}
	if request.QueryID != "" && len(filtered) == 0 {
		return result, failure("QUERY_NOT_FOUND", "catalog", false)
	}
	result.Total = len(filtered)
	offset, err := decodeCatalogCursor(request)
	if err != nil || offset > len(filtered) {
		return result, failure("INVALID_CURSOR", "catalog", false)
	}
	end := min(offset+request.Limit, len(filtered))
	for _, query := range filtered[offset:end] {
		item := CatalogItem{QueryID: query.QueryID, Title: query.Title, Group: query.Group, Status: query.Status, Enabled: query.Executable, Toolsets: query.Policy.RequiredToolsets, MinimumMode: query.Policy.MinimumMode, ScopeKinds: query.ScopeKinds, ContractVersion: query.ContractVersion, LastKnownAvailability: query.LastKnownAvailability}
		if !query.Executable {
			item.Reason = "CONTRACT_PENDING"
		} else if query.Status == "contract_limited" {
			item.Reason = "PAGINATION_FIRST_SLICE_ONLY"
		}
		if query.Executable && service.Policy.Check(query.Policy.RequiredToolsets, query.Policy.MinimumMode) != nil {
			item.Enabled = false
			item.Reason = "POLICY_DENIED"
		}
		if request.QueryID != "" {
			item.ExecutionQueryID = query.ExecutionQueryID
			item.MappingEvidence = query.MappingEvidence
			item.APIPath = query.APIPath
			item.DocumentationURL = query.DocumentationURL
			item.RequestFields = query.RequestFields
			item.MissingEvidence = query.MissingEvidence
			item.PagingKind = query.PagingKind
			item.PagingVariants = query.PagingVariants
			item.MaxDateSpanDays = query.MaxDateSpanDays
			item.ConditionalRules = query.ConditionalRules
			item.BoundUserFields = query.BoundUserFields
			item.InputSchema = query.HostInput
			item.RestrictedParameters = query.RestrictedParameters
			item.OutputFields = query.OutputFields
			item.SourceDigest = query.Policy.SourceDigest
			item.ContractFingerprint = query.Policy.ContractFingerprint
		}
		result.Items = append(result.Items, item)
	}
	result.HasMore = end < len(filtered)
	if result.HasMore {
		result.Cursor = encodeCatalogCursor(request, end)
	}
	return result, nil
}

func (service Service) Query(ctx context.Context, request contracts.OfficialQueryRequest) (contracts.OfficialQueryResult, error) {
	return service.query(ctx, request, []string{request.QueryID})
}

func (service Service) query(ctx context.Context, request contracts.OfficialQueryRequest, profile []string) (contracts.OfficialQueryResult, error) {
	result := contracts.OfficialQueryResult{QueryID: request.QueryID, Source: "official_mcp", Channel: "qianchuan", Scope: request.Scope, Status: "failed", StatisticalScope: "not_applicable", Columns: []contracts.OfficialQueryColumn{}, Items: []map[string]*string{}, Failures: []contracts.OfficialQueryFailure{}}
	if ctx == nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	request, err = contracts.DecodeOfficialQueryRequest(strings.NewReader(string(encoded)))
	if err != nil {
		return result, failure("INVALID_ARGUMENT", "request", false)
	}
	query, found, err := contracts.ApprovedOfficialQueryByID(request.QueryID)
	if err != nil {
		return result, failure("CONTRACT_INVALID", "policy", false)
	}
	if !found {
		return result, failure("QUERY_NOT_FOUND", "policy", false)
	}
	if !query.Executable {
		return result, failure("CONTRACT_PENDING", "policy", false)
	}
	if service.Policy.CheckCapability(query.Policy.CapabilityID) != nil || service.Policy.Check(query.Policy.RequiredToolsets, query.Policy.MinimumMode) != nil {
		return result, failure("POLICY_DENIED", "policy", false)
	}
	if query.Policy.BusinessMethod != "GET" || query.Policy.Effect != contracts.EffectOfficialRead || query.Policy.Channel != "qianchuan" {
		return result, failure("READ_ONLY_REQUIRED", "policy", false)
	}
	if !contains(query.ScopeKinds, request.Scope.Kind) || service.Policy.CheckSubject("qianchuan", request.Scope.Kind, request.Scope.ID) != nil {
		return result, failure("SCOPE_DENIED", "scope", false)
	}
	query = effectivePaging(query, request)
	arguments, err := prepareArguments(query, request)
	if err != nil {
		return result, err
	}
	if service.Authorizations == nil || service.Tokens == nil || service.Backend == nil {
		return result, failure("BUSINESS_CONFIGURATION_REQUIRED", "prepare", false)
	}
	state, err := service.Authorizations.LoadChannel(ctx, "qianchuan")
	if err != nil {
		return result, failure("ACCOUNT_SYNC_REQUIRED", "scope", false)
	}
	binding, err := service.resolveScope(state, query, request, arguments)
	if err != nil {
		return result, err
	}
	for _, field := range query.BoundUserFields {
		user, _ := configuration.Object(configuration.Object(state["authorizations"])[binding.AuthorizationID])["authorized_user_id"].(string)
		if !validTaskID(user) {
			return result, failure("AUTHORIZED_USER_UNVERIFIED", "scope", false)
		}
		arguments[field] = json.RawMessage(user)
	}
	if len(query.ServerFields) > 0 && (service.AppTokens == nil || binding.AppID == "") {
		return result, failure("APP_TOKEN_CONFIGURATION_REQUIRED", "app_authorization", false)
	}
	cursorState, err := decodeQueryCursorState(query, request, binding.AuthorizationID)
	if err != nil {
		return result, failure("INVALID_CURSOR", "paging", false)
	}
	page, offset := cursorState.Page, cursorState.Offset
	if query.PageField != "" {
		arguments[query.PageField] = json.RawMessage(jsonNumber(page))
	}
	if query.CursorField != "" && (query.PagingKind == "cursor" || query.PagingKind == "offset") && !(query.OmitInitialCursor && cursorState.Upstream == "") {
		upstream := cursorState.Upstream
		if upstream == "" {
			upstream = "0"
		}
		var input map[string]any
		_ = json.Unmarshal(query.ApprovedInput, &input)
		properties, _ := input["properties"].(map[string]any)
		field, _ := properties[query.CursorField].(map[string]any)
		if field["type"] == "integer" {
			if !integerToken.MatchString(upstream) {
				return result, failure("INVALID_CURSOR", "paging", false)
			}
			arguments[query.CursorField] = json.RawMessage(upstream)
		} else {
			arguments[query.CursorField] = raw(upstream)
		}
	}
	if _, ok := requestcontrol.BudgetFrom(ctx); !ok {
		return result, failure("REQUEST_BUDGET_REQUIRED", "budget", false)
	}
	lease, err := service.Tokens.Ensure(ctx, auth.TokenQuery{Channel: "qianchuan", AdvertiserID: scopeAdvertiser(request.Scope), AuthAccountID: request.AuthAccountID, AuthorizationID: binding.AuthorizationID})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var domainErr *domain.Error
		if errors.As(err, &domainErr) && domainErr.Code == "reauthorization_required" {
			return result, &port.Failure{Code: "REAUTHORIZATION_REQUIRED", Stage: "authorization", AuthorizationID: binding.AuthorizationID, AdvertiserID: scopeAdvertiser(request.Scope)}
		}
		return result, failure("AUTHORIZATION_PREPARATION_FAILED", "authorization", false)
	}
	if lease.Channel != "qianchuan" || lease.AuthorizationID != binding.AuthorizationID || lease.TokenRevision < binding.TokenRevision {
		return result, failure("AUTHORIZATION_INVALID", "authorization", false)
	}
	scopedCtx, err := auth.WithTokenLease(ctx, lease)
	if err != nil {
		return result, failure("AUTHORIZATION_INVALID", "authorization", false)
	}
	if request.Scope.Kind == "advertiser" {
		scopedCtx, err = auth.WithAdvertiserTokenLease(ctx, lease, request.Scope.ID)
		if err != nil {
			return result, failure("AUTHORIZATION_INVALID", "authorization", false)
		}
	}
	if len(query.ServerFields) > 0 {
		appLease, err := service.AppTokens.EnsureApp(scopedCtx, binding.AppID)
		if err != nil || appLease.AppID != binding.AppID || appLease.Token == "" {
			return result, failure("APP_TOKEN_PREPARATION_FAILED", "app_authorization", false)
		}
		arguments["app_id"] = json.RawMessage(binding.AppID)
		arguments["APP-Access-Token"] = raw(appLease.Token)
	}
	controlKey := request.Scope.ID
	if request.Scope.Kind != "advertiser" {
		controlKey = "subject:" + binding.AuthorizationID + ":" + request.Scope.Kind + ":" + request.Scope.ID
	}
	executionArguments := arguments
	if query.ProjectionPath != "" {
		var filtering map[string]json.RawMessage
		if json.Unmarshal(arguments["filtering"], &filtering) != nil || len(filtering["ad_id"]) == 0 {
			return result, failure("INVALID_PARAMETERS", "mapping", false)
		}
		executionArguments = map[string]json.RawMessage{"advertiser_id": arguments["advertiser_id"], "ad_id": filtering["ad_id"]}
		profile = []string{query.ExecutionQueryID}
	}
	response, err := service.Backend.Read(scopedCtx, port.Request{QueryID: query.ExecutionQueryID, Profile: profile, Lease: port.Lease{AuthorizationID: lease.AuthorizationID, Revision: lease.TokenRevision, Token: lease.AccessToken}, ControlKey: controlKey, Arguments: executionArguments, ApprovedInput: query.ExecutionInput, SourceInput: query.SourceInput})
	if err != nil {
		var controlled *port.Failure
		if errors.As(err, &controlled) && controlled.Code == "REAUTHORIZATION_REQUIRED" {
			copy := *controlled
			copy.AuthorizationID = binding.AuthorizationID
			copy.AdvertiserID = scopeAdvertiser(request.Scope)
			return result, &copy
		}
		return result, err
	}
	metadata := configuration.Object(configuration.Object(state["authorizations"])[binding.AuthorizationID])
	if err := service.verifyResponseScope(response.Data, request.Scope, binding.AdvertiserIDs, configuration.List(metadata["authorized_accounts"])); err != nil {
		return result, err
	}
	if query.ProjectionPath != "" {
		returnedID, present := readValue(response.Data, []string{"ad_id"})
		var expectedID json.Number
		if json.Unmarshal(executionArguments["ad_id"], &expectedID) != nil || !present || subjectID(returnedID) != expectedID.String() {
			return result, failure("RESPONSE_SCOPE_MISMATCH", "mapping", true)
		}
		value, present := readValue(response.Data, strings.Split(query.ProjectionPath, "."))
		if !present || value == nil {
			return result, failure("REQUIRED_OUTPUT_MISSING", "mapping", true)
		}
		response.Data = value
	}
	if query.ProjectionPath == "" {
		if err := verifyRequestedPlans(query, executionArguments, response.Data); err != nil {
			return result, err
		}
	}
	projection, err := projectResult(query, request, response.Data, page, offset)
	if err != nil {
		return result, err
	}
	result = projection.result
	if service.Now != nil {
		result.ObservedAt = service.Now().UTC().Format(time.RFC3339Nano)
	} else {
		result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if result.HasMore {
		nextUpstream := projection.nextUpstream
		if projection.nextOffset == 0 && (query.PagingKind == "cursor" || query.PagingKind == "offset") && (nextUpstream == "" || nextUpstream == cursorState.Upstream || nextUpstream == "0") {
			return result, failure("PAGINATION_INVALID", "paging", true)
		}
		if projection.nextOffset > 0 {
			nextUpstream = cursorState.Upstream
		}
		result.Cursor = encodeQueryCursor(query, request, binding.AuthorizationID, projection.nextPage, projection.nextOffset, nextUpstream)
	}
	return result, nil
}

func scopeAdvertiser(scope contracts.OfficialQueryScope) string {
	if scope.Kind == "advertiser" {
		return scope.ID
	}
	return ""
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
