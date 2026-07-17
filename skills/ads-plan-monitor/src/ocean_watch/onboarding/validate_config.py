#!/usr/bin/env python3
import argparse
import json
from types import SimpleNamespace

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.core.config_paths as config_paths
import ocean_watch.plans.create_plan as create_plan

MODES = ("query", "create-preview", "create-submit", "all")
QUERY_CAPABILITIES = {
    "marketing": "query",
    "qianchuan": "qianchuan_materials",
}


def query_capability(channel):
    try:
        return QUERY_CAPABILITIES[channel]
    except KeyError:
        raise channels.ChannelError(
            "channel_query_not_implemented",
            channel,
            f"channel {channel} does not implement a query workflow",
        ) from None


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


def validate_config(raw_config, credentials=None, plan_template=None):
    channel = channels.selected_channel(raw_config)
    merged_config = channels.runtime_config(
        raw_config,
        channel=channel,
        capability=query_capability(channel),
    )
    if credentials is None:
        advertiser_id = (merged_config.get("account") or {}).get("advertiser_id")
        if create_plan.contains_unresolved_value(advertiser_id):
            advertiser_id = None
        merged_config = authorization_store.attach_runtime(
            merged_config,
            channel,
            advertiser_id=advertiser_id,
        )
        app_credentials = authorization_store.read_app(channel)
        runtime_credentials = merged_config.get("api") or {}
    else:
        app_credentials = credentials
        runtime_credentials = credentials
        merged_config.setdefault("api", {}).update({
            key: value for key, value in credentials.items() if key != "developer_id"
        })
    query_missing = []
    if create_plan.contains_unresolved_value(create_plan.get_path(merged_config, "api.base_url")):
        query_missing.append("api.base_url")
    if create_plan.contains_unresolved_value(create_plan.get_path(merged_config, "account.advertiser_id")):
        query_missing.append("account.advertiser_id")
    if create_plan.is_missing(app_credentials.get("app_id")) or create_plan.is_missing(app_credentials.get("secret")):
        query_missing.append("local app_id and secret")
    if create_plan.is_missing(runtime_credentials.get("access_token")) and create_plan.is_missing(runtime_credentials.get("refresh_token")):
        query_missing.append("local access_token or refresh_token")

    template_error = None
    selected_template = plan_template
    preview_missing = []
    submit_missing = []
    try:
        effective_config = create_plan.apply_plan_template(
            merged_config,
            template_name=plan_template,
        )
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
        if create_plan.get_path(effective_config, "material_strategy.source_type") == "CREATOR_AUTHORIZED":
            preview_missing = [
                "runtime.creator_material_selection" if field == "materials.video_ids" else field
                for field in preview_missing
            ]
            submit_missing = [
                "runtime.creator_material_selection" if field == "materials.video_ids" else field
                for field in submit_missing
            ]
    except (KeyError, TypeError, ValueError) as exc:
        template_error = str(exc)

    if create_plan.is_missing(app_credentials.get("app_id")) or create_plan.is_missing(app_credentials.get("secret")):
        submit_missing.append("local app_id and secret")
    if create_plan.is_missing(runtime_credentials.get("access_token")) and create_plan.is_missing(runtime_credentials.get("refresh_token")):
        submit_missing.append("local access_token or refresh_token")

    preview_missing = list(dict.fromkeys(preview_missing))
    submit_missing = list(dict.fromkeys(submit_missing))
    readiness = {
        "query": not query_missing,
        "create-preview": not template_error and not preview_missing,
        "create-submit": not template_error and not submit_missing,
    }
    return {
        "selected_plan_template": selected_template,
        "channel": channel,
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
    parser.add_argument("--config", help="Config path; overrides environment and defaults.")
    parser.add_argument("--mode", choices=MODES, default="all")
    parser.add_argument(
        "--plan-template",
        help="Explicit business template required for create-preview or create-submit validation.",
    )
    args = parser.parse_args(argv)

    path = config_paths.resolve_config_path(args.config)
    if not path.exists():
        parser.error(f"missing config: {path}")

    try:
        raw_config = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        parser.error(f"invalid config {path}: {exc}")

    try:
        result = validate_config(raw_config, plan_template=args.plan_template)
    except channels.ChannelError as exc:
        print(json.dumps({
            "config": str(path),
            "validation_mode": args.mode,
            "selected_mode_ready": False,
            "channel": exc.channel,
            "error_code": exc.code,
            "error": str(exc),
        }, ensure_ascii=False, indent=2))
        return 1
    result.update({
        "config": str(path),
        "validation_mode": args.mode,
        "selected_mode_ready": mode_is_ready(result, args.mode),
        "credential_status": credential_store.status(path, channel=result["channel"]),
    })
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["selected_mode_ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
