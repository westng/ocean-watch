package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
)

type marketingAuthorizationInput struct {
	AdvertiserID string `json:"advertiser_id"`
}

type marketingAuthorizationOutput struct {
	OK             bool                         `json:"ok"`
	RequestID      string                       `json:"request_id"`
	Source         string                       `json:"source"`
	Status         qianchuanAuthorizationStatus `json:"status"`
	Mappings       []qianchuanAdvertiserMapping `json:"mappings"`
	Authorizations []qianchuanAuthorizationItem `json:"authorizations"`
}

type marketingVideosInput struct {
	AdvertiserID  string   `json:"advertiser_id"`
	AuthAccountID string   `json:"auth_account_id"`
	VideoIDs      []string `json:"video_ids"`
	MaterialIDs   []string `json:"material_ids"`
	Signatures    []string `json:"signatures"`
	Filename      string   `json:"filename"`
	StartDate     string   `json:"start_date"`
	EndDate       string   `json:"end_date"`
	Page          int      `json:"page"`
	Limit         int      `json:"limit"`
}

type marketingVideosOutput struct {
	OK             bool                 `json:"ok"`
	RequestID      string               `json:"request_id"`
	Source         string               `json:"source"`
	AdvertiserID   string               `json:"advertiser_id"`
	Page           int                  `json:"page"`
	TotalCount     int                  `json:"total_count"`
	DisplayedCount int                  `json:"displayed_count"`
	HasMore        bool                 `json:"has_more"`
	Items          []marketingVideoItem `json:"items"`
}

type marketingVideoItem struct {
	VideoID    string   `json:"video_id"`
	MaterialID string   `json:"material_id"`
	Filename   string   `json:"filename"`
	CreateTime string   `json:"create_time"`
	Width      *int64   `json:"width"`
	Height     *int64   `json:"height"`
	Duration   *float64 `json:"duration"`
	Format     string   `json:"format"`
	Source     string   `json:"source"`
	Signature  string   `json:"signature"`
}

type marketingCreatorInput struct {
	AdvertiserID         string   `json:"advertiser_id"`
	AuthAccountID        string   `json:"auth_account_id"`
	Source               string   `json:"source"`
	AwemeIDs             []string `json:"aweme_ids"`
	ItemIDs              []string `json:"item_ids"`
	MinimumRemainingDays int      `json:"minimum_remaining_days"`
	IncludeUnusable      bool     `json:"include_unusable"`
	Page                 int      `json:"page"`
	Limit                int      `json:"limit"`
}

type marketingCreatorOutput struct {
	OK             bool                   `json:"ok"`
	RequestID      string                 `json:"request_id"`
	Source         string                 `json:"source"`
	AdvertiserID   string                 `json:"advertiser_id"`
	MaterialSource string                 `json:"material_source"`
	Page           int                    `json:"page"`
	SourceTotal    int                    `json:"source_total_count"`
	DisplayedCount int                    `json:"displayed_count"`
	HasMore        bool                   `json:"has_more"`
	Items          []marketingCreatorItem `json:"items"`
}

type marketingCreatorItem struct {
	MaterialID             string   `json:"material_id"`
	VideoID                string   `json:"video_id"`
	ItemID                 string   `json:"item_id"`
	ImageMode              string   `json:"image_mode"`
	VideoCoverID           string   `json:"video_cover_id"`
	Title                  string   `json:"title"`
	Duration               *float64 `json:"duration"`
	CreatorID              string   `json:"creator_id"`
	CreatorName            string   `json:"creator_name"`
	AuthorizationSubjectID string   `json:"authorization_subject_id"`
	AuthorizationType      string   `json:"authorization_type"`
	AuthorizationStatus    string   `json:"authorization_status"`
	AuthorizationStartAt   string   `json:"authorization_start_at"`
	AuthorizationExpiresAt string   `json:"authorization_expires_at"`
	WarningTypes           []string `json:"warning_types"`
	Usable                 bool     `json:"usable"`
	UnusableReasons        []string `json:"unusable_reasons"`
}

type marketingPlanReportInput struct {
	AdvertiserID  string `json:"advertiser_id"`
	AuthAccountID string `json:"auth_account_id"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Limit         int    `json:"limit"`
}

type marketingPlanReportOutput struct {
	OK             bool                                    `json:"ok"`
	RequestID      string                                  `json:"request_id"`
	Source         string                                  `json:"source"`
	AdvertiserID   string                                  `json:"advertiser_id"`
	DateRange      queryDateRange                          `json:"date_range"`
	AmountUnit     string                                  `json:"amount_unit"`
	Summary        applicationreports.MarketingPlanSummary `json:"summary"`
	TotalRowCount  int                                     `json:"total_row_count"`
	DisplayedCount int                                     `json:"displayed_count"`
	Truncated      bool                                    `json:"truncated"`
	Presentation   queryPresentation                       `json:"presentation"`
	Items          []marketingPlanReportItem               `json:"items"`
}

type marketingPlanReportItem struct {
	ProjectID       string  `json:"project_id"`
	ProjectName     string  `json:"project_name"`
	StatCost        *string `json:"stat_cost"`
	ShowCount       *string `json:"show_count"`
	ClickCount      *string `json:"click_count"`
	CTR             *string `json:"ctr"`
	ConvertCount    *string `json:"convert_count"`
	ConversionCost  *string `json:"conversion_cost"`
	ConversionRate  *string `json:"conversion_rate"`
	InAppOrderCount *string `json:"in_app_order_count"`
	InAppOrderGMV   *string `json:"in_app_order_gmv"`
	InAppOrderROI   *string `json:"in_app_order_roi"`
}

type marketingMaterialReportInput struct {
	AdvertiserID  string   `json:"advertiser_id"`
	AuthAccountID string   `json:"auth_account_id"`
	StartDate     string   `json:"start_date"`
	EndDate       string   `json:"end_date"`
	ProjectID     string   `json:"project_id"`
	PromotionIDs  []string `json:"promotion_ids"`
	ActiveOnly    bool     `json:"active_only"`
	Limit         int      `json:"limit"`
}

type marketingMaterialReportOutput struct {
	OK             bool                                        `json:"ok"`
	RequestID      string                                      `json:"request_id"`
	Source         string                                      `json:"source"`
	AdvertiserID   string                                      `json:"advertiser_id"`
	DateRange      queryDateRange                              `json:"date_range"`
	AmountUnit     string                                      `json:"amount_unit"`
	Summary        applicationreports.MarketingMaterialSummary `json:"summary"`
	TotalRowCount  int                                         `json:"total_row_count"`
	DisplayedCount int                                         `json:"displayed_count"`
	Truncated      bool                                        `json:"truncated"`
	Items          []marketingMaterialReportItem               `json:"items"`
}

type marketingMaterialReportItem struct {
	ProjectID            string  `json:"project_id"`
	PromotionID          string  `json:"promotion_id"`
	PromotionName        string  `json:"promotion_name"`
	PromotionStatus      string  `json:"promotion_status"`
	PromotionOptStatus   string  `json:"promotion_opt_status"`
	MaterialID           string  `json:"material_id"`
	VideoID              string  `json:"video_id"`
	VideoCoverID         string  `json:"video_cover_id"`
	MaterialStatus       string  `json:"material_status"`
	MaterialOptStatus    string  `json:"material_opt_status"`
	ImageMode            string  `json:"image_mode"`
	MaterialCreateTime   string  `json:"material_create_time"`
	HasReportData        bool    `json:"has_report_data"`
	StatCost             *string `json:"stat_cost"`
	ShowCount            *string `json:"show_count"`
	ClickCount           *string `json:"click_count"`
	CTR                  *string `json:"ctr"`
	CPC                  *string `json:"cpc"`
	CPM                  *string `json:"cpm"`
	ConvertCount         *string `json:"convert_count"`
	ConversionCost       *string `json:"conversion_cost"`
	ConversionRate       *string `json:"conversion_rate"`
	TotalPlay            *string `json:"total_play"`
	PlayDuration3Seconds *string `json:"play_duration_3s"`
	PlayOverRate         *string `json:"play_over_rate"`
	InAppOrder           *string `json:"in_app_order"`
	InAppOrderGMV        *string `json:"in_app_order_gmv"`
	InAppOrderROI        *string `json:"in_app_order_roi"`
}

func (runtime Runtime) getMarketingAuthorization(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	var input marketingAuthorizationInput
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !optionalDecimalID(input.AdvertiserID) {
		return runtime.failureResult(started, requestID, "get_marketing_authorization", invalidArgumentFailure()), nil
	}
	if runtime.MarketingAuth == nil {
		return runtime.failureResult(started, requestID, "get_marketing_authorization", internalFailure()), nil
	}
	result, err := runtime.MarketingAuth.Inspect(ctx, authapplication.StatusQuery{
		Channel: "marketing", AdvertiserID: input.AdvertiserID,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "get_marketing_authorization", mapMarketingAuthorizationError(err)), nil
	}
	status, mappings, authorizations := presentMarketingAuthorization(result, input.AdvertiserID)
	return runtime.successResult(started, requestID, "get_marketing_authorization", marketingAuthorizationOutput{
		OK: true, RequestID: requestID, Source: "local_state", Status: status,
		Mappings: mappings, Authorizations: authorizations,
	}), nil
}

func (runtime Runtime) searchMarketingVideos(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := marketingVideosInput{Page: 1, Limit: 50}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !validMarketingVideosInput(input) {
		return runtime.failureResult(started, requestID, "search_marketing_videos", invalidArgumentFailure()), nil
	}
	if runtime.MarketingMaterials == nil {
		return runtime.failureResult(started, requestID, "search_marketing_videos", internalFailure()), nil
	}
	result, err := runtime.MarketingMaterials.QueryVideos(ctx, applicationmaterials.VideoQuery{
		CredentialScope: applicationmaterials.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		Mode: "library-get", VideoIDs: input.VideoIDs, MaterialIDs: input.MaterialIDs,
		Signatures: input.Signatures, Filename: input.Filename,
		StartTime: input.StartDate, EndTime: input.EndDate, Page: input.Page, PageSize: input.Limit,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "search_marketing_videos", mapMarketingOfficialError(err)), nil
	}
	items, err := presentMarketingVideos(result.SelectedVideos)
	if err != nil {
		return runtime.failureResult(started, requestID, "search_marketing_videos", invalidUpstreamFailure()), nil
	}
	total := result.MatchedCount
	hasMore := false
	if result.PageInfo != nil {
		total = result.PageInfo.TotalNumber
		hasMore = result.PageInfo.Page < result.PageInfo.TotalPages
	}
	return runtime.successResult(started, requestID, "search_marketing_videos", marketingVideosOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: input.AdvertiserID,
		Page: input.Page, TotalCount: total, DisplayedCount: len(items), HasMore: hasMore, Items: items,
	}), nil
}

func (runtime Runtime) searchMarketingCreatorMaterials(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := marketingCreatorInput{Source: "authorized", MinimumRemainingDays: 1, Page: 1, Limit: 50}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !validMarketingCreatorInput(input) {
		return runtime.failureResult(started, requestID, "search_marketing_creator_materials", invalidArgumentFailure()), nil
	}
	if runtime.MarketingMaterials == nil {
		return runtime.failureResult(started, requestID, "search_marketing_creator_materials", internalFailure()), nil
	}
	result, err := runtime.MarketingMaterials.QueryCreator(ctx, applicationmaterials.CreatorQuery{
		CredentialScope: applicationmaterials.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		Source: input.Source, AwemeIDs: input.AwemeIDs, ItemIDs: input.ItemIDs,
		MinimumRemainingDays: input.MinimumRemainingDays, Page: input.Page, PageSize: input.Limit,
		MaxPages: 1, SinglePage: true,
		IncludeUnusable: input.IncludeUnusable,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "search_marketing_creator_materials", mapMarketingOfficialError(err)), nil
	}
	items, err := presentMarketingCreatorItems(result.Candidates)
	if err != nil {
		return runtime.failureResult(started, requestID, "search_marketing_creator_materials", invalidUpstreamFailure()), nil
	}
	sourceTotal, hasMore := result.CandidateCount, false
	if result.PageInfo != nil {
		sourceTotal = result.PageInfo.TotalNumber
		hasMore = result.PageInfo.Page < result.PageInfo.TotalPages
	}
	return runtime.successResult(started, requestID, "search_marketing_creator_materials", marketingCreatorOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: input.AdvertiserID,
		MaterialSource: result.SourceType, Page: input.Page, SourceTotal: sourceTotal,
		DisplayedCount: len(items), HasMore: hasMore, Items: items,
	}), nil
}

func (runtime Runtime) reportMarketingPlans(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := marketingPlanReportInput{Limit: 10}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		!validOptionalDateRange(input.StartDate, input.EndDate) || input.Limit < 1 || input.Limit > 100 {
		return runtime.failureResult(started, requestID, "report_marketing_plans", invalidArgumentFailure()), nil
	}
	if runtime.MarketingReports == nil {
		return runtime.failureResult(started, requestID, "report_marketing_plans", internalFailure()), nil
	}
	result, err := runtime.MarketingReports.Plans(ctx, applicationreports.MarketingPlanQuery{
		CredentialScope: applicationreports.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		StartDate: input.StartDate, EndDate: input.EndDate, PageSize: 100, MaxPages: 100, Top: input.Limit,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "report_marketing_plans", mapMarketingOfficialError(err)), nil
	}
	items, err := presentMarketingPlanRows(result.Rows)
	if err != nil || !validRenderedMarkdown(result.Presentation.RenderedMarkdown) {
		return runtime.failureResult(started, requestID, "report_marketing_plans", invalidUpstreamFailure()), nil
	}
	return runtime.successResult(started, requestID, "report_marketing_plans", marketingPlanReportOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: input.AdvertiserID,
		DateRange:  queryDateRange{StartDate: result.DateRange.StartDate, EndDate: result.DateRange.EndDate},
		AmountUnit: "CNY", Summary: result.Summary, TotalRowCount: result.RowCount,
		DisplayedCount: len(items), Truncated: len(items) < result.RowCount,
		Presentation: queryPresentation{Required: true, RenderedMarkdown: result.Presentation.RenderedMarkdown},
		Items:        items,
	}), nil
}

func (runtime Runtime) reportMarketingMaterials(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := marketingMaterialReportInput{Limit: 50}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !validMarketingMaterialReportInput(input) {
		return runtime.failureResult(started, requestID, "report_marketing_materials", invalidArgumentFailure()), nil
	}
	if runtime.MarketingReports == nil {
		return runtime.failureResult(started, requestID, "report_marketing_materials", internalFailure()), nil
	}
	result, err := runtime.MarketingReports.Materials(ctx, applicationreports.MarketingMaterialQuery{
		CredentialScope: applicationreports.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		StartDate: input.StartDate, EndDate: input.EndDate,
		DataTopic: applicationreports.MarketingMaterialTopic,
		ProjectID: input.ProjectID, PromotionIDs: input.PromotionIDs, ActiveOnly: input.ActiveOnly,
		PromotionPageSize: 100, ReportPageSize: 100,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "report_marketing_materials", mapMarketingOfficialError(err)), nil
	}
	rows := result.Rows
	if len(rows) > input.Limit {
		rows = rows[:input.Limit]
	}
	items, err := presentMarketingMaterialRows(rows)
	if err != nil {
		return runtime.failureResult(started, requestID, "report_marketing_materials", invalidUpstreamFailure()), nil
	}
	return runtime.successResult(started, requestID, "report_marketing_materials", marketingMaterialReportOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: input.AdvertiserID,
		DateRange:  queryDateRange{StartDate: result.DateRange.StartDate, EndDate: result.DateRange.EndDate},
		AmountUnit: "CNY", Summary: result.Summary, TotalRowCount: result.RowCount,
		DisplayedCount: len(items), Truncated: len(items) < result.RowCount, Items: items,
	}), nil
}

func presentMarketingAuthorization(result authapplication.InspectionResult, filter string) (
	qianchuanAuthorizationStatus,
	[]qianchuanAdvertiserMapping,
	[]qianchuanAuthorizationItem,
) {
	var advertiserID *string
	if filter != "" {
		value := filter
		advertiserID = &value
	}
	statusByID := map[string]authapplication.AuthorizationStatus{}
	for _, row := range result.Status.Authorizations {
		statusByID[row.AuthorizationID] = row
	}
	authorizations := make([]qianchuanAuthorizationItem, 0, len(result.Mappings.Authorizations))
	for _, row := range result.Mappings.Authorizations {
		status := statusByID[row.AuthorizationID]
		authorizations = append(authorizations, qianchuanAuthorizationItem{
			AuthorizationID: row.AuthorizationID, TokenRevision: row.TokenRevision,
			HasAccessToken: row.HasAccessToken, HasRefreshToken: row.HasRefreshToken,
			AccessTokenExpiresAt: status.AccessTokenExpiresAt, RefreshTokenExpiresAt: status.RefreshTokenExpiresAt,
			PendingAccountSync: row.PendingAccountSync, AdvertiserIDs: append([]string(nil), row.AdvertiserIDs...),
		})
	}
	mappings := make([]qianchuanAdvertiserMapping, len(result.Mappings.Mappings))
	for index, row := range result.Mappings.Mappings {
		mappings[index] = qianchuanAdvertiserMapping{
			AdvertiserID: row.AdvertiserID, AuthorizationIDs: append([]string(nil), row.AuthorizationIDs...),
			Ambiguous: row.Ambiguous,
		}
	}
	return qianchuanAuthorizationStatus{
		HasAppID: result.Status.HasAppID, HasSecret: result.Status.HasSecret,
		AuthorizationCount:        result.Status.AuthorizationCount,
		AuthorizedAccountCount:    result.Status.AuthorizedAccountCount,
		AuthorizedAdvertiserCount: result.Status.AuthorizedAdvertiserCount,
		Generation:                result.Status.Generation, AdvertiserID: advertiserID,
		AdvertiserIDAuthorized: result.Status.AdvertiserIDAuthorized,
	}, mappings, authorizations
}

func presentMarketingVideos(rows []domainmarketing.SelectedVideo) ([]marketingVideoItem, error) {
	items := make([]marketingVideoItem, len(rows))
	for index, row := range rows {
		values := []struct {
			value string
			max   int
		}{{row.VideoID, 128}, {row.MaterialID, 128}, {row.Filename, 1000}, {row.CreateTime, 128}, {row.Format, 128}, {row.Source, 128}, {row.Signature, 512}}
		for _, value := range values {
			if !boundedText(value.value, value.max) {
				return nil, errors.New("Marketing video field is invalid")
			}
		}
		items[index] = marketingVideoItem{
			VideoID: row.VideoID, MaterialID: row.MaterialID, Filename: row.Filename,
			CreateTime: row.CreateTime, Width: row.Width, Height: row.Height, Duration: row.Duration,
			Format: row.Format, Source: row.Source, Signature: row.Signature,
		}
	}
	return items, nil
}

func presentMarketingCreatorItems(rows []domainmarketing.CreatorCandidate) ([]marketingCreatorItem, error) {
	items := make([]marketingCreatorItem, len(rows))
	for index, row := range rows {
		values := []struct {
			value string
			max   int
		}{
			{row.MaterialID, 128}, {row.VideoID, 128}, {row.ItemID, 128}, {row.ImageMode, 128},
			{row.VideoCoverID, 128}, {row.Title, 2000}, {row.CreatorID, 128}, {row.CreatorName, 500},
			{row.AuthorizationSubjectID, 128}, {row.AuthorizationType, 128}, {row.AuthorizationStatus, 128},
			{row.AuthorizationStartAt, 128}, {row.AuthorizationExpiresAt, 128},
		}
		for _, value := range values {
			if !boundedText(value.value, value.max) {
				return nil, errors.New("Marketing creator field is invalid")
			}
		}
		if !boundedStrings(row.WarningTypes, 100, 256) || !boundedStrings(row.UnusableReasons, 100, 256) {
			return nil, errors.New("Marketing creator reason is invalid")
		}
		items[index] = marketingCreatorItem{
			MaterialID: row.MaterialID, VideoID: row.VideoID, ItemID: row.ItemID,
			ImageMode: row.ImageMode, VideoCoverID: row.VideoCoverID, Title: row.Title, Duration: row.Duration,
			CreatorID: row.CreatorID, CreatorName: row.CreatorName,
			AuthorizationSubjectID: row.AuthorizationSubjectID, AuthorizationType: row.AuthorizationType,
			AuthorizationStatus: row.AuthorizationStatus, AuthorizationStartAt: row.AuthorizationStartAt,
			AuthorizationExpiresAt: row.AuthorizationExpiresAt,
			WarningTypes:           append([]string(nil), row.WarningTypes...), Usable: row.Usable,
			UnusableReasons: append([]string(nil), row.UnusableReasons...),
		}
	}
	return items, nil
}

func presentMarketingPlanRows(rows []map[string]any) ([]marketingPlanReportItem, error) {
	items := make([]marketingPlanReportItem, len(rows))
	for index, row := range rows {
		projectID := safeString(row["project_id"])
		projectName := safeString(row["project_name"])
		if !requiredDecimalID(projectID) || !boundedText(projectName, 500) {
			return nil, errors.New("Marketing plan identity is invalid")
		}
		metrics, err := marketingMetricPointers(row, []string{
			"stat_cost", "show_cnt", "click_cnt", "ctr", "convert_cnt", "conversion_cost",
			"conversion_rate", "in_app_order_count", "in_app_order_gmv", "in_app_order_roi",
		})
		if err != nil {
			return nil, err
		}
		items[index] = marketingPlanReportItem{
			ProjectID: projectID, ProjectName: projectName,
			StatCost: metrics["stat_cost"], ShowCount: metrics["show_cnt"], ClickCount: metrics["click_cnt"],
			CTR: metrics["ctr"], ConvertCount: metrics["convert_cnt"], ConversionCost: metrics["conversion_cost"],
			ConversionRate: metrics["conversion_rate"], InAppOrderCount: metrics["in_app_order_count"],
			InAppOrderGMV: metrics["in_app_order_gmv"], InAppOrderROI: metrics["in_app_order_roi"],
		}
	}
	return items, nil
}

func presentMarketingMaterialRows(rows []map[string]any) ([]marketingMaterialReportItem, error) {
	items := make([]marketingMaterialReportItem, len(rows))
	for index, row := range rows {
		ids := []string{safeString(row["project_id"]), safeString(row["promotion_id"]), safeString(row["material_id"])}
		for _, id := range ids {
			if !requiredDecimalID(id) {
				return nil, errors.New("Marketing material identity is invalid")
			}
		}
		textFields := []string{
			"promotion_name", "promotion_status", "promotion_opt_status", "video_id", "video_cover_id",
			"material_status", "material_opt_status", "image_mode", "material_create_time",
		}
		for _, field := range textFields {
			if !boundedText(safeString(row[field]), 1000) {
				return nil, errors.New("Marketing material field is invalid")
			}
		}
		hasReportData, ok := row["has_report_data"].(bool)
		if !ok {
			return nil, errors.New("Marketing material report state is invalid")
		}
		metrics, err := marketingMetricPointers(row, applicationreports.DefaultMarketingMetrics)
		if err != nil {
			return nil, err
		}
		items[index] = marketingMaterialReportItem{
			ProjectID: ids[0], PromotionID: ids[1], PromotionName: safeString(row["promotion_name"]),
			PromotionStatus: safeString(row["promotion_status"]), PromotionOptStatus: safeString(row["promotion_opt_status"]),
			MaterialID: ids[2], VideoID: safeString(row["video_id"]), VideoCoverID: safeString(row["video_cover_id"]),
			MaterialStatus: safeString(row["material_status"]), MaterialOptStatus: safeString(row["material_opt_status"]),
			ImageMode: safeString(row["image_mode"]), MaterialCreateTime: safeString(row["material_create_time"]),
			HasReportData: hasReportData, StatCost: metrics["stat_cost"], ShowCount: metrics["show_cnt"],
			ClickCount: metrics["click_cnt"], CTR: metrics["ctr"], CPC: metrics["cpc_platform"],
			CPM: metrics["cpm_platform"], ConvertCount: metrics["convert_cnt"],
			ConversionCost: metrics["conversion_cost"], ConversionRate: metrics["conversion_rate"],
			TotalPlay: metrics["total_play"], PlayDuration3Seconds: metrics["play_duration_3s"],
			PlayOverRate: metrics["play_over_rate"], InAppOrder: metrics["in_app_order"],
			InAppOrderGMV: metrics["in_app_order_gmv"], InAppOrderROI: metrics["in_app_order_roi"],
		}
	}
	return items, nil
}

func marketingMetricPointers(row map[string]any, fields []string) (map[string]*string, error) {
	result := make(map[string]*string, len(fields))
	for _, field := range fields {
		value := row[field]
		if value == nil {
			result[field] = nil
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || len(text) > 128 {
			return nil, fmt.Errorf("Marketing metric %s is invalid", field)
		}
		if _, err := domain.ParseDecimal(text); err != nil {
			return nil, fmt.Errorf("Marketing metric %s is invalid", field)
		}
		result[field] = &text
	}
	return result, nil
}

func validMarketingVideosInput(input marketingVideosInput) bool {
	return requiredDecimalID(input.AdvertiserID) && optionalDecimalID(input.AuthAccountID) &&
		input.Page >= 1 && input.Page <= 10000 && input.Limit >= 1 && input.Limit <= 100 &&
		validOptionalDateRange(input.StartDate, input.EndDate) &&
		len(input.VideoIDs) <= 100 && len(input.MaterialIDs) <= 100 && len(input.Signatures) <= 100 &&
		nonEmptyStringGroups(input.VideoIDs, input.MaterialIDs, input.Signatures) <= 1 &&
		boundedStrings(input.VideoIDs, 100, 128) && allDecimalIDs(input.MaterialIDs) &&
		boundedStrings(input.Signatures, 100, 512) && boundedText(strings.TrimSpace(input.Filename), 500)
}

func validMarketingCreatorInput(input marketingCreatorInput) bool {
	if !requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		input.Source != "authorized" && input.Source != "homepage" || input.MinimumRemainingDays < 0 ||
		input.MinimumRemainingDays > 3650 || input.Page < 1 || input.Page > 10000 || input.Limit < 1 || input.Limit > 100 ||
		len(input.AwemeIDs) > 100 || len(input.ItemIDs) > 100 ||
		!allDecimalIDs(input.AwemeIDs) || !allDecimalIDs(input.ItemIDs) {
		return false
	}
	return input.Source != "homepage" || len(input.AwemeIDs) == 1
}

func validMarketingMaterialReportInput(input marketingMaterialReportInput) bool {
	return requiredDecimalID(input.AdvertiserID) && optionalDecimalID(input.AuthAccountID) &&
		validOptionalDateRange(input.StartDate, input.EndDate) && optionalDecimalID(input.ProjectID) &&
		len(input.PromotionIDs) <= 100 && allDecimalIDs(input.PromotionIDs) &&
		input.Limit >= 1 && input.Limit <= 100
}

func nonEmptyStringGroups(groups ...[]string) int {
	count := 0
	for _, group := range groups {
		if len(group) != 0 {
			count++
		}
	}
	return count
}

func boundedText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func boundedStrings(values []string, maximumItems, maximumLength int) bool {
	if len(values) > maximumItems {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" || !boundedText(value, maximumLength) {
			return false
		}
	}
	return true
}

func validRenderedMarkdown(value string) bool {
	return value != "" && boundedText(value, 2_000_000)
}

func invalidUpstreamFailure() toolFailure {
	return toolFailure{Code: "UPSTREAM_RESPONSE_INVALID", Message: "Marketing query returned invalid data", Details: map[string]any{}}
}

func mapMarketingAuthorizationError(err error) toolFailure {
	local := mapLocalQueryError(err)
	if local.Code != "INTERNAL_ERROR" {
		return local
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) && domainErr.Code == "authorization_not_found" {
		return toolFailure{Code: "AUTHORIZATION_NOT_FOUND", Message: "Marketing authorization was not found", Details: map[string]any{}}
	}
	return internalFailure()
}

func mapMarketingOfficialError(err error) toolFailure {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Marketing query was interrupted", Retryable: true, Details: map[string]any{}}
	}
	local := mapLocalQueryError(err)
	if local.Code != "INTERNAL_ERROR" {
		return local
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		switch domainErr.Code {
		case "authorization_not_found":
			return toolFailure{Code: "AUTHORIZATION_NOT_FOUND", Message: "Marketing authorization was not found", Details: map[string]any{}}
		case "authorization_ambiguous":
			return toolFailure{Code: "AUTHORIZATION_AMBIGUOUS", Message: "Marketing advertiser maps to multiple authorizations", Details: map[string]any{}}
		case "reauthorization_required", "authorization_pending_sync":
			return toolFailure{Code: "REAUTHORIZATION_REQUIRED", Message: "Marketing authorization must be renewed", Details: map[string]any{}}
		}
	}
	var envelope *oceanengine.EnvelopeError
	if errors.As(err, &envelope) && envelope.Code == 40103 {
		return toolFailure{Code: "REAUTHORIZATION_REQUIRED", Message: "Marketing authorization must be renewed", Details: map[string]any{}}
	}
	return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Marketing query could not complete", Retryable: true, Details: map[string]any{}}
}
