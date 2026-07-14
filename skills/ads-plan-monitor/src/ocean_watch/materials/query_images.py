#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import get_json
from ocean_watch.core.data import get_path, split_csv


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--out")
    parser.add_argument("--image-id", action="append")
    parser.add_argument("--material-id", action="append", type=int)
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=20)
    parser.add_argument(
        "--mode",
        choices=["ad-get", "library-get"],
        default="ad-get",
        help="ad-get validates images by id; library-get searches this advertiser's image library.",
    )
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
    image_ids = split_csv(args.image_id)
    if args.mode == "ad-get" and not image_ids:
        image_ids = get_path(config, "resolved_ids.product_image_ids", [])
    base_url = get_path(config, "api.base_url")
    token = get_path(config, "api.access_token")

    if args.mode == "ad-get":
        path = "/2/file/image/ad/get/"
        params = {
            "advertiser_id": get_path(config, "account.advertiser_id"),
            "image_ids": [str(image_id) for image_id in image_ids],
        }
    else:
        path = "/2/file/image/get/"
        filtering = {}
        if image_ids:
            filtering["image_ids"] = [str(image_id) for image_id in image_ids]
        if args.material_id:
            filtering["material_ids"] = args.material_id
        params = {
            "advertiser_id": get_path(config, "account.advertiser_id"),
            "filtering": filtering or None,
            "page": args.page,
            "page_size": args.page_size,
        }

    response = get_json(base_url, token, path, params)
    result = {
        "endpoint": path,
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
