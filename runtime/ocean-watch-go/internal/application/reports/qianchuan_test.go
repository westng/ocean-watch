package reports

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
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

type qianchuanUnifiedReaderStub struct {
	schemaRequests []portreports.SchemaRequest
	dataRequests   []portreports.DataPageRequest
	allRequests    []portreports.AggregateRequest
	uniRequests    []portreports.AggregateRequest
	roomRequests   []portreports.DimensionPageRequest
	authorRequests []portreports.DimensionPageRequest
	dataPages      map[int]domainreports.QianchuanReportPage
	dimensionPages map[int]domainreports.QianchuanDimensionPage
}

func (stub *qianchuanUnifiedReaderStub) FetchSchemas(
	_ context.Context,
	request portreports.SchemaRequest,
) ([]domainreports.QianchuanSchema, error) {
	stub.schemaRequests = append(stub.schemaRequests, request)
	schemas := make([]domainreports.QianchuanSchema, 0, len(request.Topics))
	for _, topic := range request.Topics {
		schemas = append(schemas, domainreports.QianchuanSchema{
			Topic: topic, Dimensions: []string{"product_id"}, Metrics: []string{"stat_cost"},
			RequestID: "schema-batch",
		})
	}
	return schemas, nil
}

func (stub *qianchuanUnifiedReaderStub) FetchDataPage(
	_ context.Context,
	request portreports.DataPageRequest,
) (domainreports.QianchuanReportPage, error) {
	stub.dataRequests = append(stub.dataRequests, request)
	return stub.dataPages[request.Page], nil
}

func (stub *qianchuanUnifiedReaderStub) FetchAllPromotion(
	_ context.Context,
	request portreports.AggregateRequest,
) (domainreports.QianchuanAggregate, error) {
	stub.allRequests = append(stub.allRequests, request)
	return domainreports.QianchuanAggregate{Values: map[string]any{"stat_cost_for_roi2": 12.5}, RequestID: "all"}, nil
}

func (stub *qianchuanUnifiedReaderStub) FetchUniPromotion(
	_ context.Context,
	request portreports.AggregateRequest,
) (domainreports.QianchuanAggregate, error) {
	stub.uniRequests = append(stub.uniRequests, request)
	return domainreports.QianchuanAggregate{Values: map[string]any{"stat_cost": 8.5}, RequestID: "uni"}, nil
}

func (stub *qianchuanUnifiedReaderStub) FetchRoomDimensionPage(
	_ context.Context,
	request portreports.DimensionPageRequest,
) (domainreports.QianchuanDimensionPage, error) {
	stub.roomRequests = append(stub.roomRequests, request)
	return stub.dimensionPages[request.Page], nil
}

func (stub *qianchuanUnifiedReaderStub) FetchAuthorDimensionPage(
	_ context.Context,
	request portreports.DimensionPageRequest,
) (domainreports.QianchuanDimensionPage, error) {
	stub.authorRequests = append(stub.authorRequests, request)
	return stub.dimensionPages[request.Page], nil
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

func TestQianchuanUnifiedReportRoutingAndPagination(t *testing.T) {
	reader := &qianchuanUnifiedReaderStub{
		dataPages: map[int]domainreports.QianchuanReportPage{
			1: {
				Rows: []domainreports.QianchuanReportRow{{
					Dimensions: map[string]any{"product_id": "3000000000000001"},
					Metrics:    map[string]any{"stat_cost": 5.5},
				}},
				PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 2, TotalNumber: 2}, RequestID: "data-1",
			},
			2: {
				Rows: []domainreports.QianchuanReportRow{{
					Dimensions: map[string]any{"product_id": "3000000000000002"},
					Metrics:    map[string]any{"stat_cost": 3},
				}},
				PageInfo: domainqianchuan.PageInfo{Page: 2, TotalPages: 2, TotalNumber: 2}, RequestID: "data-2",
			},
		},
		dimensionPages: map[int]domainreports.QianchuanDimensionPage{
			1: {
				Rows:     []domainreports.QianchuanDimensionRow{{Values: map[string]any{"stat_cost": 1.5}}},
				PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1}, RequestID: "dimension-1",
			},
		},
	}
	service := Service{Tokens: &qianchuanReportTokenStub{}, UnifiedReader: reader}
	scope := CredentialScope{AdvertiserID: "1000000000000001", AuthAccountID: "9000000000000001"}

	all, err := service.QianchuanAllPromotion(context.Background(), QianchuanAggregateQuery{
		CredentialScope: scope, StartDate: "2026-08-01", EndDate: "2026-08-02",
		Fields: []string{"stat_cost_for_roi2"}, AdlabScene: "OVERALL_PROJECT", DataPeriod: "ALL_DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	uni, err := service.QianchuanUniPromotion(context.Background(), QianchuanAggregateQuery{
		CredentialScope: scope, StartDate: "2026-08-01", EndDate: "2026-08-02", Fields: []string{"stat_cost"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.allRequests) != 1 || len(reader.uniRequests) != 1 ||
		reader.allRequests[0].StartTime != "2026-08-01 00:00:00" ||
		reader.allRequests[0].AdlabScene != "OVERALL_PROJECT" || reader.allRequests[0].DataPeriod != "ALL_DATA" ||
		reader.uniRequests[0].StartTime != "2026-08-01" || all.Endpoint != QianchuanAllPromotionEndpoint ||
		uni.Endpoint != QianchuanUniPromotionEndpoint {
		t.Fatalf("aggregate report routing changed: all=%#v uni=%#v", reader.allRequests, reader.uniRequests)
	}

	schema, err := service.QianchuanSchema(context.Background(), QianchuanSchemaQuery{
		CredentialScope: scope, Topics: []string{QianchuanProductTopic, QianchuanOverallProductTopic},
		DataPeriod: "ALL_DATA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Schemas) != 2 || len(reader.schemaRequests) != 1 ||
		!reflect.DeepEqual(reader.schemaRequests[0].Topics, []string{QianchuanProductTopic, QianchuanOverallProductTopic}) ||
		reader.schemaRequests[0].DataPeriod != "ALL_DATA" ||
		!reflect.DeepEqual(schema.RequestIDs, []string{"schema-batch"}) {
		t.Fatalf("schema topic routing changed: %#v", reader.schemaRequests)
	}

	custom, err := service.QianchuanCustom(context.Background(), QianchuanCustomQuery{
		CredentialScope: scope, StartDate: "2026-08-04", EndDate: "2026-08-04",
		DataTopic: QianchuanOverallProductTopic, Dimensions: []string{"product_id"}, Metrics: []string{"stat_cost"},
		Filters:    []QianchuanFilter{{Field: "product_id", Operator: 7, Values: []string{"3000000000000001"}}},
		DataPeriod: "ALL_DATA", OrderField: "stat_cost", OrderType: "DESC", PageSize: 100, MaxPages: 100, Top: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.dataRequests) != 2 || custom.TotalRowCount != 2 || custom.DisplayedCount != 1 || !custom.Truncated ||
		reader.dataRequests[0].Topic != QianchuanOverallProductTopic || reader.dataRequests[0].DataPeriod != "ALL_DATA" ||
		!reflect.DeepEqual(reader.dataRequests[0].Filters, []portreports.ReportFilter{{
			Field: "product_id", Operator: 7, Values: []string{"3000000000000001"},
		}}) {
		t.Fatalf("custom report traversal changed: result=%#v requests=%#v", custom, reader.dataRequests)
	}

	room, err := service.QianchuanRoom(context.Background(), QianchuanDimensionQuery{
		CredentialScope: scope, DimensionID: "4000000000000001", StartDate: "2026-08-04", EndDate: "2026-08-04",
		Dimension: "TIME_GRANULARITY_HOURLY", Metrics: []string{"stat_cost_for_roi2"},
		OrderPlatform: "ALL", SmartBidType: "SMART_BID_CUSTOM",
		OrderField: "stat_cost_for_roi2", OrderType: "DESC", PageSize: 100, MaxPages: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	author, err := service.QianchuanAuthor(context.Background(), QianchuanDimensionQuery{
		CredentialScope: scope, DimensionID: "5000000000000001", StartDate: "2026-08-04", EndDate: "2026-08-04",
		Dimension: "TIME_GRANULARITY_DAILY", Metrics: []string{"stat_cost"}, MarketingGoal: "VIDEO_PROM_GOODS",
		OrderField: "stat_cost", OrderType: "DESC", PageSize: 100, MaxPages: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.roomRequests) != 1 || len(reader.authorRequests) != 1 ||
		reader.roomRequests[0].DimensionID != "4000000000000001" ||
		reader.roomRequests[0].OrderPlatform != "ALL" || reader.roomRequests[0].SmartBidType != "SMART_BID_CUSTOM" ||
		reader.authorRequests[0].DimensionID != "5000000000000001" ||
		reader.authorRequests[0].MarketingGoal != "VIDEO_PROM_GOODS" ||
		room.Endpoint != QianchuanRoomEndpoint || author.Endpoint != QianchuanAuthorEndpoint {
		t.Fatalf("dimension report routing changed: room=%#v author=%#v", reader.roomRequests, reader.authorRequests)
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
