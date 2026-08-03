#!/usr/bin/env python3
import argparse
import json

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import QianchuanClientFactory
from ocean_watch.auth import authorization_store
from ocean_watch.core.data import get_path
from ocean_watch.core.output import write_json
from ocean_watch.materials.qianchuan_creator_accounts import list_authorized_awemes


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="List creators authorized for Qianchuan product all-domain promotion."
    )
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--query", help="Optional official creator search keyword.")
    parser.add_argument("--page-size", type=int, default=100)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--out")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    runtime = channels.runtime_config(
        raw_config,
        channel="qianchuan",
        capability="qianchuan_materials",
    )
    runtime = token_manager.ensure_access_token(
        config_path,
        runtime,
        channel="qianchuan",
        advertiser_id=args.advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    client = QianchuanClientFactory(
        authorization_store.state_root(),
        args.advertiser_id,
    ).client(
        get_path(runtime, "api.base_url"),
        get_path(runtime, "api.access_token"),
    )
    result = list_authorized_awemes(
        client,
        args.advertiser_id,
        page_size=args.page_size,
        max_pages=args.max_pages,
        search_keywords=args.query,
    )
    result["creator_count"] = len(result["creators"])
    write_json(result, destination=args.out)
    return 1 if result["truncated"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
