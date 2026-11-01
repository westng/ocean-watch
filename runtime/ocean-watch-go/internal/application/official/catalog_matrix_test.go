package official

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

type matrixAppTokens struct{}

func (matrixAppTokens) EnsureApp(_ context.Context, app string) (port.AppTokenLease, error) {
	return port.AppTokenLease{AppID: app, Token: "synthetic-app-token"}, nil
}

func matrixService(t *testing.T) (Service, *stateFixture, *tokensFixture, *backendFixture) {
	s, state, tokens, backend := fixtureService(t)
	grant := configuration.Object(configuration.Object(state.state["authorizations"])[testGrant])
	accounts := configuration.List(grant["authorized_accounts"])
	for kind, id := range map[string]string{"AGENT": "301", "CUSTOMER_ADMIN": "302", "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN": "303"} {
		accounts = append(accounts, map[string]any{"account_id": id, "account_role": kind, "advertiser_ids": []any{testAD}})
		configuration.Object(state.state["account_index"])[id] = testGrant
	}
	grant["authorized_accounts"] = accounts
	s.AppTokens = matrixAppTokens{}
	return s, state, tokens, backend
}

func matrixSubject(kind string) string {
	switch kind {
	case "advertiser", "account":
		return testAD
	case "agent":
		return "301"
	case "customer_center":
		return "302"
	case "organization":
		return "303"
	case "shop":
		return "300"
	case "authorization":
		return testGrant
	default:
		return "77"
	}
}

func matrixInput(schema map[string]any, name string) any {
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	switch schema["type"] {
	case "object":
		object := map[string]any{}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, key := range required {
			text, _ := key.(string)
			child, _ := properties[text].(map[string]any)
			object[text] = matrixInput(child, text)
		}
		return object
	case "array":
		items, _ := schema["items"].(map[string]any)
		return []any{matrixInput(items, name)}
	case "boolean":
		return false
	case "integer", "number":
		if kind := contracts.OfficialSubjectKind(name); kind != "" {
			return json.Number(matrixSubject(kind))
		}
		if minimum, ok := schema["minimum"]; ok {
			return minimum
		}
		return json.Number("1")
	default:
		if contracts.OfficialIdentifierField(name) {
			return matrixSubject(contracts.OfficialSubjectKind(name))
		}
		if schema["format"] == "datetime" {
			return "2026-10-31 00:00:00"
		}
		if strings.Contains(name, "time") || strings.Contains(name, "date") {
			return "2026-10-31"
		}
		return "fixture"
	}
}

func matrixRequest(t *testing.T, query contracts.ApprovedOfficialQuery) contracts.OfficialQueryRequest {
	t.Helper()
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(query.HostInput)))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	parameters, _ := matrixInput(schema, "").(map[string]any)
	for i := 0; i < 4; i++ {
		for _, rule := range query.ConditionalRules {
			if !conditionalRuleApplies(rule, parameters) {
				continue
			}
			for _, path := range rule.Required {
				parts := strings.Split(path, ".")
				node, object := schema, parameters
				for index, part := range parts {
					props, _ := node["properties"].(map[string]any)
					node, _ = props[part].(map[string]any)
					if index == len(parts)-1 {
						if object[part] == nil {
							object[part] = matrixInput(node, part)
						}
						break
					}
					if object[part] == nil {
						object[part] = map[string]any{}
					}
					object, _ = object[part].(map[string]any)
				}
			}
		}
	}
	rawParameters := map[string]json.RawMessage{}
	for name, value := range parameters {
		if name == query.PageField || name == "cursor" || name == "offset" {
			continue
		}
		rawParameters[name] = raw(value)
	}
	kind := query.ScopeKinds[0]
	id := matrixSubject(kind)
	if kind == "public_catalog" {
		id = ""
	}
	return contracts.OfficialQueryRequest{QueryID: query.QueryID, Scope: contracts.OfficialQueryScope{Kind: kind, ID: id}, Parameters: rawParameters, Limit: 100}
}

type matrixOutputNode struct {
	name, kind string
	children   map[string]*matrixOutputNode
}

func (node *matrixOutputNode) render() any {
	array := strings.HasSuffix(node.kind, "[]") || node.kind == "list"
	if len(node.children) > 0 {
		object := map[string]any{}
		for name, child := range node.children {
			object[name] = child.render()
		}
		if array {
			return []any{object}
		}
		return object
	}
	var value any
	kind := strings.TrimSuffix(node.kind, "[]")
	switch kind {
	case "number", "float", "double":
		value = json.Number("1")
		if contracts.OfficialIdentifierField(node.name) {
			value = json.Number("77")
		}
		if subject := contracts.OfficialSubjectKind(node.name); subject != "" {
			value = json.Number(matrixSubject(subject))
		}
	case "bool", "boolean":
		value = false
	case "json", "object", "objcet", "dict[string[]]", "dict[string]", "dict[object]":
		value = map[string]any{"synthetic": "fixture"}
	default:
		value = "fixture"
		if contracts.OfficialIdentifierField(node.name) {
			value = matrixSubject(contracts.OfficialSubjectKind(node.name))
		}
	}
	if node.name == "cursor" || node.name == "offset" || node.name == "next_cursor" {
		value = json.Number("0")
	}
	if node.name == "has_more" && kind == "number" {
		value = json.Number("0")
	}
	if array {
		return []any{value}
	}
	return value
}

func matrixData(query contracts.ApprovedOfficialQuery) any {
	root := &matrixOutputNode{children: map[string]*matrixOutputNode{}}
	for _, field := range query.OutputFields {
		parts := strings.Split(field.Path, ".")
		if parts[0] != "data" {
			continue
		}
		node := root
		for _, part := range parts[1:] {
			if node.children[part] == nil {
				node.children[part] = &matrixOutputNode{name: part, children: map[string]*matrixOutputNode{}}
			}
			node = node.children[part]
		}
		node.kind = field.Type
	}
	return root.render()
}

// Every published identity crosses Core with its own frozen input, subject,
// output and paging shape. These are synthetic contracts, not account access
// claims. Curated semantic cases below cover errors the shape matrix cannot.
func TestAll156OfficialQueriesCoreContractMatrix(t *testing.T) {
	queries, err := contracts.ApprovedOfficialQueries()
	if err != nil || len(queries) != 156 {
		t.Fatal("published inventory unavailable")
	}
	for _, query := range queries {
		t.Run(query.QueryID, func(t *testing.T) {
			if !query.Executable {
				t.Fatalf("unimplemented contract: %v", query.MissingEvidence)
			}
			s, _, tokens, backend := matrixService(t)
			request := matrixRequest(t, query)
			data := matrixData(query)
			if query.ProjectionPath != "" {
				data = map[string]any{"ad_id": json.Number("77"), "pivative_words": data}
			}
			backend.data = data
			result, err := s.Query(fixtureContext(t), request)
			if err != nil {
				if errorCode(err) == "INVALID_PARAMETERS" {
					t.Logf("synthetic parameters: %s", mustMatrixJSON(request.Parameters))
				}
				if errorCode(err) == "OUTPUT_TYPE_CHANGED" {
					matrixProjectionDiagnostic(t, query, data)
				}
				t.Fatalf("synthetic normal response: %v", err)
			}
			if backend.calls != 1 || tokens.calls != 1 || len(result.Columns) == 0 || len(result.Items) == 0 {
				t.Fatal("missing projection or unexpected dispatch")
			}
			if query.PagingKind == "unverified" && result.Completeness.Complete {
				t.Fatal("unknown pagination reported complete")
			}
			backend.data = nil
			if query.ProjectionPath == "" {
				if result, err := s.Query(fixtureContext(t), request); err != nil || result.Status != "no_data" {
					t.Fatal("no-data contract changed")
				}
			} else if _, err := s.Query(fixtureContext(t), request); errorCode(err) != "RESPONSE_SCOPE_MISMATCH" {
				t.Fatal("missing mapped detail became empty words")
			}
			backend.err = &port.Failure{Code: "OFFICIAL_API_ERROR", Stage: "business", APICode: "40000", Dispatched: true}
			if _, err := s.Query(fixtureContext(t), request); errorCode(err) != "OFFICIAL_API_ERROR" {
				t.Fatal("failure disappeared")
			}
			backend.err = nil
			before := tokens.calls
			request.Parameters["unexpected_authority"] = raw("synthetic-value")
			if _, err := s.Query(fixtureContext(t), request); err == nil || tokens.calls != before {
				t.Fatal("unknown input reached TokenManager")
			}
			delete(request.Parameters, "unexpected_authority")
			foreign := request
			foreign.Scope.ID = "99"
			if _, err := s.Query(fixtureContext(t), foreign); err == nil || tokens.calls != before {
				t.Fatal("foreign subject reached TokenManager")
			}
			ctx, cancel := context.WithCancel(fixtureContext(t))
			cancel()
			if _, err := s.Query(ctx, request); err == nil || tokens.calls != before {
				t.Fatal("cancelled query reached TokenManager")
			}
			s.Policy = policy.Instance{}
			if _, err := s.Query(fixtureContext(t), request); errorCode(err) != "POLICY_DENIED" || tokens.calls != before {
				t.Fatal("invalid startup policy reached TokenManager")
			}
		})
	}
}

func mustMatrixJSON(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }
func matrixProjectionDiagnostic(t *testing.T, query contracts.ApprovedOfficialQuery, data any) {
	for _, field := range query.OutputFields {
		container := false
		for _, other := range query.OutputFields {
			if strings.HasPrefix(other.Path, field.Path+".") {
				container = true
				break
			}
		}
		if container {
			continue
		}
		if value, present := readValue(data, strings.Split(strings.TrimPrefix(field.Path, "data."), ".")); present {
			if _, err := projectCell(value, field); err != nil {
				t.Logf("synthetic output field: %s (%s), type=%T, error=%s", field.Path, field.Type, value, errorCode(err))
			}
		}
	}
}
