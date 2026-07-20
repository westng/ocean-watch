#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from contextlib import nullcontext

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path
from ocean_watch.core.output import write_json
from ocean_watch.core.process_lock import ProcessLock
from ocean_watch.integrations import qianchuan_work_metadata
from ocean_watch.materials import qianchuan_work_owner_cache
from ocean_watch.materials.douyin_work_links import (
    MAX_CONCURRENCY,
    DouyinWorkLinkResolver,
    DouyinWorkMetadataResolver,
    resolve_work_links,
)
from ocean_watch.materials.qianchuan_work_materials import resolve_work_materials
from ocean_watch.plans import create_qianchuan_plan
from ocean_watch.plans.qianchuan_executor import (
    QianchuanPlanExecutionRequest,
    QianchuanPlanExecutor,
)
from ocean_watch.plans.qianchuan_plan_gateway import (
    QianchuanPlanGateway,
    existing_aweme_item_ids,
)
from ocean_watch.templates import qianchuan_product_templates

MAX_MATERIALS_PER_WRITE = 100
VIDEO_IMAGE_MODES = {"VIDEO_LARGE", "VIDEO_VERTICAL"}
DEFAULT_QIANCHUAN_CONCURRENCY = 8
BATCH_PRESENTATION_COLUMNS = (
    ("ad_id", "计划ID"),
    ("creator_name", "达人昵称"),
    ("product_id", "商品ID"),
    ("material_id", "素材ID"),
    ("material_title", "素材标题"),
)


def build_parser():
    parser = argparse.ArgumentParser(
        description="Create or append Qianchuan product plans from Douyin work links."
    )
    parser.add_argument("--config")
    parser.add_argument("--plan-template", required=True)
    parser.add_argument("--work-url", action="append", default=[])
    parser.add_argument(
        "--concurrency",
        type=int,
        default=DEFAULT_QIANCHUAN_CONCURRENCY,
    )
    parser.add_argument("--auth-account-id")
    parser.add_argument(
        "--no-link-metadata-api",
        action="store_true",
        help="Disable the configured public Douyin work metadata hint service.",
    )
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--include-payloads", action="store_true")
    parser.add_argument("--out")
    return parser


def validate_concurrency(value):
    value = int(value)
    if value < 1 or value > MAX_CONCURRENCY:
        raise ValueError(f"concurrency must be between 1 and {MAX_CONCURRENCY}")
    return value


def filter_link_product_hints(link_result, allowed_product_ids):
    allowed = {str(value) for value in allowed_product_ids}
    resolved = []
    skipped = list(link_result["skipped"])
    for row in link_result["resolved"]:
        product_hint = row.get("product_hint") or {}
        hinted_product_id = str(product_hint.get("product_id") or "")
        if hinted_product_id and hinted_product_id not in allowed:
            skipped.append({
                **row,
                "status": "skipped",
                "reason": "link_metadata_product_mismatch",
                "message": "作品绑定商品与投放模板商品不匹配",
                "hinted_product_id": hinted_product_id,
                "template_product_ids": sorted(allowed),
            })
            continue
        resolved.append(row)
    return {"resolved": resolved, "skipped": skipped}


def weighted_truncate(value, maximum):
    result = []
    length = 0
    for character in str(value):
        width = 1 if ord(character) < 128 else 2
        if length + width > maximum:
            break
        result.append(character)
        length += width
    return "".join(result)


def build_plan_name(template, creator, now=None):
    now = now or dt.datetime.now()
    bindings = template["bindings"]
    creator_label = (
        creator.get("aweme_name")
        or creator.get("aweme_show_id")
        or creator["aweme_id"]
    )
    suffix = now.strftime("%Y%m%d%H%M%S")
    prefix = f"{bindings['product_name']}-{creator_label}"
    return f"{weighted_truncate(prefix, 84)}-{suffix}"


def group_by_creator(material_rows):
    groups = {}
    for row in material_rows:
        groups.setdefault(row["aweme_id"], []).append(row)
    return groups


def filter_supported_materials(rows):
    eligible = []
    skipped = []
    for row in rows:
        image_mode = get_path(row, "material.image_mode")
        if image_mode not in VIDEO_IMAGE_MODES:
            skipped.append({
                **row,
                "status": "skipped",
                "reason": "unsupported_image_mode",
                "message": f"作品素材类型 {image_mode!r} 不支持投放",
            })
        else:
            eligible.append(row)
    return eligible, skipped


def chunk_rows(rows, size=MAX_MATERIALS_PER_WRITE):
    return [rows[index : index + size] for index in range(0, len(rows), size)]


def product_creatives(rows, allowed_product_ids, *, for_create):
    allowed = {str(value) for value in allowed_product_ids}
    videos_by_product = {}
    for row in rows:
        for product_id in row["matched_product_ids"]:
            product_id = str(product_id)
            if product_id not in allowed:
                continue
            videos_by_product.setdefault(product_id, []).append({
                "image_mode": row["material"]["image_mode"],
                "aweme_item_id": int(row["aweme_item_id"]),
            })
    creatives = []
    for product_id in allowed_product_ids:
        videos = videos_by_product.get(str(product_id)) or []
        if not videos:
            continue
        creative = {
            "product_id": int(product_id),
            "video_material": videos,
        }
        if for_create:
            creative["creative_type"] = "PROGRAMMATIC_CREATIVE"
        creatives.append(creative)
    return creatives


def compact_response(response):
    if not isinstance(response, dict):
        return None
    return {
        key: value
        for key, value in {
            "code": response.get("code"),
            "message": response.get("message"),
            "request_id": response.get("request_id"),
        }.items()
        if value is not None
    }


def base_group_result(aweme_id, rows, existing_plan=None):
    creator = rows[0]["creator"]
    return {
        "aweme_id": aweme_id,
        "douyin_id": creator.get("aweme_show_id"),
        "creator_name": creator.get("aweme_name"),
        "ad_id": existing_plan.get("ad_id") if existing_plan else None,
        "plan_name": existing_plan.get("name") if existing_plan else None,
        "plan_status": existing_plan.get("status") if existing_plan else None,
        "product_ids": list(existing_plan.get("product_ids") or []) if existing_plan else [],
        "input_item_ids": [row["aweme_item_id"] for row in rows],
    }


def execute_add_batches(
    gateway,
    advertiser_id,
    ad_id,
    rows,
    product_ids,
    *,
    submit,
    include_payloads,
):
    batches = []
    failed = False
    for row_chunk in chunk_rows(rows):
        creatives = product_creatives(row_chunk, product_ids, for_create=False)
        if not creatives:
            continue
        payload = {
            "advertiser_id": int(advertiser_id),
            "ad_id": int(ad_id),
            "multi_product_creative_list": creatives,
        }
        batch = {
            "item_ids": [row["aweme_item_id"] for row in row_chunk],
            "mode": "submit" if submit else "dry_run",
        }
        if include_payloads:
            batch["payload"] = payload
        if submit:
            try:
                _, response = gateway.add_materials(
                    advertiser_id,
                    ad_id,
                    creatives,
                )
                batch["response"] = compact_response(response)
            except Exception as error:
                batch.update({
                    "status": "failed",
                    "reason": getattr(error, "code", "material_add_failed"),
                    "message": str(error),
                    "details": getattr(error, "details", {}),
                })
                failed = True
                batches.append(batch)
                continue
            if response.get("code") != 0:
                batch["status"] = "failed"
                failed = True
            else:
                batch["status"] = "appended"
        else:
            batch["status"] = "would_append"
        batches.append(batch)
    return batches, failed


def execute_existing_plan_group(
    gateway,
    advertiser_id,
    rows,
    plan,
    *,
    submit,
    include_payloads,
):
    result = base_group_result(rows[0]["aweme_id"], rows, plan)
    plan_products = set(plan["product_ids"])
    eligible = [
        row for row in rows
        if plan_products.intersection(row["matched_product_ids"])
    ]
    result["skipped_item_ids"] = [
        row["aweme_item_id"] for row in rows if row not in eligible
    ]
    if not eligible:
        return {
            **result,
            "status": "skipped",
            "reason": "existing_plan_product_mismatch",
        }

    material_result = gateway.list_plan_video_materials(
        advertiser_id,
        plan["ad_id"],
    )
    if material_result["truncated"]:
        return {
            **result,
            "status": "failed",
            "reason": "plan_material_query_truncated",
        }
    existing_ids = existing_aweme_item_ids(material_result["materials"])
    new_rows = [row for row in eligible if row["aweme_item_id"] not in existing_ids]
    result["already_present_item_ids"] = [
        row["aweme_item_id"] for row in eligible if row["aweme_item_id"] in existing_ids
    ]
    if not new_rows:
        return {**result, "status": "already_present"}

    batches, failed = execute_add_batches(
        gateway,
        advertiser_id,
        plan["ad_id"],
        new_rows,
        plan["product_ids"],
        submit=submit,
        include_payloads=include_payloads,
    )
    return {
        **result,
        "status": "append_failed" if failed else ("appended" if submit else "would_append"),
        "appended_item_ids": [row["aweme_item_id"] for row in new_rows],
        "batches": batches,
    }


def execute_new_plan_group(
    gateway,
    plan_executor,
    template,
    rows,
    *,
    submit,
    include_payloads,
    now=None,
):
    advertiser_id = template["bindings"]["advertiser_id"]
    aweme_id = rows[0]["aweme_id"]
    result = base_group_result(aweme_id, rows)
    result["product_ids"] = list(template["bindings"]["product_ids"])
    first_chunk, remaining = rows[:MAX_MATERIALS_PER_WRITE], rows[MAX_MATERIALS_PER_WRITE:]
    payload = qianchuan_product_templates.payload_from_template(
        template,
        name=build_plan_name(template, rows[0]["creator"], now=now),
    )
    payload["aweme_id"] = int(aweme_id)
    payload["multi_product_creative_list"] = product_creatives(
        first_chunk,
        template["bindings"]["product_ids"],
        for_create=True,
    )
    payload, blocking_fields = create_qianchuan_plan.normalize_and_validate(payload)
    if blocking_fields:
        return {
            **result,
            "status": "blocked",
            "blocking_fields": list(blocking_fields),
            **({"create_payload": payload} if include_payloads else {}),
        }

    execution = plan_executor.execute(QianchuanPlanExecutionRequest(
        payload=payload,
        submit=submit,
    ))
    if include_payloads:
        result["create_payload"] = payload
    if not submit:
        result.update({
            "status": "would_create",
            "plan_name": payload["name"],
            "created_item_ids": [row["aweme_item_id"] for row in first_chunk],
        })
        if remaining:
            result["remaining_item_ids"] = [row["aweme_item_id"] for row in remaining]
        return result
    if execution.get("submit_failed") or not execution.get("ad_id"):
        return {
            **result,
            "status": "create_failed",
            "plan_name": payload["name"],
            "response": compact_response(execution.get("response")),
        }

    ad_id = execution["ad_id"]
    result.update({
        "ad_id": ad_id,
        "plan_name": payload["name"],
        "created_item_ids": [row["aweme_item_id"] for row in first_chunk],
        "create_response": compact_response(execution.get("response")),
    })
    batches = []
    failed = False
    if remaining:
        batches, failed = execute_add_batches(
            gateway,
            advertiser_id,
            ad_id,
            remaining,
            template["bindings"]["product_ids"],
            submit=True,
            include_payloads=include_payloads,
        )
    result["status"] = "created_partial" if failed else "created"
    if batches:
        result["append_batches"] = batches
    return result


def execute_plan_actions(
    template,
    matched_rows,
    gateway,
    plan_executor,
    *,
    concurrency,
    submit,
    include_payloads=False,
    now=None,
):
    advertiser_id = template["bindings"]["advertiser_id"]
    eligible, unsupported = filter_supported_materials(matched_rows)
    groups = group_by_creator(eligible)
    if not groups:
        return [], unsupported
    discovery = gateway.find_creator_plans(
        advertiser_id,
        groups,
        aweme_show_ids={
            aweme_id: str(rows[0]["creator"].get("aweme_show_id") or "")
            for aweme_id, rows in groups.items()
        },
    )

    def execute(aweme_id, rows):
        plans = discovery["matches"].get(aweme_id) or []
        if len(plans) > 1:
            return {
                **base_group_result(aweme_id, rows),
                "status": "failed",
                "reason": "multiple_existing_plans",
                "candidate_ad_ids": [plan["ad_id"] for plan in plans],
            }
        if plans:
            return execute_existing_plan_group(
                gateway,
                advertiser_id,
                rows,
                plans[0],
                submit=submit,
                include_payloads=include_payloads,
            )
        return execute_new_plan_group(
            gateway,
            plan_executor,
            template,
            rows,
            submit=submit,
            include_payloads=include_payloads,
            now=now,
        )

    results = {}
    with ThreadPoolExecutor(max_workers=min(concurrency, len(groups))) as pool:
        futures = {
            pool.submit(execute, aweme_id, rows): aweme_id
            for aweme_id, rows in groups.items()
        }
        for future in as_completed(futures):
            aweme_id = futures[future]
            try:
                results[aweme_id] = future.result()
            except Exception as error:
                results[aweme_id] = {
                    **base_group_result(aweme_id, groups[aweme_id]),
                    "status": "failed",
                    "reason": getattr(error, "code", "creator_transaction_failed"),
                    "message": str(error),
                    "details": getattr(error, "details", {}),
                }
    return [results[aweme_id] for aweme_id in groups], unsupported


def presentation_value(value):
    if value is None or value == "":
        return "—"
    return str(value).replace("|", "\\|").replace("\r", " ").replace("\n", " ")


def completed_item_ids(group_result):
    item_ids = []

    def extend(values):
        for value in values or []:
            value = str(value)
            if value not in item_ids:
                item_ids.append(value)

    extend(group_result.get("already_present_item_ids"))
    if group_result.get("status") in {"created", "created_partial", "would_create"}:
        extend(group_result.get("created_item_ids"))
    if group_result.get("status") == "would_create":
        extend(group_result.get("remaining_item_ids"))
    for batch in [
        *(group_result.get("batches") or []),
        *(group_result.get("append_batches") or []),
    ]:
        if batch.get("status") in {"appended", "would_append"}:
            extend(batch.get("item_ids"))

    completed = set(item_ids)
    input_order = [str(value) for value in group_result.get("input_item_ids") or []]
    return [value for value in input_order if value in completed] + [
        value for value in item_ids if value not in input_order
    ]


def presentation_rows(template, material_result, group_results):
    materials_by_item_id = {
        str(row["aweme_item_id"]): row
        for row in material_result.get("matched") or []
        if row.get("aweme_item_id") is not None
    }
    default_product_ids = [str(value) for value in template["bindings"]["product_ids"]]
    rows = []
    for group_result in group_results:
        allowed_product_ids = {
            str(value) for value in group_result.get("product_ids") or default_product_ids
        }
        for item_id in completed_item_ids(group_result):
            material_row = materials_by_item_id.get(item_id)
            if material_row is None:
                continue
            material = material_row.get("material") or {}
            official_material_id = material.get("material_id")
            material_id = (
                str(official_material_id)
                if official_material_id not in {None, ""}
                else str(material.get("aweme_item_id") or item_id)
            )
            material_id_source = (
                "material_id" if official_material_id not in {None, ""} else "aweme_item_id"
            )
            matched_product_ids = {
                str(value) for value in material_row.get("matched_product_ids") or []
            }
            for product_id in default_product_ids:
                if product_id not in allowed_product_ids or product_id not in matched_product_ids:
                    continue
                rows.append(
                    {
                        "ad_id": str(group_result["ad_id"])
                        if group_result.get("ad_id") not in {None, ""}
                        else None,
                        "creator_name": group_result.get("creator_name")
                        or get_path(material_row, "creator.aweme_name"),
                        "product_id": product_id,
                        "material_id": material_id,
                        "material_title": material.get("title"),
                        "material_id_source": material_id_source,
                        "aweme_item_id": item_id,
                    }
                )
    return rows


def render_presentation_table(rows):
    labels = [label for _, label in BATCH_PRESENTATION_COLUMNS]
    table = [
        "| " + " | ".join(labels) + " |",
        "| " + " | ".join("---" for _ in labels) + " |",
    ]
    table.extend(
        "| "
        + " | ".join(presentation_value(row.get(field)) for field, _ in BATCH_PRESENTATION_COLUMNS)
        + " |"
        for row in rows
    )
    return "\n".join(table)


def presentation_contract(template, material_result, group_results):
    rows = presentation_rows(template, material_result, group_results)
    return {
        "format": "markdown_table",
        "required": True,
        "allow_column_omission": False,
        "allow_column_reordering": False,
        "columns": [
            {"field": field, "label": label} for field, label in BATCH_PRESENTATION_COLUMNS
        ],
        "rows": rows,
        "details_outside_table": ["skipped", "query_failures", "failed_results"],
        "required_details": [
            {"field": "skipped", "label": "跳过详情"},
            {"field": "query_failures", "label": "查询失败"},
            {"field": "failed_results", "label": "执行失败"},
        ],
        "rendered_markdown": render_presentation_table(rows),
    }


def summarize(mode, template, link_result, material_result, group_results, unsupported):
    skipped = [
        *link_result["skipped"],
        *material_result["skipped"],
        *unsupported,
    ]
    statuses = {}
    for row in group_results:
        status = row["status"]
        statuses[status] = statuses.get(status, 0) + 1
    counts = {
        "input_links": len(link_result["resolved"]) + len(link_result["skipped"]),
        "resolved_links": len(link_result["resolved"]),
        "matched_links": len(material_result["matched"]),
        "skipped_links": len(skipped),
        "creator_groups": len(group_results),
        **statuses,
    }
    failed_results = [
        row
        for row in group_results
        if row.get("status")
        in {
            "failed",
            "blocked",
            "create_failed",
            "append_failed",
            "created_partial",
        }
    ]
    return {
        "mode": mode,
        "channel": "qianchuan",
        "template": {
            "template_id": template["template_id"],
            "name": template["display_name"],
            "advertiser_id": template["bindings"]["advertiser_id"],
            "product_ids": template["bindings"]["product_ids"],
        },
        "counts": counts,
        "results": group_results,
        "skipped": skipped,
        "query_failures": material_result["query_failures"],
        "failed_results": failed_results,
        "presentation": presentation_contract(template, material_result, group_results),
    }


def execute(args, *, link_resolver=None, clients=None, now=None):
    started_at = time.monotonic()
    concurrency = validate_concurrency(args.concurrency)
    if not args.work_url:
        raise ValueError("at least one --work-url is required")
    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    template = qianchuan_product_templates.resolve_template(
        raw_config,
        args.plan_template,
    )
    metadata_disabled = getattr(args, "no_link_metadata_api", False)
    metadata_configured = qianchuan_work_metadata.is_configured(raw_config)
    metadata_endpoint = (
        None
        if metadata_disabled
        else qianchuan_work_metadata.endpoint_from_config(raw_config)
    )
    metadata_enabled = metadata_endpoint is not None
    if link_resolver is None and metadata_enabled:
        link_resolver = DouyinWorkLinkResolver(
            metadata_resolver=DouyinWorkMetadataResolver(
                metadata_endpoint,
            )
        )
    link_result = resolve_work_links(
        args.work_url,
        resolver=link_resolver,
        concurrency=concurrency,
    )
    link_result = filter_link_product_hints(
        link_result,
        template["bindings"]["product_ids"],
    )
    links_finished_at = time.monotonic()
    if not link_result["resolved"]:
        empty_material_result = {"matched": [], "skipped": [], "query_failures": []}
        result = summarize(
            "submit" if args.submit else "dry_run",
            template,
            link_result,
            empty_material_result,
            [],
            [],
        )
        result["performance"] = {
            "link_resolution_seconds": round(links_finished_at - started_at, 3),
            "credential_resolution_seconds": 0.0,
            "material_resolution_seconds": 0.0,
            "plan_reconciliation_seconds": 0.0,
            "total_seconds": round(time.monotonic() - started_at, 3),
            "link_metadata": {
                "configured": metadata_configured,
                "enabled": metadata_enabled,
            },
        }
        return result, 0

    advertiser_id = template["bindings"]["advertiser_id"]
    if clients is None:
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
        api_client = OceanEngineClient(
            get_path(runtime, "api.base_url"),
            get_path(runtime, "api.access_token"),
        )
        video_client = OceanEngineClient(
            get_path(runtime, "api.legacy_base_url")
            or get_path(runtime, "oauth.token_base_url"),
            get_path(runtime, "api.access_token"),
        )
    else:
        api_client, video_client = clients

    cached_owner_hints = qianchuan_work_owner_cache.load_owner_hints(
        advertiser_id,
        [row["aweme_item_id"] for row in link_result["resolved"]],
    )
    link_owner_hints = {
        row["aweme_item_id"]: row["owner_hint"]
        for row in link_result["resolved"]
        if row.get("owner_hint")
    }
    owner_hints = {**cached_owner_hints, **link_owner_hints}
    material_started_at = time.monotonic()
    material_result = resolve_work_materials(
        api_client,
        video_client,
        advertiser_id,
        template["bindings"]["product_ids"],
        link_result["resolved"],
        concurrency=concurrency,
        owner_hints=owner_hints,
    )
    cache_write_count = 0
    cache_warning = None
    try:
        cache_write_count = qianchuan_work_owner_cache.update_owner_hints(
            advertiser_id,
            material_result.get("resolved_owner_hints"),
        )
    except (OSError, TimeoutError) as error:
        cache_warning = {
            "code": "owner_hint_cache_write_failed",
            "message": str(error)[:256],
        }
    material_finished_at = time.monotonic()
    gateway = QianchuanPlanGateway(api_client)
    plan_executor = QianchuanPlanExecutor(api_client)
    lock = (
        ProcessLock(
            authorization_store.state_root()
            / "locks"
            / f"qianchuan-work-plans-{advertiser_id}.lock"
        )
        if args.submit
        else nullcontext()
    )
    with lock:
        group_results, unsupported = execute_plan_actions(
            template,
            material_result["matched"],
            gateway,
            plan_executor,
            concurrency=concurrency,
            submit=args.submit,
            include_payloads=args.include_payloads,
            now=now,
        )
    plans_finished_at = time.monotonic()
    output = summarize(
        "submit" if args.submit else "dry_run",
        template,
        link_result,
        material_result,
        group_results,
        unsupported,
    )
    output["performance"] = {
        "link_resolution_seconds": round(links_finished_at - started_at, 3),
        "credential_resolution_seconds": round(
            material_started_at - links_finished_at,
            3,
        ),
        "material_resolution_seconds": round(
            material_finished_at - material_started_at,
            3,
        ),
        "plan_reconciliation_seconds": round(
            plans_finished_at - material_finished_at,
            3,
        ),
        "total_seconds": round(plans_finished_at - started_at, 3),
        "owner_hint_cache": {
            **(material_result.get("owner_hint_summary") or {}),
            "loaded": len(owner_hints),
            "loaded_from_cache": len(cached_owner_hints),
            "loaded_from_link_metadata": len(link_owner_hints),
            "stored": cache_write_count,
            "warning": cache_warning,
        },
        "link_metadata": {
            "configured": metadata_configured,
            "enabled": metadata_enabled,
        },
    }
    failed = bool(material_result["query_failures"]) or any(
        row["status"] in {
            "failed",
            "blocked",
            "create_failed",
            "append_failed",
            "created_partial",
        }
        for row in group_results
    )
    return output, 1 if failed else 0


def main(argv=None):
    args = build_parser().parse_args(argv)
    result, exit_code = execute(args)
    write_json(result, destination=args.out)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
