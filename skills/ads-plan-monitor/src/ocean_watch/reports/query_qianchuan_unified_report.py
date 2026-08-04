#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import time

from ocean_watch.api import QianchuanClientFactory
from ocean_watch.auth import authorization_store, token_manager
from ocean_watch.core import config_paths
from ocean_watch.core.data import get_path, split_csv
from ocean_watch.core.errors import ApiError
from ocean_watch.core.output import write_json

ALL_PROMOTION_PATH = "/v1.0/qianchuan/report/all_promotion/get/"
UNI_PROMOTION_PATH = "/v1.0/qianchuan/report/uni_promotion/get/"
CONFIG_PATH = "/v1.0/qianchuan/report/uni_promotion/config/get/"
DATA_PATH = "/v1.0/qianchuan/report/uni_promotion/data/get/"
ROOM_PATH = "/v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/"
AUTHOR_PATH = "/v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/"

ENDPOINTS = {
    "account": ALL_PROMOTION_PATH,
    "uni-account": UNI_PROMOTION_PATH,
    "schema": CONFIG_PATH,
    "custom": DATA_PATH,
    "products": DATA_PATH,
    "rooms": ROOM_PATH,
    "authors": AUTHOR_PATH,
}

PRODUCT_TOPICS = {
    "uni": "SITE_PROMOTION_PRODUCT_PRODUCT",
    "overall": "OVERALL_ROI_PRODUCT_PRODUCT",
}

DEFAULT_ACCOUNT_FIELDS = [
    "stat_cost_for_roi2",
    "stat_cost_for_overall_roi2",
    "total_pay_order_count_for_roi2",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_prepay_and_pay_order_roi2",
    "total_order_settle_amount_for_roi2_1h",
    "total_prepay_and_pay_settle_roi2_1h",
    "total_prepay_and_pay_settle_overall_roi2_1h",
]
DEFAULT_UNI_ACCOUNT_FIELDS = [
    "stat_cost",
    "total_pay_order_count_for_roi2",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_prepay_and_pay_order_roi2",
]
DEFAULT_DIMENSION_FIELDS = [
    "stat_cost",
    "stat_cost_for_roi2",
    "total_pay_order_count_for_roi2",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_prepay_and_pay_order_roi2",
    "total_order_settle_amount_for_roi2_1h",
    "total_prepay_and_pay_settle_roi2_1h",
]
DEFAULT_PRODUCT_METRICS = [
    "stat_cost",
    "stat_cost_for_roi2",
    "stat_cost_for_overall_roi2",
    "total_pay_order_count_for_roi2",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_prepay_and_pay_order_roi2",
    "total_prepay_and_pay_settle_overall_roi2_1h",
]
VALID_DATA_PERIODS = {"ALL_DATA", "OVER_ALL_DATA", "UNI_DATA"}
VALID_PAGE_SIZES = {10, 20, 50, 100, 200}
RETRYABLE_CODES = {"40100", "51010"}
RETRYABLE_HTTP_STATUSES = {408, 425, 429, 500, 502, 503, 504}
MAX_PAGES = 500


def today():
    return dt.date.today().isoformat()


def date_value(value, field):
    try:
        return dt.date.fromisoformat(str(value))
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"{field} must use YYYY-MM-DD") from exc


def unique(values):
    return list(dict.fromkeys(item.strip() for item in values if item and item.strip()))


def parse_filter(value):
    try:
        if value.lstrip().startswith("{"):
            result = json.loads(value)
        else:
            field, values = value.split("=", 1)
            result = {
                "field": field.strip(),
                "operator": 7,
                "values": [item.strip() for item in values.split(",") if item.strip()],
            }
    except (ValueError, json.JSONDecodeError) as exc:
        raise argparse.ArgumentTypeError(
            '--filter must be JSON or "field=value1,value2"'
        ) from exc
    if not isinstance(result, dict):
        raise argparse.ArgumentTypeError("--filter must be a JSON object")
    if str(result.get("field") or "").strip() == "":
        raise argparse.ArgumentTypeError("--filter field must not be empty")
    if result.get("operator", 7) != 7:
        raise argparse.ArgumentTypeError("Qianchuan unified-report filters require operator 7")
    values = result.get("values")
    if not isinstance(values, list) or not values:
        raise argparse.ArgumentTypeError("--filter values must be a non-empty array")
    if any(isinstance(item, (dict, list, bool)) or item is None for item in values):
        raise argparse.ArgumentTypeError("--filter values must contain strings or numbers")
    return {
        "field": str(result["field"]).strip(),
        "operator": 7,
        "values": [str(item) for item in values],
    }


def report_value(container, *, prefer_string=False):
    if not isinstance(container, dict):
        return container
    keys = ("ValueStr", "value_str", "Value", "value") if prefer_string else (
        "Value",
        "value",
        "ValueStr",
        "value_str",
    )
    for key in keys:
        value = container.get(key)
        if value is not None and value != "":
            return value
    return None


def normalize_custom_row(row):
    if not isinstance(row, dict):
        raise ApiError("Qianchuan unified report returned an invalid row")
    dimensions = row.get("dimensions") or {}
    metrics = row.get("metrics") or {}
    if not isinstance(dimensions, dict) or not isinstance(metrics, dict):
        raise ApiError("Qianchuan unified report returned invalid row fields")
    return {
        "dimensions": dimensions,
        "metrics": metrics,
        "flat": {
            **{
                field: report_value(value, prefer_string=True)
                for field, value in dimensions.items()
            },
            **{field: report_value(value) for field, value in metrics.items()},
        },
    }


def request_error(response, endpoint):
    return ApiError(
        "Qianchuan report query failed",
        {
            "endpoint": endpoint,
            "code": response.get("code"),
            "http_status": response.get("http_status"),
            "message": response.get("message"),
            "request_id": response.get("request_id"),
        },
    )


def retryable_error(error):
    if not isinstance(error, ApiError):
        return False
    details = error.details or {}
    message = str(details.get("message") or error).lower()
    reason = str(details.get("reason") or "").lower()
    return (
        str(details.get("code") or "") in RETRYABLE_CODES
        or details.get("http_status") in RETRYABLE_HTTP_STATUSES
        or details.get("retryable") is True
        or "timeout" in message
        or "timed out" in message
        or "timeout" in reason
        or "timed out" in reason
        or "超时" in message
        or "超时" in reason
    )


def get_with_retry(client, endpoint, params, *, retry_delays=(1, 2), sleep_fn=None):
    sleep_fn = sleep_fn or time.sleep
    for attempt in range(len(retry_delays) + 1):
        try:
            response = client.get(endpoint, params=params)
        except ApiError as error:
            if not retryable_error(error) or attempt == len(retry_delays):
                raise
            sleep_fn(retry_delays[attempt])
            continue
        if response.get("code") == 0:
            return response
        retryable = (
            str(response.get("code") or "") in RETRYABLE_CODES
            or response.get("http_status") in RETRYABLE_HTTP_STATUSES
            or "timeout" in str(response.get("message") or "").lower()
            or "timed out" in str(response.get("message") or "").lower()
            or "超时" in str(response.get("message") or "")
        )
        if not retryable or attempt == len(retry_delays):
            raise request_error(response, endpoint)
        sleep_fn(retry_delays[attempt])
    raise AssertionError("unreachable")


def page_info(data, expected_page, endpoint):
    info = data.get("page_info") if isinstance(data, dict) else None
    if not isinstance(info, dict):
        raise ApiError("Qianchuan report omitted pagination metadata", {"endpoint": endpoint})
    try:
        page = int(info.get("page"))
        total_page = int(info.get("total_page"))
        total_number = int(info.get("total_number"))
    except (TypeError, ValueError) as exc:
        raise ApiError(
            "Qianchuan report returned invalid pagination metadata",
            {"endpoint": endpoint, "page": expected_page},
        ) from exc
    if page != expected_page or total_page < 0 or total_number < 0:
        raise ApiError(
            "Qianchuan report returned inconsistent pagination metadata",
            {"endpoint": endpoint, "page": expected_page, "page_info": info},
        )
    if total_page > MAX_PAGES:
        raise ApiError(
            "Qianchuan report exceeds the pagination safety cap",
            {"endpoint": endpoint, "total_page": total_page, "max_pages": MAX_PAGES},
        )
    return page, total_page, total_number


def aggregate_report(
    client, action, advertiser_id, start_date, end_date, fields,
    marketing_goal, order_platform, adlab_scene=None, data_period=None,
):
    endpoint = ENDPOINTS[action]
    date_key = "time" if action == "account" else "date"
    params = {
        "advertiser_id": int(advertiser_id),
        f"start_{date_key}": f"{start_date} 00:00:00" if action == "account" else start_date,
        f"end_{date_key}": f"{end_date} 23:59:59" if action == "account" else end_date,
        "fields": fields,
        "marketing_goal": marketing_goal,
        "order_platform": order_platform,
    }
    if action == "account":
        params["adlab_scene"] = adlab_scene or "OVERALL_PROJECT"
        if data_period:
            params["data_period"] = data_period
    response = get_with_retry(client, endpoint, params)
    data = response.get("data")
    if not isinstance(data, dict):
        raise ApiError("Qianchuan account report returned invalid data", {"endpoint": endpoint})
    return {
        "mode": action.replace("-", "_"),
        "endpoint": endpoint,
        "advertiser_id": str(advertiser_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "fields": fields,
        "adlab_scene": params.get("adlab_scene"),
        "data_period": params.get("data_period"),
        "marketing_goal": marketing_goal,
        "order_platform": order_platform,
        "data": data,
        "request_ids": [response["request_id"]] if response.get("request_id") else [],
    }


def schema_report(client, advertiser_id, topics, data_period=None):
    params = {"advertiser_id": int(advertiser_id), "data_topics": topics}
    if data_period:
        params["data_period"] = data_period
    response = get_with_retry(
        client,
        CONFIG_PATH,
        params,
    )
    data = response.get("data")
    if not isinstance(data, dict) or not isinstance(data.get("custom_config_datas"), list):
        raise ApiError("Qianchuan unified report schema returned invalid data")
    return {
        "mode": "qianchuan_unified_report_schema",
        "endpoint": CONFIG_PATH,
        "advertiser_id": str(advertiser_id),
        "data_topics": topics,
        "data_period": data_period,
        "schemas": data["custom_config_datas"],
        "request_ids": [response["request_id"]] if response.get("request_id") else [],
    }


def paged_report(client, endpoint, params, *, top, max_pages, row_key="list", custom_rows=False):
    rows = []
    request_ids = []
    expected_total_page = None
    expected_total_number = None
    page_count = 0
    for page in range(1, max_pages + 1):
        response = get_with_retry(client, endpoint, {**params, "page": page})
        data = response.get("data")
        if not isinstance(data, dict) or not isinstance(data.get(row_key), list):
            raise ApiError("Qianchuan report returned invalid page data", {"endpoint": endpoint, "page": page})
        _, total_page, total_number = page_info(data, page, endpoint)
        if expected_total_page is None:
            expected_total_page, expected_total_number = total_page, total_number
        elif (total_page, total_number) != (expected_total_page, expected_total_number):
            raise ApiError("Qianchuan report pagination changed during traversal", {"endpoint": endpoint, "page": page})
        page_rows = data[row_key]
        rows.extend(normalize_custom_row(row) for row in page_rows) if custom_rows else rows.extend(page_rows)
        page_count += 1
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        if page >= total_page:
            break
    else:
        raise ApiError("Qianchuan report reached the configured page cap", {"endpoint": endpoint, "max_pages": max_pages})
    if expected_total_number is not None and len(rows) != expected_total_number:
        raise ApiError(
            "Qianchuan report row count contradicts pagination metadata",
            {"endpoint": endpoint, "row_count": len(rows), "total_number": expected_total_number},
        )
    displayed = rows if top == 0 else rows[:top]
    return {
        "rows": displayed,
        "displayed_count": len(displayed),
        "total_row_count": len(rows),
        "page_count": page_count,
        "request_ids": request_ids,
        "truncated": len(displayed) != len(rows),
    }


def custom_report(client, advertiser_id, start_date, end_date, topic, dimensions, metrics, filters, data_period, order_field, order_type, page_size, top, max_pages):
    params = {
        "advertiser_id": int(advertiser_id),
        "data_topic": topic,
        "dimensions": dimensions,
        "metrics": metrics,
        "filters": filters,
        "start_time": f"{start_date} 00:00:00",
        "end_time": f"{end_date} 23:59:59",
        "order_by": [{"field": order_field, "type": 1 if order_type == "ASC" else 2}],
        "page_size": page_size,
        "data_period": data_period,
    }
    page = paged_report(client, DATA_PATH, params, top=top, max_pages=max_pages, row_key="rows", custom_rows=True)
    return {
        "mode": "qianchuan_unified_report",
        "endpoint": DATA_PATH,
        "advertiser_id": str(advertiser_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "data_topic": topic,
        "dimensions": dimensions,
        "metrics": metrics,
        "filters": filters,
        "data_period": data_period,
        **page,
    }


def dimension_report(
    client, action, advertiser_id, start_date, end_date, dimension_id, metrics,
    dimension, marketing_goal, order_platform, smart_bid_type, order_field,
    order_type, page_size, top, max_pages,
):
    endpoint = ENDPOINTS[action]
    id_field = "room_id" if action == "rooms" else "aweme_id"
    params = {
        "advertiser_id": int(advertiser_id),
        id_field: int(dimension_id),
        "start_time": f"{start_date} 00:00:00",
        "end_time": f"{end_date} 23:59:59",
        "dimension": dimension,
        "metrics": metrics,
        "order_field": order_field,
        "order_type": order_type,
        "page_size": page_size,
        "filtering": {
            key: value
            for key, value in {
                "order_platform": order_platform,
                "smart_bid_type": smart_bid_type,
            }.items()
            if value
        },
    }
    if action == "authors":
        params["marketing_goal"] = marketing_goal
    page = paged_report(client, endpoint, params, top=top, max_pages=max_pages)
    return {
        "mode": f"qianchuan_{action}_dimension_report",
        "endpoint": endpoint,
        "advertiser_id": str(advertiser_id),
        id_field: str(dimension_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "dimension": dimension,
        "metrics": metrics,
        "filtering": params["filtering"],
        **page,
    }


def build_parser(action):
    parser = argparse.ArgumentParser(description=f"Query official Qianchuan {action} report data.")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--start-date", default=today())
    parser.add_argument("--end-date", default=today())
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    parser.add_argument("--marketing-goal", choices=["ALL", "VIDEO_PROM_GOODS", "LIVE_PROM_GOODS"], default="ALL")
    if action in {"account", "uni-account"}:
        parser.add_argument("--field", action="append", help="Field or comma-separated fields.")
        parser.add_argument("--order-platform", default="QIANCHUAN")
        if action == "account":
            parser.add_argument("--adlab-scene", choices=["OVERALL_PROJECT", "UNI_PROJECT"], default="OVERALL_PROJECT")
            parser.add_argument("--data-period", choices=sorted(VALID_DATA_PERIODS))
    elif action == "schema":
        parser.add_argument("--data-topic", action="append", required=True)
        parser.add_argument("--data-period", choices=sorted(VALID_DATA_PERIODS))
    elif action in {"custom", "products"}:
        if action == "custom":
            parser.add_argument("--data-topic", required=True)
            parser.add_argument("--dimension", action="append", required=True)
            parser.add_argument("--metric", action="append", required=True)
        else:
            parser.add_argument("--report-mode", choices=sorted(PRODUCT_TOPICS), default="uni")
            parser.add_argument("--dimension", action="append")
            parser.add_argument("--metric", action="append")
        parser.add_argument("--filter", action="append", type=parse_filter)
        parser.add_argument("--data-period", choices=sorted(VALID_DATA_PERIODS))
        parser.add_argument("--order-field", default="stat_cost")
        parser.add_argument("--order-type", choices=["ASC", "DESC"], default="DESC")
        parser.add_argument("--page-size", type=int, choices=sorted(VALID_PAGE_SIZES), default=100)
        parser.add_argument("--max-pages", type=int, default=100)
        parser.add_argument("--top", type=int, default=10)
    else:
        parser.add_argument("--room-id" if action == "rooms" else "--aweme-id", required=True)
        parser.add_argument("--metric", action="append")
        parser.add_argument("--order-platform", choices=["ALL", "ECP_AWEME", "QIANCHUAN"], default="QIANCHUAN")
        parser.add_argument("--smart-bid-type", choices=["SMART_BID_CONSERVATIVE", "SMART_BID_CUSTOM"])
        parser.add_argument("--dimension", choices=["TIME_GRANULARITY_DAILY", "TIME_GRANULARITY_HOURLY"], default="TIME_GRANULARITY_DAILY")
        parser.add_argument("--order-field", default="stat_cost_for_roi2" if action == "rooms" else "stat_cost")
        parser.add_argument("--order-type", choices=["ASC", "DESC"], default="DESC")
        parser.add_argument("--page-size", type=int, choices=range(1, 101), default=100)
        parser.add_argument("--max-pages", type=int, default=100)
        parser.add_argument("--top", type=int, default=10)
    return parser


def main(argv=None):
    argv = list(argv or [])
    if not argv or argv[0] not in ENDPOINTS:
        raise SystemExit("expected Qianchuan report action")
    action = argv.pop(0)
    parser = build_parser(action)
    args = parser.parse_args(argv)
    start_date = date_value(args.start_date, "start_date")
    end_date = date_value(args.end_date, "end_date")
    if start_date > end_date:
        parser.error("start_date cannot be after end_date")
    if not str(args.advertiser_id).isdigit() or int(args.advertiser_id) <= 0:
        parser.error("advertiser_id must be a positive integer")
    if hasattr(args, "top") and args.top < 0:
        parser.error("top must be zero or a positive integer")
    if hasattr(args, "max_pages") and not 1 <= args.max_pages <= MAX_PAGES:
        parser.error("max_pages must be between 1 and 500")
    if action == "account" and args.adlab_scene != "OVERALL_PROJECT" and args.data_period:
        parser.error("data_period is supported only for OVERALL_PROJECT")

    config_path = config_paths.resolve_config_path(args.config)
    runtime = token_manager.ensure_access_token(
        config_path,
        channel="qianchuan",
        advertiser_id=args.advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    client = QianchuanClientFactory(
        authorization_store.state_root(),
        args.advertiser_id,
    ).client(get_path(runtime, "api.base_url"), get_path(runtime, "api.access_token"))
    start_text, end_text = start_date.isoformat(), end_date.isoformat()
    if action in {"account", "uni-account"}:
        defaults = DEFAULT_ACCOUNT_FIELDS if action == "account" else DEFAULT_UNI_ACCOUNT_FIELDS
        result = aggregate_report(
            client,
            action,
            args.advertiser_id,
            start_text,
            end_text,
            split_csv(args.field) or defaults,
            args.marketing_goal,
            args.order_platform,
            getattr(args, "adlab_scene", None),
            getattr(args, "data_period", None),
        )
    elif action == "schema":
        result = schema_report(
            client, args.advertiser_id, unique(args.data_topic), args.data_period,
        )
    elif action in {"custom", "products"}:
        topic = args.data_topic if action == "custom" else PRODUCT_TOPICS[args.report_mode]
        dimensions = split_csv(args.dimension) or (["product_id"] if action == "products" else [])
        metrics = split_csv(args.metric) or (DEFAULT_PRODUCT_METRICS if action == "products" else [])
        if not dimensions or not metrics:
            parser.error("dimension and metric must not be empty")
        result = custom_report(
            client,
            args.advertiser_id,
            start_text,
            end_text,
            topic,
            dimensions,
            metrics,
            args.filter or [],
            args.data_period,
            args.order_field,
            args.order_type,
            args.page_size,
            args.top,
            args.max_pages,
        )
        if action == "products":
            result["mode"] = "qianchuan_product_dimension_report"
            result["report_mode"] = args.report_mode
    else:
        dimension_id = args.room_id if action == "rooms" else args.aweme_id
        if not str(dimension_id).isdigit() or int(dimension_id) <= 0:
            parser.error(f"{'room_id' if action == 'rooms' else 'aweme_id'} must be a positive integer")
        result = dimension_report(
            client,
            action,
            args.advertiser_id,
            start_text,
            end_text,
            dimension_id,
            split_csv(args.metric) or DEFAULT_DIMENSION_FIELDS,
            args.dimension,
            args.marketing_goal,
            args.order_platform,
            args.smart_bid_type,
            args.order_field,
            args.order_type,
            args.page_size,
            args.top,
            args.max_pages,
        )
    write_json(result, args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
