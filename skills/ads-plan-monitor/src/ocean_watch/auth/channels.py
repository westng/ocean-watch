#!/usr/bin/env python3
import copy

CONFIG_SCHEMA_VERSION = 2
DEFAULT_CHANNEL = "marketing"
LEGACY_API_CREDENTIAL_KEYS = {
    "app_id",
    "secret",
    "access_token",
    "refresh_token",
    "access_token_expires_at",
    "refresh_token_expires_at",
    "last_token_update_at",
    "last_authorized_account_sync_at",
    "oauth_authorized_accounts",
    "authorized_advertiser_ids",
}
CHANNELS = {
    "marketing": {
        "display_name": "巨量营销",
        "oauth_state_code": "AD",
        "implemented": True,
        "capabilities": {"oauth", "accounts", "create", "query", "report"},
    },
    "qianchuan": {
        "display_name": "巨量千川",
        "oauth_state_code": "QC",
        "implemented": False,
        "capabilities": set(),
    },
}


class ChannelError(ValueError):
    def __init__(self, code, channel, message):
        super().__init__(message)
        self.code = code
        self.channel = channel


def oauth_state_code(channel):
    normalized = str(channel or "").strip().lower()
    definition = CHANNELS.get(normalized)
    if definition is None:
        raise ChannelError("unknown_channel", normalized, f"unknown channel: {normalized}")
    return definition["oauth_state_code"]


def channel_from_oauth_state_code(code):
    normalized = str(code or "").strip().upper()
    for channel, definition in CHANNELS.items():
        if definition["oauth_state_code"] == normalized:
            return channel
    raise ChannelError(
        "unknown_oauth_state_channel_code",
        normalized,
        f"unknown OAuth state channel code: {normalized}",
    )


def get(channel=None, capability=None):
    channel = str(channel or DEFAULT_CHANNEL).strip().lower()
    definition = CHANNELS.get(channel)
    if definition is None:
        raise ChannelError("unknown_channel", channel, f"unknown channel: {channel}")
    if not definition["implemented"]:
        raise ChannelError(
            "channel_not_implemented",
            channel,
            f"channel {definition['display_name']} ({channel}) is not implemented yet",
        )
    if capability and capability not in definition["capabilities"]:
        raise ChannelError(
            "channel_capability_not_implemented",
            channel,
            f"channel {definition['display_name']} does not implement {capability}",
        )
    return channel, definition


def selected_channel(config, explicit=None):
    account = config.get("account") or {}
    channel = explicit or account.get("channel") or config.get("default_channel") or DEFAULT_CHANNEL
    return str(channel).strip().lower()


def migrate_config(config):
    migrated = copy.deepcopy(config)
    version = migrated.get("config_schema_version")
    if version is not None:
        try:
            parsed_version = int(version)
        except (TypeError, ValueError) as exc:
            raise ValueError("config_schema_version must be an integer") from exc
        if parsed_version > CONFIG_SCHEMA_VERSION:
            raise ValueError(
                f"config schema {parsed_version} is newer than supported {CONFIG_SCHEMA_VERSION}"
            )

    channel_map = migrated.setdefault("channels", {})
    marketing = channel_map.setdefault("marketing", {})
    legacy_api = migrated.get("api") or {}
    legacy_oauth = migrated.get("oauth") or {}

    marketing_api = marketing.setdefault("api", {})
    for key, value in legacy_api.items():
        if key in LEGACY_API_CREDENTIAL_KEYS:
            continue
        if key in marketing_api and marketing_api[key] != value:
            raise ValueError(f"conflicting marketing API config field: {key}")
        marketing_api.setdefault(key, copy.deepcopy(value))

    for configured_channel in channel_map.values():
        configured_api = configured_channel.get("api") if isinstance(configured_channel, dict) else None
        if isinstance(configured_api, dict):
            for key in LEGACY_API_CREDENTIAL_KEYS:
                configured_api.pop(key, None)

    marketing_oauth = marketing.setdefault("oauth", {})
    for key, value in legacy_oauth.items():
        if key in marketing_oauth and marketing_oauth[key] != value:
            raise ValueError(f"conflicting marketing OAuth config field: {key}")
        marketing_oauth.setdefault(key, copy.deepcopy(value))

    channel_map.setdefault("qianchuan", {"status": "not_implemented"})
    migrated["default_channel"] = migrated.get("default_channel") or DEFAULT_CHANNEL
    migrated.setdefault("account", {})["channel"] = (
        (migrated.get("account") or {}).get("channel") or DEFAULT_CHANNEL
    )
    for template in (migrated.get("plan_templates") or {}).values():
        if isinstance(template, dict) and isinstance(template.get("bindings"), dict):
            template["bindings"].setdefault("channel", DEFAULT_CHANNEL)
    migrated["config_schema_version"] = CONFIG_SCHEMA_VERSION
    migrated.pop("api", None)
    migrated.pop("oauth", None)
    return migrated


def runtime_config(config, channel=None, capability=None):
    migrated = migrate_config(config)
    selected = selected_channel(migrated, channel)
    selected, definition = get(selected, capability=capability)
    channel_config = (migrated.get("channels") or {}).get(selected) or {}
    runtime = copy.deepcopy(migrated)
    runtime["api"] = copy.deepcopy(channel_config.get("api") or {})
    runtime["oauth"] = copy.deepcopy(channel_config.get("oauth") or {})
    runtime.setdefault("account", {})["channel"] = selected
    runtime["_channel"] = {
        "id": selected,
        "display_name": definition["display_name"],
    }
    return runtime


def status_rows(config):
    migrated = migrate_config(config)
    rows = []
    for channel, definition in CHANNELS.items():
        configured = (migrated.get("channels") or {}).get(channel) or {}
        rows.append({
            "channel": channel,
            "display_name": definition["display_name"],
            "implemented": definition["implemented"],
            "configured": bool(configured) and configured.get("status") != "not_implemented",
        })
    return rows
