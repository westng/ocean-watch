#!/usr/bin/env python3
import argparse
import copy

import ocean_watch.auth.channels as channels
import ocean_watch.plans.create_plan as create_plan
import ocean_watch.templates.plan_templates as plan_templates

ACCOUNT_FIELDS = {
    "resolved_ids.event_asset_ids",
    "resolved_ids.landing_page_asset_id",
}
DYNAMIC_MATERIAL_FIELDS = {
    "materials.video_ids",
    "materials.video_cover_ids",
}
PRODUCT_FIELDS = {
    "defaults.product_info",
    "resolved_ids.brand_info",
    "resolved_ids.category_name",
    "resolved_ids.brand_name",
    "resolved_ids.product_platform_id",
    "resolved_ids.product_image_ids",
}
LINK_FIELDS = {
    "links.landing_page_url",
    "links.open_url",
    "tracking_urls.track_url",
    "tracking_urls.action_track_url",
}
RUNTIME_MISSING_FIELDS = {
    "materials.video_ids",
    "runtime.creator_material_selection",
    "api.access_token",
}
SOURCE_NAME_TEMPLATES = {
    "ACCOUNT_UPLOAD": {
        "project_name_template": "{material_date}_混剪素材roi_详情页",
        "promotion_name_template": "自动投放单元_{product_name}_{material_date}日_混剪",
    },
    "CREATOR_AUTHORIZED": {
        "project_name_template": "{material_date}_原生素材roi_详情页",
        "promotion_name_template": "自动投放单元_{product_name}_{material_date}日_原生",
    },
}
PAYLOAD_REQUIRED_FIELDS = {
    "defaults.operation",
    "defaults.product_name",
    "defaults.product_id",
    "defaults.daily_budget",
    "defaults.source",
    "defaults.landing_type",
    "defaults.marketing_goal",
    "defaults.delivery_mode",
    "defaults.ad_type",
    "defaults.gender",
    "defaults.location_type",
    "defaults.district",
    "defaults.region_version",
    "defaults.schedule_type",
    "defaults.budget_mode",
    "defaults.pricing",
    "defaults.video_image_mode",
}
MIN_TITLE_LENGTH = 5
MAX_TITLE_LENGTH = 30
MIN_SELLING_POINT_LENGTH = 6
MAX_SELLING_POINT_LENGTH = 9
MAX_SELLING_POINT_COUNT = 10
DEFAULT_DPA_PRODUCT_IMAGE_FIELDS = ["images_url"]


def material_source_label(source_type):
    return {
        "ACCOUNT_UPLOAD": "上传素材",
        "CREATOR_AUTHORIZED": "达人素材",
    }.get(source_type, source_type)


def normalize_titles(titles):
    normalized = []
    for title in titles or []:
        value = str(title).strip()
        if not value or value in normalized:
            continue
        length = len(value)
        if length < MIN_TITLE_LENGTH or length > MAX_TITLE_LENGTH:
            raise ValueError(
                f"copy title length must be {MIN_TITLE_LENGTH}-{MAX_TITLE_LENGTH} characters: {value}"
            )
        normalized.append(value)
    if not normalized:
        raise ValueError("at least one non-empty copy title is required")
    return normalized


def normalize_product_selling_points(values):
    if isinstance(values, str):
        values = values.replace("，", ",").split(",")
    normalized = []
    for item in values or []:
        value = str(item).strip()
        if not value or value in normalized:
            continue
        positions = create_plan.official_text_positions(value)
        if not MIN_SELLING_POINT_LENGTH <= positions <= MAX_SELLING_POINT_LENGTH:
            raise ValueError(
                "product selling point length must be "
                f"{MIN_SELLING_POINT_LENGTH}-{MAX_SELLING_POINT_LENGTH} positions: {value}"
            )
        normalized.append(value)
    if not normalized:
        raise ValueError("at least one product selling point is required")
    if len(normalized) > MAX_SELLING_POINT_COUNT:
        raise ValueError(
            f"at most {MAX_SELLING_POINT_COUNT} product selling points are allowed"
        )
    return normalized


def normalize_product_image_ids(values):
    if values is None:
        return []
    if isinstance(values, str):
        values = values.replace("，", ",").split(",")
    elif not isinstance(values, (list, tuple)):
        values = [values]
    normalized = []
    for item in values:
        value = str(item).strip()
        if value and value not in normalized:
            normalized.append(value)
    return normalized


def delete_path(data, dotted):
    current = data
    parts = dotted.split(".")
    for part in parts[:-1]:
        current = current.get(part)
        if not isinstance(current, dict):
            return False
    if parts[-1] not in current:
        return False
    current.pop(parts[-1])
    return True


def clone_policy(source_template, target_bindings):
    if source_template is None:
        return "default"
    source_bindings = source_template["bindings"]
    advertiser_changed = str(source_bindings.get("advertiser_id")) != str(
        target_bindings["advertiser_id"]
    )
    product_changed = str(source_bindings.get("product_id")) != str(
        target_bindings["product_id"]
    )
    if advertiser_changed and product_changed:
        return "cross_advertiser_new_product"
    if advertiser_changed:
        return "cross_advertiser_same_product"
    if product_changed:
        return "same_advertiser_new_product"
    return "same_advertiser_same_product"


def prepare_source(source_template, target_bindings):
    if source_template is None:
        return {}, {"titles": []}, None, {
            "type": "default",
            "template": None,
            "policy": "default",
            "cleared_fields": [],
        }

    overrides = copy.deepcopy(source_template["overrides"])
    copy_materials = copy.deepcopy(source_template["copy_materials"])
    material_strategy = copy.deepcopy(source_template.get("material_strategy") or {})
    policy = clone_policy(source_template, target_bindings)
    fields_to_clear = set(DYNAMIC_MATERIAL_FIELDS)
    if policy.startswith("cross_advertiser"):
        fields_to_clear.update(ACCOUNT_FIELDS | PRODUCT_FIELDS | LINK_FIELDS)
    elif policy == "same_advertiser_new_product":
        fields_to_clear.update(ACCOUNT_FIELDS | PRODUCT_FIELDS | LINK_FIELDS)

    cleared = sorted(field for field in fields_to_clear if delete_path(overrides, field))
    return overrides, copy_materials, material_strategy, {
        "type": "business_template",
        "template": source_template["name"],
        "policy": policy,
        "cleared_fields": cleared,
    }


def build_material_strategy(values, inherited, policy):
    inherited = copy.deepcopy(inherited or {})
    source_type = (
        values.get("material_source_type")
        or inherited.get("source_type")
        or "ACCOUNT_UPLOAD"
    )
    if values.get("unlimited_materials"):
        maximum = None
    else:
        maximum = values.get("max_materials_per_unit")
    if maximum is None and not values.get("unlimited_materials"):
        maximum = inherited.get("max_materials_per_unit", 5)
    strategy = {
        "source_type": source_type,
        "selection_mode": (
            values.get("selection_mode")
            or inherited.get("selection_mode")
            or "MANUAL"
        ),
        "max_materials_per_unit": int(maximum) if maximum is not None else None,
    }
    if source_type == "CREATOR_AUTHORIZED":
        inherited_filters = inherited.get("creator_filters") or {}
        if values.get("creator_ids") is not None:
            creator_ids = list(values["creator_ids"])
        elif policy.startswith("cross_advertiser"):
            creator_ids = []
        else:
            creator_ids = list(inherited_filters.get("creator_ids") or [])
        strategy["creator_filters"] = {
            "creator_ids": creator_ids,
            "auth_types": list(
                values.get("creator_auth_types")
                or inherited_filters.get("auth_types")
                or ["VIDEO_ITEM"]
            ),
            "authorization_status": "VALID",
            "minimum_remaining_days": int(
                values.get("minimum_remaining_days")
                if values.get("minimum_remaining_days") is not None
                else inherited_filters.get("minimum_remaining_days", 1)
            ),
        }
    error = plan_templates.material_strategy_error(strategy)
    if error:
        raise ValueError(error)
    return strategy


def build_template(config, values, source_name=None):
    templates = config.get("plan_templates") or {}
    source_template = None
    if source_name:
        source = templates.get(source_name)
        if source is None:
            raise ValueError(f"source plan template not found: {source_name}")
        source_template = plan_templates.normalize_template(config, source_name, source)

    bindings = {
        "channel": str(values.get("channel") or (config.get("account") or {}).get("channel") or "marketing"),
        "advertiser_id": str(values["advertiser_id"]),
        "platform": values["platform"],
        "traffic_source": values["traffic_source"],
        "product_id": str(values["product_id"]),
        "product_name": values["product_name"],
    }
    overrides, inherited_copy, inherited_strategy, provenance = prepare_source(
        source_template,
        bindings,
    )
    material_strategy = build_material_strategy(
        values,
        inherited_strategy,
        provenance["policy"],
    )
    source_changed = (
        inherited_strategy
        and inherited_strategy.get("source_type") != material_strategy["source_type"]
    )
    if source_changed and inherited_strategy.get("creator_filters"):
        provenance["cleared_fields"].append("material_strategy.creator_filters")
    elif (
        provenance["policy"].startswith("cross_advertiser")
        and (inherited_strategy.get("creator_filters") or {}).get("creator_ids")
    ):
        provenance["cleared_fields"].append(
            "material_strategy.creator_filters.creator_ids"
        )
    source_name_templates = SOURCE_NAME_TEMPLATES[material_strategy["source_type"]]
    inherited_name_defaults = overrides.get("defaults") or {}
    project_name_template = str(
        values.get("project_name_template")
        or inherited_name_defaults.get("project_name_template")
        or source_name_templates["project_name_template"]
    ).strip()
    promotion_name_template = str(
        values.get("promotion_name_template")
        or inherited_name_defaults.get("promotion_name_template")
        or source_name_templates["promotion_name_template"]
    ).strip()
    if not project_name_template:
        raise ValueError("project_name_template is required")
    if not promotion_name_template:
        raise ValueError("promotion_name_template is required")
    overrides.setdefault("defaults", {}).update({
        "product_name": bindings["product_name"],
        "product_id": bindings["product_id"],
        "project_name_template": project_name_template,
        "promotion_name_template": promotion_name_template,
    })
    for field in ("daily_budget", "roi_goal", "gender", "ages"):
        if values.get(field) is not None:
            overrides["defaults"][field] = copy.deepcopy(values[field])
    if values.get("product_info") is not None:
        overrides["defaults"]["product_info"] = copy.deepcopy(values["product_info"])
    overrides.setdefault("resolved_ids", {})["unique_product_id"] = bindings["product_id"]
    product_image_ids = values.get("product_image_ids")
    if product_image_ids is not None:
        overrides["resolved_ids"]["product_image_ids"] = normalize_product_image_ids(
            product_image_ids
        )

    field_map = {
        "source_name": ("defaults", "source", False),
        "landing_page_url": ("links", "landing_page_url", False),
        "open_url": ("links", "open_url", False),
        "track_url": ("tracking_urls", "track_url", True),
        "action_track_url": ("tracking_urls", "action_track_url", True),
    }
    for value_key, (section, field, as_list) in field_map.items():
        value = values.get(value_key)
        if value:
            overrides.setdefault(section, {})[field] = [value] if as_list else value

    titles = values.get("titles")
    copy_materials = {
        "titles": normalize_titles(titles) if titles else copy.deepcopy(inherited_copy.get("titles") or [])
    }
    if provenance["policy"].endswith("new_product") and not titles:
        copy_materials = {"titles": []}

    name = str(values.get("name") or "").strip()
    if not name:
        raise ValueError("plan template name is required")
    return name, {
        "display_name": name,
        "bindings": bindings,
        "copy_materials": copy_materials,
        "material_strategy": material_strategy,
        "created_from": provenance,
        "overrides": overrides,
    }


def payload_args():
    return argparse.Namespace(
        advertiser_id=None,
        budget=None,
        bid=None,
        roi_goal=None,
        video_id=None,
        material_date="template-preview",
        product_name=None,
        product_id=None,
        project_name=None,
        promotion_name=None,
        project_id=None,
    )


def validate_candidate(config, name, template):
    strategy_error = plan_templates.material_strategy_error(
        template.get("material_strategy")
    )
    if strategy_error:
        return {
            "ready_for_plan_creation": False,
            "template_missing_fields": [f"material_strategy: {strategy_error}"],
            "runtime_missing_fields": [],
        }
    candidate = copy.deepcopy(config)
    candidate.setdefault("plan_templates", {})[name] = copy.deepcopy(template)
    effective = plan_templates.apply(
        candidate,
        name,
        advertiser_id=template["bindings"]["advertiser_id"],
    )
    effective = channels.runtime_config(
        effective,
        channel=template["bindings"].get("channel") or "marketing",
        capability="create",
    )
    missing = [
        field
        for field in sorted(PAYLOAD_REQUIRED_FIELDS)
        if create_plan.contains_unresolved_value(create_plan.get_path(effective, field))
    ]
    if not missing:
        project_payload, promotion_payload = create_plan.build_payloads(effective, payload_args())
        missing.extend(create_plan.missing_fields(
            effective,
            project_payload,
            promotion_payload,
            False,
        ))
    source_type = template["material_strategy"]["source_type"]
    if source_type == "CREATOR_AUTHORIZED":
        missing = [
            "runtime.creator_material_selection" if field == "materials.video_ids" else field
            for field in missing
        ]
    if create_plan.is_missing(create_plan.get_path(effective, "materials.video_ids")):
        missing.append(
            "runtime.creator_material_selection"
            if source_type == "CREATOR_AUTHORIZED"
            else "materials.video_ids"
        )
    if create_plan.is_missing(create_plan.get_path(effective, "api.access_token")):
        missing.append("api.access_token")
    missing = list(dict.fromkeys(missing))
    template_missing = [field for field in missing if field not in RUNTIME_MISSING_FIELDS]
    try:
        normalize_titles((template.get("copy_materials") or {}).get("titles"))
    except ValueError as exc:
        template_missing.append(f"copy_materials.titles: {exc}")
    template_missing = list(dict.fromkeys(template_missing))
    runtime_missing = [field for field in missing if field in RUNTIME_MISSING_FIELDS]
    return {
        "ready_for_plan_creation": not template_missing,
        "template_missing_fields": template_missing,
        "runtime_missing_fields": runtime_missing,
    }


def flatten(data, prefix=""):
    rows = {}
    if isinstance(data, dict):
        for key, value in data.items():
            dotted = f"{prefix}.{key}" if prefix else key
            rows.update(flatten(value, dotted))
    else:
        rows[prefix] = copy.deepcopy(data)
    return rows


def template_diff(source_template, candidate_template):
    source = flatten(source_template or {})
    candidate = flatten(candidate_template)
    changes = []
    for field in sorted(set(source) | set(candidate)):
        if source.get(field) != candidate.get(field):
            changes.append({
                "field": field,
                "before": source.get(field),
                "after": candidate.get(field),
            })
    return changes
