#!/usr/bin/env python3
import argparse
import copy
import json
import shutil
from pathlib import Path

import config_paths
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
    query_required = [
        "api.base_url",
        "account.advertiser_id",
    ]
    field_preview = {
        field: redacted_value(get_path(config, field), field)
        for field in query_required
    }
    create_missing = list(result["missing_create_preview_required"])
    if result["plan_template_error"]:
        create_missing.append(f"plan template: {result['plan_template_error']}")
    return result["missing_query_required"], create_missing, field_preview


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
        shutil.copyfile(template, config_path)
        created = True

    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    try:
        config = apply_plan_template(raw_config)
    except ValueError:
        config = raw_config
    query_missing, create_missing, field_preview = check_fields(raw_config)
    mcp_status = configure_official_mcp.status()
    scripts_dir = skill_root() / "scripts"
    template_rows = []
    for name, raw_template in sorted((raw_config.get("plan_templates") or {}).items()):
        template = plan_templates.normalize_template(raw_config, name, raw_template)
        template_rows.append({
            "name": name,
            "active": name == raw_config.get("active_plan_template"),
            "advertiser_id": template["bindings"].get("advertiser_id"),
            "platform": template["bindings"].get("platform"),
            "product_id": template["bindings"].get("product_id"),
            "product_name": template["bindings"].get("product_name"),
            "binding_error": plan_templates.binding_error(template["bindings"]),
        })
    active_template = next((row for row in template_rows if row["active"]), None)

    print(json.dumps({
        "mode": "first_run_guide",
        "skill": "ads-plan-monitor",
        "skill_root": str(skill_root()),
        "config": str(config_path),
        "created_config_from_template": created,
        "next_action": (
            "edit_config" if query_missing
            else "create_business_template" if create_missing
            else "ready"
        ),
        "ok_for_query_data": not query_missing,
        "ok_for_create_plan": not query_missing and not create_missing,
        "active_plan_template": raw_config.get("active_plan_template"),
        "active_template_advertiser_id": (
            active_template.get("advertiser_id") if active_template else None
        ),
        "available_plan_templates": template_rows,
        "plan_template_schema_version": raw_config.get("plan_template_schema_version", 1),
        "template_migration_required": int(raw_config.get("plan_template_schema_version") or 1) < 2,
        "template_setup": {
            "rule": "Each business template belongs to exactly one advertiser_id and cannot create plans for another advertiser.",
            "default_template_usage": "default_plan_template provides shared defaults only and cannot submit plans directly.",
            "list_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" list',
            "migrate_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" migrate',
            "create_command": f'python3 "{scripts_dir / "manage_plan_templates.py"}" --config "{config_path}" create --advertiser-id <广告主ID> --platform <平台> --traffic-source CID --product-id <商品ID> --product-name <商品名> --activate',
        },
        "minimum_fields_for_query_data": [
            "local app_id and secret in the OS credential store",
            "local access_token or refresh_token from scripts/oauth_local_authorize.py",
            "account.advertiser_id",
        ],
        "oauth_setup": {
            "redirect_uri": get_path(raw_config, "oauth.redirect_uri"),
            "credential_backend": credential_store.backend_name(),
            "set_app_command": f'python3 "{scripts_dir / "credential_store.py"}" --config "{config_path}" --set-app',
            "local_authorize_command": f'python3 "{scripts_dir / "oauth_local_authorize.py"}" --config "{config_path}"',
            "token_status_command": f'python3 "{scripts_dir / "token_manager.py"}" --config "{config_path}" --status',
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
