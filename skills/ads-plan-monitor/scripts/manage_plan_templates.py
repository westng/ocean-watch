#!/usr/bin/env python3
import argparse
import copy
import json

import config_store
import config_paths
import plan_templates
import template_workflow


def load_config(path):
    return config_store.load_json(path)


def save_config(path, config):
    config_store.atomic_write_json(path, config)


def template_name(platform, traffic_source, product_name, product_id):
    return template_workflow.template_name(platform, traffic_source, product_name, product_id)


def normalize_titles(titles):
    return template_workflow.normalize_titles(titles)


def list_templates(config):
    rows = []
    for name, template in sorted((config.get("plan_templates") or {}).items()):
        normalized = plan_templates.normalize_template(config, name, template)
        bindings = normalized["bindings"]
        titles = normalized["copy_materials"].get("titles") or []
        rows.append({
            "name": name,
            "active": name == config.get("active_plan_template"),
            "channel": bindings.get("channel"),
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
    templates = config.setdefault("plan_templates", {})
    if args.from_template:
        source = templates.get(args.from_template)
        if source is None:
            raise ValueError(f"source plan template not found: {args.from_template}")
        source_template = plan_templates.normalize_template(config, args.from_template, source)
        source_advertiser_id = source_template["bindings"].get("advertiser_id")
        if (
            str(source_advertiser_id) != str(args.advertiser_id)
            and not getattr(args, "allow_cross_advertiser_clone", False)
        ):
            raise ValueError(
                f"source plan template {args.from_template} is bound to advertiser "
                f"{source_advertiser_id}; cross-advertiser template cloning is not allowed"
            )
    values = {
        "channel": getattr(args, "channel", None)
        or (config.get("account") or {}).get("channel")
        or "marketing",
        "advertiser_id": args.advertiser_id,
        "platform": args.platform,
        "traffic_source": args.traffic_source,
        "product_id": args.product_id,
        "product_name": args.product_name,
        "name": args.name,
        "source_name": args.source_name,
        "landing_page_url": args.landing_page_url,
        "open_url": args.open_url,
        "track_url": args.track_url,
        "action_track_url": args.action_track_url,
        "titles": args.title,
    }
    name, template = template_workflow.build_template(config, values, args.from_template)
    if name in templates and not args.force:
        raise ValueError(f"plan template already exists: {name}; use --force to replace it")
    if args.activate:
        validation = template_workflow.validate_candidate(config, name, template)
        if not validation["ready_for_plan_creation"]:
            missing = ", ".join(validation["template_missing_fields"])
            raise ValueError(f"incomplete plan template cannot be activated: {missing}")
    templates[name] = template
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
        bindings = template["bindings"]
        output_fn(
            f"  {index}. {name}（渠道 {bindings.get('channel')}，"
            f"广告主 {bindings.get('advertiser_id')}，"
            f"平台 {bindings.get('platform')}，商品 {bindings.get('product_name')}，"
            f"商品 ID {bindings.get('product_id')}）"
        )
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

    target_bindings = {
        "channel": bindings.get("channel")
        or (config.get("account") or {}).get("channel")
        or "marketing",
        "advertiser_id": advertiser_id,
        "platform": platform,
        "traffic_source": traffic_source,
        "product_id": product_id,
        "product_name": product_name,
    }
    policy = template_workflow.clone_policy(source, target_bindings)
    preserve_business_defaults = policy == "same_advertiser_same_product"
    links = (overrides.get("links") or {}) if preserve_business_defaults else {}
    tracking = (overrides.get("tracking_urls") or {}) if preserve_business_defaults else {}

    same_product = policy in {
        "same_advertiser_same_product",
        "cross_advertiser_same_product",
    }
    source_titles = (
        ((source or {}).get("copy_materials") or {}).get("titles") or []
    ) if same_product else []
    titles = collect_titles(input_fn, source_titles)
    arguments = argparse.Namespace(
        channel=target_bindings["channel"],
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
    validation = template_workflow.validate_candidate(config, created_name, created)
    source_snapshot = config["plan_templates"].get(source_name) if source_name else None
    preview = {
        "action": "create_business_template",
        "template": created_name,
        "source": created["created_from"],
        "bindings": created["bindings"],
        "copy_title_count": len(created["copy_materials"].get("titles") or []),
        "validation": validation,
        "changes": template_workflow.template_diff(source_snapshot, created),
        "activate": False,
    }
    output_fn("创建前预览：")
    output_fn(json.dumps(preview, ensure_ascii=False, indent=2))
    if not prompt_yes_no(input_fn, "确认创建此业务模板", default=False):
        return config, {**preview, "confirmed": False, "changed": False}
    activate = validation["ready_for_plan_creation"] and prompt_yes_no(
        input_fn,
        "创建后设为当前模板",
        default=False,
    )
    if activate:
        candidate["active_plan_template"] = created_name
        candidate.setdefault("account", {})["channel"] = target_bindings["channel"]
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
