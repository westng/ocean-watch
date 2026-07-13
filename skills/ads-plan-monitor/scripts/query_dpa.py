#!/usr/bin/env python3
import argparse
import json
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import token_manager
import config_paths


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


def request_json(base_url, token, method, path, params=None, payload=None):
    url = base_url.rstrip("/") + path
    if params:
        query = urllib.parse.urlencode(
            {
                key: value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
                for key, value in params.items()
                if value is not None
            }
        )
        url += "?" + query
    data = None
    headers = {"Access-Token": token}
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": body}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--out")
    parser.add_argument(
        "--mode",
        choices=["meta", "dict", "ebp-detail", "asset-detail"],
        required=True,
    )
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=20)
    token_manager.add_authorization_arguments(parser)
    args = parser.parse_args()

    config_path = config_paths.resolve_config_path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(
        config_path,
        config,
        channel=args.channel,
        advertiser_id=(config.get("account") or {}).get("advertiser_id"),
        auth_account_id=args.auth_account_id,
    )
    base_url = get_path(config, "api.base_url")
    token = get_path(config, "api.access_token")
    advertiser_id = get_path(config, "account.advertiser_id")
    platform_id = get_path(config, "resolved_ids.product_platform_id")
    unique_product_id = get_path(config, "resolved_ids.unique_product_id")

    if args.mode == "meta":
        method = "GET"
        path = "/2/dpa/meta/get/"
        params = {
            "advertiser_id": advertiser_id,
            "platform_id": platform_id,
        }
        payload = None
    elif args.mode == "dict":
        method = "GET"
        path = "/2/dpa/dict/get/"
        params = {
            "advertiser_id": advertiser_id,
            "platform_id": platform_id,
        }
        payload = None
    elif args.mode == "ebp-detail":
        method = "GET"
        path = "/v3.0/dpa/ebp/product/detail/get/"
        params = {
            "account_id": advertiser_id,
            "account_type": "EBP",
            "platform_id": platform_id,
            "filtering": {"product_id": str(unique_product_id)},
            "page": args.page,
            "page_size": args.page_size,
        }
        payload = None
    else:
        method = "POST"
        path = "/2/dpa/asset_v2/detail/read/"
        params = None
        payload = {
            "advertiser_id": advertiser_id,
            "asset_ids": [],
            "unique_product_ids": [unique_product_id],
        }

    response = request_json(base_url, token, method, path, params=params, payload=payload)
    result = {
        "endpoint": path,
        "method": method,
        "params": params,
        "payload": payload,
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
