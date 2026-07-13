#!/usr/bin/env python3
import argparse
import copy
import json

import config_paths
import config_store
import authorization_store
import channels
import credential_store
import configure_official_mcp
import plan_templates
import validate_config


SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
PLACEHOLDER_PREFIX = "REPLACE_WITH"
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")


def skill_root():
    return config_paths.skill_root()


def project_root():
    return config_paths.plugin_root()


def default_project_config():
    return config_paths.project_config_path()


def fallback_codex_config():
    return config_paths.home_config_path()


def get_path(data, dotted):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return None
        current = current[part]
    return current


def deep_merge(base, override):
    if not isinstance(base, dict) or not isinstance(override, dict):
        return copy.deepcopy(override)
    result = copy.deepcopy(base)
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def apply_plan_template(config):
    return plan_templates.apply(config)


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith(PLACEHOLDER_PREFIX)
    if isinstance(value, list):
        return len(value) == 0
    return False


def redacted_value(value, key):
    if key.split(".")[-1] in SECRET_KEYS and not is_missing(value):
        return "<redacted>"
    return value


def check_fields(config):
    result = validate_config.validate_config(config)
    runtime = channels.runtime_config(config, capability="query")
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


def next_action(query_missing, active_template, create_missing):
    if query_missing:
        return "edit_config"
    if not active_template:
        return "create_business_template"
    if create_missing:
        return "complete_active_template"
    return "ready"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--config",
        help="Config path to initialize or check. Defaults to project-local config/ads-plan-monitor/config.json.",
    )
    parser.add_argument(
        "--home-config",
        action="store_true",
        help="Use ~/.codex/ads-plan-monitor/config.json instead of the project-local config.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite an existing config with the bundled example template.",
    )
    args = parser.parse_args()

    template = skill_root() / "assets" / "config.example.json"
    config_path = (
        fallback_codex_config()
        if args.home_config
        else config_paths.resolve_config_path(args.config, prefer_project=True)
    )
    config_path = config_path.resolve()

    created = False
    if args.force or not config_path.exists():
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_store.atomic_write_json(
            config_path,
            json.loads(template.read_text(encoding="utf-8")),
        )
        created = True

    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    migrated_config = channels.migrate_config(raw_config)
    try:
        config = apply_plan_template(migrated_config)
    except ValueError:
        config = migrated_config
    query_missing, create_missing, field_preview = check_fields(migrated_config)
    mcp_status = configure_official_mcp.status()
    scripts_dir = skill_root() / "scripts"
    template_rows = []
    for name, raw_template in sorted((migrated_config.get("plan_templates") or {}).items()):
        template = plan_templates.normalize_template(migrated_config, name, raw_template)
        copy_titles = template["copy_materials"].get("titles") or []
        template_rows.append({
            "name": name,
            "channel": template["bindings"].get("channel"),
            "active": name == raw_config.get("active_plan_template"),
            "advertiser_id": template["bindings"].get("advertiser_id"),
            "platform": template["bindings"].get("platform"),
            "product_id": template["bindings"].get("product_id"),
            "product_name": template["bindings"].get("product_name"),
            "copy_materials_configured": bool(copy_titles),
            "copy_title_count": len(copy_titles),
            "binding_error": plan_templates.binding_error(template["bindings"]),
        })
    active_template = next((row for row in template_rows if row["active"]), None)
    channel_rows = []
    for row in channels.status_rows(migrated_config):
        channel_status = authorization_store.status(
            row["channel"],
            advertiser_id=(migrated_config.get("account") or {}).get("advertiser_id"),
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
        "next_action": next_action(query_missing, active_template, create_missing),
        "ok_for_query_data": not query_missing,
        "ok_for_create_plan": not query_missing and not create_missing,
        "active_plan_template": migrated_config.get("active_plan_template"),
        "active_template_advertiser_id": (
            active_template.get("advertiser_id") if active_template else None
        ),
        "available_plan_templates": template_rows,
        "plan_template_schema_version": migrated_config.get("plan_template_schema_version", 1),
        "template_migration_required": int(migrated_config.get("plan_template_schema_version") or 1) < 2,
        "channel_migration_required": int(raw_config.get("config_schema_version") or 1) < 2,
        "channel_migration_command": f'python3 "{scripts_dir / "migrate_channels.py"}" --config "{config_path}"',
        "template_setup": {
            "rule": "Each business template belongs to exactly one advertiser_id and cannot create plans for another advertiser.",
            "default_template_usage": "default_plan_template is a creation base shown by create-wizard and never participates in business delivery.",
            "list_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" list',
            "migrate_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" migrate',
            "create_wizard_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" create-wizard',
            "set_copy_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" set-copy --template <模板名> --title <文案1> --title <文案2>',
            "copy_from_template_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" set-copy --template <目标模板名> --from-template <来源模板名>',
        },
        "minimum_fields_for_query_data": [
            "local app_id and secret in the OS credential store",
            "local access_token or refresh_token from scripts/oauth_local_authorize.py",
            "account.advertiser_id",
        ],
        "oauth_setup": {
            "channel": "marketing",
            "channel_display_name": "巨量营销",
            "redirect_uri": get_path(migrated_config, "channels.marketing.oauth.redirect_uri"),
            "credential_backend": credential_store.backend_name(),
            "set_app_command": f'python3 "{scripts_dir / "credential_store.py"}" --config "{config_path}" --channel marketing --set-app',
            "local_authorize_command": f'python3 "{scripts_dir / "oauth_local_authorize.py"}" --config "{config_path}" --channel marketing',
            "token_status_command": f'python3 "{scripts_dir / "token_manager.py"}" --config "{config_path}" --channel marketing --status',
        },
        "official_docs_mcp": {
            **mcp_status,
            "configure_command": f'python3 "{scripts_dir / "configure_official_mcp.py"}"',
            "status_command": f'python3 "{scripts_dir / "configure_official_mcp.py"}" --status',
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
