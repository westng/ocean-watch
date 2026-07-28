#!/usr/bin/env python3
"""Standard-library compatibility launcher for both Ocean Watch Skills."""

from __future__ import annotations

import importlib
import json
import os
import platform
import stat
import subprocess
import sys
from pathlib import Path

POLICY_LIMIT = 64 * 1024
POLICY_NAME = "runtime-policy.json"
PLUGIN_MANIFEST_NAME = "plugin.json"
BOOTSTRAP_NAMES = {
    ("darwin", "arm64"): "ocean-watch-bootstrap_darwin_arm64",
    ("darwin", "amd64"): "ocean-watch-bootstrap_darwin_amd64",
    ("linux", "arm64"): "ocean-watch-bootstrap_linux_arm64",
    ("linux", "amd64"): "ocean-watch-bootstrap_linux_amd64",
    ("windows", "amd64"): "ocean-watch-bootstrap_windows_amd64.exe",
}


class LauncherError(RuntimeError):
    pass


def _read_json(path: Path) -> dict:
    try:
        if path.stat().st_size > POLICY_LIMIT:
            raise LauncherError(f"launcher metadata is too large: {path.name}")
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise LauncherError(f"launcher metadata is unreadable: {path.name}") from error
    if not isinstance(value, dict):
        raise LauncherError(f"launcher metadata must be an object: {path.name}")
    return value


def _plugin_version(plugin_root: Path) -> str:
    manifest = _read_json(plugin_root / ".codex-plugin" / PLUGIN_MANIFEST_NAME)
    version = str(manifest.get("version") or "").strip()
    if not version:
        raise LauncherError("Plugin version is missing")
    return version


def _runtime_policy(plugin_root: Path) -> dict:
    path = plugin_root / ".codex-plugin" / POLICY_NAME
    if not path.exists():
        return {"schema_version": 1, "enabled": False}
    policy = _read_json(path)
    if policy.get("schema_version") != 1 or not isinstance(policy.get("enabled"), bool):
        raise LauncherError("runtime policy is malformed")
    return policy


def _route(arguments: list[str], commands: set[str]) -> str | None:
    if arguments == ["--version"]:
        return "--version"
    if len(arguments) < 2 or arguments[0].startswith("-") or arguments[1].startswith("-"):
        return None
    candidate = f"{arguments[0]} {arguments[1]}"
    return candidate if candidate in commands else None


def _platform_key() -> tuple[str, str]:
    if sys.platform == "darwin":
        goos = "darwin"
    elif sys.platform.startswith("linux"):
        goos = "linux"
    elif sys.platform in {"win32", "cygwin"}:
        goos = "windows"
    else:
        raise LauncherError(f"unsupported runtime operating system: {sys.platform}")
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        goarch = "amd64"
    elif machine in {"arm64", "aarch64"}:
        goarch = "arm64"
    else:
        raise LauncherError(f"unsupported runtime architecture: {machine or 'unknown'}")
    if (goos, goarch) not in BOOTSTRAP_NAMES:
        raise LauncherError(f"unsupported runtime platform: {goos}-{goarch}")
    return goos, goarch


def _bootstrap_path(plugin_root: Path) -> Path:
    goos, goarch = _platform_key()
    path = (
        plugin_root
        / ".codex-plugin"
        / "runtime"
        / "bootstrap"
        / BOOTSTRAP_NAMES[(goos, goarch)]
    )
    try:
        details = path.lstat()
    except FileNotFoundError as error:
        raise LauncherError(f"runtime bootstrap is missing for {goos}-{goarch}") from error
    if stat.S_ISLNK(details.st_mode) or not stat.S_ISREG(details.st_mode):
        raise LauncherError("runtime bootstrap is not a regular file")
    if goos != "windows" and not os.access(path, os.X_OK):
        raise LauncherError("runtime bootstrap is not executable")
    return path


def _python_fallback(plugin_root: Path, arguments: list[str]) -> int:
    source_root = plugin_root / "skills" / "ads-plan-monitor" / "src"
    sys.path.insert(0, str(source_root))
    main = importlib.import_module("ocean_watch.cli.main").main
    return int(main(arguments) or 0)


def _probe_bootstrap(bootstrap: Path, route: str) -> tuple[int, dict | None]:
    try:
        completed = subprocess.run(
            [str(bootstrap), "--route", route],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            check=False,
        )
    except OSError as error:
        raise LauncherError("runtime bootstrap could not be started") from error
    if completed.returncode != 0:
        sys.stdout.buffer.write(completed.stdout)
        sys.stderr.buffer.write(completed.stderr)
        return completed.returncode, None
    try:
        payload = json.loads(completed.stdout.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise LauncherError("runtime bootstrap returned malformed output") from error
    result = payload.get("result") if isinstance(payload, dict) and payload.get("ok") is True else None
    if not isinstance(result, dict) or result.get("route") not in {"python", "go"}:
        raise LauncherError("runtime bootstrap returned an invalid route")
    return 0, result


def _execute_bootstrap(bootstrap: Path, route: str, arguments: list[str]) -> int:
    command = [str(bootstrap), "--route", route, "--execute", "--", *arguments]
    if os.name != "nt":
        os.execv(str(bootstrap), command)
        raise AssertionError("os.execv returned unexpectedly")
    try:
        return subprocess.run(command, check=False).returncode
    except OSError as error:
        raise LauncherError("runtime bootstrap could not be started") from error


def launch(plugin_root: Path, arguments: list[str] | None = None) -> int:
    arguments = list(sys.argv[1:] if arguments is None else arguments)
    plugin_root = plugin_root.resolve()
    try:
        policy = _runtime_policy(plugin_root)
        if not policy["enabled"]:
            return _python_fallback(plugin_root, arguments)
        plugin_version = _plugin_version(plugin_root)
        product_version = plugin_version.split("+", 1)[0]
        if policy.get("plugin_version") != plugin_version:
            raise LauncherError("runtime policy Plugin version mismatch")
        if policy.get("product_version") != product_version:
            raise LauncherError("runtime policy product version mismatch")
        commands = policy.get("commands")
        if not isinstance(commands, list) or not all(isinstance(value, str) for value in commands):
            raise LauncherError("runtime policy command list is malformed")
        route = _route(arguments, set(commands))
        if route is None:
            return _python_fallback(plugin_root, arguments)
        bootstrap = _bootstrap_path(plugin_root)
        exit_code, result = _probe_bootstrap(bootstrap, route)
        if result is None:
            return exit_code
        if result["route"] == "python":
            return _python_fallback(plugin_root, arguments)
        return _execute_bootstrap(bootstrap, route, arguments)
    except LauncherError as error:
        payload = {
            "ok": False,
            "error": {"code": "runtime_launcher_failed", "message": str(error), "details": {}},
        }
        print(json.dumps(payload, ensure_ascii=False), file=sys.stderr)
        return 2


def main() -> int:
    return launch(Path(__file__).resolve().parents[1])


if __name__ == "__main__":
    raise SystemExit(main())
