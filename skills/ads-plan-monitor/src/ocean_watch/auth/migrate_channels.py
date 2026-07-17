#!/usr/bin/env python3
import argparse
import json
import secrets
from pathlib import Path

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
import ocean_watch.templates.plan_templates as plan_templates
import ocean_watch.templates.qianchuan_live_templates as qianchuan_live_templates
import ocean_watch.templates.qianchuan_product_templates as qianchuan_product_templates
from ocean_watch.core.process_lock import ProcessLock

JOURNAL_SCHEMA_VERSION = 1
ACTIVE = "schema_v2_active"


def migration_root():
    return authorization_store.state_root() / "migration"


def journal_path():
    return migration_root() / "journal.json"


def migration_lock_path():
    return migration_root() / "migration.lock"


def new_journal(config_path):
    return {
        "schema_version": JOURNAL_SCHEMA_VERSION,
        "migration_id": secrets.token_hex(12),
        "authorization_id": secrets.token_hex(12),
        "config_path": str(config_path.resolve()),
        "config": "pending",
        "credentials": "pending",
        "activation": "pending",
    }


def load_or_create_journal(config_path):
    path = journal_path()
    if path.exists():
        journal = json.loads(path.read_text(encoding="utf-8"))
        if int(journal.get("schema_version") or 0) != JOURNAL_SCHEMA_VERSION:
            raise RuntimeError("unsupported migration journal schema")
        if journal.get("config_path") != str(config_path.resolve()):
            if journal.get("activation") != ACTIVE:
                raise RuntimeError(
                    "another channel migration is incomplete for " + str(journal.get("config_path"))
                )
            journal = new_journal(config_path)
            write_journal(journal)
        return journal
    journal = new_journal(config_path)
    write_journal(journal)
    return journal


def write_journal(journal):
    config_store.atomic_write_json(journal_path(), journal, backup=False)


def prepare_config(raw, confirm_remove_legacy_materials=False):
    prepared = raw
    if int(prepared.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        prepared = plan_templates.migrate(
            prepared,
            confirm_remove_legacy_materials=confirm_remove_legacy_materials,
        )
    prepared = qianchuan_product_templates.ensure_config(prepared)
    prepared = qianchuan_live_templates.ensure_config(prepared)
    return channels.migrate_config(prepared)


def prepare_legacy_credentials(raw):
    extracted = credential_store.extract_credentials(raw, channel="marketing")
    if extracted:
        existing = credential_store.read_credentials()
        credential_store.write_credentials({**existing, **extracted})
    return extracted


def migrate(config_path, confirm_remove_legacy_materials=False):
    config_path = Path(config_path).expanduser()
    with ProcessLock(migration_lock_path()):
        with config_store.json_file_lock(config_path):
            raw = config_store.load_json(config_path)
            migrated = prepare_config(
                raw,
                confirm_remove_legacy_materials=confirm_remove_legacy_materials,
            )
            journal = load_or_create_journal(config_path)
            extracted = prepare_legacy_credentials(raw)

            credential_result = journal.get("credential_result") or {}
            if journal["credentials"] != "committed":
                credential_result = authorization_store.migrate_legacy_marketing(
                    legacy=credential_store.read_credentials(),
                    authorization_id=journal["authorization_id"],
                )
                journal["credential_result"] = credential_result
                journal["credentials"] = "committed"
                write_journal(journal)

            if journal["config"] != "committed" or raw != migrated:
                config_store.atomic_write_json(config_path, migrated, backup=False)
                if extracted:
                    config_path.with_suffix(config_path.suffix + ".bak").unlink(missing_ok=True)
                journal["config"] = "committed"
                write_journal(journal)

            if journal["activation"] != ACTIVE:
                journal["activation"] = ACTIVE
                write_journal(journal)

            return {
                "config": str(config_path),
                "migration_id": journal["migration_id"],
                "config_schema_version": migrated["config_schema_version"],
                "default_channel": migrated["default_channel"],
                "credential_migration": credential_result,
                "config_sensitive_fields_migrated": sorted(extracted),
                "activation": journal["activation"],
            }


def assert_migration_ready(config_path):
    path = journal_path()
    if not path.exists():
        return
    journal = json.loads(path.read_text(encoding="utf-8"))
    if journal.get("config_path") == str(Path(config_path).expanduser().resolve()) and journal.get("activation") != ACTIVE:
        raise RuntimeError(
            "channel migration is incomplete; rerun ocean-watch auth migrate for this config"
        )


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Migrate existing Ocean Engine Marketing config and credentials to channel-aware storage."
    )
    parser.add_argument("--config")
    parser.add_argument(
        "--confirm-remove-legacy-materials",
        action="store_true",
        help="Confirm removal of fixed video IDs while upgrading plan templates.",
    )
    args = parser.parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)
    try:
        result = migrate(
            config_path,
            confirm_remove_legacy_materials=args.confirm_remove_legacy_materials,
        )
    except plan_templates.LegacyMaterialSelectionError as exc:
        print(json.dumps({
            "config": str(config_path),
            "changed": False,
            "error_code": "legacy_material_selection_requires_confirmation",
            "error": str(exc),
            "affected_templates": exc.templates,
            "required_flag": "--confirm-remove-legacy-materials",
        }, ensure_ascii=False, indent=2))
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
