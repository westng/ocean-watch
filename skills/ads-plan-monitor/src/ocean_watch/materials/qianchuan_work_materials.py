import copy
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

from ocean_watch.core.errors import ApiError
from ocean_watch.materials.qianchuan_creator_accounts import (
    list_authorized_awemes,
    resolve_authorized_aweme,
)
from ocean_watch.materials.query_qianchuan_creator_videos import (
    compact_video,
    fetch_creator_videos,
)

MAX_ITEM_IDS_PER_QUERY = 50
RATE_LIMIT_CODE = "40100"
RATE_LIMIT_RETRY_DELAYS = (1, 2, 4)


def chunks(values, size):
    values = list(values)
    return [values[index : index + size] for index in range(0, len(values), size)]


def work_product_ids(work, template_product_ids):
    hinted_product_id = str(
        ((work.get("product_hint") or {}).get("product_id")) or ""
    ).strip()
    if hinted_product_id and hinted_product_id in template_product_ids:
        return [hinted_product_id]
    return template_product_ids


def creator_is_usable(creator):
    if creator.get("has_authorized") is False:
        return False
    return creator.get("is_product_uni_prom_disabled") is not True


def compact_query_error(error, *, aweme_id, product_id=None):
    details = getattr(error, "details", {}) or {}
    return {
        "aweme_id": str(aweme_id),
        "product_id": str(product_id) if product_id is not None else None,
        "code": details.get("code") or getattr(error, "code", None),
        "message": details.get("message") or str(error),
        "request_id": details.get("request_id"),
    }


def is_rate_limit_error(error):
    details = getattr(error, "details", {}) or {}
    return str(details.get("code") or "") == RATE_LIMIT_CODE


def run_video_queries(
    client,
    advertiser_id,
    tasks,
    concurrency,
    *,
    retry_rate_limits=False,
):
    results = []
    failures = []

    def query(task):
        delays = RATE_LIMIT_RETRY_DELAYS if retry_rate_limits else ()
        for attempt in range(len(delays) + 1):
            try:
                response = fetch_creator_videos(
                    client,
                    advertiser_id,
                    task["creator"]["aweme_id"],
                    product_id=task.get("product_id"),
                    aweme_item_ids=task["item_ids"],
                    count=MAX_ITEM_IDS_PER_QUERY,
                )
                return task, response
            except Exception as error:
                if attempt >= len(delays) or not is_rate_limit_error(error):
                    raise
                time.sleep(delays[attempt])

    with ThreadPoolExecutor(max_workers=min(concurrency, max(1, len(tasks)))) as pool:
        futures = {pool.submit(query, task): task for task in tasks}
        for future in as_completed(futures):
            task = futures[future]
            try:
                results.append(future.result())
            except Exception as error:
                failures.append(compact_query_error(
                    error,
                    aweme_id=task["creator"]["aweme_id"],
                    product_id=task.get("product_id"),
                ))
    return results, failures


def normalize_owner_hints(owner_hints, work_by_id):
    result = {}
    for item_id, value in (owner_hints or {}).items():
        item_id = str(item_id)
        if item_id not in work_by_id:
            continue
        if isinstance(value, dict):
            aweme_id = str(value.get("aweme_id") or "")
            aweme_show_id = str(value.get("aweme_show_id") or "").strip()
        else:
            aweme_id = str(value or "")
            aweme_show_id = ""
        if aweme_id.isdigit():
            result[item_id] = {
                "aweme_id": aweme_id,
                "aweme_show_id": aweme_show_id or None,
            }
    return result


def resolve_authorized_hint_creators(client, advertiser_id, hints, concurrency):
    aweme_ids = sorted({
        hint["aweme_id"]
        for hint in hints.values()
    })
    resolved_by_aweme_id = {}
    failures = []

    def query(aweme_id):
        for attempt in range(len(RATE_LIMIT_RETRY_DELAYS) + 1):
            try:
                return aweme_id, resolve_authorized_aweme(
                    client,
                    advertiser_id,
                    aweme_id,
                )
            except Exception as error:
                if (
                    attempt >= len(RATE_LIMIT_RETRY_DELAYS)
                    or not is_rate_limit_error(error)
                ):
                    raise
                time.sleep(RATE_LIMIT_RETRY_DELAYS[attempt])

    with ThreadPoolExecutor(max_workers=min(concurrency, max(1, len(aweme_ids)))) as pool:
        futures = {pool.submit(query, aweme_id): aweme_id for aweme_id in aweme_ids}
        for future in as_completed(futures):
            aweme_id = futures[future]
            try:
                _, creator = future.result()
                resolved_by_aweme_id[aweme_id] = creator
            except Exception:
                failures.append(aweme_id)

    creators_by_item = {}
    for item_id, hint in hints.items():
        creator = resolved_by_aweme_id.get(hint["aweme_id"])
        if (
            creator
            and creator.get("aweme_id") == hint["aweme_id"]
            and creator_is_usable(creator)
        ):
            creators_by_item[item_id] = creator
    return creators_by_item, failures, len(aweme_ids)


def resolve_work_materials(
    authorized_client,
    video_client,
    advertiser_id,
    product_ids,
    work_rows,
    *,
    concurrency=4,
    max_creator_pages=100,
    owner_hints=None,
):
    product_ids = [str(value) for value in product_ids]
    work_by_id = {row["aweme_item_id"]: copy.deepcopy(row) for row in work_rows}
    work_ids = list(work_by_id)
    if not work_ids:
        return {
            "matched": [],
            "skipped": [],
            "creators": [],
            "query_failures": [],
        }

    supplied_hints = normalize_owner_hints(owner_hints, work_by_id)
    hinted_creators, hint_auth_failures, hint_auth_query_count = (
        resolve_authorized_hint_creators(
            authorized_client,
            advertiser_id,
            supplied_hints,
            concurrency,
        )
    )
    eligible_hints = {
        item_id: creator["aweme_id"]
        for item_id, creator in hinted_creators.items()
    }
    creators_by_id = {
        creator["aweme_id"]: creator for creator in hinted_creators.values()
    }
    hinted_items_by_creator = {}
    for item_id, aweme_id in eligible_hints.items():
        hinted_items_by_creator.setdefault(aweme_id, []).append(item_id)
    hinted_tasks = [
        {"creator": creators_by_id[aweme_id], "item_ids": item_chunk}
        for aweme_id, item_ids in hinted_items_by_creator.items()
        for item_chunk in chunks(item_ids, MAX_ITEM_IDS_PER_QUERY)
    ]
    hinted_results, hinted_failures = run_video_queries(
        video_client,
        advertiser_id,
        hinted_tasks,
        concurrency,
        retry_rate_limits=True,
    )
    owners_by_item = {}

    def collect_owners(query_results):
        for task, response in query_results:
            requested = set(task["item_ids"])
            for item in response["videos"]:
                item_id = str(item.get("aweme_item_id") or "")
                if item_id not in requested:
                    continue
                owners_by_item.setdefault(item_id, {})[task["creator"]["aweme_id"]] = {
                    "creator": copy.deepcopy(task["creator"]),
                    "material": compact_video(item),
                }

    collect_owners(hinted_results)
    verified_hint_ids = {
        item_id
        for item_id, aweme_id in eligible_hints.items()
        if aweme_id in (owners_by_item.get(item_id) or {})
    }
    broad_item_ids = [item_id for item_id in work_ids if item_id not in verified_hint_ids]
    creator_result = {"creators": [], "page_count": 0, "truncated": False}
    broad_creators = []
    disabled_creators = []
    if broad_item_ids:
        creator_result = list_authorized_awemes(
            authorized_client,
            advertiser_id,
            max_pages=max_creator_pages,
        )
        if creator_result["truncated"]:
            raise ApiError(
                "Qianchuan authorized creator query was truncated",
                {
                    "advertiser_id": str(advertiser_id),
                    "page_count": creator_result["page_count"],
                },
            )
        broad_creators = [
            creator for creator in creator_result["creators"]
            if creator_is_usable(creator)
        ]
        disabled_creators = [
            creator for creator in creator_result["creators"]
            if not creator_is_usable(creator)
        ]
    creators_by_id.update({
        creator["aweme_id"]: creator for creator in broad_creators
    })
    creators = list(creators_by_id.values())
    broad_tasks = []
    for creator in broad_creators:
        creator_item_ids = [
            item_id
            for item_id in broad_item_ids
            if eligible_hints.get(item_id) != creator["aweme_id"]
        ]
        broad_tasks.extend(
            {"creator": creator, "item_ids": item_chunk}
            for item_chunk in chunks(creator_item_ids, MAX_ITEM_IDS_PER_QUERY)
        )
    broad_results, broad_failures = run_video_queries(
        video_client,
        advertiser_id,
        broad_tasks,
        concurrency,
    )
    collect_owners(broad_results)
    ownership_failures = [*hinted_failures, *broad_failures]

    skipped = []
    resolved_owners = {}
    for item_id, work in work_by_id.items():
        owners = owners_by_item.get(item_id) or {}
        if len(owners) == 1:
            resolved_owners[item_id] = next(iter(owners.values()))
            continue
        reason = "ambiguous_creator" if len(owners) > 1 else (
            "creator_query_incomplete"
            if ownership_failures
            else "not_found_under_authorized_creators"
        )
        skipped.append({
            **work,
            "status": "skipped",
            "reason": reason,
            "message": (
                "作品匹配到多个授权达人"
                if len(owners) > 1
                else "作品未在当前广告主可投的授权达人中找到"
            ),
            "candidate_aweme_ids": sorted(owners),
        })

    works_by_creator = {}
    creators_by_id = {}
    for item_id, owner in resolved_owners.items():
        aweme_id = owner["creator"]["aweme_id"]
        creators_by_id[aweme_id] = owner["creator"]
        works_by_creator.setdefault(aweme_id, []).append(work_by_id[item_id])

    product_tasks = []
    for aweme_id, creator_works in works_by_creator.items():
        item_ids_by_product = {}
        for work in creator_works:
            for product_id in work_product_ids(work, product_ids):
                item_ids_by_product.setdefault(product_id, []).append(
                    work["aweme_item_id"]
                )
        for product_id in product_ids:
            for item_chunk in chunks(
                item_ids_by_product.get(product_id) or [],
                MAX_ITEM_IDS_PER_QUERY,
            ):
                product_tasks.append({
                    "creator": creators_by_id[aweme_id],
                    "product_id": product_id,
                    "item_ids": item_chunk,
                })
    product_results, product_failures = run_video_queries(
        video_client,
        advertiser_id,
        product_tasks,
        concurrency,
        retry_rate_limits=True,
    )
    matches_by_item = {}
    for task, response in product_results:
        requested = set(task["item_ids"])
        for item in response["videos"]:
            item_id = str(item.get("aweme_item_id") or "")
            if item_id not in requested:
                continue
            match = matches_by_item.setdefault(item_id, {
                "material": compact_video(item),
                "matched_product_ids": [],
            })
            product_id = str(task["product_id"])
            if product_id not in match["matched_product_ids"]:
                match["matched_product_ids"].append(product_id)

    failed_creators = {row["aweme_id"] for row in product_failures}
    matched = []
    for item_id, owner in resolved_owners.items():
        work = work_by_id[item_id]
        match = matches_by_item.get(item_id)
        if not match:
            aweme_id = owner["creator"]["aweme_id"]
            skipped.append({
                **work,
                "status": "skipped",
                "reason": (
                    "product_query_incomplete"
                    if aweme_id in failed_creators
                    else "product_mismatch"
                ),
                "message": "作品与模板绑定商品不匹配",
                "aweme_id": aweme_id,
                "creator_name": owner["creator"].get("aweme_name"),
            })
            continue
        matched_products = set(match["matched_product_ids"])
        matched.append({
            **work,
            "status": "matched",
            "aweme_id": owner["creator"]["aweme_id"],
            "creator": copy.deepcopy(owner["creator"]),
            "material": match["material"],
            "matched_product_ids": [
                product_id for product_id in product_ids
                if product_id in matched_products
            ],
        })

    incomplete_reasons = {
        row.get("reason") for row in skipped
        if row.get("reason") in {"creator_query_incomplete", "product_query_incomplete"}
    }
    impacting_failures = []
    if "creator_query_incomplete" in incomplete_reasons:
        impacting_failures.extend(ownership_failures)
    if "product_query_incomplete" in incomplete_reasons:
        incomplete_creators = {
            row.get("aweme_id") for row in skipped
            if row.get("reason") == "product_query_incomplete"
        }
        impacting_failures.extend(
            row for row in product_failures
            if row.get("aweme_id") in incomplete_creators
        )

    return {
        "matched": sorted(matched, key=lambda row: row["input_index"]),
        "skipped": sorted(skipped, key=lambda row: row["input_index"]),
        "creators": creators,
        "disabled_creators": disabled_creators,
        "query_failures": impacting_failures,
        "resolved_owner_hints": {
            item_id: {
                "aweme_id": owner["creator"]["aweme_id"],
                "aweme_show_id": owner["creator"].get("aweme_show_id"),
            }
            for item_id, owner in resolved_owners.items()
        },
        "owner_hint_summary": {
            "supplied": len(supplied_hints),
            "eligible": len(eligible_hints),
            "verified": len(verified_hint_ids),
            "stale": len(supplied_hints) - len(verified_hint_ids),
            "broad_scan_work_count": len(broad_item_ids),
            "authorized_hint_query_count": hint_auth_query_count,
            "authorized_hint_failure_count": len(hint_auth_failures),
            "official_video_query_count": len(hinted_tasks) + len(broad_tasks),
            "product_video_query_count": len(product_tasks),
        },
        "authorized_creator_query": {
            "mode": "broad_scan" if broad_item_ids else "targeted_hint",
            "page_count": creator_result["page_count"],
            "truncated": creator_result["truncated"],
        },
    }
