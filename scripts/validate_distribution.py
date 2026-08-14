#!/usr/bin/env python3
import json
import re
import stat
import sys
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]
TARGETS = (
    "ocean-watch_darwin_amd64",
    "ocean-watch_darwin_arm64",
    "ocean-watch_linux_amd64",
    "ocean-watch_linux_arm64",
    "ocean-watch_windows_amd64.exe",
)


def fail(message):
    raise RuntimeError(message)


def validate_manifest():
    manifest = json.loads((ROOT / ".codex-plugin" / "plugin.json").read_text(encoding="utf-8"))
    if manifest.get("name") != "ocean-watch" or manifest.get("skills") != "./skills/":
        fail("plugin manifest identity is invalid")
    if manifest.get("mcpServers") != "./.mcp.json":
        fail("plugin manifest MCP contract is invalid")
    version = str(manifest.get("version") or "")
    if re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:\+codex\.[A-Za-z0-9.-]+)?", version) is None:
        fail("plugin manifest version is invalid")


def validate_mcp():
    payload = json.loads((ROOT / ".mcp.json").read_text(encoding="utf-8"))
    if set(payload) != {"mcpServers"}:
        fail("MCP manifest has unsupported top-level fields")
    servers = payload.get("mcpServers")
    if not isinstance(servers, dict) or set(servers) != {"ocean-watch"}:
        fail("MCP manifest must contain only the ocean-watch server")
    server = servers["ocean-watch"]
    expected = {
        "command": "./.codex-plugin/bin/ocean-watch_darwin_arm64",
        "args": ["mcp", "serve", "--stdio"],
        "cwd": ".",
    }
    if server != expected:
        fail("MCP server must use the fixed darwin/arm64 Gate 0 command")
    command = server["command"].lower()
    if command in {"sh", "bash", "zsh", "cmd", "cmd.exe", "powershell", "pwsh"}:
        fail("MCP server must not start through a shell")
    target = (ROOT / server["command"]).resolve()
    binary_root = (ROOT / ".codex-plugin" / "bin").resolve()
    if target.parent != binary_root or not target.is_file():
        fail("MCP server command escaped the prepared runtime directory")
    if target.stat().st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        fail("MCP server executable is writable by group or other")
    for path in (ROOT / ".mcp.json", ROOT / ".codex-plugin" / "plugin.json"):
        if path.stat().st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            fail(f"MCP manifest file is writable by group or other: {path.name}")


def validate_skill(name):
    root = ROOT / "skills" / name
    content = (root / "SKILL.md").read_text(encoding="utf-8")
    match = re.match(r"^---\n(.*?)\n---", content, re.DOTALL)
    if match is None:
        fail(f"{name} has invalid frontmatter")
    metadata = yaml.safe_load(match.group(1))
    if metadata.get("name") != name or not str(metadata.get("description") or "").strip():
        fail(f"{name} has invalid metadata")
    interface = yaml.safe_load((root / "agents" / "openai.yaml").read_text(encoding="utf-8"))
    prompt = str(((interface or {}).get("interface") or {}).get("default_prompt") or "")
    if f"${name}" not in prompt:
        fail(f"{name} default_prompt does not mention the Skill")
    dependencies = ((interface or {}).get("dependencies") or {}).get("tools")
    expected_dependency = [{
        "type": "mcp",
        "value": "ocean-watch",
        "description": "Ocean Watch local read-only template tools",
        "transport": "stdio",
    }]
    if dependencies != expected_dependency:
        fail(f"{name} MCP dependency contract is invalid")
    if "MCP `list_templates`" not in content or "MCP `get_template`" not in content:
        fail(f"{name} does not route template reads through MCP")
    if "Never search the repository" not in content or "silent fallback" not in content:
        fail(f"{name} does not fail closed when MCP template tools are unavailable")
    if not (root / "run.cmd").is_file():
        fail(f"{name} Windows launcher is missing")
    launcher = root / "run"
    if not launcher.is_file() or not launcher.stat().st_mode & stat.S_IXUSR:
        fail(f"{name} Unix launcher is missing or not executable")


def validate_binaries():
    binary_root = ROOT / ".codex-plugin" / "bin"
    actual = sorted(path.name for path in binary_root.iterdir() if path.is_file())
    if actual != sorted(TARGETS):
        fail(f"prepared runtime set is invalid: {actual}")
    for name in TARGETS:
        path = binary_root / name
        if path.stat().st_size == 0:
            fail(f"prepared runtime is empty: {name}")
        if not name.endswith(".exe") and not path.stat().st_mode & stat.S_IXUSR:
            fail(f"prepared runtime is not executable: {name}")


def main():
    try:
        validate_manifest()
        validate_mcp()
        validate_skill("ads-plan-monitor")
        validate_skill("qc-plan-monitor")
        validate_binaries()
    except (OSError, ValueError, RuntimeError, yaml.YAMLError) as error:
        print(f"distribution validation failed: {error}", file=sys.stderr)
        return 1
    print("distribution validation passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
