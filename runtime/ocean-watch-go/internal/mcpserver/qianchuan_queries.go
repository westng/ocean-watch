package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

var queryDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type managedAccountsInput struct {
	Channel         string `json:"channel"`
	IncludeDisabled bool   `json:"include_disabled"`
}

type managedAccountsOutput struct {
	OK           bool                 `json:"ok"`
	RequestID    string               `json:"request_id"`
	Source       string               `json:"source"`
	TotalCount   int                  `json:"total_count"`
	Accounts     []managedAccountItem `json:"accounts"`
	Presentation queryPresentation    `json:"presentation"`
}

type managedAccountItem struct {
	Channel      string `json:"channel"`
	Name         string `json:"name"`
	AdvertiserID string `json:"advertiser_id"`
	Enabled      bool   `json:"enabled"`
}

type queryPresentation struct {
	Required         bool   `json:"required"`
	RenderedMarkdown string `json:"rendered_markdown"`
}

type qianchuanAuthorizationInput struct {
	AdvertiserID string `json:"advertiser_id"`
}

type qianchuanAuthorizationOutput struct {
	OK             bool                         `json:"ok"`
	RequestID      string                       `json:"request_id"`
	Source         string                       `json:"source"`
	Status         qianchuanAuthorizationStatus `json:"status"`
	Mappings       []qianchuanAdvertiserMapping `json:"mappings"`
	Authorizations []qianchuanAuthorizationItem `json:"authorizations"`
}

type qianchuanAuthorizationStatus struct {
	HasAppID                  bool    `json:"has_app_id"`
	HasSecret                 bool    `json:"has_secret"`
	AuthorizationCount        int     `json:"authorization_count"`
	AuthorizedAccountCount    int     `json:"authorized_account_count"`
	AuthorizedAdvertiserCount int     `json:"authorized_advertiser_count"`
	Generation                int     `json:"generation"`
	AdvertiserID              *string `json:"advertiser_id"`
	AdvertiserIDAuthorized    *bool   `json:"advertiser_id_authorized"`
}

type qianchuanAdvertiserMapping struct {
	AdvertiserID     string   `json:"advertiser_id"`
	AuthorizationIDs []string `json:"authorization_ids"`
	Ambiguous        bool     `json:"ambiguous"`
}

type qianchuanAuthorizationItem struct {
	AuthorizationID       string   `json:"authorization_id"`
	TokenRevision         int      `json:"token_revision"`
	HasAccessToken        bool     `json:"has_access_token"`
	HasRefreshToken       bool     `json:"has_refresh_token"`
	AccessTokenExpiresAt  string   `json:"access_token_expires_at"`
	RefreshTokenExpiresAt string   `json:"refresh_token_expires_at"`
	PendingAccountSync    bool     `json:"pending_account_sync"`
	AdvertiserIDs         []string `json:"advertiser_ids"`
}

type qianchuanProductsInput struct {
	AdvertiserID  string   `json:"advertiser_id"`
	AuthAccountID string   `json:"auth_account_id"`
	ProductIDs    []string `json:"product_ids"`
	ProductName   string   `json:"product_name"`
	Limit         int      `json:"limit"`
}

type qianchuanProductsOutput struct {
	OK             bool                   `json:"ok"`
	RequestID      string                 `json:"request_id"`
	Source         string                 `json:"source"`
	AdvertiserID   string                 `json:"advertiser_id"`
	TotalCount     int                    `json:"total_count"`
	DisplayedCount int                    `json:"displayed_count"`
	Truncated      bool                   `json:"truncated"`
	Items          []qianchuanProductItem `json:"items"`
}

type qianchuanProductItem struct {
	ProductID   string `json:"product_id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	ChannelID   string `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	SellNumber  *int64 `json:"sell_number"`
	StockNumber *int64 `json:"stock_number"`
	AuditTime   string `json:"audit_time"`
}

type qianchuanPlansInput struct {
	AdvertiserID  string `json:"advertiser_id"`
	AuthAccountID string `json:"auth_account_id"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Status        string `json:"status"`
	Limit         int    `json:"limit"`
}

type qianchuanPlansOutput struct {
	OK             bool                `json:"ok"`
	RequestID      string              `json:"request_id"`
	Source         string              `json:"source"`
	AdvertiserID   string              `json:"advertiser_id"`
	DateRange      queryDateRange      `json:"date_range"`
	Status         string              `json:"status"`
	TotalCount     int                 `json:"total_count"`
	DisplayedCount int                 `json:"displayed_count"`
	Truncated      bool                `json:"truncated"`
	Items          []qianchuanPlanItem `json:"items"`
}

type queryDateRange struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type qianchuanPlanItem struct {
	AdID          string          `json:"ad_id"`
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	OptStatus     string          `json:"opt_status"`
	CreateTime    string          `json:"create_time"`
	MarketingGoal string          `json:"marketing_goal"`
	CreatorIDs    []string        `json:"creator_ids"`
	Budget        *domain.Decimal `json:"budget"`
	SmartBidType  string          `json:"smart_bid_type"`
	ROI2Goal      *domain.Decimal `json:"roi2_goal"`
}

type qianchuanPlanInput struct {
	AdvertiserID     string `json:"advertiser_id"`
	AuthAccountID    string `json:"auth_account_id"`
	AdID             string `json:"ad_id"`
	IncludeMaterials bool   `json:"include_materials"`
}

type qianchuanPlanOutput struct {
	OK            bool                    `json:"ok"`
	RequestID     string                  `json:"request_id"`
	Source        string                  `json:"source"`
	AdvertiserID  string                  `json:"advertiser_id"`
	Plan          qianchuanPlanDetailItem `json:"plan"`
	Materials     []qianchuanMaterialItem `json:"materials"`
	MaterialCount int                     `json:"material_count"`
}

type qianchuanPlanDetailItem struct {
	AdID          string                 `json:"ad_id"`
	Name          string                 `json:"name"`
	Status        string                 `json:"status"`
	OptStatus     string                 `json:"opt_status"`
	CreateTime    string                 `json:"create_time"`
	ModifyTime    string                 `json:"modify_time"`
	MarketingGoal string                 `json:"marketing_goal"`
	AwemeID       string                 `json:"aweme_id"`
	Creators      []qianchuanCreatorItem `json:"creators"`
	Products      []qianchuanPlanProduct `json:"products"`
	Budget        *domain.Decimal        `json:"budget"`
	BudgetMode    string                 `json:"budget_mode"`
	SmartBidType  string                 `json:"smart_bid_type"`
	ROI2Goal      *domain.Decimal        `json:"roi2_goal"`
}

type qianchuanCreatorItem struct {
	AwemeID   string `json:"aweme_id"`
	VisibleID string `json:"visible_id"`
	Name      string `json:"name"`
}

type qianchuanPlanProduct struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	ChannelID   string `json:"channel_id"`
	ChannelType string `json:"channel_type"`
}

type qianchuanMaterialItem struct {
	MaterialID         string   `json:"material_id"`
	AwemeItemID        string   `json:"aweme_item_id"`
	VideoID            string   `json:"video_id"`
	Title              string   `json:"title"`
	MaterialType       string   `json:"material_type"`
	MaterialSelectType string   `json:"material_select_type"`
	MaterialStatus     string   `json:"material_status"`
	AuditStatus        string   `json:"audit_status"`
	Duration           *int64   `json:"duration"`
	Deleted            *bool    `json:"deleted"`
	AwemeIDs           []string `json:"aweme_ids"`
	ProductIDs         []string `json:"product_ids"`
}

type qianchuanAccountReportInput struct {
	AdvertiserID  string `json:"advertiser_id"`
	AuthAccountID string `json:"auth_account_id"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Scope         string `json:"scope"`
}

type qianchuanAccountReportOutput struct {
	OK           bool                    `json:"ok"`
	RequestID    string                  `json:"request_id"`
	Source       string                  `json:"source"`
	AdvertiserID string                  `json:"advertiser_id"`
	Scope        string                  `json:"scope"`
	DateRange    queryDateRange          `json:"date_range"`
	Metrics      qianchuanAccountMetrics `json:"metrics"`
}

type qianchuanAccountMetrics struct {
	StatCost                         any `json:"stat_cost"`
	StatCostForROI2                  any `json:"stat_cost_for_roi2"`
	StatCostForOverallROI2           any `json:"stat_cost_for_overall_roi2"`
	TotalPayOrderCountForROI2        any `json:"total_pay_order_count_for_roi2"`
	TotalPayOrderGMVForROI2          any `json:"total_pay_order_gmv_include_coupon_for_roi2"`
	TotalPayOrderROI2                any `json:"total_prepay_and_pay_order_roi2"`
	TotalSettledAmountForROI2OneHour any `json:"total_order_settle_amount_for_roi2_1h"`
	TotalSettledROI2OneHour          any `json:"total_prepay_and_pay_settle_roi2_1h"`
	TotalSettledOverallROI2OneHour   any `json:"total_prepay_and_pay_settle_overall_roi2_1h"`
}

type qianchuanPlanReportInput struct {
	AdvertiserID  string `json:"advertiser_id"`
	AuthAccountID string `json:"auth_account_id"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Status        string `json:"status"`
	Limit         int    `json:"limit"`
}

type qianchuanPlanReportOutput struct {
	OK             bool                           `json:"ok"`
	RequestID      string                         `json:"request_id"`
	Source         string                         `json:"source"`
	AdvertiserID   string                         `json:"advertiser_id"`
	DateRange      queryDateRange                 `json:"date_range"`
	AmountUnit     string                         `json:"amount_unit"`
	Summary        applicationreports.PlanSummary `json:"summary"`
	DisplayedCount int                            `json:"displayed_count"`
	TotalRowCount  int                            `json:"total_row_count"`
	Truncated      bool                           `json:"truncated"`
	Presentation   queryPresentation              `json:"presentation"`
	Details        []qianchuanPlanReportDetail    `json:"details"`
}

type qianchuanPlanReportDetail struct {
	AdID                string `json:"ad_id"`
	Name                string `json:"name"`
	BudgetModeLabel     string `json:"budget_mode_label"`
	CostGuaranteeResult string `json:"cost_guarantee_result"`
	CostGuaranteeReason string `json:"cost_guarantee_reason"`
}

func (runtime Runtime) listManagedAccounts(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := managedAccountsInput{Channel: "all"}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		input.Channel != "all" && input.Channel != "marketing" && input.Channel != "qianchuan" {
		return runtime.failureResult(started, requestID, "list_managed_accounts", invalidArgumentFailure()), nil
	}
	if runtime.ManagedAccounts == nil {
		return runtime.failureResult(started, requestID, "list_managed_accounts", internalFailure()), nil
	}
	book, err := runtime.ManagedAccounts.Read(ctx)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_managed_accounts", mapLocalQueryError(err)), nil
	}
	var selected *domain.Channel
	if input.Channel != "all" {
		channel, parseErr := domain.ParseChannel(input.Channel)
		if parseErr != nil {
			return runtime.failureResult(started, requestID, "list_managed_accounts", invalidArgumentFailure()), nil
		}
		selected = &channel
	}
	accounts := book.List(selected, !input.IncludeDisabled)
	items := make([]managedAccountItem, len(accounts))
	for index, account := range accounts {
		items[index] = managedAccountItem{
			Channel: string(account.Channel), Name: account.Name,
			AdvertiserID: account.AdvertiserID, Enabled: account.Enabled,
		}
	}
	presentation := domain.ManagedAccountPresentation(accounts, input.IncludeDisabled)
	return runtime.successResult(started, requestID, "list_managed_accounts", managedAccountsOutput{
		OK: true, RequestID: requestID, Source: "local_state", TotalCount: len(items), Accounts: items,
		Presentation: queryPresentation{Required: presentation.Required, RenderedMarkdown: presentation.RenderedMarkdown},
	}), nil
}

func (runtime Runtime) getQianchuanAuthorization(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	var input qianchuanAuthorizationInput
	if err := decodeStrict(request.Params.Arguments, &input); err != nil || !optionalDecimalID(input.AdvertiserID) {
		return runtime.failureResult(started, requestID, "get_qianchuan_authorization", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanAuth == nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_authorization", internalFailure()), nil
	}
	result, err := runtime.QianchuanAuth.Inspect(ctx, authapplication.StatusQuery{
		Channel: "qianchuan", AdvertiserID: input.AdvertiserID,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_authorization", mapAuthorizationQueryError(err)), nil
	}
	var advertiserID *string
	if input.AdvertiserID != "" {
		value := input.AdvertiserID
		advertiserID = &value
	}
	items := make([]qianchuanAuthorizationItem, 0, len(result.Mappings.Authorizations))
	statusByID := map[string]authapplication.AuthorizationStatus{}
	for _, row := range result.Status.Authorizations {
		statusByID[row.AuthorizationID] = row
	}
	for _, row := range result.Mappings.Authorizations {
		status := statusByID[row.AuthorizationID]
		items = append(items, qianchuanAuthorizationItem{
			AuthorizationID: row.AuthorizationID, TokenRevision: row.TokenRevision,
			HasAccessToken: row.HasAccessToken, HasRefreshToken: row.HasRefreshToken,
			AccessTokenExpiresAt:  status.AccessTokenExpiresAt,
			RefreshTokenExpiresAt: status.RefreshTokenExpiresAt,
			PendingAccountSync:    row.PendingAccountSync,
			AdvertiserIDs:         append([]string(nil), row.AdvertiserIDs...),
		})
	}
	mappings := make([]qianchuanAdvertiserMapping, len(result.Mappings.Mappings))
	for index, row := range result.Mappings.Mappings {
		mappings[index] = qianchuanAdvertiserMapping{
			AdvertiserID:     row.AdvertiserID,
			AuthorizationIDs: append([]string(nil), row.AuthorizationIDs...), Ambiguous: row.Ambiguous,
		}
	}
	return runtime.successResult(started, requestID, "get_qianchuan_authorization", qianchuanAuthorizationOutput{
		OK: true, RequestID: requestID, Source: "local_state",
		Status: qianchuanAuthorizationStatus{
			HasAppID: result.Status.HasAppID, HasSecret: result.Status.HasSecret,
			AuthorizationCount:        result.Status.AuthorizationCount,
			AuthorizedAccountCount:    result.Status.AuthorizedAccountCount,
			AuthorizedAdvertiserCount: result.Status.AuthorizedAdvertiserCount,
			Generation:                result.Status.Generation, AdvertiserID: advertiserID,
			AdvertiserIDAuthorized: result.Status.AdvertiserIDAuthorized,
		},
		Mappings: mappings, Authorizations: items,
	}), nil
}

func (runtime Runtime) searchQianchuanProducts(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := qianchuanProductsInput{Limit: 50}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		len(input.ProductIDs) > 30 || !allDecimalIDs(input.ProductIDs) || input.Limit < 1 || input.Limit > 100 ||
		len([]rune(strings.TrimSpace(input.ProductName))) > 100 {
		return runtime.failureResult(started, requestID, "search_qianchuan_products", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanReads == nil {
		return runtime.failureResult(started, requestID, "search_qianchuan_products", internalFailure()), nil
	}
	result, err := runtime.QianchuanReads.QueryProducts(ctx, applicationqianchuan.ProductQuery{
		CredentialScope: applicationqianchuan.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		ProductIDs: append([]string(nil), input.ProductIDs...), ProductName: strings.TrimSpace(input.ProductName),
	}, "qianchuan_product_list")
	if err != nil {
		return runtime.failureResult(started, requestID, "search_qianchuan_products", mapOfficialQueryError(err)), nil
	}
	limit := input.Limit
	if limit > len(result.Products) {
		limit = len(result.Products)
	}
	items := make([]qianchuanProductItem, limit)
	for index, product := range result.Products[:limit] {
		items[index] = qianchuanProductItem{
			ProductID: product.ProductID, Name: product.Name, Category: product.CategoryName,
			ChannelID: product.ChannelID, ChannelType: product.ChannelType,
			SellNumber: product.SellNumber, StockNumber: product.StockNumber, AuditTime: product.AuditTime,
		}
	}
	return runtime.successResult(started, requestID, "search_qianchuan_products", qianchuanProductsOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: result.AdvertiserID,
		TotalCount: len(result.Products), DisplayedCount: len(items), Truncated: len(items) < len(result.Products), Items: items,
	}), nil
}

func (runtime Runtime) listQianchuanPlans(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := qianchuanPlansInput{Status: "ALL", Limit: 50}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		!validOptionalDateRange(input.StartDate, input.EndDate) || !validPlanStatus(input.Status) ||
		input.Limit < 1 || input.Limit > 100 {
		return runtime.failureResult(started, requestID, "list_qianchuan_plans", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanReads == nil {
		return runtime.failureResult(started, requestID, "list_qianchuan_plans", internalFailure()), nil
	}
	result, err := runtime.QianchuanReads.ListPlans(ctx, applicationqianchuan.PlanListQuery{
		CredentialScope: applicationqianchuan.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		StartDate: input.StartDate, EndDate: input.EndDate, Status: input.Status,
		Top: input.Limit, Full: false,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "list_qianchuan_plans", mapOfficialQueryError(err)), nil
	}
	items, err := compactPlanItems(result.Plans)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_qianchuan_plans", internalFailure()), nil
	}
	return runtime.successResult(started, requestID, "list_qianchuan_plans", qianchuanPlansOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: result.AdvertiserID,
		DateRange: queryDateRange{StartDate: result.DataPeriod.StartDate, EndDate: result.DataPeriod.EndDate},
		Status:    input.Status, TotalCount: result.PlanCount, DisplayedCount: len(items),
		Truncated: len(items) < result.PlanCount, Items: items,
	}), nil
}

func (runtime Runtime) getQianchuanPlan(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	var input qianchuanPlanInput
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) || !requiredDecimalID(input.AdID) {
		return runtime.failureResult(started, requestID, "get_qianchuan_plan", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanReads == nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_plan", internalFailure()), nil
	}
	scope := applicationqianchuan.CredentialScope{AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID}
	result, err := runtime.QianchuanReads.ShowPlan(ctx, scope, input.AdID)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_qianchuan_plan", mapOfficialQueryError(err)), nil
	}
	materials := []qianchuanMaterialItem{}
	if input.IncludeMaterials {
		materialResult, materialErr := runtime.QianchuanReads.ListPlanMaterials(ctx, applicationqianchuan.PlanMaterialsQuery{
			CredentialScope: scope, AdID: input.AdID,
		})
		if materialErr != nil {
			return runtime.failureResult(started, requestID, "get_qianchuan_plan", mapOfficialQueryError(materialErr)), nil
		}
		materials = presentMaterials(materialResult.Materials)
	}
	return runtime.successResult(started, requestID, "get_qianchuan_plan", qianchuanPlanOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: result.AdvertiserID,
		Plan: presentPlanDetail(result.Plan), Materials: materials, MaterialCount: len(materials),
	}), nil
}

func (runtime Runtime) reportQianchuanAccount(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := qianchuanAccountReportInput{Scope: "overall"}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		!validOptionalDateRange(input.StartDate, input.EndDate) || input.Scope != "overall" && input.Scope != "uni" {
		return runtime.failureResult(started, requestID, "report_qianchuan_account", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanReports == nil {
		return runtime.failureResult(started, requestID, "report_qianchuan_account", internalFailure()), nil
	}
	fields := append([]string(nil), applicationreports.DefaultQianchuanAllPromotionFields...)
	query := applicationreports.QianchuanAggregateQuery{
		CredentialScope: applicationreports.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		StartDate: input.StartDate, EndDate: input.EndDate, Fields: fields,
	}
	var result applicationreports.QianchuanAggregateResult
	var err error
	if input.Scope == "uni" {
		query.Fields = append([]string(nil), applicationreports.DefaultQianchuanUniPromotionFields...)
		result, err = runtime.QianchuanReports.QianchuanUniPromotion(ctx, query)
	} else {
		result, err = runtime.QianchuanReports.QianchuanAllPromotion(ctx, query)
	}
	if err != nil {
		return runtime.failureResult(started, requestID, "report_qianchuan_account", mapOfficialQueryError(err)), nil
	}
	metrics, err := presentAccountMetrics(result.Data)
	if err != nil {
		return runtime.failureResult(started, requestID, "report_qianchuan_account", mapOfficialQueryError(err)), nil
	}
	return runtime.successResult(started, requestID, "report_qianchuan_account", qianchuanAccountReportOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: result.AdvertiserID,
		Scope: input.Scope, DateRange: queryDateRange{StartDate: result.DateRange.StartDate, EndDate: result.DateRange.EndDate},
		Metrics: metrics,
	}), nil
}

func (runtime Runtime) reportQianchuanPlans(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started, requestID := runtime.now(), runtime.requestID()
	input := qianchuanPlanReportInput{Status: "ALL", Limit: 10}
	if err := decodeStrict(request.Params.Arguments, &input); err != nil ||
		!requiredDecimalID(input.AdvertiserID) || !optionalDecimalID(input.AuthAccountID) ||
		!validOptionalDateRange(input.StartDate, input.EndDate) || !validPlanStatus(input.Status) ||
		input.Limit < 1 || input.Limit > 100 {
		return runtime.failureResult(started, requestID, "report_qianchuan_plans", invalidArgumentFailure()), nil
	}
	if runtime.QianchuanReports == nil {
		return runtime.failureResult(started, requestID, "report_qianchuan_plans", internalFailure()), nil
	}
	result, err := runtime.QianchuanReports.PlanReport(ctx, applicationreports.PlanQuery{
		CredentialScope: applicationreports.CredentialScope{
			AdvertiserID: input.AdvertiserID, AuthAccountID: input.AuthAccountID,
		},
		StartDate: input.StartDate, EndDate: input.EndDate, Status: input.Status, Top: input.Limit,
	})
	if err != nil {
		return runtime.failureResult(started, requestID, "report_qianchuan_plans", mapOfficialQueryError(err)), nil
	}
	details := make([]qianchuanPlanReportDetail, len(result.Rows))
	for index, row := range result.Rows {
		details[index] = qianchuanPlanReportDetail{
			AdID: safeString(row["ad_id"]), Name: safeString(row["name"]),
			BudgetModeLabel:     safeString(row["budget_mode_label"]),
			CostGuaranteeResult: safeString(row["cost_guarantee_result"]),
			CostGuaranteeReason: safeString(row["cost_guarantee_reason"]),
		}
	}
	return runtime.successResult(started, requestID, "report_qianchuan_plans", qianchuanPlanReportOutput{
		OK: true, RequestID: requestID, Source: "official_api", AdvertiserID: result.AdvertiserID,
		DateRange:  queryDateRange{StartDate: result.DateRange.StartDate, EndDate: result.DateRange.EndDate},
		AmountUnit: result.AmountUnit, Summary: result.Summary,
		DisplayedCount: result.DisplayedCount, TotalRowCount: result.TotalRowCount,
		Truncated:    result.DisplayedCount < result.TotalRowCount,
		Presentation: queryPresentation{Required: result.Presentation.Required, RenderedMarkdown: result.Presentation.RenderedMarkdown},
		Details:      details,
	}), nil
}

func compactPlanItems(value any) ([]qianchuanPlanItem, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var rows []applicationqianchuan.CompactPlan
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil, err
	}
	items := make([]qianchuanPlanItem, len(rows))
	for index, row := range rows {
		items[index] = qianchuanPlanItem{
			AdID: row.AdID, Name: row.Name, Status: row.Status, OptStatus: row.OptStatus,
			CreateTime: row.CreateTime, MarketingGoal: row.MarketingGoal,
			CreatorIDs: append([]string(nil), row.CreatorIDs...), Budget: row.Budget,
			SmartBidType: row.SmartBidType, ROI2Goal: row.ROI2Goal,
		}
	}
	return items, nil
}

func presentPlanDetail(row domainqianchuan.PlanDetail) qianchuanPlanDetailItem {
	creators := make([]qianchuanCreatorItem, len(row.Creators))
	for index, creator := range row.Creators {
		creators[index] = qianchuanCreatorItem{AwemeID: creator.AwemeID, VisibleID: creator.VisibleID, Name: creator.Name}
	}
	products := make([]qianchuanPlanProduct, len(row.Products))
	for index, product := range row.Products {
		products[index] = qianchuanPlanProduct{
			ProductID: product.ProductID, ProductName: product.ProductName,
			ChannelID: product.ChannelID, ChannelType: product.ChannelType,
		}
	}
	return qianchuanPlanDetailItem{
		AdID: row.AdID, Name: row.Name, Status: row.Status, OptStatus: row.OptStatus,
		CreateTime: row.CreateTime, ModifyTime: row.ModifyTime, MarketingGoal: row.MarketingGoal,
		AwemeID: row.AwemeID, Creators: creators, Products: products, Budget: row.Budget,
		BudgetMode: row.BudgetMode, SmartBidType: row.SmartBidType, ROI2Goal: row.ROI2Goal,
	}
}

func presentMaterials(rows []domainqianchuan.PlanMaterial) []qianchuanMaterialItem {
	items := make([]qianchuanMaterialItem, len(rows))
	for index, row := range rows {
		items[index] = qianchuanMaterialItem{
			MaterialID: row.MaterialID, AwemeItemID: row.AwemeItemID, VideoID: row.VideoID,
			Title: row.Title, MaterialType: row.MaterialType, MaterialSelectType: row.MaterialSelectType,
			MaterialStatus: row.MaterialStatus, AuditStatus: row.AuditStatus, Duration: row.Duration,
			Deleted: row.Deleted, AwemeIDs: append([]string(nil), row.AwemeIDs...),
			ProductIDs: append([]string(nil), row.ProductIDs...),
		}
	}
	return items
}

func presentAccountMetrics(values map[string]any) (qianchuanAccountMetrics, error) {
	fields := []string{
		"stat_cost", "stat_cost_for_roi2", "stat_cost_for_overall_roi2",
		"total_pay_order_count_for_roi2", "total_pay_order_gmv_include_coupon_for_roi2",
		"total_prepay_and_pay_order_roi2", "total_order_settle_amount_for_roi2_1h",
		"total_prepay_and_pay_settle_roi2_1h", "total_prepay_and_pay_settle_overall_roi2_1h",
	}
	safe := map[string]any{}
	for _, field := range fields {
		value := values[field]
		if value == nil {
			safe[field] = nil
			continue
		}
		switch value.(type) {
		case json.Number, float64, float32, int, int32, int64, uint, uint32, uint64, string:
			safe[field] = value
		default:
			return qianchuanAccountMetrics{}, fmt.Errorf("Qianchuan account report returned an invalid metric %s", field)
		}
	}
	return qianchuanAccountMetrics{
		StatCost: safe["stat_cost"], StatCostForROI2: safe["stat_cost_for_roi2"],
		StatCostForOverallROI2:           safe["stat_cost_for_overall_roi2"],
		TotalPayOrderCountForROI2:        safe["total_pay_order_count_for_roi2"],
		TotalPayOrderGMVForROI2:          safe["total_pay_order_gmv_include_coupon_for_roi2"],
		TotalPayOrderROI2:                safe["total_prepay_and_pay_order_roi2"],
		TotalSettledAmountForROI2OneHour: safe["total_order_settle_amount_for_roi2_1h"],
		TotalSettledROI2OneHour:          safe["total_prepay_and_pay_settle_roi2_1h"],
		TotalSettledOverallROI2OneHour:   safe["total_prepay_and_pay_settle_overall_roi2_1h"],
	}, nil
}

func requiredDecimalID(value string) bool {
	return domain.ValidateDecimalID(value, "id") == nil
}

func optionalDecimalID(value string) bool {
	return value == "" || requiredDecimalID(value)
}

func allDecimalIDs(values []string) bool {
	for _, value := range values {
		if !requiredDecimalID(value) {
			return false
		}
	}
	return true
}

func validOptionalDateRange(start, end string) bool {
	if start == "" && end == "" {
		return true
	}
	if start == "" || end == "" || !validDate(start) || !validDate(end) {
		return false
	}
	return start <= end
}

func validDate(value string) bool {
	if !queryDatePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func validPlanStatus(value string) bool {
	if value == "" {
		return true
	}
	for _, status := range []string{
		"ALL", "ALL_INCLUDE_DELETED", "DELIVERY_OK", "DISABLE", "AUDIT", "DELETED",
		"SYSTEM_DISABLE", "OFFLINE_AUDIT", "OFFLINE_BALANCE", "OFFLINE_BUDGET", "TIME_DONE", "TIME_NO_REACH",
	} {
		if value == status {
			return true
		}
	}
	return false
}

func safeString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapLocalQueryError(err error) toolFailure {
	switch {
	case errors.Is(err, os.ErrPermission):
		return toolFailure{Code: "LOCAL_ACCESS_DENIED", Message: "local Ocean Watch state is not accessible", Details: map[string]any{}}
	case errors.Is(err, os.ErrNotExist):
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local Ocean Watch configuration is unavailable", Details: map[string]any{}}
	case errors.Is(err, filesystem.ErrManagedConfigInvalid):
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local Ocean Watch configuration is unavailable", Details: map[string]any{}}
	default:
		return internalFailure()
	}
}

func mapAuthorizationQueryError(err error) toolFailure {
	local := mapLocalQueryError(err)
	if local.Code != "INTERNAL_ERROR" {
		return local
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) && domainErr.Code == "authorization_not_found" {
		return toolFailure{Code: "AUTHORIZATION_NOT_FOUND", Message: "Qianchuan authorization was not found", Details: map[string]any{}}
	}
	return internalFailure()
}

func mapOfficialQueryError(err error) toolFailure {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Qianchuan query was interrupted", Retryable: true, Details: map[string]any{}}
	}
	local := mapLocalQueryError(err)
	if local.Code != "INTERNAL_ERROR" {
		return local
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		switch domainErr.Code {
		case "authorization_not_found":
			return toolFailure{Code: "AUTHORIZATION_NOT_FOUND", Message: "Qianchuan authorization was not found", Details: map[string]any{}}
		case "authorization_ambiguous":
			return toolFailure{Code: "AUTHORIZATION_AMBIGUOUS", Message: "Qianchuan advertiser maps to multiple authorizations", Details: map[string]any{}}
		case "reauthorization_required", "authorization_pending_sync":
			return toolFailure{Code: "REAUTHORIZATION_REQUIRED", Message: "Qianchuan authorization must be renewed", Details: map[string]any{}}
		}
	}
	var envelope *oceanengine.EnvelopeError
	if errors.As(err, &envelope) && envelope.Code == 40103 {
		return toolFailure{Code: "REAUTHORIZATION_REQUIRED", Message: "Qianchuan authorization must be renewed", Details: map[string]any{}}
	}
	return toolFailure{Code: "UPSTREAM_QUERY_FAILED", Message: "Qianchuan query could not complete", Retryable: true, Details: map[string]any{}}
}
