package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

var qianchuanPreflightIDPattern = regexp.MustCompile(`^qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12}$`)

type preflightInput struct {
	PlanTemplate  string   `json:"plan_template"`
	WorkURLs      []string `json:"work_urls"`
	Concurrency   int      `json:"concurrency"`
	AuthAccountID string   `json:"auth_account_id"`
	PlanType      string   `json:"plan_type"`
	Business      string   `json:"business"`
}

type getPreflightInput struct {
	PreflightID string `json:"preflight_id"`
}

type preflightOutput struct {
	OK            bool                  `json:"ok"`
	RequestID     string                `json:"request_id"`
	Mode          string                `json:"mode"`
	Channel       string                `json:"channel"`
	Template      preflightTemplate     `json:"template"`
	Counts        map[string]int        `json:"counts"`
	Results       []preflightGroup      `json:"results"`
	Skipped       []preflightSkipped    `json:"skipped"`
	QueryFailures []preflightQueryFail  `json:"query_failures"`
	FailedResults []preflightGroup      `json:"failed_results"`
	Performance   preflightPerformance  `json:"performance"`
	Presentation  preflightPresentation `json:"presentation"`
	PreflightID   string                `json:"preflight_id"`
	ExpiresAt     string                `json:"expires_at"`
}

type preflightTemplate struct {
	TemplateID   string   `json:"template_id"`
	Name         string   `json:"name"`
	AdvertiserID string   `json:"advertiser_id"`
	ProductIDs   []string `json:"product_ids"`
}

type preflightGroup struct {
	CreatorID        string   `json:"creator_id"`
	DouyinID         string   `json:"douyin_id,omitempty"`
	CreatorName      string   `json:"creator_name,omitempty"`
	ExistingPlanID   string   `json:"existing_plan_id,omitempty"`
	PlanName         string   `json:"plan_name,omitempty"`
	PlanStatus       string   `json:"plan_status,omitempty"`
	ProductIDs       []string `json:"product_ids"`
	InputItemIDs     []string `json:"input_item_ids"`
	AlreadyPresent   []string `json:"already_present_item_ids"`
	CompletedItemIDs []string `json:"completed_item_ids"`
	Status           string   `json:"status"`
	Error            string   `json:"error,omitempty"`
}

type preflightSkipped struct {
	InputIndex  int    `json:"input_index"`
	AwemeItemID string `json:"aweme_item_id,omitempty"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
}

type preflightQueryFail struct {
	CreatorID string `json:"creator_id"`
	ProductID string `json:"product_id,omitempty"`
	Message   string `json:"message"`
}

type preflightPerformance struct {
	LinkResolutionSeconds       float64                   `json:"link_resolution_seconds"`
	CredentialResolutionSeconds float64                   `json:"credential_resolution_seconds"`
	MaterialResolutionSeconds   float64                   `json:"material_resolution_seconds"`
	PlanReconciliationSeconds   float64                   `json:"plan_reconciliation_seconds"`
	TotalSeconds                float64                   `json:"total_seconds"`
	OwnerHintCache              preflightOwnerHintMetrics `json:"owner_hint_cache"`
	LinkMetadata                preflightLinkMetadata     `json:"link_metadata"`
}

type preflightOwnerHintMetrics struct {
	Supplied                   int `json:"supplied"`
	Eligible                   int `json:"eligible"`
	Verified                   int `json:"verified"`
	Stale                      int `json:"stale"`
	AuthorizedHintQueryCount   int `json:"authorized_hint_query_count"`
	AuthorizedHintFailureCount int `json:"authorized_hint_failure_count"`
	OfficialVideoQueryCount    int `json:"official_video_query_count"`
	Loaded                     int `json:"loaded"`
	LoadedFromCache            int `json:"loaded_from_cache"`
	LoadedFromLinkMetadata     int `json:"loaded_from_link_metadata"`
	Stored                     int `json:"stored"`
}

type preflightLinkMetadata struct {
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

type preflightPresentationColumn struct {
	Field string `json:"field"`
	Label string `json:"label"`
}

type preflightPresentationRow struct {
	PlanID          string `json:"plan_id"`
	CreatorNickname string `json:"creator_nickname"`
	ProductID       string `json:"product_id"`
	MaterialID      string `json:"material_id"`
	MaterialTitle   string `json:"material_title"`
}

type preflightPresentation struct {
	Format                string                        `json:"format"`
	Required              bool                          `json:"required"`
	AllowColumnOmission   bool                          `json:"allow_column_omission"`
	AllowColumnReordering bool                          `json:"allow_column_reordering"`
	Columns               []preflightPresentationColumn `json:"columns"`
	Rows                  []preflightPresentationRow    `json:"rows"`
	RequiredDetails       []preflightPresentationColumn `json:"required_details"`
	DetailsOutsideTable   []string                      `json:"details_outside_table"`
	RenderedMarkdown      string                        `json:"rendered_markdown"`
}

type getPreflightDecision struct {
	CreatorID      string `json:"creator_id"`
	Action         string `json:"action"`
	ExistingPlanID string `json:"existing_plan_id,omitempty"`
}

type getPreflightSnapshot struct {
	PreflightID      string                 `json:"preflight_id"`
	CreatedAt        string                 `json:"created_at"`
	ExpiresAt        string                 `json:"expires_at"`
	AdvertiserID     string                 `json:"advertiser_id"`
	TemplateID       string                 `json:"template_id"`
	TemplateName     string                 `json:"template_name"`
	ProductName      string                 `json:"product_name"`
	ProductShortName string                 `json:"product_short_name"`
	ProductIDs       []string               `json:"product_ids"`
	EligibleWorks    int                    `json:"eligible_works"`
	SkippedWorks     int                    `json:"skipped_works"`
	Decisions        []getPreflightDecision `json:"decisions"`
	ReadyForSubmit   bool                   `json:"ready_for_submit"`
}

type getPreflightOutput struct {
	OK        bool                 `json:"ok"`
	RequestID string               `json:"request_id"`
	Preflight getPreflightSnapshot `json:"preflight"`
}

func (runtime Runtime) preflightQianchuanWorks(
	ctx context.Context,
	request *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input, failure := decodePreflightInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "preflight_qianchuan_works", *failure), nil
	}
	if runtime.QianchuanPreflights == nil {
		return runtime.failureResult(started, requestID, "preflight_qianchuan_works", internalFailure()), nil
	}
	result, err := runtime.QianchuanPreflights.BatchWorks(ctx, applicationqianchuan.BatchWorksCommand{
		PlanTemplate: input.PlanTemplate, WorkURLs: append([]string(nil), input.WorkURLs...),
		Concurrency: input.Concurrency, AuthAccountID: input.AuthAccountID,
		PlanType: input.PlanType, Business: input.Business, Submit: false, IncludePayloads: false,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "preflight_qianchuan_works", mapQianchuanPreflightError(err)), nil
	}
	output := presentPreflightOutput(requestID, result)
	return runtime.successResult(started, requestID, "preflight_qianchuan_works", output), nil
}

func (runtime Runtime) getQianchuanPreflight(
	ctx context.Context,
	request *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input, failure := decodeGetPreflightInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_preflight", *failure), nil
	}
	if runtime.QianchuanPreflights == nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_preflight", internalFailure()), nil
	}
	summary, err := runtime.QianchuanPreflights.GetBatchPreflight(ctx, input.PreflightID)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_preflight", mapQianchuanSnapshotError(err)), nil
	}
	return runtime.successResult(started, requestID, "get_qianchuan_preflight", getPreflightOutput{
		OK: true, RequestID: requestID, Preflight: presentGetPreflightSnapshot(summary),
	}), nil
}

func decodePreflightInput(raw json.RawMessage) (preflightInput, *toolFailure) {
	input := preflightInput{Concurrency: applicationqianchuan.DefaultBatchConcurrency}
	if err := decodeStrict(raw, &input); err != nil ||
		!validText(input.PlanTemplate, 256, true) || len(input.WorkURLs) < 1 || len(input.WorkURLs) > 100 ||
		input.Concurrency < 1 || input.Concurrency > 10 ||
		!validText(input.AuthAccountID, 256, false) || !validText(input.PlanType, 128, false) ||
		!validText(input.Business, 128, false) {
		failure := invalidArgumentFailure()
		return preflightInput{}, &failure
	}
	for _, value := range input.WorkURLs {
		if !validText(value, 2048, true) {
			failure := invalidArgumentFailure()
			return preflightInput{}, &failure
		}
	}
	return input, nil
}

func decodeGetPreflightInput(raw json.RawMessage) (getPreflightInput, *toolFailure) {
	var input getPreflightInput
	if err := decodeStrict(raw, &input); err != nil || !qianchuanPreflightIDPattern.MatchString(input.PreflightID) {
		failure := invalidArgumentFailure()
		return getPreflightInput{}, &failure
	}
	return input, nil
}

func validText(value string, maximum int, required bool) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == value && (!required || value != "") && len([]rune(value)) <= maximum
}

func presentPreflightOutput(requestID string, result applicationqianchuan.BatchCommandResult) preflightOutput {
	skipped := make([]preflightSkipped, 0, len(result.Skipped))
	for _, row := range result.Skipped {
		skipped = append(skipped, preflightSkipped{
			InputIndex: row.InputIndex, AwemeItemID: row.AwemeItemID,
			Reason: row.Reason, Message: "work was skipped during preflight",
		})
	}
	queryFailures := make([]preflightQueryFail, 0, len(result.QueryFailures))
	for _, row := range result.QueryFailures {
		queryFailures = append(queryFailures, preflightQueryFail{
			CreatorID: row.AwemeID, ProductID: row.ProductID, Message: "official query did not complete",
		})
	}
	return preflightOutput{
		OK: true, RequestID: requestID, Mode: result.Mode, Channel: result.Channel,
		Template: preflightTemplate{
			TemplateID: result.Template.TemplateID, Name: result.Template.Name,
			AdvertiserID: result.Template.AdvertiserID, ProductIDs: append([]string(nil), result.Template.ProductIDs...),
		},
		Counts: cloneCounts(result.Counts), Results: presentPreflightGroups(result.Results, false),
		Skipped: skipped, QueryFailures: queryFailures, FailedResults: presentPreflightGroups(result.FailedResults, true),
		Performance: presentPreflightPerformance(result.Performance), Presentation: presentPreflightPresentation(result.Presentation),
		PreflightID: result.PreflightID, ExpiresAt: result.ExpiresAt,
	}
}

func presentPreflightPresentation(value domain.Presentation) preflightPresentation {
	columns := []preflightPresentationColumn{
		{Field: "plan_id", Label: "计划ID"},
		{Field: "creator_nickname", Label: "达人昵称"},
		{Field: "product_id", Label: "商品ID"},
		{Field: "material_id", Label: "素材ID"},
		{Field: "material_title", Label: "素材标题"},
	}
	details := []preflightPresentationColumn{
		{Field: "skipped", Label: "跳过详情"},
		{Field: "query_failures", Label: "查询失败"},
		{Field: "failed_results", Label: "执行失败"},
	}
	rows := make([]preflightPresentationRow, 0, len(value.Rows))
	for _, row := range value.Rows {
		rows = append(rows, preflightPresentationRow{
			PlanID: presentationValue(row["plan_id"]), CreatorNickname: presentationValue(row["creator_nickname"]),
			ProductID: presentationValue(row["product_id"]), MaterialID: presentationValue(row["material_id"]),
			MaterialTitle: presentationValue(row["material_title"]),
		})
	}
	return preflightPresentation{
		Format: "markdown", Required: true, AllowColumnOmission: false, AllowColumnReordering: false,
		Columns: columns, Rows: rows, RequiredDetails: details,
		DetailsOutsideTable: []string{"skipped", "query_failures", "failed_results"},
		RenderedMarkdown:    renderPreflightMarkdown(rows),
	}
}

func presentationValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " "))
}

func renderPreflightMarkdown(rows []preflightPresentationRow) string {
	lines := []string{
		"| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |",
		"| --- | --- | --- | --- | --- |",
	}
	for _, row := range rows {
		lines = append(lines, "| "+strings.Join([]string{
			markdownValue(row.PlanID), markdownValue(row.CreatorNickname), markdownValue(row.ProductID),
			markdownValue(row.MaterialID), markdownValue(row.MaterialTitle),
		}, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func markdownValue(value string) string {
	if value == "" {
		return "—"
	}
	return strings.ReplaceAll(value, "|", `\|`)
}

func presentGetPreflightSnapshot(summary applicationqianchuan.BatchPreflightSummary) getPreflightSnapshot {
	decisions := make([]getPreflightDecision, 0, len(summary.Decisions))
	for _, decision := range summary.Decisions {
		decisions = append(decisions, getPreflightDecision{
			CreatorID: decision.CreatorID, Action: decision.Action, ExistingPlanID: decision.ExistingPlanID,
		})
	}
	return getPreflightSnapshot{
		PreflightID: summary.PreflightID, CreatedAt: summary.CreatedAt, ExpiresAt: summary.ExpiresAt,
		AdvertiserID: summary.AdvertiserID, TemplateID: summary.TemplateID, TemplateName: summary.TemplateName,
		ProductName: summary.ProductName, ProductShortName: summary.ProductShortName,
		ProductIDs: append([]string(nil), summary.ProductIDs...), EligibleWorks: summary.EligibleWorks,
		SkippedWorks: summary.SkippedWorks, Decisions: decisions, ReadyForSubmit: summary.ReadyForSubmit,
	}
}

func presentPreflightGroups(rows []applicationqianchuan.BatchGroupResult, failed bool) []preflightGroup {
	result := make([]preflightGroup, 0, len(rows))
	for _, row := range rows {
		failure := ""
		if failed || row.Status == "failed" || row.Status == "preflight_changed" {
			failure = "creator preflight did not complete"
		}
		result = append(result, preflightGroup{
			CreatorID: row.AwemeID, DouyinID: row.DouyinID, CreatorName: row.CreatorName,
			ExistingPlanID: row.AdID, PlanName: row.PlanName, PlanStatus: row.PlanStatus,
			ProductIDs: append([]string(nil), row.ProductIDs...), InputItemIDs: append([]string(nil), row.InputItemIDs...),
			AlreadyPresent:   append([]string(nil), row.AlreadyPresent...),
			CompletedItemIDs: append([]string(nil), row.CompletedItemIDs...), Status: row.Status, Error: failure,
		})
	}
	return result
}

func presentPreflightPerformance(value applicationqianchuan.BatchPerformance) preflightPerformance {
	cache := value.OwnerHintCache
	return preflightPerformance{
		LinkResolutionSeconds:       value.LinkResolutionSeconds,
		CredentialResolutionSeconds: value.CredentialResolutionSeconds,
		MaterialResolutionSeconds:   value.MaterialResolutionSeconds,
		PlanReconciliationSeconds:   value.PlanReconciliationSeconds, TotalSeconds: value.TotalSeconds,
		OwnerHintCache: preflightOwnerHintMetrics{
			Supplied: cache.Supplied, Eligible: cache.Eligible, Verified: cache.Verified, Stale: cache.Stale,
			AuthorizedHintQueryCount:   cache.AuthorizedHintQueryCount,
			AuthorizedHintFailureCount: cache.AuthorizedHintFailureCount, OfficialVideoQueryCount: cache.OfficialVideoQueryCount,
			Loaded: cache.Loaded, LoadedFromCache: cache.LoadedFromCache,
			LoadedFromLinkMetadata: cache.LoadedFromLinkMetadata, Stored: cache.Stored,
		},
		LinkMetadata: preflightLinkMetadata{Provider: value.LinkMetadata.Provider, Enabled: value.LinkMetadata.Enabled},
	}
}

func cloneCounts(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func mapQianchuanPreflightError(err error) toolFailure {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Qianchuan preflight was interrupted", Retryable: true, Details: map[string]any{}}
	case errors.Is(err, os.ErrPermission):
		return toolFailure{Code: "LOCAL_ACCESS_DENIED", Message: "local Ocean Watch state is not accessible", Details: map[string]any{}}
	case errors.Is(err, os.ErrNotExist):
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local Ocean Watch configuration is unavailable", Details: map[string]any{}}
	}
	var domainError *domain.Error
	if errors.As(err, &domainError) {
		switch domainError.Code {
		case "authorization_not_found", "authorization_ambiguous", "authorization_pending_sync", "reauthorization_required":
			return toolFailure{Code: "AUTHORIZATION_UNAVAILABLE", Message: "Qianchuan authorization is unavailable", Details: map[string]any{}}
		}
	}
	var linkError *domain.WorkLinkError
	if errors.As(err, &linkError) {
		switch linkError.Code {
		case "f2_runtime_unavailable":
			return toolFailure{Code: "F2_RUNTIME_UNAVAILABLE", Message: "F2 work metadata runtime is unavailable", Details: map[string]any{}}
		case "f2_cli_timeout", "f2_work_timeout":
			return toolFailure{Code: "F2_QUERY_TIMEOUT", Message: "F2 work metadata query timed out", Retryable: true, Details: map[string]any{}}
		case "f2_response_too_large", "invalid_f2_response", "f2_cli_failed":
			return toolFailure{Code: "F2_QUERY_FAILED", Message: "F2 work metadata query failed", Retryable: true, Details: map[string]any{}}
		}
	}
	var stageError *applicationqianchuan.PreflightStageError
	if errors.As(err, &stageError) {
		var inventoryError *applicationqianchuan.PlanInventoryError
		if errors.As(err, &inventoryError) {
			switch inventoryError.Reason {
			case "official_query":
				return toolFailure{Code: "PLAN_INVENTORY_QUERY_FAILED", Message: "current Qianchuan plan inventory query failed", Retryable: true, Details: map[string]any{}}
			case "pagination_changed", "count_mismatch", "duplicate_plan":
				return toolFailure{Code: "PLAN_INVENTORY_CHANGED", Message: "current Qianchuan plan inventory changed during pagination", Retryable: true, Details: map[string]any{}}
			case "pagination_metadata", "invalid_plan":
				return toolFailure{Code: "PLAN_INVENTORY_INVALID", Message: "current Qianchuan plan inventory response is invalid", Retryable: true, Details: map[string]any{}}
			case "safety_cap":
				return toolFailure{Code: "PLAN_INVENTORY_TOO_LARGE", Message: "current Qianchuan plan inventory exceeds the safety limit", Details: map[string]any{}}
			}
		}
		switch stageError.Stage {
		case "configuration":
			return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local Ocean Watch configuration is unavailable", Details: map[string]any{}}
		case "template":
			return toolFailure{Code: "TEMPLATE_NOT_FOUND", Message: "Qianchuan product template is unavailable", Details: map[string]any{}}
		case "work_metadata":
			return toolFailure{Code: "WORK_METADATA_FAILED", Message: "Douyin work metadata resolution failed", Retryable: true, Details: map[string]any{}}
		case "authorization":
			return toolFailure{Code: "AUTHORIZATION_UNAVAILABLE", Message: "Qianchuan authorization is unavailable", Details: map[string]any{}}
		case "work_verification":
			return toolFailure{Code: "WORK_VERIFICATION_FAILED", Message: "official work verification failed", Retryable: true, Details: map[string]any{}}
		case "plan_inventory":
			return toolFailure{Code: "PLAN_INVENTORY_FAILED", Message: "current Qianchuan plan inventory query failed", Retryable: true, Details: map[string]any{}}
		case "plan_reconciliation":
			return toolFailure{Code: "PLAN_RECONCILIATION_FAILED", Message: "Qianchuan plan reconciliation failed", Retryable: true, Details: map[string]any{}}
		case "local_coordination":
			return toolFailure{Code: "LOCAL_COORDINATION_FAILED", Message: "local Qianchuan preflight coordination failed", Retryable: true, Details: map[string]any{}}
		case "snapshot":
			return toolFailure{Code: "PREFLIGHT_SNAPSHOT_FAILED", Message: "preflight snapshot could not be saved", Retryable: true, Details: map[string]any{}}
		}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "template") &&
		(strings.Contains(message, "not found") || strings.Contains(message, "not active")) {
		return toolFailure{Code: "TEMPLATE_NOT_FOUND", Message: "Qianchuan product template is unavailable", Details: map[string]any{}}
	}
	if strings.Contains(message, "token") || strings.Contains(message, "authorization") || strings.Contains(message, "credential") {
		return toolFailure{Code: "AUTHORIZATION_UNAVAILABLE", Message: "Qianchuan authorization is unavailable", Details: map[string]any{}}
	}
	if strings.Contains(message, "config") {
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local Ocean Watch configuration is unavailable", Details: map[string]any{}}
	}
	return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Qianchuan preflight could not complete", Retryable: true, Details: map[string]any{}}
}

func mapQianchuanSnapshotError(err error) toolFailure {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return toolFailure{Code: "PREFLIGHT_READ_FAILED", Message: "preflight snapshot read was interrupted", Retryable: true, Details: map[string]any{}}
	case errors.Is(err, applicationqianchuan.ErrBatchPreflightNotFound), errors.Is(err, os.ErrNotExist):
		return toolFailure{Code: "PREFLIGHT_NOT_FOUND", Message: "preflight was not found", Details: map[string]any{}}
	case errors.Is(err, applicationqianchuan.ErrBatchPreflightExpired):
		return toolFailure{Code: "PREFLIGHT_EXPIRED", Message: "preflight expired; run preflight again", Details: map[string]any{}}
	case errors.Is(err, applicationqianchuan.ErrBatchPreflightInvalid):
		return toolFailure{Code: "PREFLIGHT_INVALID", Message: "preflight is invalid", Details: map[string]any{}}
	case errors.Is(err, os.ErrPermission):
		return toolFailure{Code: "LOCAL_ACCESS_DENIED", Message: "local Ocean Watch state is not accessible", Details: map[string]any{}}
	}
	return toolFailure{Code: "PREFLIGHT_READ_FAILED", Message: "preflight snapshot could not be read", Retryable: true, Details: map[string]any{}}
}
