#!/usr/bin/env python3
import argparse
import json

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import QianchuanClientFactory, qianchuan_advertiser_lock_path
from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.process_lock import ProcessLock
from ocean_watch.materials.douyin_work_links import (
    DEFAULT_CONCURRENCY,
    MAX_CONCURRENCY,
    resolve_work_links,
)
from ocean_watch.plans.qianchuan_plan_gateway import (
    QianchuanPlanGateway,
    decimal_id,
)

MAX_MATERIALS_PER_DELETE = 100
REMOVE_REQUEST_LIMIT = 512
CUSTOM_MATERIAL = "CUSTOM"
DELETED_STATUS = "DELETED"
MULTI_BINDING_DELETE_NOTICE = (
    "官方接口在多号或多商品场景下可能同时删除同一素材的关联投放"
)


def build_parser():
    parser = argparse.ArgumentParser(
        description="Remove Qianchuan plan materials by Douyin work link."
    )
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id", required=True)
    parser.add_argument("--ad-id", required=True)
    parser.add_argument("--work-url", action="append", default=[])
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    parser.add_argument("--auth-account-id")
    parser.add_argument("--confirm-delete", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    return parser


def chunks(values, size=MAX_MATERIALS_PER_DELETE):
    values = list(values)
    return [values[index : index + size] for index in range(0, len(values), size)]


def material_reference(row):
    video = get_path(row, "material_info.video_material", {}) or {}
    item_id = video.get("aweme_item_id")
    material_id = video.get("material_id")
    return {
        "aweme_item_id": (
            str(item_id) if item_id is not None and str(item_id) != "0" else None
        ),
        "material_id": (
            str(material_id)
            if material_id is not None and str(material_id) != "0"
            else None
        ),
        "material_select_type": row.get("material_select_type"),
        "material_status": row.get("material_status"),
    }


def index_materials(material_rows, key):
    indexed = {}
    for row in material_rows:
        reference = material_reference(row)
        value = reference.get(key)
        if value:
            indexed.setdefault(value, []).append(reference)
    return indexed


def base_work_result(work):
    return {
        "input_index": work["input_index"],
        "input_url": work.get("input_url"),
        "canonical_url": work.get("canonical_url"),
        "aweme_item_id": work["aweme_item_id"],
    }


def reconcile_work_materials(work_rows, material_rows):
    by_item = index_materials(material_rows, "aweme_item_id")
    results = []
    candidates = []
    for work in work_rows:
        result = base_work_result(work)
        matches = by_item.get(work["aweme_item_id"]) or []
        if not matches:
            results.append({
                **result,
                "status": "skipped",
                "reason": "work_not_in_plan",
                "message": "作品不在目标计划的视频素材中",
            })
            continue

        if any(match.get("material_id") is None for match in matches):
            results.append({
                **result,
                "status": "failed",
                "reason": "missing_material_id",
                "message": "计划素材没有返回可删除的 material_id",
            })
            continue

        material_ids = list(dict.fromkeys(match["material_id"] for match in matches))
        if len(material_ids) != 1:
            results.append({
                **result,
                "status": "failed",
                "reason": "ambiguous_material_match",
                "message": "同一作品匹配到多个不同的计划素材 ID",
                "candidate_material_ids": material_ids,
            })
            continue

        material_id = material_ids[0]
        select_types = sorted({
            str(match.get("material_select_type") or "UNKNOWN")
            for match in matches
        })
        statuses = sorted({
            str(match.get("material_status") or "UNKNOWN")
            for match in matches
        })
        result.update({
            "material_id": material_id,
            "material_select_types": select_types,
            "material_statuses": statuses,
        })
        if select_types != [CUSTOM_MATERIAL]:
            results.append({
                **result,
                "status": "skipped",
                "reason": "unsupported_material_select_type",
                "message": "官方删除接口仅支持 CUSTOM 自提素材",
            })
            continue
        if statuses == [DELETED_STATUS]:
            results.append({**result, "status": "already_deleted"})
            continue

        result["status"] = "would_delete"
        results.append(result)
        candidates.append(result)
    return results, candidates


def compact_response(response):
    return {
        key: value
        for key, value in {
            "code": response.get("code"),
            "message": response.get("message") or response.get("msg"),
            "request_id": response.get("request_id"),
        }.items()
        if value is not None
    }


def mark_batch_failure(results_by_material, material_ids, response):
    for material_id in material_ids:
        for result in results_by_material.get(material_id, []):
            result.update({
                "status": "failed",
                "reason": "official_delete_failed",
                "response": compact_response(response),
            })


def submit_delete_batches(gateway, advertiser_id, ad_id, candidates):
    results_by_material = {}
    for result in candidates:
        results_by_material.setdefault(result["material_id"], []).append(result)
    material_ids = list(results_by_material)
    batches = []
    submitted_ids = set()
    for batch_ids in chunks(material_ids):
        payload, response = gateway.delete_materials(advertiser_id, ad_id, batch_ids)
        batch = {
            "material_ids": batch_ids,
            "payload": payload,
            "response": compact_response(response),
        }
        if response.get("code") != 0:
            batch["status"] = "failed"
            mark_batch_failure(results_by_material, batch_ids, response)
        else:
            batch["status"] = "submitted"
            submitted_ids.update(batch_ids)
            for material_id in batch_ids:
                for result in results_by_material[material_id]:
                    result["status"] = "delete_submitted"
        batches.append(batch)
    return batches, submitted_ids


def verify_deleted_materials(gateway, advertiser_id, ad_id, candidates, submitted_ids):
    if not submitted_ids:
        return None
    material_result = gateway.list_plan_video_materials(advertiser_id, ad_id)
    if material_result["truncated"]:
        for result in candidates:
            if result["material_id"] in submitted_ids:
                result.update({
                    "status": "failed",
                    "reason": "delete_verification_truncated",
                })
        return material_result

    by_material = index_materials(material_result["materials"], "material_id")
    for result in candidates:
        material_id = result["material_id"]
        if material_id not in submitted_ids:
            continue
        matches = by_material.get(material_id) or []
        statuses = sorted({
            str(match.get("material_status") or "UNKNOWN")
            for match in matches
        })
        result["verified_material_statuses"] = statuses
        if statuses == [DELETED_STATUS]:
            result["status"] = "deleted"
        else:
            result.update({
                "status": "failed",
                "reason": "delete_verification_failed",
                "message": "删除接口成功后未确认到 DELETED 状态",
            })
    return material_result


def summarize(mode, advertiser_id, ad_id, plan, link_result, results, batches):
    status_counts = {}
    for row in results:
        status = row["status"]
        status_counts[status] = status_counts.get(status, 0) + 1
    return {
        "mode": mode,
        "channel": "qianchuan",
        "advertiser_id": advertiser_id,
        "plan": {
            "ad_id": ad_id,
            "name": plan.get("name"),
            "aweme_id": str(plan.get("aweme_id") or "") or None,
            "status": plan.get("status"),
        },
        "counts": {
            "input_links": len(link_result["resolved"]) + len(link_result["skipped"]),
            "resolved_links": len(link_result["resolved"]),
            "invalid_or_duplicate_links": len(link_result["skipped"]),
            **status_counts,
        },
        "risk_notice": MULTI_BINDING_DELETE_NOTICE,
        "results": results,
        "skipped_links": link_result["skipped"],
        "batches": batches,
    }


def execute(args, *, link_resolver=None, client=None, lock_factory=ProcessLock):
    if args.submit and not args.confirm_delete:
        raise ConfigurationError(
            "Qianchuan material deletion requires --submit --confirm-delete"
        )
    if args.confirm_delete and not args.submit:
        raise ConfigurationError("--confirm-delete is valid only with --submit")
    advertiser_id = decimal_id(args.advertiser_id, "advertiser_id")
    ad_id = decimal_id(args.ad_id, "ad_id")
    concurrency = int(args.concurrency)
    if concurrency < 1 or concurrency > MAX_CONCURRENCY:
        raise ValueError(f"concurrency must be between 1 and {MAX_CONCURRENCY}")
    if not args.work_url:
        raise ValueError("at least one --work-url is required")
    link_result = resolve_work_links(
        args.work_url,
        resolver=link_resolver,
        concurrency=concurrency,
    )
    if not link_result["resolved"]:
        output = summarize(
            "submit" if args.submit else "dry_run",
            advertiser_id,
            ad_id,
            {},
            link_result,
            [],
            [],
        )
        return output, 0

    if client is None:
        config_path = config_paths.resolve_config_path(args.config)
        raw_config = json.loads(config_path.read_text(encoding="utf-8"))
        runtime = channels.runtime_config(
            raw_config,
            channel="qianchuan",
            capability="qianchuan_create",
        )
        runtime = token_manager.ensure_access_token(
            config_path,
            runtime,
            channel="qianchuan",
            advertiser_id=advertiser_id,
            auth_account_id=args.auth_account_id,
        )
        client = QianchuanClientFactory(
            authorization_store.state_root(),
            advertiser_id,
            request_limit=REMOVE_REQUEST_LIMIT,
        ).client(
            get_path(runtime, "api.base_url"),
            get_path(runtime, "api.access_token"),
        )

    gateway = QianchuanPlanGateway(client)
    lock = lock_factory(
        qianchuan_advertiser_lock_path(
            authorization_store.state_root(),
            advertiser_id,
        )
    )
    with lock:
        plan = gateway.get_plan_detail(advertiser_id, ad_id)
        material_result = gateway.list_plan_video_materials(advertiser_id, ad_id)
        if material_result["truncated"]:
            raise ApiError(
                "Qianchuan plan material query was truncated",
                {"advertiser_id": advertiser_id, "ad_id": ad_id},
            )
        results, candidates = reconcile_work_materials(
            link_result["resolved"],
            material_result["materials"],
        )
        batches = []
        if args.submit and candidates:
            batches, submitted_ids = submit_delete_batches(
                gateway,
                advertiser_id,
                ad_id,
                candidates,
            )
            verify_deleted_materials(
                gateway,
                advertiser_id,
                ad_id,
                candidates,
                submitted_ids,
            )

    output = summarize(
        "submit" if args.submit else "dry_run",
        advertiser_id,
        ad_id,
        plan,
        link_result,
        results,
        batches,
    )
    failed = any(row["status"] == "failed" for row in results)
    return output, 1 if failed else 0


def main(argv=None):
    args = build_parser().parse_args(argv)
    result, exit_code = execute(args)
    write_json(result, destination=args.out)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
