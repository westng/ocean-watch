#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import json
from decimal import Decimal, InvalidOperation
from pathlib import Path

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import QianchuanClientFactory, qianchuan_advertiser_lock_path
from ocean_watch.core.data import get_path, is_missing
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.process_lock import ProcessLock
from ocean_watch.plans.qianchuan_executor import (
    QianchuanPlanExecutionRequest,
    QianchuanPlanExecutor,
)
from ocean_watch.templates import qianchuan_live_templates, qianchuan_product_templates

MARKETING_GOALS = {"LIVE_PROM_GOODS", "VIDEO_PROM_GOODS"}
SMART_BID_TYPES = {"SMART_BID_CUSTOM", "SMART_BID_CONSERVATIVE"}
SCHEDULE_TYPES = {"SCHEDULE_FROM_NOW", "SCHEDULE_START_END"}
DEEP_EXTERNAL_ACTIONS = {
    "AD_CONVERT_TYPE_LIVE_PAY_ROI",
    "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
}
QCPX_MODES = {"QCPX_MODE_OFF", "QCPX_MODE_ON"}
CHANNEL_TYPES = {"SHOP_SELL", "STAR_SELL"}
VIDEO_IMAGE_MODES = {"VIDEO_LARGE", "VIDEO_VERTICAL"}
PRODUCT_IMAGE_MODES = {"SQUARE"}
TITLE_TYPES = {"COMMODITY_CARD", "CUSTOM"}
TOP_LEVEL_FIELDS = {
    "advertiser_id",
    "name",
    "aweme_id",
    "marketing_goal",
    "product_ids",
    "product_channel_info",
    "delivery_setting",
    "creative_setting",
    "programmatic_creative_media_list",
    "multi_product_creative_list",
}


def api_integer(value, field):
    if isinstance(value, bool):
        raise ConfigurationError(f"{field} must be a positive integer")
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    return int(text)


def decimal_value(value, field, positive=True):
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError):
        raise ConfigurationError(f"{field} must be a number") from None
    if not parsed.is_finite() or (positive and parsed <= 0):
        raise ConfigurationError(f"{field} must be greater than zero")
    exponent = parsed.as_tuple().exponent
    if not isinstance(exponent, int) or exponent < -2:
        raise ConfigurationError(f"{field} supports at most two decimal places")
    return parsed


def official_character_length(value):
    return sum(1 if ord(character) < 128 else 2 for character in str(value or ""))


def require_mapping(value, field, errors):
    if not isinstance(value, dict):
        errors.append(field)
        return {}
    return value


def require_list(value, field, errors):
    if not isinstance(value, list):
        errors.append(field)
        return []
    return value


def validate_date_range(setting, schedule_field, errors, today=None):
    schedule_type = setting.get(schedule_field)
    if schedule_type is not None and schedule_type not in SCHEDULE_TYPES:
        errors.append(f"delivery_setting.{schedule_field}")
    if schedule_type != "SCHEDULE_START_END":
        return
    start = setting.get("start_time")
    end = setting.get("end_time")
    try:
        start_date = dt.date.fromisoformat(str(start))
    except (TypeError, ValueError):
        errors.append("delivery_setting.start_time")
        start_date = None
    try:
        end_date = dt.date.fromisoformat(str(end))
    except (TypeError, ValueError):
        errors.append("delivery_setting.end_time")
        end_date = None
    today = today or dt.date.today()
    if start_date and start_date < today:
        errors.append("delivery_setting.start_time")
    if start_date and end_date and end_date < start_date:
        errors.append("delivery_setting.end_time")


def validate_video_materials(materials, field, errors, homepage_only_vertical=False):
    for index, material in enumerate(require_list(materials, field, errors)):
        item_field = f"{field}[{index}]"
        if not isinstance(material, dict):
            errors.append(item_field)
            continue
        video_id = material.get("video_id")
        aweme_item_id = material.get("aweme_item_id")
        if is_missing(video_id) and is_missing(aweme_item_id):
            errors.append(f"{item_field}.video_id|aweme_item_id")
        image_mode = material.get("image_mode")
        if image_mode not in VIDEO_IMAGE_MODES:
            errors.append(f"{item_field}.image_mode")
        if homepage_only_vertical and not is_missing(aweme_item_id) and image_mode != "VIDEO_VERTICAL":
            errors.append(f"{item_field}.image_mode")


def validate_product_creatives(payload, errors):
    product_ids = {str(value) for value in payload.get("product_ids") or []}
    raw_creatives = payload.get("multi_product_creative_list")
    if raw_creatives is None:
        return
    creatives = require_list(raw_creatives, "multi_product_creative_list", errors)
    if len(creatives) > 30:
        errors.append("multi_product_creative_list")
    for index, creative in enumerate(creatives):
        field = f"multi_product_creative_list[{index}]"
        if not isinstance(creative, dict):
            errors.append(field)
            continue
        product_id = creative.get("product_id")
        if is_missing(product_id) or str(product_id) not in product_ids:
            errors.append(f"{field}.product_id")
        if creative.get("creative_type") not in {None, "PROGRAMMATIC_CREATIVE"}:
            errors.append(f"{field}.creative_type")
        video_materials = creative.get("video_material") or []
        image_materials = creative.get("image_material") or []
        carousel_materials = creative.get("carousel_material") or []
        if len(video_materials) + len(image_materials) + len(carousel_materials) > 100:
            errors.append(field)
        if video_materials:
            validate_video_materials(video_materials, f"{field}.video_material", errors)
        for image_index, image in enumerate(image_materials):
            image_field = f"{field}.image_material[{image_index}]"
            if not isinstance(image, dict) or image.get("image_mode") not in PRODUCT_IMAGE_MODES:
                errors.append(f"{image_field}.image_mode")
            if not isinstance(image, dict) or len(image.get("image_ids") or []) != 1:
                errors.append(f"{image_field}.image_ids")
        titles = creative.get("title_material") or []
        if len(titles) > 30:
            errors.append(f"{field}.title_material")
        for title_index, title in enumerate(titles):
            title_field = f"{field}.title_material[{title_index}]"
            if not isinstance(title, dict):
                errors.append(title_field)
                continue
            length = official_character_length(title.get("title"))
            if not 10 <= length <= 110:
                errors.append(f"{title_field}.title")
            if title.get("title_type") is not None and title.get("title_type") not in TITLE_TYPES:
                errors.append(f"{title_field}.title_type")
            if len(title.get("dynamic_words") or []) > 2:
                errors.append(f"{title_field}.dynamic_words")
        if image_materials and not any(
            isinstance(title, dict) and title.get("title_type") == "COMMODITY_CARD"
            for title in titles
        ):
            errors.append(f"{field}.title_material.COMMODITY_CARD")
        creative_card = creative.get("creative_card")
        if isinstance(creative_card, dict):
            for point_index, point in enumerate(
                creative_card.get("promotion_card_selling_points") or []
            ):
                if not 11 <= official_character_length(point) <= 18:
                    errors.append(
                        f"{field}.creative_card.promotion_card_selling_points[{point_index}]"
                    )


def validate_live_creatives(payload, errors):
    creative = payload.get("creative_setting")
    if creative is not None and not isinstance(creative, dict):
        errors.append("creative_setting")
    programmatic = payload.get("programmatic_creative_media_list")
    if programmatic is None:
        return
    programmatic = require_mapping(
        programmatic,
        "programmatic_creative_media_list",
        errors,
    )
    videos = programmatic.get("video_material") or []
    if videos:
        validate_video_materials(
            videos,
            "programmatic_creative_media_list.video_material",
            errors,
            homepage_only_vertical=True,
        )
    for index, title in enumerate(programmatic.get("title_material") or []):
        if not isinstance(title, dict) or is_missing(title.get("title")):
            errors.append(f"programmatic_creative_media_list.title_material[{index}].title")


def normalize_and_validate(payload, advertiser_id=None, today=None):
    if not isinstance(payload, dict):
        raise ConfigurationError("Qianchuan plan payload must be a JSON object")
    normalized = copy.deepcopy(payload)
    unknown = sorted(set(normalized) - TOP_LEVEL_FIELDS)
    if unknown:
        raise ConfigurationError("Qianchuan plan payload contains unknown fields", {"fields": unknown})

    if advertiser_id is not None:
        override = api_integer(advertiser_id, "advertiser_id")
        current = normalized.get("advertiser_id")
        if current is not None and api_integer(current, "advertiser_id") != override:
            raise ConfigurationError("payload advertiser_id does not match --advertiser-id")
        normalized["advertiser_id"] = override
    normalized["advertiser_id"] = api_integer(
        normalized.get("advertiser_id"),
        "advertiser_id",
    )

    errors = []
    goal = normalized.get("marketing_goal")
    if goal not in MARKETING_GOALS:
        errors.append("marketing_goal")
    delivery = require_mapping(normalized.get("delivery_setting"), "delivery_setting", errors)
    bid_type = delivery.get("smart_bid_type")
    if bid_type not in SMART_BID_TYPES:
        errors.append("delivery_setting.smart_bid_type")
    try:
        budget = decimal_value(delivery.get("budget"), "delivery_setting.budget")
        delivery["budget"] = float(budget)
    except ConfigurationError:
        errors.append("delivery_setting.budget")
    roi_goal = delivery.get("roi2_goal")
    if bid_type == "SMART_BID_CUSTOM":
        try:
            roi_goal = decimal_value(roi_goal, "delivery_setting.roi2_goal")
            delivery["roi2_goal"] = float(roi_goal)
        except ConfigurationError:
            errors.append("delivery_setting.roi2_goal")
    elif bid_type == "SMART_BID_CONSERVATIVE" and roi_goal is not None:
        errors.append("delivery_setting.roi2_goal")
    if delivery.get("qcpx_mode") is not None and delivery.get("qcpx_mode") not in QCPX_MODES:
        errors.append("delivery_setting.qcpx_mode")
    if (
        delivery.get("deep_external_action") is not None
        and delivery.get("deep_external_action") not in DEEP_EXTERNAL_ACTIONS
    ):
        errors.append("delivery_setting.deep_external_action")

    if goal == "VIDEO_PROM_GOODS":
        product_ids = require_list(normalized.get("product_ids"), "product_ids", errors)
        if not product_ids or len(product_ids) > 30:
            errors.append("product_ids")
        else:
            try:
                normalized["product_ids"] = [
                    api_integer(value, f"product_ids[{index}]")
                    for index, value in enumerate(product_ids)
                ]
            except ConfigurationError:
                errors.append("product_ids")
        if normalized.get("name") is not None:
            name_length = official_character_length(normalized["name"])
            if not 1 <= name_length <= 100:
                errors.append("name")
        validate_date_range(delivery, "video_schedule_type", errors, today=today)
        validate_product_creatives(normalized, errors)
    elif goal == "LIVE_PROM_GOODS":
        if is_missing(normalized.get("aweme_id")):
            errors.append("aweme_id")
        else:
            try:
                normalized["aweme_id"] = api_integer(normalized["aweme_id"], "aweme_id")
            except ConfigurationError:
                errors.append("aweme_id")
        if normalized.get("name") is not None:
            errors.append("name")
        validate_date_range(delivery, "live_schedule_type", errors, today=today)
        daily_delivery_time = delivery.get("daily_delivery_time")
        if daily_delivery_time is not None:
            if bid_type != "SMART_BID_CONSERVATIVE":
                errors.append("delivery_setting.daily_delivery_time")
            else:
                try:
                    duration = Decimal(str(daily_delivery_time))
                    if duration < Decimal("0.5") or duration > 24 or duration % Decimal("0.5"):
                        errors.append("delivery_setting.daily_delivery_time")
                except InvalidOperation:
                    errors.append("delivery_setting.daily_delivery_time")
        validate_live_creatives(normalized, errors)

    if normalized.get("product_channel_info") is not None:
        for index, item in enumerate(require_list(
            normalized["product_channel_info"],
            "product_channel_info",
            errors,
        )):
            field = f"product_channel_info[{index}]"
            if not isinstance(item, dict):
                errors.append(field)
                continue
            if item.get("channel_type") not in CHANNEL_TYPES:
                errors.append(f"{field}.channel_type")
            for identifier in ("product_id", "channel_id"):
                try:
                    item[identifier] = api_integer(item.get(identifier), f"{field}.{identifier}")
                except ConfigurationError:
                    errors.append(f"{field}.{identifier}")

    blocking_fields = tuple(dict.fromkeys(errors))
    return normalized, blocking_fields


def load_payload(payload_file=None, payload_json=None):
    if bool(payload_file) == bool(payload_json):
        raise ConfigurationError("provide exactly one of --payload-file or --payload-json")
    if payload_json:
        source = payload_json
    elif payload_file == "-":
        import sys

        source = sys.stdin.read()
    elif payload_file:
        source = Path(payload_file).expanduser().read_text(encoding="utf-8")
    else:
        raise ConfigurationError("payload_file is required")
    try:
        return json.loads(source)
    except json.JSONDecodeError as error:
        raise ConfigurationError("Qianchuan plan payload is not valid JSON") from error


def load_payload_source(args, config):
    sources = [
        bool(args.payload_file),
        bool(args.payload_json),
        bool(args.plan_template),
        bool(args.live_template),
    ]
    if sum(sources) != 1:
        raise ConfigurationError(
            "provide exactly one of --payload-file, --payload-json, --plan-template, "
            "or --live-template"
        )
    if args.plan_template:
        template = qianchuan_product_templates.resolve_template(
            config,
            args.plan_template,
        )
        return (
            qianchuan_product_templates.payload_from_template(template, name=args.name),
            {
                "template_id": template["template_id"],
                "name": template["display_name"],
                "product_name": template["bindings"]["product_name"],
                "template_type": qianchuan_product_templates.TEMPLATE_TYPE,
            },
        )
    if args.live_template:
        if args.name:
            raise ConfigurationError("Qianchuan live plans do not accept --name")
        template = qianchuan_live_templates.resolve_template(
            config,
            args.live_template,
        )
        return (
            qianchuan_live_templates.payload_from_template(template),
            {
                "template_id": template["template_id"],
                "name": template["display_name"],
                "creator_name": template["bindings"]["creator_name"],
                "template_type": qianchuan_live_templates.TEMPLATE_TYPE,
            },
        )
    if args.name:
        raise ConfigurationError("--name is supported only with --plan-template")
    return load_payload(args.payload_file, args.payload_json), None


def preflight_summary(payload):
    creatives = payload.get("multi_product_creative_list") or []
    video_count = sum(len(item.get("video_material") or []) for item in creatives if isinstance(item, dict))
    image_count = sum(len(item.get("image_material") or []) for item in creatives if isinstance(item, dict))
    carousel_count = sum(len(item.get("carousel_material") or []) for item in creatives if isinstance(item, dict))
    delivery = payload.get("delivery_setting") or {}
    return {
        "advertiser_id": str(payload.get("advertiser_id")),
        "marketing_goal": payload.get("marketing_goal"),
        "name": payload.get("name"),
        "aweme_id": str(payload.get("aweme_id")) if payload.get("aweme_id") else None,
        "product_count": len(payload.get("product_ids") or []),
        "budget": delivery.get("budget"),
        "smart_bid_type": delivery.get("smart_bid_type"),
        "roi2_goal": delivery.get("roi2_goal"),
        "video_count": video_count,
        "image_count": image_count,
        "carousel_count": carousel_count,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Create one Qianchuan all-domain plan.")
    parser.add_argument("--config")
    parser.add_argument("--payload-file")
    parser.add_argument("--payload-json")
    parser.add_argument("--plan-template")
    parser.add_argument("--live-template")
    parser.add_argument("--name")
    parser.add_argument("--advertiser-id")
    parser.add_argument("--auth-account-id")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    payload, template_summary = load_payload_source(args, raw_config)
    payload, blocking_fields = normalize_and_validate(
        payload,
        advertiser_id=args.advertiser_id,
    )
    if (
        template_summary
        and template_summary.get("template_type")
        == qianchuan_product_templates.TEMPLATE_TYPE
        and not payload.get("multi_product_creative_list")
    ):
        blocking_fields = (*blocking_fields, "runtime_creator_materials")
    runtime = channels.runtime_config(
        raw_config,
        channel="qianchuan",
        capability="qianchuan_create",
    )

    if args.submit and not blocking_fields:
        runtime = token_manager.ensure_access_token(
            config_path,
            runtime,
            channel="qianchuan",
            advertiser_id=str(payload["advertiser_id"]),
            auth_account_id=args.auth_account_id,
        )

    if args.submit and not blocking_fields:
        client_factory = QianchuanClientFactory(
            authorization_store.state_root(),
            str(payload["advertiser_id"]),
        )
        executor = QianchuanPlanExecutor.from_credentials(
            get_path(runtime, "api.base_url"),
            get_path(runtime, "api.access_token"),
            client_factory=client_factory.client,
        )
    else:
        executor = QianchuanPlanExecutor(None)
    request = QianchuanPlanExecutionRequest(
        payload=payload, submit=args.submit, blocking_fields=blocking_fields,
    )
    if args.submit and not blocking_fields:
        with ProcessLock(qianchuan_advertiser_lock_path(
            authorization_store.state_root(),
            str(payload["advertiser_id"]),
        )):
            result = executor.execute(request)
    else:
        result = executor.execute(request)
    output = {
        "mode": "submit" if args.submit else "dry_run",
        "channel": "qianchuan",
        "config": str(config_path),
        "plan_template": template_summary,
        "preflight": preflight_summary(payload),
        "blocking_fields": list(blocking_fields),
        **result,
    }
    write_json(output, destination=args.out)
    return 1 if args.submit and (result.get("submit_blocked") or result.get("submit_failed")) else 0


if __name__ == "__main__":
    raise SystemExit(main())
