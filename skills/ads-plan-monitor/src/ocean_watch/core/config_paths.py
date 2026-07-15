#!/usr/bin/env python3
import json
import os
from pathlib import Path

CONFIG_ENV = "ADS_PLAN_MONITOR_CONFIG"
CODEX_HOME_ENV = "CODEX_HOME"
PLUGIN_MANIFEST = Path(".codex-plugin") / "plugin.json"
SOURCE_MODULE = (
    Path("skills")
    / "ads-plan-monitor"
    / "src"
    / "ocean_watch"
    / "core"
    / "config_paths.py"
)


def skill_root():
    return Path(__file__).resolve().parents[3]


def _ancestor_with(marker, start=None):
    current = Path(start or __file__).resolve()
    if current.is_file():
        current = current.parent
    for candidate in (current, *current.parents):
        if (candidate / marker).exists():
            return candidate
    return None


def codex_home():
    configured = os.environ.get(CODEX_HOME_ENV)
    return Path(configured).expanduser() if configured else Path.home() / ".codex"


def _is_within(path, parent):
    try:
        path.resolve().relative_to(parent.resolve())
        return True
    except ValueError:
        return False


def plugin_root():
    return _ancestor_with(PLUGIN_MANIFEST) or skill_root()


def _is_ocean_watch_checkout(root):
    manifest_path = root / PLUGIN_MANIFEST
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return (
        manifest.get("name") == "ocean-watch"
        and Path(__file__).resolve() == (root / SOURCE_MODULE).resolve()
    )


def repository_root():
    root = _ancestor_with(".git")
    if root is None or not _is_ocean_watch_checkout(root):
        return None
    plugin_cache = codex_home() / "plugins" / "cache"
    if _is_within(Path(__file__), plugin_cache):
        return None
    return root


def project_config_path():
    root = repository_root()
    if root is None:
        return None
    return root / "config" / "ads-plan-monitor" / "config.json"


def home_config_path():
    return codex_home() / "ads-plan-monitor" / "config.json"


def resolve_config_path(explicit=None, prefer_project=False):
    if explicit:
        return Path(explicit).expanduser()
    env_path = os.environ.get(CONFIG_ENV)
    if env_path:
        return Path(env_path).expanduser()
    project_path = project_config_path()
    if project_path is not None and (prefer_project or project_path.exists()):
        return project_path
    return home_config_path()
