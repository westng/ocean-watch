#!/usr/bin/env python3
import argparse

from ocean_watch.auth import authorization_store, channels, credential_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json


def channel_mappings(channel, advertiser_id=None):
    channel, definition = channels.get(channel)
    state = authorization_store.load_channel_state(channel)
    requested = str(advertiser_id).strip() if advertiser_id is not None else None
    if requested is not None:
        authorization_store.normalize_id(requested, "advertiser_id")
    authorizations = []
    for authorization_id, metadata in sorted(
        (state.get("authorizations") or {}).items()
    ):
        advertiser_ids = [str(value) for value in metadata.get("advertiser_ids") or []]
        if requested is not None and requested not in advertiser_ids:
            continue
        revision = int(metadata.get("token_revision") or 1)
        token = credential_store.read_entry(
            authorization_store.authorization_account(channel, authorization_id, revision)
        )
        accounts = []
        for account in metadata.get("authorized_accounts") or []:
            account_advertisers = [
                str(value) for value in account.get("advertiser_ids") or []
            ]
            if requested is not None and requested not in account_advertisers:
                continue
            accounts.append({
                "account_id": account.get("account_id"),
                "account_name": account.get("account_name"),
                "account_role": account.get("account_role"),
                "account_type": account.get("account_type"),
                "advertiser_ids": account_advertisers,
            })
        authorizations.append({
            "authorization_id": authorization_id,
            "token_revision": revision,
            "has_access_token": bool(token.get("access_token")),
            "has_refresh_token": bool(token.get("refresh_token")),
            "pending_account_sync": bool(metadata.get("pending_account_sync")),
            "advertiser_ids": advertiser_ids,
            "authorized_accounts": accounts,
        })

    mappings = []
    for mapped_advertiser_id, authorization_ids in sorted(
        (state.get("advertiser_index") or {}).items()
    ):
        if requested is not None and mapped_advertiser_id != requested:
            continue
        mappings.append({
            "advertiser_id": mapped_advertiser_id,
            "authorization_ids": list(authorization_ids),
            "ambiguous": len(authorization_ids) > 1,
        })
    return {
        "channel": channel,
        "channel_name": definition["display_name"],
        "advertiser_filter": requested,
        "authorization_count": len(authorizations),
        "mapping_count": len(mappings),
        "mappings": mappings,
        "authorizations": authorizations,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Show sanitized advertiser-to-authorization mappings."
    )
    parser.add_argument("--channel", choices=tuple(channels.CHANNELS))
    parser.add_argument("--advertiser-id")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    selected = [args.channel] if args.channel else list(channels.CHANNELS)
    rows = [channel_mappings(channel, args.advertiser_id) for channel in selected]
    if args.advertiser_id and not any(row["mappings"] for row in rows):
        raise ConfigurationError(
            "advertiser is not mapped to an authorization",
            {"advertiser_id": str(args.advertiser_id), "channels": selected},
        )
    result = {
        "ok": True,
        "mode": "authorization_mappings",
        "credential_values_exposed": False,
        "channels": rows,
    }
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
