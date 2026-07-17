import copy

from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path, is_missing

PROJECT_LIST_PATH = "/v3.0/project/list/"
PROMOTION_LIST_PATH = "/v3.0/promotion/list/"
EVENT_ASSET_LIST_PATH = "/2/tools/event/all_assets/list/"
OPTIMIZED_GOAL_PATH = "/v3.0/event_manager/optimized_goal/get_v2/"
DPA_ASSET_DETAIL_PATH = "/2/dpa/asset_v2/detail/read/"

PAGE_SIZE = 100
MAX_PROJECT_PAGES = 3
MAX_REFERENCE_PROJECTS = 20

PROJECT_FIELDS = [
    "project_id",
    "advertiser_id",
    "name",
    "landing_type",
    "marketing_goal",
    "delivery_mode",
    "ad_type",
    "asset_type",
    "related_product",
    "optimize_goal",
    "project_create_time",
]
PROMOTION_FIELDS = [
    "promotion_id",
    "project_id",
    "promotion_name",
    "promotion_materials",
    "brand_info",
    "promotion_modify_time",
]


class MarketingRuntimeAssetError(ValueError):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def decimal_integer(value):
    if isinstance(value, int) and not isinstance(value, bool):
        return value
    normalized = str(value or "").strip()
    return int(normalized) if normalized.isdigit() else None


def unique_ids(values):
    result = []
    seen = set()
    for value in values or []:
        if is_missing(value):
            continue
        normalized = str(value)
        if normalized in seen:
            continue
        seen.add(normalized)
        result.append(value)
    return result


def require_ok(response, endpoint):
    if response.get("code") == 0:
        return response
    raise MarketingRuntimeAssetError(
        "marketing_runtime_asset_query_failed",
        f"official runtime asset query failed: {endpoint}",
        {
            "endpoint": endpoint,
            "response_code": response.get("code"),
            "response_message": response.get("message") or response.get("msg"),
            "request_id": response.get("request_id"),
        },
    )


def project_matches(config, project):
    product_id = get_path(config, "resolved_ids.unique_product_id")
    project_product_id = get_path(project, "related_product.unique_product_id")
    if not is_missing(product_id) and str(project_product_id) != str(product_id):
        return False

    defaults = config.get("defaults") or {}
    for config_field, project_field in (
        ("landing_type", "landing_type"),
        ("marketing_goal", "marketing_goal"),
        ("delivery_mode", "delivery_mode"),
        ("ad_type", "ad_type"),
        ("asset_type", "asset_type"),
    ):
        expected = defaults.get(config_field)
        actual = project.get(project_field)
        if not is_missing(expected) and not is_missing(actual) and actual != expected:
            return False

    external_action = defaults.get("external_action")
    project_action = get_path(project, "optimize_goal.external_action")
    if (
        not is_missing(external_action)
        and not is_missing(project_action)
        and project_action != external_action
    ):
        return False
    return True


def fetch_reference_projects(client, config):
    advertiser_id = decimal_integer(get_path(config, "account.advertiser_id"))
    matches = []
    request_ids = []
    for page in range(1, MAX_PROJECT_PAGES + 1):
        response = require_ok(
            client.get(
                PROJECT_LIST_PATH,
                {
                    "advertiser_id": advertiser_id,
                    "fields": PROJECT_FIELDS,
                    "filtering": {},
                    "page": page,
                    "page_size": PAGE_SIZE,
                },
            ),
            PROJECT_LIST_PATH,
        )
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        data = response.get("data") or {}
        rows = data.get("list") or []
        matches.extend(row for row in rows if project_matches(config, row))
        total_page = int(get_path(data, "page_info.total_page", page) or page)
        if page >= total_page or len(rows) < PAGE_SIZE:
            break
    return matches, request_ids


def optimized_goal_params(config, asset_id):
    defaults = config.get("defaults") or {}
    params = {
        "advertiser_id": decimal_integer(get_path(config, "account.advertiser_id")),
        "landing_type": defaults.get("landing_type"),
        "ad_type": defaults.get("ad_type"),
        "asset_type": defaults.get("asset_type"),
        "marketing_goal": defaults.get("marketing_goal"),
        "delivery_mode": defaults.get("delivery_mode"),
        "delivery_type": "NORMAL",
        "asset_id": decimal_integer(asset_id) or asset_id,
    }
    return {key: value for key, value in params.items() if not is_missing(value)}


def event_asset_supports_goal(client, config, asset_id):
    response = require_ok(
        client.get(OPTIMIZED_GOAL_PATH, optimized_goal_params(config, asset_id)),
        OPTIMIZED_GOAL_PATH,
    )
    external_action = get_path(config, "defaults.external_action")
    goals = get_path(response, "data.goals", []) or []
    supported = any(
        is_missing(external_action) or goal.get("external_action") == external_action
        for goal in goals
    )
    return supported, response.get("request_id")


def project_event_asset_ids(projects):
    return unique_ids(
        asset_id
        for project in projects
        for asset_id in (get_path(project, "optimize_goal.asset_ids", []) or [])
    )


def fetch_event_asset_candidates(client, config):
    advertiser_id = decimal_integer(get_path(config, "account.advertiser_id"))
    response = require_ok(
        client.get(
            EVENT_ASSET_LIST_PATH,
            {
                "advertiser_id": advertiser_id,
                "filtering": {"asset_type": "THIRD_EXTERNAL"},
                "page": 1,
                "page_size": PAGE_SIZE,
            },
        ),
        EVENT_ASSET_LIST_PATH,
    )
    return get_path(response, "data.asset_list", []) or [], response.get("request_id")


def valid_event_assets(client, config, candidates):
    valid = []
    request_ids = []
    for candidate in candidates:
        asset_id = candidate.get("asset_id") if isinstance(candidate, dict) else candidate
        if is_missing(asset_id):
            continue
        supported, request_id = event_asset_supports_goal(client, config, asset_id)
        if request_id:
            request_ids.append(request_id)
        if supported:
            valid.append(candidate)
    return valid, request_ids


def resolve_event_assets(client, config, reference_projects=None):
    configured = unique_ids(get_path(config, "resolved_ids.event_asset_ids", []))
    if configured:
        return configured, {
            "status": "configured",
            "source": "template",
            "asset_ids": [str(value) for value in configured],
        }

    projects = reference_projects or []
    project_candidates = project_event_asset_ids(projects)
    if project_candidates:
        valid, request_ids = valid_event_assets(client, config, project_candidates)
        if len(valid) == 1:
            return valid, {
                "status": "resolved",
                "source": "matching_project",
                "asset_ids": [str(valid[0])],
                "request_ids": request_ids,
            }
        if len(valid) > 1:
            raise MarketingRuntimeAssetError(
                "event_asset_selection_required",
                "multiple event assets are used by matching projects",
                {"candidate_event_asset_ids": [str(value) for value in valid]},
            )

    candidates, list_request_id = fetch_event_asset_candidates(client, config)
    valid, goal_request_ids = valid_event_assets(client, config, candidates)
    if len(valid) == 1:
        asset_id = valid[0]["asset_id"]
        return [asset_id], {
            "status": "resolved",
            "source": "event_asset_list",
            "asset_ids": [str(asset_id)],
            "request_ids": [list_request_id, *goal_request_ids],
        }
    details = {
        "candidate_event_assets": [
            {
                "asset_id": str(item.get("asset_id")),
                "asset_name": item.get("asset_name"),
            }
            for item in valid
        ],
    }
    if not valid:
        raise MarketingRuntimeAssetError(
            "event_asset_unavailable",
            "no event asset supports the selected optimization goal",
            details,
        )
    raise MarketingRuntimeAssetError(
        "event_asset_selection_required",
        "multiple event assets support the selected optimization goal",
        details,
    )


def contains_product_field(value, field_names):
    if isinstance(value, dict):
        for key, child in value.items():
            if key in field_names and not is_missing(child):
                return True
            if contains_product_field(child, field_names):
                return True
    elif isinstance(value, list):
        return any(contains_product_field(item, field_names) for item in value)
    return False


def query_dpa_product_fields(client, config, field_names):
    advertiser_id = decimal_integer(get_path(config, "account.advertiser_id"))
    product_id = decimal_integer(get_path(config, "resolved_ids.unique_product_id"))
    if advertiser_id is None or product_id is None:
        return False, None, "non_decimal_product_id"
    response = client.post(
        DPA_ASSET_DETAIL_PATH,
        {
            "advertiser_id": advertiser_id,
            "asset_ids": [],
            "unique_product_ids": [product_id],
        },
    )
    if response.get("code") != 0:
        return False, response.get("request_id"), "query_failed"
    available = contains_product_field(response.get("data"), set(field_names))
    return available, response.get("request_id"), "available" if available else "empty"


def clean_brand_info(value):
    if not isinstance(value, dict):
        return {}
    return {
        key: item
        for key, item in value.items()
        if item not in (None, "", [], {})
    }


def reference_product_creative(client, projects):
    request_ids = []
    for project in projects[:MAX_REFERENCE_PROJECTS]:
        project_id = project.get("project_id")
        if is_missing(project_id):
            continue
        response = require_ok(
            client.get(
                PROMOTION_LIST_PATH,
                {
                    "advertiser_id": project.get("advertiser_id"),
                    "filtering": {"project_id": project_id},
                    "fields": PROMOTION_FIELDS,
                    "page": 1,
                    "page_size": 20,
                },
            ),
            PROMOTION_LIST_PATH,
        )
        if response.get("request_id"):
            request_ids.append(response["request_id"])
        for promotion in get_path(response, "data.list", []) or []:
            image_ids = unique_ids(
                get_path(promotion, "promotion_materials.product_info.image_ids", [])
            )
            if not image_ids:
                continue
            return {
                "image_ids": image_ids,
                "brand_info": clean_brand_info(promotion.get("brand_info")),
                "project_id": str(project_id),
                "promotion_id": str(promotion.get("promotion_id")),
                "request_ids": request_ids,
            }
    return None


def apply_custom_product_images(config, image_ids, brand_info=None):
    defaults = config.setdefault("defaults", {})
    product_info = defaults.setdefault("product_info", {})
    product_info["product_image_type"] = "CUSTOM"
    product_info.pop("product_image_fields", None)
    config.setdefault("resolved_ids", {})["product_image_ids"] = unique_ids(image_ids)
    clean_brand = clean_brand_info(brand_info)
    if clean_brand:
        config["resolved_ids"]["brand_info"] = clean_brand


def resolve_product_creative(client, config, reference_projects=None, dpa_result=None):
    image_type = get_path(config, "defaults.product_info.product_image_type")
    if image_type != "DPA":
        return {
            "status": "not_required",
            "source": "template_custom",
            "image_count": len(get_path(config, "resolved_ids.product_image_ids", []) or []),
        }

    configured_images = unique_ids(get_path(config, "resolved_ids.product_image_ids", []))
    if configured_images:
        apply_custom_product_images(
            config,
            configured_images,
            get_path(config, "resolved_ids.brand_info", {}),
        )
        return {
            "status": "resolved",
            "source": "template_image_ids",
            "image_count": len(configured_images),
        }

    field_names = get_path(config, "defaults.product_info.product_image_fields", []) or []
    available, request_id, dpa_status = (
        dpa_result
        if dpa_result is not None
        else query_dpa_product_fields(client, config, field_names)
    )
    if available:
        return {
            "status": "validated",
            "source": "dpa_product_fields",
            "fields": list(field_names),
            "request_id": request_id,
        }

    reference = reference_product_creative(client, reference_projects or [])
    if reference:
        apply_custom_product_images(config, reference["image_ids"], reference["brand_info"])
        return {
            "status": "resolved",
            "source": "matching_promotion",
            "image_count": len(reference["image_ids"]),
            "reference_project_id": reference["project_id"],
            "reference_promotion_id": reference["promotion_id"],
            "request_ids": reference["request_ids"],
            "dpa_status": dpa_status,
            "dpa_request_id": request_id,
        }

    raise MarketingRuntimeAssetError(
        "product_creative_asset_required",
        "DPA product image fields are unavailable and no matching promotion image was found",
        {
            "advertiser_id": str(get_path(config, "account.advertiser_id")),
            "product_id": str(get_path(config, "resolved_ids.unique_product_id")),
            "dpa_status": dpa_status,
            "dpa_request_id": request_id,
        },
    )


def resolve(config, client=None):
    effective = copy.deepcopy(config)
    advertiser_id = decimal_integer(get_path(effective, "account.advertiser_id"))
    if advertiser_id is None:
        raise MarketingRuntimeAssetError(
            "advertiser_id_required",
            "a decimal advertiser ID is required for runtime asset resolution",
        )
    external_action_required = not is_missing(
        get_path(effective, "defaults.external_action")
    )
    needs_event_asset = (
        external_action_required
        and not get_path(effective, "resolved_ids.event_asset_ids", [])
    )
    dpa_image_type = (
        get_path(effective, "defaults.product_info.product_image_type") == "DPA"
    )
    configured_product_images = get_path(
        effective,
        "resolved_ids.product_image_ids",
        [],
    )
    needs_dpa_query = dpa_image_type and not configured_product_images
    needs_client = needs_event_asset or needs_dpa_query
    if client is None and needs_client:
        client = OceanEngineClient(
            get_path(effective, "api.base_url"),
            get_path(effective, "api.access_token"),
        )

    dpa_result = None
    if needs_dpa_query:
        field_names = get_path(
            effective,
            "defaults.product_info.product_image_fields",
            [],
        ) or []
        dpa_result = query_dpa_product_fields(client, effective, field_names)
    needs_reference_creative = bool(dpa_result and not dpa_result[0])
    reference_projects = []
    project_request_ids = []
    if needs_event_asset or needs_reference_creative:
        reference_projects, project_request_ids = fetch_reference_projects(client, effective)

    if not external_action_required:
        event_resolution = {"status": "not_required"}
    else:
        event_asset_ids, event_resolution = resolve_event_assets(
            client,
            effective,
            reference_projects,
        )
        effective.setdefault("resolved_ids", {})["event_asset_ids"] = event_asset_ids
    product_resolution = resolve_product_creative(
        client,
        effective,
        reference_projects,
        dpa_result=dpa_result,
    )
    return effective, {
        "reference_project_count": len(reference_projects),
        "project_request_ids": project_request_ids,
        "event_asset": event_resolution,
        "product_creative": product_resolution,
    }
