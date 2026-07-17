#!/usr/bin/env python3
import argparse
import copy

from ocean_watch.auth import authorization_store
from ocean_watch.core import config_paths, config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.templates import qianchuan_live_templates as live_templates
from ocean_watch.templates import template_advertiser_binding


def prompt_value(input_fn, label, default=None, required=False):
    suffix = f" [{default}]" if default not in (None, "") else ""
    while True:
        value = input_fn(f"{label}{suffix}: ").strip()
        if value:
            return value
        if default not in (None, ""):
            return str(default)
        if not required:
            return None


def prompt_yes_no(input_fn, label, default=False):
    hint = "Y/n" if default else "y/N"
    raw = input_fn(f"{label} [{hint}]: ").strip().lower()
    if not raw:
        return default
    return raw in {"y", "yes", "是"}


def select_source(config, input_fn, output_fn):
    rows = live_templates.list_templates(config)
    output_fn("创建来源：")
    output_fn("  0. 千川直播全域默认模板（仅作为创建骨架）")
    for index, row in enumerate(rows, start=1):
        output_fn(
            f"  {index}. {row['name']}（广告主 {row['advertiser_id']}，"
            f"直播账号 {row['creator_name']} / {row['aweme_id']}）"
        )
    while True:
        raw = input_fn("请选择来源编号: ").strip()
        if raw.isdigit() and 0 <= int(raw) <= len(rows):
            if raw == "0":
                normalized = live_templates.ensure_config(config)
                return live_templates.DEFAULT_TEMPLATE_KEY, normalized[
                    live_templates.DEFAULT_TEMPLATE_KEY
                ]
            selected = rows[int(raw) - 1]
            return selected["template_id"], live_templates.resolve_template(
                config,
                selected["template_id"],
            )


def select_bid_type(input_fn, output_fn, inherited):
    choices = (
        ("SMART_BID_CUSTOM", "控成本（目标 ROI）"),
        ("SMART_BID_CONSERVATIVE", "放量（保守出价）"),
    )
    default_index = next(
        (index for index, (value, _) in enumerate(choices) if value == inherited),
        1,
    )
    output_fn("直播出价方式：")
    for index, (_, label) in enumerate(choices):
        suffix = "（当前）" if index == default_index else ""
        output_fn(f"  {index}. {label}{suffix}")
    while True:
        raw = input_fn(f"请选择出价方式编号 [{default_index}]: ").strip()
        if not raw:
            return choices[default_index][0]
        if raw.isdigit() and 0 <= int(raw) < len(choices):
            return choices[int(raw)][0]


def run_create_wizard(config, input_fn=input, output_fn=print, authorization_state=None):
    normalized = live_templates.ensure_config(config)
    source_id, source = select_source(normalized, input_fn, output_fn)
    bindings = source.get("bindings") or {}
    advertiser_id, verification = template_advertiser_binding.prompt_advertiser_id(
        "qianchuan",
        (bindings.get("advertiser_id"),),
        input_fn=input_fn,
        output_fn=output_fn,
        channel_state=authorization_state,
    )
    inherited_name = bindings.get("creator_name")
    if str(inherited_name or "").startswith("REPLACE_WITH"):
        inherited_name = None
    inherited_aweme_id = bindings.get("aweme_id")
    if str(inherited_aweme_id or "").startswith("REPLACE_WITH"):
        inherited_aweme_id = None
    creator_name = prompt_value(
        input_fn,
        "直播账号名称",
        inherited_name,
        required=True,
    )
    aweme_id = prompt_value(
        input_fn,
        "直播账号 numeric aweme_id",
        inherited_aweme_id,
        required=True,
    )
    delivery = copy.deepcopy(source.get("delivery_setting") or {})
    delivery["budget"] = prompt_value(
        input_fn,
        "日预算",
        delivery.get("budget", 5000),
        required=True,
    )
    delivery["smart_bid_type"] = select_bid_type(
        input_fn,
        output_fn,
        delivery.get("smart_bid_type"),
    )
    if delivery["smart_bid_type"] == "SMART_BID_CUSTOM":
        delivery["roi2_goal"] = prompt_value(
            input_fn,
            "目标 ROI",
            delivery.get("roi2_goal", 1.7),
            required=True,
        )
        delivery.pop("daily_delivery_time", None)
    else:
        delivery["daily_delivery_time"] = prompt_value(
            input_fn,
            "每日投放时长（0.5 小时步进）",
            delivery.get("daily_delivery_time", 8.5),
            required=True,
        )
        delivery.pop("roi2_goal", None)
    source = copy.deepcopy(source)
    source["delivery_setting"] = delivery
    candidate = live_templates.build_business_template(
        advertiser_id,
        creator_name,
        aweme_id,
        source=source,
    )
    preview = {
        "source_template": source_id,
        "template": candidate,
        "advertiser_binding_verification": verification,
    }
    write_json(preview, output_fn=output_fn)
    if not prompt_yes_no(input_fn, "确认创建模板", default=False):
        return normalized, {"created": False, "preview": preview}
    templates = normalized.setdefault(live_templates.TEMPLATES_KEY, {})
    if any(
        row.get("display_name") == candidate["display_name"]
        for row in templates.values()
    ):
        raise ConfigurationError(
            "Qianchuan live template already exists",
            {"name": candidate["display_name"]},
        )
    templates[candidate["template_id"]] = candidate
    return normalized, {
        "created": True,
        "template_id": candidate["template_id"],
        "name": candidate["display_name"],
        "template": candidate,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Manage Qianchuan live templates.")
    parser.add_argument("action", choices=("list", "create-wizard", "migrate"))
    parser.add_argument("--config")
    args = parser.parse_args(argv)
    path = config_paths.resolve_config_path(args.config)
    config = config_store.load_json(path)
    revision = config_store.json_revision(config)
    if args.action == "list":
        normalized = live_templates.ensure_config(config)
        result = {
            "default_template": {
                "name": live_templates.DEFAULT_TEMPLATE_KEY,
                "business_usable": False,
                "template": normalized[live_templates.DEFAULT_TEMPLATE_KEY],
            },
            "templates": live_templates.list_templates(normalized),
        }
    elif args.action == "migrate":
        normalized = live_templates.ensure_config(config)
        changed = normalized != config
        if changed:
            config_store.compare_and_swap_json(path, revision, normalized)
        result = {
            "migrated": changed,
            "schema_version": live_templates.SCHEMA_VERSION,
            "templates": live_templates.list_templates(normalized),
        }
    else:
        normalized, result = run_create_wizard(
            config,
            authorization_state=authorization_store.load_channel_state("qianchuan"),
        )
        if result.get("created"):
            config_store.compare_and_swap_json(path, revision, normalized)
    result["config"] = str(path)
    write_json(result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
