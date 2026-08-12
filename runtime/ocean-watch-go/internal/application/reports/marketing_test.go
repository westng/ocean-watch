package reports

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type marketingReportTokenStub struct {
	queries []authapplication.TokenQuery
}

func (stub *marketingReportTokenStub) Ensure(
	_ context.Context,
	query authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	stub.queries = append(stub.queries, query)
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: "fixture-authorization",
		AccessToken: "TEST_MARKETING_REPORT_TOKEN_DO_NOT_USE",
	}, nil
}

type marketingReportReaderStub struct {
	schemaRequests    []portreports.MarketingSchemaRequest
	reportRequests    []portreports.MarketingReportPageRequest
	promotionRequests []portreports.MarketingPromotionPageRequest
	schema            domainreports.MarketingSchema
	reportPages       map[int]domainreports.MarketingReportPage
	promotionPages    map[int]domainreports.MarketingPromotionPage
}

func (stub *marketingReportReaderStub) FetchSchema(
	_ context.Context,
	request portreports.MarketingSchemaRequest,
) (domainreports.MarketingSchema, error) {
	stub.schemaRequests = append(stub.schemaRequests, request)
	return stub.schema, nil
}

func (stub *marketingReportReaderStub) FetchReportPage(
	_ context.Context,
	request portreports.MarketingReportPageRequest,
) (domainreports.MarketingReportPage, error) {
	stub.reportRequests = append(stub.reportRequests, request)
	return stub.reportPages[request.Page], nil
}

func (stub *marketingReportReaderStub) FetchPromotionPage(
	_ context.Context,
	request portreports.MarketingPromotionPageRequest,
) (domainreports.MarketingPromotionPage, error) {
	stub.promotionRequests = append(stub.promotionRequests, request)
	return stub.promotionPages[request.Page], nil
}

func TestMarketingReportSchemaAndCustomContracts(t *testing.T) {
	reader := &marketingReportReaderStub{
		schema: domainreports.MarketingSchema{
			Topics: []domainreports.MarketingTopic{{
				DataTopic:  MarketingMaterialTopic,
				Dimensions: []domainreports.MarketingField{{Field: "material_id", Name: "素材 ID"}},
				Metrics:    []domainreports.MarketingField{{Field: "stat_cost", Name: "消耗"}},
			}},
			RequestID: "schema-request", Message: "OK",
		},
		reportPages: map[int]domainreports.MarketingReportPage{
			1: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"material_id": "9007199254740993"},
					Metrics:    map[string]string{"stat_cost": "1.005"},
				}},
				TotalMetrics: map[string]string{"stat_cost": "1.005"},
				Page:         1, PageSize: 100, TotalPages: 1, TotalNumber: 1,
				RequestID: "report-request", Message: "OK",
			},
		},
	}
	tokens := &marketingReportTokenStub{}
	service := MarketingService{
		Tokens: tokens, Reader: reader,
		Now: func() time.Time { return time.Date(2026, 7, 26, 2, 0, 0, 0, time.UTC) },
	}
	scope := CredentialScope{AdvertiserID: "1000000000000001", AuthAccountID: "9000000000000001"}
	schema, err := service.Schema(context.Background(), MarketingSchemaQuery{
		CredentialScope: scope, DataTopics: []string{MarketingMaterialTopic},
	})
	if err != nil {
		t.Fatal(err)
	}
	topics, ok := schema.Topics.([]MarketingSchemaTopic)
	if !ok || len(topics) != 1 || topics[0].DimensionCount != 1 || topics[0].MetricCount != 1 ||
		schema.Endpoint != MarketingSchemaEndpoint || schema.RequestID != "schema-request" {
		t.Fatalf("schema result changed: %#v", schema)
	}
	custom, err := service.Custom(context.Background(), MarketingCustomQuery{
		CredentialScope: scope, Dimensions: []string{"material_id"}, Metrics: []string{"stat_cost"},
		Filters:   []MarketingFilter{{Field: "material_id", Type: 2, Operator: 1, Values: []string{"9007199254740993"}}},
		StartTime: "2026-07-15", EndTime: "2026-07-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.RowCount != 1 || custom.FlatRows[0]["material_id"] != "9007199254740993" ||
		custom.FlatRows[0]["stat_cost"] != "1.005" || custom.PageInfo.TotalNumber != 1 {
		t.Fatalf("custom report lost exact values: %#v", custom)
	}
	if len(tokens.queries) != 2 {
		t.Fatalf("token query count changed: %#v", tokens.queries)
	}
	for _, query := range tokens.queries {
		if query.Channel != "marketing" || query.AdvertiserID != scope.AdvertiserID ||
			query.AuthAccountID != scope.AuthAccountID {
			t.Fatalf("Marketing report crossed credential scope: %#v", query)
		}
	}
	request := reader.reportRequests[0]
	if request.StartTime != "2026-07-15 00:00:00" || request.EndTime != "2026-07-15 23:59:59" ||
		request.OrderField != "stat_cost" || request.OrderType != "DESC" || request.Page != 1 || request.PageSize != 100 {
		t.Fatalf("custom report request changed: %#v", request)
	}
}

func TestMarketingPlanReportNegotiatesFieldsAndAggregatesAllPages(t *testing.T) {
	reader := &marketingReportReaderStub{
		schema: domainreports.MarketingSchema{
			Topics: []domainreports.MarketingTopic{{
				DataTopic: MarketingPlanTopic,
				Dimensions: []domainreports.MarketingField{
					{Field: "project_id"}, {Field: "project_name"},
				},
				Metrics: []domainreports.MarketingField{
					{Field: "stat_cost"}, {Field: "show_cnt"}, {Field: "in_app_order_gmv"},
				},
			}},
			RequestID: "plan-schema",
		},
		reportPages: map[int]domainreports.MarketingReportPage{
			1: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"project_id": "9007199254740993", "project_name": "Plan one"},
					Metrics:    map[string]string{"stat_cost": "1.005", "show_cnt": "10", "in_app_order_gmv": "2.5"},
				}},
				Page: 1, PageSize: 100, TotalPages: 2, TotalNumber: 2, RequestID: "plan-page-1",
			},
			2: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"project_id": "9007199254740995", "project_name": "Plan two"},
					Metrics:    map[string]string{"stat_cost": "2.005", "show_cnt": "20", "in_app_order_gmv": "4"},
				}},
				Page: 2, PageSize: 100, TotalPages: 2, TotalNumber: 2, RequestID: "plan-page-2",
			},
		},
	}
	result, err := (MarketingService{
		Tokens: &marketingReportTokenStub{}, Reader: reader,
	}).Plans(context.Background(), MarketingPlanQuery{
		CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"},
		StartDate:       "2026-07-15", EndDate: "2026-07-16", Top: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 2 || result.DisplayedCount != 1 || result.PageCount != 2 ||
		result.Summary.TotalSpend.String() != "3.01" || result.Summary.PlansWithSpend != 2 {
		t.Fatalf("plan full-page summary changed: %#v", result)
	}
	if result.Summary.TotalGMV == nil || result.Summary.TotalGMV.String() != "6.5" ||
		result.Summary.WeightedROI == nil || result.Summary.WeightedROI.String() != "2.1595" ||
		result.Summary.TotalOrders != nil {
		t.Fatalf("plan null or Decimal summary changed: %#v", result.Summary)
	}
	if len(result.Rows) != 1 || result.Rows[0]["project_id"] != "9007199254740995" ||
		result.Rows[0]["stat_cost"] != "2.005" {
		t.Fatalf("plan top ranking lost exact values: %#v", result.Rows)
	}
	if len(result.Presentation.Columns) != 13 || !result.Presentation.Required ||
		result.Presentation.AllowColumnOmission || result.Presentation.AllowColumnReordering ||
		!strings.HasPrefix(result.Presentation.RenderedMarkdown, "| 排名 | 项目ID | 项目名称 | 消耗 |") ||
		result.Presentation.Rows[0]["in_app_order_count"] != nil {
		t.Fatalf("plan presentation contract changed: %#v", result.Presentation)
	}
	if !reflect.DeepEqual(result.Contract.OmittedDefaultMetrics, []string{
		"click_cnt", "ctr", "convert_cnt", "conversion_cost", "conversion_rate",
		"in_app_order_count", "in_app_order_roi",
	}) {
		t.Fatalf("plan field negotiation changed: %#v", result.Contract)
	}
	if len(reader.reportRequests) != 2 {
		t.Fatalf("plan pages changed: %#v", reader.reportRequests)
	}
	for index, request := range reader.reportRequests {
		if request.Page != index+1 || request.StartTime != "2026-07-15 00:00:00" ||
			request.EndTime != "2026-07-16 23:59:59" || request.DataTopic != MarketingPlanTopic ||
			!reflect.DeepEqual(request.Dimensions, []string{"project_id", "project_name"}) ||
			!reflect.DeepEqual(request.Metrics, []string{"stat_cost", "show_cnt", "in_app_order_gmv"}) {
			t.Fatalf("plan page request changed: %#v", request)
		}
	}
}

func TestMarketingMaterialReportPaginatesFiltersAndJoins(t *testing.T) {
	reader := &marketingReportReaderStub{
		promotionPages: map[int]domainreports.MarketingPromotionPage{
			1: {
				Rows: []domainreports.MarketingPromotion{{
					ProjectID: "2000000000000001", PromotionID: "9007199254740993",
					PromotionName: "Active promotion", PromotionStatus: "ENABLE",
					PromotionStatusFirst: "RUNNING", PromotionOptStatus: "ENABLE",
					Materials: []domainreports.MarketingVideoMaterial{{
						MaterialID: "9007199254740995", VideoID: "video-one", MaterialStatus: "MATERIAL_STATUS_OK",
					}},
				}},
				Page: 1, PageSize: 20, TotalPages: 2, TotalNumber: 2,
				RequestID: "promotion-page-1", Message: "OK",
			},
			2: {
				Rows: []domainreports.MarketingPromotion{{
					ProjectID: "2000000000000002", PromotionID: "9007199254740997",
					PromotionName: "Paused promotion", PromotionStatus: "DISABLE",
					PromotionStatusFirst: "STOPPED", PromotionOptStatus: "DISABLE",
					Materials: []domainreports.MarketingVideoMaterial{{
						MaterialID: "9007199254740999", VideoID: "video-two", MaterialStatus: "MATERIAL_STATUS_OK",
					}},
				}},
				Page: 2, PageSize: 20, TotalPages: 2, TotalNumber: 2,
				RequestID: "promotion-page-2", Message: "OK",
			},
		},
		reportPages: map[int]domainreports.MarketingReportPage{
			1: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"material_id": "9007199254740995", "cdp_promotion_id": "9007199254740993", "cdp_promotion_name": "Active promotion"},
					Metrics:    map[string]string{"stat_cost": "1.005"},
				}},
				TotalMetrics: map[string]string{"stat_cost": "3.010"},
				Page:         1, PageSize: 100, TotalPages: 2, TotalNumber: 2,
				RequestID: "material-page-1", Message: "OK",
			},
			2: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"material_id": "9007199254740999", "cdp_promotion_id": "9007199254740997", "cdp_promotion_name": "Paused promotion"},
					Metrics:    map[string]string{"stat_cost": "2.005"},
				}},
				TotalMetrics: map[string]string{"stat_cost": "3.010"},
				Page:         2, PageSize: 100, TotalPages: 2, TotalNumber: 2,
				RequestID: "material-page-2", Message: "OK",
			},
		},
	}
	tokens := &marketingReportTokenStub{}
	result, err := (MarketingService{Tokens: tokens, Reader: reader}).Materials(
		context.Background(),
		MarketingMaterialQuery{
			CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"},
			StartDate:       "2026-07-15", EndDate: "2026-07-15", Metrics: []string{"stat_cost"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens.queries) != 1 || result.PromotionCount != 2 || result.SelectedPromotionCount != 2 ||
		result.ActiveLikePromotionCount != 1 || result.MaterialCount != 2 || result.RowCount != 2 {
		t.Fatalf("material counts or token scope changed: result=%#v tokens=%#v", result, tokens.queries)
	}
	if !reflect.DeepEqual(result.PromotionRequestIDs, []string{"promotion-page-1", "promotion-page-2"}) ||
		!reflect.DeepEqual(result.MaterialReportRequestIDs, []string{"material-page-1", "material-page-2"}) ||
		result.Rows[0]["material_id"] != "9007199254740995" || result.Rows[0]["stat_cost"] != "1.005" ||
		result.Rows[0]["has_report_data"] != true || result.Rows[1]["stat_cost"] != "2.005" {
		t.Fatalf("material join changed: %#v", result)
	}
	if result.ReportTotalMetrics.(map[string]string)["stat_cost"] != "3.010" ||
		result.ReportScope != "promotion_and_extracted_material_ids" {
		t.Fatalf("material report diagnostics changed: %#v", result)
	}
	if len(reader.promotionRequests) != 2 || len(reader.reportRequests) != 2 {
		t.Fatalf("material pagination changed: promotions=%#v reports=%#v", reader.promotionRequests, reader.reportRequests)
	}
	filters := reader.reportRequests[0].Filters
	if len(filters) != 2 || filters[0].Field != "material_id" || filters[1].Field != "cdp_promotion_id" ||
		!reflect.DeepEqual(filters[0].Values, []string{"9007199254740995", "9007199254740999"}) ||
		!reflect.DeepEqual(filters[1].Values, []string{"9007199254740993", "9007199254740997"}) {
		t.Fatalf("material exact filters changed: %#v", filters)
	}
}

func TestMarketingReportsFailClosedBeforeCredentialsAndOnPagination(t *testing.T) {
	tokens := &marketingReportTokenStub{}
	service := MarketingService{Tokens: tokens, Reader: &marketingReportReaderStub{}}
	if _, err := service.Plans(context.Background(), MarketingPlanQuery{
		CredentialScope: CredentialScope{AdvertiserID: "invalid"},
	}); err == nil {
		t.Fatal("invalid advertiser was accepted")
	}
	if len(tokens.queries) != 0 {
		t.Fatalf("invalid request resolved credentials: %#v", tokens.queries)
	}

	reader := &marketingReportReaderStub{
		schema: domainreports.MarketingSchema{Topics: []domainreports.MarketingTopic{{
			DataTopic:  MarketingPlanTopic,
			Dimensions: []domainreports.MarketingField{{Field: "project_id"}},
			Metrics:    []domainreports.MarketingField{{Field: "stat_cost"}},
		}}},
		reportPages: map[int]domainreports.MarketingReportPage{
			1: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"project_id": "2000000000000001"},
					Metrics:    map[string]string{"stat_cost": "1"},
				}},
				Page: 1, PageSize: 100, TotalPages: 2, TotalNumber: 2,
			},
			2: {
				Rows: []domainreports.MarketingReportRow{{
					Dimensions: map[string]string{"project_id": "2000000000000001"},
					Metrics:    map[string]string{"stat_cost": "2"},
				}},
				Page: 2, PageSize: 100, TotalPages: 2, TotalNumber: 2,
			},
		},
	}
	_, err := (MarketingService{Tokens: &marketingReportTokenStub{}, Reader: reader}).Plans(
		context.Background(),
		MarketingPlanQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}},
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate unique key") {
		t.Fatalf("duplicate project IDs did not fail closed: %v", err)
	}
}

func TestMarketingMaterialActiveOnlyAndSinglePageCompatibility(t *testing.T) {
	reader := &marketingReportReaderStub{promotionPages: map[int]domainreports.MarketingPromotionPage{
		2: {
			Rows: []domainreports.MarketingPromotion{{
				PromotionID: "2000000000000002", PromotionName: "Inactive",
				PromotionStatus: "DISABLE", PromotionOptStatus: "DISABLE",
				Materials: []domainreports.MarketingVideoMaterial{{MaterialID: "5000000000000002"}},
			}},
			Page: 2, PageSize: 20, TotalPages: 3, TotalNumber: 3, RequestID: "promotion-page-2",
		},
	}}
	result, err := (MarketingService{Tokens: &marketingReportTokenStub{}, Reader: reader}).Materials(
		context.Background(),
		MarketingMaterialQuery{
			CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"},
			PromotionPage:   2, SinglePage: true, ActiveOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.PromotionCount != 1 || result.SelectedPromotionCount != 0 || result.MaterialCount != 0 ||
		result.StatusHandling != "active_only" || len(result.ExcludedPromotions) != 1 ||
		len(reader.promotionRequests) != 1 || reader.promotionRequests[0].Page != 2 ||
		len(reader.reportRequests) != 0 || result.MaterialReportParams != nil {
		t.Fatalf("active-only or single-page compatibility changed: %#v", result)
	}
}

func TestMarketingPlanPresentationDecimalType(t *testing.T) {
	value := domain.MustDecimal("1.25")
	row := parsedMarketingPlan{
		ID: "2000000000000001", Spend: value,
		Metrics: map[string]*domain.Decimal{"stat_cost": &value},
	}
	if row.presentation(1)["stat_cost"].(domain.Decimal).String() != "1.25" {
		t.Fatal("presentation Decimal type changed")
	}
}
