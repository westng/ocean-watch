#!/usr/bin/env python3
import argparse
import json
import secrets
from pathlib import Path

import authorization_store
import channels
import config_paths
import config_store
import credential_store
import plan_templates
from process_lock import ProcessLock


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


def prepare_config(raw):
    prepared = raw
    if int(prepared.get("plan_template_schema_version") or 1) < plan_templates.SCHEMA_VERSION:
        prepared = plan_templates.migrate(prepared)
    return channels.migrate_config(prepared)


def prepare_legacy_credentials(raw):
    extracted = credential_store.extract_credentials(raw, channel="marketing")
    if extracted:
        existing = credential_store.read_credentials()
        credential_store.write_credentials({**existing, **extracted})
    return extracted


def migrate(config_path):
    config_path = Path(config_path).expanduser()
    with ProcessLock(migration_lock_path()):
        raw = json.loads(config_path.read_text(encoding="utf-8"))
        migrated = prepare_config(raw)
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

        if journal["config"] != "committed":
            config_store.atomic_write_json(config_path, migrated)
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
            "channel migration is incomplete; rerun scripts/migrate_channels.py for this config"
        )


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Migrate existing Ocean Engine Marketing config and credentials to channel-aware storage."
    )
    parser.add_argument("--config")
    args = parser.parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)
    print(json.dumps(migrate(config_path), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
