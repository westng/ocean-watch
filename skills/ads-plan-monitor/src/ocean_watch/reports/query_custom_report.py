#!/usr/bin/env python3
import argparse
import csv
import datetime as dt
import json
from pathlib import Path

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import get_json
from ocean_watch.core.data import get_path, split_csv

DEFAULT_DATA_TOPIC = "MATERIAL_DATA"
DEFAULT_DIMENSIONS = ["material_id", "cdp_promotion_id", "cdp_promotion_name"]
DEFAULT_METRICS = [
    "stat_cost",
    "show_cnt",
    "click_cnt",
    "ctr",
    "cpc_platform",
    "cpm_platform",
    "convert_cnt",
    "conversion_cost",
    "conversion_rate",
    "total_play",
    "play_duration_3s",
    "play_over_rate",
    "in_app_order",
    "in_app_order_gmv",
    "in_app_order_roi",
]


def today_time(which):
    suffix = "00:00:00" if which == "start" else "23:59:59"
    return f"{dt.date.today().isoformat()} {suffix}"


def normalize_time(value, which):
    if not value:
        return today_time(which)
    if len(value) == 10:
        suffix = "00:00:00" if which == "start" else "23:59:59"
        return f"{value} {suffix}"
    return value


def parse_filter(value):
    if value.strip().startswith("{"):
        return json.loads(value)
    parts = value.split(":", 3)
    if len(parts) != 4:
        raise ValueError("--filter must be JSON or field:type:operator:value1,value2")
    field, type_value, operator, values = parts
    return {
        "field": field,
        "type": int(type_value),
        "operator": int(operator),
        "values": [item.strip() for item in values.split(",") if item.strip()],
    }


def flatten_rows(rows):
    flat = []
    for row in rows or []:
        merged = {}
        merged.update(row.get("dimensions") or {})
        merged.update(row.get("metrics") or {})
        flat.append(merged)
    return flat


def write_csv(path, rows):
    fieldnames = list(dict.fromkeys(key for row in rows for key in row.keys()))
    out_path = Path(path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    parser.add_argument("--csv-out", help="Optional CSV output path. No file is written by default.")
    parser.add_argument("--data-topic", default=DEFAULT_DATA_TOPIC)
    parser.add_argument("--dimension", action="append", help="Dimension field or comma-separated fields.")
    parser.add_argument("--metric", action="append", help="Metric field or comma-separated fields.")
    parser.add_argument(
        "--filter",
        action="append",
        help='Filter JSON or shorthand "field:type:operator:value1,value2".',
    )
    parser.add_argument("--start-time")
    parser.add_argument("--end-time")
    parser.add_argument("--start-date")
    parser.add_argument("--end-date")
    parser.add_argument("--order-field", default="stat_cost")
    parser.add_argument("--order-type", choices=["ASC", "DESC"], default="DESC")
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=100)
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
    dimensions = split_csv(args.dimension) or DEFAULT_DIMENSIONS
    metrics = split_csv(args.metric) or DEFAULT_METRICS
    filters = [parse_filter(value) for value in args.filter or []]
    start_time = normalize_time(args.start_time or args.start_date, "start")
    end_time = normalize_time(args.end_time or args.end_date, "end")
    params = {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "data_topic": args.data_topic,
        "dimensions": dimensions,
        "metrics": metrics,
        "filters": filters,
        "start_time": start_time,
        "end_time": end_time,
        "order_by": [{"field": args.order_field, "type": args.order_type}],
        "page": args.page,
        "page_size": args.page_size,
    }
    response = get_json(
        get_path(config, "api.base_url"),
        get_path(config, "api.access_token"),
        "/v3.0/report/custom/get/",
        params,
    )
    rows = get_path(response, "data.rows", []) or []
    result = {
        "endpoint": "/v3.0/report/custom/get/",
        "params": params,
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "page_info": get_path(response, "data.page_info"),
        "total_metrics": get_path(response, "data.total_metrics"),
        "row_count": len(rows),
        "rows": rows,
        "flat_rows": flatten_rows(rows),
    }
    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    if args.csv_out:
        write_csv(args.csv_out, result["flat_rows"])
    print(output)
    return 0 if response.get("code") == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
