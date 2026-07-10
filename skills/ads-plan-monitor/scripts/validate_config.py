#!/usr/bin/env python3
import argparse
import json
from types import SimpleNamespace

import config_paths
import create_plan
import credential_store


MODES = ("query", "create-preview", "create-submit", "all")


def payload_args():
    return SimpleNamespace(
        advertiser_id=None,
        budget=None,
        bid=None,
        roi_goal=None,
        video_id=None,
        material_date=None,
        product_name=None,
        product_id=None,
        project_name=None,
        promotion_name=None,
        project_id=None,
    )


def validate_config(raw_config, credentials=None):
    credentials = credential_store.read_credentials() if credentials is None else credentials
    merged_config = credential_store.merge_credentials(raw_config, credentials)
    query_missing = []
    if create_plan.contains_unresolved_value(create_plan.get_path(merged_config, "api.base_url")):
        query_missing.append("api.base_url")
    if create_plan.contains_unresolved_value(create_plan.get_path(merged_config, "account.advertiser_id")):
        query_missing.append("account.advertiser_id")
    if create_plan.is_missing(credentials.get("app_id")) or create_plan.is_missing(credentials.get("secret")):
        query_missing.append("local app_id and secret")
    if create_plan.is_missing(credentials.get("access_token")) and create_plan.is_missing(credentials.get("refresh_token")):
        query_missing.append("local access_token or refresh_token")

    template_error = None
    selected_template = raw_config.get("active_plan_template")
    preview_missing = []
    submit_missing = []
    try:
        effective_config = create_plan.apply_plan_template(merged_config)
        selected = effective_config.get("_selected_plan_template") or {}
        selected_template = selected.get("name")
        project_payload, promotion_payload = create_plan.build_payloads(effective_config, payload_args())
        preview_missing = create_plan.missing_fields(
            effective_config,
            project_payload,
            promotion_payload,
            False,
        )
        submit_missing = create_plan.missing_fields(
            effective_config,
            project_payload,
            promotion_payload,
            True,
        )
    except (KeyError, TypeError, ValueError) as exc:
        template_error = str(exc)

    if create_plan.is_missing(credentials.get("app_id")) or create_plan.is_missing(credentials.get("secret")):
        submit_missing.append("local app_id and secret")
    if create_plan.is_missing(credentials.get("access_token")) and create_plan.is_missing(credentials.get("refresh_token")):
        submit_missing.append("local access_token or refresh_token")

    preview_missing = list(dict.fromkeys(preview_missing))
    submit_missing = list(dict.fromkeys(submit_missing))
    readiness = {
        "query": not query_missing,
        "create-preview": not template_error and not preview_missing,
        "create-submit": not template_error and not submit_missing,
    }
    return {
        "active_plan_template": selected_template,
        "plan_template_error": template_error,
        "ok_for_query_data": readiness["query"],
        "ok_for_create_payload_preview": readiness["create-preview"],
        "ok_for_create_api_submission": readiness["create-submit"],
        "missing_query_required": query_missing,
        "missing_create_preview_required": preview_missing,
        "missing_create_submit_required": submit_missing,
        "readiness": readiness,
    }


def mode_is_ready(result, mode):
    readiness = result["readiness"]
    if mode == "all":
        return all(readiness.values())
    return readiness[mode]


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("config", nargs="?", help="Config path; overrides environment and defaults.")
    parser.add_argument("--mode", choices=MODES, default="all")
    args = parser.parse_args(argv)

    path = config_paths.resolve_config_path(args.config)
    if not path.exists():
        parser.error(f"missing config: {path}")

    try:
        raw_config = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        parser.error(f"invalid config {path}: {exc}")

    result = validate_config(raw_config)
    result.update({
        "config": str(path),
        "validation_mode": args.mode,
        "selected_mode_ready": mode_is_ready(result, args.mode),
        "credential_status": credential_store.status(path),
    })
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["selected_mode_ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
