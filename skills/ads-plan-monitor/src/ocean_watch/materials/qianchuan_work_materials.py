import copy
from concurrent.futures import ThreadPoolExecutor, as_completed

from ocean_watch.core.errors import ApiError
from ocean_watch.materials.qianchuan_creator_accounts import list_authorized_awemes
from ocean_watch.materials.query_qianchuan_creator_videos import (
    compact_video,
    fetch_creator_videos,
)

MAX_ITEM_IDS_PER_QUERY = 50


def chunks(values, size):
    values = list(values)
    return [values[index : index + size] for index in range(0, len(values), size)]


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


def run_video_queries(client, advertiser_id, tasks, concurrency):
    results = []
    failures = []

    def query(task):
        response = fetch_creator_videos(
            client,
            advertiser_id,
            task["creator"]["aweme_id"],
            product_id=task.get("product_id"),
            aweme_item_ids=task["item_ids"],
            count=MAX_ITEM_IDS_PER_QUERY,
        )
        return task, response

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


def resolve_work_materials(
    authorized_client,
    video_client,
    advertiser_id,
    product_ids,
    work_rows,
    *,
    concurrency=4,
    max_creator_pages=100,
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
    creators = [
        creator for creator in creator_result["creators"]
        if creator_is_usable(creator)
    ]
    disabled_creators = [
        creator for creator in creator_result["creators"]
        if not creator_is_usable(creator)
    ]

    ownership_tasks = [
        {"creator": creator, "item_ids": item_chunk}
        for creator in creators
        for item_chunk in chunks(work_ids, MAX_ITEM_IDS_PER_QUERY)
    ]
    ownership_results, ownership_failures = run_video_queries(
        video_client,
        advertiser_id,
        ownership_tasks,
        concurrency,
    )
    owners_by_item = {}
    for task, response in ownership_results:
        requested = set(task["item_ids"])
        for item in response["videos"]:
            item_id = str(item.get("aweme_item_id") or "")
            if item_id not in requested:
                continue
            owners_by_item.setdefault(item_id, {})[task["creator"]["aweme_id"]] = {
                "creator": copy.deepcopy(task["creator"]),
                "material": compact_video(item),
            }

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

    item_ids_by_creator = {}
    creators_by_id = {}
    for item_id, owner in resolved_owners.items():
        aweme_id = owner["creator"]["aweme_id"]
        creators_by_id[aweme_id] = owner["creator"]
        item_ids_by_creator.setdefault(aweme_id, []).append(item_id)

    product_tasks = [
        {
            "creator": creators_by_id[aweme_id],
            "product_id": product_id,
            "item_ids": item_chunk,
        }
        for aweme_id, item_ids in item_ids_by_creator.items()
        for product_id in product_ids
        for item_chunk in chunks(item_ids, MAX_ITEM_IDS_PER_QUERY)
    ]
    product_results, product_failures = run_video_queries(
        video_client,
        advertiser_id,
        product_tasks,
        concurrency,
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
        "authorized_creator_query": {
            "page_count": creator_result["page_count"],
            "truncated": creator_result["truncated"],
        },
        "query_failures": impacting_failures,
    }
