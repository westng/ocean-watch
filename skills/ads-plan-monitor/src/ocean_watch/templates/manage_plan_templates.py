#!/usr/bin/env python3
import argparse
import copy
import json

import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
import ocean_watch.templates.plan_templates as plan_templates
import ocean_watch.templates.template_workflow as template_workflow
from ocean_watch.auth import authorization_store
from ocean_watch.templates import template_advertiser_binding


def load_config(path):
    return config_store.load_json(path)


def save_config(path, config):
    config_store.atomic_write_json(path, config)


def normalize_titles(titles):
    return template_workflow.normalize_titles(titles)


def list_templates(config):
    rows = []
    for name, template in sorted((config.get("plan_templates") or {}).items()):
        normalized = plan_templates.normalize_template(config, name, template)
        bindings = normalized["bindings"]
        strategy = normalized["material_strategy"]
        overrides = normalized.get("overrides") or {}
        effective_defaults = plan_templates.deep_merge(
            (plan_templates.default_bundle(config).get("defaults") or {}),
            overrides.get("defaults") or {},
        )
        product_info = effective_defaults.get("product_info") or {}
        titles = normalized["copy_materials"].get("titles") or []
        rows.append({
            "name": name,
            "channel": bindings.get("channel"),
            "advertiser_id": bindings.get("advertiser_id"),
            "platform": bindings.get("platform"),
            "traffic_source": bindings.get("traffic_source"),
            "product_id": bindings.get("product_id"),
            "product_name": bindings.get("product_name"),
            "product_image_ids": list(
                (overrides.get("resolved_ids") or {}).get(
                    "product_image_ids"
                )
                or []
            ),
            "product_image": {
                "type": product_info.get("product_image_type"),
                "fields": list(product_info.get("product_image_fields") or []),
                "manual_image_ids_required": product_info.get("product_image_type") != "DPA",
            },
            "delivery_settings": {
                "daily_budget": effective_defaults.get("daily_budget"),
                "roi_goal": effective_defaults.get("roi_goal"),
                "gender": effective_defaults.get("gender"),
                "ages": list(effective_defaults.get("ages") or []),
            },
            "material_source_type": strategy.get("source_type"),
            "material_source_name": template_workflow.material_source_label(
                strategy.get("source_type")
            ),
            "material_strategy": strategy,
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
    defaults = base.get("defaults") or {}
    product_info = defaults.get("product_info") or {}
    resolved_ids = base.get("resolved_ids") or {}
    return {
        "name": "default_plan_template",
        "type": "creation_base_only",
        "business_usable": False,
        "selectable_for_plan_creation": False,
        "purpose": "Base configuration for the business-template creation wizard.",
        "delivery_settings": {
            "daily_budget": defaults.get("daily_budget"),
            "roi_goal": defaults.get("roi_goal"),
            "gender": defaults.get("gender"),
            "ages": list(defaults.get("ages") or []),
        },
        "product_image": {
            "type": product_info.get("product_image_type"),
            "fields": list(product_info.get("product_image_fields") or []),
            "manual_image_ids_required": product_info.get("product_image_type") != "DPA",
        },
        "regions": {
            "city_count": len(resolved_ids.get("city_ids") or []),
            "city_names": list(resolved_ids.get("city_names") or []),
        },
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
        "product_image_ids": getattr(args, "product_image_ids", None),
        "product_info": getattr(args, "product_info", None),
        "daily_budget": getattr(args, "daily_budget", None),
        "roi_goal": getattr(args, "roi_goal", None),
        "gender": getattr(args, "gender", None),
        "ages": getattr(args, "ages", None),
        "name": args.name,
        "source_name": args.source_name,
        "landing_page_url": args.landing_page_url,
        "open_url": args.open_url,
        "track_url": args.track_url,
        "action_track_url": args.action_track_url,
        "titles": args.title,
        "material_source_type": getattr(args, "material_source_type", None),
        "selection_mode": getattr(args, "selection_mode", None),
        "max_materials_per_unit": getattr(args, "max_materials_per_unit", None),
        "unlimited_materials": getattr(args, "unlimited_materials", False),
        "creator_ids": getattr(args, "creator_ids", None),
        "creator_auth_types": getattr(args, "creator_auth_types", None),
        "minimum_remaining_days": getattr(args, "minimum_remaining_days", None),
    }
    name, template = template_workflow.build_template(config, values, args.from_template)
    if name in templates and not args.force:
        raise ValueError(f"plan template already exists: {name}; use --force to replace it")
    validation = template_workflow.validate_candidate(config, name, template)
    if (
        not validation["ready_for_plan_creation"]
        and not getattr(args, "allow_incomplete_preview", False)
    ):
        missing = ", ".join(validation["template_missing_fields"])
        raise ValueError(f"incomplete plan template cannot be saved: {missing}")
    templates[name] = template
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


def select_template_source(config, input_fn, output_fn, material_source_type=None):
    names = []
    for name in sorted((config.get("plan_templates") or {}).keys()):
        template = plan_templates.normalize_template(
            config,
            name,
            config["plan_templates"][name],
        )
        source_type = (template.get("material_strategy") or {}).get("source_type")
        if material_source_type is None or source_type == material_source_type:
            names.append(name)
    source_label = {
        "ACCOUNT_UPLOAD": "混剪素材",
        "CREATOR_AUTHORIZED": "原生素材",
    }.get(material_source_type)
    output_fn(f"创建来源（{source_label}）:" if source_label else "创建来源：")
    output_fn("  0. 默认模板（仅作为新业务模板骨架）")
    for index, name in enumerate(names, start=1):
        template = plan_templates.normalize_template(config, name, config["plan_templates"][name])
        bindings = template["bindings"]
        output_fn(
            f"  {index}. {name}（渠道 {bindings.get('channel')}，"
            f"广告主 {bindings.get('advertiser_id')}，"
            f"平台 {bindings.get('platform')}，商品 {bindings.get('product_name')}，"
            f"商品 ID {bindings.get('product_id')}，"
            f"素材来源 {template_workflow.material_source_label((template.get('material_strategy') or {}).get('source_type'))}）"
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


def prompt_positive_number(input_fn, label, default):
    while True:
        value = prompt_value(input_fn, label, default, required=True)
        try:
            number = float(value)
        except (TypeError, ValueError):
            continue
        if number > 0:
            return int(number) if number.is_integer() else number


def select_gender(input_fn, inherited=None):
    aliases = {
        "0": "NONE",
        "不限": "NONE",
        "none": "NONE",
        "1": "GENDER_MALE",
        "男": "GENDER_MALE",
        "gender_male": "GENDER_MALE",
        "2": "GENDER_FEMALE",
        "女": "GENDER_FEMALE",
        "gender_female": "GENDER_FEMALE",
    }
    labels = {"NONE": "不限", "GENDER_MALE": "男", "GENDER_FEMALE": "女"}
    default = labels.get(inherited, "不限")
    while True:
        raw = input_fn(f"性别（0 不限 / 1 男 / 2 女） [{default}]: ").strip()
        value = aliases.get((raw or default).lower())
        if value:
            return value


AGE_PRESETS = {
    "不限": [],
    "none": [],
    "18-23": ["AGE_BETWEEN_18_23"],
    "24-49": [
        "AGE_BETWEEN_24_30",
        "AGE_BETWEEN_31_40",
        "AGE_BETWEEN_41_49",
    ],
    "50+": ["AGE_ABOVE_50"],
}
OFFICIAL_AGE_GROUPS = {
    "AGE_BETWEEN_18_23",
    "AGE_BETWEEN_24_30",
    "AGE_BETWEEN_31_40",
    "AGE_BETWEEN_41_49",
    "AGE_ABOVE_50",
}
OFFICIAL_AGE_REFINED_GROUPS = {
    "AGE_BETWEEN_18_19",
    "AGE_BETWEEN_20_23",
    "AGE_BETWEEN_24_30",
    "AGE_BETWEEN_31_35",
    "AGE_BETWEEN_36_40",
    "AGE_BETWEEN_41_45",
    "AGE_BETWEEN_46_50",
    "AGE_BETWEEN_51_55",
    "AGE_BETWEEN_56_59",
    "AGE_ABOVE_60",
}


def age_display(values):
    values = list(values or [])
    for label, preset in AGE_PRESETS.items():
        if label not in {"none"} and values == preset:
            return label
    return ",".join(values)


def normalize_ages(value):
    normalized_value = str(value or "不限").strip()
    preset = AGE_PRESETS.get(normalized_value.lower())
    if preset is not None:
        return list(preset)
    ages = [item.strip().upper() for item in normalized_value.replace("，", ",").split(",")]
    ages = list(dict.fromkeys(item for item in ages if item))
    if not ages:
        return []
    age_set = set(ages)
    if age_set <= OFFICIAL_AGE_GROUPS or age_set <= OFFICIAL_AGE_REFINED_GROUPS:
        return ages
    raise ValueError("unsupported or mixed official age groups")


def select_ages(input_fn, inherited=None):
    default = age_display(inherited)
    while True:
        raw = input_fn(
            "年龄（不限 / 18-23 / 24-49 / 50+ / 官方枚举逗号分隔）"
            f" [{default}]: "
        ).strip()
        try:
            return normalize_ages(raw or default)
        except ValueError:
            continue


def suggested_product_selling_point(product_name):
    for candidate in (f"{product_name}推荐", str(product_name)):
        try:
            return template_workflow.normalize_product_selling_points([candidate])[0]
        except ValueError:
            continue
    return None


def collect_product_selling_points(input_fn, inherited, product_name):
    default = ",".join(str(value) for value in inherited or [])
    if not default:
        default = suggested_product_selling_point(product_name)
    while True:
        value = prompt_value(
            input_fn,
            "产品卖点（每条 6-9 位置，多个用逗号分隔）",
            default,
            required=True,
        )
        try:
            return template_workflow.normalize_product_selling_points(value)
        except ValueError:
            continue


def select_material_source(input_fn, inherited=None):
    default = "1" if inherited == "ACCOUNT_UPLOAD" else "2" if inherited == "CREATOR_AUTHORIZED" else None
    suffix = f" [{default}]" if default else ""
    while True:
        raw = input_fn(f"素材来源（1 上传素材 / 2 达人素材）{suffix}: ").strip()
        if not raw and default:
            raw = default
        if raw == "1":
            return "ACCOUNT_UPLOAD"
        if raw == "2":
            return "CREATOR_AUTHORIZED"


def select_selection_mode(input_fn, inherited=None):
    default = "2" if inherited == "LATEST" else "1"
    while True:
        raw = input_fn(
            f"素材选择方式（1 手动选择 / 2 自动选择最新） [{default}]: "
        ).strip()
        if not raw:
            raw = default
        if raw == "1":
            return "MANUAL"
        if raw == "2":
            return "LATEST"


def select_material_limit(input_fn, source_type, inherited=None):
    if source_type != "CREATOR_AUTHORIZED":
        return int(prompt_value(
            input_fn,
            "每单元素材数量",
            inherited or 5,
            required=True,
        )), False
    default = "不限" if inherited is None else str(inherited)
    while True:
        raw = input_fn(f"每单元素材数量（正整数 / 不限） [{default}]: ").strip()
        value = raw or default
        if value.lower() in {"不限", "unlimited", "none"}:
            return None, True
        try:
            maximum = int(value)
        except ValueError:
            continue
        if maximum > 0:
            return maximum, False


def run_create_wizard(
    config,
    input_fn=input,
    output_fn=print,
    material_source_type=None,
    authorization_state=None,
):
    if int(config.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        config = plan_templates.migrate(config)
    source_name = select_template_source(
        config,
        input_fn,
        output_fn,
        material_source_type=material_source_type,
    )
    source = None
    if source_name:
        source = plan_templates.normalize_template(
            config,
            source_name,
            config["plan_templates"][source_name],
        )
    bindings = (source or {}).get("bindings") or {}
    inherited_strategy = (source or {}).get("material_strategy") or {}
    overrides = (source or {}).get("overrides") or {}
    defaults = overrides.get("defaults") or {}

    advertiser_id, advertiser_verification = (
        template_advertiser_binding.prompt_advertiser_id(
            "marketing",
            (
                bindings.get("advertiser_id"),
                (config.get("account") or {}).get("advertiser_id"),
            ),
            input_fn=input_fn,
            output_fn=output_fn,
            channel_state=authorization_state,
        )
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
    source_product_info = (
        ((overrides.get("defaults") or {}).get("product_info") or {})
        if source
        else {}
    )
    default_product_info = (
        (plan_templates.default_bundle(config).get("defaults") or {}).get("product_info") or {}
    )
    if policy == "same_advertiser_same_product" and source:
        product_info = copy.deepcopy(source_product_info or default_product_info)
    else:
        product_info = copy.deepcopy(default_product_info)
        product_info["product_image_type"] = "DPA"
        product_info["product_image_fields"] = list(
            template_workflow.DEFAULT_DPA_PRODUCT_IMAGE_FIELDS
        )
    if product_info.get("product_name_type", "CUSTOM") == "CUSTOM":
        product_info["titles"] = [product_name]
    if product_info.get("product_selling_point_type", "CUSTOM") == "CUSTOM":
        product_info["product_selling_point_type"] = "CUSTOM"
        product_info["selling_points"] = collect_product_selling_points(
            input_fn,
            product_info.get("selling_points") or [],
            product_name,
        )
    product_image_ids = None
    inherited_defaults = {
        **(plan_templates.default_bundle(config).get("defaults") or {}),
        **defaults,
    }
    daily_budget = prompt_positive_number(
        input_fn,
        "日预算",
        inherited_defaults.get("daily_budget", 300),
    )
    roi_goal = prompt_positive_number(
        input_fn,
        "净成交 ROI 出价",
        inherited_defaults.get("roi_goal", 1.5),
    )
    gender = select_gender(input_fn, inherited_defaults.get("gender"))
    ages = select_ages(input_fn, inherited_defaults.get("ages"))
    if material_source_type is None:
        material_source_type = select_material_source(
            input_fn,
            inherited_strategy.get("source_type"),
        )
    selection_mode = select_selection_mode(
        input_fn,
        inherited_strategy.get("selection_mode"),
    )
    max_materials_per_unit, unlimited_materials = select_material_limit(
        input_fn,
        material_source_type,
        inherited_strategy.get("max_materials_per_unit", 5),
    )
    creator_ids = None
    minimum_remaining_days = None
    if material_source_type == "CREATOR_AUTHORIZED":
        creator_ids_value = prompt_value(
            input_fn,
            "达人 ID 白名单（逗号分隔，留空表示不限）",
            ",".join((inherited_strategy.get("creator_filters") or {}).get("creator_ids") or []),
        )
        creator_ids = [
            value.strip()
            for value in str(creator_ids_value or "").split(",")
            if value.strip()
        ]
        minimum_remaining_days = int(prompt_value(
            input_fn,
            "授权至少剩余天数",
            (inherited_strategy.get("creator_filters") or {}).get(
                "minimum_remaining_days",
                1,
            ),
            required=True,
        ))
    generated_name = template_workflow.template_name(
        advertiser_id,
        product_name,
        product_id,
        material_source_type,
    )
    output_fn(f"模板名称（按绑定信息自动生成）: {generated_name}")

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
        product_image_ids=product_image_ids,
        product_info=product_info,
        daily_budget=daily_budget,
        roi_goal=roi_goal,
        gender=gender,
        ages=ages,
        name=generated_name,
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
        material_source_type=material_source_type,
        selection_mode=selection_mode,
        max_materials_per_unit=max_materials_per_unit,
        unlimited_materials=unlimited_materials,
        creator_ids=creator_ids,
        creator_auth_types=["VIDEO_ITEM"] if material_source_type == "CREATOR_AUTHORIZED" else None,
        minimum_remaining_days=minimum_remaining_days,
        from_template=source_name,
        allow_incomplete_preview=True,
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
        "advertiser_binding_verification": advertiser_verification,
        "delivery_settings": {
            "daily_budget": daily_budget,
            "roi_goal": roi_goal,
            "gender": gender,
            "ages": ages,
        },
        "product_image": {
            "type": product_info.get("product_image_type"),
            "fields": product_info.get("product_image_fields") or [],
            "manual_image_ids_required": product_info.get("product_image_type") != "DPA",
        },
        "product_selling_points": list(product_info.get("selling_points") or []),
        "material_strategy": created["material_strategy"],
        "copy_title_count": len(created["copy_materials"].get("titles") or []),
        "validation": validation,
        "changes": template_workflow.template_diff(source_snapshot, created),
    }
    output_fn("创建前预览：")
    output_fn(json.dumps(preview, ensure_ascii=False, indent=2))
    if not validation["ready_for_plan_creation"]:
        return config, {
            **preview,
            "confirmed": False,
            "changed": False,
            "blocked": True,
        }
    if not prompt_yes_no(input_fn, "确认创建此业务模板", default=False):
        return config, {**preview, "confirmed": False, "changed": False}
    return candidate, {**preview, "confirmed": True, "changed": True}


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
    parser.add_argument("command", choices=("list", "migrate", "create-wizard", "set-copy"))
    parser.add_argument("--config")
    parser.add_argument(
        "--material-source-type",
        choices=("ACCOUNT_UPLOAD", "CREATOR_AUTHORIZED"),
    )
    parser.add_argument(
        "--confirm-remove-legacy-materials",
        action="store_true",
        help="Confirm that fixed legacy video IDs will be removed from business templates.",
    )
    parser.add_argument("--template")
    copy_source = parser.add_mutually_exclusive_group()
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
    if args.command == "set-copy":
        if not args.template:
            parser.error("--template is required for set-copy")
        if args.title is None and args.from_template is None:
            parser.error("one of --title or --from-template is required for set-copy")
    elif args.template is not None or args.title is not None or args.from_template is not None:
        parser.error("--template, --title, and --from-template are only valid for set-copy")
    if args.command != "migrate" and args.confirm_remove_legacy_materials:
        parser.error("--confirm-remove-legacy-materials is only valid for migrate")
    if args.command != "create-wizard" and args.material_source_type:
        parser.error("--material-source-type is only valid for create-wizard")

    path = config_paths.resolve_config_path(args.config)
    config = load_config(path)
    original_revision = config_store.json_revision(config)
    changed = False
    created_name = None
    wizard_result = None
    try:
        if args.command == "migrate":
            config = plan_templates.migrate(
                config,
                confirm_remove_legacy_materials=args.confirm_remove_legacy_materials,
            )
            changed = True
        elif args.command == "create-wizard":
            config, wizard_result = run_create_wizard(
                config,
                material_source_type=args.material_source_type,
                authorization_state=authorization_store.load_channel_state("marketing"),
            )
            changed = wizard_result["changed"]
        elif args.command == "set-copy":
            config = set_copy_materials(
                config,
                args.template,
                titles=args.title,
                from_template=args.from_template,
            )
            changed = True
    except plan_templates.LegacyMaterialSelectionError as exc:
        print(json.dumps({
            "config": str(path),
            "command": args.command,
            "changed": False,
            "error_code": "legacy_material_selection_requires_confirmation",
            "error": str(exc),
            "affected_templates": exc.templates,
            "required_flag": "--confirm-remove-legacy-materials",
        }, ensure_ascii=False, indent=2))
        return 2
    if changed:
        config_store.compare_and_swap_json(path, original_revision, config)

    print(json.dumps({
        "config": str(path),
        "command": args.command,
        "changed": changed,
        "created_template": created_name,
        "wizard_result": wizard_result,
        "default_template": default_template_summary(config),
        "templates": list_templates(config),
    }, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
