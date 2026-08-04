#!/usr/bin/env python3
import copy

from ocean_watch.core.data import deep_merge
from ocean_watch.templates import business_template_names

SCHEMA_VERSION = 6
MATERIAL_STRATEGY_SCHEMA_VERSION = 3
NAMING_SCHEMA_VERSION = 4
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")
REQUIRED_BINDINGS = ("channel", "advertiser_id", "platform", "traffic_source", "product_id", "product_name")
SHARED_RESOLVED_ID_FIELDS = ("city_ids", "city_names")
PRODUCT_DEFAULT_FIELDS = ("product_name", "product_id", "source")
MATERIAL_SOURCE_TYPES = ("ACCOUNT_UPLOAD", "CREATOR_AUTHORIZED")
MATERIAL_SELECTION_MODES = ("MANUAL", "LATEST")
DYNAMIC_MATERIAL_FIELDS = ("video_ids", "video_cover_ids")
TEMPLATE_MIGRATION_CORE_FIELDS = {
    "display_name",
    "bindings",
    "copy_materials",
    "material_strategy",
    "created_from",
    "overrides",
    "platform",
    "traffic_source",
    "product_label",
    "product_id",
    "defaults",
    "materials",
    "resolved_ids",
    "links",
    "tracking_urls",
    "titles",
}


class LegacyMaterialSelectionError(ValueError):
    def __init__(self, templates):
        self.templates = templates
        super().__init__(
            "legacy templates contain fixed video IDs; rerun migration with explicit confirmation: "
            + ", ".join(templates)
        )


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith("REPLACE_WITH")
    return False


def deep_diff(base, value):
    if not isinstance(base, dict) or not isinstance(value, dict):
        return copy.deepcopy(value) if base != value else None
    result = {}
    for key, item in value.items():
        if key not in base:
            result[key] = copy.deepcopy(item)
            continue
        difference = deep_diff(base[key], item)
        if difference is not None:
            result[key] = difference
    return result or None


def section_bundle(config):
    bundle = {
        section: copy.deepcopy(config.get(section) or {})
        for section in TEMPLATE_SECTIONS
    }
    bundle["titles"] = copy.deepcopy(config.get("titles") or [])
    return bundle


def binding_error(bindings):
    missing = [field for field in REQUIRED_BINDINGS if is_missing(bindings.get(field))]
    if missing:
        return "template bindings missing: " + ", ".join(missing)
    return None


def material_strategy_error(strategy):
    if not isinstance(strategy, dict):
        return "template material_strategy must be an object"
    source_type = strategy.get("source_type")
    if source_type not in MATERIAL_SOURCE_TYPES:
        return "template material_strategy.source_type must be ACCOUNT_UPLOAD or CREATOR_AUTHORIZED"
    selection_mode = strategy.get("selection_mode")
    if selection_mode not in MATERIAL_SELECTION_MODES:
        return "template material_strategy.selection_mode must be MANUAL or LATEST"
    if source_type == "CREATOR_AUTHORIZED":
        maximum = strategy.get("max_materials_per_unit")
        if maximum is not None:
            try:
                maximum = int(maximum)
            except (TypeError, ValueError):
                return "creator material max_materials_per_unit must be a positive integer or null"
            if maximum < 1:
                return "creator material max_materials_per_unit must be a positive integer or null"
        filters = strategy.get("creator_filters")
        if not isinstance(filters, dict):
            return "creator material template requires material_strategy.creator_filters"
        if filters.get("authorization_status") != "VALID":
            return "creator material template requires authorization_status VALID"
        try:
            remaining = int(filters.get("minimum_remaining_days"))
        except (TypeError, ValueError):
            return "creator material minimum_remaining_days must be an integer"
        if remaining < 0:
            return "creator material minimum_remaining_days must be non-negative"
        creator_ids = filters.get("creator_ids") or []
        if not isinstance(creator_ids, list) or any(
            not str(value).isdigit() for value in creator_ids
        ):
            return "creator material creator_ids must be decimal string values"
        auth_types = filters.get("auth_types") or []
        if auth_types != ["VIDEO_ITEM"]:
            return "creator material auth_types currently supports only VIDEO_ITEM"
    else:
        try:
            maximum = int(strategy.get("max_materials_per_unit"))
        except (TypeError, ValueError):
            return "account-upload max_materials_per_unit must be an integer"
        if maximum < 1:
            return "account-upload max_materials_per_unit must be positive"
        if strategy.get("creator_filters"):
            return "account-upload material strategy must not contain creator_filters"
    return None


def fixed_material_fields(template, *, require_value=False):
    materials = (template.get("overrides") or {}).get("materials") or {}
    return [
        f"overrides.materials.{field}"
        for field in DYNAMIC_MATERIAL_FIELDS
        if field in materials and (not require_value or bool(materials.get(field)))
    ]


def legacy_material_strategy(config, template=None):
    template = template or {}
    overrides = template.get("overrides") or {}
    maximum = (overrides.get("defaults") or {}).get("max_videos_per_project")
    if maximum is None:
        maximum = ((config.get("default_plan_template") or {}).get("defaults") or {}).get(
            "max_videos_per_project"
        )
    if maximum is None:
        maximum = (config.get("defaults") or {}).get("max_videos_per_project", 5)
    return {
        "source_type": "ACCOUNT_UPLOAD",
        "selection_mode": "MANUAL",
        "max_materials_per_unit": int(maximum or 5),
    }


def legacy_bindings(config, template):
    defaults = template.get("defaults") or {}
    return {
        "channel": (config.get("account") or {}).get("channel") or "marketing",
        "advertiser_id": (config.get("account") or {}).get("advertiser_id"),
        "platform": template.get("platform"),
        "traffic_source": template.get("traffic_source"),
        "product_id": template.get("product_id") or defaults.get("product_id"),
        "product_name": defaults.get("product_name") or template.get("product_label"),
    }


def normalize_template(config, name, template):
    schema_version = int(config.get("plan_template_schema_version") or 1)
    if "bindings" in template or "overrides" in template:
        overrides = copy.deepcopy(template.get("overrides") or {})
        legacy_titles = overrides.pop("titles", None)
        copy_materials = copy.deepcopy(template.get("copy_materials") or {})
        if legacy_titles is not None and "titles" not in copy_materials:
            copy_materials["titles"] = legacy_titles
        bindings = copy.deepcopy(template.get("bindings") or {})
        bindings.setdefault(
            "channel",
            (config.get("account") or {}).get("channel")
            or config.get("default_channel")
            or "marketing",
        )
        normalized = {
            "name": name,
            "display_name": template.get("display_name", name),
            "bindings": bindings,
            "copy_materials": copy_materials,
            "material_strategy": copy.deepcopy(template.get("material_strategy") or {}),
            "created_from": copy.deepcopy(template.get("created_from")),
            "overrides": overrides,
            "legacy": False,
        }
        if (
            not normalized["material_strategy"]
            and schema_version < MATERIAL_STRATEGY_SCHEMA_VERSION
        ):
            normalized["material_strategy"] = legacy_material_strategy(config, template)
        return normalized
    overrides = {
        section: copy.deepcopy(template[section])
        for section in TEMPLATE_SECTIONS
        if section in template
    }
    if "titles" in template:
        overrides["titles"] = copy.deepcopy(template["titles"])
    return {
        "name": name,
        "display_name": template.get("display_name", name),
        "bindings": legacy_bindings(config, template),
        "copy_materials": {"titles": copy.deepcopy(template.get("titles") or [])},
        "material_strategy": legacy_material_strategy(config, {"overrides": overrides}),
        "created_from": copy.deepcopy(template.get("created_from")),
        "overrides": overrides,
        "legacy": True,
    }


def default_bundle(config):
    configured = config.get("default_plan_template")
    return copy.deepcopy(configured) if isinstance(configured, dict) else section_bundle(config)


def shared_default_bundle(bundle):
    shared = copy.deepcopy(bundle)
    defaults = shared.setdefault("defaults", {})
    for field in PRODUCT_DEFAULT_FIELDS:
        defaults.pop(field, None)
    resolved_ids = shared.get("resolved_ids") or {}
    shared["resolved_ids"] = {
        field: copy.deepcopy(resolved_ids[field])
        for field in SHARED_RESOLVED_ID_FIELDS
        if field in resolved_ids
    }
    shared["materials"] = {}
    shared["links"] = {}
    shared["tracking_urls"] = {}
    shared["titles"] = []
    return shared


def apply(config, template_name=None, advertiser_id=None, require_template=None, channel=None):
    effective = copy.deepcopy(config)
    templates = config.get("plan_templates") or {}
    selected = template_name
    schema_version = int(config.get("plan_template_schema_version") or 1)
    schema_v2 = schema_version >= 2
    schema_v3 = schema_version >= MATERIAL_STRATEGY_SCHEMA_VERSION
    require_template = True if require_template is None else require_template

    base = default_bundle(config)
    for section in TEMPLATE_SECTIONS:
        effective[section] = copy.deepcopy(base.get(section) or {})
    effective["titles"] = copy.deepcopy(base.get("titles") or [])

    if not selected:
        if require_template:
            raise ValueError(
                "no business plan template selected; pass an explicit plan template"
            )
        effective["_selected_plan_template"] = None
        return effective
    if selected not in templates:
        available = ", ".join(sorted(templates)) or "<none>"
        raise ValueError(f"unknown plan template: {selected}; available: {available}")

    template = normalize_template(config, selected, templates[selected])
    bindings = template["bindings"]
    error = binding_error(bindings)
    if schema_v2 and error:
        raise ValueError(error)
    strategy = template["material_strategy"]
    strategy_error = material_strategy_error(strategy)
    if schema_v3 and strategy_error:
        raise ValueError(strategy_error)
    fixed_fields = fixed_material_fields(template)
    if schema_v3 and fixed_fields:
        raise ValueError(
            "plan templates cannot store runtime material IDs: "
            + ", ".join(fixed_fields)
        )

    bound_advertiser_id = bindings.get("advertiser_id")
    bound_channel = bindings.get("channel") or "marketing"
    requested_channel = channel or (config.get("account") or {}).get("channel") or config.get("default_channel") or "marketing"
    if str(bound_channel) != str(requested_channel):
        raise ValueError(
            f"plan template {selected} is bound to channel {bound_channel}, "
            f"not channel {requested_channel}"
        )
    requested_advertiser_id = advertiser_id or (config.get("account") or {}).get("advertiser_id")
    if not is_missing(bound_advertiser_id) and not is_missing(requested_advertiser_id):
        if str(bound_advertiser_id) != str(requested_advertiser_id):
            raise ValueError(
                f"plan template {selected} is bound to advertiser {bound_advertiser_id}, "
                f"not advertiser {requested_advertiser_id}"
            )

    overrides = template["overrides"]
    for section in TEMPLATE_SECTIONS:
        if section in overrides:
            effective[section] = deep_merge(effective.get(section, {}), overrides[section] or {})
    effective["titles"] = copy.deepcopy(template["copy_materials"].get("titles") or [])
    effective["material_strategy"] = copy.deepcopy(strategy)

    if not is_missing(bound_advertiser_id):
        effective.setdefault("account", {})["advertiser_id"] = bound_advertiser_id
    effective.setdefault("account", {})["channel"] = bound_channel
    if not is_missing(bindings.get("product_name")):
        effective.setdefault("defaults", {})["product_name"] = bindings["product_name"]
    if not is_missing(bindings.get("product_id")):
        effective.setdefault("defaults", {})["product_id"] = bindings["product_id"]
        resolved_product_id = (effective.get("resolved_ids") or {}).get("unique_product_id")
        if not is_missing(resolved_product_id) and str(resolved_product_id) != str(bindings["product_id"]):
            raise ValueError(
                f"plan template {selected} binds product {bindings['product_id']}, "
                f"but resolved_ids.unique_product_id is {resolved_product_id}"
            )

    effective["_selected_plan_template"] = {
        "name": selected,
        "display_name": template["display_name"],
        "bindings": copy.deepcopy(bindings),
        "material_strategy": copy.deepcopy(strategy),
        "legacy": template["legacy"],
    }
    return effective


def canonical_template_name(template):
    return business_template_names.format_marketing_template_name(
        template["bindings"].get("advertiser_id"),
        template["bindings"].get("product_name"),
        template["bindings"].get("product_id"),
        template["material_strategy"].get("source_type"),
    )


def migrate_names(config):
    templates = config.get("plan_templates") or {}
    name_map = {}
    canonical_owners = {}
    for old_name, raw_template in templates.items():
        normalized = normalize_template(config, old_name, raw_template)
        new_name = canonical_template_name(normalized)
        if new_name in canonical_owners and canonical_owners[new_name] != old_name:
            raise ValueError(
                "plan template naming collision after schema v4 migration: "
                f"{canonical_owners[new_name]}, {old_name} -> {new_name}"
            )
        name_map[old_name] = new_name
        canonical_owners[new_name] = old_name

    migrated = copy.deepcopy(config)
    renamed = {}
    for old_name, raw_template in templates.items():
        new_name = name_map[old_name]
        template = copy.deepcopy(raw_template)
        template["display_name"] = new_name
        created_from = template.get("created_from")
        if isinstance(created_from, dict) and created_from.get("template") in name_map:
            created_from["template"] = name_map[created_from["template"]]
        copy_materials = template.get("copy_materials")
        if (
            isinstance(copy_materials, dict)
            and copy_materials.get("copied_from_template") in name_map
        ):
            copy_materials["copied_from_template"] = name_map[
                copy_materials["copied_from_template"]
            ]
        renamed[new_name] = template
    migrated["plan_templates"] = renamed
    migrated["plan_template_schema_version"] = NAMING_SCHEMA_VERSION
    return migrated


def migrate(config, confirm_remove_legacy_materials=False):
    try:
        input_version = int(config.get("plan_template_schema_version") or 1)
    except (TypeError, ValueError) as exc:
        raise ValueError("plan_template_schema_version must be an integer") from exc
    if input_version > SCHEMA_VERSION:
        raise ValueError(
            f"plan template schema {input_version} is newer than supported {SCHEMA_VERSION}"
        )
    if input_version == SCHEMA_VERSION:
        validated = copy.deepcopy(config)
        if "active_plan_template" in validated:
            raise ValueError(
                "schema v6 does not support active_plan_template; migrate the config"
            )
        for name, raw_template in (validated.get("plan_templates") or {}).items():
            normalized = normalize_template(validated, name, raw_template)
            error = binding_error(normalized["bindings"]) or material_strategy_error(
                normalized["material_strategy"]
            )
            if error:
                raise ValueError(f"invalid plan template {name}: {error}")
            fixed_fields = fixed_material_fields(normalized)
            if fixed_fields:
                raise ValueError(
                    f"invalid plan template {name}: plan templates cannot store "
                    "runtime material IDs: " + ", ".join(fixed_fields)
                )
            if is_missing(name) or normalized["display_name"] != name:
                raise ValueError(
                    f"invalid plan template {name}: display_name must match the template key"
                )
        return validated

    if input_version == 5:
        migrated = copy.deepcopy(config)
        migrated.pop("active_plan_template", None)
        migrated["plan_template_schema_version"] = SCHEMA_VERSION
        return migrate(migrated, confirm_remove_legacy_materials)

    if input_version == NAMING_SCHEMA_VERSION:
        migrated = copy.deepcopy(config)
        migrated.pop("active_plan_template", None)
        migrated["plan_template_schema_version"] = SCHEMA_VERSION
        return migrate(migrated, confirm_remove_legacy_materials)

    if input_version >= MATERIAL_STRATEGY_SCHEMA_VERSION:
        return migrate(migrate_names(config), confirm_remove_legacy_materials)

    migrated = copy.deepcopy(config)
    original_base = default_bundle(config)
    base = shared_default_bundle(original_base)
    templates = {}
    legacy_material_templates = []
    for name, raw_template in (config.get("plan_templates") or {}).items():
        normalized = normalize_template(config, name, raw_template)
        normalized["bindings"].setdefault(
            "channel",
            (config.get("account") or {}).get("channel") or "marketing",
        )
        effective = copy.deepcopy(original_base)
        for section in TEMPLATE_SECTIONS:
            if section in normalized["overrides"]:
                effective[section] = deep_merge(
                    effective.get(section, {}),
                    normalized["overrides"][section] or {},
                )
        effective["titles"] = copy.deepcopy(
            normalized["copy_materials"].get("titles") or []
        )
        dynamic_materials = effective.get("materials") or {}
        if any(dynamic_materials.get(field) for field in DYNAMIC_MATERIAL_FIELDS):
            legacy_material_templates.append(name)
        for field in DYNAMIC_MATERIAL_FIELDS:
            dynamic_materials.pop(field, None)
        effective["materials"] = dynamic_materials
        overrides = deep_diff(base, effective) or {}
        overrides.pop("titles", None)
        maximum = (effective.get("defaults") or {}).get("max_videos_per_project", 5)
        template_result = {
            "display_name": normalized["display_name"],
            "bindings": normalized["bindings"],
            "copy_materials": normalized["copy_materials"],
            "material_strategy": {
                "source_type": "ACCOUNT_UPLOAD",
                "selection_mode": "MANUAL",
                "max_materials_per_unit": int(maximum or 5),
            },
            "overrides": overrides,
        }
        if normalized.get("created_from") is not None:
            template_result["created_from"] = normalized["created_from"]
        for key, value in raw_template.items():
            if key not in TEMPLATE_MIGRATION_CORE_FIELDS:
                template_result[key] = copy.deepcopy(value)
        templates[name] = template_result

    if legacy_material_templates and not confirm_remove_legacy_materials:
        raise LegacyMaterialSelectionError(sorted(legacy_material_templates))

    migrated["plan_template_schema_version"] = MATERIAL_STRATEGY_SCHEMA_VERSION
    migrated["default_plan_template"] = base
    migrated["plan_templates"] = templates
    for section in TEMPLATE_SECTIONS:
        migrated.pop(section, None)
    migrated.pop("titles", None)
    return migrate(migrated)
