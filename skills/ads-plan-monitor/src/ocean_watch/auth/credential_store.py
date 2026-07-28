#!/usr/bin/env python3
import argparse
import base64
import json
import os
import platform
import shutil
import subprocess
from pathlib import Path

import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store

SERVICE = "ads-plan-monitor"
ACCOUNT = "oceanengine-oauth"
PLACEHOLDER_PREFIX = "REPLACE_WITH"
INSECURE_FALLBACK_ENV = "ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK"
SENSITIVE_KEYS = {
    "app_id",
    "developer_id",
    "secret",
    "auth_code",
    "access_token",
    "refresh_token",
    "access_token_expires_at",
    "refresh_token_expires_at",
    "last_token_update_at",
    "last_authorized_account_sync_at",
    "oauth_authorized_accounts",
    "authorized_advertiser_ids",
}
API_CREDENTIAL_KEYS = SENSITIVE_KEYS - {"developer_id"}


def is_missing(value):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith(PLACEHOLDER_PREFIX)
    if isinstance(value, list):
        return len(value) == 0
    return False


def credentials_dir():
    return config_paths.codex_home() / "ads-plan-monitor"


def fallback_path(account=ACCOUNT):
    suffix = "credentials" if account == ACCOUNT else account.replace("/", "-")
    return credentials_dir() / f"{suffix}.json"


def has_command(command):
    return shutil.which(command) is not None


def command_path(command):
    resolved = shutil.which(command)
    if not resolved:
        raise RuntimeError(f"required credential command is unavailable: {command}")
    return resolved


def backend_name():
    system = platform.system()
    if system == "Darwin" and has_command("security"):
        return "macos-keychain"
    if system == "Windows":
        try:
            import ctypes  # noqa: F401
            return "windows-dpapi"
        except Exception:
            return "unavailable"
    if has_command("secret-tool"):
        return "linux-secret-service"
    if os.environ.get(INSECURE_FALLBACK_ENV) == "1":
        return "file-fallback"
    return "unavailable"


def redact(data):
    if not isinstance(data, dict):
        return data
    redacted = {}
    for key, value in data.items():
        if key in SENSITIVE_KEYS and not is_missing(value):
            if key == "authorized_advertiser_ids":
                redacted[key] = f"<{len(value)} authorized advertisers>"
            elif key == "oauth_authorized_accounts":
                redacted[key] = f"<{len(value)} OAuth authorized accounts>"
            else:
                redacted[key] = "<redacted>"
        else:
            redacted[key] = value
    return redacted


def decode_stored_credentials(text):
    stripped = text.strip()
    if not stripped:
        return {}
    try:
        return json.loads(stripped)
    except json.JSONDecodeError as json_error:
        if len(stripped) % 2 == 0 and all(char in "0123456789abcdefABCDEF" for char in stripped):
            try:
                return json.loads(bytes.fromhex(stripped).decode("utf-8"))
            except (UnicodeDecodeError, ValueError, json.JSONDecodeError):
                pass
        raise RuntimeError("Stored credentials are not valid JSON or hex-encoded JSON") from json_error


def macos_read(account=ACCOUNT):
    result = subprocess.run(
        [command_path("security"), "find-generic-password", "-s", SERVICE, "-a", account, "-w"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
    )
    if result.returncode != 0:
        return {}
    return decode_stored_credentials(result.stdout)


def macos_write(data, account=ACCOUNT):
    subprocess.run(
        [command_path("security"), "delete-generic-password", "-s", SERVICE, "-a", account],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    subprocess.run(
        [
            command_path("security"),
            "add-generic-password",
            "-U",
            "-s",
            SERVICE,
            "-a",
            account,
            "-w",
            json.dumps(data, ensure_ascii=False),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )


def windows_protect(data):
    import ctypes
    from ctypes import wintypes

    class DATA_BLOB(ctypes.Structure):
        _fields_ = [("cbData", wintypes.DWORD), ("pbData", ctypes.POINTER(ctypes.c_char))]

    payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
    in_buffer = ctypes.create_string_buffer(payload)
    in_blob = DATA_BLOB(len(payload), ctypes.cast(in_buffer, ctypes.POINTER(ctypes.c_char)))
    out_blob = DATA_BLOB()
    if not ctypes.windll.crypt32.CryptProtectData(
        ctypes.byref(in_blob),
        None,
        None,
        None,
        None,
        0,
        ctypes.byref(out_blob),
    ):
        raise OSError("CryptProtectData failed")
    try:
        encrypted = ctypes.string_at(out_blob.pbData, out_blob.cbData)
    finally:
        ctypes.windll.kernel32.LocalFree(out_blob.pbData)
    return base64.b64encode(encrypted).decode("ascii")


def windows_unprotect(text):
    import ctypes
    from ctypes import wintypes

    class DATA_BLOB(ctypes.Structure):
        _fields_ = [("cbData", wintypes.DWORD), ("pbData", ctypes.POINTER(ctypes.c_char))]

    encrypted = base64.b64decode(text.encode("ascii"))
    in_buffer = ctypes.create_string_buffer(encrypted)
    in_blob = DATA_BLOB(len(encrypted), ctypes.cast(in_buffer, ctypes.POINTER(ctypes.c_char)))
    out_blob = DATA_BLOB()
    if not ctypes.windll.crypt32.CryptUnprotectData(
        ctypes.byref(in_blob),
        None,
        None,
        None,
        None,
        0,
        ctypes.byref(out_blob),
    ):
        raise OSError("CryptUnprotectData failed")
    try:
        decrypted = ctypes.string_at(out_blob.pbData, out_blob.cbData)
    finally:
        ctypes.windll.kernel32.LocalFree(out_blob.pbData)
    return json.loads(decrypted.decode("utf-8"))


def windows_read(account=ACCOUNT):
    path = fallback_path(account).with_suffix(".dpapi")
    if not path.exists():
        return {}
    return windows_unprotect(path.read_text(encoding="utf-8").strip())


def windows_write(data, account=ACCOUNT):
    path = fallback_path(account).with_suffix(".dpapi")
    config_store.atomic_write_text(path, windows_protect(data) + "\n", backup=False)


def linux_read(account=ACCOUNT):
    result = subprocess.run(
        [command_path("secret-tool"), "lookup", "service", SERVICE, "account", account],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
    )
    if result.returncode != 0:
        return {}
    text = result.stdout.strip()
    return json.loads(text) if text else {}


def linux_write(data, account=ACCOUNT):
    subprocess.run(
        [command_path("secret-tool"), "store", "--label", "Ads Plan Monitor OceanEngine OAuth", "service", SERVICE, "account", account],
        input=json.dumps(data, ensure_ascii=False),
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )


def fallback_read(account=ACCOUNT):
    path = fallback_path(account)
    if not path.exists():
        return {}
    return json.loads(path.read_text(encoding="utf-8"))


def fallback_write(data, account=ACCOUNT):
    path = fallback_path(account)
    config_store.atomic_write_text(
        path,
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        backup=False,
    )


def read_entry(account=ACCOUNT):
    backend = backend_name()
    if backend == "macos-keychain":
        return macos_read(account)
    if backend == "windows-dpapi":
        return windows_read(account)
    if backend == "linux-secret-service":
        return linux_read(account)
    if backend == "file-fallback":
        return fallback_read(account)
    return {}


def write_entry(account, data):
    backend = backend_name()
    if backend == "macos-keychain":
        macos_write(data, account)
    elif backend == "windows-dpapi":
        windows_write(data, account)
    elif backend == "linux-secret-service":
        linux_write(data, account)
    elif backend == "file-fallback":
        fallback_write(data, account)
    else:
        raise RuntimeError(
            "No secure credential backend is available. Install secret-tool/libsecret, "
            f"or set {INSECURE_FALLBACK_ENV}=1 to explicitly allow a plaintext fallback."
        )
    return backend


def read_credentials():
    return read_entry(ACCOUNT)


def write_credentials(data):
    return write_entry(ACCOUNT, data)


def merge_credentials(config, credentials=None):
    credentials = read_credentials() if credentials is None else credentials
    merged = json.loads(json.dumps(config, ensure_ascii=False))
    api = merged.setdefault("api", {})
    for key in API_CREDENTIAL_KEYS:
        if key in credentials and not is_missing(credentials[key]):
            api[key] = credentials[key]
    return merged


def extract_credentials(config, channel="marketing"):
    sources = [config.get("api") or {}]
    channel_config = (config.get("channels") or {}).get(channel) or {}
    sources.append(channel_config.get("api") or {})
    extracted = {}
    for source in sources:
        for key in SENSITIVE_KEYS:
            value = source.get(key)
            if is_missing(value):
                continue
            if key in extracted and extracted[key] != value:
                raise ValueError(f"conflicting {channel} credential field: {key}")
            extracted[key] = value
    return extracted


def strip_sensitive_config(config):
    cleaned = json.loads(json.dumps(config, ensure_ascii=False))
    api_nodes = [cleaned.setdefault("api", {})]
    for channel_config in (cleaned.get("channels") or {}).values():
        if isinstance(channel_config, dict) and isinstance(channel_config.get("api"), dict):
            api_nodes.append(channel_config["api"])
    for api in api_nodes:
        for key in SENSITIVE_KEYS:
            api.pop(key, None)
    return cleaned


def sensitive_config_fields(config):
    fields = []
    for key in SENSITIVE_KEYS:
        if key in (config.get("api") or {}):
            fields.append(f"api.{key}")
    for channel, channel_config in (config.get("channels") or {}).items():
        api = channel_config.get("api") if isinstance(channel_config, dict) else None
        for key in SENSITIVE_KEYS:
            if isinstance(api, dict) and key in api:
                fields.append(f"channels.{channel}.api.{key}")
    return sorted(fields)


def migrate_from_config(config_path):
    path = Path(config_path).expanduser()
    with config_store.json_file_lock(path):
        config = config_store.load_json(path)
        existing = read_credentials()
        extracted = extract_credentials(config)
        merged_credentials = {**existing, **extracted}
        backend = None
        if merged_credentials:
            backend = write_credentials(merged_credentials)
        cleaned = strip_sensitive_config(config)
        config_store.atomic_write_json(path, cleaned, backup=False)
        path.with_suffix(path.suffix + ".bak").unlink(missing_ok=True)
    return {
        "config": str(path),
        "backend": backend or backend_name(),
        "migrated_keys": sorted(extracted.keys()),
        "config_sensitive_fields_removed": sorted(extracted.keys()),
    }


def configure_app(app_id=None, secret=None, channel="marketing"):
    import ocean_watch.auth.authorization_store as authorization_store
    import ocean_watch.auth.channels as channels

    channels.get(channel, capability="oauth")
    current = authorization_store.read_app(channel)
    app_id = app_id or current.get("app_id")
    secret = secret or current.get("secret")
    if is_missing(app_id) or is_missing(secret):
        raise ValueError("app_id and secret are required")
    backend = authorization_store.write_app(channel, app_id, secret)
    return {
        "backend": backend,
        "channel": channel,
        "has_app_id": True,
        "has_secret": True,
    }


def configure_developer_id(developer_id):
    credentials = read_credentials()
    credentials["developer_id"] = str(developer_id).strip()
    backend = write_credentials(credentials)
    return {
        "backend": backend,
        "has_developer_id": not is_missing(credentials.get("developer_id")),
    }


def status(config_path=None, channel="marketing"):
    import ocean_watch.auth.authorization_store as authorization_store

    backend = backend_name()
    channel_status = authorization_store.status(channel)
    legacy = read_credentials()
    result = {
        "backend": backend,
        "channel": channel,
        "credential_location": (
            str(fallback_path())
            if backend == "file-fallback"
            else "system credential store"
            if backend != "unavailable"
            else None
        ),
        "secure_backend_available": backend not in {"file-fallback", "unavailable"},
        "insecure_file_fallback": backend == "file-fallback",
        "has_developer_id": not is_missing(legacy.get("developer_id")),
        **channel_status,
    }
    if config_path:
        config = json.loads(Path(config_path).expanduser().read_text(encoding="utf-8"))
        advertiser_id = (config.get("account") or {}).get("advertiser_id")
        result["advertiser_id"] = advertiser_id
        result.update(authorization_store.status(channel, advertiser_id=advertiser_id))
        result["project_config_has_sensitive_fields"] = sensitive_config_fields(config)
    return result


def main(argv=None):
    import ocean_watch.core.config_paths as config_paths

    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--status", action="store_true")
    parser.add_argument("--migrate-config", action="store_true")
    parser.add_argument("--channel", default="marketing", choices=("marketing", "qianchuan"))
    args = parser.parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)

    if args.migrate_config:
        print(json.dumps(migrate_from_config(config_path), ensure_ascii=False, indent=2))
        return 0

    print(json.dumps(status(config_path, channel=args.channel), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
