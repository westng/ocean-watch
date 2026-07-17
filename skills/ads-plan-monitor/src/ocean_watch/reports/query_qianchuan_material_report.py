#!/usr/bin/env python3
import argparse
import datetime as dt
import json
from decimal import Decimal, InvalidOperation

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path, split_csv
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.pagination import declared_page_count
from ocean_watch.core.validation import positive_integer

QIANCHUAN_MATERIAL_REPORT_PATH = "/v1.0/qianchuan/report/material/get/"
DEFAULT_FIELDS = [
    "stat_cost",
    "show_cnt",
    "click_cnt",
    "ctr",
    "convert_cnt",
    "convert_rate",
    "cpa_platform",
    "pay_order_amount",
    "pay_order_count",
    "prepay_and_pay_order_roi",
    "total_play",
    "play_duration_3s_rate",
    "play_over_rate",
]
MATERIAL_TYPES = ("video", "image", "carousel")
MAX_PAGE_SIZE = 100


def iso_date(value, field):
    try:
        return dt.date.fromisoformat(str(value)).isoformat()
    except ValueError as error:
        raise ConfigurationError(f"{field} must use YYYY-MM-DD") from error


def decimal_metric(value, field):
    if value in (None, ""):
        return Decimal("0")
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as error:
        raise ApiError("Qianchuan material report returned a non-numeric metric", {
            "field": field,
            "value": value,
        }) from error
    if not parsed.is_finite():
        raise ApiError("Qianchuan material report returned a non-finite metric", {
            "field": field,
        })
    return parsed


def flatten_material_row(row):
    flattened = {
        key: value
        for key, value in row.items()
        if key != "fields"
    }
    flattened.update(row.get("fields") or {})
    if flattened.get("material_id") is not None:
        flattened["material_id"] = str(flattened["material_id"])
    flattened["related_ad_ids"] = [
        str(value) for value in flattened.get("related_ad_ids") or []
    ]
    return flattened


def integer_total(value, field):
    if value != value.to_integral_value():
        raise ApiError("Qianchuan material report returned a fractional count metric", {
            "field": field,
            "value": str(value),
        })
    return int(value)


def summarize(rows, selected_fields=None):
    available = set(selected_fields) if selected_fields is not None else {
        field
        for row in rows
        for field in row
    }
    spend = (
        sum((decimal_metric(row.get("stat_cost"), "stat_cost") for row in rows), Decimal())
        if "stat_cost" in available
        else None
    )
    gmv = (
        sum(
            (decimal_metric(row.get("pay_order_amount"), "pay_order_amount") for row in rows),
            Decimal(),
        )
        if "pay_order_amount" in available
        else None
    )
    orders = (
        sum(
            (decimal_metric(row.get("pay_order_count"), "pay_order_count") for row in rows),
            Decimal(),
        )
        if "pay_order_count" in available
        else None
    )
    return {
        "total_spend": float(spend) if spend is not None else None,
        "total_pay_order_amount": float(gmv) if gmv is not None else None,
        "total_pay_order_count": (
            integer_total(orders, "pay_order_count")
            if orders is not None
            else None
        ),
        "weighted_roi": (
            float(gmv / spend)
            if gmv is not None and spend is not None and spend
            else None
        ),
        "materials_with_spend": (
            sum(decimal_metric(row.get("stat_cost"), "stat_cost") > 0 for row in rows)
            if spend is not None
            else None
        ),
    }


def query_material_report(
    client,
    advertiser_id,
    start_date,
    end_date,
    *,
    fields=None,
    filtering=None,
    order_field="stat_cost",
    order_type="DESC",
    page_size=MAX_PAGE_SIZE,
    max_pages=100,
):
    advertiser_id = positive_integer(advertiser_id, "advertiser_id")
    page_size = positive_integer(page_size, "page_size", maximum=MAX_PAGE_SIZE)
    max_pages = positive_integer(max_pages, "max_pages")
    rows = []
    request_ids = []
    page = 1
    expected_pages = None
    truncated = False
    while page <= max_pages:
        response = client.get(QIANCHUAN_MATERIAL_REPORT_PATH, params={
            "start_date": start_date,
            "end_date": end_date,
            "advertiser_id": advertiser_id,
            "fields": fields or DEFAULT_FIELDS,
            "order_type": order_type,
            "order_field": order_field,
            "filtering": filtering or None,
            "page": page,
            "page_size": page_size,
        })
        if response.get("code") != 0:
            raise ApiError("Qianchuan material report query failed", {
                "code": response.get("code"),
                "message": response.get("message"),
                "request_id": response.get("request_id"),
                "page": page,
            })
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        page_rows = get_path(response, "data.list", []) or []
        if not isinstance(page_rows, list):
            raise ApiError("Qianchuan material report rows must be a list", {"page": page})
        rows.extend(flatten_material_row(row) for row in page_rows)
        total_pages = declared_page_count(
            get_path(response, "data.page_info"),
            source="qianchuan_material_report",
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
        "rows": rows,
        "page_count": page if not truncated else max_pages,
        "request_ids": request_ids,
        "truncated": truncated,
    }


def main(argv=None):
    today = dt.date.today().isoformat()
    parser = argparse.ArgumentParser(description="Query Qianchuan material performance.")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--start-date", default=today)
    parser.add_argument("--end-date", default=today)
    parser.add_argument("--field", action="append")
    parser.add_argument("--material-id", action="append")
    parser.add_argument("--material-type", choices=MATERIAL_TYPES)
    parser.add_argument("--material-mode", action="append")
    parser.add_argument("--video-source", action="append")
    parser.add_argument("--order-field", default="stat_cost")
    parser.add_argument("--order-type", choices=("ASC", "DESC"), default="DESC")
    parser.add_argument("--page-size", type=int, default=MAX_PAGE_SIZE)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--top", type=int, default=10, help="0 displays every row.")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    start_date = iso_date(args.start_date, "start_date")
    end_date = iso_date(args.end_date, "end_date")
    if start_date > end_date:
        raise ConfigurationError("start_date must not be later than end_date")
    if args.top < 0:
        raise ConfigurationError("top must be zero or a positive integer")
    fields = list(dict.fromkeys(split_csv(args.field) or DEFAULT_FIELDS))
    if args.order_field not in fields:
        fields.append(args.order_field)
    filtering = {}
    material_ids = split_csv(args.material_id)
    if material_ids:
        filtering["material_id"] = [
            positive_integer(value, f"material_id[{index}]")
            for index, value in enumerate(material_ids)
        ]
    if args.material_type:
        filtering["material_type"] = args.material_type
    if args.material_mode:
        filtering["material_mode"] = split_csv(args.material_mode)
    if args.video_source:
        filtering["video_source"] = split_csv(args.video_source)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    runtime = channels.runtime_config(
        raw_config,
        channel="qianchuan",
        capability="qianchuan_report",
    )
    runtime = token_manager.ensure_access_token(
        config_path,
        runtime,
        channel="qianchuan",
        advertiser_id=args.advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    queried = query_material_report(
        OceanEngineClient(
            get_path(runtime, "api.base_url"),
            get_path(runtime, "api.access_token"),
        ),
        args.advertiser_id,
        start_date,
        end_date,
        fields=fields,
        filtering=filtering,
        order_field=args.order_field,
        order_type=args.order_type,
        page_size=args.page_size,
        max_pages=args.max_pages,
    )
    rows = queried.pop("rows")
    displayed = rows[: args.top] if args.top else rows
    result = {
        "mode": "qianchuan_material_report",
        "endpoint": QIANCHUAN_MATERIAL_REPORT_PATH,
        "advertiser_id": str(args.advertiser_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "fields": fields,
        "filters": filtering,
        "summary": summarize(rows, fields),
        "row_count": len(rows),
        "displayed_count": len(displayed),
        "rows": displayed,
        **queried,
    }
    write_json(result, destination=args.out)
    return 1 if result["truncated"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
