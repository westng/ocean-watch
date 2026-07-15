from ocean_watch.core.data import get_path, is_missing
from ocean_watch.core.errors import ApiError, ConfigurationError

QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH = "/v1.0/qianchuan/uni_aweme/authorized/get/"
PRODUCT_ALL_DOMAIN_GOAL = "VIDEO_PROM_GOODS"
CREATE_SCENE = "CREATE"
MAX_PAGE_SIZE = 100


def positive_integer(value, field, maximum=None):
    if isinstance(value, bool):
        raise ConfigurationError(f"{field} must be a positive integer")
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    parsed = int(text)
    if maximum is not None and parsed > maximum:
        raise ConfigurationError(f"{field} must not exceed {maximum}")
    return parsed


def compact_authorized_aweme(item):
    row = {
        "aweme_id": str(item.get("aweme_id")) if item.get("aweme_id") is not None else None,
        "aweme_show_id": str(item.get("aweme_show_id") or "").strip() or None,
        "aweme_name": str(item.get("aweme_name") or "").strip() or None,
        "aweme_avatar": item.get("aweme_avatar"),
        "auth_type": item.get("auth_type"),
    }
    permission_fields = (
        "has_authorized",
        "is_product_uni_prom_disabled",
        "product_disable_reasons",
        "product_uni_prom_apply_type",
        "can_control_uniprom",
        "can_apply_uniprom",
        "has_shop_permission",
        "has_live_permission",
    )
    for field in permission_fields:
        if field in item:
            row[field] = item[field]
    return row


def exact_match(row, douyin_id):
    requested = str(douyin_id).strip()
    if row.get("aweme_show_id") == requested:
        return "aweme_show_id"
    if requested.isdigit() and row.get("aweme_id") == requested:
        return "aweme_id"
    return None


def authorized_filtering(search_keywords=None):
    filtering = {
        "marketing_goal": PRODUCT_ALL_DOMAIN_GOAL,
        "scene": CREATE_SCENE,
    }
    if not is_missing(search_keywords):
        filtering["search_key_words"] = str(search_keywords).strip()
    return filtering


def fetch_authorized_aweme_page(
    client,
    advertiser_id,
    *,
    page,
    page_size=MAX_PAGE_SIZE,
    search_keywords=None,
):
    response = client.get(
        QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH,
        params={
            "advertiser_id": positive_integer(advertiser_id, "advertiser_id"),
            "filtering": authorized_filtering(search_keywords),
            "page": positive_integer(page, "page"),
            "page_size": positive_integer(
                page_size,
                "page_size",
                maximum=MAX_PAGE_SIZE,
            ),
        },
    )
    if response.get("code") != 0:
        raise ApiError(
            "Qianchuan authorized creator query failed",
            {
                "code": response.get("code"),
                "message": response.get("message"),
                "request_id": response.get("request_id"),
                "search_keywords": search_keywords,
            },
        )
    return response


def list_authorized_awemes(
    client,
    advertiser_id,
    *,
    page_size=MAX_PAGE_SIZE,
    max_pages=100,
):
    advertiser_id = positive_integer(advertiser_id, "advertiser_id")
    page_size = positive_integer(page_size, "page_size", maximum=MAX_PAGE_SIZE)
    max_pages = positive_integer(max_pages, "max_pages")
    rows = {}
    request_ids = []
    page = 1
    pages = 0
    more_pages = False
    while pages < max_pages:
        response = fetch_authorized_aweme_page(
            client,
            advertiser_id,
            page=page,
            page_size=page_size,
        )
        pages += 1
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        for item in get_path(response, "data.aweme_id_list", []) or []:
            row = compact_authorized_aweme(item)
            if row.get("aweme_id"):
                rows[row["aweme_id"]] = row

        page_info = get_path(response, "data.page_info", {}) or {}
        total_page = page_info.get("total_page")
        if total_page is None:
            more_pages = False
            break
        total_page = positive_integer(total_page, "page_info.total_page")
        more_pages = page < total_page
        if not more_pages:
            break
        page += 1

    return {
        "endpoint": QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH,
        "advertiser_id": str(advertiser_id),
        "creators": list(rows.values()),
        "page_count": pages,
        "request_ids": request_ids,
        "truncated": more_pages and pages >= max_pages,
    }


def resolve_authorized_aweme(
    client,
    advertiser_id,
    douyin_id,
    creator_name=None,
    page_size=MAX_PAGE_SIZE,
    max_pages=100,
):
    requested = str(douyin_id or "").strip()
    if is_missing(requested):
        raise ConfigurationError("douyin_id is required")
    advertiser_id = positive_integer(advertiser_id, "advertiser_id")
    page_size = positive_integer(page_size, "resolver_page_size", maximum=MAX_PAGE_SIZE)
    max_pages = positive_integer(max_pages, "resolver_max_pages")

    page = 1
    pages = 0
    request_ids = []
    candidates = []
    matches = {}
    more_pages = False
    while pages < max_pages:
        response = fetch_authorized_aweme_page(
            client,
            advertiser_id,
            page=page,
            page_size=page_size,
            search_keywords=requested,
        )
        pages += 1
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        for item in get_path(response, "data.aweme_id_list", []) or []:
            row = compact_authorized_aweme(item)
            candidates.append(row)
            match_field = exact_match(row, requested)
            if match_field and row.get("aweme_id"):
                matches[row["aweme_id"]] = {**row, "match_field": match_field}
        if matches:
            break

        page_info = get_path(response, "data.page_info", {}) or {}
        total_page = page_info.get("total_page")
        if total_page is None:
            break
        total_page = positive_integer(total_page, "page_info.total_page")
        more_pages = page < total_page
        if not more_pages:
            break
        page += 1

    truncated = not matches and more_pages and pages >= max_pages
    if not matches:
        raise ConfigurationError(
            "No exact authorized Qianchuan creator matched douyin_id",
            {
                "douyin_id": requested,
                "advertiser_id": str(advertiser_id),
                "candidate_count": len(candidates),
                "candidates": candidates[:10],
                "truncated": truncated,
            },
        )
    if len(matches) != 1:
        raise ConfigurationError(
            "douyin_id matched multiple authorized Qianchuan creators",
            {
                "douyin_id": requested,
                "matches": list(matches.values()),
            },
        )

    resolved = next(iter(matches.values()))
    requested_name = str(creator_name or "").strip() or None
    return {
        **resolved,
        "requested_douyin_id": requested,
        "requested_creator_name": requested_name,
        "creator_name_matches": (
            None if requested_name is None else resolved.get("aweme_name") == requested_name
        ),
        "endpoint": QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH,
        "page_count": pages,
        "request_ids": request_ids,
    }
