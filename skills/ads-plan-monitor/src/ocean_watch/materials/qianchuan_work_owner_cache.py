import datetime as dt
import json
from pathlib import Path

from ocean_watch.auth import authorization_store
from ocean_watch.core import config_store

CACHE_SCHEMA_VERSION = 1
CACHE_TTL_DAYS = 30
MAX_ENTRIES_PER_ADVERTISER = 10000


def default_cache_path():
    return authorization_store.state_root() / "cache" / "qianchuan-work-owners.json"


def empty_cache():
    return {"schema_version": CACHE_SCHEMA_VERSION, "advertisers": {}}


def parse_timestamp(value):
    try:
        parsed = dt.datetime.fromisoformat(str(value))
    except (TypeError, ValueError):
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc)


def normalized_cache(data):
    if not isinstance(data, dict):
        return empty_cache()
    if data.get("schema_version") != CACHE_SCHEMA_VERSION:
        return empty_cache()
    advertisers = data.get("advertisers")
    if not isinstance(advertisers, dict):
        return empty_cache()
    return {"schema_version": CACHE_SCHEMA_VERSION, "advertisers": advertisers}


def read_cache(path=None):
    path = Path(path or default_cache_path())
    if not path.exists():
        return empty_cache()
    try:
        return normalized_cache(json.loads(path.read_text(encoding="utf-8")))
    except (OSError, json.JSONDecodeError):
        return empty_cache()


def load_owner_hints(advertiser_id, item_ids, *, path=None, now=None):
    advertiser_id = str(advertiser_id)
    requested = {str(value) for value in item_ids}
    now = now or dt.datetime.now(dt.timezone.utc)
    cutoff = now - dt.timedelta(days=CACHE_TTL_DAYS)
    rows = (read_cache(path).get("advertisers") or {}).get(advertiser_id) or {}
    hints = {}
    for item_id in requested:
        row = rows.get(item_id)
        if not isinstance(row, dict):
            continue
        aweme_id = str(row.get("aweme_id") or "")
        aweme_show_id = str(row.get("aweme_show_id") or "").strip()
        updated_at = parse_timestamp(row.get("updated_at"))
        if not aweme_id.isdigit() or updated_at is None or updated_at < cutoff:
            continue
        hints[item_id] = {
            "aweme_id": aweme_id,
            "aweme_show_id": aweme_show_id or None,
        }
    return hints


def update_owner_hints(advertiser_id, owner_hints, *, path=None, now=None):
    normalized_hints = {}
    for item_id, value in (owner_hints or {}).items():
        item_id = str(item_id)
        if isinstance(value, dict):
            aweme_id = str(value.get("aweme_id") or "")
            aweme_show_id = str(value.get("aweme_show_id") or "").strip() or None
        else:
            aweme_id = str(value or "")
            aweme_show_id = None
        if item_id.isdigit() and aweme_id.isdigit():
            normalized_hints[item_id] = {
                "aweme_id": aweme_id,
                "aweme_show_id": aweme_show_id,
            }
    if not normalized_hints:
        return 0
    advertiser_id = str(advertiser_id)
    path = Path(path or default_cache_path())
    now = now or dt.datetime.now(dt.timezone.utc)
    timestamp = now.astimezone(dt.timezone.utc).isoformat()
    with config_store.json_file_lock(path):
        cache = read_cache(path)
        advertisers = cache.setdefault("advertisers", {})
        rows = advertisers.setdefault(advertiser_id, {})
        for item_id, hint in normalized_hints.items():
            rows[item_id] = {**hint, "updated_at": timestamp}
        if len(rows) > MAX_ENTRIES_PER_ADVERTISER:
            retained = sorted(
                rows.items(),
                key=lambda item: parse_timestamp((item[1] or {}).get("updated_at"))
                or dt.datetime.min.replace(tzinfo=dt.timezone.utc),
                reverse=True,
            )[:MAX_ENTRIES_PER_ADVERTISER]
            advertisers[advertiser_id] = dict(retained)
        config_store.atomic_write_json(path, cache, backup=False)
    return len(normalized_hints)
