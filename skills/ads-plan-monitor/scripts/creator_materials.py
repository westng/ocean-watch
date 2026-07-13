#!/usr/bin/env python3
import copy
import datetime as dt


SOURCE_TYPE = "CREATOR_AUTHORIZED"
ENDPOINT = "/2/tools/aweme_auth_list/"
AUTHORIZED_STATUS = "AUTHRIZED"
DEFAULT_AUTH_TYPES = ("VIDEO_ITEM",)
OFFICIAL_MAX_MATERIALS = 10


class CreatorMaterialError(ValueError):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def official_id(value):
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        return None
    normalized = str(value).strip()
    if not normalized or not normalized.isdigit():
        return None
    return normalized


def parse_official_time(value):
    if not value:
        return None
    if isinstance(value, dt.datetime):
        return value
    normalized = str(value).strip()
    if not normalized:
        return None
    try:
        return dt.datetime.fromisoformat(normalized.replace("Z", "+00:00"))
    except ValueError:
        pass
    for pattern in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d"):
        try:
            return dt.datetime.strptime(normalized, pattern)
        except ValueError:
            continue
    return None


def comparable_now(reference, now=None):
    current = now or dt.datetime.now(reference.tzinfo)
    if reference.tzinfo is None and current.tzinfo is not None:
        return current.replace(tzinfo=None)
    if reference.tzinfo is not None and current.tzinfo is None:
        return current.replace(tzinfo=reference.tzinfo)
    return current


def source_key(advertiser_id, item_id):
    return ":".join((
        "marketing",
        str(advertiser_id),
        SOURCE_TYPE,
        "ITEM_ID",
        str(item_id),
    ))


def normalize_relation(row, advertiser_id, minimum_remaining_days=1, now=None):
    video = row.get("video_info") or {}
    aweme_id = official_id(row.get("aweme_id"))
    item_id = official_id(video.get("item_id"))
    video_id = official_id(video.get("video_id"))
    cover_id = official_id(video.get("video_cover_id"))
    end_time = parse_official_time(row.get("end_time"))
    reasons = []

    if row.get("auth_type") not in DEFAULT_AUTH_TYPES:
        reasons.append("unsupported_auth_type")
    raw_authorization_status = row.get("auth_status")
    if raw_authorization_status != AUTHORIZED_STATUS:
        reasons.append("authorization_not_active")
    if not aweme_id:
        reasons.append("missing_aweme_id")
    if not item_id:
        reasons.append("missing_item_id")
    if not video_id:
        reasons.append("missing_video_id")
    if not cover_id:
        reasons.append("missing_video_cover_id")

    minimum_remaining_days = max(0, int(minimum_remaining_days or 0))
    if end_time is None:
        reasons.append("authorization_end_time_missing")
    else:
        current = comparable_now(end_time, now)
        if end_time <= current:
            reasons.append("authorization_expired")
        elif minimum_remaining_days and end_time < current + dt.timedelta(days=minimum_remaining_days):
            reasons.append("authorization_expires_too_soon")

    candidate_key = source_key(advertiser_id, item_id) if item_id else None
    return {
        "channel": "marketing",
        "owner_advertiser_id": str(advertiser_id),
        "source_type": SOURCE_TYPE,
        "source_key": {
            "id_type": "ITEM_ID",
            "id_value": item_id,
            "canonical": candidate_key,
        },
        "material_id": official_id(video.get("mid")),
        "video_id": video_id,
        "item_id": item_id,
        "image_mode": video.get("image_mode"),
        "video_cover_id": cover_id,
        "video_cover_url": video.get("video_cover_url"),
        "title": video.get("title"),
        "duration": video.get("duration"),
        "creator_id": aweme_id,
        "creator_name": row.get("aweme_name"),
        "authorization_subject_id": (
            str(row.get("open_id")).strip() if row.get("open_id") else None
        ),
        "authorization_type": row.get("auth_type"),
        "authorization_status": (
            "VALID" if raw_authorization_status == AUTHORIZED_STATUS else "INVALID"
        ),
        "raw_authorization_status": raw_authorization_status,
        "authorization_start_at": row.get("start_time"),
        "authorization_expires_at": row.get("end_time"),
        "warning_types": list(row.get("warning_types") or []),
        "usable": not reasons,
        "unusable_reasons": reasons,
    }


def response_total_pages(response):
    page_info = ((response.get("data") or {}).get("page_info") or {})
    try:
        return max(1, int(page_info.get("total_page") or 1))
    except (TypeError, ValueError):
        return 1


def fetch_candidates(
    fetch_json,
    base_url,
    access_token,
    advertiser_id,
    *,
    auth_types=None,
    aweme_ids=None,
    item_ids=None,
    minimum_remaining_days=1,
    page_size=100,
    now=None,
):
    normalized_advertiser_id = official_id(advertiser_id)
    if not normalized_advertiser_id:
        raise CreatorMaterialError("invalid_advertiser_id", "advertiser_id must be a decimal string")

    filtering = {
        "auth_type": list(auth_types or DEFAULT_AUTH_TYPES),
        "auth_status": [AUTHORIZED_STATUS],
    }
    normalized_aweme_ids = [official_id(value) for value in (aweme_ids or [])]
    normalized_item_ids = [official_id(value) for value in (item_ids or [])]
    if any(value is None for value in normalized_aweme_ids + normalized_item_ids):
        raise CreatorMaterialError("invalid_material_filter", "aweme_ids and item_ids must be decimal strings")
    if normalized_aweme_ids:
        filtering["aweme_ids"] = normalized_aweme_ids
    if normalized_item_ids:
        filtering["item_ids"] = [int(value) for value in normalized_item_ids]

    responses = []
    rows = []
    page = 1
    while True:
        params = {
            "advertiser_id": normalized_advertiser_id,
            "filtering": filtering,
            "page": page,
            "page_size": int(page_size),
        }
        response = fetch_json(base_url, access_token, ENDPOINT, params)
        responses.append(response)
        if response.get("code") != 0:
            raise CreatorMaterialError(
                "creator_material_query_failed",
                str(response.get("message") or "creator material query failed"),
                {"code": response.get("code"), "request_id": response.get("request_id")},
            )
        rows.extend(((response.get("data") or {}).get("list") or []))
        if page >= response_total_pages(response):
            break
        page += 1

    candidates = []
    seen = set()
    for row in rows:
        candidate = normalize_relation(
            row,
            normalized_advertiser_id,
            minimum_remaining_days=minimum_remaining_days,
            now=now,
        )
        canonical = candidate["source_key"]["canonical"]
        if canonical and canonical in seen:
            continue
        if canonical:
            seen.add(canonical)
        candidates.append(candidate)

    return {
        "endpoint": ENDPOINT,
        "advertiser_id": normalized_advertiser_id,
        "filters": filtering,
        "page_count": len(responses),
        "request_ids": [response.get("request_id") for response in responses if response.get("request_id")],
        "candidates": candidates,
    }


def select_candidates(candidates, item_ids, max_materials=5):
    requested = [official_id(value) for value in (item_ids or [])]
    if not requested or any(value is None for value in requested):
        raise CreatorMaterialError("material_selection_empty", "select at least one valid item_id")
    if len(set(requested)) != len(requested):
        raise CreatorMaterialError("duplicate_material_selection", "item_id selection contains duplicates")
    limit = min(OFFICIAL_MAX_MATERIALS, int(max_materials or 0))
    if limit < 1 or len(requested) > limit:
        raise CreatorMaterialError(
            "material_selection_limit_exceeded",
            f"select no more than {limit} creator materials for one unit",
        )

    by_item_id = {
        candidate.get("item_id"): candidate
        for candidate in candidates
        if candidate.get("item_id")
    }
    missing = [item_id for item_id in requested if item_id not in by_item_id]
    if missing:
        raise CreatorMaterialError(
            "creator_material_not_found",
            "selected creator material was not returned by the current authorization query",
            {"item_ids": missing},
        )
    selected = [copy.deepcopy(by_item_id[item_id]) for item_id in requested]
    unusable = [candidate for candidate in selected if not candidate.get("usable")]
    if unusable:
        raise CreatorMaterialError(
            "creator_material_unavailable",
            "selected creator material is not currently usable",
            {
                "materials": [
                    {
                        "item_id": candidate.get("item_id"),
                        "reasons": candidate.get("unusable_reasons"),
                    }
                    for candidate in unusable
                ]
            },
        )

    owners = {candidate["owner_advertiser_id"] for candidate in selected}
    creators = {candidate["creator_id"] for candidate in selected}
    if len(owners) != 1:
        raise CreatorMaterialError(
            "creator_material_owner_mismatch",
            "one unit cannot contain materials authorized to different advertisers",
        )
    if len(creators) != 1:
        raise CreatorMaterialError(
            "mixed_creator_materials",
            "one native unit can only use materials from one aweme_id",
        )
    return selected


def select_latest_candidates(candidates, max_materials=5):
    usable = [candidate for candidate in candidates if candidate.get("usable")]
    if not usable:
        raise CreatorMaterialError(
            "material_selection_empty",
            "no usable creator materials are available for latest selection",
        )
    groups = {}
    for candidate in usable:
        key = (candidate["owner_advertiser_id"], candidate["creator_id"])
        groups.setdefault(key, []).append(candidate)

    def candidate_order(candidate):
        started_at = parse_official_time(candidate.get("authorization_start_at"))
        timestamp = started_at.timestamp() if started_at else 0
        return timestamp, int(candidate["item_id"])

    for rows in groups.values():
        rows.sort(key=candidate_order, reverse=True)
    selected_group = max(
        groups.values(),
        key=lambda rows: candidate_order(rows[0]),
    )
    limit = min(OFFICIAL_MAX_MATERIALS, int(max_materials or 0))
    return select_candidates(
        selected_group,
        [candidate["item_id"] for candidate in selected_group[:limit]],
        max_materials=limit,
    )


def apply_to_promotion_payload(promotion_payload, selected_candidates):
    if not selected_candidates:
        raise CreatorMaterialError("material_selection_empty", "creator material selection is empty")
    advertiser_id = official_id(promotion_payload.get("advertiser_id"))
    selected = select_candidates(
        selected_candidates,
        [candidate.get("item_id") for candidate in selected_candidates],
        max_materials=len(selected_candidates),
    )
    owner = selected[0]["owner_advertiser_id"]
    if advertiser_id != owner:
        raise CreatorMaterialError(
            "creator_material_owner_mismatch",
            f"creator materials belong to advertiser {owner}, not {advertiser_id}",
        )

    payload = copy.deepcopy(promotion_payload)
    materials = payload.setdefault("promotion_materials", {})
    materials["video_material_list"] = [
        {
            "image_mode": candidate.get("image_mode") or "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
            "video_id": candidate["video_id"],
            "video_cover_id": candidate["video_cover_id"],
            "item_id": int(candidate["item_id"]),
        }
        for candidate in selected
    ]
    native_setting = payload.setdefault("native_setting", {})
    existing_aweme_id = official_id(native_setting.get("aweme_id"))
    selected_aweme_id = selected[0]["creator_id"]
    if existing_aweme_id and existing_aweme_id != selected_aweme_id:
        raise CreatorMaterialError(
            "native_identity_mismatch",
            "promotion payload already contains a different native aweme_id",
        )
    native_setting["aweme_id"] = selected_aweme_id
    return payload
