#!/usr/bin/env python3
import os
from pathlib import Path


CONFIG_ENV = "ADS_PLAN_MONITOR_CONFIG"


def skill_root():
    return Path(__file__).resolve().parents[1]


def project_config_path():
    return skill_root() / "config" / "ads-plan-monitor" / "config.json"


def home_config_path():
    return Path.home() / ".codex" / "ads-plan-monitor" / "config.json"


def resolve_config_path(explicit=None, prefer_project=False):
    if explicit:
        return Path(explicit).expanduser()
    env_path = os.environ.get(CONFIG_ENV)
    if env_path:
        return Path(env_path).expanduser()
    project_path = project_config_path()
    if prefer_project or project_path.exists():
        return project_path
    return home_config_path()
