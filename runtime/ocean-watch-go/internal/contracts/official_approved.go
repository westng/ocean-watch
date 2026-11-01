package contracts

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

const OfficialQueryContractVersion = "official-read-1-2026-11-01"

//go:embed catalog/qianchuan-schema-source-2026-11-01.json
var officialSchemaSourceJSON []byte

//go:embed catalog/qianchuan-doc-source-2026-11-01.json
var officialDocSourceJSON []byte

type OfficialDocumentField struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type OfficialSubjectRule struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// A paging variant changes only documented paging parameters, never the
// approved input or output whitelist. Unknown termination remains partial.
type OfficialPagingVariant struct {
	Field             string   `json:"field"`
	Values            []string `json:"values"`
	Kind              string   `json:"kind"`
	OmitInitialCursor bool     `json:"omit_initial_cursor,omitempty"`
}

type ApprovedOfficialQuery struct {
	PublishedOfficialQuery
	Title                 string                    `json:"title"`
	ContractVersion       string                    `json:"contract_version"`
	Policy                OfficialQueryPolicy       `json:"policy"`
	ApprovedInput         json.RawMessage           `json:"-"`
	SourceInput           json.RawMessage           `json:"-"`
	ExecutionQueryID      string                    `json:"execution_query_id"`
	ExecutionInput        json.RawMessage           `json:"-"`
	ProjectionPath        string                    `json:"projection_path,omitempty"`
	MappingEvidence       string                    `json:"mapping_evidence,omitempty"`
	HostInput             json.RawMessage           `json:"input_schema"`
	OutputFields          []OfficialDocumentField   `json:"output_fields,omitempty"`
	RequestFields         []OfficialDocumentField   `json:"request_fields,omitempty"`
	SubjectRules          []OfficialSubjectRule     `json:"subject_rules"`
	ScopeKinds            []string                  `json:"scope_kinds"`
	RestrictedParameters  []string                  `json:"restricted_parameters,omitempty"`
	DocumentDigest        string                    `json:"document_digest"`
	LastKnownAvailability string                    `json:"last_known_availability"`
	PrimaryArray          string                    `json:"primary_array,omitempty"`
	PageField             string                    `json:"page_field,omitempty"`
	PageSizeField         string                    `json:"page_size_field,omitempty"`
	HasPageInfo           bool                      `json:"has_page_info"`
	PageInfoPath          string                    `json:"page_info_path,omitempty"`
	TotalField            string                    `json:"total_field,omitempty"`
	CursorField           string                    `json:"cursor_field,omitempty"`
	PagingKind            string                    `json:"paging_kind"`
	PagingVariants        []OfficialPagingVariant   `json:"paging_variants,omitempty"`
	OmitInitialCursor     bool                      `json:"omit_initial_cursor,omitempty"`
	ServerFields          []string                  `json:"server_fields,omitempty"`
	MissingEvidence       []string                  `json:"missing_evidence,omitempty"`
	MaxDateSpanDays       int                       `json:"max_date_span_days,omitempty"`
	ConditionalRules      []OfficialConditionalRule `json:"conditional_rules,omitempty"`
	BoundUserFields       []string                  `json:"bound_user_fields,omitempty"`
}

// ApprovedOfficialQueries joins verified public GET facts and captured input
// schemas. Missing sources stay visibly pending; compilation cannot enable
// arbitrary tools discovered by a remote server.
var approvedCatalogOnce sync.Once
var approvedCatalog []ApprovedOfficialQuery
var approvedCatalogErr error

func ApprovedOfficialQueries() ([]ApprovedOfficialQuery, error) {
	approvedCatalogOnce.Do(func() { approvedCatalog, approvedCatalogErr = compileOfficialQueries() })
	if approvedCatalogErr != nil {
		return nil, approvedCatalogErr
	}
	result := make([]ApprovedOfficialQuery, len(approvedCatalog))
	for index, query := range approvedCatalog {
		result[index] = cloneOfficialQuery(query)
	}
	return result, nil
}
func ApprovedOfficialQueryByID(id string) (ApprovedOfficialQuery, bool, error) {
	approvedCatalogOnce.Do(func() { approvedCatalog, approvedCatalogErr = compileOfficialQueries() })
	if approvedCatalogErr != nil {
		return ApprovedOfficialQuery{}, false, approvedCatalogErr
	}
	for _, query := range approvedCatalog {
		if query.QueryID == id {
			return cloneOfficialQuery(query), true, nil
		}
	}
	return ApprovedOfficialQuery{}, false, nil
}
func cloneOfficialQuery(query ApprovedOfficialQuery) ApprovedOfficialQuery {
	query.ApprovedInput = append(json.RawMessage(nil), query.ApprovedInput...)
	query.SourceInput = append(json.RawMessage(nil), query.SourceInput...)
	query.ExecutionInput = append(json.RawMessage(nil), query.ExecutionInput...)
	query.HostInput = append(json.RawMessage(nil), query.HostInput...)
	query.Policy.InputSchema = append(json.RawMessage(nil), query.Policy.InputSchema...)
	query.Policy.OutputSchema = append(json.RawMessage(nil), query.Policy.OutputSchema...)
	query.Policy.RequiredToolsets = append([]string(nil), query.Policy.RequiredToolsets...)
	query.Policy.SubjectFields = append([]string(nil), query.Policy.SubjectFields...)
	query.Policy.Roles = append([]string(nil), query.Policy.Roles...)
	query.Policy.Scenes = append([]string(nil), query.Policy.Scenes...)
	query.OutputFields = append([]OfficialDocumentField(nil), query.OutputFields...)
	query.RequestFields = append([]OfficialDocumentField(nil), query.RequestFields...)
	query.SubjectRules = append([]OfficialSubjectRule(nil), query.SubjectRules...)
	query.ScopeKinds = append([]string(nil), query.ScopeKinds...)
	query.RestrictedParameters = append([]string(nil), query.RestrictedParameters...)
	query.MissingEvidence = append([]string(nil), query.MissingEvidence...)
	query.ServerFields = append([]string(nil), query.ServerFields...)
	query.BoundUserFields = append([]string(nil), query.BoundUserFields...)
	query.PagingVariants = append([]OfficialPagingVariant(nil), query.PagingVariants...)
	for index := range query.PagingVariants {
		query.PagingVariants[index].Values = append([]string(nil), query.PagingVariants[index].Values...)
	}
	encodedRules, _ := json.Marshal(query.ConditionalRules)
	_ = json.Unmarshal(encodedRules, &query.ConditionalRules)
	return query
}

func compileOfficialQueries() ([]ApprovedOfficialQuery, error) {
	catalog, err := QianchuanOfficialCandidates()
	if err != nil {
		return nil, err
	}
	var schemas struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Input       json.RawMessage `json:"input_schema"`
			Output      json.RawMessage `json:"output_schema,omitempty"`
			Digest      string          `json:"source_digest"`
		} `json:"tools"`
	}
	var documents struct {
		Documents []struct {
			QueryID string                  `json:"query_id"`
			Title   string                  `json:"title"`
			Path    string                  `json:"api_path"`
			Method  string                  `json:"method"`
			URL     string                  `json:"documentation_url"`
			Digest  string                  `json:"document_digest"`
			Output  []OfficialDocumentField `json:"response_fields"`
			Input   []OfficialDocumentField `json:"request_fields"`
			Notes   []string                `json:"notes"`
			Status  string                  `json:"capture_status"`
		} `json:"documents"`
	}
	if json.Unmarshal(officialSchemaSourceJSON, &schemas) != nil || json.Unmarshal(officialDocSourceJSON, &documents) != nil {
		return nil, ErrInvalidOfficialQueryCatalog
	}
	bySchema := map[string]int{}
	byDoc := map[string]int{}
	for i, schema := range schemas.Tools {
		if _, duplicate := bySchema[schema.Name]; duplicate {
			return nil, ErrInvalidOfficialQueryCatalog
		}
		bySchema[schema.Name] = i
	}
	for i, doc := range documents.Documents {
		if _, duplicate := byDoc[doc.QueryID]; duplicate {
			return nil, ErrInvalidOfficialQueryCatalog
		}
		byDoc[doc.QueryID] = i
	}
	queries := make([]ApprovedOfficialQuery, 0, len(catalog.Queries))
	for _, candidate := range catalog.Queries {
		query := ApprovedOfficialQuery{PublishedOfficialQuery: candidate, ContractVersion: OfficialQueryContractVersion, ExecutionQueryID: candidate.QueryID, LastKnownAvailability: "unknown", ScopeKinds: []string{}, SubjectRules: []OfficialSubjectRule{}, OutputFields: []OfficialDocumentField{}}
		query.Policy = OfficialQueryPolicy{CapabilityID: "qianchuan.official_query", RequiredToolsets: []string{"qianchuan"}, MinimumMode: QueryModeRead, Channel: "qianchuan", Effect: EffectOfficialRead, ScopeResolver: "documented_subjects", BusinessMethod: "GET", Roles: []string{}, Scenes: []string{}}
		if candidate.Group == "reports" {
			query.Policy.RequiredToolsets = []string{"qianchuan", "reports"}
		}
		if candidate.Group == "accounts" {
			query.Policy.RequiredToolsets = []string{"qianchuan", "core"}
		}
		docIndex, hasDoc := byDoc[candidate.QueryID]
		if !hasDoc {
			query.MissingEvidence = append(query.MissingEvidence, "official_api_document")
		} else {
			doc := documents.Documents[docIndex]
			query.Title = doc.Title
			query.DocumentDigest = doc.Digest
			query.DocumentationURL = doc.URL
			query.OutputFields = doc.Output
			query.RequestFields = doc.Input
			if candidate.QueryID == "qianchuan_today_live_room_product_list_get_v1" && !hasDocumentField(query.OutputFields, "data.page_info.page") {
				// Public HTML omits this section; the pinned official SDK declares
				// Page/PageSize inputs and these exact response JSON fields.
				query.MappingEvidence = "Pinned ad_open_sdk_go@v1.1.92 QianchuanTodayLiveRoomProductListGetV10Api and ResponseDataPageInfo supply paging fields omitted by the public GET table"
				for _, name := range []string{"page", "page_size", "total_num", "total_page"} {
					query.OutputFields = append(query.OutputFields, OfficialDocumentField{Path: "data.page_info." + name, Name: name, Type: "number", Description: "分页字段；依据锁定的官方 Go SDK v1.1.92", Required: name == "page" || name == "page_size"})
				}
			}
			if candidate.QueryID == "advertiser_public_info_v2" {
				query.MappingEvidence = "Official public-info document spells response root date; pinned ad_open_sdk_go@v1.1.92 AdvertiserPublicInfoV2Response declares json:data"
				for index, field := range query.OutputFields {
					if field.Path == "date" || strings.HasPrefix(field.Path, "date.") {
						query.OutputFields[index].Path = "data" + strings.TrimPrefix(field.Path, "date")
					}
				}
			}
			if doc.Status != "captured" || doc.Method != "GET" || doc.Path != candidate.APIPath {
				query.MissingEvidence = append(query.MissingEvidence, "verified_get_method_and_path")
			}
			if len(doc.Output) == 0 {
				query.MissingEvidence = append(query.MissingEvidence, "documented_output")
			}
		}
		schemaIndex, hasSchema := bySchema[candidate.QueryID]
		if !hasSchema && candidate.QueryID == "qianchuan_ad_pivativewords_get_v1" {
			// Both public GET documents explicitly define the same phrase_words
			// and precise_words. Keep the missing published identifier, while
			// dispatching only the registered detail tool and fixed projection.
			if index, exists := bySchema["qianchuan_ad_detail_get_v1"]; exists {
				detailDoc, docExists := byDoc["qianchuan_ad_detail_get_v1"]
				if docExists && documents.Documents[detailDoc].Method == "GET" && hasDocumentField(documents.Documents[detailDoc].Output, "data.pivative_words.phrase_words") && hasDocumentField(documents.Documents[detailDoc].Output, "data.pivative_words.precise_words") {
					schemaIndex, hasSchema = index, true
					query.ExecutionQueryID = "qianchuan_ad_detail_get_v1"
					query.ProjectionPath = "pivative_words"
					query.LastKnownAvailability = "published_tool_missing_fixed_detail_mapping"
					query.MappingEvidence = "Official GET negative-word and detail documents plus pinned SDK QianchuanAdDetailGetV10ResponseDataPivativeWords agree on phrase_words/precise_words; fixed filtering.ad_id -> ad_id mapping"
				}
			}
		}
		if !hasSchema {
			query.MissingEvidence = append(query.MissingEvidence, "remote_input_schema")
			query.LastKnownAvailability = "not_discovered"
		} else {
			schema := schemas.Tools[schemaIndex]
			query.ApprovedInput = append(json.RawMessage(nil), schema.Input...)
			query.SourceInput = append(json.RawMessage(nil), schema.Input...)
			query.ExecutionInput = append(json.RawMessage(nil), schema.Input...)
			if query.ProjectionPath != "" {
				query.ApprovedInput = json.RawMessage(`{"type":"object","properties":{"advertiser_id":{"type":"integer","format":"int64","minimum":1,"description":"客户ID"},"filtering":{"type":"object","properties":{"ad_id":{"type":"integer","format":"int64","minimum":1,"description":"待获取否定词的计划ID"}},"required":["ad_id"]}},"required":["advertiser_id","filtering"]}`)
			}
			query.Policy.SourceDigest = schema.Digest
			var input map[string]any
			decoder := json.NewDecoder(strings.NewReader(string(query.ApprovedInput)))
			decoder.UseNumber()
			if decoder.Decode(&input) != nil {
				return nil, ErrInvalidOfficialQueryCatalog
			}
			if correction := correctPublishedInput(query.QueryID, input, query.RequestFields); correction != "" {
				if query.MappingEvidence != "" {
					query.MappingEvidence += "; "
				}
				query.MappingEvidence += correction
				query.ApprovedInput, _ = json.Marshal(input)
				query.ExecutionInput = append(json.RawMessage(nil), query.ApprovedInput...)
				query.LastKnownAvailability = "captured_with_documented_input_correction"
			}
			query.MaxDateSpanDays = documentedDateSpan(query.RequestFields)
			query.ConditionalRules = documentedConditionalRules(query.QueryID)
			collectSubjectRules(input, "", &query.SubjectRules)
			query.ScopeKinds = scopeKindsForQuery(query.QueryID, query.SubjectRules)
			query.RestrictedParameters = restrictConflictingEnumParameters(input)
			applyDocumentRequirements(input, query.RequestFields, &query.MissingEvidence)
			for _, path := range query.RestrictedParameters {
				if requiredSchemaPath(input, path) {
					query.MissingEvidence = append(query.MissingEvidence, "required_parameter_schema_conflict:"+path)
				}
			}
			if propertyPresent(input, "APP-Access-Token") {
				query.ServerFields = []string{"APP-Access-Token", "app_id"}
				query.ScopeKinds = []string{"advertiser"}
			}
			if propertyPresent(input, "core_user_id") && hasDocumentField(query.RequestFields, "core_user_id") {
				query.BoundUserFields = []string{"core_user_id"}
			}
			sealHostSchema(input)
			boundHostParameters(input)
			removeSecretInput(input)
			if len(query.ServerFields) > 0 {
				removeServerFields(input, query.ServerFields)
			}
			removeServerFields(input, query.BoundUserFields)
			host, _ := json.Marshal(input)
			query.HostInput = host
			query.Policy.InputSchema = host
			query.Policy.SubjectFields = []string{}
			for _, rule := range query.SubjectRules {
				query.Policy.SubjectFields = append(query.Policy.SubjectFields, rule.Path)
			}
			properties, _ := input["properties"].(map[string]any)
			if _, ok := properties["page"]; ok {
				query.PageField = "page"
			}
			if _, ok := properties["page_size"]; ok && !containsString(query.RestrictedParameters, "page_size") {
				query.PageSizeField = "page_size"
			}
		}
		arrayCandidates := []OfficialDocumentField{}
		for _, field := range query.OutputFields {
			if field.Path == "data.page_info" {
				query.HasPageInfo = true
			}
			if (strings.HasSuffix(field.Type, "[]") || field.Type == "list") && (field.Path == "data" || strings.HasPrefix(field.Path, "data.") && strings.Count(field.Path, ".") == 1) {
				arrayCandidates = append(arrayCandidates, field)
			}
		}
		if len(arrayCandidates) == 1 && arrayCandidates[0].Name != "children" {
			query.PrimaryArray = strings.TrimPrefix(arrayCandidates[0].Path, "data.")
			if arrayCandidates[0].Path == "data" {
				query.PrimaryArray = "$data"
			}
		} else {
			for _, field := range arrayCandidates {
				if containsString([]string{"list", "ad_list", "product_list", "account_list", "data_list", "material_list", "video_list", "rows"}, field.Name) {
					query.PrimaryArray = strings.TrimPrefix(field.Path, "data.")
					break
				}
			}
		}
		if strings.Contains(query.QueryID, "detail") && !hasDocumentField(query.OutputFields, "data.list") {
			query.PrimaryArray = ""
		}
		query.Policy.OutputSchema, _ = json.Marshal(query.OutputFields)
		compilePaging(&query)
		compilePagingVariants(&query)
		stablePolicy := query.Policy
		stablePolicy.SourceDigest = ""
		fingerprint, _ := json.Marshal(struct {
			Version               string
			Policy                OfficialQueryPolicy
			Subjects              []OfficialSubjectRule
			Scopes                []string
			Array, Page, PageSize string
			PageInfo              bool
		}{query.ContractVersion + ":" + query.ExecutionQueryID + ":" + query.ProjectionPath, stablePolicy, query.SubjectRules, query.ScopeKinds, query.PrimaryArray, query.PageField, query.PageSizeField, query.HasPageInfo})
		extra, _ := json.Marshal([]any{query.PagingKind, query.PagingVariants, query.PageInfoPath, query.TotalField, query.CursorField, query.MaxDateSpanDays, query.ServerFields, query.MappingEvidence, query.ConditionalRules, query.BoundUserFields})
		fingerprint = append(fingerprint, extra...)
		sum := sha256.Sum256(fingerprint)
		query.Policy.ContractFingerprint = hex.EncodeToString(sum[:])
		query.Executable = len(query.MissingEvidence) == 0
		if query.Executable {
			query.Status = "contract_verified"
			if query.PagingKind == "unverified" || pagingHasLimitedVariant(query.PagingVariants) {
				query.Status = "contract_limited"
			}
		} else {
			query.Status = "contract_pending"
		}
		queries = append(queries, query)
	}
	return queries, nil
}

func removeServerFields(schema map[string]any, fields []string) {
	properties, _ := schema["properties"].(map[string]any)
	for _, name := range fields {
		delete(properties, name)
	}
	values, _ := schema["required"].([]any)
	kept := []any{}
	for _, value := range values {
		if !containsString(fields, fmtString(value)) {
			kept = append(kept, value)
		}
	}
	schema["required"] = kept
}

func propertyPresent(schema map[string]any, name string) bool {
	properties, _ := schema["properties"].(map[string]any)
	_, ok := properties[name]
	return ok
}

func applyDocumentRequirements(schema map[string]any, fields []OfficialDocumentField, missing *[]string) {
	for _, field := range fields {
		if !field.Required || reservedOfficialQueryField(field.Name) || strings.Contains(strings.ToLower(field.Name), "token") || strings.Contains(field.Description, "条件") || strings.Contains(field.Description, "时必填") || strings.Contains(field.Description, "当") && strings.Contains(field.Description, "必填") {
			continue
		}
		parts := strings.Split(field.Path, ".")
		node := schema
		for i, part := range parts {
			properties, _ := node["properties"].(map[string]any)
			child, ok := properties[part].(map[string]any)
			if !ok {
				if i == 0 {
					*missing = append(*missing, "required_document_parameter_absent:"+field.Path)
				}
				break
			}
			if i == len(parts)-1 {
				required, _ := node["required"].([]any)
				found := false
				for _, name := range required {
					if name == part {
						found = true
					}
				}
				if !found {
					node["required"] = append(required, part)
				}
			}
			node = child
		}
	}
}

func requiredSchemaPath(schema map[string]any, path string) bool {
	parts := strings.Split(path, ".")
	node := schema
	for i, part := range parts {
		required, _ := node["required"].([]any)
		requiredHere := false
		for _, name := range required {
			if name == part {
				requiredHere = true
			}
		}
		if i == len(parts)-1 {
			return requiredHere
		}
		properties, _ := node["properties"].(map[string]any)
		node, _ = properties[part].(map[string]any)
		if node == nil {
			return false
		}
	}
	return false
}

func removeSecretInput(schema map[string]any) {
	properties, _ := schema["properties"].(map[string]any)
	for name, child := range properties {
		if reservedOfficialQueryField(name) || strings.Contains(strings.ToLower(name), "token") {
			delete(properties, name)
			continue
		}
		if node, ok := child.(map[string]any); ok {
			removeSecretInput(node)
		}
	}
	if values, ok := schema["required"].([]any); ok {
		kept := []any{}
		for _, name := range values {
			if _, exists := properties[fmtString(name)]; exists {
				kept = append(kept, name)
			}
		}
		schema["required"] = kept
	}
}
func fmtString(value any) string { valueString, _ := value.(string); return valueString }

func compilePaging(query *ApprovedOfficialQuery) {
	query.PagingKind = "none"
	if query.PageField != "" {
		query.PagingKind = "page"
		for _, prefix := range []string{"data.page_info", "data.pagination"} {
			if !hasDocumentField(query.OutputFields, prefix+".page") {
				continue
			}
			query.PageInfoPath = strings.TrimPrefix(prefix, "data.")
			query.HasPageInfo = true
			for _, name := range []string{"total_num", "total_number", "total_count", "total"} {
				if hasDocumentField(query.OutputFields, prefix+"."+name) {
					query.TotalField = name
					break
				}
			}
		}
		if query.PageInfoPath == "" && hasDocumentField(query.OutputFields, "data.total") {
			query.TotalField = "total"
		}
		if query.PageInfoPath == "" && query.TotalField == "" {
			query.PagingKind = "unverified"
		}
		return
	}
	var schema map[string]any
	_ = json.Unmarshal(query.ApprovedInput, &schema)
	if propertyPresent(schema, "offset") && hasDocumentField(query.OutputFields, "data.offset") {
		query.PagingKind = "offset"
		query.CursorField = "offset"
		return
	}
	if propertyPresent(schema, "cursor") {
		query.PagingKind = "unverified"
		if hasDocumentField(query.OutputFields, "data.page_info.has_more") && hasDocumentField(query.OutputFields, "data.page_info.cursor") {
			query.PagingKind = "cursor"
			query.CursorField = "cursor"
			query.PageInfoPath = "page_info"
		}
	}
}

func compilePagingVariants(query *ApprovedOfficialQuery) {
	switch query.QueryID {
	case "qianchuan_product_available_get_v1":
		if hasDocumentField(query.RequestFields, "campaign_scene") && hasDocumentField(query.OutputFields, "data.page_info.has_more") && hasDocumentField(query.OutputFields, "data.page_info.cursor") {
			query.PagingVariants = []OfficialPagingVariant{{Field: "campaign_scene", Values: []string{"NEW_PRODUCT_BOOST"}, Kind: "cursor", OmitInitialCursor: true}}
		}
	case "qianchuan_uni_promotion_product_get_v1":
		if hasDocumentField(query.RequestFields, "filtering.tab") && hasDocumentField(query.OutputFields, "data.page_info.cursor") {
			// The GET table documents cursor mode for these tabs, but gives no
			// has_more or terminal cursor rule. Only the first batch is exposed.
			query.PagingVariants = []OfficialPagingVariant{{Field: "filtering.tab", Values: []string{"BREAKTHROUGH_PRODUCT", "NEW_PRODUCT"}, Kind: "unverified", OmitInitialCursor: true}}
		}
	}
}

func pagingHasLimitedVariant(variants []OfficialPagingVariant) bool {
	for _, variant := range variants {
		if variant.Kind == "unverified" {
			return true
		}
	}
	return false
}

func hasDocumentField(fields []OfficialDocumentField, path string) bool {
	for _, field := range fields {
		if field.Path == path {
			return true
		}
	}
	return false
}

func collectSubjectRules(schema map[string]any, path string, rules *[]OfficialSubjectRule) {
	properties, _ := schema["properties"].(map[string]any)
	names := []string{}
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child, _ := properties[name].(map[string]any)
		childPath := name
		if path != "" {
			childPath = path + "." + name
		}
		if kind := subjectKind(name); kind != "" {
			*rules = append(*rules, OfficialSubjectRule{Path: childPath, Kind: kind})
		}
		collectSubjectRules(child, childPath, rules)
		if items, ok := child["items"].(map[string]any); ok {
			collectSubjectRules(items, childPath+".*", rules)
		}
	}
}

func subjectKind(name string) string {
	switch name {
	case "advertiser_id", "advertiser_ids", "target_advertiser_id", "target_advertiser_ids":
		return "advertiser"
	case "agent_id", "agent_ids", "agent_company_id":
		return "agent"
	case "shop_id", "shop_ids":
		return "shop"
	case "cc_account_id", "customer_center_id":
		return "customer_center"
	case "organization_id", "organization_ids", "enterprise_organization_id":
		return "organization"
	case "account_id", "account_ids", "account_id_list", "target_account_id_list", "target_id_list", "opponent_target_id":
		return "account"
	default:
		return ""
	}
}

func OfficialSubjectKind(name string) string { return subjectKind(name) }

func scopeKindsForQuery(id string, rules []OfficialSubjectRule) []string {
	if id == "tools_industry_get_v2" {
		return []string{"public_catalog"}
	}
	if id == "account_fund_get_v3" {
		return []string{"advertiser", "agent", "customer_center", "organization", "shop", "authorization"}
	}
	if len(rules) == 0 {
		return []string{"authorization"}
	}
	for _, kind := range []string{"advertiser", "customer_center", "organization", "agent", "shop", "account"} {
		for _, rule := range rules {
			if !strings.Contains(rule.Path, ".") && rule.Kind == kind {
				if kind == "account" {
					return []string{"agent", "customer_center", "organization", "shop"}
				}
				return []string{kind, "authorization"}
			}
		}
	}
	return []string{"authorization"}
}

func restrictConflictingEnumParameters(schema map[string]any) []string {
	result := []string{}
	properties, _ := schema["properties"].(map[string]any)
	for name, value := range properties {
		child, _ := value.(map[string]any)
		values, _ := child["enum"].([]any)
		if child["type"] == "integer" && len(values) > 0 {
			conflict := false
			for _, value := range values {
				if _, ok := value.(string); ok {
					conflict = true
				}
			}
			if conflict {
				result = append(result, name)
			}
		}
	}
	sort.Strings(result)
	return result
}

func sealHostSchema(schema map[string]any) {
	if schema["type"] == "object" {
		schema["additionalProperties"] = false
		properties, _ := schema["properties"].(map[string]any)
		for name, value := range properties {
			child, _ := value.(map[string]any)
			if child["type"] == "string" {
				switch child["format"] {
				case "datetime":
					child["pattern"] = `^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`
				case "date":
					child["pattern"] = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
				}
			}
			if OfficialIdentifierField(name) && child["type"] == "integer" {
				child["type"] = "string"
				child["pattern"] = "^[1-9][0-9]{0,18}$"
				delete(child, "minimum")
				delete(child, "maximum")
				delete(child, "format")
			}
			if OfficialIdentifierField(name) && child["type"] == "array" {
				items, _ := child["items"].(map[string]any)
				if items["type"] == "integer" {
					items["type"] = "string"
					items["pattern"] = "^[1-9][0-9]{0,18}$"
					delete(items, "minimum")
					delete(items, "maximum")
					delete(items, "format")
				}
			}
			sealHostSchema(child)
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		sealHostSchema(items)
	}
}

func OfficialIdentifierField(name string) bool {
	return name == "id" || name == "ids" || strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_ids") || strings.HasSuffix(name, "_id_list") || strings.HasSuffix(name, "Id") || strings.HasSuffix(name, "Ids")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var ErrOfficialContractPending = errors.New("official query contract is not complete")
