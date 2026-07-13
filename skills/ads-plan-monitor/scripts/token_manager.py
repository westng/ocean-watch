#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import json
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import config_paths
import channels
import authorization_store
import credential_store
import migrate_channels
from process_lock import ProcessLock


PLACEHOLDER_PREFIX = "REPLACE_WITH"
SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
DEFAULT_OAUTH_BASE_URL = "https://ad.oceanengine.com/open_api"
ACCESS_TOKEN_PATH = "/oauth2/access_token/"
REFRESH_TOKEN_PATH = "/oauth2/refresh_token/"
AUTHORIZED_ACCOUNT_PATH = "/oauth2/advertiser/get/"
CUSTOMER_CENTER_ADVERTISER_PATH = "/2/customer_center/advertiser/list/"
EBP_ADVERTISER_PATH = "/2/ebp/advertiser/list/"
ADVERTISER_INFO_PATH = "/2/advertiser/info/"
DEFAULT_REFRESH_MARGIN_SECONDS = 30 * 60
DEFAULT_LOCK_TIMEOUT_SECONDS = 60


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith(PLACEHOLDER_PREFIX)
    if isinstance(value, list):
        return len(value) == 0
    return False


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


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
    return (
        get_path(config, "oauth.token_base_url")
        or get_path(config, "api.oauth_base_url")
        or DEFAULT_OAUTH_BASE_URL
    ).rstrip("/")


def post_json(base_url, path, payload):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    request = urllib.request.Request(
        base_url.rstrip("/") + path,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        text = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": text}


def get_json(base_url, path, params):
    url = base_url.rstrip("/") + path + "?" + urllib.parse.urlencode(params)
    request = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        text = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": text}


def get_api_json(config, path, params):
    access_token = get_path(config, "api.access_token")
    if is_missing(access_token):
        raise RuntimeError("missing api.access_token")
    url = get_path(config, "api.base_url").rstrip("/") + path + "?" + urllib.parse.urlencode(params)
    request = urllib.request.Request(
        url,
        headers={"Access-Token": access_token},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        text = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": text}


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
            "channel migration required; run scripts/migrate_channels.py for this config"
        )
    runtime = channels.runtime_config(raw_config, channel=channel, capability=capability)
    selected = channels.selected_channel(runtime, channel)
    advertiser_id = advertiser_id or get_path(runtime, "account.advertiser_id")
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


def positive_account_id(account):
    for key in ("account_id", "account_string_id", "advertiser_id"):
        value = account.get(key)
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            continue
        if parsed > 0:
            return parsed
    return None


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


def fetch_role_advertiser_ids(config, account):
    role = account.get("account_role") or account.get("account_type") or "UNKNOWN"
    source_id = positive_account_id(account)
    if source_id is None:
        return [], {"role": role, "status": "missing_account_id", "count": 0}
    if role == "ADVERTISER":
        return [source_id], {"role": role, "status": "ok", "count": 1}
    if role in {"CUSTOMER_ADMIN", "CUSTOMER_OPERATOR"}:
        path = CUSTOMER_CENTER_ADVERTISER_PATH
        base_params = {"cc_account_id": source_id, "account_source": "AD"}
        list_key = "list"
        id_keys = ("advertiser_id", "account_id")
    elif role in {"PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR"}:
        path = EBP_ADVERTISER_PATH
        base_params = {"enterprise_organization_id": source_id, "account_source": "AD"}
        list_key = "account_list"
        id_keys = ("account_id", "advertiser_id")
    else:
        return [], {"role": role, "status": "unsupported_role", "count": 0}

    identifiers = []
    page = 1
    while page <= 100:
        response = get_api_json(
            config,
            path,
            {**base_params, "page": page, "page_size": 100},
        )
        if response.get("code") != 0:
            return identifiers, {
                "role": role,
                "status": "api_error",
                "code": response.get("code"),
                "count": len(set(identifiers)),
            }
        data = response.get("data") or {}
        rows = data.get(list_key) or []
        identifiers.extend(response_ids(rows, id_keys))
        page_info = data.get("page_info") or {}
        total_page = int(page_info.get("total_page") or 1)
        if page >= total_page or not rows:
            break
        page += 1
    unique_ids = list(dict.fromkeys(identifiers))
    return unique_ids, {"role": role, "status": "ok", "count": len(unique_ids), "pages": page}


def verify_advertiser_ids(config, advertiser_ids):
    verified = []
    errors = []
    for offset in range(0, len(advertiser_ids), 50):
        chunk = advertiser_ids[offset:offset + 50]
        response = get_api_json(
            config,
            ADVERTISER_INFO_PATH,
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
    for account in accounts:
        identifiers, result = fetch_role_advertiser_ids(config, account)
        if result.get("status") != "ok":
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
    return rows, list(dict.fromkeys(str(value) for value in verified_ids))


def fetch_authorized_accounts(config):
    access_token = get_path(config, "api.access_token")
    if is_missing(access_token):
        raise RuntimeError("missing api.access_token for authorized account sync")
    response = get_json(
        oauth_base_url(config),
        AUTHORIZED_ACCOUNT_PATH,
        {"access_token": access_token},
    )
    if response.get("code") != 0:
        raise RuntimeError(json.dumps(redact(response), ensure_ascii=False))
    accounts = normalize_authorized_accounts((response.get("data") or {}).get("list") or [])
    valid_accounts = [account for account in accounts if account.get("is_valid") is not False]
    snapshot, advertiser_ids = build_authorized_account_snapshot(config, valid_accounts)
    advertiser_ids = [int(value) for value in advertiser_ids]
    expansion_summary = {
        "complete_snapshot": True,
        "candidate_advertiser_count": len(advertiser_ids),
        "verified_advertiser_count": len(advertiser_ids),
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
    api["last_authorized_account_sync_at"] = now_utc().isoformat()
    return updated, {
        "oauth_authorized_account_count": len(accounts),
        "authorized_advertiser_count": len(advertiser_ids),
        "account_types": account_types,
        "advertiser_expansion": expansion_summary,
    }


def sync_authorized_accounts(config_path, config=None):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    updated, summary = update_authorized_accounts(config)
    authorization = updated.get("_authorization") or {}
    if authorization.get("authorization_id") and not authorization.get("legacy"):
        authorization_store.replace_authorization_snapshot(
            authorization["channel"],
            authorization["authorization_id"],
            get_path(updated, "api.oauth_authorized_accounts", []),
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
    response = post_json(oauth_base_url(config), ACCESS_TOKEN_PATH, payload)
    if response.get("code") != 0:
        raise RuntimeError(json.dumps(redact(response), ensure_ascii=False))

    data = response.get("data") or {}
    if is_missing(data.get("access_token")) or is_missing(data.get("refresh_token")):
        raise RuntimeError("OAuth authorization response did not include access_token and refresh_token")
    updated = update_token_fields(config, data)
    updated, account_summary = update_authorized_accounts(updated)
    selected_channel = channels.selected_channel(updated, channel)
    authorization_id = authorization_store.save_authorization(
        selected_channel,
        updated["api"],
        get_path(updated, "api.oauth_authorized_accounts", []),
        rebind_existing=rebind_existing,
    )
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
            "OAuth refresh token is expired; run scripts/oauth_local_authorize.py to authorize again"
        )

    payload = {
        "app_id": int(get_path(config, "api.app_id")),
        "secret": get_path(config, "api.secret"),
        "grant_type": "refresh_token",
        "refresh_token": get_path(config, "api.refresh_token"),
    }
    response = post_json(oauth_base_url(config), REFRESH_TOKEN_PATH, payload)
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
    if config is None or channel is not None or is_missing(get_path(config, "api.access_token")):
        config = load_config(
            config_path,
            channel=channel,
            advertiser_id=advertiser_id,
            auth_account_id=auth_account_id,
            authorization_id=authorization_id,
            allow_pending=allow_pending,
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
        locked_config = load_config(
            config_path,
            channel=selected_channel,
            advertiser_id=advertiser_id,
            auth_account_id=auth_account_id,
            authorization_id=authorization_id,
            allow_pending=allow_pending,
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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--refresh", action="store_true", help="Force refresh using api.refresh_token.")
    parser.add_argument("--sync-accounts", action="store_true", help="Sync authorized account details without refreshing a valid token.")
    parser.add_argument("--status", action="store_true", help="Print redacted token status.")
    add_authorization_arguments(parser, include_local_authorization=True)
    args = parser.parse_args()

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
        config, _ = sync_authorized_accounts(config_path, config)

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
            advertiser_id=get_path(config, "account.advertiser_id"),
        ),
        "resolution_error": resolution_error,
    }
    print(json.dumps(status, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
