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
    gmv = sum((number(row.get("gmv")) for row in rows), Decimal(0))
    return {
        "account_count": len(rows),
        "total_spend": rounded(spend, 2),
        "total_gmv": rounded(gmv, 2),
        "weighted_roi": rounded(gmv / spend, 4) if spend else 0.0,
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
    result = {
        "ok": all(row["query_status"] == "ok" for row in rows),
        "mode": "managed_accounts_spend",
        "date_range": {
            "start_date": start_date.isoformat(),
            "end_date": end_date.isoformat(),
        },
        "summary": build_summary(rows),
        "accounts": rows,
    }
    write_json(result, args.out)
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
