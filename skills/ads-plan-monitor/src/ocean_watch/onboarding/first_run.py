#!/usr/bin/env python3
import argparse
import json
import sys
from importlib import resources

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
import ocean_watch.integrations.configure_official_mcp as configure_official_mcp
import ocean_watch.onboarding.environment_check as environment_check
import ocean_watch.onboarding.validate_config as validate_config
import ocean_watch.templates.plan_templates as plan_templates
from ocean_watch.core.data import get_path, is_missing

SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
PLACEHOLDER_PREFIX = "REPLACE_WITH"
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")
CONFIG_TEMPLATE = "config.example.json"


def skill_root():
    return config_paths.skill_root()


def project_root():
    return config_paths.plugin_root()


def default_project_config():
    return config_paths.project_config_path()


def fallback_codex_config():
    return config_paths.home_config_path()


def cli_command():
    runner = skill_root() / "run.py"
    return f'"{sys.executable}" "{runner}"' if runner.is_file() else "ocean-watch"


def load_config_template():
    source_template = skill_root() / "assets" / CONFIG_TEMPLATE
    if source_template.is_file():
        return json.loads(source_template.read_text(encoding="utf-8"))
    packaged_template = resources.files("ocean_watch.resources").joinpath(CONFIG_TEMPLATE)
    return json.loads(packaged_template.read_text(encoding="utf-8"))


def redacted_value(value, key):
    if key.split(".")[-1] in SECRET_KEYS and not is_missing(value):
        return "<redacted>"
    return value


def check_fields(config, plan_template=None):
    result = validate_config.validate_config(config, plan_template=plan_template)
    selected_channel = channels.selected_channel(config)
    runtime = channels.runtime_config(
        config,
        channel=selected_channel,
        capability=validate_config.query_capability(selected_channel),
    )
    query_required = [
        "api.base_url",
        "account.advertiser_id",
    ]
    field_preview = {
        field: redacted_value(get_path(runtime, field), field)
        for field in query_required
    }
    create_missing = list(result["missing_create_preview_required"])
    if result["plan_template_error"]:
        create_missing.append(f"plan template: {result['plan_template_error']}")
    return result["missing_query_required"], create_missing, field_preview


def next_action(query_missing, templates):
    if query_missing:
        return "edit_config"
    if not templates:
        return "create_business_template"
    return "select_business_template"


def configured_advertiser_id(config):
    value = (config.get("account") or {}).get("advertiser_id")
    if is_missing(value):
        return None
    text = str(value)
    return text if text.isdigit() and int(text) > 0 else None


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--config",
        help="Config path to initialize or check. Defaults to project-local config/ads-plan-monitor/config.json.",
    )
    parser.add_argument(
        "--home-config",
        action="store_true",
        help="Use $CODEX_HOME/ads-plan-monitor/config.json instead of project config.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing config with the bundled example template.",
    )
    args = parser.parse_args(argv)

    config_path = (
        fallback_codex_config()
        if args.home_config
        else config_paths.resolve_config_path(args.config, prefer_project=True)
    )
    config_path = config_path.resolve()

    created = config_store.initialize_json(
        config_path,
        load_config_template,
        overwrite=args.force,
    )

    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    migrated_config = channels.migrate_config(raw_config)
    query_missing, create_missing, field_preview = check_fields(migrated_config)
    environment = environment_check.environment_report(
        config_path,
        channel=channels.selected_channel(migrated_config),
    )
    mcp_status = configure_official_mcp.status()
    command = cli_command()
    template_rows = []
    for name, raw_template in sorted((migrated_config.get("plan_templates") or {}).items()):
        template = plan_templates.normalize_template(migrated_config, name, raw_template)
        copy_titles = template["copy_materials"].get("titles") or []
        template_rows.append({
            "name": name,
            "channel": template["bindings"].get("channel"),
            "advertiser_id": template["bindings"].get("advertiser_id"),
            "platform": template["bindings"].get("platform"),
            "product_id": template["bindings"].get("product_id"),
            "product_name": template["bindings"].get("product_name"),
            "material_source_type": template["material_strategy"].get("source_type"),
            "material_strategy_error": plan_templates.material_strategy_error(
                template["material_strategy"]
            ),
            "copy_materials_configured": bool(copy_titles),
            "copy_title_count": len(copy_titles),
            "binding_error": plan_templates.binding_error(template["bindings"]),
        })
    advertiser_id = configured_advertiser_id(migrated_config)
    channel_rows = []
    for row in channels.status_rows(migrated_config):
        channel_status = authorization_store.status(
            row["channel"],
            advertiser_id=advertiser_id,
        ) if row["implemented"] else {}
        channel_rows.append({**row, **channel_status})

    print(json.dumps({
        "mode": "first_run_guide",
        "skill": "ads-plan-monitor",
        "skill_root": str(skill_root()),
        "default_channel": migrated_config.get("default_channel"),
        "channels": channel_rows,
        "config": str(config_path),
        "created_config_from_template": created,
        "environment": environment,
        "environment_ready": environment["ok"],
        "environment_check_command": f'{command} setup doctor --config "{config_path}"',
        "next_action": next_action(query_missing, template_rows),
        "ok_for_query_data": not query_missing,
        "create_plan_requires_explicit_template": True,
        "available_plan_templates": template_rows,
        "plan_template_schema_version": migrated_config.get("plan_template_schema_version", 1),
        "template_migration_required": (
            int(migrated_config.get("plan_template_schema_version") or 1)
            < plan_templates.SCHEMA_VERSION
        ),
        "channel_migration_required": int(raw_config.get("config_schema_version") or 1) < 2,
        "channel_migration_command": f'{command} auth migrate --config "{config_path}"',
        "template_setup": {
            "rule": "Each business template belongs to exactly one advertiser_id and cannot create plans for another advertiser.",
            "default_template_usage": "default_plan_template is a creation base shown by create-wizard and never participates in business delivery.",
            "business_template_selection": "Every create command must name a business template explicitly; there is no active or default business template.",
            "list_command": f'{command} templates list --config "{config_path}"',
            "migrate_command": f'{command} templates migrate --config "{config_path}"',
            "create_wizard_command": f'{command} templates create --config "{config_path}"',
            "set_copy_command": f'{command} templates set-copy --config "{config_path}" --template <模板名> --title <文案1> --title <文案2>',
            "copy_from_template_command": f'{command} templates set-copy --config "{config_path}" --template <目标模板名> --from-template <来源模板名>',
        },
        "minimum_fields_for_query_data": [
            "local app_id and secret in the OS credential store",
            "local access_token or refresh_token from ocean-watch auth authorize",
            "account.advertiser_id",
        ],
        "oauth_setup": {
            "channel": "marketing",
            "channel_display_name": "巨量营销",
            "redirect_uri": get_path(migrated_config, "channels.marketing.oauth.redirect_uri"),
            "redirect_uri_usage": "Register this exact URI in the official console; do not open it directly.",
            "credential_backend": credential_store.backend_name(),
            "local_authorize_command": f'{command} auth authorize --config "{config_path}" --channel marketing',
            "replace_app_command": f'{command} auth set-app --config "{config_path}" --channel marketing',
            "token_status_command": f'{command} auth status --config "{config_path}" --channel marketing',
        },
        "official_docs_mcp": {
            **mcp_status,
            "configure_command": f'{command} mcp configure',
            "status_command": f'{command} mcp status',
            "capabilities_command": f'{command} mcp capabilities',
        },
        "additional_fields_for_create_plan": create_missing,
        "missing_query_fields": query_missing,
        "current_query_field_preview": field_preview,
        "safe_notes": [
            "Do not paste tokens into chat; store app credentials and tokens in the OS credential store.",
            "The approved local callback is http://127.0.0.1:8787/oauth/callback.",
            "Query-data mode is read-only.",
            "Create-plan mode writes to Ocean Engine only after explicit user confirmation.",
        ],
        "example_first_prompts": [
            "用 ads-plan-monitor 初始化配置",
            "查询今天消耗前十",
            "查询昨天汇总数据",
            "创建计划前先检查参数",
        ],
    }, ensure_ascii=False, indent=2))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
