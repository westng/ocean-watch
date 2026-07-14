#!/usr/bin/env python3
import os
from pathlib import Path

CONFIG_ENV = "ADS_PLAN_MONITOR_CONFIG"


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


def plugin_root():
    return _ancestor_with(Path(".codex-plugin") / "plugin.json") or skill_root()


def repository_root():
    return _ancestor_with(".git")


def project_config_path():
    root = repository_root()
    if root is None:
        return None
    return root / "config" / "ads-plan-monitor" / "config.json"


def home_config_path():
    return Path.home() / ".codex" / "ads-plan-monitor" / "config.json"


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
