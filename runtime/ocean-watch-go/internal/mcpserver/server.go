package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/credentials"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
	applicationqianchuanread "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/bootstrap"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/runtimeupdate"
)

type QianchuanPreflightService interface {
	BatchWorks(context.Context, applicationqianchuan.BatchWorksCommand) (applicationqianchuan.BatchCommandResult, error)
	GetBatchPreflight(context.Context, string) (applicationqianchuan.BatchPreflightSummary, error)
}

type ManagedAccountReader interface {
	Read(context.Context) (domain.AccountBook, error)
}

type QianchuanAuthorizationReader interface {
	Inspect(context.Context, authapplication.StatusQuery) (authapplication.InspectionResult, error)
}

type MarketingAuthorizationReader interface {
	Inspect(context.Context, authapplication.StatusQuery) (authapplication.InspectionResult, error)
}

type MarketingMaterialService interface {
	QueryVideos(context.Context, applicationmaterials.VideoQuery) (applicationmaterials.VideoResult, error)
	QueryCreator(context.Context, applicationmaterials.CreatorQuery) (applicationmaterials.CreatorResult, error)
}

type MarketingReportService interface {
	Plans(context.Context, applicationreports.MarketingPlanQuery) (applicationreports.MarketingPlanResult, error)
	Materials(context.Context, applicationreports.MarketingMaterialQuery) (applicationreports.MarketingMaterialResult, error)
}

type QianchuanReadService interface {
	QueryProducts(context.Context, applicationqianchuanread.ProductQuery, string) (applicationqianchuanread.ProductResult, error)
	ListPlans(context.Context, applicationqianchuanread.PlanListQuery) (applicationqianchuanread.PlanListResult, error)
	ShowPlan(context.Context, applicationqianchuanread.CredentialScope, string) (applicationqianchuanread.PlanDetailResult, error)
	ListPlanMaterials(context.Context, applicationqianchuanread.PlanMaterialsQuery) (applicationqianchuanread.PlanMaterialsResult, error)
}

type QianchuanReportService interface {
	PlanReport(context.Context, applicationreports.PlanQuery) (applicationreports.PlanResult, error)
	QianchuanAllPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error)
	QianchuanUniPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error)
}

type Runtime struct {
	Query               applicationtemplates.Query
	ManagedAccounts     ManagedAccountReader
	MarketingAuth       MarketingAuthorizationReader
	MarketingMaterials  MarketingMaterialService
	MarketingReports    MarketingReportService
	QianchuanAuth       QianchuanAuthorizationReader
	QianchuanReads      QianchuanReadService
	QianchuanReports    QianchuanReportService
	QianchuanPreflights QianchuanPreflightService
	LogWriter           io.Writer
	Now                 func() time.Time
	RequestID           func() string
	Forward             mcp.ToolHandler
}

type toolFailure struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
}

type errorEnvelope struct {
	OK        bool         `json:"ok"`
	RequestID string       `json:"request_id"`
	Error     errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}

type listInput struct {
	Channel string `json:"channel"`
	Limit   int    `json:"limit"`
	Cursor  string `json:"cursor"`
}

type listOutput struct {
	OK           bool           `json:"ok"`
	RequestID    string         `json:"request_id"`
	StateVersion string         `json:"state_version"`
	Source       string         `json:"source"`
	TotalCount   int            `json:"total_count"`
	Items        []templateItem `json:"items"`
	NextCursor   *string        `json:"next_cursor"`
}

type templateItem struct {
	TemplateID           string  `json:"template_id"`
	Channel              string  `json:"channel"`
	TemplateKind         string  `json:"template_kind"`
	Name                 string  `json:"name"`
	Status               *string `json:"status"`
	AdvertiserID         *string `json:"advertiser_id"`
	ReadyForPlanCreation bool    `json:"ready_for_plan_creation"`
}

type getInput struct {
	Channel    string `json:"channel"`
	TemplateID string `json:"template_id"`
}

type getOutput struct {
	OK           bool           `json:"ok"`
	RequestID    string         `json:"request_id"`
	StateVersion string         `json:"state_version"`
	Source       string         `json:"source"`
	Template     templateDetail `json:"template"`
}

type templateDetail struct {
	TemplateID            string   `json:"template_id"`
	Channel               string   `json:"channel"`
	TemplateKind          string   `json:"template_kind"`
	Name                  string   `json:"name"`
	Status                *string  `json:"status"`
	ReadyForPlanCreation  bool     `json:"ready_for_plan_creation"`
	AdvertiserID          *string  `json:"advertiser_id"`
	ProductID             *string  `json:"product_id"`
	ProductIDs            []string `json:"product_ids"`
	ProductName           *string  `json:"product_name"`
	CreatorName           *string  `json:"creator_name"`
	AwemeID               *string  `json:"aweme_id"`
	MaterialSourceType    *string  `json:"material_source_type"`
	DailyBudget           *float64 `json:"daily_budget"`
	ROIGoal               *float64 `json:"roi_goal"`
	SmartBidType          *string  `json:"smart_bid_type"`
	ProjectNameTemplate   *string  `json:"project_name_template"`
	PromotionNameTemplate *string  `json:"promotion_name_template"`
	ValidationIssues      []string `json:"validation_issues"`
}

type cursorPayload struct {
	Version      int    `json:"v"`
	Channel      string `json:"channel"`
	StateVersion string `json:"state_version"`
	Offset       int    `json:"offset"`
}

func IsServeCommand(args []string) bool {
	return len(args) == 3 && args[0] == "mcp" && args[1] == "serve" && args[2] == "--stdio"
}

func RunManaged(ctx context.Context, version string) error {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	runtimeRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Clean(executable))))
	if hostRoot := strings.TrimSpace(os.Getenv("PWD")); hostRoot != "" {
		manager := runtimeupdate.Manager{
			CodexRoot: filesystem.CodexHome(os.Getenv, userHome), PluginRoot: hostRoot,
		}
		if err := manager.PreserveInstalledHostRoot(ctx); err != nil {
			return err
		}
	}
	store, err := filesystem.NewManagedConfigStore(os.Getenv, userHome)
	if err != nil {
		return err
	}
	if err := retainManagedEnvironment(os.Getenv, os.Clearenv, os.Setenv); err != nil {
		return err
	}
	stateRoot := filepath.Join(store.Root, "state")
	credentialStore := credentials.Store{Root: store.Root, Getenv: os.Getenv}
	qianchuanRuntime, err := bootstrap.NewQianchuanRuntime(bootstrap.QianchuanOptions{
		Config: store, StateRoot: stateRoot,
		CredentialStore: credentialStore,
		PythonResolver:  python.Resolver{Getenv: os.Getenv}, PluginRoot: filesystem.ResolvePluginRoot(runtimeRoot),
		OnlineRead: true, BatchReadConcurrency: 10,
	})
	if err != nil {
		return err
	}
	marketingRuntime, err := bootstrap.NewMarketingRuntime(bootstrap.MarketingOptions{
		StateRoot: stateRoot, CredentialStore: credentialStore,
	})
	if err != nil {
		return err
	}
	runtime := Runtime{
		Query:               applicationtemplates.Query{Store: store, VersionedStore: store},
		ManagedAccounts:     filesystem.AccountStore{Path: store.Path},
		MarketingAuth:       marketingRuntime.Auth,
		MarketingMaterials:  marketingRuntime.Materials,
		MarketingReports:    marketingRuntime.Reports,
		QianchuanAuth:       qianchuanRuntime.Auth,
		QianchuanReads:      qianchuanRuntime.Reads,
		QianchuanReports:    qianchuanRuntime.Reports,
		QianchuanPreflights: qianchuanRuntime.Preflights,
		LogWriter:           os.Stderr,
	}
	server := runtime.NewServer(version)
	err = server.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func retainManagedEnvironment(
	getenv func(string) string,
	clearenv func(),
	setenv func(string, string) error,
) error {
	names := []string{
		"LANG", "LC_ALL", "LC_CTYPE", "PATH",
		"OCEAN_WATCH_PYTHON", "OCEAN_WATCH_F2_DOUYIN_COOKIE",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
		"ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK",
	}
	if runtime.GOOS == "windows" {
		names = append(names, "SystemRoot", "WINDIR")
	}
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value := getenv(name); value != "" {
			values[name] = value
		}
	}
	clearenv()
	for _, name := range names {
		if value := values[name]; value != "" {
			if err := setenv(name, value); err != nil {
				return fmt.Errorf("retain MCP runtime environment: %w", err)
			}
		}
	}
	return nil
}

func (runtime Runtime) NewServer(version string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "ocean-watch", Version: version},
		&mcp.ServerOptions{
			Instructions: "Use Ocean Watch task tools for the current OS user's managed local state and explicitly scoped official reads.",
			Logger:       slog.New(slog.DiscardHandler),
			Capabilities: &mcp.ServerCapabilities{},
		},
	)
	readOnly, closedWorld := false, false
	annotations := &mcp.ToolAnnotations{
		DestructiveHint: &readOnly, IdempotentHint: true,
		OpenWorldHint: &closedWorld, ReadOnlyHint: true,
	}
	listAnnotations := *annotations
	listAnnotations.Title = "列出投放模板"
	server.AddTool(&mcp.Tool{
		Name: "list_templates", Annotations: &listAnnotations,
		Description: "当用户想查找、浏览或选择本地巨量营销或巨量千川投放模板时调用。返回可供 get_template 使用的稳定字符串 ID；不读取官方账户数据。",
		InputSchema: json.RawMessage(listInputSchema), OutputSchema: json.RawMessage(listOutputSchema),
	}, runtime.handler(runtime.listTemplates))
	getAnnotations := *annotations
	getAnnotations.Title = "查看投放模板详情"
	server.AddTool(&mcp.Tool{
		Name: "get_template", Annotations: &getAnnotations,
		Description: "当用户已经给出或在当前会话中选中了一个本地投放模板，并需要查看安全详情或创建计划前的就绪状态时调用。必须使用渠道和精确模板 ID。",
		InputSchema: json.RawMessage(getInputSchema), OutputSchema: json.RawMessage(getOutputSchema),
	}, runtime.handler(runtime.getTemplate))
	capabilityAnnotations := *annotations
	capabilityAnnotations.Title = "查询 Ocean Watch 能力"
	server.AddTool(&mcp.Tool{
		Name: "get_capabilities", Annotations: &capabilityAnnotations,
		Description: "仅当用户目标未命中已知高频工具，或需要了解 Ocean Watch 当前完整能力时调用。只返回当前本地 Runtime 的命令、渠道和副作用等级，不读取业务数据、不执行操作。",
		InputSchema: json.RawMessage(capabilitiesInputSchema), OutputSchema: json.RawMessage(capabilitiesOutputSchema),
	}, runtime.handler(capabilitiesHandler(version)))
	preflightReadOnly, openWorld := false, true
	preflightAnnotations := &mcp.ToolAnnotations{
		Title: "预检千川作品计划", DestructiveHint: &readOnly, IdempotentHint: false,
		OpenWorldHint: &openWorld, ReadOnlyHint: preflightReadOnly,
	}
	server.AddTool(&mcp.Tool{
		Name: "preflight_qianchuan_works", Annotations: preflightAnnotations,
		Description: "当用户给出千川商品全域模板和抖音作品链接，需要校验授权、归属、商品匹配并决定新建或追加计划时调用。不会创建或修改计划，但会读取官方接口、必要时刷新授权并保存短期本地预检快照。",
		InputSchema: json.RawMessage(preflightInputSchema), OutputSchema: json.RawMessage(preflightOutputSchema),
	}, runtime.handler(withUnboundedRequestBudget(runtime.preflightQianchuanWorks)))
	getPreflightAnnotations := *annotations
	getPreflightAnnotations.Title = "查看千川预检快照"
	server.AddTool(&mcp.Tool{
		Name: "get_qianchuan_preflight", Annotations: &getPreflightAnnotations,
		Description: "当用户已经有精确的千川预检 ID，并需要确认其有效期、模板、商品和新建或追加决策时调用。只读本地快照，不刷新授权且不访问官方接口。",
		InputSchema: json.RawMessage(getPreflightInputSchema), OutputSchema: json.RawMessage(getPreflightOutputSchema),
	}, runtime.handler(runtime.getQianchuanPreflight))
	accountAnnotations := *annotations
	accountAnnotations.Title = "列出负责账户"
	server.AddTool(&mcp.Tool{
		Name: "list_managed_accounts", Annotations: &accountAnnotations,
		Description: "当用户询问自己负责、常用或日常投放范围内的账户时调用。只读取本地负责账户清单，不读取凭据、不刷新 Token，也不访问官方接口。",
		InputSchema: json.RawMessage(managedAccountsInputSchema), OutputSchema: json.RawMessage(managedAccountsOutputSchema),
	}, runtime.handler(runtime.listManagedAccounts))
	authAnnotations := *annotations
	authAnnotations.Title = "查看千川授权状态"
	server.AddTool(&mcp.Tool{
		Name: "get_qianchuan_authorization", Annotations: &authAnnotations,
		Description: "当用户需要确认本机千川应用、Token 存在状态或广告主到授权的映射时调用。只读取本地授权与凭据存在性，不刷新 Token、不访问官方接口，也不返回凭据值。",
		InputSchema: json.RawMessage(qianchuanAuthorizationInputSchema), OutputSchema: json.RawMessage(qianchuanAuthorizationOutputSchema),
	}, runtime.handler(runtime.getQianchuanAuthorization))
	officialReadAnnotations := &mcp.ToolAnnotations{
		DestructiveHint: &readOnly, IdempotentHint: false,
		OpenWorldHint: &openWorld, ReadOnlyHint: false,
	}
	productAnnotations := *officialReadAnnotations
	productAnnotations.Title = "搜索千川商品"
	server.AddTool(&mcp.Tool{
		Name: "search_qianchuan_products", Annotations: &productAnnotations,
		Description: "当用户需要查找千川可投商品、商品 ID、名称、类目、渠道、销量或库存时调用。读取官方接口，必要时会刷新本地 Token；不用于查询商品消耗或 ROI。",
		InputSchema: json.RawMessage(qianchuanProductsInputSchema), OutputSchema: json.RawMessage(qianchuanProductsOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.searchQianchuanProducts)))
	planListAnnotations := *officialReadAnnotations
	planListAnnotations.Title = "列出千川计划"
	server.AddTool(&mcp.Tool{
		Name: "list_qianchuan_plans", Annotations: &planListAnnotations,
		Description: "当用户需要按广告主、日期和状态列出千川商品全域计划时调用。读取官方计划列表，必要时会刷新本地 Token；不把列表统计字段当成报表金额。",
		InputSchema: json.RawMessage(qianchuanPlansInputSchema), OutputSchema: json.RawMessage(qianchuanPlansOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.listQianchuanPlans)))
	planDetailAnnotations := *officialReadAnnotations
	planDetailAnnotations.Title = "查看千川计划详情"
	server.AddTool(&mcp.Tool{
		Name: "get_qianchuan_plan", Annotations: &planDetailAnnotations,
		Description: "当用户给出精确千川计划 ID，需要查看计划设置、达人、商品及可选素材成员时调用。读取官方详情和可选素材接口，必要时会刷新本地 Token。",
		InputSchema: json.RawMessage(qianchuanPlanInputSchema), OutputSchema: json.RawMessage(qianchuanPlanOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.getQianchuanPlan)))
	accountReportAnnotations := *officialReadAnnotations
	accountReportAnnotations.Title = "查询千川账户报表"
	server.AddTool(&mcp.Tool{
		Name: "report_qianchuan_account", Annotations: &accountReportAnnotations,
		Description: "当用户需要一个千川广告主的账户汇总消耗、订单、GMV 或 ROI 时调用。scope=overall 包含乘方，scope=uni 仅全域；使用固定官方指标集。",
		InputSchema: json.RawMessage(qianchuanAccountReportInputSchema), OutputSchema: json.RawMessage(qianchuanAccountReportOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.reportQianchuanAccount)))
	planReportAnnotations := *officialReadAnnotations
	planReportAnnotations.Title = "查询千川计划报表"
	server.AddTool(&mcp.Tool{
		Name: "report_qianchuan_plans", Annotations: &planReportAnnotations,
		Description: "当用户需要千川商品全域计划级消耗、订单、GMV、ROI、预算和成本保障信息时调用。保留应用服务生成的完整 Markdown 表格。",
		InputSchema: json.RawMessage(qianchuanPlanReportInputSchema), OutputSchema: json.RawMessage(qianchuanPlanReportOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.reportQianchuanPlans)))
	marketingAuthAnnotations := *annotations
	marketingAuthAnnotations.Title = "查看营销授权状态"
	server.AddTool(&mcp.Tool{
		Name: "get_marketing_authorization", Annotations: &marketingAuthAnnotations,
		Description: "当用户需要确认本机巨量营销应用、Token 存在状态或广告主到授权的映射时调用。只读取本地授权与凭据存在性，不刷新 Token、不访问官方接口，也不返回凭据值。",
		InputSchema: json.RawMessage(marketingAuthorizationInputSchema), OutputSchema: json.RawMessage(marketingAuthorizationOutputSchema),
	}, runtime.handler(runtime.getMarketingAuthorization))
	marketingVideoAnnotations := *officialReadAnnotations
	marketingVideoAnnotations.Title = "搜索营销视频素材"
	server.AddTool(&mcp.Tool{
		Name: "search_marketing_videos", Annotations: &marketingVideoAnnotations,
		Description: "当用户需要按巨量营销广告主、素材标识、文件名或日期查找账户视频素材时调用。只查询账户素材库，不返回视频或封面 URL。",
		InputSchema: json.RawMessage(marketingVideosInputSchema), OutputSchema: json.RawMessage(marketingVideosOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.searchMarketingVideos)))
	marketingCreatorAnnotations := *officialReadAnnotations
	marketingCreatorAnnotations.Title = "搜索营销达人素材"
	server.AddTool(&mcp.Tool{
		Name: "search_marketing_creator_materials", Annotations: &marketingCreatorAnnotations,
		Description: "当用户需要查询巨量营销达人授权素材或一个达人主页素材时调用。返回授权和可用性白名单字段，不返回播放或封面 URL。",
		InputSchema: json.RawMessage(marketingCreatorInputSchema), OutputSchema: json.RawMessage(marketingCreatorOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.searchMarketingCreatorMaterials)))
	marketingMaterialReportAnnotations := *officialReadAnnotations
	marketingMaterialReportAnnotations.Title = "查询营销素材报表"
	server.AddTool(&mcp.Tool{
		Name: "report_marketing_materials", Annotations: &marketingMaterialReportAnnotations,
		Description: "当用户需要巨量营销项目或单元下的素材级消耗、曝光、点击、转化、订单、GMV 或 ROI 时调用。使用固定 MATERIAL_DATA 指标集。",
		InputSchema: json.RawMessage(marketingMaterialReportInputSchema), OutputSchema: json.RawMessage(marketingMaterialReportOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.reportMarketingMaterials)))
	marketingPlanReportAnnotations := *officialReadAnnotations
	marketingPlanReportAnnotations.Title = "查询营销项目报表"
	server.AddTool(&mcp.Tool{
		Name: "report_marketing_plans", Annotations: &marketingPlanReportAnnotations,
		Description: "当用户需要巨量营销项目级消耗、曝光、点击、转化、订单、GMV 或 ROI 时调用。使用固定指标集并保留应用服务生成的完整 Markdown 表格。",
		InputSchema: json.RawMessage(marketingPlanReportInputSchema), OutputSchema: json.RawMessage(marketingPlanReportOutputSchema),
	}, runtime.handler(withBoundedRequestBudget(runtime.reportMarketingPlans)))
	return server
}

func (runtime Runtime) handler(local mcp.ToolHandler) mcp.ToolHandler {
	if runtime.Forward != nil {
		return runtime.Forward
	}
	return local
}

func withBoundedRequestBudget(handler mcp.ToolHandler) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, _, _, err := requestcontrol.PrepareCommandContext(ctx, requestcontrol.DefaultCommandRequestLimit)
		if err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

func withUnboundedRequestBudget(handler mcp.ToolHandler) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, _, _, err := requestcontrol.PrepareUnboundedCommandContext(ctx)
		if err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

func (runtime Runtime) listTemplates(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := runtime.now()
	requestID := runtime.requestID()
	input, failure := decodeListInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "list_templates", *failure), nil
	}
	versioned, err := runtime.Query.ListVersioned(ctx, input.Channel, false)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_templates", mapQueryError(err)), nil
	}
	items, err := presentList(versioned.Value)
	if err != nil {
		return runtime.failureResult(started, requestID, "list_templates", toolFailure{
			Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{},
		}), nil
	}
	offset := 0
	if input.Cursor != "" {
		cursor, cursorFailure := decodeCursor(input.Cursor, input.Channel, versioned.StateVersion, len(items))
		if cursorFailure != nil {
			return runtime.failureResult(started, requestID, "list_templates", *cursorFailure), nil
		}
		offset = cursor.Offset
	}
	end := offset + input.Limit
	if end > len(items) {
		end = len(items)
	}
	page := append([]templateItem(nil), items[offset:end]...)
	var nextCursor *string
	if end < len(items) {
		encoded, encodeErr := encodeCursor(cursorPayload{
			Version: 1, Channel: input.Channel, StateVersion: versioned.StateVersion, Offset: end,
		})
		if encodeErr != nil {
			return runtime.failureResult(started, requestID, "list_templates", internalFailure()), nil
		}
		nextCursor = &encoded
	}
	output := listOutput{
		OK: true, RequestID: requestID, StateVersion: versioned.StateVersion, Source: "local_state",
		TotalCount: len(items), Items: page, NextCursor: nextCursor,
	}
	return runtime.successResult(started, requestID, "list_templates", output), nil
}

func (runtime Runtime) getTemplate(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	started := runtime.now()
	requestID := runtime.requestID()
	input, failure := decodeGetInput(request.Params.Arguments)
	if failure != nil {
		return runtime.failureResult(started, requestID, "get_template", *failure), nil
	}
	versioned, err := runtime.Query.ShowExactVersioned(ctx, input.Channel, input.TemplateID)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_template", mapQueryError(err)), nil
	}
	detail, err := presentDetail(versioned.Value)
	if err != nil {
		return runtime.failureResult(started, requestID, "get_template", toolFailure{
			Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{},
		}), nil
	}
	output := getOutput{
		OK: true, RequestID: requestID, StateVersion: versioned.StateVersion,
		Source: "local_state", Template: detail,
	}
	return runtime.successResult(started, requestID, "get_template", output), nil
}

func decodeListInput(raw json.RawMessage) (listInput, *toolFailure) {
	input := listInput{Channel: "all", Limit: 50}
	if err := decodeStrict(raw, &input); err != nil ||
		input.Channel != "all" && input.Channel != "marketing" && input.Channel != "qianchuan" ||
		input.Limit < 1 || input.Limit > 100 || len(input.Cursor) > 512 {
		failure := invalidArgumentFailure()
		return listInput{}, &failure
	}
	return input, nil
}

func decodeGetInput(raw json.RawMessage) (getInput, *toolFailure) {
	var input getInput
	if err := decodeStrict(raw, &input); err != nil ||
		input.Channel != "marketing" && input.Channel != "qianchuan" ||
		strings.TrimSpace(input.TemplateID) != input.TemplateID || len([]rune(input.TemplateID)) < 1 || len([]rune(input.TemplateID)) > 256 {
		failure := invalidArgumentFailure()
		return getInput{}, &failure
	}
	return input, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("arguments must contain one JSON object")
	}
	return nil
}

func encodeCursor(cursor cursorPayload) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(value, channel, stateVersion string, total int) (cursorPayload, *toolFailure) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	var cursor cursorPayload
	if err := decodeStrict(payload, &cursor); err != nil || cursor.Version != 1 || cursor.Channel != channel {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	if cursor.StateVersion != stateVersion {
		failure := toolFailure{Code: "STATE_CHANGED", Message: "local template state changed; restart the listing", Retryable: true, Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	if cursor.Offset < 0 || cursor.Offset >= total {
		failure := toolFailure{Code: "CURSOR_INVALID", Message: "cursor is invalid", Details: map[string]any{}}
		return cursorPayload{}, &failure
	}
	return cursor, nil
}

func mapQueryError(err error) toolFailure {
	if errors.Is(err, os.ErrPermission) {
		return toolFailure{Code: "LOCAL_ACCESS_DENIED", Message: "local template state is not readable", Details: map[string]any{}}
	}
	if errors.Is(err, os.ErrNotExist) {
		return toolFailure{Code: "CONFIG_UNAVAILABLE", Message: "local template state is unavailable", Details: map[string]any{}}
	}
	if errors.Is(err, filesystem.ErrManagedConfigInvalid) {
		return toolFailure{Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{}}
	}
	var templateError *domaintemplates.Error
	if errors.As(err, &templateError) {
		message := strings.ToLower(templateError.Message)
		if strings.Contains(message, "not found") {
			return toolFailure{Code: "TEMPLATE_NOT_FOUND", Message: "template was not found", Details: map[string]any{}}
		}
		if strings.Contains(message, "not unique") || strings.Contains(message, "does not match template_id") {
			return toolFailure{Code: "TEMPLATE_ID_CONFLICT", Message: "template ID is inconsistent or not unique", Details: map[string]any{}}
		}
		return toolFailure{Code: "CONFIG_INVALID", Message: "local template state is invalid", Details: map[string]any{}}
	}
	return internalFailure()
}

func invalidArgumentFailure() toolFailure {
	return toolFailure{Code: "INVALID_ARGUMENT", Message: "tool arguments are invalid", Details: map[string]any{}}
}

func internalFailure() toolFailure {
	return toolFailure{Code: "INTERNAL_ERROR", Message: "internal tool error", Details: map[string]any{}}
}

func (runtime Runtime) successResult(started time.Time, requestID, tool string, output any) *mcp.CallToolResult {
	runtime.log(started, requestID, tool, "ok", "")
	return resultFor(output, false)
}

func (runtime Runtime) failureResult(started time.Time, requestID, tool string, failure toolFailure) *mcp.CallToolResult {
	runtime.log(started, requestID, tool, "error", failure.Code)
	return resultFor(errorEnvelope{
		OK: false, RequestID: requestID,
		Error: errorPayload{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable, Details: failure.Details},
	}, true)
}

func resultFor(value any, isError bool) *mcp.CallToolResult {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(`{"ok":false,"request_id":"serialization","error":{"code":"INTERNAL_ERROR","message":"internal tool error","retryable":false,"details":{}}}`)
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		StructuredContent: json.RawMessage(payload), IsError: isError,
	}
}

func (runtime Runtime) now() time.Time {
	if runtime.Now != nil {
		return runtime.Now()
	}
	return time.Now().UTC()
}

func (runtime Runtime) requestID() string {
	if runtime.RequestID != nil {
		return runtime.RequestID()
	}
	payload := make([]byte, 16)
	if _, err := rand.Read(payload); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", runtime.now().UnixNano())))
	}
	return hex.EncodeToString(payload)
}

func (runtime Runtime) log(started time.Time, requestID, tool, status, errorCode string) {
	if runtime.LogWriter == nil {
		return
	}
	record := struct {
		Timestamp  string `json:"timestamp"`
		Level      string `json:"level"`
		RequestID  string `json:"request_id"`
		Tool       string `json:"tool"`
		DurationMS int64  `json:"duration_ms"`
		Status     string `json:"status"`
		ErrorCode  string `json:"error_code,omitempty"`
	}{
		Timestamp: runtime.now().Format(time.RFC3339Nano), Level: "info", RequestID: requestID,
		Tool: tool, DurationMS: runtime.now().Sub(started).Milliseconds(), Status: status, ErrorCode: errorCode,
	}
	_ = json.NewEncoder(runtime.LogWriter).Encode(record)
}
