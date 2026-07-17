#!/usr/bin/env python3
import argparse

from ocean_watch.core import config_paths, config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.templates import (
    plan_templates,
    qianchuan_live_templates,
    qianchuan_product_templates,
)


def marketing_template_validation(config, name, raw):
    errors = []
    try:
        template = plan_templates.normalize_template(config, name, raw)
        binding_error = plan_templates.binding_error(template["bindings"])
        strategy_error = plan_templates.material_strategy_error(
            template["material_strategy"]
        )
        if binding_error:
            errors.append(binding_error)
        if strategy_error:
            errors.append(strategy_error)
        fixed_fields = plan_templates.fixed_material_fields(template)
        if fixed_fields:
            errors.append("runtime material IDs stored in template: " + ", ".join(fixed_fields))
        if int(config.get("plan_template_schema_version") or 1) >= 4:
            canonical = plan_templates.canonical_template_name(template)
            if name != canonical or template["display_name"] != canonical:
                errors.append(f"template name must be {canonical}")
    except (AttributeError, ConfigurationError, KeyError, TypeError, ValueError) as error:
        errors.append(str(error))
    return {"template": name, "valid": not errors, "errors": errors}


def schema_version(config, key):
    raw = config.get(key)
    raw = 1 if raw in (None, "") else raw
    try:
        parsed = int(raw)
    except (TypeError, ValueError):
        return None, f"{key} must be an integer"
    if isinstance(raw, bool) or parsed < 1:
        return None, f"{key} must be a positive integer"
    return parsed, None


def validate_marketing(config, selector=None):
    errors = []
    templates = config.get("plan_templates") or {}
    if not isinstance(templates, dict):
        errors.append("plan_templates must be an object")
        templates = {}
    if selector is not None and selector not in templates:
        raise ConfigurationError("Marketing template not found", {"template": selector})
    names = [selector] if selector else sorted(templates)
    rows = [marketing_template_validation(config, name, templates[name]) for name in names]
    version, version_error = schema_version(config, "plan_template_schema_version")
    if version_error:
        errors.append(version_error)
    default_errors = []
    if not isinstance(config.get("default_plan_template"), dict):
        default_errors.append("default_plan_template must be an object")
    default_skeleton = {
        "template": "default_plan_template",
        "valid": not default_errors,
        "errors": default_errors,
    }
    migration_required = version != plan_templates.SCHEMA_VERSION
    return {
        "channel": "marketing",
        "schema_version": version,
        "supported_schema_version": plan_templates.SCHEMA_VERSION,
        "migration_required": migration_required,
        "valid": not errors
        and not migration_required
        and default_skeleton["valid"]
        and all(row["valid"] for row in rows),
        "errors": errors,
        "default_skeletons": [default_skeleton],
        "templates": rows,
    }


def template_mapping(config, key, errors):
    templates = config.get(key) or {}
    if not isinstance(templates, dict):
        errors.append(f"{key} must be an object")
        return {}
    return templates


def default_template_validation(config, key, kind, validator):
    errors = []
    raw = config.get(key)
    if raw is None:
        errors.append(f"{key} is missing")
    else:
        try:
            validator(raw)
        except (ConfigurationError, TypeError, ValueError) as error:
            errors.append(str(error))
    return {
        "template": key,
        "template_kind": kind,
        "valid": not errors,
        "errors": errors,
    }


def selected_qianchuan_templates(selector, product_templates, live_templates):
    matches = []
    for kind, templates in (("product", product_templates), ("live", live_templates)):
        for template_id, template in templates.items():
            display_name = template.get("display_name") if isinstance(template, dict) else None
            if selector in {template_id, display_name}:
                matches.append((kind, template_id))
    if not matches:
        raise ConfigurationError("Qianchuan template not found", {"template": selector})
    if len(matches) > 1:
        raise ConfigurationError(
            "Qianchuan template selector is ambiguous; use template_id",
            {"template": selector},
        )
    return matches


def validate_qianchuan(config, selector=None):
    errors = []
    product_templates = template_mapping(
        config,
        qianchuan_product_templates.TEMPLATES_KEY,
        errors,
    )
    live_templates = template_mapping(
        config,
        qianchuan_live_templates.TEMPLATES_KEY,
        errors,
    )
    ids = (
        selected_qianchuan_templates(selector, product_templates, live_templates)
        if selector
        else [
            *[("product", item) for item in sorted(product_templates)],
            *[("live", item) for item in sorted(live_templates)],
        ]
    )
    selected_kind = ids[0][0] if selector else None
    rows = []
    for kind, template_id in ids:
        row_errors = []
        try:
            if kind == "product":
                template = qianchuan_product_templates.validate_business_template(
                    product_templates[template_id]
                )
            else:
                template = qianchuan_live_templates.validate_business_template(
                    live_templates[template_id]
                )
            if template["template_id"] != template_id:
                row_errors.append("template key does not match template_id")
        except (ConfigurationError, TypeError, ValueError) as error:
            row_errors.append(str(error))
        rows.append({
            "template": template_id,
            "template_kind": kind,
            "valid": not row_errors,
            "errors": row_errors,
        })
    default_skeletons = [
        default_template_validation(
            config,
            qianchuan_product_templates.DEFAULT_TEMPLATE_KEY,
            "product",
            qianchuan_product_templates.validate_default_template,
        ),
        default_template_validation(
            config,
            qianchuan_live_templates.DEFAULT_TEMPLATE_KEY,
            "live",
            qianchuan_live_templates.validate_default_template,
        ),
    ]
    product_schema_version, product_version_error = schema_version(
        config,
        qianchuan_product_templates.SCHEMA_VERSION_KEY,
    )
    live_schema_version, live_version_error = schema_version(
        config,
        qianchuan_live_templates.SCHEMA_VERSION_KEY,
    )
    errors.extend(
        error for error in (product_version_error, live_version_error) if error
    )
    migration_required = (
        product_schema_version != qianchuan_product_templates.SCHEMA_VERSION
        or live_schema_version != qianchuan_live_templates.SCHEMA_VERSION
    )
    return {
        "channel": "qianchuan",
        "selected_template_kind": selected_kind,
        "schema_versions": {
            "product": product_schema_version,
            "live": live_schema_version,
        },
        "supported_schema_versions": {
            "product": qianchuan_product_templates.SCHEMA_VERSION,
            "live": qianchuan_live_templates.SCHEMA_VERSION,
        },
        "migration_required": migration_required,
        "valid": not errors
        and not migration_required
        and all(row["valid"] for row in default_skeletons)
        and all(row["valid"] for row in rows),
        "errors": errors,
        "default_skeletons": default_skeletons,
        "templates": rows,
    }


def validate_templates(config, channel=None, selector=None):
    channels = [channel] if channel else ["marketing", "qianchuan"]
    rows = []
    for current in channels:
        if current == "marketing":
            rows.append(validate_marketing(config, selector))
        else:
            rows.append(validate_qianchuan(config, selector))
    return {
        "ok": all(row["valid"] for row in rows),
        "mode": "template_validation",
        "channels": rows,
    }


def marketing_dependents(config, template_name):
    dependents = []
    for name, raw in (config.get("plan_templates") or {}).items():
        if name == template_name:
            continue
        try:
            normalized = plan_templates.normalize_template(config, name, raw)
        except (AttributeError, ConfigurationError, KeyError, TypeError, ValueError) as error:
            raise ConfigurationError(
                "a dependent Marketing template is invalid; validate templates before deletion",
                {"template": name},
            ) from error
        references = []
        if (normalized.get("created_from") or {}).get("template") == template_name:
            references.append("created_from.template")
        if (
            normalized.get("copy_materials") or {}
        ).get("copied_from_template") == template_name:
            references.append("copy_materials.copied_from_template")
        if references:
            dependents.append({"template": name, "references": references})
    return dependents


def delete_template(config, channel, selector, *, force=False):
    updated = config.copy()
    if channel == "marketing":
        version, _ = schema_version(config, "plan_template_schema_version")
        if version != plan_templates.SCHEMA_VERSION:
            raise ConfigurationError("migrate Marketing templates before deletion")
        raw_templates = config.get("plan_templates") or {}
        if not isinstance(raw_templates, dict):
            raise ConfigurationError("plan_templates must be an object")
        templates = dict(raw_templates)
        if selector not in templates:
            raise ConfigurationError("Marketing template not found", {"template": selector})
        validation = marketing_template_validation(config, selector, templates[selector])
        if not validation["valid"]:
            raise ConfigurationError(
                "validate the Marketing template before deletion",
                {"template": selector, "errors": validation["errors"]},
            )
        dependents = marketing_dependents(config, selector)
        if dependents and not force:
            raise ConfigurationError(
                "template is referenced by other templates; pass --force to delete",
                {"dependents": dependents},
            )
        templates.pop(selector)
        updated["plan_templates"] = templates
        return updated, {"channel": channel, "template": selector, "dependents": dependents}

    product_config = qianchuan_product_templates.ensure_config(config)
    product_schema_version, _ = schema_version(
        config,
        qianchuan_product_templates.SCHEMA_VERSION_KEY,
    )
    live_config = qianchuan_live_templates.ensure_config(config)
    live_schema_version, _ = schema_version(
        config,
        qianchuan_live_templates.SCHEMA_VERSION_KEY,
    )
    if product_schema_version != qianchuan_product_templates.SCHEMA_VERSION:
        raise ConfigurationError("migrate Qianchuan templates before deletion")
    if live_schema_version != qianchuan_live_templates.SCHEMA_VERSION:
        raise ConfigurationError("migrate Qianchuan live templates before deletion")
    try:
        template = qianchuan_product_templates.resolve_template(product_config, selector)
        templates = dict(
            product_config.get(qianchuan_product_templates.TEMPLATES_KEY) or {}
        )
        templates.pop(template["template_id"])
        product_config[qianchuan_product_templates.TEMPLATES_KEY] = templates
        updated = product_config
        template_kind = "product"
    except ConfigurationError as product_error:
        try:
            template = qianchuan_live_templates.resolve_template(live_config, selector)
        except ConfigurationError:
            raise product_error from None
        templates = dict(live_config.get(qianchuan_live_templates.TEMPLATES_KEY) or {})
        templates.pop(template["template_id"])
        live_config[qianchuan_live_templates.TEMPLATES_KEY] = templates
        updated = live_config
        template_kind = "live"
    return updated, {
        "channel": channel,
        "template": template["template_id"],
        "name": template["display_name"],
        "template_kind": template_kind,
        "dependents": [],
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Validate or delete business templates.")
    parser.add_argument("action", choices=("validate", "delete"))
    parser.add_argument("--config")
    parser.add_argument("--channel", choices=("marketing", "qianchuan"))
    parser.add_argument("--template")
    parser.add_argument("--force", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if args.action == "delete" and (not args.channel or not args.template):
        raise ConfigurationError("delete requires --channel and --template")
    if args.action == "delete" and args.channel == "qianchuan" and args.force:
        raise ConfigurationError("--force is supported only for Marketing template references")
    if args.action == "validate" and (args.force or args.submit):
        raise ConfigurationError("--force and --submit are only valid for delete")
    if args.action == "validate" and args.template and not args.channel:
        raise ConfigurationError("--channel is required when validating one template")

    path = config_paths.resolve_config_path(args.config)
    config = config_store.load_json(path)
    if args.action == "validate":
        result = validate_templates(config, args.channel, args.template)
        result["config"] = str(path)
        write_json(result, destination=args.out)
        return 0 if result["ok"] else 1

    revision = config_store.json_revision(config)
    updated, deletion = delete_template(
        config,
        args.channel,
        args.template,
        force=args.force,
    )
    if args.submit:
        config_store.compare_and_swap_json(path, revision, updated)
    result = {
        "ok": True,
        "mode": "submit" if args.submit else "dry_run",
        "operation": "template_delete",
        "config": str(path),
        "changed": args.submit,
        "deletion": deletion,
    }
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
