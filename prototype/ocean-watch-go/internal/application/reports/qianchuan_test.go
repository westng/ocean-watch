package reports

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/reports"
	portreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/reports"
)

type qianchuanReportTokenStub struct {
	queries []authapplication.TokenQuery
}

func (stub *qianchuanReportTokenStub) Ensure(
	_ context.Context,
	query authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	stub.queries = append(stub.queries, query)
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: "fixture-authorization",
		AccessToken: "TEST_QIANCHUAN_TOKEN_DO_NOT_USE",
	}, nil
}

type qianchuanReportReaderStub struct {
	materialRequests []portreports.MaterialPageRequest
	schemaRequests   []portreports.PlanSchemaRequest
	metricRequests   []portreports.PlanMetricPageRequest
	metadataRequests []portreports.PlanMetadataPageRequest
	materialPages    map[int]domainreports.MaterialPage
	metricPages      map[int]domainreports.PlanMetricPage
	metadataPages    map[int]domainqianchuan.PlanPage
}

func (stub *qianchuanReportReaderStub) FetchMaterialPage(
	_ context.Context,
	request portreports.MaterialPageRequest,
) (domainreports.MaterialPage, error) {
	stub.materialRequests = append(stub.materialRequests, request)
	return stub.materialPages[request.Page], nil
}

func (stub *qianchuanReportReaderStub) FetchPlanSchema(
	_ context.Context,
	request portreports.PlanSchemaRequest,
) (domainreports.PlanSchema, error) {
	stub.schemaRequests = append(stub.schemaRequests, request)
	return domainreports.PlanSchema{
		Topic: PlanReportTopic, Dimensions: []string{"ad_id"},
		Metrics: append([]string(nil), DefaultPlanFields...), RequestID: "schema-request",
	}, nil
}

func (stub *qianchuanReportReaderStub) FetchPlanMetricPage(
	_ context.Context,
	request portreports.PlanMetricPageRequest,
) (domainreports.PlanMetricPage, error) {
	stub.metricRequests = append(stub.metricRequests, request)
	return stub.metricPages[request.Page], nil
}

func (stub *qianchuanReportReaderStub) FetchPlanMetadataPage(
	_ context.Context,
	request portreports.PlanMetadataPageRequest,
) (domainqianchuan.PlanPage, error) {
	stub.metadataRequests = append(stub.metadataRequests, request)
	return stub.metadataPages[request.Page], nil
}

func TestReportMetricContracts(t *testing.T) {
	t.Run("material summary uses all pages and keeps omitted metrics null", func(t *testing.T) {
		reader := &qianchuanReportReaderStub{materialPages: map[int]domainreports.MaterialPage{
			1: {
				Rows: []domainreports.MaterialRow{{
					MaterialID: "5000000000000001",
					Values:     map[string]any{"material_id": "5000000000000001", "stat_cost": "1.005"},
				}},
				PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 2, TotalNumber: 2},
			},
			2: {
				Rows: []domainreports.MaterialRow{{
					MaterialID: "5000000000000002",
					Values:     map[string]any{"material_id": "5000000000000002", "stat_cost": "2.005"},
				}},
				PageInfo:  domainqianchuan.PageInfo{Page: 2, TotalPages: 2, TotalNumber: 2},
				RequestID: "material-request-2",
			},
		}}
		tokens := &qianchuanReportTokenStub{}
		result, err := (Service{Tokens: tokens, Reader: reader}).MaterialReport(
			context.Background(),
			MaterialQuery{
				CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"},
				StartDate:       "2026-07-15", EndDate: "2026-07-15",
				Fields: []string{"stat_cost"}, Top: 1,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.RowCount != 2 || result.DisplayedCount != 1 || result.PageCount != 2 {
			t.Fatalf("material counts changed: %#v", result)
		}
		if result.Summary.TotalSpend == nil || result.Summary.TotalSpend.String() != "3.01" {
			t.Fatalf("material total did not aggregate raw pages: %#v", result.Summary)
		}
		if result.Summary.TotalPayOrderAmount != nil || result.Summary.TotalPayOrderCount != nil ||
			result.Summary.WeightedROI != nil {
			t.Fatalf("omitted material metrics were converted to zero: %#v", result.Summary)
		}
		if !reflect.DeepEqual(result.RequestIDs, []string{"material-request-2"}) {
			t.Fatalf("material request IDs changed: %v", result.RequestIDs)
		}
	})

	t.Run("specific status requires complete metadata", func(t *testing.T) {
		reader := planReportReaderFixture(false)
		_, err := (Service{Tokens: &qianchuanReportTokenStub{}, Reader: reader}).PlanReport(
			context.Background(),
			PlanQuery{
				CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"},
				StartDate:       "2026-07-15", EndDate: "2026-07-15", Status: "DELIVERY_OK",
			},
		)
		if err == nil || !strings.Contains(err.Error(), "metadata could not be resolved") {
			t.Fatalf("specific status accepted missing metadata: %v", err)
		}
	})
}

func TestQianchuanPlanReportSDKParity(t *testing.T) {
	reader := planReportReaderFixture(true)
	tokens := &qianchuanReportTokenStub{}
	result, err := (Service{
		Tokens: tokens, Reader: reader,
		Now: func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) },
	}).PlanReport(context.Background(), PlanQuery{
		CredentialScope: CredentialScope{
			AdvertiserID: "1000000000000001", AuthAccountID: "9000000000000001",
		},
		StartDate: "2026-07-15", EndDate: "2026-07-16", Top: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens.queries, []authapplication.TokenQuery{{
		Channel: "qianchuan", AdvertiserID: "1000000000000001", AuthAccountID: "9000000000000001",
	}}) {
		t.Fatalf("Qianchuan token scope changed: %#v", tokens.queries)
	}
	if len(reader.schemaRequests) != 1 || reader.schemaRequests[0].Topic != PlanReportTopic {
		t.Fatalf("plan schema contract changed: %#v", reader.schemaRequests)
	}
	if len(reader.metricRequests) != 2 || len(reader.metadataRequests) != 1 {
		t.Fatalf("plan page traversal changed: metric=%#v metadata=%#v", reader.metricRequests, reader.metadataRequests)
	}
	for _, request := range reader.metricRequests {
		if request.StartTime != "2026-07-15 00:00:00" || request.EndTime != "2026-07-16 23:59:59" ||
			request.Topic != PlanReportTopic || !reflect.DeepEqual(request.Dimensions, []string{"ad_id"}) ||
			!reflect.DeepEqual(request.Metrics, DefaultPlanFields) || request.OrderField != "stat_cost" ||
			request.OrderType != 2 || request.PageSize != DefaultPageSize {
			t.Fatalf("plan financial request changed: %#v", request)
		}
	}
	metadataRequest := reader.metadataRequests[0]
	if metadataRequest.StartTime != "2026-07-15 00:00:00" ||
		metadataRequest.EndTime != "2026-07-16 23:59:59" || metadataRequest.Status != "ALL" ||
		metadataRequest.MarketingGoal != "VIDEO_PROM_GOODS" || metadataRequest.AdlabScene != "UNI_PROJECT" ||
		!metadataRequest.NeedCompensateInfo || metadataRequest.PageSize != DefaultPageSize {
		t.Fatalf("plan metadata request changed: %#v", metadataRequest)
	}
	if result.DisplayedCount != 1 || result.TotalRowCount != 2 || result.Summary.PlanCount != 2 ||
		result.Summary.PlansWithCost != 2 || result.Summary.MetadataMissingCount != 1 {
		t.Fatalf("plan report counts changed: %#v", result)
	}
	if result.Summary.TotalCost.String() != "8.5" || result.Summary.TotalPayOrderGMV.String() != "13.5" ||
		result.Summary.TotalPayOrderCount != 3 || result.Summary.TotalPayROI.String() != "1.5882" ||
		result.Summary.TotalSettledAmount1H.String() != "12" ||
		result.Summary.TotalSettledROI1H.String() != "1.4118" {
		t.Fatalf("plan summary changed: %#v", result.Summary)
	}
	if len(result.Presentation.Columns) != 15 || !result.Presentation.Required ||
		result.Presentation.AllowColumnOmission || result.Presentation.AllowColumnReordering ||
		!strings.HasPrefix(result.Presentation.RenderedMarkdown, "| 排名 | 计划 | 达人 | 商品 |") {
		t.Fatalf("mandatory plan presentation changed: %#v", result.Presentation)
	}
	if result.Rows[0]["ad_id"] != "2000000000000001" || result.Rows[0]["stat_cost"].(domain.Decimal).String() != "5" {
		t.Fatalf("plan ranking changed: %#v", result.Rows)
	}
	if !reflect.DeepEqual(result.PageCount, PlanPageCount{PlanMetadata: 1, ReportData: 2}) ||
		!reflect.DeepEqual(result.RequestIDs, []string{
			"schema-request", "metric-request-1", "metric-request-2", "metadata-request-1",
		}) || result.Transport != "official_sdk_rest" || result.AmountUnit != "CNY" {
		t.Fatalf("plan diagnostics changed: %#v", result)
	}
}

func planReportReaderFixture(includeFirstMetadata bool) *qianchuanReportReaderStub {
	metadataRows := []domainqianchuan.Plan{}
	if includeFirstMetadata {
		budget := domain.MustDecimal("5000")
		goal := domain.MustDecimal("1.7")
		metadataRows = append(metadataRows, domainqianchuan.Plan{
			AdID: "2000000000000001", Name: "Fixture plan", Status: "DELIVERY_OK",
			Budget: &budget, BudgetMode: "BUDGET_MODE_DAY", SmartBidType: "SMART_BID_CUSTOM", ROI2Goal: &goal,
			Creators: []domainqianchuan.Creator{{VisibleID: "creator001", Name: "Fixture creator"}},
			Products: []domainqianchuan.PlanProduct{{ProductID: "3000000000000001", ProductName: "Fixture product"}},
			Guarantee: &domainqianchuan.CostGuarantee{
				Status: "SUCCESS", CompensateStatus: "IN_EFFECT", Reason: "fixture",
			},
		})
	}
	return &qianchuanReportReaderStub{
		metricPages: map[int]domainreports.PlanMetricPage{
			1: {
				Rows:      []domainreports.PlanMetricRow{planMetricRow("2000000000000001", "5", "10", "9", "2")},
				PageInfo:  domainqianchuan.PageInfo{Page: 1, TotalPages: 2, TotalNumber: 2},
				RequestID: "metric-request-1",
			},
			2: {
				Rows:      []domainreports.PlanMetricRow{planMetricRow("2000000000000002", "3.5", "3.5", "3", "1")},
				PageInfo:  domainqianchuan.PageInfo{Page: 2, TotalPages: 2, TotalNumber: 2},
				RequestID: "metric-request-2",
			},
		},
		metadataPages: map[int]domainqianchuan.PlanPage{
			1: {
				Rows:      metadataRows,
				PageInfo:  domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: len(metadataRows)},
				RequestID: "metadata-request-1",
			},
		},
	}
}

func planMetricRow(adID, cost, gmv, settled, orders string) domainreports.PlanMetricRow {
	metrics := map[string]domain.Decimal{
		"stat_cost":                                   domain.MustDecimal(cost),
		"total_pay_order_count_for_roi2":              domain.MustDecimal(orders),
		"total_pay_order_gmv_include_coupon_for_roi2": domain.MustDecimal(gmv),
		"total_prepay_and_pay_order_roi2":             domain.MustDecimal("0"),
		"total_order_settle_amount_for_roi2_1h":       domain.MustDecimal(settled),
		"total_order_settle_count_for_roi2_1h":        domain.MustDecimal(orders),
		"total_prepay_and_pay_settle_roi2_1h":         domain.MustDecimal("0"),
	}
	return domainreports.PlanMetricRow{AdID: adID, Metrics: metrics}
}
