#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import re
from pathlib import Path

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
import ocean_watch.templates.plan_templates as plan_templates
from ocean_watch.core.data import get_path, is_missing
from ocean_watch.plans.executor import PlanExecutionRequest, PlanExecutor

PLACEHOLDER_PREFIX = "REPLACE_WITH"
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")
UNRESOLVED_TEMPLATE_PATTERN = re.compile(r"\{+[A-Za-z_][A-Za-z0-9_]*\}+")
UNRESOLVED_MARKERS = ("REPLACE_WITH", "TODO", "待填", "待反查")


def contains_unresolved_value(value):
    if isinstance(value, str):
        stripped = value.strip()
        return (
            is_missing(value)
            or "example.com" in stripped.lower()
            or any(marker.lower() in stripped.lower() for marker in UNRESOLVED_MARKERS)
            or bool(UNRESOLVED_TEMPLATE_PATTERN.search(stripped))
        )
    if isinstance(value, list):
        return not value or any(contains_unresolved_value(item) for item in value)
    if isinstance(value, dict):
        return any(contains_unresolved_value(item) for item in value.values())
    return value is None


def api_integer_if_decimal(value):
    if isinstance(value, str) and value.isdigit():
        return int(value)
    return value


def official_text_positions(value):
    return sum(0.5 if ord(char) < 128 else 1 for char in str(value or ""))


def apply_plan_template(config, template_name=None, channel=None):
    return plan_templates.apply(config, template_name, channel=channel)


def material_date_for_yesterday(now=None):
    now = now or dt.datetime.now()
    yesterday = now.date() - dt.timedelta(days=1)
    return f"{yesterday.month}.{yesterday.day}"


def render_template(template, values):
    result = template
    for key, value in values.items():
        result = result.replace("{" + key + "}", str(value))
    return result


def build_payloads(config, args):
    defaults = config["defaults"]
    advertiser_id = args.advertiser_id or get_path(config, "account.advertiser_id")
    material_date = args.material_date or material_date_for_yesterday()
    product_name = args.product_name or defaults["product_name"]
    product_id = args.product_id or defaults["product_id"]
    budget = args.budget if args.budget is not None else defaults["daily_budget"]
    cpa_bid = args.bid if args.bid is not None else defaults.get("cpa_bid")
    videos = args.video_id or get_path(config, "materials.video_ids", [])

    names = {
        "material_date": material_date,
        "product_name": product_name,
        "group_index": 1,
        "index": 1,
        "suffix": "01",
    }
    project_name = args.project_name or render_template(defaults["project_name_template"], names)
    promotion_name = args.promotion_name or render_template(defaults["promotion_name_template"], names)

    related_product = {
        "product_setting": "SINGLE",
    }
    product_platform_id = get_path(config, "resolved_ids.product_platform_id")
    unique_product_id = get_path(config, "resolved_ids.unique_product_id")
    if not is_missing(unique_product_id):
        related_product["unique_product_id"] = api_integer_if_decimal(unique_product_id)
    elif not is_missing(product_id) and not is_missing(product_platform_id):
        related_product["products"] = [
            {
                "product_id": product_id,
                "product_platform_id": product_platform_id,
            }
        ]
    elif not is_missing(product_id):
        related_product["product_id"] = product_id
    if is_missing(unique_product_id) and not is_missing(product_platform_id):
        related_product["product_platform_id"] = product_platform_id

    audience = {
        "district": defaults["district"],
        "region_version": defaults["region_version"],
        "city": get_path(config, "resolved_ids.city_ids", []),
        "location_type": defaults["location_type"],
        "gender": defaults["gender"],
    }
    ages = defaults.get("ages")
    if not is_missing(ages):
        audience["age"] = ages
    hide_if_converted = defaults.get("hide_if_converted")
    if not is_missing(hide_if_converted):
        if hide_if_converted == "UNLIMITED":
            hide_if_converted = "NO_EXCLUDE"
        audience["hide_if_converted"] = hide_if_converted

    delivery_setting = {
        "schedule_type": defaults["schedule_type"],
        "budget_mode": defaults["budget_mode"],
        "budget": budget,
        "bid_type": defaults.get("bid_type", "CUSTOM"),
        "pricing": defaults["pricing"],
    }
    deep_external_action = defaults.get("deep_external_action")
    deep_bid_type = defaults.get("deep_bid_type")
    if cpa_bid is not None:
        delivery_setting["cpa_bid"] = cpa_bid
    roi_goal = getattr(args, "roi_goal", None)
    if roi_goal is None:
        roi_goal = defaults.get("roi_goal")
    if deep_bid_type != "DEEP_BID_DEFAULT" and roi_goal is not None:
        delivery_setting["roi_goal"] = roi_goal
    if deep_bid_type:
        delivery_setting["deep_bid_type"] = deep_bid_type

    optimize_goal = {}
    external_action = defaults.get("external_action")
    if external_action:
        optimize_goal["external_action"] = external_action
    if deep_external_action:
        optimize_goal["deep_external_action"] = deep_external_action
    asset_ids = get_path(config, "resolved_ids.event_asset_ids", [])
    if asset_ids:
        optimize_goal["asset_ids"] = asset_ids

    project_payload = {
        "advertiser_id": advertiser_id,
        "name": project_name,
        "operation": defaults["operation"],
        "delivery_mode": defaults["delivery_mode"],
        "landing_type": defaults["landing_type"],
        "asset_type": defaults.get("asset_type", "THIRDPARTY"),
        "marketing_goal": defaults["marketing_goal"],
        "ad_type": defaults["ad_type"],
        "related_product": related_product,
        "delivery_range": {
            "inventory_catalog": "UNIVERSAL_SMART"
        },
        "delivery_setting": delivery_setting,
        "optimize_goal": optimize_goal,
        "audience": audience,
        "track_url_setting": {
            "track_url": get_path(config, "tracking_urls.track_url", []),
            "action_track_url": get_path(config, "tracking_urls.action_track_url", []),
        },
    }

    product_info_defaults = defaults.get("product_info", {})
    product_info = {
        "titles": product_info_defaults.get("titles") or [product_name],
        "selling_points": product_info_defaults.get("selling_points") or [product_name],
    }
    product_name_type = product_info_defaults.get("product_name_type")
    product_image_type = product_info_defaults.get("product_image_type")
    product_selling_point_type = product_info_defaults.get("product_selling_point_type")
    if product_name_type:
        product_info["product_name_type"] = product_name_type
        if product_name_type == "DPA":
            product_info.pop("titles", None)
    if product_image_type:
        product_info["product_image_type"] = product_image_type
    if product_selling_point_type:
        product_info["product_selling_point_type"] = product_selling_point_type
        if product_selling_point_type == "DPA":
            product_info.pop("selling_points", None)

    product_image_ids = get_path(config, "resolved_ids.product_image_ids", [])
    if product_image_type == "DPA":
        product_info["product_image_fields"] = product_info_defaults.get("product_image_fields", ["product_image"])
    elif product_image_ids:
        product_info["image_ids"] = product_image_ids
    if product_name_type == "DPA":
        product_info["product_name_fields"] = product_info_defaults.get("product_name_fields", ["name"])
    if product_selling_point_type == "DPA":
        product_info["product_selling_point_fields"] = product_info_defaults.get("product_selling_point_fields", ["selling_points"])

    video_cover_ids = get_path(config, "materials.video_cover_ids", [])
    if isinstance(video_cover_ids, dict):
        video_cover_ids_by_id = video_cover_ids
        video_cover_ids = []
    else:
        video_cover_ids_by_id = {}

    video_material_list = []
    for index, video_id in enumerate(videos):
        video_material = {
            "video_id": video_id,
            "image_mode": defaults["video_image_mode"],
        }
        video_cover_id = video_cover_ids_by_id.get(str(video_id))
        if not video_cover_id and index < len(video_cover_ids):
            video_cover_id = video_cover_ids[index]
        if not is_missing(video_cover_id):
            video_material["video_cover_id"] = video_cover_id
        video_material_list.append(video_material)

    promotion_materials = {
        "video_material_list": video_material_list,
        "title_material_list": [{"title": title} for title in config["titles"]],
        "external_url_material_list": [get_path(config, "links.landing_page_url", "")],
        "open_url_type": defaults.get("open_url_type", "CUSTOM"),
        "open_url": get_path(config, "links.open_url", ""),
        "component_material_list": [],
        "product_info": product_info,
    }
    call_to_action_buttons = defaults.get("call_to_action_buttons", [])
    if call_to_action_buttons:
        promotion_materials["call_to_action_buttons"] = call_to_action_buttons

    brand_info = get_path(config, "resolved_ids.brand_info", {})
    clean_brand_info = {k: v for k, v in brand_info.items() if not is_missing(v)}

    promotion_payload = {
        "advertiser_id": advertiser_id,
        "project_id": args.project_id or "{{project_id}}",
        "name": promotion_name,
        "operation": defaults["operation"],
        "source": defaults["source"],
        "promotion_materials": promotion_materials,
    }
    if not is_missing(unique_product_id):
        promotion_payload["promotion_related_product"] = [
            {
                "unique_product_id": api_integer_if_decimal(unique_product_id),
            }
        ]
    if clean_brand_info:
        promotion_payload["brand_info"] = clean_brand_info

    return project_payload, promotion_payload


def missing_fields(config, project_payload, promotion_payload, submit):
    missing = []
    if contains_unresolved_value(get_path(config, "api.base_url")):
        missing.append("api.base_url")
    api_token = get_path(config, "api.access_token")
    if submit and is_missing(api_token):
        missing.append("api.access_token")
    if is_missing(project_payload.get("advertiser_id")):
        missing.append("advertiser_id")
    if contains_unresolved_value(project_payload.get("name")):
        missing.append("defaults.project_name_template")
    if contains_unresolved_value(promotion_payload.get("name")):
        missing.append("defaults.promotion_name_template")
    if contains_unresolved_value(project_payload["track_url_setting"]["track_url"]):
        missing.append("tracking_urls.track_url")
    if contains_unresolved_value(project_payload["track_url_setting"]["action_track_url"]):
        missing.append("tracking_urls.action_track_url")
    if is_missing(project_payload["audience"]["city"]):
        missing.append("resolved_ids.city_ids")
    unique_product_id = get_path(config, "resolved_ids.unique_product_id")
    if is_missing(unique_product_id) and is_missing(get_path(config, "resolved_ids.product_platform_id")):
        missing.append("resolved_ids.product_platform_id")
    if is_missing(promotion_payload["promotion_materials"]["video_material_list"]):
        missing.append("materials.video_ids")
    product_image_type = get_path(config, "defaults.product_info.product_image_type")
    if product_image_type != "DPA" and is_missing(get_path(config, "resolved_ids.product_image_ids")):
        missing.append("resolved_ids.product_image_ids")
    if contains_unresolved_value(get_path(config, "links.landing_page_url")):
        missing.append("links.landing_page_url")
    if contains_unresolved_value(get_path(config, "links.open_url")):
        missing.append("links.open_url")
    if contains_unresolved_value(promotion_payload.get("source")):
        missing.append("defaults.source")
    titles = promotion_payload["promotion_materials"].get("title_material_list") or []
    if not titles or any(contains_unresolved_value(item.get("title")) for item in titles):
        missing.append("titles")
    selling_points = get_path(
        promotion_payload,
        "promotion_materials.product_info.selling_points",
        [],
    )
    if selling_points and any(
        not 6 <= official_text_positions(value) <= 9
        for value in selling_points
    ):
        missing.append("defaults.product_info.selling_points")
    product_id = get_path(config, "defaults.product_id")
    if is_missing(unique_product_id) and contains_unresolved_value(product_id):
        missing.append("defaults.product_id")
    return missing


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", type=int)
    parser.add_argument("--budget", type=float)
    parser.add_argument("--bid", type=float)
    parser.add_argument("--roi-goal", type=float)
    parser.add_argument("--video-id", action="append")
    parser.add_argument("--material-date")
    parser.add_argument("--product-name")
    parser.add_argument("--product-id")
    parser.add_argument("--project-name")
    parser.add_argument("--promotion-name")
    parser.add_argument("--project-id")
    parser.add_argument("--plan-template")
    parser.add_argument("--promotion-only", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    token_manager.add_authorization_arguments(parser)
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    raw_config = channels.runtime_config(raw_config, channel=args.channel, capability="create")
    submit_failed = False
    try:
        config = plan_templates.apply(
            raw_config,
            args.plan_template,
            advertiser_id=args.advertiser_id,
            channel=args.channel,
        )
        if get_path(config, "material_strategy.source_type") == "CREATOR_AUTHORIZED":
            raise ValueError(
                "creator-authorized material templates must use create_creator_plan.py"
            )
        if args.submit:
            raw_config = token_manager.ensure_access_token(
                config_path,
                raw_config,
                channel=args.channel,
                advertiser_id=args.advertiser_id,
                auth_account_id=args.auth_account_id,
            )
            config = plan_templates.apply(
                raw_config,
                args.plan_template,
                advertiser_id=args.advertiser_id,
                channel=args.channel,
            )
    except ValueError as exc:
        print(json.dumps({
            "mode": "submit" if args.submit else "dry_run",
            "config": str(config_path),
            "error": str(exc),
            "available_plan_templates": sorted((raw_config.get("plan_templates") or {}).keys()),
        }, ensure_ascii=False, indent=2))
        return 2
    project_payload, promotion_payload = build_payloads(config, args)
    missing = missing_fields(config, project_payload, promotion_payload, args.submit)

    delivery_setting = project_payload["delivery_setting"]
    preflight_summary = {
        "advertiser_id": project_payload["advertiser_id"],
        "project_name": project_payload["name"],
        "promotion_name": promotion_payload["name"],
        "budget": delivery_setting.get("budget"),
        "cpa_bid": delivery_setting.get("cpa_bid"),
        "city_count": len(project_payload["audience"].get("city") or []),
        "video_count": len(promotion_payload["promotion_materials"].get("video_material_list") or []),
        "operation": project_payload["operation"],
    }
    if delivery_setting.get("roi_goal") is not None:
        preflight_summary["roi_goal"] = delivery_setting["roi_goal"]

    result = {
        "mode": "submit" if args.submit else "dry_run",
        "selected_plan_template": config.get("_selected_plan_template"),
        "project_endpoint": "/v3.0/project/create/",
        "promotion_endpoint": "/v3.0/promotion/create/",
        "project_payload": project_payload,
        "promotion_payload": promotion_payload,
        "missing_fields": missing,
        "preflight_summary": preflight_summary,
    }

    if args.submit:
        blocking = missing_fields(config, project_payload, promotion_payload, True)
        execution = PlanExecutor.from_credentials(
            get_path(config, "api.base_url"),
            get_path(config, "api.access_token"),
        ).execute(PlanExecutionRequest(
            project_payload=project_payload,
            promotion_payload=promotion_payload,
            submit=True,
            project_id=args.project_id,
            promotion_only=args.promotion_only,
            blocking_fields=tuple(blocking),
        ))
        result.update(execution)
        submit_failed = bool(result.get("submit_failed"))

    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)
    return 1 if args.submit and (result.get("submit_blocked") or submit_failed) else 0


if __name__ == "__main__":
    raise SystemExit(main())
