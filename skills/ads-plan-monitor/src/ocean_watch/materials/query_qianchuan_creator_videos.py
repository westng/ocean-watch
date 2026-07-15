#!/usr/bin/env python3
import argparse
import json

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path, is_missing
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.materials.qianchuan_creator_accounts import (
    positive_integer,
    resolve_authorized_aweme,
)
from ocean_watch.templates import qianchuan_product_templates

QIANCHUAN_AWEME_VIDEO_PATH = "/v1.0/qianchuan/file/video/aweme/get/"


def compact_video(item):
    return {
        "aweme_item_id": str(item.get("aweme_item_id"))
        if item.get("aweme_item_id") is not None
        else None,
        "image_mode": item.get("image_mode"),
        "video_id": item.get("video_id"),
        "material_id": str(item.get("material_id"))
        if item.get("material_id") is not None
        else None,
        "title": item.get("title"),
        "video_cover_url": item.get("video_cover_url"),
        "url": item.get("url"),
        "width": item.get("width"),
        "height": item.get("height"),
        "duration": item.get("duration"),
        "is_recommend": item.get("is_recommend"),
        "view_count": item.get("view_cnt"),
        "like_count": item.get("like_cnt"),
        "share_count": item.get("share_cnt"),
        "comment_count": item.get("comment_cnt"),
        "is_ai_create": item.get("is_ai_create"),
    }


def material_key(item):
    return (
        item.get("aweme_item_id")
        or item.get("video_id")
        or item.get("material_id")
        or item.get("url")
    )


def fetch_creator_videos(
    client,
    advertiser_id,
    aweme_id,
    *,
    product_id=None,
    aweme_item_ids=None,
    count=50,
    max_pages=100,
):
    count = positive_integer(count, "count", maximum=50)
    max_pages = positive_integer(max_pages, "max_pages")
    cursor = None
    videos = []
    request_ids = []
    pages = 0
    has_more = False
    filtering = {}
    if product_id is not None:
        filtering["product_id"] = positive_integer(product_id, "product_id")
    if aweme_item_ids is not None:
        normalized_item_ids = [
            positive_integer(value, f"aweme_item_ids[{index}]")
            for index, value in enumerate(aweme_item_ids)
        ]
        if len(normalized_item_ids) > 50:
            raise ConfigurationError("aweme_item_ids must not exceed 50")
        filtering["aweme_item_ids"] = normalized_item_ids
    while pages < max_pages:
        params = {
            "advertiser_id": positive_integer(advertiser_id, "advertiser_id"),
            "aweme_id": positive_integer(aweme_id, "aweme_id"),
            "filtering": filtering,
            "cursor": cursor,
            "count": count,
        }
        response = client.get(QIANCHUAN_AWEME_VIDEO_PATH, params=params)
        pages += 1
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        if response.get("code") != 0:
            raise ApiError(
                "Qianchuan creator video query failed",
                {
                    "code": response.get("code"),
                    "message": response.get("message"),
                    "request_id": response.get("request_id"),
                    "product_id": str(product_id) if product_id is not None else None,
                    "aweme_id": str(aweme_id),
                },
            )
        videos.extend(get_path(response, "data.video_list", []) or [])
        page_info = get_path(response, "data.page_info", {}) or {}
        has_more = bool(page_info.get("has_more"))
        if not has_more:
            break
        next_cursor = page_info.get("cursor")
        if next_cursor is None or next_cursor == cursor:
            raise ApiError(
                "Qianchuan creator video pagination returned an invalid cursor",
                {
                    "product_id": str(product_id) if product_id is not None else None,
                    "aweme_id": str(aweme_id),
                    "cursor": next_cursor,
                },
            )
        cursor = next_cursor
    return {
        "videos": videos,
        "page_count": pages,
        "request_ids": request_ids,
        "truncated": has_more and pages >= max_pages,
    }


def fetch_product_videos(
    client,
    advertiser_id,
    aweme_id,
    product_id,
    count=50,
    max_pages=100,
):
    return fetch_creator_videos(
        client,
        advertiser_id,
        aweme_id,
        product_id=product_id,
        count=count,
        max_pages=max_pages,
    )


def fetch_template_creator_videos(
    authorized_aweme_client,
    video_client,
    template,
    douyin_id,
    creator_name=None,
    count=50,
    max_pages=100,
):
    template = qianchuan_product_templates.validate_business_template(template)
    if is_missing(douyin_id):
        raise ConfigurationError("douyin_id is required")
    bindings = template["bindings"]
    resolved_creator = resolve_authorized_aweme(
        authorized_aweme_client,
        bindings["advertiser_id"],
        douyin_id,
        creator_name=creator_name,
        max_pages=max_pages,
    )
    merged = {}
    query_rows = []
    for product_id in bindings["product_ids"]:
        result = fetch_product_videos(
            video_client,
            bindings["advertiser_id"],
            resolved_creator["aweme_id"],
            product_id,
            count=count,
            max_pages=max_pages,
        )
        matched_count = 0
        for raw_video in result["videos"]:
            video = compact_video(raw_video)
            key = material_key(video)
            if not key:
                continue
            matched_count += 1
            if key not in merged:
                merged[key] = {**video, "matched_product_ids": []}
            if product_id not in merged[key]["matched_product_ids"]:
                merged[key]["matched_product_ids"].append(product_id)
        query_rows.append({
            "product_id": product_id,
            "matched_count": matched_count,
            "page_count": result["page_count"],
            "request_ids": result["request_ids"],
            "truncated": result["truncated"],
        })
    materials = list(merged.values())
    return {
        "endpoint": QIANCHUAN_AWEME_VIDEO_PATH,
        "creator_resolution_endpoint": resolved_creator["endpoint"],
        "advertiser_id": bindings["advertiser_id"],
        "template_id": template["template_id"],
        "template_name": template["display_name"],
        "product_ids": bindings["product_ids"],
        "douyin_id": str(douyin_id),
        "aweme_id": resolved_creator["aweme_id"],
        "creator_name": resolved_creator.get("aweme_name") or creator_name,
        "resolved_creator": resolved_creator,
        "query_count": len(query_rows),
        "material_count": len(materials),
        "queries": query_rows,
        "materials": materials,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Query Qianchuan creator videos filtered by product template."
    )
    parser.add_argument("--config")
    parser.add_argument("--plan-template")
    parser.add_argument("--douyin-id", required=True)
    parser.add_argument("--creator-name")
    parser.add_argument("--auth-account-id")
    parser.add_argument("--page-size", type=int, default=50)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--out")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    template = qianchuan_product_templates.resolve_template(
        raw_config,
        args.plan_template,
    )
    advertiser_id = template["bindings"]["advertiser_id"]
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
        auth_account_id=args.auth_account_id,
    )
    authorized_aweme_client = OceanEngineClient(
        get_path(runtime, "api.base_url"),
        get_path(runtime, "api.access_token"),
    )
    video_client = OceanEngineClient(
        get_path(runtime, "api.legacy_base_url")
        or get_path(runtime, "oauth.token_base_url"),
        get_path(runtime, "api.access_token"),
    )
    result = fetch_template_creator_videos(
        authorized_aweme_client,
        video_client,
        template,
        args.douyin_id,
        creator_name=args.creator_name,
        count=args.page_size,
        max_pages=args.max_pages,
    )
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
