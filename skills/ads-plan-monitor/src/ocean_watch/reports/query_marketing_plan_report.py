#!/usr/bin/env python3
import argparse
import datetime as dt
import json
from decimal import Decimal, InvalidOperation

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path, split_csv
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.identifiers import require_advertiser_id
from ocean_watch.core.output import write_json
from ocean_watch.core.pagination import declared_page_count
from ocean_watch.core.validation import positive_integer

REPORT_CONFIG_PATH = "/v3.0/report/custom/config/get/"
REPORT_DATA_PATH = "/v3.0/report/custom/get/"
DATA_TOPIC = "UNI_PROJECT_DATA"
ID_DIMENSION_CANDIDATES = ("project_id", "cdp_project_id")
NAME_DIMENSION_CANDIDATES = ("project_name", "cdp_project_name")
DEFAULT_METRIC_CANDIDATES = (
    "stat_cost",
    "show_cnt",
    "click_cnt",
    "ctr",
    "convert_cnt",
    "conversion_cost",
    "conversion_rate",
    "in_app_order_count",
    "in_app_order_gmv",
    "in_app_order_roi",
)
MAX_PAGE_SIZE = 100


def field_names(rows):
    return {
        str(row.get("field"))
        for row in rows or []
        if isinstance(row, dict) and row.get("field")
    }


def select_contract(config_response, requested_metrics=None):
    if config_response.get("code") != 0:
        raise ApiError("Marketing report contract query failed", {
            "code": config_response.get("code"),
            "message": config_response.get("message"),
            "request_id": config_response.get("request_id"),
        })
    topics = get_path(config_response, "data.list", []) or []
    topic = next((row for row in topics if row.get("data_topic") == DATA_TOPIC), None)
    if topic is None:
        raise ConfigurationError(
            "UNI_PROJECT_DATA is unavailable for this advertiser permission set"
        )
    dimensions = field_names(topic.get("dimensions"))
    metrics = field_names(topic.get("metrics"))
    id_dimension = next(
        (field for field in ID_DIMENSION_CANDIDATES if field in dimensions),
        None,
    )
    if id_dimension is None:
        raise ConfigurationError("UNI_PROJECT_DATA has no supported project ID dimension", {
            "available_dimensions": sorted(dimensions),
        })
    selected_dimensions = [id_dimension]
    name_dimension = next(
        (field for field in NAME_DIMENSION_CANDIDATES if field in dimensions),
        None,
    )
    if name_dimension:
        selected_dimensions.append(name_dimension)
    requested = list(dict.fromkeys(
        requested_metrics or list(DEFAULT_METRIC_CANDIDATES)
    ))
    missing = [field for field in requested if field not in metrics]
    if requested_metrics and missing:
        raise ConfigurationError("requested Marketing report metrics are unavailable", {
            "missing_metrics": missing,
            "available_metrics": sorted(metrics),
        })
    selected_metrics = [field for field in requested if field in metrics]
    if "stat_cost" not in selected_metrics:
        raise ConfigurationError("UNI_PROJECT_DATA does not expose stat_cost")
    return {
        "dimensions": selected_dimensions,
        "metrics": selected_metrics,
        "available_dimension_count": len(dimensions),
        "available_metric_count": len(metrics),
        "omitted_default_metrics": missing,
    }


def flatten_row(row):
    flattened = {}
    flattened.update(row.get("dimensions") or {})
    flattened.update(row.get("metrics") or {})
    for field in ID_DIMENSION_CANDIDATES:
        if flattened.get(field) is not None:
            flattened[field] = str(flattened[field])
    return flattened


def metric_decimal(value, field):
    if value in (None, ""):
        return Decimal("0")
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as error:
        raise ApiError("Marketing plan report returned a non-numeric metric", {
            "field": field,
            "value": value,
        }) from error
    if not parsed.is_finite():
        raise ApiError("Marketing plan report returned a non-finite metric", {"field": field})
    return parsed


def integer_total(value, field):
    if value != value.to_integral_value():
        raise ApiError("Marketing plan report returned a fractional count metric", {
            "field": field,
            "value": str(value),
        })
    return int(value)


def summarize(rows, selected_metrics=None):
    available = set(selected_metrics) if selected_metrics is not None else {
        field
        for row in rows
        for field in row
    }
    spend = sum((metric_decimal(row.get("stat_cost"), "stat_cost") for row in rows), Decimal())
    gmv = (
        sum(
            (metric_decimal(row.get("in_app_order_gmv"), "in_app_order_gmv") for row in rows),
            Decimal(),
        )
        if "in_app_order_gmv" in available
        else None
    )
    orders = (
        sum(
            (
                metric_decimal(row.get("in_app_order_count"), "in_app_order_count")
                for row in rows
            ),
            Decimal(),
        )
        if "in_app_order_count" in available
        else None
    )
    return {
        "total_spend": float(spend),
        "total_gmv": float(gmv) if gmv is not None else None,
        "total_orders": (
            integer_total(orders, "in_app_order_count")
            if orders is not None
            else None
        ),
        "weighted_roi": float(gmv / spend) if gmv is not None and spend else None,
        "plans_with_spend": sum(
            metric_decimal(row.get("stat_cost"), "stat_cost") > 0 for row in rows
        ),
    }


def query_plan_rows(
    client,
    advertiser_id,
    start_date,
    end_date,
    contract,
    *,
    page_size=MAX_PAGE_SIZE,
    max_pages=100,
):
    rows = []
    request_ids = []
    page = 1
    expected_pages = None
    truncated = False
    while page <= max_pages:
        response = client.get(REPORT_DATA_PATH, params={
            "advertiser_id": advertiser_id,
            "data_topic": DATA_TOPIC,
            "dimensions": contract["dimensions"],
            "metrics": contract["metrics"],
            "filters": [],
            "start_time": f"{start_date} 00:00:00",
            "end_time": f"{end_date} 23:59:59",
            "order_by": [{"field": "stat_cost", "type": "DESC"}],
            "page": page,
            "page_size": page_size,
        })
        if response.get("code") != 0:
            raise ApiError("Marketing plan report query failed", {
                "code": response.get("code"),
                "message": response.get("message"),
                "request_id": response.get("request_id"),
                "page": page,
            })
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        page_rows = get_path(response, "data.rows", []) or []
        if not isinstance(page_rows, list):
            raise ApiError("Marketing plan report rows must be a list", {"page": page})
        rows.extend(flatten_row(row) for row in page_rows)
        total_pages = declared_page_count(
            get_path(response, "data.page_info"),
            source="marketing_plan_report",
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
    parser = argparse.ArgumentParser(description="Query Marketing project performance.")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id")
    parser.add_argument("--auth-account-id")
    parser.add_argument("--start-date", default=today)
    parser.add_argument("--end-date", default=today)
    parser.add_argument("--metric", action="append")
    parser.add_argument("--page-size", type=int, default=MAX_PAGE_SIZE)
    parser.add_argument("--max-pages", type=int, default=100)
    parser.add_argument("--top", type=int, default=10, help="0 displays every row.")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    try:
        start_date = dt.date.fromisoformat(args.start_date).isoformat()
        end_date = dt.date.fromisoformat(args.end_date).isoformat()
    except ValueError as error:
        raise ConfigurationError("dates must use YYYY-MM-DD") from error
    if start_date > end_date:
        raise ConfigurationError("start_date must not be later than end_date")
    if args.top < 0:
        raise ConfigurationError("top must be zero or a positive integer")
    page_size = positive_integer(args.page_size, "page_size", maximum=MAX_PAGE_SIZE)
    max_pages = positive_integer(args.max_pages, "max_pages")

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    advertiser_id = require_advertiser_id(raw_config, args.advertiser_id)
    runtime = token_manager.ensure_access_token(
        config_path,
        raw_config,
        channel="marketing",
        advertiser_id=advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    client = OceanEngineClient(
        get_path(runtime, "api.base_url"),
        get_path(runtime, "api.access_token"),
    )
    config_response = client.get(REPORT_CONFIG_PATH, params={
        "advertiser_id": advertiser_id,
        "data_topics": [DATA_TOPIC],
    })
    contract = select_contract(config_response, split_csv(args.metric) or None)
    queried = query_plan_rows(
        client,
        advertiser_id,
        start_date,
        end_date,
        contract,
        page_size=page_size,
        max_pages=max_pages,
    )
    rows = queried.pop("rows")
    displayed = rows[: args.top] if args.top else rows
    result = {
        "mode": "marketing_plan_report",
        "config_endpoint": REPORT_CONFIG_PATH,
        "report_endpoint": REPORT_DATA_PATH,
        "advertiser_id": str(advertiser_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "contract": contract,
        "config_request_id": config_response.get("request_id"),
        "summary": summarize(rows, contract["metrics"]),
        "row_count": len(rows),
        "displayed_count": len(displayed),
        "rows": displayed,
        **queried,
    }
    write_json(result, destination=args.out)
    return 1 if result["truncated"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
