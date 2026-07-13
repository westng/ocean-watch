#!/usr/bin/env python3
import copy


SCHEMA_VERSION = 2
TEMPLATE_SECTIONS = ("defaults", "materials", "resolved_ids", "links", "tracking_urls")
REQUIRED_BINDINGS = ("advertiser_id", "platform", "traffic_source", "product_id", "product_name")


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith("REPLACE_WITH")
    return False


def deep_merge(base, override):
    if not isinstance(base, dict) or not isinstance(override, dict):
        return copy.deepcopy(override)
    result = copy.deepcopy(base)
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


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


def legacy_bindings(config, template):
    defaults = template.get("defaults") or {}
    return {
        "advertiser_id": (config.get("account") or {}).get("advertiser_id"),
        "platform": template.get("platform"),
        "traffic_source": template.get("traffic_source"),
        "product_id": template.get("product_id") or defaults.get("product_id"),
        "product_name": defaults.get("product_name") or template.get("product_label"),
    }


def normalize_template(config, name, template):
    if "bindings" in template or "overrides" in template:
        return {
            "name": name,
            "display_name": template.get("display_name", name),
            "bindings": copy.deepcopy(template.get("bindings") or {}),
            "overrides": copy.deepcopy(template.get("overrides") or {}),
            "legacy": False,
        }
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
        "overrides": overrides,
        "legacy": True,
    }


def default_bundle(config):
    configured = config.get("default_plan_template")
    return copy.deepcopy(configured) if isinstance(configured, dict) else section_bundle(config)


def apply(config, template_name=None, advertiser_id=None, require_template=None):
    effective = copy.deepcopy(config)
    templates = config.get("plan_templates") or {}
    selected = template_name or config.get("active_plan_template")
    schema_v2 = int(config.get("plan_template_schema_version") or 1) >= SCHEMA_VERSION
    require_template = schema_v2 if require_template is None else require_template

    base = default_bundle(config)
    for section in TEMPLATE_SECTIONS:
        effective[section] = copy.deepcopy(base.get(section) or {})
    effective["titles"] = copy.deepcopy(base.get("titles") or [])

    if not selected:
        if require_template:
            raise ValueError(
                "no business plan template selected; the default template cannot create plans"
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

    bound_advertiser_id = bindings.get("advertiser_id")
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
    if "titles" in overrides:
        effective["titles"] = copy.deepcopy(overrides["titles"])

    if not is_missing(bound_advertiser_id):
        effective.setdefault("account", {})["advertiser_id"] = bound_advertiser_id
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
        "legacy": template["legacy"],
    }
    return effective


def migrate(config):
    migrated = copy.deepcopy(config)
    base = default_bundle(config)
    templates = {}
    for name, raw_template in (config.get("plan_templates") or {}).items():
        normalized = normalize_template(config, name, raw_template)
        effective = copy.deepcopy(base)
        for section in TEMPLATE_SECTIONS:
            if section in normalized["overrides"]:
                effective[section] = deep_merge(
                    effective.get(section, {}),
                    normalized["overrides"][section] or {},
                )
        if "titles" in normalized["overrides"]:
            effective["titles"] = copy.deepcopy(normalized["overrides"]["titles"])
        overrides = deep_diff(base, effective) or {}
        templates[name] = {
            "display_name": normalized["display_name"],
            "bindings": normalized["bindings"],
            "overrides": overrides,
        }

    migrated["plan_template_schema_version"] = SCHEMA_VERSION
    migrated["default_plan_template"] = base
    migrated["plan_templates"] = templates
    for section in TEMPLATE_SECTIONS:
        migrated.pop(section, None)
    migrated.pop("titles", None)
    return migrated
