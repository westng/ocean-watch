#!/usr/bin/env python3
import argparse
import json

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.validation import positive_integer
from ocean_watch.plans.qianchuan_plan_gateway import QianchuanPlanGateway


def compact_plan(row):
    ad_info = row.get("ad_info") or {}
    delivery = row.get("delivery_setting") or ad_info.get("delivery_setting") or {}
    rooms = row.get("room_info") or []
    return {
        "ad_id": str(ad_info.get("id")) if ad_info.get("id") is not None else None,
        "name": ad_info.get("name"),
        "status": ad_info.get("status"),
        "opt_status": ad_info.get("opt_status"),
        "create_time": ad_info.get("create_time"),
        "marketing_goal": ad_info.get("marketing_goal"),
        "creator_ids": [
            str(item["anchor_id"])
            for item in rooms
            if isinstance(item, dict) and item.get("anchor_id") is not None
        ],
        "budget": ad_info.get("budget", delivery.get("budget")),
        "smart_bid_type": ad_info.get(
            "smart_bid_type",
            delivery.get("smart_bid_type"),
        ),
        "roi2_goal": ad_info.get("roi2_goal", delivery.get("roi2_goal")),
    }


def build_gateway(config_path, raw_config, advertiser_id, auth_account_id):
    runtime = channels.runtime_config(
        raw_config,
        channel="qianchuan",
        capability="qianchuan_materials",
    )
    runtime = token_manager.ensure_access_token(
        config_path,
        runtime,
        channel="qianchuan",
        advertiser_id=advertiser_id,
        auth_account_id=auth_account_id,
    )
    return QianchuanPlanGateway(OceanEngineClient(
        get_path(runtime, "api.base_url"),
        get_path(runtime, "api.access_token"),
    ))


def main(argv=None):
    parser = argparse.ArgumentParser(description="Query Qianchuan product all-domain plans.")
    parser.add_argument("action", choices=("list", "show", "materials"))
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--ad-id")
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--top", type=int, default=0, help="0 returns every fetched plan.")
    parser.add_argument("--full", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if args.action in {"show", "materials"} and not args.ad_id:
        raise ConfigurationError("--ad-id is required for this action")
    if args.top < 0:
        raise ConfigurationError("--top must be zero or a positive integer")
    max_pages = positive_integer(args.max_pages, "max_pages")

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    gateway = build_gateway(
        config_path,
        raw_config,
        args.advertiser_id,
        args.auth_account_id,
    )
    if args.action == "list":
        queried = gateway.list_product_plans(
            args.advertiser_id,
            max_pages=max_pages,
        )
        rows = queried["plans"]
        displayed = rows[: args.top] if args.top else rows
        result = {
            "mode": "qianchuan_plan_list",
            "endpoint": "/v1.0/qianchuan/uni_promotion/list/",
            "advertiser_id": str(args.advertiser_id),
            "plan_count": len(rows),
            "displayed_count": len(displayed),
            "plans": displayed if args.full else [compact_plan(row) for row in displayed],
            **{key: value for key, value in queried.items() if key != "plans"},
        }
    elif args.action == "show":
        result = {
            "mode": "qianchuan_plan_detail",
            "endpoint": "/v1.0/qianchuan/uni_promotion/ad/detail/",
            "advertiser_id": str(args.advertiser_id),
            "ad_id": str(args.ad_id),
            "plan": gateway.get_plan_detail(args.advertiser_id, args.ad_id),
        }
    else:
        queried = gateway.list_plan_video_materials(
            args.advertiser_id,
            args.ad_id,
            max_pages=max_pages,
        )
        result = {
            "mode": "qianchuan_plan_materials",
            "endpoint": "/v1.0/qianchuan/uni_promotion/ad/material/get/",
            "advertiser_id": str(args.advertiser_id),
            "ad_id": str(args.ad_id),
            "material_count": len(queried["materials"]),
            **queried,
        }
    write_json(result, destination=args.out)
    return 1 if result.get("truncated") else 0


if __name__ == "__main__":
    raise SystemExit(main())
