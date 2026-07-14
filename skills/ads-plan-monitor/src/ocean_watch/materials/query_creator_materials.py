#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
import ocean_watch.materials.creator_materials as creator_materials
import ocean_watch.materials.query_videos as query_videos
from ocean_watch.core.data import get_path


def build_parser():
    parser = argparse.ArgumentParser(description="Query creator-authorized video materials.")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id")
    parser.add_argument("--aweme-id", action="append")
    parser.add_argument(
        "--source",
        choices=("authorized", "homepage"),
        default="authorized",
        help="Query cooperation-authorized videos or public homepage videos.",
    )
    parser.add_argument("--item-id", action="append")
    parser.add_argument("--minimum-remaining-days", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=100)
    parser.add_argument("--include-unusable", action="store_true")
    parser.add_argument("--out")
    token_manager.add_authorization_arguments(parser)
    return parser


def source_type(source):
    return (
        creator_materials.SOURCE_TYPE
        if source == "authorized"
        else "CREATOR_HOMEPAGE"
    )


def main(argv=None):
    args = build_parser().parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    config = channels.runtime_config(raw_config, channel=args.channel, capability="query")
    advertiser_id = args.advertiser_id or get_path(config, "account.advertiser_id")
    config = token_manager.ensure_access_token(
        config_path,
        config,
        channel=args.channel,
        advertiser_id=advertiser_id,
        auth_account_id=args.auth_account_id,
    )

    try:
        if args.source == "homepage":
            if not args.aweme_id or len(args.aweme_id) != 1:
                raise creator_materials.CreatorMaterialError(
                    "homepage_aweme_id_required",
                    "homepage source requires exactly one --aweme-id",
                )
            result = creator_materials.fetch_homepage_videos(
                query_videos.get_json,
                get_path(config, "api.base_url"),
                get_path(config, "api.access_token"),
                advertiser_id,
                args.aweme_id[0],
                page_size=args.page_size,
            )
        else:
            result = creator_materials.fetch_candidates(
                query_videos.get_json,
                get_path(config, "api.base_url"),
                get_path(config, "api.access_token"),
                advertiser_id,
                aweme_ids=args.aweme_id,
                item_ids=args.item_id,
                minimum_remaining_days=args.minimum_remaining_days,
                page_size=args.page_size,
            )
    except creator_materials.CreatorMaterialError as exc:
        output = {
            "source_type": source_type(args.source),
            "error_code": exc.code,
            "error": str(exc),
            "details": exc.details,
        }
        print(json.dumps(output, ensure_ascii=False, indent=2))
        return 1

    candidates = result["candidates"]
    if not args.include_unusable:
        candidates = [candidate for candidate in candidates if candidate["usable"]]
    output = {
        **{key: value for key, value in result.items() if key != "candidates"},
        "source_type": source_type(args.source),
        "candidate_count": len(candidates),
        "candidates": candidates,
    }
    rendered = json.dumps(output, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
