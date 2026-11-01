package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	accounts "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/accounts"
	official "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/official"
	reports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

type QianchuanUnifiedTaskReports interface {
	QianchuanSchema(context.Context, reports.QianchuanSchemaQuery) (reports.QianchuanSchemaResult, error)
	QianchuanCustom(context.Context, reports.QianchuanCustomQuery) (reports.QianchuanCustomResult, error)
}
type ManagedAccountReports interface {
	Report(context.Context, accounts.Query) (domain.AccountReportResult, *domain.Error)
}

type officialToolOutput struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"request_id"`
	Source    string `json:"source"`
	Result    any    `json:"result"`
}

const officialOutputSchema = `{"type":"object","properties":{"ok":{"type":"boolean"},"request_id":{"type":"string"},"source":{"type":"string","enum":["local_contract","official_mcp","official_sdk"]},"result":{"type":"object"}},"required":["ok","request_id","source","result"],"additionalProperties":false}`

func objectSchema(properties map[string]any, required ...string) json.RawMessage {
	result := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		result["required"] = required
	}
	encoded, _ := json.Marshal(result)
	return encoded
}
func stringSchema() map[string]any { return map[string]any{"type": "string", "maxLength": 4096} }
func idSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,18}$"}
}
func stringsSchema(maximum int) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string", "maxLength": 128}, "maxItems": maximum}
}
func numberSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}
func parameterSchema() map[string]any { return map[string]any{"type": "object", "maxProperties": 128} }
func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func (runtime Runtime) addOfficialTools(server *mcp.Server) {
	scope := objectSchema(map[string]any{"kind": enumSchema("advertiser", "agent", "shop", "customer_center", "organization", "authorization", "public_catalog"), "id": stringSchema()}, "kind", "id")
	works := map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": objectSchema(map[string]any{"url": stringSchema(), "product_id": idSchema(), "creator_uid": idSchema(), "creator_douyin_id": stringSchema(), "creator_name": stringSchema()}, "url")}
	tasks := []struct {
		id, name, description string
		schema                json.RawMessage
		handler               mcp.ToolHandler
		local                 bool
	}{
		{"qianchuan.official_capabilities", "get_qianchuan_official_capabilities", "查询已审核的千川官方 GET 目录。先用关键词检索，再用精确 query_id 获取参数、主体、单位和不可用原因。只读本地合同。", objectSchema(map[string]any{"query_id": stringSchema(), "group": stringSchema(), "keyword": stringSchema(), "limit": numberSchema(1, 100), "cursor": stringSchema()}), runtime.officialCatalog, true},
		{"qianchuan.official_query", "query_qianchuan_official", "执行目录中一个精确 query_id 的受控官方 GET。scope 指定当前账户主体，参数须符合该项合同；cursor 原样续页。不能指定 URL、Token、工具名、模式或后端。", objectSchema(map[string]any{"query_id": stringSchema(), "scope": scope, "auth_account_id": idSchema(), "parameters": parameterSchema(), "limit": numberSchema(1, 1000), "cursor": stringSchema()}, "query_id", "scope"), runtime.officialQuery, false},
		{"qianchuan.account_health", "get_qianchuan_account_health", "检查一个明确广告主，或本地启用的负责账户，余额、日预算和计划配额。选择 advertiser_id 或 managed_accounts；limit 有界，失败和空值不会视为正常。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "managed_accounts": map[string]any{"type": "boolean"}, "checks": map[string]any{"type": "array", "items": enumSchema("balance", "budget", "quota"), "maxItems": 3, "uniqueItems": true}, "limit": numberSchema(1, 20), "cursor": stringSchema()}), runtime.officialHealth, false},
		{"qianchuan.plan_diagnose", "diagnose_qianchuan_plan", "诊断一个精确计划。先核实当前计划场景，再查询对应审核、学习期和赔付事实；不适用项目明确跳过。legacy 必须明确指定。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "plan_id": idSchema(), "scene": enumSchema("uni", "overall", "legacy")}, "advertiser_id", "plan_id"), runtime.officialDiagnose, false},
		{"qianchuan.material_quality", "inspect_qianchuan_material_quality", "核对精确千川素材的原创和低效标签。提供 material_ids 或 works；作品须经当前 F2 和官方达人、商品、作品关系确认素材 ID。不会创建计划预检。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "material_ids": map[string]any{"type": "array", "items": idSchema(), "maxItems": 100, "minItems": 1, "uniqueItems": true}, "works": works}, "advertiser_id"), runtime.officialQuality, false},
		{"qianchuan.delivery_suggestions", "get_qianchuan_delivery_suggestions", "查询指定场景的官方预算、ROI、出价或审核建议。有 plan_id 时先核对当前场景；缺少参数会返回补充要求。返回建议及单位，不自动调整计划。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "kind": enumSchema("budget", "roi", "bid", "audit"), "scene": enumSchema("uni", "overall", "legacy"), "plan_id": idSchema(), "parameters": parameterSchema()}, "advertiser_id", "kind", "scene"), runtime.officialSuggestions, false},
		{"qianchuan.report_schema", "get_qianchuan_report_schema", "查询当前千川账户支持的统一报表主题、维度、指标与口径。复用官方 SDK，先查此工具再构造自定义报表。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "topics": stringsSchema(10), "data_period": stringSchema()}, "advertiser_id"), runtime.officialReportSchema, false},
		{"qianchuan.custom_report", "report_qianchuan_custom", "按已确认的报表主题、维度和指标查询自定义千川报表。复用 SDK，日期和翻页有界；保留指标单位、口径和截断状态。", objectSchema(map[string]any{"advertiser_id": idSchema(), "auth_account_id": idSchema(), "start_date": stringSchema(), "end_date": stringSchema(), "data_topic": stringSchema(), "dimensions": stringsSchema(20), "metrics": stringsSchema(50), "filters": map[string]any{"type": "array", "maxItems": 20, "items": objectSchema(map[string]any{"field": stringSchema(), "operator": numberSchema(0, 100), "values": stringsSchema(100)}, "field", "operator", "values")}, "data_period": stringSchema(), "order_field": stringSchema(), "order_type": enumSchema("ASC", "DESC"), "page_size": numberSchema(1, 100), "max_pages": numberSchema(1, 20), "top": numberSchema(1, 1000)}, "advertiser_id", "start_date", "end_date", "data_topic", "dimensions", "metrics"), runtime.officialCustomReport, false},
		{"shared.managed_account_report", "report_qianchuan_managed_accounts", "查询本地已启用的负责千川账户消耗和表现。复用 SDK 的负责账户报表服务，保留失败账户及缺失指标；不会扫描整份授权列表。", objectSchema(map[string]any{"start_date": stringSchema(), "end_date": stringSchema(), "concurrency": numberSchema(1, 8)}, "start_date", "end_date"), runtime.officialManagedReport, false},
	}
	for _, task := range tasks {
		no := false
		open := !task.local
		annotations := &mcp.ToolAnnotations{ReadOnlyHint: task.local, DestructiveHint: &no, OpenWorldHint: &open, IdempotentHint: task.local}
		handler := task.handler
		if !task.local {
			handler = withBoundedRequestBudget(handler)
		}
		addCapabilityTool(server, task.id, &mcp.Tool{Name: task.name, Description: task.description, InputSchema: task.schema, OutputSchema: json.RawMessage(officialOutputSchema), Annotations: annotations}, runtime.handler(handler))
	}
}

func (runtime Runtime) officialRun(ctx context.Context, request *mcp.CallToolRequest, source string, invoke func() (any, error)) (*mcp.CallToolResult, error) {
	started, id := runtime.now(), runtime.requestID()
	value, err := invoke()
	if err != nil {
		return runtime.failureResult(started, id, request.Params.Name, mapOfficialError(err)), nil
	}
	return runtime.successResult(started, id, request.Params.Name, officialToolOutput{OK: true, RequestID: id, Source: source, Result: value}), nil
}
func mapOfficialError(err error) toolFailure {
	result := toolFailure{Code: "QUERY_FAILED", Message: "查询未完成。", Details: map[string]any{}}
	var controlled *port.Failure
	if errors.As(err, &controlled) {
		result.Code = controlled.Code
		result.Message = controlled.Code
		result.Details["stage"] = controlled.Stage
		result.Details["dispatched"] = controlled.Dispatched
		if controlled.APICode != "" {
			result.Details["api_code"] = controlled.APICode
		}
		if len(controlled.RequiredFields) > 0 {
			result.Details["required_fields"] = controlled.RequiredFields
		}
		if controlled.AuthorizationID != "" {
			result.Details["authorization_id"] = controlled.AuthorizationID
			result.Details["channel"] = "qianchuan"
		}
		if controlled.AdvertiserID != "" {
			result.Details["advertiser_id"] = controlled.AdvertiserID
		}
	}
	if errors.Is(err, context.Canceled) {
		result.Code = "CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.Code = "TIMEOUT"
	}
	return result
}
func officialUnavailable() error {
	return &port.Failure{Code: "BUSINESS_CONFIGURATION_REQUIRED", Stage: "prepare"}
}
func invalidOfficialInput() error { return &port.Failure{Code: "INVALID_ARGUMENT", Stage: "request"} }

func (runtime Runtime) officialCatalog(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "local_contract", func() (any, error) {
		var input official.CatalogQuery
		if decodeStrict(r.Params.Arguments, &input) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialQueries == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialQueries.Catalog(ctx, input)
	})
}
func (runtime Runtime) officialQuery(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_mcp", func() (any, error) {
		input, err := contracts.DecodeOfficialQueryRequest(bytes.NewReader(r.Params.Arguments))
		if err != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialQueries == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialQueries.Query(ctx, input)
	})
}
func (runtime Runtime) officialHealth(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_mcp", func() (any, error) {
		var input official.HealthRequest
		if decodeStrict(r.Params.Arguments, &input) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialTasks == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialTasks.Health(ctx, input)
	})
}
func (runtime Runtime) officialDiagnose(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_mcp", func() (any, error) {
		var input official.PlanDiagnosisRequest
		if decodeStrict(r.Params.Arguments, &input) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialTasks == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialTasks.Diagnose(ctx, input)
	})
}
func (runtime Runtime) officialQuality(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_mcp", func() (any, error) {
		var input official.MaterialQualityRequest
		if decodeStrict(r.Params.Arguments, &input) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialTasks == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialTasks.Quality(ctx, input)
	})
}
func (runtime Runtime) officialSuggestions(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_mcp", func() (any, error) {
		var input official.SuggestionRequest
		if decodeStrict(r.Params.Arguments, &input) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.OfficialTasks == nil {
			return nil, officialUnavailable()
		}
		return runtime.OfficialTasks.Suggestions(ctx, input)
	})
}

type schemaTaskInput struct {
	AdvertiserID  string   `json:"advertiser_id"`
	AuthAccountID string   `json:"auth_account_id,omitempty"`
	Topics        []string `json:"topics,omitempty"`
	DataPeriod    string   `json:"data_period,omitempty"`
}

func (runtime Runtime) officialReportSchema(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_sdk", func() (any, error) {
		var i schemaTaskInput
		if decodeStrict(r.Params.Arguments, &i) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.UnifiedReports == nil {
			return nil, officialUnavailable()
		}
		if runtime.checkReportSubject(i.AdvertiserID) != nil {
			return nil, &port.Failure{Code: "SCOPE_DENIED", Stage: "scope"}
		}
		return runtime.UnifiedReports.QianchuanSchema(ctx, reports.QianchuanSchemaQuery{CredentialScope: reports.CredentialScope{AdvertiserID: i.AdvertiserID, AuthAccountID: i.AuthAccountID}, Topics: i.Topics, DataPeriod: i.DataPeriod})
	})
}

type customTaskInput struct {
	AdvertiserID  string   `json:"advertiser_id"`
	AuthAccountID string   `json:"auth_account_id,omitempty"`
	StartDate     string   `json:"start_date"`
	EndDate       string   `json:"end_date"`
	DataTopic     string   `json:"data_topic"`
	Dimensions    []string `json:"dimensions"`
	Metrics       []string `json:"metrics"`
	Filters       []struct {
		Field    string   `json:"field"`
		Operator int64    `json:"operator"`
		Values   []string `json:"values"`
	} `json:"filters,omitempty"`
	DataPeriod string `json:"data_period,omitempty"`
	OrderField string `json:"order_field,omitempty"`
	OrderType  string `json:"order_type,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	MaxPages   int    `json:"max_pages,omitempty"`
	Top        int    `json:"top,omitempty"`
}

func (runtime Runtime) officialCustomReport(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_sdk", func() (any, error) {
		var i customTaskInput
		if decodeStrict(r.Params.Arguments, &i) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.UnifiedReports == nil {
			return nil, officialUnavailable()
		}
		if runtime.checkReportSubject(i.AdvertiserID) != nil {
			return nil, &port.Failure{Code: "SCOPE_DENIED", Stage: "scope"}
		}
		filters := []reports.QianchuanFilter{}
		for _, filter := range i.Filters {
			filters = append(filters, reports.QianchuanFilter{Field: filter.Field, Operator: filter.Operator, Values: filter.Values})
		}
		return runtime.UnifiedReports.QianchuanCustom(ctx, reports.QianchuanCustomQuery{CredentialScope: reports.CredentialScope{AdvertiserID: i.AdvertiserID, AuthAccountID: i.AuthAccountID}, StartDate: i.StartDate, EndDate: i.EndDate, DataTopic: i.DataTopic, Dimensions: i.Dimensions, Metrics: i.Metrics, Filters: filters, DataPeriod: i.DataPeriod, OrderField: i.OrderField, OrderType: i.OrderType, PageSize: i.PageSize, MaxPages: i.MaxPages, Top: i.Top})
	})
}
func (runtime Runtime) checkReportSubject(advertiser string) error {
	if !validOptionalRoutingID(advertiser) || advertiser == "" {
		return invalidOfficialInput()
	}
	if runtime.Policy != nil {
		return runtime.Policy.CheckSubject("qianchuan", "advertiser", advertiser)
	}
	return nil
}
func (runtime Runtime) officialManagedReport(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return runtime.officialRun(ctx, r, "official_sdk", func() (any, error) {
		var i struct {
			StartDate   string `json:"start_date"`
			EndDate     string `json:"end_date"`
			Concurrency int    `json:"concurrency,omitempty"`
		}
		if decodeStrict(r.Params.Arguments, &i) != nil {
			return nil, invalidOfficialInput()
		}
		if runtime.AccountReports == nil {
			return nil, officialUnavailable()
		}
		result, err := runtime.AccountReports.Report(ctx, accounts.Query{Channels: []domain.Channel{domain.Qianchuan}, StartDate: i.StartDate, EndDate: i.EndDate, Concurrency: i.Concurrency})
		if err != nil {
			return nil, err
		}
		return result, nil
	})
}
