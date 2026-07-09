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


SERVICE = "ads-plan-monitor"
ACCOUNT = "oceanengine-oauth"
PLACEHOLDER_PREFIX = "REPLACE_WITH"
SENSITIVE_KEYS = {
    "app_id",
    "secret",
    "access_token",
    "refresh_token",
    "access_token_expires_at",
    "refresh_token_expires_at",
    "last_token_update_at",
    "authorized_advertiser_ids",
}


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
            return "none"
    if has_command("secret-tool"):
        return "linux-secret-service"
    return "file-fallback"


def redact(data):
    if not isinstance(data, dict):
        return data
    redacted = {}
    for key, value in data.items():
        if key in SENSITIVE_KEYS and not is_missing(value):
            if key == "authorized_advertiser_ids":
                redacted[key] = f"<{len(value)} authorized advertisers>"
            else:
                redacted[key] = "<redacted>"
        else:
            redacted[key] = value
    return redacted


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
    text = result.stdout.strip()
    return json.loads(text) if text else {}


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
    return fallback_read()


def write_credentials(data):
    backend = backend_name()
    if backend == "macos-keychain":
        macos_write(data)
    elif backend == "windows-dpapi":
        windows_write(data)
    elif backend == "linux-secret-service":
        linux_write(data)
    else:
        fallback_write(data)
    return backend


def merge_credentials(config, credentials=None):
    credentials = read_credentials() if credentials is None else credentials
    merged = json.loads(json.dumps(config, ensure_ascii=False))
    api = merged.setdefault("api", {})
    for key in SENSITIVE_KEYS:
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
    path.write_text(json.dumps(cleaned, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
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


def status(config_path=None):
    credentials = read_credentials()
    result = {
        "backend": backend_name(),
        "credential_location": "system credential store" if backend_name() != "file-fallback" else str(fallback_path()),
        "has_app_id": not is_missing(credentials.get("app_id")),
        "has_secret": not is_missing(credentials.get("secret")),
        "has_access_token": not is_missing(credentials.get("access_token")),
        "has_refresh_token": not is_missing(credentials.get("refresh_token")),
        "access_token_expires_at": credentials.get("access_token_expires_at"),
        "refresh_token_expires_at": credentials.get("refresh_token_expires_at"),
        "authorized_advertiser_count": len(credentials.get("authorized_advertiser_ids") or []),
    }
    if config_path:
        config = json.loads(Path(config_path).expanduser().read_text(encoding="utf-8"))
        advertiser_id = (config.get("account") or {}).get("advertiser_id")
        result["advertiser_id"] = advertiser_id
        result["advertiser_id_authorized"] = advertiser_id in (credentials.get("authorized_advertiser_ids") or [])
        result["project_config_has_sensitive_fields"] = sorted(
            key for key in SENSITIVE_KEYS if key in (config.get("api") or {})
        )
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="config/ads-plan-monitor/config.json")
    parser.add_argument("--status", action="store_true")
    parser.add_argument("--migrate-config", action="store_true")
    parser.add_argument("--set-app", action="store_true")
    parser.add_argument("--app-id")
    parser.add_argument("--secret")
    args = parser.parse_args()

    if args.migrate_config:
        print(json.dumps(migrate_from_config(args.config), ensure_ascii=False, indent=2))
        return 0

    if args.set_app:
        app_id = args.app_id or input("OceanEngine APP_ID: ").strip()
        secret = args.secret or getpass.getpass("OceanEngine Secret: ").strip()
        print(json.dumps(configure_app(app_id, secret), ensure_ascii=False, indent=2))
        return 0

    print(json.dumps(status(args.config), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
