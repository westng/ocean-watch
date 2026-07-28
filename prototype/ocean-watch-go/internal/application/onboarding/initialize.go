package onboarding

import (
	"context"
	"fmt"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

const marketingTemplateSchemaVersion = 5

type ConfigInitializer interface {
	Initialize(context.Context, map[string]any, bool) (bool, error)
	Read(context.Context) (map[string]any, error)
}

type MCPStatusProbe interface {
	Status(context.Context, bool, bool) map[string]any
}

type Initializer struct {
	Store      ConfigInitializer
	State      LocalState
	Doctor     Doctor
	MCP        MCPStatusProbe
	ConfigPath string
	SkillRoot  string
	Command    string
}

func (initializer Initializer) Run(
	ctx context.Context,
	template map[string]any,
	force bool,
) (map[string]any, error) {
	created, err := initializer.Store.Initialize(ctx, template, force)
	if err != nil {
		return nil, err
	}
	raw, err := initializer.Store.Read(ctx)
	if err != nil {
		return nil, err
	}
	migrated, err := configuration.MigrateChannels(raw)
	if err != nil {
		return nil, err
	}
	channel := configuration.SelectedChannel(migrated, "")
	runtimeConfig, _, err := configuration.Runtime(migrated, channel, "oauth")
	if err != nil {
		return nil, err
	}
	redirectURI := strings.TrimSpace(fmt.Sprint(configuration.Value(runtimeConfig, "oauth.redirect_uri")))
	environment := initializer.Doctor.Report(ctx, channel, redirectURI)
	validation, err := (Validator{State: initializer.State}).Validate(ctx, migrated, "all", "")
	if err != nil {
		return nil, err
	}
	templateRows, err := domaintemplates.MarketingGuideRows(migrated)
	if err != nil {
		return nil, err
	}
	advertiserID := validConfiguredAdvertiserID(migrated)
	channelRows, err := initializer.State.ChannelRows(ctx, migrated, advertiserID)
	if err != nil {
		return nil, err
	}
	marketingSnapshot, err := initializer.State.Snapshot(ctx, "marketing", "", raw)
	if err != nil {
		return nil, err
	}
	mcpStatus := defaultMCPStatus(
		!configuration.Missing(marketingSnapshot.App["app_id"]),
		!configuration.Missing(marketingSnapshot.Legacy["developer_id"]),
	)
	if initializer.MCP != nil {
		mcpStatus = initializer.MCP.Status(
			ctx,
			!configuration.Missing(marketingSnapshot.App["app_id"]),
			!configuration.Missing(marketingSnapshot.Legacy["developer_id"]),
		)
	}

	command := initializer.Command
	if strings.TrimSpace(command) == "" {
		command = "ocean-watch"
	}
	configPath := initializer.ConfigPath
	queryMissing := stringsFromAny(validation["missing_query_required"])
	createMissing := stringsFromAny(validation["missing_create_preview_required"])
	if templateError := validation["plan_template_error"]; templateError != nil {
		createMissing = append(createMissing, "plan template: "+fmt.Sprint(templateError))
	}
	templateSchemaVersion, err := integerDefault(migrated["plan_template_schema_version"], 1)
	if err != nil {
		return nil, err
	}
	channelSchemaVersion, err := integerDefault(raw["config_schema_version"], 1)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"mode":                                   "first_run_guide",
		"skill":                                  "ads-plan-monitor",
		"skill_root":                             initializer.SkillRoot,
		"default_channel":                        migrated["default_channel"],
		"channels":                               channelRows,
		"config":                                 configPath,
		"created_config_from_template":           created,
		"environment":                            environment,
		"environment_ready":                      environment.OK,
		"environment_check_command":              fmt.Sprintf(`%s setup doctor --config %q`, command, configPath),
		"next_action":                            firstRunNextAction(queryMissing, templateRows),
		"ok_for_query_data":                      len(queryMissing) == 0,
		"create_plan_requires_explicit_template": true,
		"available_plan_templates":               templateRows,
		"plan_template_schema_version":           templateSchemaVersion,
		"template_migration_required":            templateSchemaVersion < marketingTemplateSchemaVersion,
		"channel_migration_required":             channelSchemaVersion < configuration.SchemaVersion,
		"channel_migration_command":              fmt.Sprintf(`%s auth migrate --config %q`, command, configPath),
		"template_setup": map[string]any{
			"rule":                        "Each business template belongs to exactly one advertiser_id and cannot create plans for another advertiser.",
			"default_template_usage":      "default_plan_template is a creation base shown by create-wizard and never participates in business delivery.",
			"business_template_selection": "Every create command must name a business template explicitly; there is no active or default business template.",
			"list_command":                fmt.Sprintf(`%s templates list --config %q`, command, configPath),
			"migrate_command":             fmt.Sprintf(`%s templates migrate --config %q`, command, configPath),
			"create_wizard_command":       fmt.Sprintf(`%s templates create --config %q`, command, configPath),
			"set_copy_command":            fmt.Sprintf(`%s templates set-copy --config %q --template <模板名> --title <文案1> --title <文案2>`, command, configPath),
			"copy_from_template_command":  fmt.Sprintf(`%s templates set-copy --config %q --template <目标模板名> --from-template <来源模板名>`, command, configPath),
		},
		"minimum_fields_for_query_data": []any{
			"local app_id and secret in the OS credential store",
			"local access_token or refresh_token from ocean-watch auth authorize",
			"account.advertiser_id",
		},
		"oauth_setup": map[string]any{
			"channel":                 "marketing",
			"channel_display_name":    "巨量营销",
			"redirect_uri":            configuration.Value(runtimeConfig, "oauth.redirect_uri"),
			"redirect_uri_usage":      "Register this exact URI in the official console; do not open it directly.",
			"credential_backend":      initializer.State.Credentials.BackendName(),
			"local_authorize_command": fmt.Sprintf(`%s auth authorize --config %q --channel marketing`, command, configPath),
			"replace_app_command":     fmt.Sprintf(`%s auth set-app --config %q --channel marketing`, command, configPath),
			"token_status_command":    fmt.Sprintf(`%s auth status --config %q --channel marketing`, command, configPath),
		},
		"official_docs_mcp": mergeMap(mcpStatus, map[string]any{
			"configure_command":    command + " mcp configure",
			"status_command":       command + " mcp status",
			"capabilities_command": command + " mcp capabilities",
		}),
		"additional_fields_for_create_plan": anyStrings(createMissing),
		"missing_query_fields":              anyStrings(queryMissing),
		"current_query_field_preview": map[string]any{
			"api.base_url":          configuration.Value(runtimeConfig, "api.base_url"),
			"account.advertiser_id": configuration.Value(runtimeConfig, "account.advertiser_id"),
		},
		"safe_notes": []any{
			"Do not paste tokens into chat; store app credentials and tokens in the OS credential store.",
			"The approved local callback is http://127.0.0.1:8787/oauth/callback.",
			"Query-data mode is read-only.",
			"Create-plan mode writes to Ocean Engine only after explicit user confirmation.",
		},
		"example_first_prompts": []any{
			"用 ads-plan-monitor 初始化配置",
			"查询今天消耗前十",
			"查询昨天汇总数据",
			"创建计划前先检查参数",
		},
	}
	return result, nil
}

func firstRunNextAction(queryMissing []string, templates []any) string {
	if len(queryMissing) != 0 {
		return "edit_config"
	}
	if len(templates) == 0 {
		return "create_business_template"
	}
	return "select_business_template"
}

func integerDefault(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	parsed, err := configuration.Integer(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func defaultMCPStatus(hasAppID, hasDeveloperID bool) map[string]any {
	nextAction := "register_mcp"
	if !hasAppID {
		nextAction = "save_app_credentials"
	} else if !hasDeveloperID {
		nextAction = "configure_developer_id"
	}
	return map[string]any{
		"server": "oceanengine-developer-docs", "codex_cli_available": false,
		"has_app_id": hasAppID, "has_developer_id": hasDeveloperID,
		"registered": false, "uses_sse_bridge": false, "ready": false,
		"next_action": nextAction,
	}
}

func mergeMap(left, right map[string]any) map[string]any {
	result := make(map[string]any, len(left)+len(right))
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}
