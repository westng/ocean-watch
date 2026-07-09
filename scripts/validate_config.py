#!/usr/bin/env python3
import json
import sys
import copy
from pathlib import Path

import credential_store


SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")


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
    effective = copy.deepcopy(config)
    templates = config.get("plan_templates") or {}
    selected = config.get("active_plan_template")
    if not selected:
        return effective, None, None
    if selected not in templates:
        return effective, selected, f"unknown active_plan_template: {selected}"
    template = templates[selected]
    for section in TEMPLATE_SECTIONS:
        if section in template:
            effective[section] = deep_merge(effective.get(section, {}), template[section] or {})
    if "titles" in template:
        effective["titles"] = copy.deepcopy(template["titles"])
    return effective, selected, None


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith("REPLACE_WITH")
    if isinstance(value, list):
        return len(value) == 0
    return False


def has_token_path():
    credentials = credential_store.read_credentials()
    return not is_missing(credentials.get("access_token")) or not is_missing(credentials.get("refresh_token"))


def has_app_credentials():
    credentials = credential_store.read_credentials()
    return not is_missing(credentials.get("app_id")) and not is_missing(credentials.get("secret"))


def main():
    if len(sys.argv) != 2:
        print("usage: validate_config.py <config.json>", file=sys.stderr)
        return 2

    path = Path(sys.argv[1]).expanduser()
    if not path.exists():
        print(f"missing config: {path}", file=sys.stderr)
        return 2

    raw_data = json.loads(path.read_text(encoding="utf-8"))
    data, selected_template, template_error = apply_plan_template(raw_data)
    common_required = [
        "api.base_url",
        "account.advertiser_id",
    ]
    query_required = common_required
    create_preview_required = [
        "api.base_url",
        "account.advertiser_id",
        "defaults.project_name_template",
        "defaults.promotion_name_template",
        "defaults.product_id",
        "defaults.daily_budget",
        "defaults.roi_goal",
        "materials.video_ids",
        "tracking_urls.track_url",
        "tracking_urls.action_track_url",
        "links.landing_page_url",
        "links.open_url",
    ]
    create_submit_required = create_preview_required + common_required
    create_recommended = [
        "resolved_ids.city_ids",
        "resolved_ids.product_platform_id",
        "resolved_ids.product_image_ids",
        "titles",
    ]

    missing_query_required = [key for key in query_required if is_missing(get_path(data, key))]
    if not has_app_credentials():
        missing_query_required.append("local app_id and secret")
    if not has_token_path():
        missing_query_required.append("local access_token or refresh_token")
    missing_create_preview_required = [key for key in create_preview_required if is_missing(get_path(data, key))]
    missing_create_submit_required = [key for key in create_submit_required if is_missing(get_path(data, key))]
    if not has_app_credentials():
        missing_create_submit_required.append("local app_id and secret")
    if not has_token_path():
        missing_create_submit_required.append("local access_token or refresh_token")
    missing_create_recommended = [key for key in create_recommended if is_missing(get_path(data, key))]

    print(json.dumps({
        "config": str(path),
        "active_plan_template": selected_template,
        "plan_template_error": template_error,
        "ok_for_query_data": not missing_query_required,
        "ok_for_create_payload_preview": not template_error and not missing_create_preview_required,
        "ok_for_create_api_submission": not template_error and not missing_create_submit_required and not missing_create_recommended,
        "missing_query_required": missing_query_required,
        "missing_create_preview_required": missing_create_preview_required,
        "missing_create_submit_required": missing_create_submit_required,
        "missing_create_recommended": missing_create_recommended,
        "secret_fields_redacted": sorted(SECRET_KEYS),
        "credential_status": credential_store.status(path),
    }, ensure_ascii=False, indent=2))

    return 1 if missing_query_required or template_error else 0


if __name__ == "__main__":
    raise SystemExit(main())
