import copy
import re

from ocean_watch.auth import channels
from ocean_watch.core.errors import ConfigurationError

SCHEMA_VERSION = 1
MAX_DECIMAL_ID = "9223372036854775807"
DECIMAL_ID_PATTERN = re.compile(r"[1-9][0-9]*\Z")
UNSET = object()


def decimal_id(value, field):
    if value is None or isinstance(value, bool):
        raise ConfigurationError(f"{field} must be a canonical positive decimal ID")
    normalized = str(value)
    if not DECIMAL_ID_PATTERN.fullmatch(normalized):
        raise ConfigurationError(f"{field} must be a canonical positive decimal ID")
    if len(normalized) > len(MAX_DECIMAL_ID) or (
        len(normalized) == len(MAX_DECIMAL_ID) and normalized > MAX_DECIMAL_ID
    ):
        raise ConfigurationError(f"{field} must not exceed {MAX_DECIMAL_ID}")
    return normalized


def advertiser_id(value):
    return decimal_id(value, "advertiser_id")


def enabled_value(value):
    if not isinstance(value, bool):
        raise ConfigurationError("managed account enabled must be a boolean")
    return value


def account_name(value):
    normalized = str(value or "").strip()
    if not normalized:
        raise ConfigurationError("managed account name cannot be empty")
    if len(normalized) > 100:
        raise ConfigurationError("managed account name cannot exceed 100 characters")
    return normalized


def channel_id(value):
    channel, _ = channels.get(value)
    return channel


def migrate(config):
    migrated = copy.deepcopy(config)
    try:
        version = int(migrated.get("managed_account_schema_version") or SCHEMA_VERSION)
    except (TypeError, ValueError) as exc:
        raise ConfigurationError("managed_account_schema_version must be an integer") from exc
    if version > SCHEMA_VERSION:
        raise ConfigurationError(
            f"managed account schema {version} is newer than supported {SCHEMA_VERSION}"
        )

    source = migrated.get("managed_accounts") or {}
    if not isinstance(source, dict):
        raise ConfigurationError("managed_accounts must be an object grouped by channel")
    normalized = {channel: [] for channel in channels.CHANNELS}
    seen = set()
    for configured_channel, records in source.items():
        channel = channel_id(configured_channel)
        if not isinstance(records, list):
            raise ConfigurationError(f"managed_accounts.{channel} must be a list")
        for record in records:
            if not isinstance(record, dict):
                raise ConfigurationError(f"managed_accounts.{channel} contains an invalid record")
            identifier = advertiser_id(record.get("advertiser_id"))
            key = (channel, identifier)
            if key in seen:
                raise ConfigurationError(
                    "managed account is duplicated",
                    {"channel": channel, "advertiser_id": identifier},
                )
            seen.add(key)
            normalized_record = {
                "advertiser_id": identifier,
                "name": account_name(record.get("name")),
                "enabled": enabled_value(record.get("enabled", True)),
            }
            if "auth_account_id" in record:
                normalized_record["auth_account_id"] = decimal_id(
                    record["auth_account_id"],
                    "auth_account_id",
                )
            normalized[channel].append(normalized_record)
    migrated["managed_account_schema_version"] = SCHEMA_VERSION
    migrated["managed_accounts"] = normalized
    return migrated


def list_accounts(config, *, channel=None, enabled_only=False):
    migrated = migrate(config)
    selected_channels = [channel_id(channel)] if channel else list(channels.CHANNELS)
    result = []
    for selected in selected_channels:
        for record in migrated["managed_accounts"][selected]:
            if enabled_only and not record["enabled"]:
                continue
            result.append({"channel": selected, **copy.deepcopy(record)})
    return result


def upsert(
    config,
    *,
    channel,
    advertiser_id_value,
    name,
    enabled=UNSET,
    auth_account_id=UNSET,
):
    migrated = migrate(config)
    selected = channel_id(channel)
    identifier = advertiser_id(advertiser_id_value)
    normalized_name = account_name(name)
    records = migrated["managed_accounts"][selected]
    for record in records:
        if record["advertiser_id"] == identifier:
            record["name"] = normalized_name
            if enabled is not UNSET:
                record["enabled"] = enabled_value(enabled)
            if auth_account_id is not UNSET:
                record["auth_account_id"] = decimal_id(
                    auth_account_id,
                    "auth_account_id",
                )
            return migrated, {"channel": selected, **copy.deepcopy(record)}, False
    record = {
        "advertiser_id": identifier,
        "name": normalized_name,
        "enabled": True if enabled is UNSET else enabled_value(enabled),
    }
    if auth_account_id is not UNSET:
        record["auth_account_id"] = decimal_id(auth_account_id, "auth_account_id")
    records.append(record)
    return migrated, {"channel": selected, **copy.deepcopy(record)}, True


def remove(config, *, channel, advertiser_id_value):
    migrated = migrate(config)
    selected = channel_id(channel)
    identifier = advertiser_id(advertiser_id_value)
    records = migrated["managed_accounts"][selected]
    remaining = [record for record in records if record["advertiser_id"] != identifier]
    if len(remaining) == len(records):
        raise ConfigurationError(
            "managed account was not found",
            {"channel": selected, "advertiser_id": identifier},
        )
    migrated["managed_accounts"][selected] = remaining
    return migrated, {"channel": selected, "advertiser_id": identifier}


def set_enabled(config, *, channel, advertiser_id_value, enabled):
    migrated = migrate(config)
    selected = channel_id(channel)
    identifier = advertiser_id(advertiser_id_value)
    normalized_enabled = enabled_value(enabled)
    for record in migrated["managed_accounts"][selected]:
        if record["advertiser_id"] == identifier:
            record["enabled"] = normalized_enabled
            return migrated, {"channel": selected, **copy.deepcopy(record)}
    raise ConfigurationError(
        "managed account was not found",
        {"channel": selected, "advertiser_id": identifier},
    )
