#!/usr/bin/env python3
import argparse
import importlib.metadata
import ipaddress
import json
import platform
import re
import shutil
import socket
import subprocess
import sys
import urllib.parse

import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.core.config_paths as config_paths
from ocean_watch.core.output import write_json

MINIMUM_PYTHON = (3, 10)
REQUIRED_F2_VERSION = "0.0.1.7"
MINIMUM_CODEX_CLI = (0, 144, 1)
SUPPORTED_SYSTEMS = {"Darwin", "Linux", "Windows"}
CODEX_VERSION_PATTERN = re.compile(r"(?<!\d)(\d+)\.(\d+)\.(\d+)(?!\d)")


def version_text(version):
    return ".".join(str(part) for part in version)


def check_python():
    current = tuple(sys.version_info[:3])
    minimum = MINIMUM_PYTHON
    ready = current >= minimum
    return {
        "id": "python",
        "required": True,
        "status": "ready" if ready else "blocked",
        "version": version_text(current),
        "minimum_version": version_text(minimum),
        "executable": sys.executable,
        "message": (
            "Python runtime is supported."
            if ready
            else f"Python {version_text(minimum)} or newer is required."
        ),
        "remediation": None if ready else "Install Python 3.10 or newer, then start a new Codex task.",
    }


def check_f2_runtime(version_factory=None):
    version_factory = version_factory or importlib.metadata.version
    try:
        version = str(version_factory("f2") or "").strip()
    except importlib.metadata.PackageNotFoundError:
        version = None
    ready = version == REQUIRED_F2_VERSION
    return {
        "id": "f2_runtime",
        "required": True,
        "status": "ready" if ready else "blocked",
        "version": version,
        "required_version": REQUIRED_F2_VERSION,
        "executable": sys.executable,
        "message": (
            "Pinned F2 runtime is available."
            if ready
            else f"F2 {REQUIRED_F2_VERSION} is required in the current Python runtime."
        ),
        "remediation": (
            None
            if ready
            else "Install the project dependencies into this Python runtime, then rerun setup doctor."
        ),
    }


def check_platform():
    system = platform.system() or "Unknown"
    ready = system in SUPPORTED_SYSTEMS
    return {
        "id": "platform",
        "required": True,
        "status": "ready" if ready else "blocked",
        "system": system,
        "release": platform.release(),
        "machine": platform.machine(),
        "message": (
            "Operating system is supported."
            if ready
            else "Only Windows, macOS, and Linux are supported."
        ),
        "remediation": None if ready else "Use Ocean Watch on Windows, macOS, or Linux.",
    }


def parse_codex_version(output):
    match = CODEX_VERSION_PATTERN.search(str(output or ""))
    return tuple(int(part) for part in match.groups()) if match else None


def check_codex_cli(runner=subprocess.run):
    executable = shutil.which("codex")
    if executable is None:
        return {
            "id": "codex_cli",
            "required": False,
            "status": "warning",
            "available": False,
            "minimum_version": version_text(MINIMUM_CODEX_CLI),
            "message": "Codex CLI was not found on PATH.",
            "remediation": (
                "The Plugin can still run inside Codex, but CLI installation and docs MCP commands "
                "require Codex CLI 0.144.1 or newer on PATH."
            ),
        }
    try:
        result = runner(
            [executable, "--version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError) as error:
        return {
            "id": "codex_cli",
            "required": False,
            "status": "warning",
            "available": True,
            "executable": executable,
            "minimum_version": version_text(MINIMUM_CODEX_CLI),
            "message": "Codex CLI version could not be read.",
            "remediation": str(error)[:256],
        }
    output = (result.stdout or result.stderr or "").strip()
    parsed = parse_codex_version(output)
    ready = result.returncode == 0 and parsed is not None and parsed >= MINIMUM_CODEX_CLI
    return {
        "id": "codex_cli",
        "required": False,
        "status": "ready" if ready else "warning",
        "available": True,
        "executable": executable,
        "version": version_text(parsed) if parsed else None,
        "minimum_version": version_text(MINIMUM_CODEX_CLI),
        "raw_version": output[:128],
        "message": (
            "Codex CLI is available."
            if ready
            else "Codex CLI is unavailable, too old, or returned an unknown version."
        ),
        "remediation": None if ready else "Install or upgrade Codex CLI to 0.144.1 or newer.",
    }


def check_credential_backend():
    backend = credential_store.backend_name()
    if backend == "unavailable":
        status = "blocked"
        message = "No secure credential backend is available for OAuth secrets."
        remediation = (
            "Use macOS Keychain, Windows DPAPI, or Linux Secret Service. "
            "Plaintext fallback is development-only."
        )
    elif backend == "file-fallback":
        status = "warning"
        message = "Development-only plaintext credential fallback is enabled."
        remediation = "Disable plaintext fallback before using a real advertising account."
    else:
        status = "ready"
        message = "Secure credential backend is available."
        remediation = None
    return {
        "id": "credential_backend",
        "required": True,
        "status": status,
        "backend": backend,
        "message": message,
        "remediation": remediation,
    }


def callback_bind_target(redirect_uri):
    parsed = urllib.parse.urlparse(str(redirect_uri or ""))
    if (
        parsed.scheme != "http"
        or parsed.hostname is None
        or parsed.port is None
        or parsed.port <= 0
    ):
        raise ValueError("OAuth callback must be an HTTP loopback URI with an explicit port")
    try:
        addresses = socket.getaddrinfo(
            parsed.hostname,
            parsed.port,
            type=socket.SOCK_STREAM,
        )
    except socket.gaierror as error:
        raise ValueError("OAuth callback host could not be resolved") from error
    if not addresses or not all(is_loopback_address(item[4][0]) for item in addresses):
        raise ValueError("OAuth callback must use a loopback host")
    family, _, _, _, socket_address = addresses[0]
    return parsed.hostname, parsed.port, parsed.path or "/", family, socket_address


def is_loopback_address(value):
    try:
        return ipaddress.ip_address(str(value).split("%", 1)[0]).is_loopback
    except ValueError:
        return False


def check_callback(redirect_uri, socket_factory=socket.socket):
    try:
        host, port, path, address_family, socket_address = callback_bind_target(redirect_uri)
    except ValueError as error:
        return {
            "id": "oauth_callback",
            "required": True,
            "status": "blocked",
            "redirect_uri": redirect_uri,
            "message": str(error),
            "remediation": "Use http://127.0.0.1:8787/oauth/callback in local config and the official console.",
        }
    probe = socket_factory(address_family, socket.SOCK_STREAM)
    try:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        probe.bind(socket_address)
    except OSError as error:
        return {
            "id": "oauth_callback",
            "required": True,
            "status": "blocked",
            "redirect_uri": redirect_uri,
            "host": host,
            "port": port,
            "path": path,
            "message": f"OAuth callback port {port} is unavailable.",
            "remediation": f"Stop the process using {host}:{port}, then rerun the environment check.",
            "error": str(error)[:256],
        }
    finally:
        probe.close()
    return {
        "id": "oauth_callback",
        "required": True,
        "status": "ready",
        "redirect_uri": redirect_uri,
        "host": host,
        "port": port,
        "path": path,
        "message": "OAuth callback host and port are available.",
        "remediation": None,
    }


def default_redirect_uri(config_path=None, channel="marketing"):
    try:
        config = channels.runtime_config(
            load_environment_config(config_path),
            channel,
            capability="oauth",
        )
        return (config.get("oauth") or {}).get("redirect_uri") or channels.CHANNELS[channel][
            "redirect_uri"
        ]
    except (OSError, ValueError, KeyError):
        return channels.CHANNELS[channel]["redirect_uri"]


def load_environment_config(config_path=None):
    path = config_paths.resolve_config_path(config_path)
    if not path.is_file():
        return {}
    return json.loads(path.read_text(encoding="utf-8"))


def environment_report(config_path=None, channel="marketing", redirect_uri=None):
    selected_channel, _ = channels.get(channel, capability="oauth")
    callback_uri = redirect_uri or default_redirect_uri(config_path, selected_channel)
    checks = [
        check_python(),
        *([check_f2_runtime()] if selected_channel == "qianchuan" else []),
        check_platform(),
        check_codex_cli(),
        check_credential_backend(),
        check_callback(callback_uri),
    ]
    blockers = [item["id"] for item in checks if item["required"] and item["status"] == "blocked"]
    warnings = [item["id"] for item in checks if item["status"] == "warning"]
    return {
        "ok": not blockers,
        "mode": "environment_check",
        "channel": selected_channel,
        "checks": checks,
        "blocking_checks": blockers,
        "warnings": warnings,
        "next_action": "ready" if not blockers else "resolve_blocking_checks",
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Check the local Ocean Watch runtime environment.")
    parser.add_argument("--config")
    parser.add_argument("--channel", default="marketing", choices=("marketing", "qianchuan"))
    parser.add_argument("--redirect-uri")
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    args = parser.parse_args(argv)
    result = environment_report(
        args.config,
        channel=args.channel,
        redirect_uri=args.redirect_uri,
    )
    write_json(result, args.out)
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
