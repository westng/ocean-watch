#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from decimal import Decimal, InvalidOperation

from ocean_watch.accounts import managed_accounts
from ocean_watch.api.client import OceanEngineClient
from ocean_watch.auth import channels, token_manager
from ocean_watch.core import config_paths
from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ApiError, ConfigurationError, OceanWatchError
from ocean_watch.core.output import write_json
from ocean_watch.integrations.mcp_streamable_http import StreamableHttpMcpClient
from ocean_watch.reports import query_qianchuan_plan_report

MARKETING_REPORT_PATH = "/v3.0/report/custom/get/"
RETRYABLE_API_CODES = {"40100", "51010"}
RETRYABLE_HTTP_STATUSES = {429, 500, 502, 503, 504}
METRIC_BASES = {
    "marketing": {
        "spend": "stat_cost",
        "orders": "in_app_order_count",
        "gmv": "in_app_order_gmv",
        "roi": "in_app_order_roi",
        "net_orders_1h": "in_app_order_net_count_1h",
        "net_gmv_1h": "in_app_order_net_gmv_1h",
        "net_roi_1h": "in_app_order_net_roi_1h",
    },
    "qianchuan": {
        "spend": "stat_cost",
        "orders": "total_pay_order_count_for_roi2",
        "gmv": "total_pay_order_gmv_include_coupon_for_roi2",
        "roi": "total_prepay_and_pay_order_roi2",
        "net_orders_1h": "total_order_settle_count_for_roi2_1h",
        "net_gmv_1h": "total_order_settle_amount_for_roi2_1h",
        "net_roi_1h": "total_prepay_and_pay_settle_roi2_1h",
    },
}
MARKETING_METRICS = [
    "stat_cost",
    "in_app_order_count",
    "in_app_order_gmv",
    "in_app_order_roi",
    "in_app_order_net_count_1h",
    "in_app_order_net_gmv_1h",
    "in_app_order_net_roi_1h",
]
PRESENTATION_COLUMNS = (
    ("channel_name", "渠道"),
    ("name", "账户名称"),
    ("advertiser_id", "广告主 ID"),
    ("enabled_label", "启用状态"),
    ("query_status_label", "查询状态"),
    ("spend", "消耗"),
    ("orders", "订单"),
    ("gmv", "GMV"),
    ("roi", "ROI"),
    ("net_orders_1h", "1h 结算订单"),
    ("net_gmv_1h", "1h 结算金额"),
    ("net_roi_1h", "1h 结算 ROI"),
    ("error_summary", "失败原因"),
)
PRESENTATION_MONEY_FIELDS = {
    "spend",
    "gmv",
    "net_gmv_1h",
    "total_spend",
    "total_gmv",
    "total_net_gmv_1h",
}
PRESENTATION_RATIO_FIELDS = {
    "roi",
    "net_roi_1h",
    "weighted_roi",
    "weighted_net_roi_1h",
}
METRIC_LABELS = {
    "spend": "消耗",
    "orders": "订单",
    "gmv": "GMV",
    "roi": "ROI",
    "net_orders_1h": "1h 结算订单",
    "net_gmv_1h": "1h 结算金额",
    "net_roi_1h": "1h 结算 ROI",
}


def today():
    return dt.date.today().isoformat()


def number(value):
    if value is None or value == "":
        return Decimal(0)
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ApiError("Report returned an invalid numeric metric", {"value": str(value)}) from exc
    if not parsed.is_finite():
        raise ApiError("Report returned a non-finite numeric metric")
    return parsed


def rounded(value, places=4):
    return float(number(value).quantize(Decimal(1).scaleb(-places)))


def marketing_account_report(config_path, account, start_date, end_date):
    runtime = token_manager.ensure_access_token(
        config_path,
        channel="marketing",
        advertiser_id=account["advertiser_id"],
        auth_account_id=account.get("auth_account_id"),
    )
    client = OceanEngineClient(
        get_path(runtime, "api.base_url"),
        get_path(runtime, "api.access_token"),
    )
    response = client.get(
        MARKETING_REPORT_PATH,
        params={
            "advertiser_id": account["advertiser_id"],
            "data_topic": "BASIC_DATA",
            "dimensions": [],
            "metrics": MARKETING_METRICS,
            "filters": [],
            "start_time": f"{start_date} 00:00:00",
            "end_time": f"{end_date} 23:59:59",
            "order_by": [{"field": "stat_cost", "type": "DESC"}],
            "page": 1,
            "page_size": 100,
        },
    )
    if response.get("code") != 0:
        raise ApiError(
            "Marketing account report failed",
            {
                "code": response.get("code"),
                "http_status": response.get("http_status"),
                "message": response.get("message"),
                "request_id": response.get("request_id"),
            },
        )
    report_rows = get_path(response, "data.rows", []) or []
    metrics = get_path(response, "data.total_metrics") or (
        (report_rows[0].get("metrics") or {}) if report_rows else {}
    )
    return {
        "metric_basis": METRIC_BASES["marketing"],
        "spend": rounded(metrics.get("stat_cost"), 2),
        "orders": int(number(metrics.get("in_app_order_count"))),
        "gmv": rounded(metrics.get("in_app_order_gmv"), 2),
        "roi": rounded(metrics.get("in_app_order_roi"), 4),
        "net_orders_1h": int(number(metrics.get("in_app_order_net_count_1h"))),
        "net_gmv_1h": rounded(metrics.get("in_app_order_net_gmv_1h"), 2),
        "net_roi_1h": rounded(metrics.get("in_app_order_net_roi_1h"), 4),
        "request_ids": [response.get("request_id")] if response.get("request_id") else [],
    }


def qianchuan_account_report(config_path, account, start_date, end_date):
    runtime = token_manager.ensure_access_token(
        config_path,
        channel="qianchuan",
        advertiser_id=account["advertiser_id"],
        auth_account_id=account.get("auth_account_id"),
    )
    client = StreamableHttpMcpClient(
        query_qianchuan_plan_report.QIANCHUAN_MCP_ENDPOINT,
        get_path(runtime, "api.access_token"),
        tool_range=query_qianchuan_plan_report.QIANCHUAN_REPORT_TOOLS,
    )
    result = query_qianchuan_plan_report.query_plan_report(
        client,
        account["advertiser_id"],
        start_date=start_date,
        end_date=end_date,
        top=0,
    )
    if not result.get("ok") or result.get("truncated"):
        raise ApiError("Qianchuan account report was incomplete")
    summary = result["summary"]
    return {
        "metric_basis": METRIC_BASES["qianchuan"],
        "spend": summary["total_cost"],
        "orders": summary["total_pay_order_count"],
        "gmv": summary["total_pay_order_gmv"],
        "roi": summary["total_pay_roi"],
        "net_orders_1h": sum(
            int(row.get("total_order_settle_count_for_roi2_1h") or 0)
            for row in result["rows"]
        ),
        "net_gmv_1h": summary["total_settled_amount_1h"],
        "net_roi_1h": summary["total_settled_roi_1h"],
        "request_ids": result["request_ids"],
    }


def query_account(config_path, account, start_date, end_date):
    if account["channel"] == "marketing":
        metrics = marketing_account_report(config_path, account, start_date, end_date)
    elif account["channel"] == "qianchuan":
        metrics = qianchuan_account_report(config_path, account, start_date, end_date)
    else:
        raise ConfigurationError(f"unsupported managed account channel: {account['channel']}")
    return {
        **account,
        "channel_name": channels.CHANNELS[account["channel"]]["display_name"],
        "query_status": "ok",
        **metrics,
    }


def failed_account(account, error):
    if isinstance(error, OceanWatchError):
        details = error.as_dict()["error"]
    else:
        details = {
            "code": "unexpected_error",
            "message": str(error),
            "details": {},
        }
    return {
        **account,
        "channel_name": channels.CHANNELS[account["channel"]]["display_name"],
        "query_status": "failed",
        "error": details,
    }


def query_with_retry(
    query_fn,
    config_path,
    account,
    start_date,
    end_date,
    *,
    retry_delays=(1, 2),
    sleep_fn=time.sleep,
):
    for attempt in range(len(retry_delays) + 1):
        try:
            return query_fn(config_path, account, start_date, end_date)
        except ApiError as exc:
            code = str(exc.details.get("code") or "")
            http_status = exc.details.get("http_status")
            retryable = (
                code in RETRYABLE_API_CODES
                or http_status in RETRYABLE_HTTP_STATUSES
                or exc.details.get("retryable") is True
            )
            if not retryable or attempt >= len(retry_delays):
                raise
            sleep_fn(retry_delays[attempt])
    raise AssertionError("unreachable")


def query_accounts(
    config_path,
    accounts,
    start_date,
    end_date,
    *,
    concurrency=4,
    query_fn=query_account,
    retry_delays=(1, 2),
    sleep_fn=time.sleep,
):
    results = [None] * len(accounts)
    workers = max(1, min(int(concurrency), len(accounts) or 1))
    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {
            executor.submit(
                query_with_retry,
                query_fn,
                config_path,
                account,
                start_date,
                end_date,
                retry_delays=retry_delays,
                sleep_fn=sleep_fn,
            ): index
            for index, account in enumerate(accounts)
        }
        for future in as_completed(futures):
            index = futures[future]
            try:
                results[index] = future.result()
            except Exception as exc:
                results[index] = failed_account(accounts[index], exc)
    return results


def summarize_metrics(rows, *, metric_basis=None):
    spend = sum((number(row.get("spend")) for row in rows), Decimal(0))
    orders = sum((number(row.get("orders")) for row in rows), Decimal(0))
    gmv = sum((number(row.get("gmv")) for row in rows), Decimal(0))
    net_orders = sum((number(row.get("net_orders_1h")) for row in rows), Decimal(0))
    net_gmv = sum((number(row.get("net_gmv_1h")) for row in rows), Decimal(0))
    return {
        "account_count": len(rows),
        "total_spend": rounded(spend, 2),
        "total_orders": int(orders),
        "total_gmv": rounded(gmv, 2),
        "weighted_roi": rounded(gmv / spend, 4) if spend else 0.0,
        "total_net_orders_1h": int(net_orders),
        "total_net_gmv_1h": rounded(net_gmv, 2),
        "weighted_net_roi_1h": rounded(net_gmv / spend, 4) if spend else 0.0,
        "metric_basis": metric_basis,
    }


def build_summary(rows):
    successful = [row for row in rows if row["query_status"] == "ok"]
    total_spend = sum((number(row.get("spend")) for row in successful), Decimal(0))
    channel_summaries = {
        channel: summarize_metrics(
            [row for row in successful if row.get("channel") == channel],
            metric_basis=METRIC_BASES[channel],
        )
        for channel in channels.CHANNELS
        if any(row.get("channel") == channel for row in successful)
    }
    comparable = len(channel_summaries) <= 1
    comparable_summary = next(iter(channel_summaries.values()), None)
    return {
        "account_count": len(rows),
        "successful_account_count": len(successful),
        "failed_account_count": len(rows) - len(successful),
        "total_spend": rounded(total_spend, 2),
        "total_gmv": (
            comparable_summary["total_gmv"]
            if comparable and comparable_summary
            else 0.0 if comparable else None
        ),
        "weighted_roi": (
            comparable_summary["weighted_roi"]
            if comparable and comparable_summary
            else 0.0 if comparable else None
        ),
        "aggregate_gmv_comparable": comparable,
        "mixed_channel_note": (
            None
            if comparable
            else "Marketing and Qianchuan GMV/ROI use different official metric definitions."
        ),
        "channel_summaries": channel_summaries,
    }


def presentation_value(field, value):
    if value is None or value == "":
        return "—"
    if field in PRESENTATION_MONEY_FIELDS:
        value = f"¥{number(value):,.2f}"
    elif field in PRESENTATION_RATIO_FIELDS:
        value = f"{number(value):.4f}".rstrip("0").rstrip(".")
    return str(value).replace("|", "\\|").replace("\r", " ").replace("\n", " ")


def account_error_summary(row):
    error = row.get("error") or {}
    details = error.get("details") or {}
    code = details.get("code") or error.get("code")
    message = details.get("message") or error.get("message")
    if code and message:
        return f"{code}: {message}"
    return str(message or code or "—")


def presentation_account_row(row):
    successful = row.get("query_status") == "ok"
    return {
        **row,
        "channel_name": row.get("channel_name")
        or channels.CHANNELS[row["channel"]]["display_name"],
        "enabled_label": "已启用" if row.get("enabled") else "已停用",
        "query_status_label": "成功" if successful else "失败",
        "spend": row.get("spend") if successful else None,
        "orders": row.get("orders") if successful else None,
        "gmv": row.get("gmv") if successful else None,
        "roi": row.get("roi") if successful else None,
        "net_orders_1h": row.get("net_orders_1h") if successful else None,
        "net_gmv_1h": row.get("net_gmv_1h") if successful else None,
        "net_roi_1h": row.get("net_roi_1h") if successful else None,
        "error_summary": "—" if successful else account_error_summary(row),
    }


def markdown_table(columns, rows):
    table = [
        "| " + " | ".join(label for _, label in columns) + " |",
        "| " + " | ".join("---" for _ in columns) + " |",
    ]
    table.extend(
        "| "
        + " | ".join(presentation_value(field, row.get(field)) for field, _ in columns)
        + " |"
        for row in rows
    )
    return "\n".join(table)


def render_presentation(summary, rows, start_date, end_date):
    date_range = start_date if start_date == end_date else f"{start_date} 至 {end_date}"
    lines = [
        f"**查询日期：** {date_range}",
        "",
        (
            "**负责账户汇总：** "
            f"共 {summary['account_count']} 个；成功 {summary['successful_account_count']} 个；"
            f"失败 {summary['failed_account_count']} 个；总消耗 ¥{summary['total_spend']:,.2f}"
        ),
    ]
    if summary["aggregate_gmv_comparable"] and summary["channel_summaries"]:
        lines.extend([
            "",
            (
                "**同渠道成交汇总：** "
                f"GMV ¥{summary['total_gmv']:,.2f}；加权 ROI "
                f"{summary['weighted_roi']:.4f}".rstrip("0").rstrip(".")
            ),
        ])
    elif summary["mixed_channel_note"]:
        lines.extend(["", f"**跨渠道说明：** {summary['mixed_channel_note']}"])

    lines.extend([
        "",
        "### 账户明细",
        "",
        markdown_table(PRESENTATION_COLUMNS, [presentation_account_row(row) for row in rows]),
        "",
        "### 分渠道汇总",
        "",
    ])
    channel_summary_columns = (
        ("channel_name", "渠道"),
        ("account_count", "成功账户"),
        ("total_spend", "消耗"),
        ("total_orders", "订单"),
        ("total_gmv", "GMV"),
        ("weighted_roi", "ROI"),
        ("total_net_orders_1h", "1h 结算订单"),
        ("total_net_gmv_1h", "1h 结算金额"),
        ("weighted_net_roi_1h", "1h 结算 ROI"),
    )
    channel_rows = [
        {
            **summary["channel_summaries"][channel],
            "channel_name": channels.CHANNELS[channel]["display_name"],
        }
        for channel in channels.CHANNELS
        if channel in summary["channel_summaries"]
    ]
    lines.extend([
        markdown_table(channel_summary_columns, channel_rows),
        "",
        "### 指标口径",
        "",
    ])
    metric_columns = (
        ("channel_name", "渠道"),
        ("metric", "指标"),
        ("field", "官方字段"),
    )
    metric_rows = [
        {
            "channel_name": channels.CHANNELS[channel]["display_name"],
            "metric": METRIC_LABELS[metric],
            "field": field,
        }
        for channel in channels.CHANNELS
        if any(row.get("channel") == channel for row in rows)
        for metric, field in METRIC_BASES[channel].items()
    ]
    lines.append(markdown_table(metric_columns, metric_rows))
    return "\n".join(lines)


def presentation_contract(summary, rows, start_date, end_date):
    return {
        "format": "markdown",
        "required": True,
        "allow_column_omission": False,
        "allow_column_reordering": False,
        "columns": [
            {"field": field, "label": label}
            for field, label in PRESENTATION_COLUMNS
        ],
        "required_sections": [
            "date_range",
            "summary",
            "accounts",
            "channel_summaries",
            "metric_basis",
        ],
        "rendered_markdown": render_presentation(summary, rows, start_date, end_date),
    }


def build_result(rows, start_date, end_date):
    summary = build_summary(rows)
    return {
        "ok": all(row["query_status"] == "ok" for row in rows),
        "mode": "managed_accounts_spend",
        "date_range": {
            "start_date": start_date,
            "end_date": end_date,
        },
        "summary": summary,
        "accounts": rows,
        "presentation": presentation_contract(summary, rows, start_date, end_date),
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Query spend for locally responsible accounts.")
    parser.add_argument("--config")
    parser.add_argument("--channel", action="append", choices=tuple(channels.CHANNELS))
    parser.add_argument("--start-date", default=today())
    parser.add_argument("--end-date", default=today())
    parser.add_argument("--include-disabled", action="store_true")
    parser.add_argument("--concurrency", type=int, default=4)
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    args = parser.parse_args(argv)
    if args.concurrency < 1 or args.concurrency > 8:
        parser.error("concurrency must be between 1 and 8")
    try:
        start_date = dt.date.fromisoformat(args.start_date)
        end_date = dt.date.fromisoformat(args.end_date)
    except ValueError:
        parser.error("start-date and end-date must use YYYY-MM-DD")
    if start_date > end_date:
        parser.error("start-date cannot be after end-date")

    config_path = config_paths.resolve_config_path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    accounts = managed_accounts.list_accounts(
        config,
        enabled_only=not args.include_disabled,
    )
    selected_channels = set(args.channel or channels.CHANNELS)
    accounts = [account for account in accounts if account["channel"] in selected_channels]
    if not accounts:
        raise ConfigurationError("no managed accounts matched this query")
    rows = query_accounts(
        config_path,
        accounts,
        start_date.isoformat(),
        end_date.isoformat(),
        concurrency=args.concurrency,
    )
    result = build_result(rows, start_date.isoformat(), end_date.isoformat())
    write_json(result, args.out)
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
