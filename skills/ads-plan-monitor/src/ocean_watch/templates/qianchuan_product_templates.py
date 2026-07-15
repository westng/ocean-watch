import copy
import re
import uuid
from decimal import Decimal, InvalidOperation

from ocean_watch.core.data import is_missing
from ocean_watch.core.errors import ConfigurationError

SCHEMA_VERSION = 2
MAX_PRODUCTS = 30
TEMPLATE_TYPE = "QIANCHUAN_PRODUCT_ALL_DOMAIN"
MATERIAL_SOURCE_TYPE = "CREATOR_RUNTIME_QUERY"
DEFAULT_TEMPLATE_KEY = "default_qianchuan_product_template"
TEMPLATES_KEY = "qianchuan_product_templates"
ACTIVE_TEMPLATE_KEY = "active_qianchuan_product_template"
SCHEMA_VERSION_KEY = "qianchuan_product_template_schema_version"

DEFAULT_DELIVERY_SETTING = {
    "smart_bid_type": "SMART_BID_CUSTOM",
    "roi2_goal": 1.7,
    "qcpx_mode": "QCPX_MODE_ON",
    "budget": 5000,
    "video_schedule_type": "SCHEDULE_FROM_NOW",
    "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
}


def default_template():
    return {
        "template_type": TEMPLATE_TYPE,
        "business_usable": False,
        "bindings": {
            "channel": "qianchuan",
            "advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
            "product_name": "REPLACE_WITH_PRODUCT_NAME",
            "product_ids": [],
        },
        "delivery_setting": copy.deepcopy(DEFAULT_DELIVERY_SETTING),
        "material_strategy": {
            "source_type": MATERIAL_SOURCE_TYPE,
            "persist_material_ids": False,
        },
    }


def ensure_config(config):
    normalized = copy.deepcopy(config)
    current_version = int(normalized.get(SCHEMA_VERSION_KEY) or 1)
    if current_version < 2:
        normalized[DEFAULT_TEMPLATE_KEY] = default_template()
        for template in (normalized.get(TEMPLATES_KEY) or {}).values():
            bindings = template.get("bindings") or {}
            bindings.pop("shop_name", None)
            product_ids = bindings.get("product_ids") or []
            if bindings.get("advertiser_id") and bindings.get("product_name") and product_ids:
                template["display_name"] = display_name(
                    str(bindings["advertiser_id"]),
                    str(bindings["product_name"]),
                    [str(value) for value in product_ids],
                )
    normalized[SCHEMA_VERSION_KEY] = SCHEMA_VERSION
    normalized.setdefault(DEFAULT_TEMPLATE_KEY, default_template())
    normalized.setdefault(TEMPLATES_KEY, {})
    normalized.setdefault(ACTIVE_TEMPLATE_KEY, None)
    return normalized


def normalize_positive_id(value, field):
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    return text


def split_product_ids(values):
    if isinstance(values, str):
        values = [values]
    parts = []
    for value in values or []:
        parts.extend(re.split(r"[/,，\s]+", str(value)))
    return [part.strip() for part in parts if part.strip()]


def normalize_product_ids(values):
    result = []
    seen = set()
    for index, value in enumerate(split_product_ids(values)):
        product_id = normalize_positive_id(value, f"product_ids[{index}]")
        if product_id not in seen:
            seen.add(product_id)
            result.append(product_id)
    if not result:
        raise ConfigurationError("product_ids must contain at least one product")
    if len(result) > MAX_PRODUCTS:
        raise ConfigurationError(
            f"product_ids supports at most {MAX_PRODUCTS} products",
            {"product_count": len(result)},
        )
    return result


def required_text(value, field):
    text = str(value or "").strip()
    if is_missing(text):
        raise ConfigurationError(f"{field} is required")
    return text


def display_name(advertiser_id, product_name, product_ids):
    return f"{advertiser_id}-商品全域-{product_name}-{'/'.join(product_ids)}"


def validate_delivery_setting(setting):
    if not isinstance(setting, dict):
        raise ConfigurationError("delivery_setting must be an object")
    expected = DEFAULT_DELIVERY_SETTING
    unknown = sorted(set(setting) - set(expected))
    if unknown:
        raise ConfigurationError(
            "Qianchuan product template contains unsupported delivery fields",
            {"fields": unknown},
        )
    missing = [key for key in expected if setting.get(key) is None]
    if missing:
        raise ConfigurationError(
            "Qianchuan product template is missing delivery fields",
            {"fields": missing},
        )
    if setting.get("smart_bid_type") != "SMART_BID_CUSTOM":
        raise ConfigurationError("smart_bid_type must be SMART_BID_CUSTOM")
    for field in ("roi2_goal", "budget"):
        try:
            value = Decimal(str(setting.get(field)))
        except (InvalidOperation, ValueError):
            raise ConfigurationError(f"delivery_setting.{field} must be a number") from None
        if not value.is_finite() or value <= 0:
            raise ConfigurationError(f"delivery_setting.{field} must be greater than zero")
        if value.as_tuple().exponent < -2:
            raise ConfigurationError(
                f"delivery_setting.{field} supports at most two decimal places"
            )
    if setting.get("qcpx_mode") != "QCPX_MODE_ON":
        raise ConfigurationError("qcpx_mode must be QCPX_MODE_ON")
    if setting.get("video_schedule_type") != "SCHEDULE_FROM_NOW":
        raise ConfigurationError("video_schedule_type must be SCHEDULE_FROM_NOW")
    if setting.get("deep_external_action") != "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI":
        raise ConfigurationError(
            "deep_external_action must be AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI"
        )
    return copy.deepcopy(setting)


def contains_forbidden_key(value):
    forbidden = {
        "aweme_id",
        "aweme_item_id",
        "channel_id",
        "channel_type",
        "image_ids",
        "multi_product_creative_list",
        "product_channel_info",
        "video_id",
    }
    if isinstance(value, dict):
        return next(
            (
                key
                for key, nested in value.items()
                if key in forbidden or contains_forbidden_key(nested)
            ),
            None,
        )
    if isinstance(value, list):
        return next((key for item in value if (key := contains_forbidden_key(item))), None)
    return None


def build_business_template(
    advertiser_id,
    product_name,
    product_ids,
    source=None,
    template_id=None,
    active=True,
):
    advertiser_id = normalize_positive_id(advertiser_id, "advertiser_id")
    product_name = required_text(product_name, "product_name")
    product_ids = normalize_product_ids(product_ids)
    source = source or default_template()
    delivery = validate_delivery_setting(
        source.get("delivery_setting") or DEFAULT_DELIVERY_SETTING
    )
    identifier = template_id or f"qcpt_{uuid.uuid4().hex[:12]}"
    return {
        "template_id": identifier,
        "display_name": display_name(advertiser_id, product_name, product_ids),
        "template_type": TEMPLATE_TYPE,
        "status": "active" if active else "inactive",
        "bindings": {
            "channel": "qianchuan",
            "advertiser_id": advertiser_id,
            "product_name": product_name,
            "product_ids": product_ids,
        },
        "delivery_setting": delivery,
        "material_strategy": {
            "source_type": MATERIAL_SOURCE_TYPE,
            "persist_material_ids": False,
        },
    }


def validate_business_template(template):
    if not isinstance(template, dict):
        raise ConfigurationError("Qianchuan product template must be an object")
    if template.get("template_type") != TEMPLATE_TYPE:
        raise ConfigurationError("invalid Qianchuan product template type")
    bindings = template.get("bindings") or {}
    if bindings.get("channel") != "qianchuan":
        raise ConfigurationError("Qianchuan product template channel must be qianchuan")
    normalized = build_business_template(
        advertiser_id=bindings.get("advertiser_id"),
        product_name=bindings.get("product_name"),
        product_ids=bindings.get("product_ids"),
        source=template,
        template_id=required_text(template.get("template_id"), "template_id"),
        active=template.get("status") == "active",
    )
    if template.get("display_name") != normalized["display_name"]:
        raise ConfigurationError("Qianchuan product template display_name is inconsistent")
    strategy = template.get("material_strategy") or {}
    if strategy.get("source_type") != MATERIAL_SOURCE_TYPE:
        raise ConfigurationError("invalid Qianchuan product material strategy")
    if strategy.get("persist_material_ids") is not False:
        raise ConfigurationError("Qianchuan product templates cannot persist material IDs")
    forbidden = contains_forbidden_key(template)
    if forbidden:
        raise ConfigurationError(
            "Qianchuan product template contains runtime or unsupported fields",
            {"field": forbidden},
        )
    return normalized


def list_templates(config):
    config = ensure_config(config)
    rows = []
    active_id = config.get(ACTIVE_TEMPLATE_KEY)
    for template_id, raw in sorted((config.get(TEMPLATES_KEY) or {}).items()):
        template = validate_business_template(raw)
        if template["template_id"] != template_id:
            raise ConfigurationError(
                "Qianchuan product template key does not match template_id",
                {"key": template_id, "template_id": template["template_id"]},
            )
        bindings = template["bindings"]
        rows.append({
            "template_id": template_id,
            "name": template["display_name"],
            "active": template_id == active_id,
            "status": template["status"],
            "advertiser_id": bindings["advertiser_id"],
            "product_name": bindings["product_name"],
            "product_ids": bindings["product_ids"],
            "product_count": len(bindings["product_ids"]),
            "material_source_type": MATERIAL_SOURCE_TYPE,
        })
    return rows


def resolve_template(config, selector=None):
    config = ensure_config(config)
    templates = config.get(TEMPLATES_KEY) or {}
    selector = selector or config.get(ACTIVE_TEMPLATE_KEY)
    if is_missing(selector):
        raise ConfigurationError("no active Qianchuan product template")
    if selector in templates:
        return validate_business_template(templates[selector])
    matches = [
        validate_business_template(template)
        for template in templates.values()
        if template.get("display_name") == selector
    ]
    if not matches:
        raise ConfigurationError(
            "Qianchuan product template not found",
            {"selector": selector},
        )
    if len(matches) > 1:
        raise ConfigurationError(
            "Qianchuan product template name is ambiguous; use template_id",
            {"selector": selector},
        )
    return matches[0]


def payload_from_template(template, name=None):
    template = validate_business_template(template)
    bindings = template["bindings"]
    payload = {
        "advertiser_id": int(bindings["advertiser_id"]),
        "marketing_goal": "VIDEO_PROM_GOODS",
        "product_ids": [int(value) for value in bindings["product_ids"]],
        "delivery_setting": copy.deepcopy(template["delivery_setting"]),
    }
    if name:
        payload["name"] = str(name)
    return payload
