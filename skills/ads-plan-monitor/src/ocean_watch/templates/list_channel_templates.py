#!/usr/bin/env python3
import argparse
import json

import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.templates import (
    manage_plan_templates,
    qianchuan_product_templates,
)

CHANNELS = ("marketing", "qianchuan")


def compact_marketing_template(row):
    delivery = row["delivery_settings"]
    source_type = row["material_source_type"]
    return {
        "name": row["name"],
        "advertiser_id": row["advertiser_id"],
        "product_name": row["product_name"],
        "product_id": row["product_id"],
        "template_type": (
            "混剪素材" if source_type == "ACCOUNT_UPLOAD" else "原生素材"
        ),
        "material_source_type": source_type,
        "platform": row["platform"],
        "traffic_source": row["traffic_source"],
        "daily_budget": delivery["daily_budget"],
        "roi_goal": delivery["roi_goal"],
        "gender": delivery["gender"],
        "ages": delivery["ages"],
        "copy_title_count": row["copy_materials"]["title_count"],
        "ready_for_plan_creation": row["binding_error"] is None,
    }


def marketing_channel(config, *, include_details):
    rows = manage_plan_templates.list_templates(config)
    templates = rows if include_details else [compact_marketing_template(row) for row in rows]
    default = manage_plan_templates.default_template_summary(config)
    return {
        "channel": "marketing",
        "display_name": "巨量营销",
        "business_template_count": len(templates),
        "default_skeleton": default if include_details else {
            "name": default["name"],
            "business_usable": False,
        },
        "templates": templates,
    }


def compact_qianchuan_template(template):
    bindings = template["bindings"]
    delivery = template["delivery_setting"]
    return {
        "template_id": template["template_id"],
        "name": template["display_name"],
        "status": template["status"],
        "advertiser_id": bindings["advertiser_id"],
        "product_name": bindings["product_name"],
        "product_ids": bindings["product_ids"],
        "product_count": len(bindings["product_ids"]),
        "template_type": "商品全域",
        "material_source_type": template["material_strategy"]["source_type"],
        "daily_budget": delivery["budget"],
        "roi_goal": delivery["roi2_goal"],
        "smart_bid_type": delivery["smart_bid_type"],
        "deep_external_action": delivery["deep_external_action"],
        "ready_for_plan_creation": template["status"] == "active",
    }


def qianchuan_channel(config, *, include_details):
    normalized = qianchuan_product_templates.ensure_config(config)
    listed = qianchuan_product_templates.list_templates(normalized)
    templates = [
        qianchuan_product_templates.validate_business_template(
            normalized[qianchuan_product_templates.TEMPLATES_KEY][row["template_id"]]
        )
        for row in listed
    ]
    rows = templates if include_details else [
        compact_qianchuan_template(template) for template in templates
    ]
    default = normalized[qianchuan_product_templates.DEFAULT_TEMPLATE_KEY]
    return {
        "channel": "qianchuan",
        "display_name": "巨量千川",
        "business_template_count": len(rows),
        "default_skeleton": (
            {
                "name": qianchuan_product_templates.DEFAULT_TEMPLATE_KEY,
                "business_usable": False,
                "template": default,
            }
            if include_details
            else {
                "name": qianchuan_product_templates.DEFAULT_TEMPLATE_KEY,
                "business_usable": False,
            }
        ),
        "templates": rows,
    }


def list_all_templates(config, *, channel="all", include_details=False):
    selected = CHANNELS if channel == "all" else (channel,)
    builders = {
        "marketing": marketing_channel,
        "qianchuan": qianchuan_channel,
    }
    channels = {
        name: builders[name](config, include_details=include_details)
        for name in selected
    }
    return {
        "ok": True,
        "source": "local_config",
        "summary": {
            "business_template_count": sum(
                item["business_template_count"] for item in channels.values()
            ),
            "default_skeleton_count": len(channels),
            "by_channel": {
                name: item["business_template_count"]
                for name, item in channels.items()
            },
        },
        "channels": channels,
    }


def show_template(config, *, channel, selector):
    if channel == "marketing":
        matches = [
            row
            for row in manage_plan_templates.list_templates(config)
            if row["name"] == selector
        ]
        if not matches:
            raise ConfigurationError(
                "Marketing plan template not found",
                {"channel": channel, "selector": selector},
            )
        template = matches[0]
        ready = template["binding_error"] is None
    else:
        template = qianchuan_product_templates.resolve_template(config, selector)
        ready = template["status"] == "active"
    return {
        "ok": True,
        "source": "local_config",
        "channel": channel,
        "display_name": "巨量营销" if channel == "marketing" else "巨量千川",
        "selector": selector,
        "ready_for_plan_creation": ready,
        "template": template,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="List Marketing and Qianchuan templates from one local config read."
    )
    parser.add_argument("--config")
    parser.add_argument("--channel", choices=("all", *CHANNELS), default="all")
    parser.add_argument("--include-details", action="store_true")
    args = parser.parse_args(argv)
    path = config_paths.resolve_config_path(args.config)
    config = config_store.load_json(path)
    result = list_all_templates(
        config,
        channel=args.channel,
        include_details=args.include_details,
    )
    result["config"] = str(path)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


def show_main(argv=None):
    parser = argparse.ArgumentParser(
        description="Show one Marketing or Qianchuan template from local config."
    )
    parser.add_argument("--config")
    parser.add_argument("--channel", choices=CHANNELS, required=True)
    parser.add_argument("--template", required=True)
    args = parser.parse_args(argv)
    path = config_paths.resolve_config_path(args.config)
    config = config_store.load_json(path)
    result = show_template(
        config,
        channel=args.channel,
        selector=args.template,
    )
    result["config"] = str(path)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
