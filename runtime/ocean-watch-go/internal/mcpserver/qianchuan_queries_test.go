package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

const queryTestSecret = "SECRET_TOKEN_COOKIE_RAW_URL_ERROR"

func TestQianchuanQueryToolsUseTaskServicesAndWhitelistOutputs(t *testing.T) {
	accounts := &managedAccountReaderStub{book: domain.AccountBook{
		SchemaVersion: domain.ManagedAccountSchemaVersion,
		Accounts: map[domain.Channel][]domain.ManagedAccount{
			domain.Marketing: {},
			domain.Qianchuan: {{
				Channel: domain.Qianchuan, AdvertiserID: "2000000000000001",
				Name: "fixture account", Enabled: true, AuthAccountID: "9000000000000001",
			}},
		},
	}}
	authorization := &authorizationReaderStub{result: authapplication.InspectionResult{
		Status: authapplication.StatusResult{
			Channel: "qianchuan", HasAppID: true, HasSecret: true, AuthorizationCount: 1,
			AuthorizedAccountCount: 1, AuthorizedAdvertiserCount: 1, Generation: 2,
			AdvertiserID: "2000000000000001", AdvertiserIDAuthorized: boolPointer(true),
			Authorizations: []authapplication.AuthorizationStatus{{
				AuthorizationID: "auth-fixture", TokenRevision: 3,
				HasAccessToken: true, HasRefreshToken: true,
				AccessTokenExpiresAt:  "2026-08-17T00:00:00Z",
				RefreshTokenExpiresAt: "2026-09-17T00:00:00Z",
			}},
		},
		Mappings: authapplication.MappingResult{
			Channel: "qianchuan", AuthorizationCount: 1, MappingCount: 1,
			Mappings: []authapplication.AdvertiserMapping{{
				AdvertiserID: "2000000000000001", AuthorizationIDs: []string{"auth-fixture"},
			}},
			Authorizations: []authapplication.AuthorizationMapping{{
				AuthorizationID: "auth-fixture", TokenRevision: 3,
				HasAccessToken: true, HasRefreshToken: true,
				AdvertiserIDs: []string{"2000000000000001"},
			}},
		},
	}}
	reads := &qianchuanReadServiceStub{
		productResult: applicationqianchuan.ProductResult{
			AdvertiserID: "2000000000000001",
			Products: []domainqianchuan.Product{{
				ProductID: "8000000000000001", Name: "fixture product",
				Image:        "https://private.invalid/" + queryTestSecret,
				SquareImages: []domainqianchuan.ProductImage{{URL: "https://private.invalid/" + queryTestSecret}},
				CategoryName: "fixture category", ChannelID: "channel", ChannelType: "QIANCHUAN",
			}},
		},
		planListResult: applicationqianchuan.PlanListResult{
			AdvertiserID: "2000000000000001", PlanCount: 1, DisplayedCount: 1,
			DataPeriod: applicationqianchuan.DatePeriod{StartDate: "2026-08-16", EndDate: "2026-08-16"},
			Plans: []applicationqianchuan.CompactPlan{{
				AdID: "3000000000000001", Name: "fixture plan", Status: "DELIVERY_OK",
				CreatorIDs: []string{"4000000000000001"},
			}},
		},
		planResult: applicationqianchuan.PlanDetailResult{
			AdvertiserID: "2000000000000001", AdID: "3000000000000001",
			Plan: domainqianchuan.PlanDetail{
				AdID: "3000000000000001", Name: "fixture plan", Status: "DELIVERY_OK",
				AwemeID:  "4000000000000001",
				Creators: []domainqianchuan.Creator{{AwemeID: "4000000000000001", VisibleID: "fixture-visible", Name: "fixture creator"}},
				Products: []domainqianchuan.PlanProduct{{
					ProductID: "8000000000000001", ProductName: "fixture product",
					ProductImage: "https://private.invalid/" + queryTestSecret,
				}},
			},
		},
		materialResult: applicationqianchuan.PlanMaterialsResult{
			AdvertiserID: "2000000000000001", AdID: "3000000000000001", MaterialCount: 1,
			Materials: []domainqianchuan.PlanMaterial{{
				MaterialID: "5000000000000001", AwemeItemID: "6000000000000001",
				Title: "fixture material", URL: "https://private.invalid/" + queryTestSecret,
			}},
		},
	}
	reports := &qianchuanReportServiceStub{
		uniResult: applicationreports.QianchuanAggregateResult{
			AdvertiserID: "2000000000000001",
			DateRange:    applicationreports.DateRange{StartDate: "2026-08-16", EndDate: "2026-08-16"},
			Data: map[string]any{
				"stat_cost": json.Number("12.5"), "total_pay_order_count_for_roi2": json.Number("2"),
				"total_pay_order_gmv_include_coupon_for_roi2": json.Number("20"),
				"total_prepay_and_pay_order_roi2":             json.Number("1.6"),
				"raw_private_field":                           queryTestSecret,
			},
		},
		planResult: applicationreports.PlanResult{
			OK: true, AdvertiserID: "2000000000000001",
			DateRange:  applicationreports.DateRange{StartDate: "2026-08-16", EndDate: "2026-08-16"},
			AmountUnit: "CNY", DisplayedCount: 1, TotalRowCount: 2,
			Summary: applicationreports.PlanSummary{
				PlanCount: 2, PlansWithCost: 1, TotalCost: domain.MustDecimal("12.5"),
				TotalPayOrderCount: 2, TotalPayOrderGMV: domain.MustDecimal("20"),
				TotalPayROI: domain.MustDecimal("1.6"),
			},
			Presentation: domain.Presentation{Required: true, RenderedMarkdown: "| 完整计划表 |\n| --- |\n| fixture |"},
			Rows: []map[string]any{{
				"ad_id": "3000000000000001", "name": "fixture plan",
				"budget_mode_label": "日预算", "cost_guarantee_result": "IN_EFFECT",
				"cost_guarantee_reason": "fixture reason", "raw_private_field": queryTestSecret,
			}},
		},
	}
	runtime := Runtime{
		ManagedAccounts: accounts, QianchuanAuth: authorization,
		QianchuanReads: reads, QianchuanReports: reports,
		RequestID: func() string { return "request-query" },
	}
	session := connectTestServer(t, runtime)
	defer session.Close()

	managed := callTestTool(t, session, "list_managed_accounts", map[string]any{"channel": "qianchuan"})
	managedOutput := decodeStructured[managedAccountsOutput](t, managed)
	if accounts.calls != 1 || len(managedOutput.Accounts) != 1 || managedOutput.Accounts[0].Name != "fixture account" ||
		managedOutput.Presentation.RenderedMarkdown == "" || bytes.Contains(resultBytes(t, managed), []byte("auth_account_id")) {
		t.Fatalf("managed-account contract changed: calls=%d output=%#v", accounts.calls, managedOutput)
	}

	authResult := callTestTool(t, session, "get_qianchuan_authorization", map[string]any{"advertiser_id": "2000000000000001"})
	authOutput := decodeStructured[qianchuanAuthorizationOutput](t, authResult)
	if authorization.calls != 1 || authOutput.Status.Generation != 2 || len(authOutput.Mappings) != 1 ||
		bytes.Contains(resultBytes(t, authResult), []byte("access_token\":\"")) {
		t.Fatalf("authorization contract changed: calls=%d output=%#v", authorization.calls, authOutput)
	}

	productResult := callTestTool(t, session, "search_qianchuan_products", map[string]any{
		"advertiser_id": "2000000000000001", "product_name": "fixture", "limit": 10,
	})
	productOutput := decodeStructured[qianchuanProductsOutput](t, productResult)
	if reads.productCalls != 1 || reads.lastProductMode != "qianchuan_product_list" ||
		reads.lastProductQuery.ProductName != "fixture" || len(productOutput.Items) != 1 {
		t.Fatalf("product service boundary changed: service=%#v output=%#v", reads, productOutput)
	}

	planListResult := callTestTool(t, session, "list_qianchuan_plans", map[string]any{
		"advertiser_id": "2000000000000001", "start_date": "2026-08-16", "end_date": "2026-08-16",
	})
	planListOutput := decodeStructured[qianchuanPlansOutput](t, planListResult)
	if reads.planListCalls != 1 || planListOutput.Items[0].AdID != "3000000000000001" {
		t.Fatalf("plan list boundary changed: service=%#v output=%#v", reads, planListOutput)
	}

	planResult := callTestTool(t, session, "get_qianchuan_plan", map[string]any{
		"advertiser_id": "2000000000000001", "ad_id": "3000000000000001", "include_materials": true,
	})
	planOutput := decodeStructured[qianchuanPlanOutput](t, planResult)
	if reads.planCalls != 1 || reads.materialCalls != 1 || planOutput.MaterialCount != 1 ||
		planOutput.Materials[0].MaterialID != "5000000000000001" {
		t.Fatalf("plan detail boundary changed: service=%#v output=%#v", reads, planOutput)
	}

	accountReport := callTestTool(t, session, "report_qianchuan_account", map[string]any{
		"advertiser_id": "2000000000000001", "scope": "uni",
	})
	accountOutput := decodeStructured[qianchuanAccountReportOutput](t, accountReport)
	if reports.uniCalls != 1 || reports.allCalls != 0 || accountOutput.Metrics.StatCost == nil {
		t.Fatalf("account report boundary changed: service=%#v output=%#v", reports, accountOutput)
	}

	planReport := callTestTool(t, session, "report_qianchuan_plans", map[string]any{
		"advertiser_id": "2000000000000001", "limit": 1,
	})
	planReportOutput := decodeStructured[qianchuanPlanReportOutput](t, planReport)
	if reports.planCalls != 1 || planReportOutput.Presentation.RenderedMarkdown != reports.planResult.Presentation.RenderedMarkdown ||
		len(planReportOutput.Details) != 1 || !planReportOutput.Truncated {
		t.Fatalf("plan report boundary changed: service=%#v output=%#v", reports, planReportOutput)
	}

	for _, result := range []*mcp.CallToolResult{productResult, planResult, accountReport, planReport} {
		if bytes.Contains(resultBytes(t, result), []byte(queryTestSecret)) || bytes.Contains(resultBytes(t, result), []byte("private.invalid")) {
			t.Fatalf("query tool leaked private upstream data: %s", resultBytes(t, result))
		}
	}
}

func TestQianchuanQueryToolsRejectInvalidArgumentsBeforeServices(t *testing.T) {
	reads := &qianchuanReadServiceStub{}
	reports := &qianchuanReportServiceStub{}
	runtime := Runtime{QianchuanReads: reads, QianchuanReports: reports, RequestID: func() string { return "request-invalid" }}
	session := connectTestServer(t, runtime)
	defer session.Close()

	inputs := []struct {
		name string
		args map[string]any
	}{
		{name: "search_qianchuan_products", args: map[string]any{"advertiser_id": "01"}},
		{name: "list_qianchuan_plans", args: map[string]any{"advertiser_id": "2000000000000001", "start_date": "2026-02-30", "end_date": "2026-02-30"}},
		{name: "get_qianchuan_plan", args: map[string]any{"advertiser_id": "2000000000000001", "ad_id": "3000000000000001", "config": "/tmp/private"}},
		{name: "report_qianchuan_account", args: map[string]any{"advertiser_id": "2000000000000001", "scope": "custom"}},
		{name: "report_qianchuan_plans", args: map[string]any{"advertiser_id": "9999999999999999999"}},
	}
	for _, test := range inputs {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.name, Arguments: test.args})
		if err != nil || !result.IsError || decodeStructured[errorEnvelope](t, result).Error.Code != "INVALID_ARGUMENT" {
			t.Fatalf("%s invalid input accepted: result=%#v err=%v", test.name, result, err)
		}
	}
	if reads.totalCalls() != 0 || reports.totalCalls() != 0 {
		t.Fatalf("invalid input reached services: reads=%d reports=%d", reads.totalCalls(), reports.totalCalls())
	}
}

func TestQianchuanQueryErrorsAreSanitized(t *testing.T) {
	reads := &qianchuanReadServiceStub{err: errors.New("upstream " + queryTestSecret + " /private/config.json")}
	runtime := Runtime{QianchuanReads: reads, RequestID: func() string { return "request-error" }}
	session := connectTestServer(t, runtime)
	defer session.Close()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_qianchuan_products", Arguments: map[string]any{"advertiser_id": "2000000000000001"},
	})
	if err != nil || !result.IsError {
		t.Fatalf("upstream error was not returned: result=%#v err=%v", result, err)
	}
	failure := decodeStructured[errorEnvelope](t, result)
	if failure.Error.Code != "UPSTREAM_QUERY_FAILED" || bytes.Contains(resultBytes(t, result), []byte(queryTestSecret)) ||
		bytes.Contains(resultBytes(t, result), []byte("/private/config.json")) {
		t.Fatalf("upstream error leaked: %s", resultBytes(t, result))
	}
}

type managedAccountReaderStub struct {
	book  domain.AccountBook
	err   error
	calls int
}

func (stub *managedAccountReaderStub) Read(context.Context) (domain.AccountBook, error) {
	stub.calls++
	return stub.book, stub.err
}

type authorizationReaderStub struct {
	result authapplication.InspectionResult
	err    error
	calls  int
}

func (stub *authorizationReaderStub) Inspect(context.Context, authapplication.StatusQuery) (authapplication.InspectionResult, error) {
	stub.calls++
	return stub.result, stub.err
}

type qianchuanReadServiceStub struct {
	productResult    applicationqianchuan.ProductResult
	planListResult   applicationqianchuan.PlanListResult
	planResult       applicationqianchuan.PlanDetailResult
	materialResult   applicationqianchuan.PlanMaterialsResult
	err              error
	productCalls     int
	planListCalls    int
	planCalls        int
	materialCalls    int
	lastProductQuery applicationqianchuan.ProductQuery
	lastProductMode  string
}

func (stub *qianchuanReadServiceStub) QueryProducts(_ context.Context, query applicationqianchuan.ProductQuery, mode string) (applicationqianchuan.ProductResult, error) {
	stub.productCalls++
	stub.lastProductQuery, stub.lastProductMode = query, mode
	return stub.productResult, stub.err
}

func (stub *qianchuanReadServiceStub) ListPlans(context.Context, applicationqianchuan.PlanListQuery) (applicationqianchuan.PlanListResult, error) {
	stub.planListCalls++
	return stub.planListResult, stub.err
}

func (stub *qianchuanReadServiceStub) ShowPlan(context.Context, applicationqianchuan.CredentialScope, string) (applicationqianchuan.PlanDetailResult, error) {
	stub.planCalls++
	return stub.planResult, stub.err
}

func (stub *qianchuanReadServiceStub) ListPlanMaterials(context.Context, applicationqianchuan.PlanMaterialsQuery) (applicationqianchuan.PlanMaterialsResult, error) {
	stub.materialCalls++
	return stub.materialResult, stub.err
}

func (stub *qianchuanReadServiceStub) totalCalls() int {
	return stub.productCalls + stub.planListCalls + stub.planCalls + stub.materialCalls
}

type qianchuanReportServiceStub struct {
	planResult applicationreports.PlanResult
	allResult  applicationreports.QianchuanAggregateResult
	uniResult  applicationreports.QianchuanAggregateResult
	err        error
	planCalls  int
	allCalls   int
	uniCalls   int
}

func (stub *qianchuanReportServiceStub) PlanReport(context.Context, applicationreports.PlanQuery) (applicationreports.PlanResult, error) {
	stub.planCalls++
	return stub.planResult, stub.err
}

func (stub *qianchuanReportServiceStub) QianchuanAllPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error) {
	stub.allCalls++
	return stub.allResult, stub.err
}

func (stub *qianchuanReportServiceStub) QianchuanUniPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error) {
	stub.uniCalls++
	return stub.uniResult, stub.err
}

func (stub *qianchuanReportServiceStub) totalCalls() int {
	return stub.planCalls + stub.allCalls + stub.uniCalls
}

func callTestTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result.IsError {
		t.Fatalf("%s result=%#v err=%v", name, result, err)
	}
	return result
}

func resultBytes(t *testing.T, result *mcp.CallToolResult) []byte {
	t.Helper()
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func boolPointer(value bool) *bool { return &value }
