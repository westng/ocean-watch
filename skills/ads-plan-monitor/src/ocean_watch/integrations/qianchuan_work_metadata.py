#!/usr/bin/env python3
import argparse
import copy
import urllib.parse
from pathlib import Path

from ocean_watch.core import config_paths, config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json

INTEGRATION_KEY = "qianchuan_work_metadata"


def validate_endpoint(value):
    endpoint = str(value or "").strip()
    if not endpoint:
        return None
    parsed = urllib.parse.urlsplit(endpoint)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.fragment
    ):
        raise ConfigurationError(
            "Qianchuan work metadata endpoint must be a credential-free HTTPS URL"
        )
    try:
        _ = parsed.port
    except ValueError as error:
        raise ConfigurationError(
            "Qianchuan work metadata endpoint has an invalid port"
        ) from error
    return urllib.parse.urlunsplit(parsed)


def endpoint_from_config(config):
    integrations = config.get("integrations") if isinstance(config, dict) else None
    section = (
        integrations.get(INTEGRATION_KEY)
        if isinstance(integrations, dict)
        else None
    )
    endpoint = section.get("endpoint") if isinstance(section, dict) else None
    return validate_endpoint(endpoint)


def is_configured(config):
    integrations = config.get("integrations") if isinstance(config, dict) else None
    section = (
        integrations.get(INTEGRATION_KEY)
        if isinstance(integrations, dict)
        else None
    )
    endpoint = section.get("endpoint") if isinstance(section, dict) else None
    return bool(str(endpoint or "").strip())


def status(config_path):
    config_path = Path(config_path)
    endpoint = endpoint_from_config(config_store.load_json(config_path))
    return {
        "ok": True,
        "mode": "qianchuan_work_metadata_status",
        "config": str(config_path.resolve()),
        "configured": endpoint is not None,
        "endpoint": "<configured locally>" if endpoint else None,
    }


def set_endpoint(config_path, endpoint):
    endpoint = validate_endpoint(endpoint)
    if endpoint is None:
        raise ConfigurationError("--endpoint cannot be empty")

    def updater(config):
        updated = copy.deepcopy(config)
        integrations = updated.setdefault("integrations", {})
        if not isinstance(integrations, dict):
            raise ConfigurationError("config integrations must be an object")
        integrations[INTEGRATION_KEY] = {"endpoint": endpoint}
        return updated, None

    config_store.update_json(config_path, updater)
    return status(config_path)


def clear_endpoint(config_path):
    def updater(config):
        updated = copy.deepcopy(config)
        integrations = updated.get("integrations")
        if isinstance(integrations, dict):
            integrations.pop(INTEGRATION_KEY, None)
            if not integrations:
                updated.pop("integrations", None)
        return updated, None

    config_store.update_json(config_path, updater)
    return status(config_path)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Configure the optional local Qianchuan work metadata endpoint."
    )
    parser.add_argument("--config")
    parser.add_argument("--home-config", action="store_true")
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument("--endpoint")
    actions.add_argument("--clear", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if args.config and args.home_config:
        raise ConfigurationError("--config and --home-config cannot be used together")
    config_path = (
        config_paths.home_config_path()
        if args.home_config
        else config_paths.resolve_config_path(args.config)
    )
    if not config_path.is_file():
        raise ConfigurationError(
            "local config does not exist; run setup init before configuring integrations",
            {"config": str(config_path)},
        )
    if args.endpoint is not None:
        result = set_endpoint(config_path, args.endpoint)
    elif args.clear:
        result = clear_endpoint(config_path)
    else:
        result = status(config_path)
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
