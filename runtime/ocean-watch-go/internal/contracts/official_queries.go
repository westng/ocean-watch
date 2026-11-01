package contracts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const (
	QianchuanOfficialCatalogVersion = "qianchuan-public-2026-11-01"
	OfficialQueryPublishedOnly      = "published_only"
	MaxOfficialQueryRequestBytes    = 64 << 10
)

var (
	ErrInvalidOfficialQueryCatalog = errors.New("invalid official query candidate catalog")
	ErrInvalidOfficialQueryRequest = errors.New("invalid official query request")
	officialQueryIDPattern         = regexp.MustCompile("^[a-z][a-z0-9_]{0,127}$")
	officialDecimalIDPattern       = regexp.MustCompile("^[1-9][0-9]{0,63}$")
)

//go:embed catalog/qianchuan-official-2026-11-01.json
var qianchuanOfficialCatalogJSON []byte

// PublishedOfficialQuery describes public metadata, never dispatch permission.
// HTTP methods, subject rules and upstream MCP schemas remain unverified until
// separately approved; the public group does not imply any of them.
type PublishedOfficialQuery struct {
	QueryID          string `json:"query_id"`
	UpstreamTool     string `json:"upstream_tool"`
	APIPath          string `json:"api_path"`
	DocumentationURL string `json:"documentation_url"`
	Group            string `json:"group"`
	Status           string `json:"status"`
	Executable       bool   `json:"executable"`
	Notes            string `json:"notes,omitempty"`
}

type OfficialQueryCatalog struct {
	Version    string                   `json:"catalog_version"`
	SourceURL  string                   `json:"source_url"`
	ObservedOn string                   `json:"observed_on"`
	Queries    []PublishedOfficialQuery `json:"queries"`
}

// QianchuanOfficialCandidates reads embedded metadata only. Each call owns its
// decoded result, so a caller cannot turn shared candidates into permissions.
// It deliberately does not register a Host tool or touch authorization state.
func QianchuanOfficialCandidates() (OfficialQueryCatalog, error) {
	return decodeOfficialQueryCatalog(qianchuanOfficialCatalogJSON)
}

func (catalog OfficialQueryCatalog) Find(queryID string) (PublishedOfficialQuery, bool) {
	for _, query := range catalog.Queries {
		if query.QueryID == queryID {
			return query, true
		}
	}
	return PublishedOfficialQuery{}, false
}

func decodeOfficialQueryCatalog(data []byte) (OfficialQueryCatalog, error) {
	var catalog OfficialQueryCatalog
	if err := decodeOneJSON(data, &catalog); err != nil {
		return OfficialQueryCatalog{}, ErrInvalidOfficialQueryCatalog
	}
	if catalog.Version != QianchuanOfficialCatalogVersion ||
		catalog.ObservedOn != "2026-11-01" ||
		catalog.SourceURL != "https://open.oceanengine.com/labels/12/docs/1847297003631945" ||
		len(catalog.Queries) != 156 {
		return OfficialQueryCatalog{}, ErrInvalidOfficialQueryCatalog
	}
	groups := map[string]bool{
		"accounts": true, "finance": true, "plans": true, "assets": true,
		"products": true, "reports": true, "live": true, "aweme_orders": true,
		"targeting": true, "compliance": true, "operations": true,
	}
	seenNames, seenPaths := map[string]bool{}, map[string]bool{}
	for _, query := range catalog.Queries {
		doc, err := url.Parse(query.DocumentationURL)
		if !officialQueryIDPattern.MatchString(query.QueryID) ||
			query.QueryID != query.UpstreamTool || !groups[query.Group] ||
			query.Status != OfficialQueryPublishedOnly || query.Executable ||
			!strings.HasPrefix(query.APIPath, "/open_api/") ||
			strings.ContainsAny(query.APIPath, "?#\\%") ||
			strings.Contains(query.APIPath, "..") ||
			seenNames[query.QueryID] || seenPaths[query.APIPath] ||
			err != nil || doc.Scheme != "https" || doc.Host != "open.oceanengine.com" ||
			doc.User != nil || doc.RawQuery != "" || doc.Fragment != "" ||
			!strings.HasPrefix(doc.Path, "/labels/12/docs/") {
			return OfficialQueryCatalog{}, ErrInvalidOfficialQueryCatalog
		}
		seenNames[query.QueryID], seenPaths[query.APIPath] = true, true
	}
	return catalog, nil
}

type QueryExecutionMode string

const (
	QueryModeRead    QueryExecutionMode = "read"
	QueryModePlan    QueryExecutionMode = "plan"
	QueryModeExecute QueryExecutionMode = "execute"
)

type OfficialQueryScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// OfficialQueryPolicy is a server-owned contract draft, not a request field.
// A future Application handler must resolve it from the approved query ID and
// enforce the fixed instance policy before token acquisition or dispatch.
type OfficialQueryPolicy struct {
	CapabilityID        string             `json:"capability_id"`
	RequiredToolsets    []string           `json:"required_toolsets"`
	MinimumMode         QueryExecutionMode `json:"minimum_mode"`
	Channel             string             `json:"channel"`
	Effect              Effect             `json:"effect"`
	BusinessMethod      string             `json:"business_method"`
	ScopeResolver       string             `json:"scope_resolver"`
	SubjectFields       []string           `json:"subject_fields"`
	Roles               []string           `json:"roles"`
	Scenes              []string           `json:"scenes"`
	InputSchema         json.RawMessage    `json:"input_schema"`
	OutputSchema        json.RawMessage    `json:"output_schema"`
	SourceDigest        string             `json:"source_digest"`
	ContractFingerprint string             `json:"contract_fingerprint"`
}

// Raw parameter values preserve long integer and decimal tokens. Schema and
// ownership validation are query-specific and are not replaced by this decoder.
type OfficialQueryRequest struct {
	QueryID       string                     `json:"query_id"`
	Scope         OfficialQueryScope         `json:"scope"`
	AuthAccountID string                     `json:"auth_account_id,omitempty"`
	Parameters    map[string]json.RawMessage `json:"parameters"`
	Limit         int                        `json:"limit"`
	Cursor        string                     `json:"cursor,omitempty"`
}

func DecodeOfficialQueryRequest(reader io.Reader) (OfficialQueryRequest, error) {
	var request OfficialQueryRequest
	data, err := io.ReadAll(io.LimitReader(reader, MaxOfficialQueryRequestBytes+1))
	if err != nil || len(data) > MaxOfficialQueryRequestBytes {
		return request, ErrInvalidOfficialQueryRequest
	}
	var envelope map[string]json.RawMessage
	if decodeOneJSON(data, &envelope) != nil || envelope == nil {
		return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
	}
	allowed := map[string]bool{
		"query_id": true, "scope": true, "auth_account_id": true,
		"parameters": true, "limit": true, "cursor": true,
	}
	for name := range envelope {
		if !allowed[name] {
			return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
		}
	}
	var scopeFields map[string]json.RawMessage
	if decodeOneJSON(envelope["scope"], &scopeFields) != nil {
		return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
	}
	for name := range scopeFields {
		if name != "kind" && name != "id" {
			return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
		}
	}
	if err := decodeOneJSON(data, &request); err != nil ||
		!officialQueryIDPattern.MatchString(request.QueryID) ||
		!validOfficialQueryScope(request.Scope) ||
		request.AuthAccountID != "" && !officialDecimalIDPattern.MatchString(request.AuthAccountID) ||
		request.Limit < 0 || request.Limit > 1000 || len(request.Cursor) > 4096 ||
		len(request.Parameters) > 128 {
		return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
	}
	if request.Limit == 0 {
		request.Limit = 200
	}
	if request.Parameters == nil {
		request.Parameters = map[string]json.RawMessage{}
	}
	for name, raw := range request.Parameters {
		var value any
		if name == "" || len(name) > 128 || reservedOfficialQueryField(name) ||
			decodeOneJSON(raw, &value) != nil ||
			!validOfficialQueryParameter(value, 0) {
			return OfficialQueryRequest{}, ErrInvalidOfficialQueryRequest
		}
	}
	return request, nil
}

func validOfficialQueryScope(scope OfficialQueryScope) bool {
	switch scope.Kind {
	case "advertiser", "agent", "shop", "customer_center", "organization":
		return officialDecimalIDPattern.MatchString(scope.ID)
	case "authorization":
		return scope.ID != "" && len(scope.ID) <= 256 &&
			!strings.ContainsAny(scope.ID, "/\\?#\x00\r\n\t ")
	case "public_catalog":
		return scope.ID == ""
	default:
		return false
	}
}

func reservedOfficialQueryField(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	switch normalized {
	case "accesstoken", "refreshtoken", "secret", "cookie", "authcode",
		"authorization", "apikey", "clientsecret", "appsecret",
		"headers", "serverurl", "proxy", "httpmethod", "executionmode", "requiredtoolsets":
		return true
	}
	return false
}

func validOfficialQueryParameter(value any, depth int) bool {
	if depth > 8 {
		return false
	}
	switch value := value.(type) {
	case map[string]any:
		if len(value) > 256 {
			return false
		}
		for key, child := range value {
			if len(key) > 128 || reservedOfficialQueryField(key) ||
				!validOfficialQueryParameter(child, depth+1) {
				return false
			}
		}
	case []any:
		if len(value) > 1000 {
			return false
		}
		for _, child := range value {
			if !validOfficialQueryParameter(child, depth+1) {
				return false
			}
		}
	case string:
		return len(value) <= 8192
	case nil, bool, json.Number:
		return true
	default:
		return false
	}
	return true
}

func decodeOneJSON(data []byte, target any) error {
	keys := json.NewDecoder(bytes.NewReader(data))
	keys.UseNumber()
	if err := checkUniqueJSONKeys(keys, 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

// DecodeStrictJSON rejects repeated fields and trailing values before a Host
// decodes task inputs. It never persists the input or accepts a partial value.
func DecodeStrictJSON(data []byte, target any) error {
	if len(data) > MaxOfficialQueryRequestBytes {
		return ErrInvalidOfficialQueryRequest
	}
	return decodeOneJSON(data, target)
}

func checkUniqueJSONKeys(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("invalid or duplicate JSON field")
			}
			seen[name] = true
			if err := checkUniqueJSONKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkUniqueJSONKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

// OfficialQueryResult is a projected business result, not an upstream payload.
// IDs, integers and decimals use string cells; missing fields remain null.
// Columns define their meaning and units. Projection and completeness validation
// belong to the query-specific Application mapping before a Host can receive it.
type OfficialQueryResult struct {
	QueryID          string                    `json:"query_id"`
	Source           string                    `json:"source"`
	ObservedAt       string                    `json:"observed_at"`
	Channel          string                    `json:"channel"`
	Scope            OfficialQueryScope        `json:"scope"`
	Period           *OfficialQueryPeriod      `json:"period,omitempty"`
	StatisticalScope string                    `json:"statistical_scope"`
	Status           string                    `json:"status"`
	Columns          []OfficialQueryColumn     `json:"columns"`
	Items            []map[string]*string      `json:"items"`
	Completeness     OfficialQueryCompleteness `json:"completeness"`
	HasMore          bool                      `json:"has_more"`
	Cursor           string                    `json:"cursor,omitempty"`
	Failures         []OfficialQueryFailure    `json:"failures"`
	Presentation     *domain.Presentation      `json:"presentation,omitempty"`
}

type OfficialQueryPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type OfficialQueryColumn struct {
	Field     string `json:"field"`
	Label     string `json:"label"`
	ValueType string `json:"value_type"`
	Unit      string `json:"unit,omitempty"`
}

type OfficialQueryCompleteness struct {
	Complete bool   `json:"complete"`
	Returned int    `json:"returned"`
	Limit    int    `json:"limit"`
	Reason   string `json:"reason,omitempty"`
}

type OfficialQueryFailure struct {
	QueryID         string              `json:"query_id"`
	Code            string              `json:"code"`
	Stage           string              `json:"stage"`
	Dispatched      bool                `json:"dispatched"`
	Scope           *OfficialQueryScope `json:"scope,omitempty"`
	AuthorizationID string              `json:"authorization_id,omitempty"`
	APICode         string              `json:"api_code,omitempty"`
	RequiredFields  []string            `json:"required_fields,omitempty"`
}
