#!/usr/bin/env python3
import argparse
import json
from decimal import Decimal, InvalidOperation

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import (
    OceanEngineClient,
    QianchuanClientFactory,
    qianchuan_advertiser_lock_path,
)
from ocean_watch.auth import authorization_store
from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.process_lock import ProcessLock

MARKETING_ENDPOINTS = {
    "project-status": "/v3.0/project/status/update/",
    "promotion-status": "/v3.0/promotion/status/update/",
    "budget": "/v3.0/promotion/budget/update/",
    "bid": "/v3.0/promotion/bid/update/",
    "roi": "/v3.0/project/roigoal/update/",
}
QIANCHUAN_ENDPOINTS = {
    "status": "/v1.0/qianchuan/uni_promotion/ad/status/update/",
    "budget": "/v1.0/qianchuan/uni_promotion/ad/budget/update/",
    "roi": "/v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/",
}
MARKETING_STATUSES = ("ENABLE", "DISABLE")
QIANCHUAN_STATUSES = ("ENABLE", "DISABLE", "DELETE")
DEEP_EXTERNAL_ACTIONS = (
    "AD_CONVERT_TYPE_LIVE_PAY_ROI",
    "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
)
MAX_BATCH_SIZE = 10


def positive_ids(values, field, maximum=MAX_BATCH_SIZE):
    result = []
    seen = set()
    for index, value in enumerate(values or []):
        text = str(value or "").strip()
        if not text.isdigit() or int(text) <= 0:
            raise ConfigurationError(f"{field}[{index}] must be a positive integer")
        if text not in seen:
            seen.add(text)
            result.append(text)
    if not result:
        raise ConfigurationError(f"at least one {field} is required")
    if len(result) > maximum:
        raise ConfigurationError(
            f"this command accepts at most {maximum} {field} values per request",
            {"count": len(result)},
        )
    return result


def positive_decimal(value, field, *, minimum=None, maximum=None):
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as error:
        raise ConfigurationError(f"{field} must be a number") from error
    if not parsed.is_finite() or parsed <= 0:
        raise ConfigurationError(f"{field} must be greater than zero")
    if parsed.as_tuple().exponent < -2:
        raise ConfigurationError(f"{field} supports at most two decimal places")
    if minimum is not None and parsed < Decimal(str(minimum)):
        raise ConfigurationError(f"{field} must be at least {minimum}")
    if maximum is not None and parsed > Decimal(str(maximum)):
        raise ConfigurationError(f"{field} must not exceed {maximum}")
    return float(parsed)


def marketing_payload(operation, advertiser_id, ids, *, status=None, value=None):
    advertiser_id = int(positive_ids([advertiser_id], "advertiser_id", maximum=1)[0])
    if operation == "project-status":
        normalized = positive_ids(ids, "project_id")
        return {
            "advertiser_id": advertiser_id,
            "data": [{"project_id": int(item), "opt_status": status} for item in normalized],
        }
    if operation == "promotion-status":
        normalized = positive_ids(ids, "promotion_id")
        return {
            "advertiser_id": advertiser_id,
            "data": [{"promotion_id": int(item), "opt_status": status} for item in normalized],
        }
    if operation == "budget":
        normalized = positive_ids(ids, "promotion_id")
        amount = positive_decimal(value, "budget")
        return {
            "advertiser_id": advertiser_id,
            "data": [{"promotion_id": int(item), "budget": amount} for item in normalized],
        }
    if operation == "bid":
        normalized = positive_ids(ids, "promotion_id")
        amount = positive_decimal(value, "bid", minimum="0.01", maximum="10000")
        return {
            "advertiser_id": advertiser_id,
            "data": [{"promotion_id": int(item), "bid": amount} for item in normalized],
        }
    normalized = positive_ids(ids, "project_id")
    amount = positive_decimal(value, "roi_goal")
    return {
        "advertiser_id": advertiser_id,
        "data": [{"project_id": int(item), "roi_goal": amount} for item in normalized],
    }


def qianchuan_payload(
    operation,
    advertiser_id,
    ids,
    *,
    status=None,
    value=None,
    deep_external_action=None,
):
    advertiser_id = int(positive_ids([advertiser_id], "advertiser_id", maximum=1)[0])
    normalized = positive_ids(ids, "ad_id")
    if operation == "status":
        return {
            "advertiser_id": advertiser_id,
            "ad_ids": [int(item) for item in normalized],
            "opt_status": status,
        }
    if operation == "budget":
        amount = positive_decimal(value, "budget")
        return {
            "advertiser_id": advertiser_id,
            "update_budget_infos": [
                {"ad_id": int(item), "budget": amount} for item in normalized
            ],
        }
    amount = positive_decimal(value, "roi2_goal")
    rows = [{"ad_id": int(item), "roi2_goal": amount} for item in normalized]
    if deep_external_action:
        for row in rows:
            row["deep_external_action"] = deep_external_action
    return {"advertiser_id": advertiser_id, "update_roi2_infos": rows}


def response_failed(response):
    data = response.get("data") if isinstance(response, dict) else None
    errors = []
    if isinstance(data, dict):
        for field in ("errors", "error_list"):
            if isinstance(data.get(field), list):
                errors.extend(data[field])
        for row in data.get("results") or []:
            if not isinstance(row, dict):
                errors.append({"message": "invalid result row"})
                continue
            if row.get("status") not in {None, "SUCCESS"}:
                errors.append(row)
            elif "flag" in row and row.get("flag") is not True:
                errors.append(row)
            elif row.get("error"):
                errors.append(row)
    return response.get("code") != 0 or bool(errors), errors


def mutation_lock_path(channel, advertiser_id):
    if channel == "qianchuan":
        return qianchuan_advertiser_lock_path(
            authorization_store.state_root(),
            advertiser_id,
        )
    return (
        authorization_store.state_root()
        / "locks"
        / f"{channel}-plan-settings-{advertiser_id}.lock"
    )


def main(argv=None):
    parser = argparse.ArgumentParser(description="Preview or submit official plan-setting updates.")
    parser.add_argument("channel", choices=("marketing", "qianchuan"))
    parser.add_argument("operation")
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--project-id", action="append")
    parser.add_argument("--promotion-id", action="append")
    parser.add_argument("--ad-id", action="append")
    parser.add_argument("--status")
    parser.add_argument("--value")
    parser.add_argument("--deep-external-action", choices=DEEP_EXTERNAL_ACTIONS)
    parser.add_argument("--confirm-delete", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)

    if args.channel == "marketing":
        if args.operation not in MARKETING_ENDPOINTS:
            raise ConfigurationError("unsupported Marketing plan update operation")
        if args.ad_id or args.deep_external_action or args.confirm_delete:
            raise ConfigurationError("Qianchuan-only options cannot be used for Marketing")
        if args.operation.endswith("status"):
            if args.status not in MARKETING_STATUSES:
                raise ConfigurationError("Marketing status must be ENABLE or DISABLE")
            if args.value is not None:
                raise ConfigurationError("status update does not accept --value")
        elif args.status is not None:
            raise ConfigurationError("--status is only valid for status updates")
        uses_project_ids = args.operation in {"project-status", "roi"}
        if uses_project_ids and args.promotion_id:
            raise ConfigurationError("this operation accepts only --project-id")
        if not uses_project_ids and args.project_id:
            raise ConfigurationError("this operation accepts only --promotion-id")
        id_values = (
            args.project_id
            if uses_project_ids
            else args.promotion_id
        )
        if args.operation in {"budget", "bid", "roi"} and args.value is None:
            raise ConfigurationError("--value is required for this update")
        payload = marketing_payload(
            args.operation,
            args.advertiser_id,
            id_values,
            status=args.status,
            value=args.value,
        )
        endpoint = MARKETING_ENDPOINTS[args.operation]
        capability = "create"
    else:
        if args.operation not in QIANCHUAN_ENDPOINTS:
            raise ConfigurationError("unsupported Qianchuan plan update operation")
        if args.project_id or args.promotion_id:
            raise ConfigurationError("Marketing-only IDs cannot be used for Qianchuan")
        if args.operation == "status":
            if args.status not in QIANCHUAN_STATUSES:
                raise ConfigurationError(
                    "Qianchuan status must be ENABLE, DISABLE, or DELETE"
                )
            if args.value is not None or args.deep_external_action:
                raise ConfigurationError("status update does not accept value or ROI action")
            if args.submit and args.status == "DELETE" and not args.confirm_delete:
                raise ConfigurationError("DELETE submission requires --confirm-delete")
            if args.status != "DELETE" and args.confirm_delete:
                raise ConfigurationError("--confirm-delete is valid only with DELETE")
        else:
            if args.status is not None or args.confirm_delete:
                raise ConfigurationError("status options are only valid for status updates")
            if args.value is None:
                raise ConfigurationError("--value is required for this update")
            if args.operation != "roi" and args.deep_external_action:
                raise ConfigurationError("--deep-external-action is only valid for ROI updates")
        payload = qianchuan_payload(
            args.operation,
            args.advertiser_id,
            args.ad_id,
            status=args.status,
            value=args.value,
            deep_external_action=args.deep_external_action,
        )
        endpoint = QIANCHUAN_ENDPOINTS[args.operation]
        capability = "qianchuan_create"

    result = {
        "ok": True,
        "mode": "submit" if args.submit else "dry_run",
        "channel": args.channel,
        "operation": args.operation,
        "endpoint": endpoint,
        "payload": payload,
        "submitted": False,
    }
    if args.submit:
        config_path = config_paths.resolve_config_path(args.config)
        raw_config = json.loads(config_path.read_text(encoding="utf-8"))
        runtime = channels.runtime_config(
            raw_config,
            channel=args.channel,
            capability=capability,
        )
        runtime = token_manager.ensure_access_token(
            config_path,
            runtime,
            channel=args.channel,
            advertiser_id=args.advertiser_id,
            auth_account_id=args.auth_account_id,
        )
        if args.channel == "qianchuan":
            client = QianchuanClientFactory(
                authorization_store.state_root(),
                args.advertiser_id,
            ).client(
                get_path(runtime, "api.base_url"),
                get_path(runtime, "api.access_token"),
            )
        else:
            client = OceanEngineClient(
                get_path(runtime, "api.base_url"),
                get_path(runtime, "api.access_token"),
            )
        with ProcessLock(mutation_lock_path(args.channel, args.advertiser_id)):
            response = client.post(endpoint, payload)
        failed, errors = response_failed(response)
        result.update({
            "ok": not failed,
            "submitted": True,
            "response": response,
            "partial_errors": errors,
        })
        if failed:
            write_json(result, destination=args.out)
            return 1
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
