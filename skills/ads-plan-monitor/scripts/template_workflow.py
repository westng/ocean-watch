#!/usr/bin/env python3
import argparse
import copy

import create_plan
import plan_templates


ACCOUNT_FIELDS = {
    "materials.video_ids",
    "materials.video_cover_ids",
    "resolved_ids.event_asset_ids",
    "resolved_ids.landing_page_asset_id",
}
PRODUCT_FIELDS = {
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
RUNTIME_MISSING_FIELDS = {"materials.video_ids", "api.access_token"}
PAYLOAD_REQUIRED_FIELDS = {
    "defaults.operation",
    "defaults.project_name_template",
    "defaults.promotion_name_template",
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


def template_name(platform, traffic_source, product_name, product_id):
    return f"{platform}-{traffic_source}-{product_name}-{product_id}"


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


def delete_path(data, dotted):
    current = data
    parts = dotted.split(".")
    for part in parts[:-1]:
        current = current.get(part)
        if not isinstance(current, dict):
            return False
    return current.pop(parts[-1], None) is not None


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
        return {}, {"titles": []}, {
            "type": "default",
            "template": None,
            "policy": "default",
            "cleared_fields": [],
        }

    overrides = copy.deepcopy(source_template["overrides"])
    copy_materials = copy.deepcopy(source_template["copy_materials"])
    policy = clone_policy(source_template, target_bindings)
    fields_to_clear = set()
    if policy.startswith("cross_advertiser"):
        fields_to_clear.update(ACCOUNT_FIELDS | PRODUCT_FIELDS | LINK_FIELDS)
    elif policy == "same_advertiser_new_product":
        fields_to_clear.update(ACCOUNT_FIELDS | PRODUCT_FIELDS | LINK_FIELDS)

    cleared = sorted(field for field in fields_to_clear if delete_path(overrides, field))
    return overrides, copy_materials, {
        "type": "business_template",
        "template": source_template["name"],
        "policy": policy,
        "cleared_fields": cleared,
    }


def build_template(config, values, source_name=None):
    templates = config.get("plan_templates") or {}
    source_template = None
    if source_name:
        source = templates.get(source_name)
        if source is None:
            raise ValueError(f"source plan template not found: {source_name}")
        source_template = plan_templates.normalize_template(config, source_name, source)

    bindings = {
        "advertiser_id": str(values["advertiser_id"]),
        "platform": values["platform"],
        "traffic_source": values["traffic_source"],
        "product_id": str(values["product_id"]),
        "product_name": values["product_name"],
    }
    overrides, inherited_copy, provenance = prepare_source(source_template, bindings)
    overrides.setdefault("defaults", {}).update({
        "product_name": bindings["product_name"],
        "product_id": bindings["product_id"],
    })
    overrides.setdefault("resolved_ids", {})["unique_product_id"] = bindings["product_id"]

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

    name = values.get("name") or template_name(
        bindings["platform"],
        bindings["traffic_source"],
        bindings["product_name"],
        bindings["product_id"],
    )
    return name, {
        "display_name": name,
        "bindings": bindings,
        "copy_materials": copy_materials,
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
    candidate = copy.deepcopy(config)
    candidate.setdefault("plan_templates", {})[name] = copy.deepcopy(template)
    effective = plan_templates.apply(
        candidate,
        name,
        advertiser_id=template["bindings"]["advertiser_id"],
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
    if create_plan.is_missing(create_plan.get_path(effective, "materials.video_ids")):
        missing.append("materials.video_ids")
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
