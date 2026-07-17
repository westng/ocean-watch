import copy

from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path
from ocean_watch.materials import creator_materials

PROMOTION_LIST_PATH = "/v3.0/promotion/list/"
PAGE_SIZE = 20
MAX_PAGES = 50
USABLE_MATERIAL_STATUS = "MATERIAL_STATUS_OK"
PROMOTION_FIELDS = [
    "promotion_id",
    "project_id",
    "advertiser_id",
    "promotion_materials",
    "promotion_modify_time",
]


def missing_cover(candidate):
    return (
        not candidate.get("video_cover_id")
        and "missing_video_cover_id" in (candidate.get("unusable_reasons") or [])
    )


def require_ok(response):
    if response.get("code") == 0:
        return response
    raise creator_materials.CreatorMaterialError(
        "creator_cover_history_query_failed",
        "official promotion query failed while resolving a creator cover",
        {
            "endpoint": PROMOTION_LIST_PATH,
            "response_code": response.get("code"),
            "response_message": response.get("message") or response.get("msg"),
            "request_id": response.get("request_id"),
        },
    )


def total_pages(response, page, rows):
    raw_value = get_path(response, "data.page_info.total_page")
    if raw_value is None:
        if len(rows) >= PAGE_SIZE:
            raise creator_materials.CreatorMaterialError(
                "creator_cover_history_pagination_invalid",
                "promotion history omitted pagination metadata for a full page",
            )
        return page
    try:
        value = int(raw_value)
    except (TypeError, ValueError) as exc:
        raise creator_materials.CreatorMaterialError(
            "creator_cover_history_pagination_invalid",
            "promotion history returned an invalid total_page",
        ) from exc
    if value < 0 or (value == 0 and rows) or value < page:
        raise creator_materials.CreatorMaterialError(
            "creator_cover_history_pagination_invalid",
            "promotion history returned contradictory pagination metadata",
        )
    if value > MAX_PAGES:
        raise creator_materials.CreatorMaterialError(
            "creator_cover_history_too_large",
            "promotion history exceeds the creator-cover safety limit",
            {"total_pages": value, "max_pages": MAX_PAGES},
        )
    return value


def fetch_promotions(client, advertiser_id):
    promotions = []
    request_ids = []
    page = 1
    while True:
        response = require_ok(client.get(PROMOTION_LIST_PATH, {
            "advertiser_id": int(advertiser_id),
            "filtering": {},
            "fields": PROMOTION_FIELDS,
            "page": page,
            "page_size": PAGE_SIZE,
        }))
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        rows = list(get_path(response, "data.list", []) or [])
        promotions.extend(rows)
        page_count = total_pages(response, page, rows)
        if page >= page_count:
            break
        page += 1
    return promotions, request_ids


def matching_cover_references(promotions, advertiser_id, candidates):
    targets = {
        (candidate.get("item_id"), candidate.get("material_id"))
        for candidate in candidates
        if candidate.get("item_id") and candidate.get("material_id")
    }
    matches = {target: {} for target in targets}
    for promotion in promotions:
        promotion_advertiser_id = creator_materials.official_id(promotion.get("advertiser_id"))
        if promotion_advertiser_id and promotion_advertiser_id != advertiser_id:
            continue
        for material in get_path(
            promotion,
            "promotion_materials.video_material_list",
            [],
        ) or []:
            key = (
                creator_materials.official_id(material.get("item_id")),
                creator_materials.official_id(material.get("material_id")),
            )
            if key not in matches:
                continue
            if material.get("material_status") != USABLE_MATERIAL_STATUS:
                continue
            cover_id = creator_materials.official_string(material.get("video_cover_id"))
            if not cover_id:
                continue
            matches[key].setdefault(cover_id, []).append({
                "project_id": creator_materials.official_id(promotion.get("project_id")),
                "promotion_id": creator_materials.official_id(promotion.get("promotion_id")),
                "material_id": key[1],
            })
    return matches


def resolve_missing_covers(candidates, config, client=None):
    effective = copy.deepcopy(candidates)
    missing = [candidate for candidate in effective if missing_cover(candidate)]
    if not missing:
        return effective, {"status": "not_required"}

    advertiser_ids = {
        creator_materials.official_id(candidate.get("owner_advertiser_id"))
        for candidate in missing
    }
    if None in advertiser_ids or len(advertiser_ids) != 1:
        raise creator_materials.CreatorMaterialError(
            "creator_cover_owner_mismatch",
            "missing creator covers must belong to one valid advertiser",
        )
    advertiser_id = advertiser_ids.pop()
    if client is None:
        client = OceanEngineClient(
            get_path(config, "api.base_url"),
            get_path(config, "api.access_token"),
        )
    promotions, request_ids = fetch_promotions(client, advertiser_id)
    references = matching_cover_references(promotions, advertiser_id, missing)

    resolved = []
    unresolved = []
    for candidate in missing:
        key = (candidate.get("item_id"), candidate.get("material_id"))
        covers = references.get(key) or {}
        if len(covers) > 1:
            raise creator_materials.CreatorMaterialError(
                "creator_cover_selection_required",
                "multiple historical covers match the selected creator material",
                {
                    "item_id": key[0],
                    "material_id": key[1],
                    "candidate_cover_ids": sorted(covers),
                },
            )
        if not covers:
            unresolved.append(key[0])
            continue
        cover_id, cover_references = next(iter(covers.items()))
        candidate["video_cover_id"] = cover_id
        candidate["unusable_reasons"] = [
            reason
            for reason in candidate.get("unusable_reasons") or []
            if reason != "missing_video_cover_id"
        ]
        candidate["usable"] = not candidate["unusable_reasons"]
        resolved.append({
            "item_id": key[0],
            "material_id": key[1],
            "video_cover_id": cover_id,
            "references": cover_references,
        })

    return effective, {
        "status": "resolved" if not unresolved else "partially_resolved",
        "source": "matching_official_promotion",
        "resolved": resolved,
        "unresolved_item_ids": unresolved,
        "promotion_count": len(promotions),
        "request_ids": request_ids,
    }
