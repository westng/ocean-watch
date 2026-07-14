#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import get_json as request_json
from ocean_watch.core.data import get_path


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--out")
    parser.add_argument("--asset-id", type=int)
    parser.add_argument("--external-action")
    parser.add_argument("--deep-external-action")
    parser.add_argument("--delivery-mode")
    parser.add_argument("--landing-type")
    parser.add_argument("--ad-type")
    parser.add_argument("--marketing-goal")
    parser.add_argument("--product-setting", default="SINGLE")
    parser.add_argument("--value-optimized-type")
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
        "asset_id": args.asset_id or (get_path(config, "resolved_ids.event_asset_ids", [None]) or [None])[0],
        "external_action": args.external_action or defaults.get("external_action"),
        "deep_external_action": args.deep_external_action or defaults.get("deep_external_action"),
        "delivery_mode": args.delivery_mode or defaults.get("delivery_mode"),
        "landing_type": args.landing_type or defaults.get("landing_type"),
        "ad_type": args.ad_type or defaults.get("ad_type"),
        "marketing_goal": args.marketing_goal or defaults.get("marketing_goal"),
        "product_setting": args.product_setting,
        "value_optimized_type": args.value_optimized_type or defaults.get("value_optimized_type"),
    }
    response = request_json(
        get_path(config, "api.base_url"),
        get_path(config, "api.access_token"),
        "/v3.0/event_manager/deep_bid_type/get/",
        params,
    )
    result = {
        "endpoint": "/v3.0/event_manager/deep_bid_type/get/",
        "params": params,
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
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
