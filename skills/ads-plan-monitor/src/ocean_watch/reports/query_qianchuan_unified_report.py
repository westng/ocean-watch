#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import time
from decimal import Decimal, InvalidOperation
from pathlib import Path

from ocean_watch.accounts import managed_accounts
from ocean_watch.api import QianchuanClientFactory
from ocean_watch.auth import authorization_store, token_manager
from ocean_watch.core import config_paths
from ocean_watch.core.data import get_path, split_csv
from ocean_watch.core.errors import ApiError, ConfigurationError, OceanWatchError
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
DEFAULT_SCHEMA_TOPICS = [
    "SITE_PROMOTION_PRODUCT_AD",
    "SITE_PROMOTION_PRODUCT_PRODUCT",
    "OVERALL_ROI_PRODUCT_PRODUCT",
    "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO",
    "SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE",
    "SITE_PROMOTION_PRODUCT_POST_DATA_TITLE",
    "SITE_PROMOTION_PRODUCT_POST_DATA_OTHER",
    "OVERALL_ROI_PRODUCT_MATERIAL",
]

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
NON_ADDITIVE_METRIC_PARTS = (
    "rate",
    "ctr",
    "cvr",
    "cpc",
    "cpm",
    "ecpm",
    "cost_per",
    "conversion_cost",
    "avg",
    "average",
    "ratio",
)
ADDITIVE_METRIC_PARTS = (
    "stat_cost",
    "_count",
    "_cnt",
    "show_count",
    "click_count",
    "order_count",
    "gmv",
    "amount",
)


def today():
    return dt.date.today().isoformat()


def date_value(value, field):
    try:
        return dt.date.fromisoformat(str(value))
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"{field} must use YYYY-MM-DD") from exc


def unique(values):
    return list(dict.fromkeys(item.strip() for item in values if item and item.strip()))


def positive_decimal_id(value, field):
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise argparse.ArgumentTypeError(f"{field} must be a positive integer")
    return text


def advertiser_ids(values):
    return unique(positive_decimal_id(value, "advertiser_id") for value in split_csv(values))


def metric_is_additive(field):
    lower = str(field or "").lower()
    if any(part in lower for part in NON_ADDITIVE_METRIC_PARTS):
        return False
    if any(part in lower for part in ADDITIVE_METRIC_PARTS):
        return True
    return False


def decimal_value(value, field):
    if value is None or value == "":
        return Decimal(0)
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ApiError(
            "Qianchuan report returned an invalid numeric metric",
            {"field": field, "value": str(value)},
        ) from exc
    if not parsed.is_finite():
        raise ApiError(
            "Qianchuan report returned a non-finite numeric metric",
            {"field": field},
        )
    return parsed


def json_number(value):
    if value is None:
        return None
    if value == value.to_integral_value():
        return int(value)
    return float(value)


def serialized_error(error):
    if isinstance(error, OceanWatchError):
        return error.as_dict()["error"]
    return {
        "code": "unexpected_error",
        "message": str(error),
        "details": {},
    }


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


def schema_topics(result):
    topics = []
    for schema in result.get("schemas") or []:
        topic = schema.get("data_topic") if isinstance(schema, dict) else None
        if topic:
            topics.append(str(topic))
    return unique(topics)


def merge_schema_reports(reports, *, requested_topics=None, data_period=None):
    schemas_by_topic = {}
    accounts = []
    request_ids = []
    for report in reports:
        for schema in report.get("schemas") or []:
            if not isinstance(schema, dict):
                continue
            topic = schema.get("data_topic")
            if topic and topic not in schemas_by_topic:
                schemas_by_topic[topic] = schema
        request_ids.extend(report.get("request_ids") or [])
        accounts.append({
            "advertiser_id": report["advertiser_id"],
            "query_status": "ok",
            "data_topics": schema_topics(report),
            "schema_count": len(report.get("schemas") or []),
            "request_ids": report.get("request_ids") or [],
        })
    return {
        "mode": "qianchuan_unified_report_schema",
        "endpoint": CONFIG_PATH,
        "advertiser_ids": [report["advertiser_id"] for report in reports],
        "data_topics": unique(topic for report in reports for topic in report.get("data_topics") or [])
        or list(requested_topics or []),
        "data_period": next(
            (report.get("data_period") for report in reports if report.get("data_period")),
            data_period,
        ),
        "schemas": list(schemas_by_topic.values()),
        "accounts": accounts,
        "request_ids": unique(request_ids),
    }


def build_multi_schema_result(reports, failures, topics, data_period, account_order=None):
    merged = merge_schema_reports(reports, requested_topics=topics, data_period=data_period)
    account_summaries = [
        (account["advertiser_id"], account)
        for account in merged["accounts"]
    ] + [(failure["advertiser_id"], failure) for failure in failures]
    if account_order:
        order = {advertiser_id: index for index, advertiser_id in enumerate(account_order)}
        account_summaries.sort(key=lambda item: order.get(item[0], len(order)))
    return {
        "ok": not failures,
        **merged,
        "advertiser_ids": list(account_order or merged["advertiser_ids"]),
        "accounts": [summary for _, summary in account_summaries],
    }


def paged_report(
    client,
    endpoint,
    params,
    *,
    top,
    max_pages,
    row_key="list",
    custom_rows=False,
    include_all_rows=False,
):
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
    result = {
        "rows": displayed,
        "displayed_count": len(displayed),
        "total_row_count": len(rows),
        "page_count": page_count,
        "request_ids": request_ids,
        "truncated": len(displayed) != len(rows),
    }
    if include_all_rows:
        result["all_rows"] = rows
    return result


def custom_report(
    client,
    advertiser_id,
    start_date,
    end_date,
    topic,
    dimensions,
    metrics,
    filters,
    data_period,
    order_field,
    order_type,
    page_size,
    top,
    max_pages,
    *,
    include_all_rows=False,
):
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
    page = paged_report(
        client,
        DATA_PATH,
        params,
        top=top,
        max_pages=max_pages,
        row_key="rows",
        custom_rows=True,
        include_all_rows=include_all_rows,
    )
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


def aggregate_custom_reports(
    reports,
    dimensions,
    metrics,
    *,
    top,
    sort_field=None,
    sort_type="DESC",
):
    additive_metrics = [field for field in metrics if metric_is_additive(field)]
    non_additive_metrics = [field for field in metrics if field not in additive_metrics]
    groups = {}
    for report in reports:
        for row in report.get("all_rows") or report.get("rows") or []:
            flat = row.get("flat") or {}
            key = tuple(str(flat.get(field) or "") for field in dimensions)
            if key not in groups:
                groups[key] = {
                    "dimensions": {field: flat.get(field) for field in dimensions},
                    "metrics": {field: Decimal(0) for field in additive_metrics},
                    "accounts": set(),
                    "row_count": 0,
                }
            group = groups[key]
            group["accounts"].add(report["advertiser_id"])
            group["row_count"] += 1
            for field in additive_metrics:
                group["metrics"][field] += decimal_value(flat.get(field), field)
    rows = []
    for group in groups.values():
        metric_values = {field: json_number(value) for field, value in group["metrics"].items()}
        for field in non_additive_metrics:
            metric_values[field] = None
        rows.append({
            "dimensions": group["dimensions"],
            "metrics": metric_values,
            "flat": {**group["dimensions"], **metric_values},
            "account_count": len(group["accounts"]),
            "row_count": group["row_count"],
        })
    sort_metric = sort_field if sort_field in additive_metrics else (
        additive_metrics[0] if additive_metrics else None
    )
    rows.sort(
        key=lambda row: (
            decimal_value(row["metrics"].get(sort_metric), sort_metric)
            if sort_metric
            else Decimal(0)
        ),
        reverse=(sort_type != "ASC"),
    )
    displayed = rows if top == 0 else rows[:top]
    return {
        "rows": displayed,
        "displayed_count": len(displayed),
        "total_row_count": len(rows),
        "truncated": len(displayed) != len(rows),
        "aggregation": {
            "group_by": dimensions,
            "additive_metrics": additive_metrics,
            "non_additive_metrics": non_additive_metrics,
            "non_additive_note": (
                "Non-additive ratio or unit-price metrics are not summed across accounts."
                if non_additive_metrics
                else None
            ),
            "sort_field": sort_metric,
            "sort_type": sort_type if sort_metric else None,
        },
    }


def build_multi_custom_result(
    reports,
    failures,
    start_date,
    end_date,
    topic,
    dimensions,
    metrics,
    filters,
    data_period,
    top,
    order_field,
    order_type,
    account_order=None,
):
    successful = [report for report in reports if report]
    account_results = []
    for report in successful:
        public_report = dict(report)
        public_report.pop("all_rows", None)
        account_results.append(public_report)
    account_summaries = [
        (
            report["advertiser_id"],
            {
                "advertiser_id": report["advertiser_id"],
                "query_status": "ok",
                "displayed_count": report["displayed_count"],
                "total_row_count": report["total_row_count"],
                "page_count": report["page_count"],
                "request_ids": report.get("request_ids") or [],
            },
        )
        for report in successful
    ] + [(failure["advertiser_id"], failure) for failure in failures]
    if account_order:
        order = {advertiser_id: index for index, advertiser_id in enumerate(account_order)}
        account_summaries.sort(key=lambda item: order.get(item[0], len(order)))
    accounts = [summary for _, summary in account_summaries]
    aggregate = aggregate_custom_reports(
        successful,
        dimensions,
        metrics,
        top=top,
        sort_field=order_field,
        sort_type=order_type,
    )
    return {
        "ok": not failures,
        "mode": "qianchuan_unified_report_multi_account",
        "endpoint": DATA_PATH,
        "advertiser_ids": list(account_order or (
            [report["advertiser_id"] for report in successful]
            + [failure["advertiser_id"] for failure in failures]
        )),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "data_topic": topic,
        "dimensions": dimensions,
        "metrics": metrics,
        "filters": filters,
        "data_period": data_period,
        "summary": {
            "account_count": len(accounts),
            "successful_account_count": len(successful),
            "failed_account_count": len(failures),
            "total_source_row_count": sum(report["total_row_count"] for report in successful),
            "aggregated_row_count": aggregate["total_row_count"],
        },
        "accounts": accounts,
        "account_results": account_results,
        **aggregate,
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


def load_managed_qianchuan_accounts(config_path, *, include_disabled=False):
    try:
        config = json.loads(Path(config_path).read_text(encoding="utf-8"))
    except OSError as exc:
        raise ConfigurationError("unable to read config", {"path": str(config_path)}) from exc
    except json.JSONDecodeError as exc:
        raise ConfigurationError("config file contains invalid JSON", {"path": str(config_path)}) from exc
    return managed_accounts.list_accounts(
        config,
        channel="qianchuan",
        enabled_only=not include_disabled,
    )


def account_specs(advertiser_id_values, *, config_path=None, managed=False, include_disabled=False):
    specs = []
    for advertiser_id in advertiser_ids(advertiser_id_values):
        specs.append({"advertiser_id": advertiser_id})
    if managed:
        specs.extend(load_managed_qianchuan_accounts(
            config_path,
            include_disabled=include_disabled,
        ))
    deduped = []
    deduped_by_id = {}
    seen = set()
    for spec in specs:
        advertiser_id = positive_decimal_id(spec["advertiser_id"], "advertiser_id")
        if advertiser_id in seen:
            existing = deduped_by_id[advertiser_id]
            for key, value in spec.items():
                if value is not None and existing.get(key) in (None, ""):
                    existing[key] = value
            continue
        seen.add(advertiser_id)
        deduped_spec = {**spec, "advertiser_id": advertiser_id}
        deduped.append(deduped_spec)
        deduped_by_id[advertiser_id] = deduped_spec
    return deduped


def client_for_account(config_path, account):
    runtime = token_manager.ensure_access_token(
        config_path,
        channel="qianchuan",
        advertiser_id=account["advertiser_id"],
        auth_account_id=account.get("auth_account_id"),
    )
    return QianchuanClientFactory(
        authorization_store.state_root(),
        account["advertiser_id"],
    ).client(get_path(runtime, "api.base_url"), get_path(runtime, "api.access_token"))


def query_custom_account(config_path, account, start_date, end_date, topic, dimensions, metrics, filters, data_period, order_field, order_type, page_size, top, max_pages):
    client = client_for_account(config_path, account)
    return custom_report(
        client,
        account["advertiser_id"],
        start_date,
        end_date,
        topic,
        dimensions,
        metrics,
        filters,
        data_period,
        order_field,
        order_type,
        page_size,
        top,
        max_pages,
        include_all_rows=True,
    )


def query_schema_account(config_path, account, topics, data_period):
    client = client_for_account(config_path, account)
    return schema_report(client, account["advertiser_id"], topics, data_period)


def failed_report_account(account, error):
    return {
        "advertiser_id": str(account["advertiser_id"]),
        "name": account.get("name"),
        "query_status": "failed",
        "error": serialized_error(error),
    }


def build_parser(action):
    parser = argparse.ArgumentParser(description=f"Query official Qianchuan {action} report data.")
    parser.add_argument("--config")
    advertiser_help = (
        "Advertiser ID or comma-separated IDs. Repeat for multiple accounts."
        if action in {"schema", "custom"}
        else "Advertiser ID."
    )
    parser.add_argument(
        "--advertiser-id",
        action="append",
        required=action not in {"schema", "custom"},
        help=advertiser_help,
    )
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
        parser.add_argument(
            "--data-topic",
            action="append",
            help="Data topic or comma-separated topics. Defaults to common Qianchuan report topics.",
        )
        parser.add_argument("--data-period", choices=sorted(VALID_DATA_PERIODS))
        parser.add_argument(
            "--managed-accounts",
            action="store_true",
            help="Query enabled local responsible Qianchuan accounts.",
        )
        parser.add_argument("--include-disabled", action="store_true")
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
        if action == "custom":
            parser.add_argument(
                "--managed-accounts",
                action="store_true",
                help="Query enabled local responsible Qianchuan accounts and aggregate by dimensions.",
            )
            parser.add_argument("--include-disabled", action="store_true")
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
    config_path = config_paths.resolve_config_path(args.config)
    try:
        accounts = account_specs(
            getattr(args, "advertiser_id", None),
            config_path=config_path,
            managed=getattr(args, "managed_accounts", False),
            include_disabled=getattr(args, "include_disabled", False),
        )
    except (argparse.ArgumentTypeError, ConfigurationError) as error:
        parser.error(str(error))
    if not accounts:
        parser.error("at least one advertiser_id or --managed-accounts is required")
    if len(accounts) > 1 and args.auth_account_id:
        parser.error("--auth-account-id can only be used with one advertiser_id")
    if args.auth_account_id:
        accounts[0]["auth_account_id"] = args.auth_account_id
    if hasattr(args, "top") and args.top < 0:
        parser.error("top must be zero or a positive integer")
    if hasattr(args, "max_pages") and not 1 <= args.max_pages <= MAX_PAGES:
        parser.error("max_pages must be between 1 and 500")
    if action == "account" and args.adlab_scene != "OVERALL_PROJECT" and args.data_period:
        parser.error("data_period is supported only for OVERALL_PROJECT")

    start_text, end_text = start_date.isoformat(), end_date.isoformat()
    if action in {"account", "uni-account"}:
        if len(accounts) != 1:
            parser.error(f"{action} supports exactly one advertiser_id")
        account = accounts[0]
        client = client_for_account(config_path, account)
        defaults = DEFAULT_ACCOUNT_FIELDS if action == "account" else DEFAULT_UNI_ACCOUNT_FIELDS
        result = aggregate_report(
            client,
            action,
            account["advertiser_id"],
            start_text,
            end_text,
            split_csv(args.field) or defaults,
            args.marketing_goal,
            args.order_platform,
            getattr(args, "adlab_scene", None),
            getattr(args, "data_period", None),
        )
    elif action == "schema":
        topics = unique(split_csv(args.data_topic)) or DEFAULT_SCHEMA_TOPICS
        reports = []
        failures = []
        for account in accounts:
            try:
                reports.append(query_schema_account(config_path, account, topics, args.data_period))
            except Exception as error:
                failures.append(failed_report_account(account, error))
        if len(accounts) == 1 and not failures:
            result = reports[0]
        else:
            result = build_multi_schema_result(
                reports,
                failures,
                topics,
                args.data_period,
                account_order=[account["advertiser_id"] for account in accounts],
            )
    elif action in {"custom", "products"}:
        if action == "products" and len(accounts) != 1:
            parser.error("products supports exactly one advertiser_id; use custom for multi-account topic aggregation")
        if action == "custom":
            topics = unique(split_csv([args.data_topic]))
            if len(topics) != 1:
                parser.error("custom supports exactly one data_topic")
            topic = topics[0]
        else:
            topic = PRODUCT_TOPICS[args.report_mode]
        dimensions = split_csv(args.dimension) or (["product_id"] if action == "products" else [])
        metrics = split_csv(args.metric) or (DEFAULT_PRODUCT_METRICS if action == "products" else [])
        if not dimensions or not metrics:
            parser.error("dimension and metric must not be empty")
        reports = []
        failures = []
        for account in accounts:
            try:
                reports.append(query_custom_account(
                    config_path,
                    account,
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
                ))
            except Exception as error:
                failures.append(failed_report_account(account, error))
        if len(accounts) == 1 and not failures:
            result = reports[0]
            result.pop("all_rows", None)
        else:
            result = build_multi_custom_result(
                reports,
                failures,
                start_text,
                end_text,
                topic,
                dimensions,
                metrics,
                args.filter or [],
                args.data_period,
                args.top,
                args.order_field,
                args.order_type,
                account_order=[account["advertiser_id"] for account in accounts],
            )
        if action == "products":
            result["mode"] = "qianchuan_product_dimension_report"
            result["report_mode"] = args.report_mode
    else:
        if len(accounts) != 1:
            parser.error(f"{action} supports exactly one advertiser_id")
        account = accounts[0]
        client = client_for_account(config_path, account)
        dimension_id = args.room_id if action == "rooms" else args.aweme_id
        if not str(dimension_id).isdigit() or int(dimension_id) <= 0:
            parser.error(f"{'room_id' if action == 'rooms' else 'aweme_id'} must be a positive integer")
        result = dimension_report(
            client,
            action,
            account["advertiser_id"],
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
    return 0 if result.get("ok", True) else 1


if __name__ == "__main__":
    raise SystemExit(main())
