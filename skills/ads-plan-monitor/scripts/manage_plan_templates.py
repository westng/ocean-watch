#!/usr/bin/env python3
import argparse
import copy
import json
from pathlib import Path

import config_paths
import plan_templates


def load_config(path):
    return json.loads(path.read_text(encoding="utf-8"))


def save_config(path, config):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def template_name(platform, traffic_source, product_name, product_id):
    return f"{platform}-{traffic_source}-{product_name}-{product_id}"


def list_templates(config):
    rows = []
    for name, template in sorted((config.get("plan_templates") or {}).items()):
        normalized = plan_templates.normalize_template(config, name, template)
        bindings = normalized["bindings"]
        rows.append({
            "name": name,
            "active": name == config.get("active_plan_template"),
            "advertiser_id": bindings.get("advertiser_id"),
            "platform": bindings.get("platform"),
            "traffic_source": bindings.get("traffic_source"),
            "product_id": bindings.get("product_id"),
            "product_name": bindings.get("product_name"),
            "bindings": bindings,
            "legacy": normalized["legacy"],
            "binding_error": plan_templates.binding_error(bindings),
        })
    return rows


def create_template(config, args):
    if int(config.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        config = plan_templates.migrate(config)
    name = args.name or template_name(
        args.platform,
        args.traffic_source,
        args.product_name,
        args.product_id,
    )
    templates = config.setdefault("plan_templates", {})
    if name in templates and not args.force:
        raise ValueError(f"plan template already exists: {name}; use --force to replace it")

    overrides = {}
    if args.from_template:
        source = templates.get(args.from_template)
        if source is None:
            raise ValueError(f"source plan template not found: {args.from_template}")
        normalized_source = plan_templates.normalize_template(config, args.from_template, source)
        source_advertiser_id = normalized_source["bindings"].get("advertiser_id")
        if str(source_advertiser_id) != str(args.advertiser_id):
            raise ValueError(
                f"source plan template {args.from_template} is bound to advertiser "
                f"{source_advertiser_id}; cross-advertiser template cloning is not allowed"
            )
        overrides = copy.deepcopy(normalized_source["overrides"])
    overrides.setdefault("defaults", {}).update({
        "product_name": args.product_name,
        "product_id": args.product_id,
    })
    overrides.setdefault("resolved_ids", {})["unique_product_id"] = str(args.product_id)
    if args.source_name:
        overrides["defaults"]["source"] = args.source_name
    if args.landing_page_url or args.open_url:
        links = overrides.setdefault("links", {})
        if args.landing_page_url:
            links["landing_page_url"] = args.landing_page_url
        if args.open_url:
            links["open_url"] = args.open_url
    if args.track_url or args.action_track_url:
        tracking_urls = overrides.setdefault("tracking_urls", {})
        if args.track_url:
            tracking_urls["track_url"] = [args.track_url]
        if args.action_track_url:
            tracking_urls["action_track_url"] = [args.action_track_url]

    templates[name] = {
        "display_name": name,
        "bindings": {
            "advertiser_id": str(args.advertiser_id),
            "platform": args.platform,
            "traffic_source": args.traffic_source,
            "product_id": str(args.product_id),
            "product_name": args.product_name,
        },
        "overrides": overrides,
    }
    if args.activate or not config.get("active_plan_template"):
        config["active_plan_template"] = name
        config.setdefault("account", {})["advertiser_id"] = str(args.advertiser_id)
    return config, name


def main(argv=None):
    parser = argparse.ArgumentParser(description="Create, migrate, and list bound plan templates.")
    parser.add_argument("--config")
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("list")
    subparsers.add_parser("migrate")

    create = subparsers.add_parser("create")
    create.add_argument("--advertiser-id", required=True)
    create.add_argument("--platform", required=True)
    create.add_argument("--traffic-source", required=True)
    create.add_argument("--product-id", required=True)
    create.add_argument("--product-name", required=True)
    create.add_argument("--name")
    create.add_argument("--source-name")
    create.add_argument("--landing-page-url")
    create.add_argument("--open-url")
    create.add_argument("--track-url")
    create.add_argument("--action-track-url")
    create.add_argument("--from-template")
    create.add_argument("--activate", action="store_true")
    create.add_argument("--force", action="store_true")
    args = parser.parse_args(argv)

    path = config_paths.resolve_config_path(args.config)
    config = load_config(path)
    changed = False
    created_name = None
    if args.command == "migrate":
        config = plan_templates.migrate(config)
        changed = True
    elif args.command == "create":
        config, created_name = create_template(config, args)
        changed = True
    if changed:
        save_config(path, config)

    print(json.dumps({
        "config": str(path),
        "command": args.command,
        "changed": changed,
        "created_template": created_name,
        "active_plan_template": config.get("active_plan_template"),
        "active_template_advertiser_id": next((
            row["advertiser_id"]
            for row in list_templates(config)
            if row["active"]
        ), None),
        "templates": list_templates(config),
    }, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
