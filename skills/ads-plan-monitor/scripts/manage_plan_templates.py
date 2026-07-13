#!/usr/bin/env python3
import argparse
import copy
import json
from pathlib import Path

import config_paths
import plan_templates


ACCOUNT_SCOPED_RESOLVED_IDS = {
    "brand_info",
    "product_platform_id",
    "event_asset_ids",
    "product_image_ids",
    "landing_page_asset_id",
}


def load_config(path):
    return json.loads(path.read_text(encoding="utf-8"))


def save_config(path, config):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def template_name(platform, traffic_source, product_name, product_id):
    return f"{platform}-{traffic_source}-{product_name}-{product_id}"


def normalize_titles(titles):
    normalized = []
    for title in titles or []:
        value = str(title).strip()
        if value and value not in normalized:
            normalized.append(value)
    if not normalized:
        raise ValueError("at least one non-empty copy title is required")
    return normalized


def sanitize_cross_advertiser_overrides(overrides):
    sanitized = copy.deepcopy(overrides)
    removed = []
    materials = sanitized.get("materials") or {}
    for field in sorted(materials):
        removed.append(f"materials.{field}")
    sanitized["materials"] = {}

    resolved_ids = sanitized.get("resolved_ids") or {}
    for field in sorted(ACCOUNT_SCOPED_RESOLVED_IDS):
        if field in resolved_ids:
            resolved_ids.pop(field, None)
            removed.append(f"resolved_ids.{field}")
    sanitized["resolved_ids"] = resolved_ids
    return sanitized, removed


def list_templates(config):
    rows = []
    for name, template in sorted((config.get("plan_templates") or {}).items()):
        normalized = plan_templates.normalize_template(config, name, template)
        bindings = normalized["bindings"]
        titles = normalized["copy_materials"].get("titles") or []
        rows.append({
            "name": name,
            "active": name == config.get("active_plan_template"),
            "advertiser_id": bindings.get("advertiser_id"),
            "platform": bindings.get("platform"),
            "traffic_source": bindings.get("traffic_source"),
            "product_id": bindings.get("product_id"),
            "product_name": bindings.get("product_name"),
            "copy_materials": {
                "configured": bool(titles),
                "title_count": len(titles),
                "titles": titles,
                "copied_from_template": normalized["copy_materials"].get(
                    "copied_from_template"
                ),
            },
            "bindings": bindings,
            "legacy": normalized["legacy"],
            "binding_error": plan_templates.binding_error(bindings),
        })
    return rows


def default_template_summary(config):
    base = plan_templates.default_bundle(config)
    return {
        "name": "default_plan_template",
        "type": "creation_base_only",
        "business_usable": False,
        "selectable_for_plan_creation": False,
        "purpose": "Base configuration for the business-template creation wizard.",
        "sections": sorted(key for key, value in base.items() if value),
    }


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
    inherited_copy_materials = {"titles": []}
    source_metadata = {
        "type": "default",
        "template": None,
        "cross_advertiser": False,
        "cleared_account_fields": [],
    }
    if args.from_template:
        source = templates.get(args.from_template)
        if source is None:
            raise ValueError(f"source plan template not found: {args.from_template}")
        normalized_source = plan_templates.normalize_template(config, args.from_template, source)
        source_advertiser_id = normalized_source["bindings"].get("advertiser_id")
        cross_advertiser = str(source_advertiser_id) != str(args.advertiser_id)
        if cross_advertiser and not getattr(args, "allow_cross_advertiser_clone", False):
            raise ValueError(
                f"source plan template {args.from_template} is bound to advertiser "
                f"{source_advertiser_id}; cross-advertiser template cloning is not allowed"
            )
        overrides = copy.deepcopy(normalized_source["overrides"])
        inherited_copy_materials = copy.deepcopy(normalized_source["copy_materials"])
        source_metadata = {
            "type": "business_template",
            "template": args.from_template,
            "cross_advertiser": cross_advertiser,
            "cleared_account_fields": [],
        }
        if cross_advertiser:
            overrides, removed = sanitize_cross_advertiser_overrides(overrides)
            source_metadata["cleared_account_fields"] = removed
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
    copy_materials = {
        "titles": normalize_titles(args.title),
    } if args.title else inherited_copy_materials

    templates[name] = {
        "display_name": name,
        "bindings": {
            "advertiser_id": str(args.advertiser_id),
            "platform": args.platform,
            "traffic_source": args.traffic_source,
            "product_id": str(args.product_id),
            "product_name": args.product_name,
        },
        "copy_materials": copy_materials,
        "created_from": source_metadata,
        "overrides": overrides,
    }
    if args.activate:
        config["active_plan_template"] = name
        config.setdefault("account", {})["advertiser_id"] = str(args.advertiser_id)
    return config, name


def prompt_value(input_fn, label, default=None, required=False):
    suffix = f" [{default}]" if default not in (None, "") else ""
    while True:
        value = input_fn(f"{label}{suffix}: ").strip()
        if value:
            return value
        if default not in (None, ""):
            return str(default)
        if not required:
            return None


def prompt_yes_no(input_fn, label, default=False):
    hint = "Y/n" if default else "y/N"
    value = input_fn(f"{label} [{hint}]: ").strip().lower()
    if not value:
        return default
    return value in {"y", "yes", "是"}


def select_template_source(config, input_fn, output_fn):
    names = sorted((config.get("plan_templates") or {}).keys())
    output_fn("创建来源：")
    output_fn("  0. 默认模板（仅作为新业务模板骨架）")
    for index, name in enumerate(names, start=1):
        template = plan_templates.normalize_template(config, name, config["plan_templates"][name])
        advertiser_id = template["bindings"].get("advertiser_id")
        output_fn(f"  {index}. {name}（广告主 {advertiser_id}）")
    while True:
        raw = input_fn("请选择来源编号: ").strip()
        if raw.isdigit() and 0 <= int(raw) <= len(names):
            return None if raw == "0" else names[int(raw) - 1]


def collect_titles(input_fn, inherited_titles):
    if inherited_titles and prompt_yes_no(
        input_fn,
        f"复制来源模板的 {len(inherited_titles)} 条文案",
        default=True,
    ):
        return copy.deepcopy(inherited_titles)
    titles = []
    while True:
        title = input_fn("输入文案标题（留空结束）: ").strip()
        if not title:
            break
        titles.append(title)
    return normalize_titles(titles) if titles else []


def run_create_wizard(config, input_fn=input, output_fn=print):
    if int(config.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        config = plan_templates.migrate(config)
    source_name = select_template_source(config, input_fn, output_fn)
    source = None
    if source_name:
        source = plan_templates.normalize_template(
            config,
            source_name,
            config["plan_templates"][source_name],
        )
    bindings = (source or {}).get("bindings") or {}
    overrides = (source or {}).get("overrides") or {}
    defaults = overrides.get("defaults") or {}
    links = overrides.get("links") or {}
    tracking = overrides.get("tracking_urls") or {}

    advertiser_id = prompt_value(
        input_fn,
        "广告主 ID",
        bindings.get("advertiser_id") or (config.get("account") or {}).get("advertiser_id"),
        required=True,
    )
    platform = prompt_value(input_fn, "平台", bindings.get("platform"), required=True)
    traffic_source = prompt_value(
        input_fn,
        "流量来源",
        bindings.get("traffic_source") or "CID",
        required=True,
    )
    product_name = prompt_value(input_fn, "商品名称", bindings.get("product_name"), required=True)
    product_id = prompt_value(input_fn, "商品 ID", bindings.get("product_id"), required=True)
    generated_name = template_name(platform, traffic_source, product_name, product_id)
    name = prompt_value(input_fn, "模板名称", generated_name, required=True)

    source_titles = ((source or {}).get("copy_materials") or {}).get("titles") or []
    titles = collect_titles(input_fn, source_titles)
    arguments = argparse.Namespace(
        advertiser_id=advertiser_id,
        platform=platform,
        traffic_source=traffic_source,
        product_id=product_id,
        product_name=product_name,
        name=name,
        source_name=prompt_value(input_fn, "计划来源", defaults.get("source")),
        landing_page_url=prompt_value(input_fn, "落地页链接", links.get("landing_page_url")),
        open_url=prompt_value(input_fn, "直达链接", links.get("open_url")),
        track_url=prompt_value(
            input_fn,
            "展示监测链接",
            (tracking.get("track_url") or [None])[0],
        ),
        action_track_url=prompt_value(
            input_fn,
            "点击/有效触点监测链接",
            (tracking.get("action_track_url") or [None])[0],
        ),
        title=titles,
        from_template=source_name,
        activate=False,
        force=False,
        allow_cross_advertiser_clone=True,
    )
    candidate, created_name = create_template(copy.deepcopy(config), arguments)
    created = candidate["plan_templates"][created_name]
    preview = {
        "action": "create_business_template",
        "template": created_name,
        "source": created["created_from"],
        "bindings": created["bindings"],
        "copy_title_count": len(created["copy_materials"].get("titles") or []),
        "override_sections": sorted(created["overrides"]),
        "activate": False,
    }
    output_fn("创建前预览：")
    output_fn(json.dumps(preview, ensure_ascii=False, indent=2))
    if not prompt_yes_no(input_fn, "确认创建此业务模板", default=False):
        return config, {**preview, "confirmed": False, "changed": False}
    activate = prompt_yes_no(input_fn, "创建后设为当前模板", default=False)
    if activate:
        candidate["active_plan_template"] = created_name
        candidate.setdefault("account", {})["advertiser_id"] = str(advertiser_id)
    return candidate, {**preview, "confirmed": True, "changed": True, "activate": activate}


def set_copy_materials(config, template_name, titles=None, from_template=None):
    templates = config.get("plan_templates") or {}
    if template_name not in templates:
        raise ValueError(f"plan template not found: {template_name}")
    template = templates[template_name]
    if int(config.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        raise ValueError("migrate the config before setting copy materials")
    if bool(titles) == bool(from_template):
        raise ValueError("provide either titles or one source template")
    copy_materials = {}
    if from_template:
        source = templates.get(from_template)
        if source is None:
            raise ValueError(f"source plan template not found: {from_template}")
        source_template = plan_templates.normalize_template(config, from_template, source)
        copy_materials["titles"] = normalize_titles(
            source_template["copy_materials"].get("titles")
        )
        copy_materials["copied_from_template"] = from_template
    else:
        copy_materials["titles"] = normalize_titles(titles)
    template["copy_materials"] = copy_materials
    (template.get("overrides") or {}).pop("titles", None)
    return config


def main(argv=None):
    parser = argparse.ArgumentParser(description="Create, migrate, and list bound plan templates.")
    parser.add_argument("--config")
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("list")
    subparsers.add_parser("migrate")
    subparsers.add_parser("create-wizard")

    set_copy = subparsers.add_parser("set-copy")
    set_copy.add_argument("--template", required=True)
    copy_source = set_copy.add_mutually_exclusive_group(required=True)
    copy_source.add_argument(
        "--title",
        action="append",
        help="Promotion copy title; repeat this option to configure multiple titles.",
    )
    copy_source.add_argument(
        "--from-template",
        help="Copy only copy materials from another plan template.",
    )
    args = parser.parse_args(argv)

    path = config_paths.resolve_config_path(args.config)
    config = load_config(path)
    changed = False
    created_name = None
    wizard_result = None
    if args.command == "migrate":
        config = plan_templates.migrate(config)
        changed = True
    elif args.command == "create-wizard":
        config, wizard_result = run_create_wizard(config)
        changed = wizard_result["changed"]
    elif args.command == "set-copy":
        config = set_copy_materials(
            config,
            args.template,
            titles=args.title,
            from_template=args.from_template,
        )
        changed = True
    if changed:
        save_config(path, config)

    print(json.dumps({
        "config": str(path),
        "command": args.command,
        "changed": changed,
        "created_template": created_name,
        "wizard_result": wizard_result,
        "default_template": default_template_summary(config),
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
