package official

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

const testAD = "9007199254740993"
const testGrant = "fixture-grant"

type stateFixture struct {
	loads int
	state map[string]any
}

func (s *stateFixture) LoadChannel(context.Context, string) (map[string]any, error) {
	s.loads++
	return s.state, nil
}
func (s *stateFixture) UpdateChannel(context.Context, string, func(map[string]any) error) error {
	return errors.New("unexpected write")
}

type tokensFixture struct {
	calls int
	err   error
}

func (s *tokensFixture) Ensure(context.Context, auth.TokenQuery) (auth.TokenLease, error) {
	s.calls++
	return auth.TokenLease{Channel: "qianchuan", AuthorizationID: testGrant, TokenRevision: 1, AccessToken: "synthetic-token"}, s.err
}

type backendFixture struct {
	calls    int
	requests []port.Request
	data     any
	err      error
	respond  func(port.Request) any
}

func (b *backendFixture) Read(_ context.Context, request port.Request) (port.Response, error) {
	b.calls++
	b.requests = append(b.requests, request)
	data := b.data
	if b.respond != nil {
		data = b.respond(request)
	}
	return port.Response{Data: data, APICode: "0"}, b.err
}
func fixtureJSON(t *testing.T, text string) any {
	t.Helper()
	var value any
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func fixtureService(t *testing.T) (Service, *stateFixture, *tokensFixture, *backendFixture) {
	t.Helper()
	state := &stateFixture{state: map[string]any{"authorizations": map[string]any{testGrant: map[string]any{"app_id": "100", "authorized_user_id": "200", "token_revision": 1, "advertiser_ids": []any{testAD}, "authorized_accounts": []any{map[string]any{"account_id": testAD, "account_role": "ADVERTISER", "advertiser_ids": []any{testAD}}, map[string]any{"account_id": "300", "account_role": "PLATFORM_ROLE_SHOP_ACCOUNT", "advertiser_ids": []any{testAD}}}}}, "advertiser_index": map[string]any{testAD: []any{testGrant}}, "account_index": map[string]any{testAD: testGrant, "300": testGrant}}}
	tokens := &tokensFixture{}
	backend := &backendFixture{}
	return Service{Policy: policy.Default(), Authorizations: state, Tokens: tokens, Backend: backend}, state, tokens, backend
}
func fixtureContext(t *testing.T) context.Context {
	t.Helper()
	ctx, _, _, err := requestcontrol.PrepareCommandContext(context.Background(), 16)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
func fixtureRequest(query string) contracts.OfficialQueryRequest {
	return contracts.OfficialQueryRequest{QueryID: query, Scope: contracts.OfficialQueryScope{Kind: "advertiser", ID: testAD}, Limit: 100}
}
func errorCode(err error) string {
	var failure *port.Failure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

func TestCatalogIsLocalAndCursorBound(t *testing.T) {
	service, state, tokens, backend := fixtureService(t)
	ctx := fixtureContext(t)
	query := CatalogQuery{Limit: 10}
	result, err := service.Catalog(ctx, query)
	if err != nil || result.Total != 156 || !result.HasMore {
		t.Fatalf("catalog %v %#v", err, result)
	}
	query.Cursor = result.Cursor
	next, err := service.Catalog(ctx, query)
	if err != nil || len(next.Items) != 10 || next.Items[0].QueryID == result.Items[0].QueryID {
		t.Fatal("catalog paging failed")
	}
	query.Keyword = "budget"
	if _, err := service.Catalog(ctx, query); errorCode(err) != "INVALID_CURSOR" {
		t.Fatal("catalog cursor escaped filter")
	}
	if state.loads != 0 || tokens.calls != 0 || backend.calls != 0 {
		t.Fatal("catalog touched business dependencies")
	}
}
func TestQueryRejectsAuthorityAndForeignSubjectsBeforeToken(t *testing.T) {
	for name, edit := range map[string]func(*Service, *contracts.OfficialQueryRequest){
		"unknown query": func(_ *Service, r *contracts.OfficialQueryRequest) { r.QueryID = "arbitrary_remote_tool" },
		"unknown parameter": func(_ *Service, r *contracts.OfficialQueryRequest) {
			r.Parameters = map[string]json.RawMessage{"unknown": raw("x")}
		},
		"foreign nested subject": func(_ *Service, r *contracts.OfficialQueryRequest) {
			r.Parameters = map[string]json.RawMessage{"advertiser_id": raw("999")}
		},
		"disabled toolset": func(s *Service, _ *contracts.OfficialQueryRequest) {
			s.Policy, _ = policy.New(policy.Config{Mode: contracts.QueryModeRead, Toolsets: []string{"core"}})
		},
		"local bound": func(s *Service, _ *contracts.OfficialQueryRequest) {
			s.Policy, _ = policy.New(policy.Config{Mode: contracts.QueryModeRead, Toolsets: []string{"core", "qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {"999"}}}})
		},
		"credential": func(_ *Service, r *contracts.OfficialQueryRequest) {
			r.Parameters = map[string]json.RawMessage{"APP-Access-Token": raw("fake-secret")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, tokens, backend := fixtureService(t)
			r := fixtureRequest("qianchuan_account_balance_get_v1")
			edit(&s, &r)
			if _, err := s.Query(fixtureContext(t), r); err == nil {
				t.Fatal("rejection accepted")
			}
			if tokens.calls != 0 || backend.calls != 0 {
				t.Fatal("rejected request acquired token or dispatched")
			}
		})
	}
}
func TestQueryProjectsExactAmountsAndMissingValues(t *testing.T) {
	s, _, tokens, backend := fixtureService(t)
	backend.data = fixtureJSON(t, `{"advertiser_id":9007199254740993,"account_total":1234567890.123456789,"account_valid":0,"account_frozen":null,"access_token":"synthetic-token"}`)
	result, err := s.Query(fixtureContext(t), fixtureRequest("qianchuan_account_balance_get_v1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || cellValue(result.Items[0]["account_total"]) != "1234567890.123456789" || result.Items[0]["account_frozen"] != nil || cellValue(result.Items[0]["advertiser_id"]) != testAD {
		t.Fatal("precision/null projection failed")
	}
	unit := ""
	for _, column := range result.Columns {
		if column.Field == "account_total" {
			unit = column.Unit
		}
	}
	if unit != "CNY/100000" || tokens.calls != 1 || backend.calls != 1 || string(backend.requests[0].Arguments["advertiser_id"]) != testAD {
		t.Fatal("units or exact wire ID failed")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "synthetic-token") {
		t.Fatal("credential leaked")
	}
	backend.data = fixtureJSON(t, `{"advertiser_id":999,"account_valid":10}`)
	if _, err := s.Query(fixtureContext(t), fixtureRequest("qianchuan_account_balance_get_v1")); errorCode(err) != "RESPONSE_SCOPE_MISMATCH" {
		t.Fatalf("foreign output not rejected: %v", err)
	}
}
func TestQueryBindsPagedContinuationAndPreservesIncompleteState(t *testing.T) {
	s, _, _, backend := fixtureService(t)
	backend.respond = func(r port.Request) any {
		page := string(r.Arguments["page"])
		if page == "1" {
			return fixtureJSON(t, `{"ad_list":[{"ad_info":{"id":9007199254740993,"name":"first"},"stats_info":{"stat_cost":1.01}}],"page_info":{"page":1,"page_size":1,"total_page":2,"total_num":2}}`)
		}
		return fixtureJSON(t, `{"ad_list":[{"ad_info":{"id":9007199254740995,"name":"last"},"stats_info":{"stat_cost":2.02}}],"page_info":{"page":2,"page_size":1,"total_page":2,"total_num":2}}`)
	}
	r := fixtureRequest("qianchuan_uni_promotion_list_v1")
	r.Parameters = map[string]json.RawMessage{"fields": raw([]string{"stat_cost"}), "marketing_goal": raw("VIDEO_PROM_GOODS"), "start_time": raw("2026-10-31 00:00:00"), "end_time": raw("2026-10-31 23:59:59")}
	first, err := s.Query(fixtureContext(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.Cursor == "" || first.Completeness.Complete {
		t.Fatal("first page incorrectly complete")
	}
	r.Cursor = first.Cursor
	second, err := s.Query(fixtureContext(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || second.Completeness.Complete || second.Completeness.Reason != "continuation_slice" {
		t.Fatal("last slice represented whole query")
	}
	r.Scope.ID = "999"
	before := backend.calls
	if _, err := s.Query(fixtureContext(t), r); err == nil || backend.calls != before {
		t.Fatal("cursor crossed account scope")
	}
}
func TestFrozenNegativeWordMappingCannotLeakPlanDetail(t *testing.T) {
	s, _, _, backend := fixtureService(t)
	backend.data = fixtureJSON(t, `{"ad_id":77,"name":"not requested detail","pivative_words":{"phrase_words":["one"],"precise_words":[]}}`)
	r := fixtureRequest("qianchuan_ad_pivativewords_get_v1")
	r.Parameters = map[string]json.RawMessage{"filtering": raw(map[string]string{"ad_id": "77"})}
	result, err := s.Query(fixtureContext(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if backend.requests[0].QueryID != "qianchuan_ad_detail_get_v1" || string(backend.requests[0].Arguments["ad_id"]) != "77" {
		t.Fatal("fixed mapping was not used")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "not requested detail") {
		t.Fatal("detail leaked through word query")
	}
	backend.data = fixtureJSON(t, `{"ad_id":77}`)
	if _, err := s.Query(fixtureContext(t), r); errorCode(err) != "REQUIRED_OUTPUT_MISSING" {
		t.Fatal("absent words treated as empty words")
	}
}
func TestReauthorizationKeepsExactOriginalScope(t *testing.T) {
	s, _, tokens, _ := fixtureService(t)
	tokens.err = domain.NewError("reauthorization_required", "untrusted diagnostic", 1, nil)
	_, err := s.Query(fixtureContext(t), fixtureRequest("qianchuan_account_balance_get_v1"))
	var controlled *port.Failure
	if !errors.As(err, &controlled) || controlled.Code != "REAUTHORIZATION_REQUIRED" || controlled.AuthorizationID != testGrant || controlled.AdvertiserID != testAD {
		t.Fatal("recovery scope lost")
	}
}
func TestQualityUnknownIsNullAndCannotUseForeignLabels(t *testing.T) {
	s, _, _, backend := fixtureService(t)
	backend.respond = func(r port.Request) any {
		if strings.Contains(r.QueryID, "original") {
			return fixtureJSON(t, `{"original_material_ids":["77"]}`)
		}
		return fixtureJSON(t, `{"inefficient_material_ids":[]}`)
	}
	tasks := Tasks{Queries: s}
	result, err := tasks.Quality(fixtureContext(t), MaterialQualityRequest{AdvertiserID: testAD, MaterialIDs: []string{"77", "78"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MaterialLabels) != 2 || result.MaterialLabels[0].Original == nil || !*result.MaterialLabels[0].Original || result.MaterialLabels[1].Original == nil || *result.MaterialLabels[1].Original {
		t.Fatal("known labels incorrect")
	}
	backend.err = &port.Failure{Code: "OFFICIAL_TOOL_UNAVAILABLE"}
	result, err = tasks.Quality(fixtureContext(t), MaterialQualityRequest{AdvertiserID: testAD, MaterialIDs: []string{"77"}})
	if err != nil || result.MaterialLabels[0].Original != nil || result.MaterialLabels[0].Inefficient != nil {
		t.Fatal("unknown labels became false")
	}
}
