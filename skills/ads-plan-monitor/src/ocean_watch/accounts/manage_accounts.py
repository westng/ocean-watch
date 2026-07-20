#!/usr/bin/env python3
import argparse

from ocean_watch.accounts import managed_accounts
from ocean_watch.auth import channels
from ocean_watch.core import config_paths, config_store
from ocean_watch.core.output import write_json

LIST_PRESENTATION_COLUMNS = (
    ("channel_name", "渠道"),
    ("name", "账户名称"),
    ("advertiser_id", "广告主 ID"),
    ("enabled_label", "启用状态"),
)


def load(path):
    return config_store.load_json(path)


def presentation_value(value):
    return str(value if value not in (None, "") else "—").replace(
        "|", "\\|"
    ).replace("\r", " ").replace("\n", " ")


def list_presentation(accounts, *, include_disabled=False):
    rows = [
        {
            **account,
            "channel_name": channels.CHANNELS[account["channel"]]["display_name"],
            "enabled_label": "已启用" if account["enabled"] else "已停用",
        }
        for account in accounts
    ]
    table = [
        "| " + " | ".join(label for _, label in LIST_PRESENTATION_COLUMNS) + " |",
        "| " + " | ".join("---" for _ in LIST_PRESENTATION_COLUMNS) + " |",
    ]
    table.extend(
        "| "
        + " | ".join(
            presentation_value(row.get(field))
            for field, _ in LIST_PRESENTATION_COLUMNS
        )
        + " |"
        for row in rows
    )
    scope = "包含已停用账户" if include_disabled else "仅展示已启用账户"
    return {
        "format": "markdown",
        "required": True,
        "allow_column_omission": False,
        "allow_column_reordering": False,
        "columns": [
            {"field": field, "label": label}
            for field, label in LIST_PRESENTATION_COLUMNS
        ],
        "rendered_markdown": "\n".join([
            f"**负责账户：** 共 {len(rows)} 个；{scope}",
            "",
            *table,
        ]),
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description="Manage locally responsible advertiser accounts.")
    parser.add_argument("action", choices=("list", "add", "remove", "enable", "disable"))
    parser.add_argument("--config")
    parser.add_argument("--channel", choices=tuple(channels.CHANNELS))
    parser.add_argument("--advertiser-id")
    parser.add_argument(
        "--auth-account-id",
        help="Bind this advertiser to one official OAuth account.",
    )
    parser.add_argument("--name")
    parser.add_argument("--all", action="store_true", help="Include disabled accounts when listing.")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    if args.action == "list":
        config = load(config_path)
        accounts = managed_accounts.list_accounts(
            config,
            channel=args.channel,
            enabled_only=not args.all,
        )
        result = {
            "ok": True,
            "accounts": accounts,
            "presentation": list_presentation(accounts, include_disabled=args.all),
        }
        write_json(result)
        return 0

    if not args.channel or not args.advertiser_id:
        parser.error("--channel and --advertiser-id are required for this action")
    if args.action == "add":
        if not args.name:
            parser.error("--name is required when adding an account")
    elif args.auth_account_id is not None:
        parser.error("--auth-account-id is only valid when adding an account")

    def update(config):
        if args.action == "add":
            options = {}
            if args.auth_account_id is not None:
                options["auth_account_id"] = args.auth_account_id
            updated, record, created = managed_accounts.upsert(
                config,
                channel=args.channel,
                advertiser_id_value=args.advertiser_id,
                name=args.name,
                **options,
            )
            action = "created" if created else "updated"
        elif args.action == "remove":
            updated, record = managed_accounts.remove(
                config,
                channel=args.channel,
                advertiser_id_value=args.advertiser_id,
            )
            action = "removed"
        else:
            updated, record = managed_accounts.set_enabled(
                config,
                channel=args.channel,
                advertiser_id_value=args.advertiser_id,
                enabled=args.action == "enable",
            )
            action = "enabled" if args.action == "enable" else "disabled"
        return updated, (record, action)

    record, action = config_store.update_json(config_path, update)
    write_json({"ok": True, "action": action, "account": record})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
