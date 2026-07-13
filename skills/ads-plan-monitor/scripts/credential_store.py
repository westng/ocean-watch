#!/usr/bin/env python3
import argparse
import base64
import getpass
import json
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

import config_store


SERVICE = "ads-plan-monitor"
ACCOUNT = "oceanengine-oauth"
PLACEHOLDER_PREFIX = "REPLACE_WITH"
INSECURE_FALLBACK_ENV = "ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK"
SENSITIVE_KEYS = {
    "app_id",
    "developer_id",
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
    return Path.home() / ".codex" / "ads-plan-monitor"


def fallback_path():
    return credentials_dir() / "credentials.json"


def has_command(command):
    return shutil.which(command) is not None


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


def macos_read():
    result = subprocess.run(
        ["security", "find-generic-password", "-s", SERVICE, "-a", ACCOUNT, "-w"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
    )
    if result.returncode != 0:
        return {}
    return decode_stored_credentials(result.stdout)


def macos_write(data):
    subprocess.run(
        ["security", "delete-generic-password", "-s", SERVICE, "-a", ACCOUNT],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    subprocess.run(
        [
            "security",
            "add-generic-password",
            "-U",
            "-s",
            SERVICE,
            "-a",
            ACCOUNT,
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


def windows_read():
    path = fallback_path().with_suffix(".dpapi")
    if not path.exists():
        return {}
    return windows_unprotect(path.read_text(encoding="utf-8").strip())


def windows_write(data):
    path = fallback_path().with_suffix(".dpapi")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(windows_protect(data) + "\n", encoding="utf-8")
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def linux_read():
    result = subprocess.run(
        ["secret-tool", "lookup", "service", SERVICE, "account", ACCOUNT],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
    )
    if result.returncode != 0:
        return {}
    text = result.stdout.strip()
    return json.loads(text) if text else {}


def linux_write(data):
    subprocess.run(
        ["secret-tool", "store", "--label", "Ads Plan Monitor OceanEngine OAuth", "service", SERVICE, "account", ACCOUNT],
        input=json.dumps(data, ensure_ascii=False),
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )


def fallback_read():
    path = fallback_path()
    if not path.exists():
        return {}
    return json.loads(path.read_text(encoding="utf-8"))


def fallback_write(data):
    path = fallback_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def read_credentials():
    backend = backend_name()
    if backend == "macos-keychain":
        return macos_read()
    if backend == "windows-dpapi":
        return windows_read()
    if backend == "linux-secret-service":
        return linux_read()
    if backend == "file-fallback":
        return fallback_read()
    return {}


def write_credentials(data):
    backend = backend_name()
    if backend == "macos-keychain":
        macos_write(data)
    elif backend == "windows-dpapi":
        windows_write(data)
    elif backend == "linux-secret-service":
        linux_write(data)
    elif backend == "file-fallback":
        fallback_write(data)
    else:
        raise RuntimeError(
            "No secure credential backend is available. Install secret-tool/libsecret, "
            f"or set {INSECURE_FALLBACK_ENV}=1 to explicitly allow a plaintext fallback."
        )
    return backend


def merge_credentials(config, credentials=None):
    credentials = read_credentials() if credentials is None else credentials
    merged = json.loads(json.dumps(config, ensure_ascii=False))
    api = merged.setdefault("api", {})
    for key in API_CREDENTIAL_KEYS:
        if key in credentials and not is_missing(credentials[key]):
            api[key] = credentials[key]
    return merged


def extract_credentials(config):
    api = config.get("api") or {}
    return {
        key: api[key]
        for key in SENSITIVE_KEYS
        if key in api and not is_missing(api.get(key))
    }


def strip_sensitive_config(config):
    cleaned = json.loads(json.dumps(config, ensure_ascii=False))
    api = cleaned.setdefault("api", {})
    for key in SENSITIVE_KEYS:
        api.pop(key, None)
    return cleaned


def migrate_from_config(config_path):
    path = Path(config_path).expanduser()
    config = json.loads(path.read_text(encoding="utf-8"))
    existing = read_credentials()
    extracted = extract_credentials(config)
    merged_credentials = {**existing, **extracted}
    backend = None
    if merged_credentials:
        backend = write_credentials(merged_credentials)
    cleaned = strip_sensitive_config(config)
    config_store.atomic_write_json(path, cleaned)
    return {
        "config": str(path),
        "backend": backend or backend_name(),
        "migrated_keys": sorted(extracted.keys()),
        "config_sensitive_fields_removed": sorted(extracted.keys()),
    }


def configure_app(app_id=None, secret=None):
    credentials = read_credentials()
    if app_id:
        credentials["app_id"] = app_id
    if secret:
        credentials["secret"] = secret
    backend = write_credentials(credentials)
    return {
        "backend": backend,
        "has_app_id": not is_missing(credentials.get("app_id")),
        "has_secret": not is_missing(credentials.get("secret")),
    }


def configure_developer_id(developer_id):
    credentials = read_credentials()
    credentials["developer_id"] = str(developer_id).strip()
    backend = write_credentials(credentials)
    return {
        "backend": backend,
        "has_developer_id": not is_missing(credentials.get("developer_id")),
    }


def status(config_path=None):
    backend = backend_name()
    credentials = read_credentials()
    result = {
        "backend": backend,
        "credential_location": (
            str(fallback_path())
            if backend == "file-fallback"
            else "system credential store"
            if backend != "unavailable"
            else None
        ),
        "secure_backend_available": backend not in {"file-fallback", "unavailable"},
        "insecure_file_fallback": backend == "file-fallback",
        "has_app_id": not is_missing(credentials.get("app_id")),
        "has_developer_id": not is_missing(credentials.get("developer_id")),
        "has_secret": not is_missing(credentials.get("secret")),
        "has_access_token": not is_missing(credentials.get("access_token")),
        "has_refresh_token": not is_missing(credentials.get("refresh_token")),
        "access_token_expires_at": credentials.get("access_token_expires_at"),
        "refresh_token_expires_at": credentials.get("refresh_token_expires_at"),
        "last_token_update_at": credentials.get("last_token_update_at"),
        "oauth_authorized_account_count": len(credentials.get("oauth_authorized_accounts") or []),
        "authorized_advertiser_count": len(credentials.get("authorized_advertiser_ids") or []),
        "last_authorized_account_sync_at": credentials.get("last_authorized_account_sync_at"),
    }
    if config_path:
        config = json.loads(Path(config_path).expanduser().read_text(encoding="utf-8"))
        advertiser_id = (config.get("account") or {}).get("advertiser_id")
        result["advertiser_id"] = advertiser_id
        result["advertiser_id_authorized"] = any(
            str(item) == str(advertiser_id)
            for item in (credentials.get("authorized_advertiser_ids") or [])
        )
        result["project_config_has_sensitive_fields"] = sorted(
            key for key in SENSITIVE_KEYS if key in (config.get("api") or {})
        )
    return result


def main():
    import config_paths

    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--status", action="store_true")
    parser.add_argument("--migrate-config", action="store_true")
    parser.add_argument("--set-app", action="store_true")
    parser.add_argument("--app-id")
    parser.add_argument("--secret")
    args = parser.parse_args()
    config_path = config_paths.resolve_config_path(args.config)

    if args.migrate_config:
        print(json.dumps(migrate_from_config(config_path), ensure_ascii=False, indent=2))
        return 0

    if args.set_app:
        app_id = args.app_id or input("OceanEngine APP_ID: ").strip()
        secret = args.secret or getpass.getpass("OceanEngine Secret: ").strip()
        print(json.dumps(configure_app(app_id, secret), ensure_ascii=False, indent=2))
        return 0

    print(json.dumps(status(config_path), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
