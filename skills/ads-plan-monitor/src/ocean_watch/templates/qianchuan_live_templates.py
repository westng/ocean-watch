import copy
import uuid
from decimal import Decimal, InvalidOperation

from ocean_watch.core.data import is_missing
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.templates import business_template_names

SCHEMA_VERSION = 1
SCHEMA_VERSION_KEY = "qianchuan_live_template_schema_version"
DEFAULT_TEMPLATE_KEY = "default_qianchuan_live_template"
TEMPLATES_KEY = "qianchuan_live_templates"
TEMPLATE_TYPE = "QIANCHUAN_LIVE_ALL_DOMAIN"
MATERIAL_SOURCE_TYPE = "LIVE_SMART_SELECTION"
SMART_BID_TYPES = ("SMART_BID_CUSTOM", "SMART_BID_CONSERVATIVE")

DEFAULT_DELIVERY_SETTING = {
    "smart_bid_type": "SMART_BID_CONSERVATIVE",
    "budget": 5000,
    "live_schedule_type": "SCHEDULE_FROM_NOW",
    "daily_delivery_time": 8.5,
}
DEFAULT_CREATIVE_SETTING = {"smart_select_material": True}


def default_template():
    return {
        "template_type": TEMPLATE_TYPE,
        "business_usable": False,
        "bindings": {
            "channel": "qianchuan",
            "advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
            "creator_name": "REPLACE_WITH_CREATOR_NAME",
            "aweme_id": "REPLACE_WITH_AWEME_ID",
        },
        "delivery_setting": copy.deepcopy(DEFAULT_DELIVERY_SETTING),
        "creative_setting": copy.deepcopy(DEFAULT_CREATIVE_SETTING),
        "material_strategy": {
            "source_type": MATERIAL_SOURCE_TYPE,
            "persist_material_ids": False,
        },
    }


def ensure_config(config):
    normalized = copy.deepcopy(config)
    try:
        version = int(normalized.get(SCHEMA_VERSION_KEY) or 1)
    except (TypeError, ValueError) as error:
        raise ConfigurationError(f"{SCHEMA_VERSION_KEY} must be an integer") from error
    if version > SCHEMA_VERSION:
        raise ConfigurationError(
            f"Qianchuan live template schema {version} is newer than supported {SCHEMA_VERSION}"
        )
    normalized[SCHEMA_VERSION_KEY] = SCHEMA_VERSION
    normalized.setdefault(DEFAULT_TEMPLATE_KEY, default_template())
    normalized.setdefault(TEMPLATES_KEY, {})
    return normalized


def positive_id(value, field):
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    return text


def required_text(value, field):
    text = str(value or "").strip()
    if is_missing(text):
        raise ConfigurationError(f"{field} is required")
    return text


def decimal_value(value, field):
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError) as error:
        raise ConfigurationError(f"{field} must be a number") from error
    if not parsed.is_finite() or parsed <= 0 or parsed.as_tuple().exponent < -2:
        raise ConfigurationError(f"{field} must be positive with at most two decimals")
    return float(parsed)


def validate_delivery_setting(value):
    if not isinstance(value, dict):
        raise ConfigurationError("delivery_setting must be an object")
    allowed = {
        "smart_bid_type",
        "roi2_goal",
        "budget",
        "live_schedule_type",
        "start_time",
        "end_time",
        "daily_delivery_time",
        "deep_external_action",
        "qcpx_mode",
    }
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise ConfigurationError("unsupported Qianchuan live delivery fields", {
            "fields": unknown,
        })
    result = copy.deepcopy(value)
    bid_type = result.get("smart_bid_type")
    if bid_type not in SMART_BID_TYPES:
        raise ConfigurationError("invalid live smart_bid_type")
    result["budget"] = decimal_value(result.get("budget"), "delivery_setting.budget")
    if bid_type == "SMART_BID_CUSTOM":
        result["roi2_goal"] = decimal_value(
            result.get("roi2_goal"),
            "delivery_setting.roi2_goal",
        )
        if result.get("daily_delivery_time") is not None:
            raise ConfigurationError(
                "daily_delivery_time is supported only for SMART_BID_CONSERVATIVE"
            )
    elif result.get("roi2_goal") is not None:
        raise ConfigurationError("SMART_BID_CONSERVATIVE must not set roi2_goal")
    duration = result.get("daily_delivery_time")
    if duration is not None:
        try:
            parsed_duration = Decimal(str(duration))
        except InvalidOperation as error:
            raise ConfigurationError("daily_delivery_time must be a number") from error
        if parsed_duration < Decimal("0.5") or parsed_duration > 24:
            raise ConfigurationError("daily_delivery_time must be between 0.5 and 24")
        if parsed_duration % Decimal("0.5"):
            raise ConfigurationError("daily_delivery_time must use 0.5-hour steps")
        result["daily_delivery_time"] = float(parsed_duration)
    if result.get("live_schedule_type") not in {"SCHEDULE_FROM_NOW", "SCHEDULE_START_END"}:
        raise ConfigurationError("invalid live_schedule_type")
    if result.get("deep_external_action") not in {
        None,
        "AD_CONVERT_TYPE_LIVE_PAY_ROI",
        "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
    }:
        raise ConfigurationError("invalid live deep_external_action")
    if result.get("qcpx_mode") not in {None, "QCPX_MODE_OFF", "QCPX_MODE_ON"}:
        raise ConfigurationError("invalid live qcpx_mode")
    return result


def validate_creative_setting(value):
    if not isinstance(value, dict):
        raise ConfigurationError("creative_setting must be an object")
    if set(value) != {"smart_select_material"}:
        raise ConfigurationError("live creative_setting supports only smart_select_material")
    if value.get("smart_select_material") is not True:
        raise ConfigurationError(
            "material-free live templates require smart_select_material true"
        )
    return copy.deepcopy(value)


def display_name(advertiser_id, creator_name, aweme_id):
    return business_template_names.format_qianchuan_live_template_name(
        advertiser_id,
        creator_name,
        aweme_id,
    )


def build_business_template(
    advertiser_id,
    creator_name,
    aweme_id,
    *,
    source=None,
    template_id=None,
    active=True,
):
    advertiser_id = positive_id(advertiser_id, "advertiser_id")
    creator_name = required_text(creator_name, "creator_name")
    aweme_id = positive_id(aweme_id, "aweme_id")
    source = source or default_template()
    identifier = template_id or f"qclt_{uuid.uuid4().hex[:12]}"
    return {
        "template_id": identifier,
        "display_name": display_name(advertiser_id, creator_name, aweme_id),
        "template_type": TEMPLATE_TYPE,
        "status": "active" if active else "inactive",
        "bindings": {
            "channel": "qianchuan",
            "advertiser_id": advertiser_id,
            "creator_name": creator_name,
            "aweme_id": aweme_id,
        },
        "delivery_setting": validate_delivery_setting(
            source.get("delivery_setting") or DEFAULT_DELIVERY_SETTING
        ),
        "creative_setting": validate_creative_setting(
            source.get("creative_setting") or DEFAULT_CREATIVE_SETTING
        ),
        "material_strategy": {
            "source_type": MATERIAL_SOURCE_TYPE,
            "persist_material_ids": False,
        },
    }


def validate_default_template(template):
    if not isinstance(template, dict):
        raise ConfigurationError("Qianchuan live default template must be an object")
    if template.get("template_type") != TEMPLATE_TYPE:
        raise ConfigurationError("invalid Qianchuan live default template type")
    if template.get("business_usable") is not False:
        raise ConfigurationError("Qianchuan live default template cannot be business usable")
    bindings = template.get("bindings")
    if not isinstance(bindings, dict) or bindings.get("channel") != "qianchuan":
        raise ConfigurationError("Qianchuan live default template channel must be qianchuan")
    for field in ("advertiser_id", "creator_name", "aweme_id"):
        if not is_missing(bindings.get(field)):
            raise ConfigurationError(f"Qianchuan live default template must not bind {field}")
    validate_delivery_setting(template.get("delivery_setting"))
    validate_creative_setting(template.get("creative_setting"))
    if template.get("material_strategy") != {
        "source_type": MATERIAL_SOURCE_TYPE,
        "persist_material_ids": False,
    }:
        raise ConfigurationError("invalid Qianchuan live default material strategy")
    return copy.deepcopy(template)


def validate_business_template(template):
    if not isinstance(template, dict) or template.get("template_type") != TEMPLATE_TYPE:
        raise ConfigurationError("invalid Qianchuan live template type")
    bindings = template.get("bindings") or {}
    if bindings.get("channel") != "qianchuan":
        raise ConfigurationError("Qianchuan live template channel must be qianchuan")
    normalized = build_business_template(
        bindings.get("advertiser_id"),
        bindings.get("creator_name"),
        bindings.get("aweme_id"),
        source=template,
        template_id=required_text(template.get("template_id"), "template_id"),
        active=template.get("status") == "active",
    )
    if template.get("display_name") != normalized["display_name"]:
        raise ConfigurationError("Qianchuan live template display_name is inconsistent")
    strategy = template.get("material_strategy") or {}
    if strategy != {
        "source_type": MATERIAL_SOURCE_TYPE,
        "persist_material_ids": False,
    }:
        raise ConfigurationError("invalid Qianchuan live material strategy")
    return normalized


def list_templates(config):
    normalized = ensure_config(config)
    rows = []
    for template_id, raw in sorted((normalized.get(TEMPLATES_KEY) or {}).items()):
        template = validate_business_template(raw)
        if template["template_id"] != template_id:
            raise ConfigurationError("Qianchuan live template key does not match template_id")
        bindings = template["bindings"]
        rows.append({
            "template_id": template_id,
            "name": template["display_name"],
            "status": template["status"],
            "advertiser_id": bindings["advertiser_id"],
            "creator_name": bindings["creator_name"],
            "aweme_id": bindings["aweme_id"],
            "template_type": "直播全域",
        })
    return rows


def resolve_template(config, selector):
    normalized = ensure_config(config)
    templates = normalized.get(TEMPLATES_KEY) or {}
    if selector in templates:
        return validate_business_template(templates[selector])
    matches = [
        validate_business_template(template)
        for template in templates.values()
        if template.get("display_name") == selector
    ]
    if not matches:
        raise ConfigurationError("Qianchuan live template not found", {"selector": selector})
    if len(matches) > 1:
        raise ConfigurationError("Qianchuan live template name is ambiguous; use template_id")
    return matches[0]


def payload_from_template(template):
    template = validate_business_template(template)
    bindings = template["bindings"]
    return {
        "advertiser_id": int(bindings["advertiser_id"]),
        "aweme_id": int(bindings["aweme_id"]),
        "marketing_goal": "LIVE_PROM_GOODS",
        "delivery_setting": copy.deepcopy(template["delivery_setting"]),
        "creative_setting": copy.deepcopy(template["creative_setting"]),
    }
