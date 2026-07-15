#!/usr/bin/env python3
import argparse
import json

import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
from ocean_watch.templates import qianchuan_product_templates as product_templates


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
    raw = input_fn(f"{label} [{hint}]: ").strip().lower()
    if not raw:
        return default
    return raw in {"y", "yes", "是"}


def select_source(config, input_fn, output_fn):
    rows = product_templates.list_templates(config)
    output_fn("创建来源：")
    output_fn("  0. 千川商品全域默认模板（仅作为创建骨架）")
    for index, row in enumerate(rows, start=1):
        output_fn(
            f"  {index}. {row['name']}（广告主 {row['advertiser_id']}，"
            f"产品 {row['product_name']}，"
            f"商品 {row['product_count']} 个）"
        )
    while True:
        raw = input_fn("请选择来源编号: ").strip()
        if raw.isdigit() and 0 <= int(raw) <= len(rows):
            if raw == "0":
                return (
                    "default_qianchuan_product_template",
                    config[product_templates.DEFAULT_TEMPLATE_KEY],
                )
            selected = rows[int(raw) - 1]
            return selected["template_id"], product_templates.resolve_template(
                config,
                selected["template_id"],
            )


def create_template(
    config,
    advertiser_id,
    product_name,
    product_ids,
    source=None,
    activate=True,
    template_id=None,
):
    config = product_templates.ensure_config(config)
    template = product_templates.build_business_template(
        advertiser_id=advertiser_id,
        product_name=product_name,
        product_ids=product_ids,
        source=source,
        template_id=template_id,
        active=activate,
    )
    for existing in (config.get(product_templates.TEMPLATES_KEY) or {}).values():
        if existing.get("display_name") == template["display_name"]:
            raise ValueError(f"Qianchuan product template already exists: {template['display_name']}")
    config.setdefault(product_templates.TEMPLATES_KEY, {})[template["template_id"]] = template
    if activate:
        config[product_templates.ACTIVE_TEMPLATE_KEY] = template["template_id"]
    return config, template


def run_create_wizard(config, input_fn=input, output_fn=print):
    config = product_templates.ensure_config(config)
    source_id, source = select_source(config, input_fn, output_fn)
    bindings = source.get("bindings") or {}
    advertiser_id = prompt_value(
        input_fn,
        "千川广告主 ID",
        bindings.get("advertiser_id")
        if not str(bindings.get("advertiser_id") or "").startswith("REPLACE_WITH")
        else None,
        required=True,
    )
    product_name = prompt_value(
        input_fn,
        "产品名称",
        bindings.get("product_name")
        if not str(bindings.get("product_name") or "").startswith("REPLACE_WITH")
        else None,
        required=True,
    )
    inherited_ids = "/".join(bindings.get("product_ids") or [])
    product_ids = prompt_value(
        input_fn,
        "商品 ID（多个使用 / 分隔，最多 30 个）",
        inherited_ids,
        required=True,
    )
    activate = prompt_yes_no(input_fn, "保存后设为当前千川商品模板", default=True)
    candidate = product_templates.build_business_template(
        advertiser_id=advertiser_id,
        product_name=product_name,
        product_ids=product_ids,
        source=source,
        active=activate,
    )
    preview = {
        "source_template": source_id,
        "template": candidate,
        "omitted_fields": [
            "aweme_id",
            "product_channel_info",
            "multi_product_creative_list",
        ],
    }
    output_fn(json.dumps(preview, ensure_ascii=False, indent=2))
    if not prompt_yes_no(input_fn, "确认创建模板", default=False):
        return config, {"created": False, "preview": preview}
    updated, template = create_template(
        config,
        advertiser_id=advertiser_id,
        product_name=product_name,
        product_ids=product_ids,
        source=source,
        activate=activate,
        template_id=candidate["template_id"],
    )
    return updated, {
        "created": True,
        "template_id": template["template_id"],
        "name": template["display_name"],
        "active": activate,
        "template": template,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Manage Qianchuan product templates.")
    parser.add_argument("action", choices=("list", "create-wizard", "migrate"))
    parser.add_argument("--config")
    args = parser.parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)
    config = config_store.load_json(config_path)
    original_revision = config_store.json_revision(config)
    if args.action == "migrate":
        updated = product_templates.ensure_config(config)
        changed = updated != config
        if changed:
            config_store.compare_and_swap_json(config_path, original_revision, updated)
        print(json.dumps({
            "migrated": changed,
            "schema_version": product_templates.SCHEMA_VERSION,
            "templates": product_templates.list_templates(updated),
        }, ensure_ascii=False, indent=2))
        return 0
    if args.action == "list":
        normalized = product_templates.ensure_config(config)
        print(json.dumps({
            "default_template": {
                "name": "default_qianchuan_product_template",
                "business_usable": False,
                "template": normalized[product_templates.DEFAULT_TEMPLATE_KEY],
            },
            "active_template_id": normalized.get(product_templates.ACTIVE_TEMPLATE_KEY),
            "templates": product_templates.list_templates(normalized),
        }, ensure_ascii=False, indent=2))
        return 0

    updated, result = run_create_wizard(config)
    if result.get("created"):
        config_store.compare_and_swap_json(config_path, original_revision, updated)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
