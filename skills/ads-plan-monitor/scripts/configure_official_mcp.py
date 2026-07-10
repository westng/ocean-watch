#!/usr/bin/env python3
import argparse
import getpass
import json
import shutil
import subprocess
import sys
from pathlib import Path

import credential_store
import oceanengine_mcp_bridge


SERVER_NAME = "oceanengine-developer-docs"
BRIDGE_SCRIPT = "oceanengine_mcp_bridge.py"


def is_configured(value):
    return not credential_store.is_missing(value)


def bridge_path():
    return Path(__file__).resolve().with_name(BRIDGE_SCRIPT)


def transport(server):
    value = server.get("transport") if isinstance(server, dict) else None
    return value if isinstance(value, dict) else {}


def is_current_bridge(server):
    value = transport(server)
    args = value.get("args") or []
    command = value.get("command")
    return bool(
        value.get("type") == "stdio"
        and isinstance(command, str)
        and Path(command).resolve() == Path(sys.executable).resolve()
        and len(args) == 1
        and Path(args[0]).resolve() == bridge_path()
        and bridge_path().is_file()
    )


def registration_arguments(server):
    value = transport(server)
    if value.get("type") == "streamable_http" and isinstance(value.get("url"), str):
        return ["mcp", "add", SERVER_NAME, "--url", value["url"]]
    if value.get("type") == "stdio" and isinstance(value.get("command"), str):
        args = value.get("args") or []
        if all(isinstance(item, str) for item in args):
            return ["mcp", "add", SERVER_NAME, "--", value["command"], *args]
    return None


def run_codex(arguments, check=False):
    executable = shutil.which("codex")
    if executable is None:
        raise RuntimeError("Codex CLI was not found on PATH")
    return subprocess.run(
        [executable, *arguments],
        check=check,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def get_server():
    try:
        result = run_codex(["mcp", "get", SERVER_NAME, "--json"])
    except RuntimeError:
        return None
    if result.returncode != 0:
        return None
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError:
        return None
    return payload if isinstance(payload, dict) else None


def status(credentials=None):
    credentials = credential_store.read_credentials() if credentials is None else credentials
    app_id = credentials.get("app_id")
    developer_id = credentials.get("developer_id")
    server = get_server()
    matches = bool(
        is_current_bridge(server)
        and is_configured(app_id)
        and is_configured(developer_id)
    )
    return {
        "server": SERVER_NAME,
        "codex_cli_available": shutil.which("codex") is not None,
        "has_app_id": is_configured(app_id),
        "has_developer_id": is_configured(developer_id),
        "registered": server is not None,
        "uses_sse_bridge": is_current_bridge(server),
        "ready": matches,
        "next_action": (
            "ready"
            if matches
            else "save_app_credentials"
            if not is_configured(app_id)
            else "configure_developer_id"
            if not is_configured(developer_id)
            else "register_mcp"
        ),
    }


def configure(developer_id=None):
    credentials = credential_store.read_credentials()
    app_id = credentials.get("app_id")
    if not is_configured(app_id):
        raise RuntimeError("APP_ID is missing; save app credentials before configuring the MCP")

    developer_id = str(developer_id or credentials.get("developer_id") or "").strip()
    if not developer_id:
        developer_id = getpass.getpass("OceanEngine developer_id: ").strip()
    if not developer_id:
        raise RuntimeError("developer_id is required")

    verified_tools = oceanengine_mcp_bridge.probe(app_id, developer_id)
    existing = get_server()
    if is_current_bridge(existing):
        credential_store.configure_developer_id(developer_id)
        result = status({**credentials, "developer_id": developer_id})
        result["verified_tool_count"] = len(verified_tools)
        return result
    previous_registration = registration_arguments(existing)

    if existing is not None:
        remove = run_codex(["mcp", "remove", SERVER_NAME])
        if remove.returncode != 0:
            raise RuntimeError("Unable to replace the existing official MCP registration")

    add = run_codex([
        "mcp",
        "add",
        SERVER_NAME,
        "--",
        sys.executable,
        str(bridge_path()),
    ])
    if add.returncode != 0:
        if previous_registration:
            run_codex(previous_registration)
        raise RuntimeError("Unable to register the official Ocean Engine documentation MCP")
    try:
        credential_store.configure_developer_id(developer_id)
    except Exception:
        run_codex(["mcp", "remove", SERVER_NAME])
        if previous_registration:
            run_codex(previous_registration)
        raise
    result = status({**credentials, "developer_id": developer_id})
    result["verified_tool_count"] = len(verified_tools)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Configure Ocean Engine's official developer-documentation MCP for Codex."
    )
    parser.add_argument("--developer-id", help="Developer ID; prompts securely when omitted.")
    parser.add_argument("--status", action="store_true", help="Show redacted MCP readiness.")
    args = parser.parse_args(argv)

    try:
        result = status() if args.status else configure(args.developer_id)
    except RuntimeError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False, indent=2))
        return 1
    print(json.dumps({"ok": bool(result["ready"]), **result}, ensure_ascii=False, indent=2))
    return 0 if result["ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
