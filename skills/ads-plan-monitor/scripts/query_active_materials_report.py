#!/usr/bin/env python3
import argparse
import csv
import datetime as dt
import json
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import token_manager
import config_paths


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

DEFAULT_DIMENSIONS = [
    "material_id",
    "cdp_promotion_id",
    "cdp_promotion_name",
]

PROMOTION_FIELDS = [
    "promotion_id",
    "project_id",
    "advertiser_id",
    "promotion_name",
    "status",
    "status_first",
    "status_second",
    "opt_status",
    "source",
    "promotion_materials",
    "promotion_create_time",
    "promotion_modify_time",
]

INACTIVE_SECOND_REASONS = {
    "AUDIT",
    "AUDIT_DENY",
    "OFFLINE_BALANCE",
    "PROJECT_OFFLINE_BUDGET",
    "PROJECT_OFFLINE",
    "PROMOTION_DISABLE",
    "PROMOTION_DELETE",
    "TIME_DONE",
}

INACTIVE_STATUSES = {
    "AUDIT",
    "AUDIT_DENY",
    "DELETE",
    "DISABLE",
    "DONE",
    "NOT_START",
}


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


def split_csv(values):
    result = []
    for value in values or []:
        result.extend(part.strip() for part in str(value).split(",") if part.strip())
    return result


def int_values(values):
    return [int(value) for value in values]


def today():
    return dt.date.today().isoformat()


def normalize_time(value, which):
    if len(value) == 10:
        suffix = "00:00:00" if which == "start" else "23:59:59"
        return f"{value} {suffix}"
    return value


def get_json(base_url, token, path, params):
    query = urllib.parse.urlencode(
        {
            key: value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
            for key, value in params.items()
            if value is not None
        }
    )
    request = urllib.request.Request(
        base_url.rstrip("/") + path + "?" + query,
        headers={"Access-Token": token},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": body}


def get_page_info(response):
    return get_path(response, "data.page_info") or {}


def total_pages(response):
    page_info = get_page_info(response)
    try:
        return int(page_info.get("total_page") or 0)
    except (TypeError, ValueError):
        return 0


def fetch_paged_list(base_url, token, path, params, list_dotted_path, single_page=False):
    first_response = get_json(base_url, token, path, params)
    rows = list(get_path(first_response, list_dotted_path, []) or [])
    responses = [first_response]
    if single_page or first_response.get("code") != 0:
        return rows, responses

    page_count = total_pages(first_response)
    current_page = int(params.get("page") or 1)
    for page in range(current_page + 1, page_count + 1):
        page_params = dict(params)
        page_params["page"] = page
        response = get_json(base_url, token, path, page_params)
        responses.append(response)
        if response.get("code") != 0:
            break
        rows.extend(get_path(response, list_dotted_path, []) or [])
    return rows, responses


def id_key(value):
    if value is None:
        return None
    return str(value)


def is_active_promotion(promotion):
    status = str(promotion.get("status") or "")
    opt_status = str(promotion.get("opt_status") or "")
    second = promotion.get("status_second") or []
    if opt_status != "ENABLE":
        return False
    if status in INACTIVE_STATUSES:
        return False
    return not any(reason in INACTIVE_SECOND_REASONS for reason in second)


def extract_video_materials(promotions):
    records = []
    for promotion in promotions:
        materials = get_path(promotion, "promotion_materials.video_material_list", []) or []
        for material in materials:
            material_id = material.get("material_id")
            if material_id is None:
                continue
            records.append(
                {
                    "project_id": promotion.get("project_id"),
                    "promotion_id": promotion.get("promotion_id"),
                    "promotion_name": promotion.get("promotion_name"),
                    "promotion_status": promotion.get("status"),
                    "promotion_status_first": promotion.get("status_first"),
                    "promotion_status_second": ",".join(promotion.get("status_second") or []),
                    "promotion_opt_status": promotion.get("opt_status"),
                    "material_id": material_id,
                    "video_id": material.get("video_id"),
                    "video_cover_id": material.get("video_cover_id"),
                    "material_status": material.get("material_status"),
                    "material_opt_status": material.get("material_opt_status"),
                    "image_mode": material.get("image_mode"),
                    "material_create_time": material.get("create_time"),
                }
            )
    return records


def build_material_report_params(config, args, metrics, material_ids, promotion_ids):
    filters = []
    if not args.include_extra_report_materials and material_ids:
        filters.append(
            {
                "field": "material_id",
                "type": 2,
                "operator": 1,
                "values": [str(value) for value in material_ids],
            }
        )
    if promotion_ids:
        filters.append(
            {
                "field": "cdp_promotion_id",
                "type": 2,
                "operator": 1,
                "values": [str(value) for value in promotion_ids],
            }
        )
    dimensions = split_csv(args.dimension) or DEFAULT_DIMENSIONS
    return {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "data_topic": args.data_topic,
        "dimensions": dimensions,
        "metrics": metrics,
        "filters": filters,
        "start_time": normalize_time(args.start_date, "start"),
        "end_time": normalize_time(args.end_date, "end"),
        "order_by": [{"field": args.order_field, "type": args.order_type}],
        "page": args.report_page,
        "page_size": args.report_page_size,
    }


def report_success(responses):
    return all(response.get("code") == 0 for response in responses)


def request_ids(responses):
    return [response.get("request_id") for response in responses if response.get("request_id")]


def response_codes(responses):
    return [response.get("code") for response in responses]


def response_messages(responses):
    return [response.get("message") for response in responses if response.get("message")]


def report_value(row, key):
    dimensions = row.get("dimensions") or {}
    metrics = row.get("metrics") or {}
    if key in dimensions:
        return dimensions.get(key)
    if key in metrics:
        return metrics.get(key)
    return row.get(key)


def join_rows(material_records, report_rows):
    report_by_key = {}
    for row in report_rows:
        material_id = report_value(row, "material_id")
        promotion_id = report_value(row, "cdp_promotion_id") or report_value(row, "promotion_id")
        key = (id_key(material_id), id_key(promotion_id))
        report_by_key[key] = row
        if promotion_id is None:
            report_by_key.setdefault((id_key(material_id), None), row)

    joined = []
    for record in material_records:
        report = report_by_key.get((id_key(record["material_id"]), id_key(record["promotion_id"])))
        if report is None:
            report = report_by_key.get((id_key(record["material_id"]), None), {})
        merged = dict(record)
        flat_report = {}
        flat_report.update(report.get("dimensions") or {})
        flat_report.update(report.get("metrics") or {})
        for key, value in flat_report.items():
            if key not in merged:
                merged[key] = value
        merged["has_report_data"] = bool(report)
        joined.append(merged)
    return joined


def write_csv(path, rows):
    fieldnames = list(dict.fromkeys(key for row in rows for key in row.keys()))
    out_path = Path(path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", type=int, help="Override account.advertiser_id from config.")
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    parser.add_argument("--csv-out", help="Optional CSV output path. No file is written by default.")
    parser.add_argument("--start-date", default=today())
    parser.add_argument("--end-date", default=today())
    parser.add_argument("--data-topic", default="MATERIAL_DATA")
    parser.add_argument("--dimension", action="append", help="Custom report dimension or comma-separated dimensions.")
    parser.add_argument("--metric", action="append")
    parser.add_argument(
        "--filter-material-ids",
        action="store_true",
        help="Deprecated compatibility flag. Exact material filtering is now the default.",
    )
    parser.add_argument(
        "--include-extra-report-materials",
        action="store_true",
        help="Do not filter by extracted material IDs. This may include other report-visible material rows under the same units.",
    )
    parser.add_argument("--project-id", type=int)
    parser.add_argument("--promotion-id", action="append")
    parser.add_argument(
        "--active-only",
        action="store_true",
        help="Only keep units that look active/in-delivery after local status checks. Default records all returned units.",
    )
    parser.add_argument(
        "--include-non-active",
        action="store_true",
        help="Deprecated compatibility flag. Non-active units are included by default.",
    )
    parser.add_argument("--promotion-page", type=int, default=1)
    parser.add_argument("--promotion-page-size", type=int, default=20)
    parser.add_argument("--report-page", type=int, default=1)
    parser.add_argument("--report-page-size", type=int, default=100)
    parser.add_argument(
        "--single-page",
        action="store_true",
        help="Only fetch the requested promotion/report page. Default fetches all returned pages.",
    )
    parser.add_argument("--order-field", default="stat_cost")
    parser.add_argument("--order-type", choices=["ASC", "DESC"], default="DESC")
    token_manager.add_authorization_arguments(parser)
    args = parser.parse_args()

    config_path = config_paths.resolve_config_path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(
        config_path,
        config,
        channel=args.channel,
        advertiser_id=args.advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    if args.advertiser_id:
        config.setdefault("account", {})["advertiser_id"] = args.advertiser_id
    base_url = get_path(config, "api.base_url")
    token = get_path(config, "api.access_token")
    metrics = split_csv(args.metric) or DEFAULT_METRICS

    promotion_filtering = {}
    if args.project_id:
        promotion_filtering["project_id"] = args.project_id
    promotion_ids_arg = split_csv(args.promotion_id)
    if promotion_ids_arg:
        promotion_filtering["ids"] = int_values(promotion_ids_arg)

    promotion_params = {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "filtering": promotion_filtering or None,
        "fields": PROMOTION_FIELDS,
        "page": args.promotion_page,
        "page_size": args.promotion_page_size,
    }
    promotions, promotion_responses = fetch_paged_list(
        base_url,
        token,
        "/v3.0/promotion/list/",
        promotion_params,
        "data.list",
        single_page=args.single_page,
    )
    promotion_response = promotion_responses[-1]
    filtered_promotions = [p for p in promotions if is_active_promotion(p)] if args.active_only else promotions
    material_records = extract_video_materials(filtered_promotions)

    report_response = None
    report_responses = []
    report_rows = []
    if material_records:
        material_ids = sorted({int(record["material_id"]) for record in material_records})
        promotion_ids = sorted({int(record["promotion_id"]) for record in material_records if record.get("promotion_id")})
        report_params = build_material_report_params(config, args, metrics, material_ids, promotion_ids)
        report_rows, report_responses = fetch_paged_list(
            base_url,
            token,
            "/v3.0/report/custom/get/",
            report_params,
            "data.rows",
            single_page=args.single_page,
        )
        report_response = report_responses[-1]
    else:
        report_params = None

    rows = join_rows(material_records, report_rows)
    result = {
        "mode": "unit_materials_report",
        "status_handling": "active_only" if args.active_only else "record_only",
        "date_range": {"start_date": args.start_date, "end_date": args.end_date},
        "promotion_endpoint": "/v3.0/promotion/list/",
        "material_report_endpoint": "/v3.0/report/custom/get/",
        "promotion_request_id": promotion_responses[0].get("request_id"),
        "promotion_request_ids": request_ids(promotion_responses),
        "material_report_request_id": report_responses[0].get("request_id") if report_responses else None,
        "material_report_request_ids": request_ids(report_responses),
        "promotion_response_code": promotion_response.get("code"),
        "promotion_response_codes": response_codes(promotion_responses),
        "promotion_response_message": promotion_response.get("message"),
        "promotion_response_messages": response_messages(promotion_responses),
        "material_report_response_code": report_response.get("code") if report_response else None,
        "material_report_response_codes": response_codes(report_responses),
        "material_report_response_message": report_response.get("message") if report_response else None,
        "material_report_response_messages": response_messages(report_responses),
        "promotion_count": len(promotions),
        "selected_promotion_count": len(filtered_promotions),
        "active_like_promotion_count": len([p for p in promotions if is_active_promotion(p)]),
        "material_count": len(material_records),
        "row_count": len(rows),
        "promotion_page_info": get_page_info(promotion_responses[-1]),
        "report_page_info": get_page_info(report_response) if report_response else None,
        "report_total_metrics": get_path(report_response, "data.total_metrics") if report_response else None,
        "report_scope": "promotion_and_extracted_material_ids"
        if not args.include_extra_report_materials
        else "promotion_ids_only_with_extra_report_materials",
        "promotion_params": promotion_params,
        "material_report_params": report_params,
        "rows": rows,
        "excluded_promotions": [
            {
                "promotion_id": p.get("promotion_id"),
                "promotion_name": p.get("promotion_name"),
                "status": p.get("status"),
                "status_first": p.get("status_first"),
                "status_second": p.get("status_second"),
                "opt_status": p.get("opt_status"),
            }
            for p in promotions
            if p not in filtered_promotions
        ],
    }
    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    if args.csv_out:
        write_csv(args.csv_out, rows)
    print(output)
    return 0 if report_success(promotion_responses) and (not report_responses or report_success(report_responses)) else 1


if __name__ == "__main__":
    raise SystemExit(main())
