package contracts

import (
	"fmt"
	"strings"
)

type Effect string

const (
	EffectLocalRead          Effect = "local_read"
	EffectLocalWrite         Effect = "local_write"
	EffectPublicRead         Effect = "public_read"
	EffectAuthorizationWrite Effect = "authorization_write"
	EffectOfficialRead       Effect = "official_read"
	EffectOnlineWrite        Effect = "online_write"
)

type Surface string

const (
	SurfaceCLI Surface = "cli"
	SurfaceMCP Surface = "mcp"
)

type Capability struct {
	Channel        string
	Effect         Effect
	RequiresSubmit bool
}

// CapabilitySpec is static routing metadata. Handlers and transport schemas
// remain owned by their transport packages.
type CapabilitySpec struct {
	ID                 string
	Channel            string
	Effect             Effect
	RequiresSubmit     bool
	MCPTool            string
	CLICommand         string
	PrimarySurface     Surface
	FastRoute          bool
	CommandDescription string
}

func (spec CapabilitySpec) Route() string {
	if spec.PrimarySurface == SurfaceMCP {
		return spec.MCPTool
	}
	return "./run " + spec.CLICommand
}

type FastRouteSpec struct {
	Skill        string
	Outcome      string
	CapabilityID string
	Surface      Surface
	Route        string
}

type CapabilityRegistry struct {
	specs      []CapabilitySpec
	byID       map[string]CapabilitySpec
	byCLI      map[string]CapabilitySpec
	byMCP      map[string]CapabilitySpec
	fastRoutes []FastRouteSpec
}

func mustCapabilityRegistry(specs []CapabilitySpec, fastRoutes []FastRouteSpec) CapabilityRegistry {
	registry := CapabilityRegistry{
		specs: append([]CapabilitySpec(nil), specs...), byID: map[string]CapabilitySpec{},
		byCLI: map[string]CapabilitySpec{}, byMCP: map[string]CapabilitySpec{},
		fastRoutes: append([]FastRouteSpec(nil), fastRoutes...),
	}
	for _, spec := range registry.specs {
		if spec.ID == "" || (spec.Channel != "shared" && spec.Channel != "marketing" && spec.Channel != "qianchuan") {
			panic(fmt.Sprintf("invalid capability identity: %#v", spec))
		}
		if spec.Effect == "" || spec.Effect == EffectOnlineWrite && !spec.RequiresSubmit {
			panic(fmt.Sprintf("invalid capability effect: %#v", spec))
		}
		if _, exists := registry.byID[spec.ID]; exists {
			panic("duplicate capability id: " + spec.ID)
		}
		if spec.CLICommand == "" && spec.MCPTool == "" {
			panic("capability has no transport: " + spec.ID)
		}
		if spec.PrimarySurface == SurfaceCLI && spec.CLICommand == "" || spec.PrimarySurface == SurfaceMCP && spec.MCPTool == "" {
			panic("capability primary surface is unavailable: " + spec.ID)
		}
		if spec.PrimarySurface != SurfaceCLI && spec.PrimarySurface != SurfaceMCP {
			panic("capability primary surface is invalid: " + spec.ID)
		}
		registry.byID[spec.ID] = spec
		if spec.CLICommand != "" {
			if _, exists := registry.byCLI[spec.CLICommand]; exists {
				panic("duplicate capability CLI command: " + spec.CLICommand)
			}
			if len(strings.Fields(spec.CLICommand)) != 2 || strings.TrimSpace(spec.CommandDescription) == "" {
				panic("invalid capability CLI metadata: " + spec.ID)
			}
			registry.byCLI[spec.CLICommand] = spec
		}
		if spec.MCPTool != "" {
			if _, exists := registry.byMCP[spec.MCPTool]; exists {
				panic("duplicate capability MCP tool: " + spec.MCPTool)
			}
			registry.byMCP[spec.MCPTool] = spec
		}
	}
	fastCapabilityIDs := map[string]bool{}
	for _, route := range registry.fastRoutes {
		spec, ok := registry.byID[route.CapabilityID]
		if !ok || route.Skill == "" || route.Outcome == "" || route.Route == "" {
			panic(fmt.Sprintf("invalid fast route: %#v", route))
		}
		switch route.Surface {
		case SurfaceMCP:
			if spec.PrimarySurface != SurfaceMCP || route.Route != spec.MCPTool {
				panic("fast MCP route does not match capability: " + route.CapabilityID)
			}
		case SurfaceCLI:
			if spec.PrimarySurface != SurfaceCLI || !strings.HasPrefix(route.Route, "./run "+spec.CLICommand) {
				panic("fast CLI route does not match capability: " + route.CapabilityID)
			}
		default:
			panic("fast route surface is invalid: " + route.CapabilityID)
		}
		fastCapabilityIDs[route.CapabilityID] = true
	}
	for _, spec := range registry.specs {
		if spec.FastRoute != fastCapabilityIDs[spec.ID] {
			panic("capability fast-route flag is inconsistent: " + spec.ID)
		}
	}
	return registry
}

func (registry CapabilityRegistry) All() []CapabilitySpec {
	return append([]CapabilitySpec(nil), registry.specs...)
}

func (registry CapabilityRegistry) FastRoutes() []FastRouteSpec {
	return append([]FastRouteSpec(nil), registry.fastRoutes...)
}

func (registry CapabilityRegistry) ByID(id string) (CapabilitySpec, bool) {
	spec, ok := registry.byID[id]
	return spec, ok
}

func (registry CapabilityRegistry) ByCLICommand(command string) (CapabilitySpec, bool) {
	spec, ok := registry.byCLI[command]
	return spec, ok
}

func (registry CapabilityRegistry) ByMCPTool(tool string) (CapabilitySpec, bool) {
	spec, ok := registry.byMCP[tool]
	return spec, ok
}

func (registry CapabilityRegistry) Command(name string) (Command, bool) {
	spec, ok := registry.ByCLICommand(name)
	if !ok {
		return Command{}, false
	}
	parts := strings.SplitN(spec.CLICommand, " ", 2)
	return Command{Domain: parts[0], Action: parts[1], Description: spec.CommandDescription}, true
}

func (registry CapabilityRegistry) Commands() []Command {
	commands := make([]Command, 0, len(registry.byCLI))
	for _, spec := range registry.specs {
		if spec.CLICommand == "" {
			continue
		}
		command, _ := registry.Command(spec.CLICommand)
		commands = append(commands, command)
	}
	return commands
}

func cli(id, channel string, effect Effect, command, description string) CapabilitySpec {
	return CapabilitySpec{ID: id, Channel: channel, Effect: effect, RequiresSubmit: effect == EffectOnlineWrite, CLICommand: command, PrimarySurface: SurfaceCLI, CommandDescription: description}
}

func cliMCP(id, channel string, effect Effect, command, description, tool string) CapabilitySpec {
	return CapabilitySpec{ID: id, Channel: channel, Effect: effect, RequiresSubmit: effect == EffectOnlineWrite, CLICommand: command, MCPTool: tool, PrimarySurface: SurfaceMCP, FastRoute: true, CommandDescription: description}
}

func mcpOnly(id, channel string, effect Effect, tool string, fast bool) CapabilitySpec {
	return CapabilitySpec{ID: id, Channel: channel, Effect: effect, RequiresSubmit: effect == EffectOnlineWrite, MCPTool: tool, PrimarySurface: SurfaceMCP, FastRoute: fast}
}

var DefaultCapabilityRegistry = mustCapabilityRegistry([]CapabilitySpec{
	cli("shared.setup_doctor", "shared", EffectLocalRead, "setup doctor", "Check local runtime requirements"),
	cli("shared.setup_initialize", "shared", EffectLocalWrite, "setup init", "Initialize local configuration"),
	cli("shared.setup_validate", "shared", EffectLocalRead, "setup validate", "Validate configuration readiness"),
	cli("shared.authorization_set_app", "shared", EffectAuthorizationWrite, "auth set-app", "Store app credentials"),
	cli("shared.authorization_authorize", "shared", EffectAuthorizationWrite, "auth authorize", "Run local OAuth authorization"),
	cli("shared.authorization_status", "shared", EffectLocalRead, "auth status", "Show redacted token status"),
	cli("shared.authorization_refresh", "shared", EffectAuthorizationWrite, "auth refresh", "Refresh an access token"),
	{ID: "shared.authorization_sync_accounts", Channel: "shared", Effect: EffectAuthorizationWrite, CLICommand: "auth sync-accounts", PrimarySurface: SurfaceCLI, FastRoute: true, CommandDescription: "Sync advertisers"},
	cli("shared.authorization_mappings", "shared", EffectLocalRead, "auth mappings", "Show sanitized advertiser authorization mappings"),
	cliMCP("shared.managed_account_list", "shared", EffectLocalRead, "accounts list", "List responsible accounts", "list_managed_accounts"),
	cli("shared.managed_account_add", "shared", EffectLocalWrite, "accounts add", "Add or update a responsible account"),
	cli("shared.managed_account_remove", "shared", EffectLocalWrite, "accounts remove", "Remove a responsible account"),
	cli("shared.managed_account_enable", "shared", EffectLocalWrite, "accounts enable", "Enable a responsible account"),
	cli("shared.managed_account_disable", "shared", EffectLocalWrite, "accounts disable", "Disable a responsible account"),
	{ID: "shared.managed_account_report", Channel: "shared", Effect: EffectOfficialRead, CLICommand: "accounts report", PrimarySurface: SurfaceCLI, FastRoute: true, CommandDescription: "Query spend for responsible accounts"},
	cliMCP("shared.template_list", "shared", EffectLocalRead, "templates list", "List Marketing and Qianchuan templates", "list_templates"),
	cliMCP("shared.template_get", "shared", EffectLocalRead, "templates show", "Show one Marketing or Qianchuan template", "get_template"),
	cli("shared.template_create", "shared", EffectLocalWrite, "templates create", "Create a channel-specific template"),
	cli("shared.template_set_copy", "shared", EffectLocalWrite, "templates set-copy", "Set copy materials"),
	cli("shared.template_validate", "shared", EffectLocalRead, "templates validate", "Validate business templates"),
	cli("shared.template_delete", "shared", EffectLocalWrite, "templates delete", "Delete a business template"),
	cli("qianchuan.template_list", "qianchuan", EffectLocalRead, "qc-templates list", "List Qianchuan product templates"),
	cli("qianchuan.template_create", "qianchuan", EffectLocalWrite, "qc-templates create", "Create a Qianchuan product template"),
	cli("qianchuan.live_template_list", "qianchuan", EffectLocalRead, "qc-templates list-live", "List Qianchuan live templates"),
	cli("qianchuan.live_template_create", "qianchuan", EffectLocalWrite, "qc-templates create-live", "Create a Qianchuan live template"),
	cliMCP("marketing.video_search", "marketing", EffectOfficialRead, "materials videos", "Query uploaded videos", "search_marketing_videos"),
	cliMCP("marketing.creator_material_search", "marketing", EffectOfficialRead, "materials creator", "Query creator videos", "search_marketing_creator_materials"),
	cli("marketing.image_search", "marketing", EffectOfficialRead, "materials images", "Query image assets"),
	cli("marketing.product_search", "marketing", EffectOfficialRead, "materials products", "Query product assets"),
	cli("qianchuan.creator_video_search", "qianchuan", EffectOfficialRead, "qc-materials creator-videos", "Query Qianchuan creator videos"),
	cli("qianchuan.work_inspect", "qianchuan", EffectPublicRead, "qc-materials inspect-work", "Inspect public Douyin work links"),
	cli("qianchuan.authorized_creator_list", "qianchuan", EffectOfficialRead, "qc-materials authorized-creators", "List authorized Qianchuan creators"),
	cli("qianchuan.product_list", "qianchuan", EffectOfficialRead, "qc-products list", "List Qianchuan products"),
	cliMCP("qianchuan.product_search", "qianchuan", EffectOfficialRead, "qc-products search", "Search Qianchuan products", "search_qianchuan_products"),
	cliMCP("qianchuan.plan_list", "qianchuan", EffectOfficialRead, "qc-plans list", "List Qianchuan plans", "list_qianchuan_plans"),
	cliMCP("qianchuan.plan_get", "qianchuan", EffectOfficialRead, "qc-plans show", "Show a Qianchuan plan", "get_qianchuan_plan"),
	cli("qianchuan.plan_material_list", "qianchuan", EffectOfficialRead, "qc-plans materials", "List materials in a Qianchuan plan"),
	cli("qianchuan.plan_binding_audit", "qianchuan", EffectOfficialRead, "qc-plans binding-audit", "Audit Qianchuan daily plan binding candidates"),
	{ID: "qianchuan.plan_bind", Channel: "qianchuan", Effect: EffectLocalWrite, RequiresSubmit: true, CLICommand: "qc-plans bind", PrimarySurface: SurfaceCLI, CommandDescription: "Bind a verified Qianchuan daily plan locally"},
	cli("qianchuan.plan_status_update", "qianchuan", EffectOnlineWrite, "qc-plans update-status", "Update Qianchuan plan status"),
	cli("qianchuan.plan_budget_update", "qianchuan", EffectOnlineWrite, "qc-plans update-budget", "Update Qianchuan plan budget"),
	cli("qianchuan.plan_roi_update", "qianchuan", EffectOnlineWrite, "qc-plans update-roi", "Update Qianchuan plan ROI target"),
	cli("shared.run_list", "shared", EffectLocalRead, "runs list", "List local execution runs"),
	cli("shared.run_get", "shared", EffectLocalRead, "runs show", "Show one local execution run"),
	cli("marketing.plan_create_uploaded", "marketing", EffectOnlineWrite, "plans create", "Create from uploaded materials"),
	cli("marketing.plan_create_creator", "marketing", EffectOnlineWrite, "plans create-creator", "Create from creator materials"),
	cli("qianchuan.plan_create", "qianchuan", EffectOnlineWrite, "plans create-qianchuan", "Create a Qianchuan all-domain plan"),
	cli("qianchuan.plan_batch_works", "qianchuan", EffectOnlineWrite, "plans batch-qianchuan-works", "Create or append Qianchuan plans from Douyin work links"),
	cli("qianchuan.plan_remove_work", "qianchuan", EffectOnlineWrite, "plans remove-qianchuan-work", "Remove Qianchuan plan materials by Douyin work link"),
	cli("marketing.plan_batch_uploaded", "marketing", EffectOnlineWrite, "plans batch-upload", "Batch uploaded materials"),
	cli("marketing.plan_batch_creator", "marketing", EffectOnlineWrite, "plans batch-creator", "Batch creator materials"),
	cli("marketing.project_status_update", "marketing", EffectOnlineWrite, "plans update-project-status", "Update Marketing project status"),
	cli("marketing.promotion_status_update", "marketing", EffectOnlineWrite, "plans update-promotion-status", "Update Marketing promotion status"),
	cli("marketing.promotion_budget_update", "marketing", EffectOnlineWrite, "plans update-budget", "Update Marketing promotion budget"),
	cli("marketing.promotion_bid_update", "marketing", EffectOnlineWrite, "plans update-bid", "Update Marketing promotion bid"),
	cli("marketing.project_roi_update", "marketing", EffectOnlineWrite, "plans update-roi", "Update Marketing project ROI target"),
	cliMCP("marketing.material_report", "marketing", EffectOfficialRead, "reports materials", "Material performance", "report_marketing_materials"),
	cli("marketing.report_schema", "marketing", EffectOfficialRead, "reports schema", "Available report fields"),
	cli("marketing.custom_report", "marketing", EffectOfficialRead, "reports custom", "Custom report"),
	cliMCP("marketing.plan_report", "marketing", EffectOfficialRead, "reports plans", "Marketing project performance", "report_marketing_plans"),
	cliMCP("qianchuan.plan_report", "qianchuan", EffectOfficialRead, "qc-reports plans", "Qianchuan all-domain plan performance", "report_qianchuan_plans"),
	cli("qianchuan.material_report", "qianchuan", EffectOfficialRead, "qc-reports materials", "Qianchuan material performance"),
	cli("qianchuan.account_report", "qianchuan", EffectOfficialRead, "qc-reports account", "Qianchuan all-domain and overall account performance"),
	cliMCP("qianchuan.uni_account_report", "qianchuan", EffectOfficialRead, "qc-reports uni-account", "Qianchuan all-domain account performance", "report_qianchuan_uni_account"),
	cli("qianchuan.report_schema", "qianchuan", EffectOfficialRead, "qc-reports schema", "Qianchuan unified report dimensions and metrics"),
	cli("qianchuan.custom_report", "qianchuan", EffectOfficialRead, "qc-reports custom", "Qianchuan custom all-domain or overall report"),
	cli("qianchuan.product_report", "qianchuan", EffectOfficialRead, "qc-reports products", "Qianchuan product-dimension performance"),
	cli("qianchuan.room_report", "qianchuan", EffectOfficialRead, "qc-reports rooms", "Qianchuan live-room dimension performance"),
	cli("qianchuan.author_report", "qianchuan", EffectOfficialRead, "qc-reports authors", "Qianchuan Douyin-author dimension performance"),
	cli("marketing.project_discovery", "marketing", EffectOfficialRead, "discover projects", "Find projects"),
	cli("marketing.promotion_discovery", "marketing", EffectOfficialRead, "discover promotions", "Find promotions"),
	cli("marketing.dpa_discovery", "marketing", EffectOfficialRead, "discover dpa", "Find DPA assets"),
	cli("marketing.event_discovery", "marketing", EffectOfficialRead, "discover events", "Find event assets"),
	cli("marketing.deep_bid_discovery", "marketing", EffectOfficialRead, "discover deep-bids", "Find deep bid types"),
	cli("marketing.goal_discovery", "marketing", EffectOfficialRead, "discover goals", "Find optimization goals"),
	cli("marketing.city_discovery", "marketing", EffectOfficialRead, "discover cities", "Resolve city identifiers"),
	mcpOnly("shared.capability_list", "shared", EffectLocalRead, "get_capabilities", false),
	mcpOnly("qianchuan.work_preflight", "qianchuan", EffectOfficialRead, "preflight_qianchuan_works", true),
	mcpOnly("qianchuan.preflight_get", "qianchuan", EffectLocalRead, "get_qianchuan_preflight", true),
	mcpOnly("qianchuan.authorization_inspect", "qianchuan", EffectLocalRead, "get_qianchuan_authorization", true),
	mcpOnly("marketing.authorization_inspect", "marketing", EffectLocalRead, "get_marketing_authorization", true),
	mcpOnly("qianchuan.account_report_fixed", "qianchuan", EffectOfficialRead, "report_qianchuan_account", true),
}, []FastRouteSpec{
	{Skill: "ads-plan-monitor", Outcome: "List local Marketing or Qianchuan templates", CapabilityID: "shared.template_list", Surface: SurfaceMCP, Route: "list_templates"},
	{Skill: "ads-plan-monitor", Outcome: "Show one exact template", CapabilityID: "shared.template_get", Surface: SurfaceMCP, Route: "get_template"},
	{Skill: "ads-plan-monitor", Outcome: "List accounts I manage/use", CapabilityID: "shared.managed_account_list", Surface: SurfaceMCP, Route: "list_managed_accounts"},
	{Skill: "ads-plan-monitor", Outcome: "Query spend/performance for my managed account set", CapabilityID: "shared.managed_account_report", Surface: SurfaceCLI, Route: "./run accounts report"},
	{Skill: "ads-plan-monitor", Outcome: "Inspect Marketing Token/app/advertiser mapping", CapabilityID: "marketing.authorization_inspect", Surface: SurfaceMCP, Route: "get_marketing_authorization"},
	{Skill: "ads-plan-monitor", Outcome: "Search uploaded Marketing videos", CapabilityID: "marketing.video_search", Surface: SurfaceMCP, Route: "search_marketing_videos"},
	{Skill: "ads-plan-monitor", Outcome: "Search Marketing creator materials", CapabilityID: "marketing.creator_material_search", Surface: SurfaceMCP, Route: "search_marketing_creator_materials"},
	{Skill: "ads-plan-monitor", Outcome: "Fixed Marketing material report", CapabilityID: "marketing.material_report", Surface: SurfaceMCP, Route: "report_marketing_materials"},
	{Skill: "ads-plan-monitor", Outcome: "Fixed Marketing project report", CapabilityID: "marketing.plan_report", Surface: SurfaceMCP, Route: "report_marketing_plans"},
	{Skill: "ads-plan-monitor", Outcome: "Make current OAuth user's advertiser coverage match official access", CapabilityID: "shared.authorization_sync_accounts", Surface: SurfaceCLI, Route: "./run auth sync-accounts --channel marketing"},
	{Skill: "qc-plan-monitor", Outcome: "List local Marketing or Qianchuan templates", CapabilityID: "shared.template_list", Surface: SurfaceMCP, Route: "list_templates"},
	{Skill: "qc-plan-monitor", Outcome: "Show one exact template", CapabilityID: "shared.template_get", Surface: SurfaceMCP, Route: "get_template"},
	{Skill: "qc-plan-monitor", Outcome: "List accounts I manage/use", CapabilityID: "shared.managed_account_list", Surface: SurfaceMCP, Route: "list_managed_accounts"},
	{Skill: "qc-plan-monitor", Outcome: "Query spend/performance for my managed Qianchuan account set", CapabilityID: "shared.managed_account_report", Surface: SurfaceCLI, Route: "./run accounts report --channel qianchuan"},
	{Skill: "qc-plan-monitor", Outcome: "Inspect Qianchuan Token/app/advertiser mapping", CapabilityID: "qianchuan.authorization_inspect", Surface: SurfaceMCP, Route: "get_qianchuan_authorization"},
	{Skill: "qc-plan-monitor", Outcome: "Search selectable Qianchuan products", CapabilityID: "qianchuan.product_search", Surface: SurfaceMCP, Route: "search_qianchuan_products"},
	{Skill: "qc-plan-monitor", Outcome: "List Qianchuan product all-domain plans", CapabilityID: "qianchuan.plan_list", Surface: SurfaceMCP, Route: "list_qianchuan_plans"},
	{Skill: "qc-plan-monitor", Outcome: "Show one exact plan and optional materials", CapabilityID: "qianchuan.plan_get", Surface: SurfaceMCP, Route: "get_qianchuan_plan"},
	{Skill: "qc-plan-monitor", Outcome: "Fixed account overall/uni report", CapabilityID: "qianchuan.account_report_fixed", Surface: SurfaceMCP, Route: "report_qianchuan_account"},
	{Skill: "qc-plan-monitor", Outcome: "All-domain account report", CapabilityID: "qianchuan.uni_account_report", Surface: SurfaceMCP, Route: "report_qianchuan_uni_account"},
	{Skill: "qc-plan-monitor", Outcome: "Fixed plan report", CapabilityID: "qianchuan.plan_report", Surface: SurfaceMCP, Route: "report_qianchuan_plans"},
	{Skill: "qc-plan-monitor", Outcome: "Preflight Douyin works without creating or changing a plan", CapabilityID: "qianchuan.work_preflight", Surface: SurfaceMCP, Route: "preflight_qianchuan_works"},
	{Skill: "qc-plan-monitor", Outcome: "Inspect one exact preflight snapshot", CapabilityID: "qianchuan.preflight_get", Surface: SurfaceMCP, Route: "get_qianchuan_preflight"},
})
