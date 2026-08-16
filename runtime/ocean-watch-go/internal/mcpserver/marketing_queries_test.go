package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
)

type marketingAuthorizationStub struct {
	calls  int
	query  authapplication.StatusQuery
	result authapplication.InspectionResult
	err    error
}

func (stub *marketingAuthorizationStub) Inspect(
	_ context.Context,
	query authapplication.StatusQuery,
) (authapplication.InspectionResult, error) {
	stub.calls++
	stub.query = query
	return stub.result, stub.err
}

type marketingMaterialStub struct {
	videoCalls    int
	creatorCalls  int
	videoQuery    applicationmaterials.VideoQuery
	creatorQuery  applicationmaterials.CreatorQuery
	videoResult   applicationmaterials.VideoResult
	creatorResult applicationmaterials.CreatorResult
	err           error
}

func (stub *marketingMaterialStub) QueryVideos(
	_ context.Context,
	query applicationmaterials.VideoQuery,
) (applicationmaterials.VideoResult, error) {
	stub.videoCalls++
	stub.videoQuery = query
	return stub.videoResult, stub.err
}

func (stub *marketingMaterialStub) QueryCreator(
	_ context.Context,
	query applicationmaterials.CreatorQuery,
) (applicationmaterials.CreatorResult, error) {
	stub.creatorCalls++
	stub.creatorQuery = query
	return stub.creatorResult, stub.err
}

type marketingReportStub struct {
	planCalls      int
	materialCalls  int
	planQuery      applicationreports.MarketingPlanQuery
	materialQuery  applicationreports.MarketingMaterialQuery
	planResult     applicationreports.MarketingPlanResult
	materialResult applicationreports.MarketingMaterialResult
	err            error
}

func (stub *marketingReportStub) Plans(
	_ context.Context,
	query applicationreports.MarketingPlanQuery,
) (applicationreports.MarketingPlanResult, error) {
	stub.planCalls++
	stub.planQuery = query
	return stub.planResult, stub.err
}

func (stub *marketingReportStub) Materials(
	_ context.Context,
	query applicationreports.MarketingMaterialQuery,
) (applicationreports.MarketingMaterialResult, error) {
	stub.materialCalls++
	stub.materialQuery = query
	return stub.materialResult, stub.err
}

func TestMarketingAuthorizationIsLocalAndWhitelisted(t *testing.T) {
	const secret = "SECRET_MARKETING_TOKEN"
	stub := &marketingAuthorizationStub{result: authapplication.InspectionResult{
		Status: authapplication.StatusResult{
			Channel: "marketing", HasAppID: true, HasSecret: true, AuthorizationCount: 1,
			AuthorizedAccountCount: 2, AuthorizedAdvertiserCount: 1, Generation: 3,
			AdvertiserID: "1000000000000001", AdvertiserIDAuthorized: boolPointer(true),
			Authorizations: []authapplication.AuthorizationStatus{{
				AuthorizationID: "marketing-auth", TokenRevision: 2,
				HasAccessToken: true, HasRefreshToken: true,
				AccessTokenExpiresAt: "2026-08-16T12:00:00Z", RefreshTokenExpiresAt: "2026-09-16T12:00:00Z",
			}},
		},
		Mappings: authapplication.MappingResult{
			Mappings: []authapplication.AdvertiserMapping{{
				AdvertiserID: "1000000000000001", AuthorizationIDs: []string{"marketing-auth"},
			}},
			Authorizations: []authapplication.AuthorizationMapping{{
				AuthorizationID: "marketing-auth", TokenRevision: 2, HasAccessToken: true,
				HasRefreshToken: true, AdvertiserIDs: []string{"1000000000000001"},
				AuthorizedAccounts: []authapplication.AuthorizedAccount{{AccountName: secret}},
			}},
		},
	}}
	logs := new(bytes.Buffer)
	session := connectTestServer(t, Runtime{
		MarketingAuth: stub, LogWriter: logs, RequestID: func() string { return "request-marketing-auth" },
	})
	defer session.Close()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_marketing_authorization", Arguments: map[string]any{"advertiser_id": "1000000000000001"},
	})
	if err != nil || result.IsError {
		t.Fatalf("authorization result=%#v err=%v", result, err)
	}
	output := decodeStructured[marketingAuthorizationOutput](t, result)
	if stub.calls != 1 || stub.query.Channel != "marketing" || output.Status.Generation != 3 ||
		len(output.Authorizations) != 1 || output.Authorizations[0].AuthorizationID != "marketing-auth" {
		t.Fatalf("authorization contract changed: stub=%#v output=%#v", stub, output)
	}
	encoded, _ := json.Marshal(output)
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(logs.Bytes(), []byte(secret)) {
		t.Fatalf("authorization output leaked account details: output=%s logs=%s", encoded, logs.Bytes())
	}
}

func TestMarketingMaterialSearchesUseApplicationServiceAndDropURLs(t *testing.T) {
	const secretURL = "https://private.example/video?token=SECRET"
	stub := &marketingMaterialStub{
		videoResult: applicationmaterials.VideoResult{
			Endpoint: secretURL, Params: map[string]any{"secret": secretURL}, MatchedCount: 1,
			PageInfo: &domainmarketing.PageInfo{Page: 2, PageSize: 25, TotalNumber: 3, TotalPages: 3},
			SelectedVideos: []domainmarketing.SelectedVideo{{
				VideoID: "video-1", MaterialID: "2000000000000001", Filename: "fixture.mp4",
				Width: int64Pointer(1080), Height: int64Pointer(1920), Duration: float64Pointer(15.5),
				PosterURL: secretURL,
			}},
		},
		creatorResult: applicationmaterials.CreatorResult{
			Endpoint: secretURL, AdvertiserID: "1000000000000001",
			SourceType: applicationmaterials.CreatorAuthorizedSource, CandidateCount: 1,
			PageInfo: &domainmarketing.PageInfo{Page: 2, PageSize: 25, TotalNumber: 60, TotalPages: 3},
			Candidates: []domainmarketing.CreatorCandidate{{
				MaterialID: "2000000000000001", VideoID: "video-2", ItemID: "3000000000000001",
				VideoCoverURL: secretURL, Title: "fixture title", CreatorID: "4000000000000001",
				AuthorizationStatus: "VALID", Usable: true, WarningTypes: []string{}, UnusableReasons: []string{},
			}},
		},
	}
	session := connectTestServer(t, Runtime{MarketingMaterials: stub, RequestID: func() string { return "request-marketing-materials" }})
	defer session.Close()

	videos, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_marketing_videos", Arguments: map[string]any{
			"advertiser_id": "1000000000000001", "page": 2, "limit": 25, "filename": "fixture",
		},
	})
	if err != nil || videos.IsError {
		t.Fatalf("video result=%#v err=%v", videos, err)
	}
	videoOutput := decodeStructured[marketingVideosOutput](t, videos)
	if stub.videoCalls != 1 || stub.videoQuery.Mode != "library-get" || stub.videoQuery.FetchAll ||
		stub.videoQuery.Page != 2 || stub.videoQuery.PageSize != 25 || videoOutput.TotalCount != 3 || !videoOutput.HasMore {
		t.Fatalf("video service boundary changed: stub=%#v output=%#v", stub, videoOutput)
	}

	creators, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_marketing_creator_materials", Arguments: map[string]any{
			"advertiser_id": "1000000000000001", "source": "homepage",
			"aweme_ids": []any{"4000000000000001"}, "include_unusable": true, "page": 2, "limit": 25,
		},
	})
	if err != nil || creators.IsError {
		t.Fatalf("creator result=%#v err=%v", creators, err)
	}
	creatorOutput := decodeStructured[marketingCreatorOutput](t, creators)
	if stub.creatorCalls != 1 || stub.creatorQuery.Source != "homepage" || !stub.creatorQuery.IncludeUnusable ||
		!stub.creatorQuery.SinglePage || stub.creatorQuery.Page != 2 || stub.creatorQuery.PageSize != 25 ||
		creatorOutput.Page != 2 || creatorOutput.SourceTotal != 60 || !creatorOutput.HasMore ||
		creatorOutput.DisplayedCount != 1 || creatorOutput.Items[0].CreatorID != "4000000000000001" {
		t.Fatalf("creator service boundary changed: stub=%#v output=%#v", stub, creatorOutput)
	}
	videoJSON, _ := json.Marshal(videoOutput)
	creatorJSON, _ := json.Marshal(creatorOutput)
	if bytes.Contains(videoJSON, []byte("private.example")) || bytes.Contains(creatorJSON, []byte("private.example")) {
		t.Fatalf("material output leaked URLs: videos=%s creators=%s", videoJSON, creatorJSON)
	}
}

func TestMarketingReportsPreservePresentationAndWhitelistRows(t *testing.T) {
	const secret = "SECRET_ENDPOINT_OR_RAW_RESPONSE"
	markdown := "| 项目ID | 项目名称 | 消耗 |\n| --- | --- | --- |\n| 2000000000000001 | 原样\\|保留 | ¥12.34 |"
	stub := &marketingReportStub{
		planResult: applicationreports.MarketingPlanResult{
			ConfigEndpoint: secret, ReportEndpoint: secret, AdvertiserID: "1000000000000001",
			DateRange: applicationreports.DateRange{StartDate: "2026-08-15", EndDate: "2026-08-16"},
			Summary: applicationreports.MarketingPlanSummary{
				TotalSpend: domain.MustDecimal("12.34"), PlansWithSpend: 1,
			},
			RowCount: 1, DisplayedCount: 1,
			Presentation: domain.Presentation{Required: true, RenderedMarkdown: markdown},
			Rows: []map[string]any{{
				"project_id": "2000000000000001", "project_name": "原样|保留", "stat_cost": "12.34",
				"raw_response": secret,
			}},
		},
		materialResult: applicationreports.MarketingMaterialResult{
			PromotionEndpoint: secret, MaterialReportEndpoint: secret,
			DateRange: applicationreports.DateRange{StartDate: "2026-08-15", EndDate: "2026-08-16"},
			Summary: applicationreports.MarketingMaterialSummary{
				PromotionCount: 1, SelectedPromotionCount: 1, ActivePromotionCount: 1,
				MaterialCount: 1, RowsWithReportData: 1,
			},
			RowCount: 1, Rows: []map[string]any{{
				"project_id": "2000000000000001", "promotion_id": "3000000000000001",
				"promotion_name": "fixture", "promotion_status": "ENABLE", "promotion_opt_status": "ENABLE",
				"material_id": "4000000000000001", "video_id": "video-1", "video_cover_id": "cover-1",
				"material_status": "OK", "material_opt_status": "ENABLE", "image_mode": "CREATIVE_IMAGE_MODE_VIDEO",
				"material_create_time": "2026-08-15 12:00:00", "has_report_data": true, "stat_cost": "12.34",
				"raw_response": secret,
			}},
		},
	}
	session := connectTestServer(t, Runtime{MarketingReports: stub, RequestID: func() string { return "request-marketing-reports" }})
	defer session.Close()

	plans, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "report_marketing_plans", Arguments: map[string]any{
			"advertiser_id": "1000000000000001", "start_date": "2026-08-15", "end_date": "2026-08-16", "limit": 10,
		},
	})
	if err != nil || plans.IsError {
		t.Fatalf("plan report=%#v err=%v", plans, err)
	}
	planOutput := decodeStructured[marketingPlanReportOutput](t, plans)
	if stub.planCalls != 1 || stub.planQuery.Top != 10 || planOutput.Presentation.RenderedMarkdown != markdown ||
		planOutput.Items[0].ProjectID != "2000000000000001" {
		t.Fatalf("plan report contract changed: stub=%#v output=%#v", stub, planOutput)
	}

	materials, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "report_marketing_materials", Arguments: map[string]any{
			"advertiser_id": "1000000000000001", "project_id": "2000000000000001", "active_only": true,
		},
	})
	if err != nil || materials.IsError {
		t.Fatalf("material report=%#v err=%v", materials, err)
	}
	materialOutput := decodeStructured[marketingMaterialReportOutput](t, materials)
	if stub.materialCalls != 1 || stub.materialQuery.DataTopic != applicationreports.MarketingMaterialTopic ||
		!stub.materialQuery.ActiveOnly || materialOutput.Items[0].MaterialID != "4000000000000001" {
		t.Fatalf("material report contract changed: stub=%#v output=%#v", stub, materialOutput)
	}
	planJSON, _ := json.Marshal(planOutput)
	materialJSON, _ := json.Marshal(materialOutput)
	if bytes.Contains(planJSON, []byte(secret)) || bytes.Contains(materialJSON, []byte(secret)) {
		t.Fatalf("report output leaked diagnostics: plans=%s materials=%s", planJSON, materialJSON)
	}
}

func TestMarketingToolsRejectInvalidArgumentsBeforeServiceCallsAndSanitizeErrors(t *testing.T) {
	materials := &marketingMaterialStub{}
	reports := &marketingReportStub{}
	session := connectTestServer(t, Runtime{
		MarketingMaterials: materials, MarketingReports: reports,
		RequestID: func() string { return "request-marketing-invalid" },
	})
	defer session.Close()

	invalid, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_marketing_videos", Arguments: map[string]any{
			"advertiser_id": "1000000000000001", "video_ids": []any{"v"},
			"material_ids": []any{"2000000000000001"},
		},
	})
	if err != nil || !invalid.IsError || decodeStructured[errorEnvelope](t, invalid).Error.Code != "INVALID_ARGUMENT" || materials.videoCalls != 0 {
		t.Fatalf("invalid video request crossed service boundary: result=%#v err=%v stub=%#v", invalid, err, materials)
	}

	materials.err = errors.New("request https://private.example failed with token SECRET")
	failure, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_marketing_videos", Arguments: map[string]any{"advertiser_id": "1000000000000001"},
	})
	if err != nil || !failure.IsError {
		t.Fatalf("expected sanitized failure: result=%#v err=%v", failure, err)
	}
	encoded, _ := json.Marshal(decodeStructured[errorEnvelope](t, failure))
	if bytes.Contains(encoded, []byte("private.example")) || bytes.Contains(encoded, []byte("SECRET")) ||
		!strings.Contains(string(encoded), "UPSTREAM_QUERY_FAILED") {
		t.Fatalf("raw upstream failure leaked: %s", encoded)
	}
}

func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }
