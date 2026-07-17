#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import json
from pathlib import Path

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channel_adapters as channel_adapters
import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.auth.migrate_channels as migrate_channels
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import OceanEngineClient
from ocean_watch.api.client import get_json as get_business_json
from ocean_watch.core.data import get_path
from ocean_watch.core.process_lock import ProcessLock

PLACEHOLDER_PREFIX = "REPLACE_WITH"
SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
DEFAULT_REFRESH_MARGIN_SECONDS = 30 * 60
DEFAULT_LOCK_TIMEOUT_SECONDS = 60
MAX_ROLE_EXPANSION_PAGES = 100


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith(PLACEHOLDER_PREFIX)
    if isinstance(value, list):
        return len(value) == 0
    return False


def set_path(data, dotted, value):
    current = data
    parts = dotted.split(".")
    for part in parts[:-1]:
        current = current.setdefault(part, {})
    current[parts[-1]] = value


def parse_time(value):
    if not value:
        return None
    if isinstance(value, (int, float)):
        return dt.datetime.fromtimestamp(value, tz=dt.timezone.utc)
    if isinstance(value, str):
        normalized = value.strip()
        if not normalized:
            return None
        if normalized.endswith("Z"):
            normalized = normalized[:-1] + "+00:00"
        try:
            parsed = dt.datetime.fromisoformat(normalized)
        except ValueError:
            return None
        if parsed.tzinfo is None:
            parsed = parsed.replace(tzinfo=dt.datetime.now().astimezone().tzinfo)
        return parsed.astimezone(dt.timezone.utc)
    return None


def now_utc():
    return dt.datetime.now(dt.timezone.utc)


def iso_from_expires_in(expires_in):
    try:
        seconds = int(expires_in)
    except (TypeError, ValueError):
        return None
    return (now_utc() + dt.timedelta(seconds=seconds)).isoformat()


def token_has_ttl(config, margin_seconds=DEFAULT_REFRESH_MARGIN_SECONDS):
    token = get_path(config, "api.access_token")
    if is_missing(token):
        return False
    expires_at = parse_time(get_path(config, "api.access_token_expires_at"))
    if not expires_at:
        # Legacy/manual token: keep backward compatibility and let the API response decide.
        return True
    return expires_at - now_utc() > dt.timedelta(seconds=margin_seconds)


def refresh_token_has_ttl(config, margin_seconds=0):
    refresh_token = get_path(config, "api.refresh_token")
    if is_missing(refresh_token):
        return False
    expires_at = parse_time(get_path(config, "api.refresh_token_expires_at"))
    if not expires_at:
        return True
    return expires_at - now_utc() > dt.timedelta(seconds=margin_seconds)


def token_next_action(config):
    if token_has_ttl(config):
        return "ready"
    if refresh_token_has_ttl(config):
        return "refresh"
    return "reauthorize"


def advertiser_is_authorized(advertiser_id, authorized_ids):
    if is_missing(advertiser_id):
        return False
    normalized_id = str(advertiser_id)
    return any(str(item) == normalized_id for item in (authorized_ids or []))


def oauth_base_url(config):
    adapter = channel_adapter(config)
    return (
        get_path(config, "oauth.token_base_url")
        or get_path(config, "api.oauth_base_url")
        or adapter.token_base_url
    ).rstrip("/")


def channel_adapter(config, capability=None):
    return channel_adapters.get_adapter(
        channels.selected_channel(config),
        capability=capability,
    )


def post_json(base_url, path, payload):
    return OceanEngineClient(base_url).post(path, payload=payload)


def get_api_json(config, path, params, base_url=None):
    access_token = get_path(config, "api.access_token")
    if is_missing(access_token):
        raise RuntimeError("missing api.access_token")
    return get_business_json(
        base_url
        or get_path(config, "api.base_url")
        or channel_adapter(config).business_base_url,
        access_token,
        path,
        params,
    )


def get_oauth_json(config, path, params):
    access_token = get_path(config, "api.access_token")
    if is_missing(access_token):
        raise RuntimeError("missing api.access_token")
    return get_business_json(
        oauth_base_url(config),
        access_token,
        path,
        params,
    )


def redact(data):
    if isinstance(data, dict):
        result = {}
        for key, value in data.items():
            if key in SECRET_KEYS:
                result[key] = "<redacted>" if not is_missing(value) else value
            else:
                result[key] = redact(value)
        return result
    if isinstance(data, list):
        return [redact(item) for item in data]
    return data


def load_config(
    config_path,
    channel=None,
    advertiser_id=None,
    auth_account_id=None,
    authorization_id=None,
    allow_pending=False,
    capability=None,
):
    path = Path(config_path).expanduser()
    migrate_channels.assert_migration_ready(path)
    raw_config = json.loads(path.read_text(encoding="utf-8"))
    if int(raw_config.get("config_schema_version") or 1) < channels.CONFIG_SCHEMA_VERSION:
        raise RuntimeError(
            "channel migration required; run ocean-watch auth migrate for this config"
        )
    runtime = channels.runtime_config(raw_config, channel=channel, capability=capability)
    selected = channels.selected_channel(runtime, channel)
    advertiser_id = advertiser_id or get_path(runtime, "account.advertiser_id")
    if is_missing(advertiser_id):
        advertiser_id = None
    return authorization_store.attach_runtime(
        runtime,
        selected,
        advertiser_id=advertiser_id,
        auth_account_id=auth_account_id,
        authorization_id=authorization_id,
        allow_pending=allow_pending,
    )


def save_credentials(config):
    authorization = config.get("_authorization") or {}
    if authorization.get("authorization_id") and not authorization.get("legacy"):
        api = config.get("api") or {}
        persisted_token = authorization_store.update_authorization_tokens(
            authorization["channel"],
            authorization["authorization_id"],
            {
                key: api.get(key)
                for key in (
                    "access_token",
                    "refresh_token",
                    "access_token_expires_at",
                    "refresh_token_expires_at",
                    "last_token_update_at",
                )
            },
        )
        updated = copy.deepcopy(config)
        updated.setdefault("api", {}).update(persisted_token)
        return updated
    return credential_store.write_credentials(credential_store.extract_credentials(config))


def required_oauth_missing(config, include_refresh=False):
    fields = ["api.app_id", "api.secret"]
    if include_refresh:
        fields.append("api.refresh_token")
    return [field for field in fields if is_missing(get_path(config, field))]


def update_token_fields(config, token_data):
    updated = copy.deepcopy(config)
    api = updated.setdefault("api", {})
    if not is_missing(token_data.get("access_token")):
        api["access_token"] = token_data.get("access_token")
    if not is_missing(token_data.get("refresh_token")):
        api["refresh_token"] = token_data.get("refresh_token")
    access_expires_at = iso_from_expires_in(token_data.get("expires_in"))
    refresh_expires_at = iso_from_expires_in(token_data.get("refresh_token_expires_in"))
    if access_expires_at:
        api["access_token_expires_at"] = access_expires_at
    if refresh_expires_at:
        api["refresh_token_expires_at"] = refresh_expires_at
    api["last_token_update_at"] = now_utc().isoformat()
    return updated


def normalize_authorized_accounts(rows):
    fields = (
        "account_id",
        "account_string_id",
        "shop_id",
        "account_name",
        "account_role",
        "account_type",
        "advertiser_id",
        "advertiser_name",
        "is_valid",
    )
    normalized = []
    for row in rows or []:
        if not isinstance(row, dict):
            continue
        normalized.append({key: copy.deepcopy(row[key]) for key in fields if key in row})
    return normalized


def response_ids(rows, keys):
    identifiers = []
    for row in rows or []:
        if isinstance(row, dict):
            value = next((row.get(key) for key in keys if row.get(key) is not None), None)
        else:
            value = row
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            continue
        if parsed > 0:
            identifiers.append(parsed)
    return identifiers


def nonnegative_integer(value):
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return None
    if (
        isinstance(value, bool)
        or parsed < 0
        or isinstance(value, float) and value != parsed
    ):
        return None
    return parsed


def role_expansion_total_pages(data, rows, role):
    page_info = data.get("page_info")
    if not isinstance(page_info, dict) or "total_page" not in page_info:
        raise RuntimeError(
            f"Malformed pagination metadata while expanding advertiser role {role}"
        )
    total_pages = nonnegative_integer(page_info.get("total_page"))
    if total_pages is None:
        raise RuntimeError(
            f"Malformed pagination metadata while expanding advertiser role {role}"
        )

    if total_pages == 0:
        total_number = nonnegative_integer(page_info.get("total_number"))
        if rows or total_number != 0:
            raise RuntimeError(
                f"Malformed pagination metadata while expanding advertiser role {role}"
            )
    return total_pages


def fetch_role_advertiser_ids(config, account):
    adapter = channel_adapter(config, capability="accounts")
    role = channel_adapters.account_role(account)
    if role == "ADVERTISER":
        advertiser_id = adapter.direct_advertiser_id(account)
        if advertiser_id is None:
            return [], {"role": role, "status": "missing_account_id", "count": 0}
        return [advertiser_id], {"role": role, "status": "ok", "count": 1}
    expansion = adapter.role_expansion(account)
    if expansion is None:
        has_account_id = channel_adapters.first_positive_id(
            account,
            ("shop_id", "account_id", "account_string_id", "advertiser_id"),
        )
        if has_account_id is None:
            return [], {"role": role, "status": "missing_account_id", "count": 0}
        return [], {"role": role, "status": "unsupported_role", "count": 0}

    identifiers = []
    page = 1
    expected_total_pages = None
    while True:
        response = get_api_json(
            config,
            expansion.path,
            {**expansion.base_params, "page": page, "page_size": 100},
            base_url=expansion.base_url,
        )
        if response.get("code") != 0:
            return identifiers, {
                "role": role,
                "status": "api_error",
                "code": response.get("code"),
                "count": len(set(identifiers)),
                "permission_optional": response.get("code")
                in expansion.optional_permission_codes,
            }
        data = response.get("data") or {}
        rows = data.get(expansion.list_key) or []
        identifiers.extend(response_ids(rows, expansion.id_keys))
        total_pages = role_expansion_total_pages(data, rows, role)
        if total_pages > MAX_ROLE_EXPANSION_PAGES:
            raise RuntimeError(
                "Advertiser role expansion exceeds the pagination safety cap: "
                f"{total_pages} pages"
            )
        if expected_total_pages is None:
            expected_total_pages = total_pages
        elif total_pages != expected_total_pages:
            raise RuntimeError(
                f"Malformed pagination metadata while expanding advertiser role {role}"
            )
        if page >= total_pages:
            break
        page += 1
    unique_ids = list(dict.fromkeys(identifiers))
    return unique_ids, {"role": role, "status": "ok", "count": len(unique_ids), "pages": page}


def verify_advertiser_ids(config, advertiser_ids):
    adapter = channel_adapter(config, capability="accounts")
    verified = []
    errors = []
    for offset in range(0, len(advertiser_ids), 50):
        chunk = advertiser_ids[offset:offset + 50]
        response = get_api_json(
            config,
            adapter.advertiser_info_path,
            {"advertiser_ids": json.dumps(chunk, separators=(",", ":"))},
        )
        if response.get("code") != 0:
            errors.append({"code": response.get("code"), "count": len(chunk)})
            continue
        verified.extend(response_ids(response.get("data") or [], ("id", "advertiser_id")))
    return list(dict.fromkeys(verified)), errors


def expand_authorized_advertisers(config, accounts):
    candidate_ids = []
    branch_results = []
    for account in accounts:
        identifiers, result = fetch_role_advertiser_ids(config, account)
        candidate_ids.extend(identifiers)
        branch_results.append(result)
    candidate_ids = list(dict.fromkeys(candidate_ids))
    verified_ids, verification_errors = verify_advertiser_ids(config, candidate_ids)
    if candidate_ids and verification_errors and not verified_ids:
        raise RuntimeError("Unable to verify expanded advertiser IDs through advertiser/info")
    role_counts = {}
    for result in branch_results:
        role = result["role"]
        role_counts[role] = role_counts.get(role, 0) + 1
    return verified_ids, {
        "candidate_advertiser_count": len(candidate_ids),
        "verified_advertiser_count": len(verified_ids),
        "role_counts": role_counts,
        "branch_error_count": sum(result["status"] == "api_error" for result in branch_results),
        "unsupported_role_count": sum(result["status"] == "unsupported_role" for result in branch_results),
        "verification_error_count": len(verification_errors),
    }


def build_authorized_account_snapshot(config, accounts):
    rows = []
    candidate_ids = []
    account_candidates = []
    discovery_issues = []
    for account in accounts:
        identifiers, result = fetch_role_advertiser_ids(config, account)
        if result.get("status") != "ok":
            if result.get("permission_optional"):
                discovery_issues.append({
                    "account_id": str(
                        channel_adapters.first_positive_id(
                            account,
                            ("account_id", "account_string_id", "advertiser_id"),
                        )
                    ),
                    "role": result["role"],
                    "code": result.get("code"),
                    "reason": "app_permission_missing",
                })
                account_candidates.append((account, []))
                continue
            raise RuntimeError(
                "Authorized account expansion did not produce a complete snapshot: "
                + json.dumps(redact(result), ensure_ascii=False)
            )
        candidate_ids.extend(identifiers)
        account_candidates.append((account, identifiers))
    verified_ids, verification_errors = verify_advertiser_ids(
        config,
        list(dict.fromkeys(candidate_ids)),
    )
    if verification_errors:
        raise RuntimeError("Unable to verify complete authorized advertiser snapshot")
    verified = {str(value) for value in verified_ids}
    for account, identifiers in account_candidates:
        row = copy.deepcopy(account)
        row["advertiser_ids"] = [
            str(value) for value in identifiers if str(value) in verified
        ]
        rows.append(row)
    return (
        rows,
        list(dict.fromkeys(str(value) for value in verified_ids)),
        discovery_issues,
    )


def fetch_authorized_accounts(config):
    adapter = channel_adapter(config, capability="accounts")
    response = get_oauth_json(
        config,
        adapter.authorized_account_path,
        {},
    )
    if response.get("code") != 0:
        raise RuntimeError(json.dumps(redact(response), ensure_ascii=False))
    accounts = normalize_authorized_accounts((response.get("data") or {}).get("list") or [])
    valid_accounts = [account for account in accounts if account.get("is_valid") is not False]
    snapshot, advertiser_ids, discovery_issues = build_authorized_account_snapshot(
        config,
        valid_accounts,
    )
    advertiser_ids = [int(value) for value in advertiser_ids]
    expansion_summary = {
        "complete_snapshot": not discovery_issues,
        "candidate_advertiser_count": len(advertiser_ids),
        "verified_advertiser_count": len(advertiser_ids),
        "account_discovery_issues": discovery_issues,
    }
    account_types = {}
    for account in accounts:
        account_type = account.get("account_type") or "UNKNOWN"
        account_types[account_type] = account_types.get(account_type, 0) + 1
    return snapshot, advertiser_ids, account_types, expansion_summary


def update_authorized_accounts(config):
    accounts, advertiser_ids, account_types, expansion_summary = fetch_authorized_accounts(config)
    updated = copy.deepcopy(config)
    api = updated.setdefault("api", {})
    api["oauth_authorized_accounts"] = accounts
    api["authorized_advertiser_ids"] = advertiser_ids
    api["account_discovery_issues"] = expansion_summary["account_discovery_issues"]
    api["last_authorized_account_sync_at"] = now_utc().isoformat()
    return updated, {
        "oauth_authorized_account_count": len(accounts),
        "authorized_advertiser_count": len(advertiser_ids),
        "account_types": account_types,
        "advertiser_expansion": expansion_summary,
    }


def sync_authorized_accounts(config_path, config=None, rebind_existing=False):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    updated, summary = update_authorized_accounts(config)
    authorization = updated.get("_authorization") or {}
    if authorization.get("authorization_id") and not authorization.get("legacy"):
        authorization_store.replace_authorization_snapshot(
            authorization["channel"],
            authorization["authorization_id"],
            get_path(updated, "api.oauth_authorized_accounts", []),
            rebind_existing=rebind_existing,
            synced_at=get_path(updated, "api.last_authorized_account_sync_at"),
            discovery_issues=get_path(updated, "api.account_discovery_issues", []),
        )
    else:
        save_credentials(updated)
    return updated, summary


def update_accounts_after_token(config):
    try:
        return update_authorized_accounts(config)
    except RuntimeError as exc:
        return config, {
            "sync_failed": True,
            "error": str(exc),
        }


def exchange_auth_code(config_path, auth_code, config=None, channel=None, rebind_existing=False):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    missing = required_oauth_missing(config)
    if missing:
        raise RuntimeError("missing OAuth config fields: " + ", ".join(missing))
    if is_missing(auth_code):
        raise RuntimeError("missing auth_code")

    payload = {
        "app_id": int(get_path(config, "api.app_id")),
        "secret": get_path(config, "api.secret"),
        "grant_type": "auth_code",
        "auth_code": auth_code,
    }
    adapter = channel_adapter(config, capability="oauth")
    response = post_json(oauth_base_url(config), adapter.access_token_path, payload)
    if response.get("code") != 0:
        raise RuntimeError(json.dumps(redact(response), ensure_ascii=False))

    data = response.get("data") or {}
    if is_missing(data.get("access_token")) or is_missing(data.get("refresh_token")):
        raise RuntimeError("OAuth authorization response did not include access_token and refresh_token")
    updated = update_token_fields(config, data)
    selected_channel = channels.selected_channel(updated, channel)
    pending = copy.deepcopy(updated)
    pending.setdefault("api", {})["pending_account_sync"] = True
    authorization_id = authorization_store.save_authorization(
        selected_channel,
        pending["api"],
        [],
        rebind_existing=rebind_existing,
    )
    updated["_authorization"] = {
        "channel": selected_channel,
        "authorization_id": authorization_id,
        "legacy": False,
    }
    authorization = copy.deepcopy(updated["_authorization"])
    try:
        updated, account_summary = update_authorized_accounts(updated)
        updated["_authorization"] = authorization
        authorization_store.replace_authorization_snapshot(
            selected_channel,
            authorization_id,
            get_path(updated, "api.oauth_authorized_accounts", []),
            rebind_existing=rebind_existing,
            synced_at=get_path(updated, "api.last_authorized_account_sync_at"),
            discovery_issues=get_path(updated, "api.account_discovery_issues", []),
        )
    except Exception as exc:
        updated["_authorization"] = authorization
        updated.setdefault("api", {})["pending_account_sync"] = True
        account_summary = {
            "sync_failed": True,
            "error": str(exc),
            "pending_account_sync": True,
        }
    return updated, redact({
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "access_token_expires_at": get_path(updated, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(updated, "api.refresh_token_expires_at"),
        "channel": selected_channel,
        "authorization_id": authorization_id,
        "authorized_accounts": account_summary,
    })


def refresh_access_token(config_path, config=None):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    missing = required_oauth_missing(config, include_refresh=True)
    if missing:
        raise RuntimeError("missing OAuth refresh fields: " + ", ".join(missing))
    if not refresh_token_has_ttl(config):
        raise RuntimeError(
            "OAuth refresh token is expired; run ocean-watch auth authorize again"
        )

    payload = {
        "app_id": int(get_path(config, "api.app_id")),
        "secret": get_path(config, "api.secret"),
        "grant_type": "refresh_token",
        "refresh_token": get_path(config, "api.refresh_token"),
    }
    adapter = channel_adapter(config, capability="oauth")
    response = post_json(oauth_base_url(config), adapter.refresh_token_path, payload)
    if response.get("code") != 0:
        raise RuntimeError(json.dumps(redact(response), ensure_ascii=False))

    data = response.get("data") or {}
    if is_missing(data.get("access_token")):
        raise RuntimeError("OAuth refresh response did not include access_token")
    updated = update_token_fields(config, data)
    persisted = save_credentials(updated)
    if isinstance(persisted, dict):
        updated = persisted
    return updated, redact({
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "access_token_expires_at": get_path(updated, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(updated, "api.refresh_token_expires_at"),
    })


def ensure_access_token(
    config_path,
    config=None,
    force_refresh=False,
    margin_seconds=DEFAULT_REFRESH_MARGIN_SECONDS,
    channel=None,
    advertiser_id=None,
    auth_account_id=None,
    authorization_id=None,
    allow_pending=False,
):
    config_path = Path(config_path).expanduser()
    runtime_config = copy.deepcopy(config) if config is not None else None
    selection_requested = any(
        value is not None
        for value in (channel, advertiser_id, auth_account_id, authorization_id)
    )
    if (
        config is None
        or selection_requested
        or is_missing(get_path(config, "api.access_token"))
    ):
        credential_config = load_config(
            config_path,
            channel=channel,
            advertiser_id=advertiser_id,
            auth_account_id=auth_account_id,
            authorization_id=authorization_id,
            allow_pending=allow_pending,
        )
        if runtime_config is None:
            config = credential_config
        else:
            config = authorization_store.merge_runtime_authorization(
                runtime_config,
                credential_config,
            )
    else:
        config = copy.deepcopy(config)
    if not force_refresh and token_has_ttl(config, margin_seconds=margin_seconds):
        return config

    selected_channel = channels.selected_channel(config, channel)
    authorization_id = get_path(config, "_authorization.authorization_id") or "legacy"
    lock_path = authorization_store.state_root() / "refresh-locks" / (
        f"{selected_channel}-{authorization_id}.lock"
    )
    with ProcessLock(lock_path, timeout=DEFAULT_LOCK_TIMEOUT_SECONDS):
        locked_credentials = load_config(
            config_path,
            channel=selected_channel,
            advertiser_id=advertiser_id,
            auth_account_id=auth_account_id,
            authorization_id=authorization_id,
            allow_pending=allow_pending,
        )
        locked_config = authorization_store.merge_runtime_authorization(
            config,
            locked_credentials,
        )
        if not force_refresh and token_has_ttl(locked_config, margin_seconds=margin_seconds):
            return locked_config
        refreshed, _ = refresh_access_token(config_path, locked_config)
        return refreshed


def add_authorization_arguments(parser, include_local_authorization=False):
    parser.add_argument(
        "--channel",
        default="marketing",
        choices=tuple(channels.CHANNELS),
        help="Business channel. Existing Ocean Engine Marketing operations use marketing.",
    )
    parser.add_argument(
        "--auth-account-id",
        help="Official OAuth account_id override; normally resolved from advertiser_id.",
    )
    if include_local_authorization:
        parser.add_argument(
            "--authorization-id",
            help="Local authorization ID, used to sync a migrated pending authorization.",
        )
        parser.add_argument(
            "--rebind-existing",
            action="store_true",
            help="Move conflicting authorized accounts to this authorization during sync.",
        )


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--refresh", action="store_true", help="Force refresh using api.refresh_token.")
    parser.add_argument("--sync-accounts", action="store_true", help="Sync authorized account details without refreshing a valid token.")
    parser.add_argument("--status", action="store_true", help="Print redacted token status.")
    add_authorization_arguments(parser, include_local_authorization=True)
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    resolution_error = None
    try:
        config = load_config(
            config_path,
            channel=args.channel,
            auth_account_id=args.auth_account_id,
            authorization_id=args.authorization_id,
            allow_pending=args.sync_accounts,
        )
    except authorization_store.AuthorizationError as exc:
        if args.refresh or args.sync_accounts:
            raise
        raw_config = json.loads(config_path.read_text(encoding="utf-8"))
        config = channels.runtime_config(raw_config, channel=args.channel)
        config.setdefault("api", {}).update(authorization_store.read_app(args.channel))
        resolution_error = {
            "code": exc.code,
            "message": str(exc),
            **exc.details,
        }
    if args.refresh:
        config = ensure_access_token(
            config_path,
            config,
            force_refresh=True,
            channel=args.channel,
            auth_account_id=args.auth_account_id,
            authorization_id=args.authorization_id,
            allow_pending=args.sync_accounts,
        )
    elif args.sync_accounts:
        config = ensure_access_token(
            config_path,
            config,
            channel=args.channel,
            auth_account_id=args.auth_account_id,
            authorization_id=args.authorization_id,
            allow_pending=True,
        )
        config, _ = sync_authorized_accounts(
            config_path,
            config,
            rebind_existing=args.rebind_existing,
        )

    status = {
        "channel": args.channel,
        "channel_display_name": channels.CHANNELS[args.channel]["display_name"],
        "authorization_id": get_path(config, "_authorization.authorization_id"),
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "config": str(config_path),
        "has_access_token": not is_missing(get_path(config, "api.access_token")),
        "has_refresh_token": not is_missing(get_path(config, "api.refresh_token")),
        "has_app_id": not is_missing(get_path(config, "api.app_id")),
        "has_secret": not is_missing(get_path(config, "api.secret")),
        "access_token_expires_at": get_path(config, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(config, "api.refresh_token_expires_at"),
        "token_has_ttl": token_has_ttl(config),
        "refresh_token_has_ttl": refresh_token_has_ttl(config),
        "next_action": token_next_action(config),
        "oauth_authorized_account_count": len(get_path(config, "api.oauth_authorized_accounts", []) or []),
        "authorized_advertiser_count": len(get_path(config, "api.authorized_advertiser_ids", []) or []),
        "advertiser_id_authorized": advertiser_is_authorized(
            get_path(config, "account.advertiser_id"),
            get_path(config, "api.authorized_advertiser_ids", []),
        ),
        "credential_backend": credential_store.backend_name(),
        "project_config_has_sensitive_fields": credential_store.sensitive_config_fields(
            json.loads(config_path.read_text(encoding="utf-8"))
        ),
        "authorization_status": authorization_store.status(
            args.channel,
            advertiser_id=(
                None
                if is_missing(get_path(config, "account.advertiser_id"))
                else get_path(config, "account.advertiser_id")
            ),
        ),
        "resolution_error": resolution_error,
    }
    print(json.dumps(status, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
