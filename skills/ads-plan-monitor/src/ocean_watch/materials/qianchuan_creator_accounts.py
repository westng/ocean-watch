from ocean_watch.core.data import get_path, is_missing
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.pagination import declared_page_count
from ocean_watch.core.validation import positive_integer

QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH = "/v1.0/qianchuan/uni_aweme/authorized/get/"
PRODUCT_ALL_DOMAIN_GOAL = "VIDEO_PROM_GOODS"
CREATE_SCENE = "CREATE"
MAX_PAGE_SIZE = 100


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
    search_keywords=None,
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
            search_keywords=search_keywords,
        )
        pages += 1
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        page_items = get_path(response, "data.aweme_id_list", []) or []
        if not isinstance(page_items, list):
            raise ApiError(
                "Qianchuan authorized creator rows must be a list",
                {"page": page},
            )
        for item in page_items:
            row = compact_authorized_aweme(item)
            if row.get("aweme_id"):
                rows[row["aweme_id"]] = row

        page_info = get_path(response, "data.page_info", {}) or {}
        total_page = page_info.get("total_page")
        if total_page is None:
            more_pages = False
            break
        total_page = declared_page_count(
            page_info,
            source="qianchuan_authorized_creators",
            page=page,
            row_count=len(page_items),
        )
        more_pages = page < total_page
        if not more_pages:
            break
        page += 1

    return {
        "endpoint": QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH,
        "advertiser_id": str(advertiser_id),
        "search_keywords": (
            str(search_keywords).strip()
            if not is_missing(search_keywords)
            else None
        ),
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
    expected_aweme_id=None,
):
    requested = str(douyin_id or "").strip()
    if is_missing(requested):
        raise ConfigurationError("douyin_id is required")
    advertiser_id = positive_integer(advertiser_id, "advertiser_id")
    expected_aweme_id = (
        str(positive_integer(expected_aweme_id, "expected_aweme_id"))
        if not is_missing(expected_aweme_id)
        else None
    )
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
        page_items = get_path(response, "data.aweme_id_list", []) or []
        if not isinstance(page_items, list):
            raise ApiError(
                "Qianchuan authorized creator rows must be a list",
                {"page": page},
            )
        for item in page_items:
            row = compact_authorized_aweme(item)
            candidates.append(row)
            match_field = (
                "aweme_id"
                if expected_aweme_id and row.get("aweme_id") == expected_aweme_id
                else exact_match(row, requested) if expected_aweme_id is None else None
            )
            if match_field and row.get("aweme_id"):
                matches[row["aweme_id"]] = {**row, "match_field": match_field}
        page_info = get_path(response, "data.page_info", {}) or {}
        total_page = page_info.get("total_page")
        if total_page is None:
            break
        total_page = declared_page_count(
            page_info,
            source="qianchuan_authorized_creator_resolver",
            page=page,
            row_count=len(page_items),
        )
        more_pages = page < total_page
        if matches:
            break
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
                "expected_aweme_id": expected_aweme_id,
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
