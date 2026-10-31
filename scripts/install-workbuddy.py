#!/usr/bin/env python3
"""Install the verified local runtime into WorkBuddy, using shared real state.

Does not change native trust decisions, credentials, business data, or start apps.
"""
import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import tempfile
from datetime import datetime
from pathlib import Path

SKILL_CHANNELS = {
    "ads-plan-monitor": "marketing",
    "qc-plan-monitor": "qianchuan",
    "xt-plan-monitor": "star_map",
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".ocean-watch-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def render_skill(root, name):
    content = (root / "skills" / name / "SKILL.md").read_text(encoding="utf-8")
    description = re.search(r"^description: (.+)$", content, re.M).group(1)
    routes = content.split("<!-- capability-routes:start -->", 1)[1].split("<!-- capability-routes:end -->", 1)[0]
    rows = [line for line in routes.splitlines() if line.startswith("|") and "MCP `" in line]
    channel = SKILL_CHANNELS[name]
    channel_guidance = (
        "星图账号使用 subject_id 和 subject_kind=star_account；授权管理工具的旧 advertiser_id 参数填写同一星图账号 ID。"
        "星图模板、计划、素材和报表尚未接入；能力目录中的 shared 条目不能作为星图支持证明。"
        if channel == "star_map" else ""
    )
    if channel == "qianchuan":
        channel_guidance += (
            "用户说配置 F2、初始化 F2、设置或更新抖音 Cookie，或返回 F2_CONFIGURATION_REQUIRED 时，"
            "调用 start_f2_configuration，参数为空对象。打开返回的 configuration_url，Cookie 仅在本机页面填写并保存至系统凭据库。"
            "使用 session_id 查询 get_f2_configuration_session；completed 且 credential_saved=true、can_resume=true 后重试原解析或预检。"
            "若会话未找到，读取 error.details.configuration_status；configured=true 则重试原请求。会话只在原 Runtime 进程内，不能据此认定 Cookie 未保存或要求重填。"
            "HTTP 403 或 f2_http_error 同样不能证明保存失败，应按具体请求错误继续诊断。"
            "预检 ok=true 或存在 preflight_id 不代表业务通过；须检查每组 status、counts、failed_results、skipped 和 query_failures，仅 would_create、would_append、already_present 为已完成预检。"
            "保存成功不证明 Cookie 未过期或作品可访问。用户取消时调用 cancel_f2_configuration_session，失败、取消或过期则停止自动重试。"
        )
    template = (root / "hosts/workbuddy/skill.md").read_text(encoding="utf-8")
    for key, value in {"SKILL": name, "DESCRIPTION": description, "CHANNEL": channel, "CHANNEL_GUIDANCE": channel_guidance, "ROUTES": "\n".join(rows)}.items():
        template = template.replace("{{" + key + "}}", value)
    return template


def install(root, host, state_home):
    system = {"Darwin": "darwin", "Linux": "linux", "Windows": "windows"}.get(platform.system())
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "AMD64": "amd64"}.get(platform.machine())
    binary = f"ocean-watch_{system}_{arch}" + (".exe" if system == "windows" else "")
    manifest_path = root / ".codex-plugin/runtime-manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest_digest = digest(manifest_path)
    plugin = root / ".codex-plugin/plugin.json"
    identity = json.loads(plugin.read_text(encoding="utf-8"))
    if (manifest.get("schema_version") != 1 or identity.get("name") != "ocean-watch"
            or manifest.get("plugin", {}).get("name") != "ocean-watch"
            or identity.get("version") != manifest.get("version")
            or manifest.get("plugin", {}).get("version") != manifest.get("version")):
        raise ValueError("runtime plugin identity or schema is invalid")
    if digest(plugin) != manifest["plugin"]["sha256"]:
        raise ValueError("plugin manifest does not match the runtime manifest")
    source_binary = root / ".codex-plugin/bin" / binary
    expected = manifest["sha256"].get(binary)
    if not expected or source_binary.is_symlink() or digest(source_binary) != expected:
        raise ValueError("runtime binary is unsupported or unverified; rebuild first")
    for resource in ("f2/resolve.py", *(f"skills/{name}/SKILL.md" for name in SKILL_CHANNELS)):
        if digest(root / resource) != manifest["resources"].get(resource):
            raise ValueError(f"runtime resources are stale; rebuild first: {resource}")
    rendered = {name: render_skill(root, name) for name in SKILL_CHANNELS}
    config_path = host / "mcp.json"
    config = json.loads(config_path.read_text(encoding="utf-8")) if config_path.exists() else {}
    servers = config.setdefault("mcpServers", {})
    if not isinstance(servers, dict):
        raise ValueError("mcpServers must be an object")
    version = manifest["version"]
    if not re.fullmatch(r"[A-Za-z0-9.+_-]+", version):
        raise ValueError("invalid runtime version")
    destination = host / "binaries/ocean-watch" / f"{version}-{manifest_digest[:12]}"
    payload_paths = (".codex-plugin/plugin.json", ".codex-plugin/runtime-manifest.json", ".codex-plugin/bin/" + binary, "f2/resolve.py")
    destination.parent.mkdir(parents=True, exist_ok=True)
    if not destination.exists():
        staged = Path(tempfile.mkdtemp(prefix=".install-", dir=destination.parent))
        try:
            for resource in payload_paths:
                target = staged / resource
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(root / resource, target)
                target.chmod(0o700 if resource.endswith(binary) else 0o600)
            os.replace(staged, destination)
        finally:
            if staged.exists():
                shutil.rmtree(staged)
    for resource in payload_paths:
        target = destination / resource
        if target.is_symlink() or digest(target) != digest(root / resource):
            raise ValueError("existing pinned installation is damaged")
    stamp = datetime.now().astimezone().strftime("%Y%m%dT%H%M%S%f%z")
    backup = host / "backups/ocean-watch" / stamp
    backup.mkdir(parents=True, mode=0o700)
    if config_path.exists():
        shutil.copy2(config_path, backup / "mcp.json")
        (backup / "mcp.json").chmod(0o600)
    for name in (*SKILL_CHANNELS, "ocean-watch-preview"):
        path = host / "skills" / name
        if path.exists():
            shutil.copytree(path, backup / name)
    # Install all channel Skills before switching the one local stdio service.
    for name, content in rendered.items():
        directory = host / "skills" / name
        directory.mkdir(parents=True, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix=".SKILL-", dir=directory)
        try:
            with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
                stream.write(content)
            os.replace(temporary, directory / "SKILL.md")
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    servers.pop("ocean-watch-preview", None)
    servers["ocean-watch"] = {
        "type": "stdio", "command": str(destination / ".codex-plugin/bin" / binary),
        "args": ["mcp", "serve", "--stdio"], "cwd": str(destination),
        "env": {"HOME": str(Path.home()), "OCEAN_WATCH_HOME": str(state_home), "CODEX_HOME": str(state_home),
                "OCEAN_WATCH_MANAGED_RUNTIME": "1", "PATH": os.environ.get("PATH", os.defpath)},
    }
    write_json(config_path, config)
    preview = host / "skills/ocean-watch-preview"
    if preview.exists():
        shutil.rmtree(preview)
    metadata = {"version": version, "sha256": expected, "manifest_sha256": manifest_digest, "installed_at": stamp,
                "state_home": str(state_home), "backup": str(backup), "data_mode": "shared_local_state", "native_acceptance": "pending"}
    write_json(destination / "installation.json", metadata)
    return {"install": str(destination), "backup": str(backup), "version": version, "sha256": expected, "manifest_sha256": manifest_digest}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbuddy-home", type=Path, default=Path.home() / ".workbuddy")
    parser.add_argument("--state-home", type=Path, default=Path(os.environ.get("OCEAN_WATCH_HOME") or os.environ.get("CODEX_HOME") or Path.home() / ".codex"))
    args = parser.parse_args()
    print(json.dumps(install(Path(__file__).resolve().parents[1], args.workbuddy_home.expanduser().resolve(), args.state_home.expanduser().resolve()), ensure_ascii=False))


if __name__ == "__main__":
    main()
