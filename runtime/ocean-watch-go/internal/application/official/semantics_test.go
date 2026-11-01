package official

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/policy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

func TestConditionalFieldsAndDatesRejectBeforeCredentials(t *testing.T) {
	for _, test := range []struct {
		query       string
		params      map[string]json.RawMessage
		code, field string
	}{
		{"qianchuan_tools_log_search_v1", map[string]json.RawMessage{"object_type": raw("AD")}, "MISSING_REQUIRED_PARAMETERS", "object_id"},
		{"tools_interest_action_keyword_suggest_v2", map[string]json.RawMessage{"id": raw("77"), "tag_type": raw("KEYWORD"), "targeting_type": raw("ACTION"), "action_days": raw(7)}, "MISSING_REQUIRED_PARAMETERS", "action_scene"},
		{"qianchuan_suggest_budget_v1", map[string]json.RawMessage{"aweme_id": raw("77"), "live_schedule_type": raw("SCHEDULE_TIME_FIXEDRANGE")}, "MISSING_REQUIRED_PARAMETERS", "schedule_fixed_range"},
		{"qianchuan_suggest_budget_v1", map[string]json.RawMessage{"aweme_id": raw("77"), "live_schedule_type": raw("SCHEDULE_FROM_NOW"), "schedule_fixed_range": raw(1800)}, "PARAMETER_CONDITION_CONFLICT", ""},
		{"qianchuan_suggest_budget_v1", map[string]json.RawMessage{"aweme_id": raw("77"), "live_schedule_type": raw("SCHEDULE_TIME_FIXEDRANGE"), "schedule_fixed_range": raw(1801)}, "INVALID_PARAMETERS", ""},
		{"qianchuan_uni_promotion_list_v1", map[string]json.RawMessage{"marketing_goal": raw("VIDEO_PROM_GOODS"), "fields": raw([]string{"stat_cost"}), "start_time": raw("2026-10-31"), "end_time": raw("2026-10-31")}, "INVALID_PARAMETERS", ""},
		{"qianchuan_uni_promotion_list_v1", map[string]json.RawMessage{"marketing_goal": raw("VIDEO_PROM_GOODS"), "fields": raw([]string{"stat_cost"}), "start_time": raw("2026-02-30 00:00:00"), "end_time": raw("2026-03-01 00:00:00")}, "INVALID_DATE_RANGE", ""},
	} {
		t.Run(test.query+test.code+test.field, func(t *testing.T) {
			s, _, tokens, backend := fixtureService(t)
			r := fixtureRequest(test.query)
			r.Parameters = test.params
			_, err := s.Query(fixtureContext(t), r)
			if errorCode(err) != test.code || tokens.calls != 0 || backend.calls != 0 {
				t.Fatalf("wrong pre-dispatch result: %v", err)
			}
			if test.field != "" {
				var f *port.Failure
				if !errors.As(err, &f) || !contains(f.RequiredFields, test.field) {
					t.Fatal("missing-field hint lost")
				}
			}
		})
	}
}

func TestProductPagingUsesDocumentedSceneAndTab(t *testing.T) {
	s, _, _, backend := fixtureService(t)
	request := fixtureRequest("qianchuan_product_available_get_v1")
	request.Parameters = map[string]json.RawMessage{"campaign_scene": raw("NEW_PRODUCT_BOOST")}
	backend.data = fixtureJSON(t, `{"product_list":[{"id":77}],"page_info":{"cursor":20,"has_more":true}}`)
	first, err := s.Query(fixtureContext(t), request)
	if err != nil || !first.HasMore || first.Completeness.Complete || first.Cursor == "" {
		t.Fatalf("documented cursor scene failed: %v", err)
	}
	if backend.requests[0].Arguments["page"] != nil || backend.requests[0].Arguments["cursor"] != nil {
		t.Fatal("initial cursor scene injected page or cursor")
	}
	request.Cursor = first.Cursor
	backend.data = fixtureJSON(t, `{"product_list":[{"id":78}],"page_info":{"cursor":0,"has_more":false}}`)
	last, err := s.Query(fixtureContext(t), request)
	if err != nil || last.HasMore || last.Completeness.Complete || string(backend.requests[1].Arguments["cursor"]) != "20" {
		t.Fatalf("cursor continuation lost its bound value: %v", err)
	}
	request.Parameters["campaign_scene"] = raw("DAILY_SALE")
	if _, err := s.Query(fixtureContext(t), request); errorCode(err) != "INVALID_CURSOR" {
		t.Fatal("cursor crossed scene")
	}
	request = fixtureRequest("qianchuan_uni_promotion_product_get_v1")
	request.Parameters = map[string]json.RawMessage{"filtering": raw(map[string]any{"tab": "NEW_PRODUCT"})}
	backend.data = fixtureJSON(t, `{"product_list":[{"id":77}],"page_info":{"cursor":20}}`)
	result, err := s.Query(fixtureContext(t), request)
	if err != nil || result.Completeness.Complete || result.HasMore || result.Completeness.Reason != "pagination_contract_unverified" {
		t.Fatalf("unverified tab claimed complete pagination: %v", err)
	}
	if backend.requests[len(backend.requests)-1].Arguments["page"] != nil {
		t.Fatal("cursor-only tab injected page")
	}
}

func TestExplicitGrantRestrictionAppliesThroughAdvertiserScope(t *testing.T) {
	s, _, tokens, backend := fixtureService(t)
	s.Policy, _ = policy.New(policy.Config{Mode: "read", Toolsets: []string{"core", "qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {testAD}, "authorization": {"different-grant"}}}})
	if _, err := s.Query(fixtureContext(t), fixtureRequest("qianchuan_account_balance_get_v1")); errorCode(err) != "SCOPE_DENIED" || tokens.calls != 0 || backend.calls != 0 {
		t.Fatal("grant bound bypassed")
	}
}

func TestParentScopeChecksRelationshipAndChildIntersection(t *testing.T) {
	query, _, _ := contracts.ApprovedOfficialQueryByID("account_fund_get_v3")
	for _, narrow := range []bool{false, true} {
		s, state, tokens, backend := matrixService(t)
		bounds := map[string][]string{"shop": {"300"}}
		if narrow {
			bounds["advertiser"] = []string{"99"}
		}
		s.Policy, _ = policy.New(policy.Config{Mode: "read", Toolsets: []string{"core", "qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": bounds}})
		r := matrixRequest(t, query)
		r.Scope = contracts.OfficialQueryScope{Kind: "shop", ID: "300"}
		backend.data = matrixData(query)
		_, err := s.Query(fixtureContext(t), r)
		if narrow {
			if err == nil || tokens.calls != 0 {
				t.Fatal("child bound bypassed")
			}
		} else if err != nil {
			t.Fatalf("verified child rejected: %v", err)
		}
		accounts := configuration.List(configuration.Object(configuration.Object(state.state["authorizations"])[testGrant])["authorized_accounts"])
		if err := s.verifyResponseScope(map[string]any{"advertiser_id": json.Number("99")}, r.Scope, []string{testAD, "99"}, accounts); errorCode(err) != "RESPONSE_SCOPE_MISMATCH" {
			t.Fatal("unrelated advertiser escaped parent")
		}
	}
}

type healthAccounts struct{ book domain.AccountBook }

func (s *healthAccounts) Read(context.Context) (domain.AccountBook, error) { return s.book, nil }

func TestHealthCursorBindsMembershipSelectorsAndPolicy(t *testing.T) {
	s, state, tokens, backend := fixtureService(t)
	grant := configuration.Object(configuration.Object(state.state["authorizations"])[testGrant])
	grant["advertiser_ids"] = []any{testAD, "99"}
	configuration.Object(state.state["advertiser_index"])["99"] = []any{testGrant}
	book := &healthAccounts{book: domain.AccountBook{Accounts: map[domain.Channel][]domain.ManagedAccount{domain.Qianchuan: {{AdvertiserID: testAD, Enabled: true}, {AdvertiserID: "99", Enabled: true}}}}}
	backend.data = fixtureJSON(t, `{"account_valid":1}`)
	tasks := Tasks{Queries: s, Accounts: book}
	r := HealthRequest{ManagedAccounts: true, Checks: []string{"balance"}, Limit: 1}
	first, err := tasks.Health(fixtureContext(t), r)
	if err != nil || !first.HasMore || first.Cursor == "" || first.TotalAccounts != 2 || first.ProcessedAccounts != 1 {
		t.Fatalf("health page: %v", err)
	}
	r.Cursor = first.Cursor
	before := tokens.calls
	tampered := r
	tampered.Cursor += "x"
	if _, err := tasks.Health(fixtureContext(t), tampered); errorCode(err) != "INVALID_CURSOR" || tokens.calls != before {
		t.Fatal("tampered cursor accepted")
	}
	changed := r
	changed.Checks = []string{"quota"}
	if _, err := tasks.Health(fixtureContext(t), changed); errorCode(err) != "INVALID_CURSOR" || tokens.calls != before {
		t.Fatal("cursor crossed checks")
	}
	book.book.Accounts[domain.Qianchuan][0].AuthAccountID = "300"
	if _, err := tasks.Health(fixtureContext(t), r); errorCode(err) != "INVALID_CURSOR" || tokens.calls != before {
		t.Fatal("cursor crossed selector")
	}
	book.book.Accounts[domain.Qianchuan][0].AuthAccountID = ""
	tasks.Queries.Policy, _ = policy.New(policy.Config{Mode: "read", Toolsets: []string{"core", "qianchuan"}})
	if _, err := tasks.Health(fixtureContext(t), r); errorCode(err) != "INVALID_CURSOR" || tokens.calls != before {
		t.Fatal("cursor crossed policy")
	}
	book.book.Accounts[domain.Qianchuan] = nil
	r.Cursor = ""
	if result, err := tasks.Health(fixtureContext(t), r); err != nil || result.Status != "no_data" || tokens.calls != before {
		t.Fatal("empty scope became healthy")
	}
}

func TestFailuresAndOutputDriftCannotBecomeHealthy(t *testing.T) {
	s, _, _, backend := fixtureService(t)
	backend.err = &port.Failure{Code: "OFFICIAL_API_ERROR", Stage: "business", APICode: "50000", Dispatched: true}
	result, err := (Tasks{Queries: s}).Health(fixtureContext(t), HealthRequest{AdvertiserID: testAD, Checks: []string{"budget"}})
	if err != nil || result.Status != "failed" || len(result.Failures) != 1 || result.Failures[0].APICode != "50000" {
		t.Fatal("failed budget became successful")
	}
	backend.err = nil
	backend.data = fixtureJSON(t, `{"account_valid":false}`)
	if _, err := s.Query(fixtureContext(t), fixtureRequest("qianchuan_account_balance_get_v1")); errorCode(err) != "OUTPUT_TYPE_CHANGED" {
		t.Fatal("boolean amount accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Query(ctx, fixtureRequest("qianchuan_account_balance_get_v1")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled query advanced")
	}
}

func TestDiagnosisUsesCurrentSceneAndRejectsForeignPlan(t *testing.T) {
	s, _, _, backend := matrixService(t)
	backend.respond = func(r port.Request) any {
		if r.QueryID == "qianchuan_uni_promotion_ad_detail_v1" {
			return fixtureJSON(t, `{"ad_id":77,"adlab_scene":1}`)
		}
		q, _, _ := contracts.ApprovedOfficialQueryByID(r.QueryID)
		return matrixData(q)
	}
	result, err := (Tasks{Queries: s}).Diagnose(fixtureContext(t), PlanDiagnosisRequest{AdvertiserID: testAD, PlanID: "77"})
	if err != nil || result.Scene != "overall" || len(result.Facts) != 2 || len(result.Skipped) != 2 {
		t.Fatalf("diagnostic scene: %v", err)
	}
	for _, r := range backend.requests {
		if r.QueryID == "qianchuan_ad_learing_status_get_v1" || r.QueryID == "qianchuan_ad_compensate_status_get_v1" {
			t.Fatal("legacy fact used for overall plan")
		}
	}
	backend.respond = nil
	backend.data = fixtureJSON(t, `{"ad_id":78,"adlab_scene":1}`)
	r := fixtureRequest("qianchuan_uni_promotion_ad_detail_v1")
	r.Parameters = map[string]json.RawMessage{"ad_id": raw("77")}
	if _, err := s.Query(fixtureContext(t), r); errorCode(err) != "PLAN_SCOPE_MISMATCH" {
		t.Fatal("foreign plan facts accepted")
	}
}
