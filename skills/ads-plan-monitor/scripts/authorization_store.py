#!/usr/bin/env python3
import copy
import hashlib
import json
import os
import re
import secrets
from pathlib import Path

import config_store
import credential_store
from process_lock import ProcessLock


STATE_SCHEMA_VERSION = 2
ID_PATTERN = re.compile(r"0|[1-9][0-9]*\Z")
APP_ACCOUNT_PREFIX = "oceanengine-app"
AUTH_ACCOUNT_PREFIX = "oceanengine-auth"


class AuthorizationError(RuntimeError):
    def __init__(self, code, message, **details):
        super().__init__(message)
        self.code = code
        self.details = details


def state_root():
    configured = os.environ.get("ADS_PLAN_MONITOR_STATE_DIR")
    return Path(configured).expanduser() if configured else Path.home() / ".codex" / "ads-plan-monitor" / "state"


def state_path():
    return state_root() / "authorizations.json"


def channel_state_root(channel):
    return state_root() / "channels" / channel


def channel_current_path(channel):
    return channel_state_root(channel) / "current.json"


def channel_manifest_path(channel, generation):
    return channel_state_root(channel) / f"manifest-{int(generation)}.json"


def channel_lock_path(channel, path=None):
    base = Path(path or state_path())
    return base.with_suffix(base.suffix + f".{channel}.lock")


def empty_state():
    return {"schema_version": STATE_SCHEMA_VERSION, "channels": {}}


def normalize_id(value, field="id"):
    if isinstance(value, bool) or value is None:
        raise ValueError(f"{field} must be a decimal string")
    text = str(value)
    if not ID_PATTERN.fullmatch(text):
        raise ValueError(f"{field} must match 0|[1-9][0-9]*")
    return text


def _load_legacy_state(path):
    if not path.exists():
        return empty_state()
    data = json.loads(path.read_text(encoding="utf-8"))
    if int(data.get("schema_version") or 0) != STATE_SCHEMA_VERSION:
        raise AuthorizationError("authorization_state_version_unsupported", "unsupported authorization state schema")
    return data


def _manifest_payload(channel_state):
    return json.dumps(channel_state, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _manifest_checksum(channel_state):
    return hashlib.sha256(_manifest_payload(channel_state)).hexdigest()


def load_channel_state(channel, state_file=None):
    if state_file is not None:
        state = _load_legacy_state(Path(state_file))
        return copy.deepcopy((state.get("channels") or {}).get(channel) or {})
    current_path = channel_current_path(channel)
    if not current_path.exists():
        legacy = _load_legacy_state(state_path())
        return copy.deepcopy((legacy.get("channels") or {}).get(channel) or {})
    current = json.loads(current_path.read_text(encoding="utf-8"))
    generation = int(current.get("generation") or 0)
    manifest_path = channel_manifest_path(channel, generation)
    if generation < 1 or not manifest_path.exists():
        raise AuthorizationError("authorization_state_incomplete", f"missing current manifest for {channel}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if current.get("sha256") != _manifest_checksum(manifest):
        raise AuthorizationError("authorization_state_corrupt", f"manifest checksum mismatch for {channel}")
    return manifest


def load_state(path=None):
    if path is not None:
        return _load_legacy_state(Path(path))
    state = _load_legacy_state(state_path())
    channels_dir = state_root() / "channels"
    channel_names = set((state.get("channels") or {}).keys())
    if channels_dir.exists():
        channel_names.update(path.name for path in channels_dir.iterdir() if path.is_dir())
    for channel in channel_names:
        if channel_current_path(channel).exists():
            state.setdefault("channels", {})[channel] = load_channel_state(channel)
    return state


def save_state(state, path=None):
    path = Path(path or state_path())
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.chmod(path.parent, 0o700)
    except OSError:
        pass
    config_store.atomic_write_json(path, state, backup=False)
    try:
        os.chmod(path.parent, 0o700)
        os.chmod(path, 0o600)
    except OSError:
        pass


def commit_channel_state(channel, channel_state, state_file=None):
    if state_file is not None:
        state = _load_legacy_state(Path(state_file))
        state.setdefault("channels", {})[channel] = copy.deepcopy(channel_state)
        save_state(state, state_file)
        return
    root = channel_state_root(channel)
    root.mkdir(parents=True, exist_ok=True)
    try:
        os.chmod(root.parent, 0o700)
        os.chmod(root, 0o700)
    except OSError:
        pass
    generation = int(channel_state.get("generation") or 0)
    if generation < 1:
        raise AuthorizationError("credential_generation_conflict", "channel generation must be positive")
    manifest_path = channel_manifest_path(channel, generation)
    while manifest_path.exists():
        existing = json.loads(manifest_path.read_text(encoding="utf-8"))
        if existing == channel_state:
            break
        generation += 1
        channel_state["generation"] = generation
        manifest_path = channel_manifest_path(channel, generation)
    if not manifest_path.exists():
        config_store.atomic_write_json(manifest_path, channel_state, backup=False)
    checksum = _manifest_checksum(channel_state)
    written = json.loads(manifest_path.read_text(encoding="utf-8"))
    if _manifest_checksum(written) != checksum:
        raise AuthorizationError("authorization_state_corrupt", "manifest read-back verification failed")
    config_store.atomic_write_json(
        channel_current_path(channel),
        {"schema_version": STATE_SCHEMA_VERSION, "generation": generation, "sha256": checksum},
        backup=False,
    )
    try:
        os.chmod(manifest_path, 0o600)
        os.chmod(channel_current_path(channel), 0o600)
    except OSError:
        pass


def app_account(channel):
    return f"{APP_ACCOUNT_PREFIX}-{channel}"


def authorization_account(channel, authorization_id, revision=1):
    return f"{AUTH_ACCOUNT_PREFIX}-{channel}-{authorization_id}-r{int(revision)}"


def read_app(channel, legacy=False):
    app = credential_store.read_entry(app_account(channel))
    if app or channel != "marketing" or not legacy:
        return app
    old = credential_store.read_credentials()
    return {key: old[key] for key in ("app_id", "secret") if key in old}


def write_app(channel, app_id, secret):
    data = {
        "app_id": str(app_id).strip(),
        "secret": str(secret).strip(),
    }
    return write_verified_entry(app_account(channel), data)


def write_verified_entry(account, data):
    backend = credential_store.write_entry(account, data)
    if credential_store.read_entry(account) != data:
        raise AuthorizationError(
            "credential_write_verification_failed",
            f"credential read-back verification failed for {account}",
        )
    return backend


def _channel_state(state, channel):
    return state.setdefault("channels", {}).setdefault(channel, {
        "authorizations": {},
        "account_index": {},
        "advertiser_index": {},
    })


def rebuild_indexes(channel_state):
    account_index = {}
    advertiser_index = {}
    for authorization_id, metadata in channel_state.get("authorizations", {}).items():
        active_advertisers = []
        for account in metadata.get("authorized_accounts") or []:
            account_id = normalize_id(account["account_id"], "account_id")
            owner = account_index.get(account_id)
            if owner and owner != authorization_id:
                raise AuthorizationError(
                    "authorized_account_conflict",
                    f"authorized account {account_id} belongs to multiple authorizations",
                    account_id=account_id,
                )
            account_index[account_id] = authorization_id
            active_advertisers.extend(account.get("advertiser_ids") or [])
        metadata["advertiser_ids"] = list(dict.fromkeys(
            normalize_id(value, "advertiser_id") for value in active_advertisers
        ))
        for advertiser_id in metadata["advertiser_ids"]:
            normalized = normalize_id(advertiser_id, "advertiser_id")
            advertiser_index.setdefault(normalized, []).append(authorization_id)
    channel_state["account_index"] = account_index
    channel_state["advertiser_index"] = {
        advertiser_id: list(dict.fromkeys(authorization_ids))
        for advertiser_id, authorization_ids in advertiser_index.items()
    }


def _next_generation(channel_state):
    generation = int(channel_state.get("generation") or 0) + 1
    channel_state["generation"] = generation
    return generation


def save_authorization(
    channel,
    token_data,
    authorized_accounts,
    advertiser_ids=None,
    authorization_id=None,
    state_file=None,
    rebind_existing=False,
):
    authorization_id = authorization_id or secrets.token_hex(12)
    with ProcessLock(channel_lock_path(channel, state_file)):
        channel_state = load_channel_state(channel, state_file)
        if not channel_state:
            channel_state = _channel_state(empty_state(), channel)
        accounts = []
        for row in authorized_accounts or []:
            account_id = row.get("account_id") or row.get("advertiser_id")
            if account_id is None:
                continue
            account = {
                key: copy.deepcopy(value)
                for key, value in row.items()
                if key in {"account_name", "account_role", "account_type", "advertiser_name", "is_valid"}
            } | {
                "account_id": normalize_id(account_id, "account_id"),
                "advertiser_ids": [
                    normalize_id(value, "advertiser_id")
                    for value in row.get("advertiser_ids") or []
                ],
            }
            accounts.append(account)
        existing_owners = channel_state.get("account_index") or {}
        conflicts = {
            account["account_id"]: existing_owners[account["account_id"]]
            for account in accounts
            if account["account_id"] in existing_owners
            and existing_owners[account["account_id"]] != authorization_id
        }
        if conflicts and not rebind_existing:
            raise AuthorizationError(
                "authorized_account_conflict",
                "authorized accounts already belong to another authorization; confirm rebind",
                conflicts=conflicts,
            )
        if conflicts:
            for old_authorization_id in set(conflicts.values()):
                old = channel_state.get("authorizations", {}).get(old_authorization_id) or {}
                old["authorized_accounts"] = [
                    row for row in old.get("authorized_accounts") or []
                    if conflicts.get(row.get("account_id")) != old_authorization_id
                ]
        revision = 1
        channel_state.setdefault("authorizations", {})[authorization_id] = {
            "token_revision": revision,
            "authorized_accounts": accounts,
            "advertiser_ids": [],
            "last_authorized_account_sync_at": token_data.get("last_authorized_account_sync_at"),
            "pending_account_sync": bool(token_data.get("pending_account_sync")),
        }
        rebuild_indexes(channel_state)
        _next_generation(channel_state)
        write_verified_entry(authorization_account(channel, authorization_id, revision), {
            key: copy.deepcopy(token_data[key])
            for key in (
                "access_token",
                "refresh_token",
                "access_token_expires_at",
                "refresh_token_expires_at",
                "last_token_update_at",
            )
            if token_data.get(key) is not None
        })
        commit_channel_state(channel, channel_state, state_file)
    return authorization_id


def replace_authorization_snapshot(channel, authorization_id, authorized_accounts, state_file=None):
    with ProcessLock(channel_lock_path(channel, state_file)):
        channel_state = load_channel_state(channel, state_file)
        metadata = (channel_state.get("authorizations") or {}).get(authorization_id)
        if metadata is None:
            raise AuthorizationError("authorization_not_found", f"unknown authorization {authorization_id}")
        current_owner = channel_state.get("account_index") or {}
        rows = []
        for row in authorized_accounts or []:
            account_id = normalize_id(row.get("account_id") or row.get("advertiser_id"), "account_id")
            if current_owner.get(account_id) not in {None, authorization_id}:
                continue
            rows.append({
                key: copy.deepcopy(value)
                for key, value in row.items()
                if key in {"account_name", "account_role", "account_type", "advertiser_name", "is_valid"}
            } | {
                "account_id": account_id,
                "advertiser_ids": [
                    normalize_id(value, "advertiser_id")
                    for value in row.get("advertiser_ids") or []
                ],
            })
        metadata["authorized_accounts"] = rows
        metadata["pending_account_sync"] = False
        rebuild_indexes(channel_state)
        _next_generation(channel_state)
        commit_channel_state(channel, channel_state, state_file)


def migrate_legacy_marketing(state_file=None, legacy=None, authorization_id=None):
    legacy = copy.deepcopy(credential_store.read_credentials() if legacy is None else legacy)
    if not legacy:
        return {"migrated": False, "reason": "legacy_credentials_not_found"}
    app = {key: legacy.get(key) for key in ("app_id", "secret")}
    if app.get("app_id") and app.get("secret"):
        write_app("marketing", app["app_id"], app["secret"])
    if not legacy.get("access_token") and not legacy.get("refresh_token"):
        return {
            "migrated": bool(app.get("app_id") and app.get("secret")),
            "channel": "marketing",
            "authorization_migrated": False,
            "reason": "legacy_token_not_found",
        }
    channel_state = load_channel_state("marketing", state_file)
    existing = channel_state.get("authorizations") or {}
    if authorization_id and authorization_id in existing:
        return {
            "migrated": False,
            "reason": "legacy_authorization_already_migrated",
            "authorization_id": authorization_id,
        }
    if existing:
        return {"migrated": False, "reason": "marketing_authorizations_exist"}
    accounts = []
    for row in legacy.get("oauth_authorized_accounts") or []:
        account_id = row.get("account_id") or row.get("advertiser_id")
        if account_id is None:
            continue
        copied = copy.deepcopy(row)
        role = copied.get("account_role") or copied.get("account_type")
        copied["advertiser_ids"] = [account_id] if role == "ADVERTISER" else []
        accounts.append(copied)
    unresolved = set(str(value) for value in legacy.get("authorized_advertiser_ids") or [])
    resolved = {
        advertiser_id
        for row in accounts
        for advertiser_id in row.get("advertiser_ids") or []
    }
    token_data = {**legacy, "pending_account_sync": bool(unresolved - resolved)}
    authorization_id = save_authorization(
        "marketing",
        token_data,
        accounts,
        authorization_id=authorization_id,
        state_file=state_file,
    )
    return {
        "migrated": True,
        "channel": "marketing",
        "authorization_id": authorization_id,
        "pending_account_sync": bool(unresolved - resolved),
    }


def update_authorization_tokens(channel, authorization_id, token_data, state_file=None):
    with ProcessLock(channel_lock_path(channel, state_file)):
        channel_state = load_channel_state(channel, state_file)
        metadata = (channel_state.get("authorizations") or {}).get(authorization_id)
        if metadata is None:
            raise AuthorizationError("authorization_not_found", f"unknown authorization {authorization_id}")
        previous_revision = int(metadata.get("token_revision") or 1)
        current = credential_store.read_entry(
            authorization_account(channel, authorization_id, previous_revision)
        )
        current.update({key: copy.deepcopy(value) for key, value in token_data.items() if value is not None})
        revision = previous_revision + 1
        write_verified_entry(
            authorization_account(channel, authorization_id, revision),
            current,
        )
        metadata["token_revision"] = revision
        _next_generation(channel_state)
        commit_channel_state(channel, channel_state, state_file)
        return copy.deepcopy(current)


def resolve(
    channel,
    advertiser_id=None,
    auth_account_id=None,
    authorization_id=None,
    allow_pending=False,
    state=None,
):
    state = load_state() if state is None else state
    channel_state = (state.get("channels") or {}).get(channel) or {}
    candidates = []
    if authorization_id is not None:
        if authorization_id in (channel_state.get("authorizations") or {}):
            candidates = [authorization_id]
    elif auth_account_id is not None:
        account_id = normalize_id(auth_account_id, "account_id")
        authorization_id = (channel_state.get("account_index") or {}).get(account_id)
        if authorization_id:
            candidates = [authorization_id]
    elif advertiser_id is not None:
        normalized = normalize_id(advertiser_id, "advertiser_id")
        candidates = list((channel_state.get("advertiser_index") or {}).get(normalized) or [])
    else:
        candidates = list((channel_state.get("authorizations") or {}).keys())

    candidates = list(dict.fromkeys(candidates))
    if not candidates:
        pending = [
            authorization_id
            for authorization_id, metadata in (channel_state.get("authorizations") or {}).items()
            if metadata.get("pending_account_sync")
        ]
        if pending:
            raise AuthorizationError(
                "legacy_authorization_pending_sync",
                f"{channel} legacy authorization requires an authorized-account sync",
                authorization_ids=pending,
            )
        raise AuthorizationError(
            "authorization_not_found",
            f"no {channel} authorization covers the requested account",
            advertiser_id=str(advertiser_id) if advertiser_id is not None else None,
        )
    if len(candidates) > 1:
        raise AuthorizationError(
            "authorization_ambiguous",
            f"multiple {channel} authorizations cover advertiser {advertiser_id}",
            authorization_ids=candidates,
        )
    authorization_id = candidates[0]
    metadata = (channel_state.get("authorizations") or {}).get(authorization_id) or {}
    if metadata.get("pending_account_sync") and not allow_pending:
        raise AuthorizationError(
            "legacy_authorization_pending_sync",
            f"{channel} authorization {authorization_id} requires an authorized-account sync",
            authorization_ids=[authorization_id],
        )
    pending_sync = bool(metadata.get("pending_account_sync"))
    if advertiser_id is not None and not (allow_pending and pending_sync):
        normalized = normalize_id(advertiser_id, "advertiser_id")
        covered_ids = metadata.get("advertiser_ids") or []
        if auth_account_id is not None:
            selected_account = next(
                (
                    row for row in metadata.get("authorized_accounts") or []
                    if row.get("account_id") == normalize_id(auth_account_id, "account_id")
                ),
                None,
            )
            covered_ids = (selected_account or {}).get("advertiser_ids") or []
        if normalized not in covered_ids:
            raise AuthorizationError(
                "authorized_account_not_found",
                f"authorization {authorization_id} does not cover advertiser {normalized}",
            )
    revision = int(metadata.get("token_revision") or 1)
    token = credential_store.read_entry(authorization_account(channel, authorization_id, revision))
    if not token:
        raise AuthorizationError("authorization_not_found", f"missing token for authorization {authorization_id}")
    return authorization_id, metadata, token


def attach_runtime(
    config,
    channel,
    advertiser_id=None,
    auth_account_id=None,
    authorization_id=None,
    allow_pending=False,
):
    runtime = copy.deepcopy(config)
    api = runtime.setdefault("api", {})
    api.update(read_app(channel))
    try:
        authorization_id, metadata, token = resolve(
            channel,
            advertiser_id=advertiser_id,
            auth_account_id=auth_account_id,
            authorization_id=authorization_id,
            allow_pending=allow_pending,
        )
    except AuthorizationError as exc:
        if exc.code == "authorization_not_found":
            return runtime
        raise
    api.update(token)
    api["oauth_authorized_accounts"] = copy.deepcopy(metadata.get("authorized_accounts") or [])
    api["authorized_advertiser_ids"] = copy.deepcopy(metadata.get("advertiser_ids") or [])
    runtime["_authorization"] = {
        "channel": channel,
        "authorization_id": authorization_id,
        "legacy": False,
    }
    return runtime


def status(channel, advertiser_id=None):
    channel_state = load_channel_state(channel)
    token_rows = []
    authorization_rows = []
    for authorization_id, metadata in (channel_state.get("authorizations") or {}).items():
        revision = int(metadata.get("token_revision") or 1)
        token_rows.append(credential_store.read_entry(
            authorization_account(channel, authorization_id, revision)
        ))
        authorization_rows.append({
            "authorization_id": authorization_id,
            "account_ids": [
                row.get("account_id")
                for row in metadata.get("authorized_accounts") or []
                if row.get("account_id") is not None
            ],
            "advertiser_count": len(metadata.get("advertiser_ids") or []),
            "pending_account_sync": bool(metadata.get("pending_account_sync")),
        })
    result = {
        "channel": channel,
        "authorization_count": len(channel_state.get("authorizations") or {}),
        "authorized_account_count": len(channel_state.get("account_index") or {}),
        "authorized_advertiser_count": len(channel_state.get("advertiser_index") or {}),
        "has_app_id": bool(read_app(channel).get("app_id")),
        "has_secret": bool(read_app(channel).get("secret")),
        "generation": int(channel_state.get("generation") or 0),
        "authorization_with_access_token_count": sum(
            not credential_store.is_missing(token.get("access_token")) for token in token_rows
        ),
        "authorization_with_refresh_token_count": sum(
            not credential_store.is_missing(token.get("refresh_token")) for token in token_rows
        ),
        "pending_account_sync_count": sum(
            bool(metadata.get("pending_account_sync"))
            for metadata in (channel_state.get("authorizations") or {}).values()
        ),
        "authorizations": authorization_rows,
    }
    if advertiser_id is not None:
        normalized = str(advertiser_id)
        result["advertiser_id"] = normalized
        result["advertiser_id_authorized"] = bool(
            (channel_state.get("advertiser_index") or {}).get(normalized)
        )
    return result
