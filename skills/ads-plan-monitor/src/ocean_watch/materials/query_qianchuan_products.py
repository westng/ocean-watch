#!/usr/bin/env python3
import argparse
import json

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import QianchuanClientFactory
from ocean_watch.auth import authorization_store
from ocean_watch.core.data import get_path, split_csv
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.pagination import declared_page_count
from ocean_watch.core.validation import positive_integer

QIANCHUAN_PRODUCT_PATH = "/v1.0/qianchuan/uni_promotion/product/get/"
PRODUCT_TABS = (
    "ALL",
    "BREAKTHROUGH_PRODUCT",
    "GOOD_BOOST",
    "NEW_PRODUCT",
    "SEARCH_TREND",
)
ORDER_FIELDS = ("SELL_NUM", "STOCK", "AUDIT_TIME")
PLATFORMS = ("ECP_AWEME", "QIANCHUAN")
MAX_PAGE_SIZE = 100


def compact_product(item):
    return {
        "product_id": str(item.get("id")) if item.get("id") is not None else None,
        "name": item.get("name"),
        "image": item.get("img"),
        "category_name": item.get("category_name"),
        "channel_id": str(item.get("channel_id"))
        if item.get("channel_id") is not None
        else None,
        "channel_type": item.get("channel_type"),
        "sell_num": item.get("sell_num"),
        "stock_num": item.get("stock_num"),
        "audit_time": item.get("audit_time"),
        "square_image_list": item.get("square_image_list") or [],
        "tags": item.get("tag") or [],
        "gray_reasons": item.get("gray_reason") or [],
    }


def query_products(
    client,
    advertiser_id,
    *,
    product_ids=None,
    product_name=None,
    tab="ALL",
    aweme_id=None,
    only_unpromoted=False,
    order_field="AUDIT_TIME",
    order_type="DESC",
    platform=None,
    page_size=MAX_PAGE_SIZE,
    max_pages=100,
):
    advertiser_id = positive_integer(advertiser_id, "advertiser_id")
    page_size = positive_integer(page_size, "page_size", maximum=MAX_PAGE_SIZE)
    max_pages = positive_integer(max_pages, "max_pages")
    filtering = {"tab": tab}
    if product_ids:
        filtering["product_ids"] = [
            positive_integer(value, f"product_ids[{index}]")
            for index, value in enumerate(product_ids)
        ]
    if product_name:
        filtering["product_name"] = str(product_name).strip()
    if only_unpromoted:
        filtering["create_roi2_limit_product"] = True
    rows = []
    request_ids = []
    page = 1
    expected_pages = None
    truncated = False
    while page <= max_pages:
        response = client.get(QIANCHUAN_PRODUCT_PATH, params={
            "advertiser_id": advertiser_id,
            "filtering": filtering,
            "aweme_id": positive_integer(aweme_id, "aweme_id")
            if aweme_id is not None
            else None,
            "order_field": order_field,
            "order_type": order_type,
            "page": page,
            "page_size": page_size,
            "platfrom": platform,
        })
        if response.get("code") != 0:
            raise ApiError("Qianchuan product query failed", {
                "code": response.get("code"),
                "message": response.get("message"),
                "request_id": response.get("request_id"),
                "page": page,
            })
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        page_rows = get_path(response, "data.product_list", []) or []
        if not isinstance(page_rows, list):
            raise ApiError("Qianchuan product rows must be a list", {"page": page})
        rows.extend(page_rows)
        total_pages = declared_page_count(
            get_path(response, "data.page_info"),
            source="qianchuan_product_list",
            page=page,
            row_count=len(page_rows),
            expected=expected_pages,
        )
        expected_pages = total_pages
        if total_pages == 0 or page >= total_pages:
            break
        page += 1
    else:
        truncated = True
    return {
        "endpoint": QIANCHUAN_PRODUCT_PATH,
        "advertiser_id": str(advertiser_id),
        "filters": filtering,
        "product_count": len(rows),
        "products": [compact_product(row) for row in rows],
        "page_count": page if not truncated else max_pages,
        "request_ids": request_ids,
        "truncated": truncated,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="List or search Qianchuan products.")
    parser.add_argument("action", choices=("list", "search"))
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--product-id", action="append")
    parser.add_argument("--name")
    parser.add_argument("--tab", choices=PRODUCT_TABS, default="ALL")
    parser.add_argument("--aweme-id")
    parser.add_argument("--only-unpromoted", action="store_true")
    parser.add_argument("--order-field", choices=ORDER_FIELDS, default="AUDIT_TIME")
    parser.add_argument("--order-type", choices=("ASC", "DESC"), default="DESC")
    parser.add_argument("--platform", choices=PLATFORMS)
    parser.add_argument("--page-size", type=int, default=MAX_PAGE_SIZE)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if args.action == "search" and not (args.product_id or args.name):
        raise ConfigurationError("search requires --product-id or --name")

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
    result = query_products(
        QianchuanClientFactory(
            authorization_store.state_root(),
            args.advertiser_id,
        ).client(
            get_path(runtime, "api.base_url"),
            get_path(runtime, "api.access_token"),
        ),
        args.advertiser_id,
        product_ids=split_csv(args.product_id),
        product_name=args.name,
        tab=args.tab,
        aweme_id=args.aweme_id,
        only_unpromoted=args.only_unpromoted,
        order_field=args.order_field,
        order_type=args.order_type,
        platform=args.platform,
        page_size=args.page_size,
        max_pages=args.max_pages,
    )
    result["mode"] = f"qianchuan_product_{args.action}"
    write_json(result, destination=args.out)
    return 1 if result["truncated"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
