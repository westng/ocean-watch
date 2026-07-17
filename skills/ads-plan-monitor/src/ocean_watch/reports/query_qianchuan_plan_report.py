#!/usr/bin/env python3
import argparse
import datetime as dt
from decimal import Decimal, InvalidOperation

import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ApiError
from ocean_watch.core.output import write_json
from ocean_watch.integrations.mcp_streamable_http import StreamableHttpMcpClient

QIANCHUAN_MCP_ENDPOINT = "https://open.oceanengine.com/qianchuan/mcp"
PLAN_LIST_TOOL = "qianchuan_uni_promotion_list_v1"
REPORT_CONFIG_TOOL = "qianchuan_report_uni_promotion_config_get_v1"
REPORT_DATA_TOOL = "qianchuan_report_uni_promotion_data_get_v1"
QIANCHUAN_REPORT_TOOLS = [PLAN_LIST_TOOL, REPORT_CONFIG_TOOL, REPORT_DATA_TOOL]
DEFAULT_MARKETING_GOAL = "VIDEO_PROM_GOODS"
DEFAULT_ADLAB_SCENE = "UNI_PROJECT"
REPORT_DATA_TOPIC = "SITE_PROMOTION_PRODUCT_AD"
DEFAULT_PAGE_SIZE = 100
MAX_PAGES = 500
PLAN_HISTORY_WINDOW_DAYS = 180

DEFAULT_FIELDS = [
    "stat_cost",
    "total_pay_order_count_for_roi2",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_prepay_and_pay_order_roi2",
    "total_order_settle_amount_for_roi2_1h",
    "total_order_settle_count_for_roi2_1h",
    "total_prepay_and_pay_settle_roi2_1h",
]

MONEY_FIELDS = {
    "stat_cost",
    "total_pay_order_gmv_include_coupon_for_roi2",
    "total_order_settle_amount_for_roi2_1h",
}
COUNT_FIELDS = {
    "total_pay_order_count_for_roi2",
    "total_order_settle_count_for_roi2_1h",
}
STATUS_LABELS = {
    "DELIVERY_OK": "投放中",
    "DISABLE": "已暂停",
    "SYSTEM_DISABLE": "系统暂停",
    "AUDIT": "审核中",
    "OFFLINE_AUDIT": "审核不通过",
    "OFFLINE_BALANCE": "账户余额不足",
    "OFFLINE_BUDGET": "超出预算",
    "TIME_NO_REACH": "未到投放时间",
    "TIME_DONE": "已完成",
    "DELETED": "已删除",
}
COST_GUARANTEE_LABELS = {
    "IN_EFFECT": "生效中",
    "INVALID": "未生效",
    "CONFIRMING": "确认中",
    "PAID": "已赔付",
    "ENDED": "已结束",
    "DEFAULT": "默认状态",
}
BID_TYPE_LABELS = {
    "SMART_BID_CUSTOM": "控成本投放",
    "SMART_BID_CONSERVATIVE": "放量投放",
}
BUDGET_MODE_LABELS = {
    "BUDGET_MODE_DAY": "日预算",
    "BUDGET_MODE_TOTAL": "总预算",
    "BUDGET_MODE_INFINITE": "不限预算",
}


def today():
    return dt.date.today().isoformat()


def parse_date(value, field):
    try:
        return dt.date.fromisoformat(str(value))
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"{field} must use YYYY-MM-DD") from exc


def number(value, field="metric"):
    if value is None or value == "":
        return Decimal(0)
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ApiError(
            "Qianchuan report returned an invalid numeric metric",
            {"field": field, "value": str(value)[:128]},
        ) from exc
    if not parsed.is_finite():
        raise ApiError(
            "Qianchuan report returned a non-finite numeric metric",
            {"field": field, "value": str(value)[:128]},
        )
    return parsed


def rounded(value, places=4, field="metric"):
    quantizer = Decimal(1).scaleb(-places)
    return float(number(value, field).quantize(quantizer))


def report_value(container, field):
    value = (container or {}).get(field)
    if not isinstance(value, dict):
        return value
    if value.get("Value") is not None:
        return value["Value"]
    return value.get("ValueStr")


def report_ad_id(report_row, *, page=None):
    value = report_value(report_row.get("dimensions") or {}, "ad_id")
    normalized = str(value or "")
    if not normalized.isdigit() or int(normalized) <= 0:
        raise ApiError(
            "Qianchuan report returned an invalid plan ID",
            {
                "source": "report_data",
                "page": page,
                "field": "ad_id",
                "value": normalized[:128],
            },
        )
    return normalized


def pagination_integer(value, *, source, page, field, minimum=0):
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = minimum - 1
    if (
        isinstance(value, bool)
        or parsed < minimum
        or isinstance(value, float) and value != parsed
    ):
        raise ApiError(
            "Qianchuan report returned invalid pagination metadata",
            {"source": source, "page": page, field: value},
        )
    return parsed


def page_state(
    data,
    *,
    source,
    page,
    max_pages,
    total_key,
    accumulated_rows,
    row_count=0,
    expected_pages=None,
    expected_total=None,
):
    page_info = data.get("page_info") if isinstance(data, dict) else None
    if not isinstance(page_info, dict):
        raise ApiError(
            "Qianchuan report returned invalid pagination metadata",
            {"source": source, "page": page, "page_info": None},
        )
    returned_page = pagination_integer(
        page_info.get("page"),
        source=source,
        page=page,
        field="returned_page",
        minimum=1,
    )
    if returned_page != page:
        raise ApiError(
            "Qianchuan report returned an unexpected page",
            {"source": source, "page": page, "returned_page": returned_page},
        )
    total_pages = pagination_integer(
        page_info.get("total_page"),
        source=source,
        page=page,
        field="total_page",
    )
    total_rows = pagination_integer(
        page_info.get(total_key),
        source=source,
        page=page,
        field=total_key,
    )
    if total_pages == 0 and (row_count or total_rows):
        raise ApiError(
            "Qianchuan report pagination contradicts returned rows",
            {
                "source": source,
                "page": page,
                "total_page": total_pages,
                total_key: total_rows,
                "row_count": row_count,
            },
        )
    if total_pages > max_pages:
        raise ApiError(
            "Qianchuan report exceeds the pagination safety cap",
            {"source": source, "total_page": total_pages, "max_pages": max_pages},
        )
    if total_pages and returned_page > total_pages:
        raise ApiError(
            "Qianchuan report pagination contradicts the returned page",
            {
                "source": source,
                "page": page,
                "returned_page": returned_page,
                "total_page": total_pages,
            },
        )
    if expected_pages is not None and total_pages != expected_pages:
        raise ApiError(
            "Qianchuan report pagination changed during traversal",
            {
                "source": source,
                "page": page,
                "total_page": total_pages,
                "expected": expected_pages,
            },
        )
    if expected_total is not None and total_rows != expected_total:
        raise ApiError(
            "Qianchuan report row total changed during traversal",
            {
                "source": source,
                "page": page,
                total_key: total_rows,
                "expected": expected_total,
            },
        )
    if accumulated_rows > total_rows:
        raise ApiError(
            "Qianchuan report pagination returned too many rows",
            {
                "source": source,
                "page": page,
                total_key: total_rows,
                "accumulated_rows": accumulated_rows,
            },
        )
    if page >= total_pages and accumulated_rows != total_rows:
        raise ApiError(
            "Qianchuan report pagination returned an incomplete row count",
            {
                "source": source,
                "page": page,
                total_key: total_rows,
                "accumulated_rows": accumulated_rows,
            },
        )
    return total_pages, total_rows


def fetch_plan_metadata(
    client,
    advertiser_id,
    ad_ids,
    *,
    marketing_goal=DEFAULT_MARKETING_GOAL,
    adlab_scene=DEFAULT_ADLAB_SCENE,
    status="ALL",
    page_size=DEFAULT_PAGE_SIZE,
    max_pages=MAX_PAGES,
    today_value=None,
    allow_missing=False,
):
    rows = []
    found_ids = set()
    target_ids = set(dict.fromkeys(str(value) for value in ad_ids))
    request_ids = []
    page_count = 0
    today_value = today_value or dt.date.today()
    if not target_ids:
        return {
            "rows": [],
            "page_count": 0,
            "request_ids": [],
            "truncated": False,
            "missing_ad_ids": [],
        }

    period_start = today_value - dt.timedelta(days=PLAN_HISTORY_WINDOW_DAYS - 1)
    page = 1
    expected_pages = None
    expected_total = None
    accumulated_rows = 0
    while True:
        payload = client.call_tool(
            PLAN_LIST_TOOL,
            {
                "advertiser_id": int(advertiser_id),
                "start_time": f"{period_start.isoformat()} 00:00:00",
                "end_time": f"{today_value.isoformat()} 23:59:59",
                "marketing_goal": marketing_goal,
                "adlab_scene": adlab_scene,
                "fields": ["stat_cost"],
                "filtering": {"status": "ALL", "having_cost": "ALL"},
                "need_compensate_info": True,
                "order_field": "create_time",
                "order_type": "DESC",
                "page": page,
                "page_size": page_size,
            },
        )
        if payload.get("request_id"):
            request_ids.append(payload["request_id"])
        data = payload.get("data") or {}
        page_rows = data.get("ad_list") or []
        if not isinstance(page_rows, list):
            raise ApiError(
                "Qianchuan plan metadata rows must be a list",
                {"source": "plan_metadata", "page": page},
            )
        for row in page_rows:
            ad_id = str((row.get("ad_info") or {}).get("id"))
            if ad_id in target_ids and ad_id not in found_ids:
                rows.append(row)
                found_ids.add(ad_id)
        page_count += 1
        accumulated_rows += len(page_rows)
        expected_pages, expected_total = page_state(
            data,
            source="plan_metadata",
            page=page,
            max_pages=max_pages,
            total_key="total_num",
            accumulated_rows=accumulated_rows,
            row_count=len(page_rows),
            expected_pages=expected_pages,
            expected_total=expected_total,
        )
        if page >= expected_pages or found_ids == target_ids:
            break
        page += 1

    missing_ids = target_ids - found_ids
    if missing_ids and not allow_missing:
        raise ApiError(
            "Qianchuan report plan metadata could not be resolved",
            {"source": "plan_metadata", "missing_ad_ids": sorted(missing_ids)},
        )
    return {
        "rows": rows,
        "page_count": page_count,
        "request_ids": request_ids,
        "truncated": False,
        "missing_ad_ids": sorted(missing_ids),
    }


def fetch_report_rows(
    client,
    advertiser_id,
    *,
    start_date,
    end_date,
    page_size=DEFAULT_PAGE_SIZE,
    max_pages=MAX_PAGES,
):
    rows = []
    request_ids = []
    page = 1
    expected_pages = None
    expected_total = None
    seen_ad_ids = set()
    while True:
        payload = client.call_tool(
            REPORT_DATA_TOOL,
            {
                "advertiser_id": int(advertiser_id),
                "data_topic": REPORT_DATA_TOPIC,
                "dimensions": ["ad_id"],
                "metrics": DEFAULT_FIELDS,
                "filters": [],
                "start_time": f"{start_date} 00:00:00",
                "end_time": f"{end_date} 23:59:59",
                "order_by": [{"field": "stat_cost", "type": 2}],
                "page": page,
                "page_size": page_size,
            },
        )
        if payload.get("request_id"):
            request_ids.append(payload["request_id"])
        data = payload.get("data") or {}
        page_rows = data.get("rows") or []
        if not isinstance(page_rows, list):
            raise ApiError(
                "Qianchuan report rows must be a list",
                {"source": "report_data", "page": page},
            )
        for row in page_rows:
            ad_id = report_ad_id(row, page=page)
            if ad_id in seen_ad_ids:
                raise ApiError(
                    "Qianchuan report returned a duplicate plan row",
                    {"source": "report_data", "page": page, "ad_id": ad_id},
                )
            seen_ad_ids.add(ad_id)
        rows.extend(page_rows)
        expected_pages, expected_total = page_state(
            data,
            source="report_data",
            page=page,
            max_pages=max_pages,
            total_key="total_number",
            accumulated_rows=len(rows),
            row_count=len(page_rows),
            expected_pages=expected_pages,
            expected_total=expected_total,
        )
        if page >= expected_pages:
            break
        page += 1
    return {
        "rows": rows,
        "page_count": page,
        "request_ids": request_ids,
        "truncated": False,
    }


def metric_decimal(report_row, field, *, ad_id=None):
    value = report_value(report_row.get("metrics") or {}, field)
    if value is None or value == "":
        raise ApiError(
            "Qianchuan report omitted a required metric",
            {"source": "report_data", "ad_id": ad_id, "field": field},
        )
    parsed = number(value, field)
    if field in COUNT_FIELDS and parsed != parsed.to_integral_value():
        raise ApiError(
            "Qianchuan report returned a fractional count",
            {"field": field, "value": str(value)[:128], "ad_id": ad_id},
        )
    return parsed


def normalize_metric(field, parsed):
    if field in MONEY_FIELDS:
        return rounded(parsed, 2, field)
    if field in COUNT_FIELDS:
        return int(parsed)
    return rounded(parsed, 4, field)


def normalize_row(report_row, metadata=None):
    metadata = metadata or {}
    ad_info = metadata.get("ad_info") or {}
    compensate_info = ad_info.get("compensate_info") or {}
    products = metadata.get("product_info") or []
    creators = metadata.get("room_info") or []
    dimensions = report_row.get("dimensions") or {}
    ad_id = report_value(dimensions, "ad_id")
    normalized_stats = {
        field: normalize_metric(
            field,
            metric_decimal(report_row, field, ad_id=str(ad_id) if ad_id is not None else None),
        )
        for field in DEFAULT_FIELDS
    }
    return {
        "ad_id": str(ad_id) if ad_id is not None else None,
        "metadata_available": bool(metadata),
        "metadata_missing_reason": None if metadata else "plan_not_returned_by_metadata_api",
        "name": ad_info.get("name"),
        "status": ad_info.get("status"),
        "status_label": STATUS_LABELS.get(ad_info.get("status"), ad_info.get("status")),
        "opt_status": ad_info.get("opt_status"),
        "budget": ad_info.get("budget"),
        "budget_mode": ad_info.get("budget_mode"),
        "budget_mode_label": BUDGET_MODE_LABELS.get(
            ad_info.get("budget_mode"),
            ad_info.get("budget_mode"),
        ),
        "roi_goal": ad_info.get("roi2_goal"),
        "bid": ad_info.get("roi2_goal"),
        "smart_bid_type": ad_info.get("smart_bid_type"),
        "bid_type_label": BID_TYPE_LABELS.get(
            ad_info.get("smart_bid_type"),
            ad_info.get("smart_bid_type"),
        ),
        "cost_guarantee_status": compensate_info.get("compensate_status"),
        "cost_guarantee_status_label": COST_GUARANTEE_LABELS.get(
            compensate_info.get("compensate_status"),
            compensate_info.get("compensate_status"),
        ),
        "cost_guarantee_result": compensate_info.get("status"),
        "cost_guarantee_reason": compensate_info.get("reason"),
        "creator_ids": [str(item["anchor_id"]) for item in creators if item.get("anchor_id") is not None],
        "creator_names": [item["anchor_name"] for item in creators if item.get("anchor_name")],
        "product_ids": [str(item["product_id"]) for item in products if item.get("product_id") is not None],
        "product_names": [item["product_name"] for item in products if item.get("product_name")],
        **normalized_stats,
        "roi": normalized_stats.get("total_prepay_and_pay_order_roi2"),
    }


def build_summary(report_rows, total_plan_count, metadata_missing_count=0):
    parsed_rows = [
        {
            field: metric_decimal(row, field, ad_id=report_ad_id(row))
            for field in DEFAULT_FIELDS
        }
        for row in report_rows
    ]
    cost = sum((row["stat_cost"] for row in parsed_rows), Decimal(0))
    gmv = sum(
        (row["total_pay_order_gmv_include_coupon_for_roi2"] for row in parsed_rows),
        Decimal(0),
    )
    settled = sum(
        (row["total_order_settle_amount_for_roi2_1h"] for row in parsed_rows),
        Decimal(0),
    )
    orders = sum(
        (row["total_pay_order_count_for_roi2"] for row in parsed_rows),
        Decimal(0),
    )
    return {
        "plan_count": total_plan_count,
        "plans_with_cost": sum(1 for row in parsed_rows if row["stat_cost"] > 0),
        "metadata_missing_count": metadata_missing_count,
        "total_cost": rounded(cost, 2),
        "total_pay_order_count": int(orders),
        "total_pay_order_gmv": rounded(gmv, 2),
        "total_pay_roi": rounded(gmv / cost, 4) if cost else 0.0,
        "total_settled_amount_1h": rounded(settled, 2),
        "total_settled_roi_1h": rounded(settled / cost, 4) if cost else 0.0,
    }


def query_plan_report(
    client,
    advertiser_id,
    *,
    start_date,
    end_date,
    top=10,
    marketing_goal=DEFAULT_MARKETING_GOAL,
    adlab_scene=DEFAULT_ADLAB_SCENE,
    status="ALL",
    max_pages=MAX_PAGES,
):
    report = fetch_report_rows(
        client,
        advertiser_id,
        start_date=start_date,
        end_date=end_date,
        max_pages=max_pages,
    )
    report_ad_ids = [report_ad_id(row) for row in report["rows"]]
    metadata = fetch_plan_metadata(
        client,
        advertiser_id,
        report_ad_ids,
        marketing_goal=marketing_goal,
        adlab_scene=adlab_scene,
        status=status,
        max_pages=max_pages,
        allow_missing=status == "ALL",
    )
    metadata_by_id = {
        str((row.get("ad_info") or {}).get("id")): row
        for row in metadata["rows"]
        if (row.get("ad_info") or {}).get("id") is not None
    }
    selected = []
    for report_row in report["rows"]:
        ad_id = report_ad_id(report_row)
        plan_metadata = metadata_by_id.get(ad_id)
        metadata_status = ((plan_metadata or {}).get("ad_info") or {}).get("status")
        if status != "ALL" and metadata_status != status:
            continue
        selected.append((report_row, normalize_row(report_row, plan_metadata)))
    selected.sort(
        key=lambda pair: metric_decimal(pair[0], "stat_cost", ad_id=pair[1]["ad_id"]),
        reverse=True,
    )
    selected_report_rows = [pair[0] for pair in selected]
    rows = [pair[1] for pair in selected]
    for index, row in enumerate(rows, start=1):
        row["rank"] = index
    displayed = rows if top == 0 else rows[:top]
    return {
        "ok": True,
        "channel": "qianchuan",
        "transport": "official_mcp",
        "advertiser_id": str(advertiser_id),
        "date_range": {"start_date": start_date, "end_date": end_date},
        "scope": {
            "marketing_goal": marketing_goal,
            "adlab_scene": adlab_scene,
            "status": status,
        },
        "summary": build_summary(
            selected_report_rows,
            len(rows),
            metadata_missing_count=sum(not row["metadata_available"] for row in rows),
        ),
        "rows": displayed,
        "displayed_count": len(displayed),
        "total_row_count": len(rows),
        "truncated": metadata["truncated"] or report["truncated"],
        "page_count": {
            "plan_metadata": metadata["page_count"],
            "report_data": report["page_count"],
        },
        "request_ids": [*report["request_ids"], *metadata["request_ids"]],
        "amount_unit": "CNY",
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Query Qianchuan all-domain plan spend through the official MCP.")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--start-date", default=today())
    parser.add_argument("--end-date", default=today())
    parser.add_argument("--top", type=int, default=10, help="Rows to return; use 0 for all rows.")
    parser.add_argument("--status", default="ALL")
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    args = parser.parse_args(argv)

    start = parse_date(args.start_date, "start_date")
    end = parse_date(args.end_date, "end_date")
    if start > end:
        parser.error("start_date cannot be after end_date")
    if args.top < 0:
        parser.error("top must be zero or a positive integer")

    config_path = config_paths.resolve_config_path(args.config)
    runtime = token_manager.ensure_access_token(
        config_path,
        channel="qianchuan",
        advertiser_id=args.advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    client = StreamableHttpMcpClient(
        QIANCHUAN_MCP_ENDPOINT,
        get_path(runtime, "api.access_token"),
        tool_range=QIANCHUAN_REPORT_TOOLS,
    )
    result = query_plan_report(
        client,
        args.advertiser_id,
        start_date=start.isoformat(),
        end_date=end.isoformat(),
        top=args.top,
        status=args.status,
    )
    write_json(result, args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
