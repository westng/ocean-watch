#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import get_json as request_json
from ocean_watch.core.data import get_path


def find_goal_nodes(value):
    nodes = []
    if isinstance(value, dict):
        has_goal = any(
            key in value
            for key in (
                "external_action",
                "external_action_name",
                "deep_external_action",
                "deep_external_action_name",
                "convert_type",
                "convert_type_name",
                "value",
                "label",
            )
        )
        if has_goal:
            nodes.append(value)
        for child in value.values():
            nodes.extend(find_goal_nodes(child))
    elif isinstance(value, list):
        for item in value:
            nodes.extend(find_goal_nodes(item))
    return nodes


def summarize_goals(response):
    summary = []
    seen = set()
    for node in find_goal_nodes(response.get("data")):
        compact = {
            "external_action": node.get("external_action") or node.get("convert_type") or node.get("value"),
            "external_action_name": node.get("external_action_name") or node.get("convert_type_name") or node.get("label"),
            "deep_external_action": node.get("deep_external_action"),
            "deep_external_action_name": node.get("deep_external_action_name"),
            "deep_bid_type": node.get("deep_bid_type"),
            "pricing": node.get("pricing"),
            "bid_type": node.get("bid_type"),
        }
        compact = {key: value for key, value in compact.items() if value not in (None, "", [])}
        key = json.dumps(compact, ensure_ascii=False, sort_keys=True)
        if compact and key not in seen:
            seen.add(key)
            summary.append(compact)
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--out")
    parser.add_argument("--landing-type")
    parser.add_argument("--ad-type")
    parser.add_argument("--asset-type")
    parser.add_argument("--marketing-goal")
    parser.add_argument("--delivery-mode")
    parser.add_argument("--delivery-type", default="NORMAL")
    parser.add_argument("--asset-id")
    parser.add_argument("--no-asset-id", action="store_true")
    token_manager.add_authorization_arguments(parser)
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(
        config_path,
        config,
        channel=args.channel,
        advertiser_id=(config.get("account") or {}).get("advertiser_id"),
        auth_account_id=args.auth_account_id,
    )
    defaults = config["defaults"]
    params = {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "landing_type": args.landing_type or defaults.get("landing_type"),
        "ad_type": args.ad_type or defaults.get("ad_type"),
        "asset_type": args.asset_type or defaults.get("asset_type"),
        "marketing_goal": args.marketing_goal or defaults.get("marketing_goal"),
        "delivery_mode": args.delivery_mode or defaults.get("delivery_mode"),
        "delivery_type": args.delivery_type,
    }
    if not args.no_asset_id:
        params["asset_id"] = args.asset_id or get_path(config, "resolved_ids.product_platform_id")
    response = request_json(
        get_path(config, "api.base_url"),
        get_path(config, "api.access_token"),
        "/v3.0/event_manager/optimized_goal/get_v2/",
        params,
    )
    result = {
        "endpoint": "/v3.0/event_manager/optimized_goal/get_v2/",
        "params": params,
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "goal_summary": summarize_goals(response),
        "response": response,
    }

    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)
    return 0 if response.get("code") == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
