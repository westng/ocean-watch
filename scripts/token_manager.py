#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import json
import os
import time
import urllib.error
import urllib.request
from pathlib import Path

import credential_store


PLACEHOLDER_PREFIX = "REPLACE_WITH"
SECRET_KEYS = {"access_token", "refresh_token", "secret", "auth_code"}
DEFAULT_OAUTH_BASE_URL = "https://ad.oceanengine.com/open_api"
ACCESS_TOKEN_PATH = "/oauth2/access_token/"
REFRESH_TOKEN_PATH = "/oauth2/refresh_token/"
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


def load_config(config_path):
    path = Path(config_path).expanduser()
    config = json.loads(path.read_text(encoding="utf-8"))
    return credential_store.merge_credentials(config)


def save_credentials(config):
    return credential_store.write_credentials(credential_store.extract_credentials(config))


class FileLock:
    def __init__(self, path, timeout=DEFAULT_LOCK_TIMEOUT_SECONDS):
        self.path = Path(path)
        self.timeout = timeout
        self.fd = None

    def __enter__(self):
        deadline = time.monotonic() + self.timeout
        self.path.parent.mkdir(parents=True, exist_ok=True)
        while True:
            try:
                self.fd = os.open(str(self.path), os.O_CREAT | os.O_EXCL | os.O_RDWR)
                os.write(self.fd, str(os.getpid()).encode("utf-8"))
                return self
            except FileExistsError:
                if time.monotonic() > deadline:
                    raise TimeoutError(f"Timed out waiting for token lock: {self.path}")
                time.sleep(0.2)

    def __exit__(self, exc_type, exc, tb):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None
        try:
            self.path.unlink()
        except FileNotFoundError:
            pass


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
    if token_data.get("advertiser_ids") is not None:
        api["authorized_advertiser_ids"] = token_data.get("advertiser_ids")
    return updated


def exchange_auth_code(config_path, auth_code, config=None):
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
    updated = update_token_fields(config, data)
    save_credentials(updated)
    return updated, redact({
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "access_token_expires_at": get_path(updated, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(updated, "api.refresh_token_expires_at"),
        "authorized_advertiser_ids": get_path(updated, "api.authorized_advertiser_ids"),
    })


def refresh_access_token(config_path, config=None):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    missing = required_oauth_missing(config, include_refresh=True)
    if missing:
        raise RuntimeError("missing OAuth refresh fields: " + ", ".join(missing))

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
    updated = update_token_fields(config, data)
    save_credentials(updated)
    return updated, redact({
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "access_token_expires_at": get_path(updated, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(updated, "api.refresh_token_expires_at"),
        "authorized_advertiser_ids": get_path(updated, "api.authorized_advertiser_ids"),
    })


def ensure_access_token(config_path, config=None, force_refresh=False, margin_seconds=DEFAULT_REFRESH_MARGIN_SECONDS):
    config_path = Path(config_path).expanduser()
    config = copy.deepcopy(config) if config is not None else load_config(config_path)
    if not force_refresh and token_has_ttl(config, margin_seconds=margin_seconds):
        return config

    lock_path = config_path.with_suffix(config_path.suffix + ".token.lock")
    with FileLock(lock_path):
        locked_config = load_config(config_path)
        if not force_refresh and token_has_ttl(locked_config, margin_seconds=margin_seconds):
            return locked_config
        refreshed, _ = refresh_access_token(config_path, locked_config)
        return refreshed


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="config/ads-plan-monitor/config.json")
    parser.add_argument("--refresh", action="store_true", help="Force refresh using api.refresh_token.")
    parser.add_argument("--status", action="store_true", help="Print redacted token status.")
    args = parser.parse_args()

    config_path = Path(args.config)
    config = load_config(config_path)
    if args.refresh:
        config = ensure_access_token(config_path, config, force_refresh=True)

    status = {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "config": str(config_path),
        "has_access_token": not is_missing(get_path(config, "api.access_token")),
        "has_refresh_token": not is_missing(get_path(config, "api.refresh_token")),
        "has_app_id": not is_missing(get_path(config, "api.app_id")),
        "has_secret": not is_missing(get_path(config, "api.secret")),
        "access_token_expires_at": get_path(config, "api.access_token_expires_at"),
        "refresh_token_expires_at": get_path(config, "api.refresh_token_expires_at"),
        "token_has_ttl": token_has_ttl(config),
        "authorized_advertiser_count": len(get_path(config, "api.authorized_advertiser_ids", []) or []),
        "advertiser_id_authorized": get_path(config, "account.advertiser_id") in (get_path(config, "api.authorized_advertiser_ids", []) or []),
        "credential_backend": credential_store.backend_name(),
        "project_config_has_sensitive_fields": credential_store.status(config_path).get("project_config_has_sensitive_fields"),
    }
    print(json.dumps(status, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
