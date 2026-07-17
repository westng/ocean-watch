#!/usr/bin/env python3
import argparse
import json
import re
from pathlib import Path

from ocean_watch.auth import authorization_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json

RUN_ID_PATTERN = re.compile(r"[a-z0-9][a-z0-9-]{0,127}\Z")


def runs_root():
    return authorization_store.state_root() / "runs"


def safe_run_path(run_id, root=None):
    run_id = str(run_id or "").strip()
    if not RUN_ID_PATTERN.fullmatch(run_id):
        raise ConfigurationError("run_id contains unsupported characters")
    return Path(root or runs_root()) / f"{run_id}.json"


def read_run(path, root=None):
    path = Path(path)
    root = Path(root or runs_root())
    if path.is_symlink():
        raise ConfigurationError("run journal symbolic links are not supported")
    try:
        resolved_root = root.resolve(strict=False)
        resolved_path = path.resolve(strict=True)
        resolved_path.relative_to(resolved_root)
    except FileNotFoundError as error:
        raise ConfigurationError("run not found", {"run_id": path.stem}) from error
    except ValueError as error:
        raise ConfigurationError("run journal is outside the managed state directory") from error
    try:
        data = json.loads(resolved_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ConfigurationError("run journal is unreadable", {"run_id": path.stem}) from error
    jobs = data.get("jobs", {}) if isinstance(data, dict) else None
    if not isinstance(jobs, dict) or any(
        not isinstance(job, dict) for job in jobs.values()
    ):
        raise ConfigurationError("run journal has an invalid schema", {"run_id": path.stem})
    return data


def summarize_run(path, data):
    jobs = data.get("jobs") or {}
    status_counts = {}
    for row in (jobs or {}).values():
        status = str((row or {}).get("status") or "unknown")
        status_counts[status] = status_counts.get(status, 0) + 1
    return {
        "run_id": path.stem,
        "kind": path.stem.rsplit("-", 1)[0],
        "schema_version": data.get("schema_version"),
        "created_at": data.get("created_at"),
        "fingerprint": data.get("fingerprint"),
        "job_count": len(jobs or {}),
        "status_counts": status_counts,
        "updated_at": path.stat().st_mtime,
    }


def list_runs(root=None, limit=50):
    root = Path(root or runs_root())
    if limit < 1 or limit > 500:
        raise ConfigurationError("limit must be between 1 and 500")
    rows = []
    for path in root.glob("*.json") if root.is_dir() else []:
        try:
            rows.append(summarize_run(path, read_run(path, root=root)))
        except ConfigurationError:
            rows.append({"run_id": path.stem, "readable": False})
    rows.sort(key=lambda row: row.get("updated_at") or 0, reverse=True)
    return rows[:limit]


def main(argv=None):
    parser = argparse.ArgumentParser(description="Read local Ocean Watch execution journals.")
    parser.add_argument("action", choices=("list", "show"))
    parser.add_argument("--run-id")
    parser.add_argument("--limit", type=int, default=50)
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if args.action == "show" and not args.run_id:
        raise ConfigurationError("--run-id is required for show")
    if args.action == "list" and args.run_id:
        raise ConfigurationError("--run-id is only valid for show")

    if args.action == "list":
        rows = list_runs(limit=args.limit)
        result = {"mode": "run_history", "run_count": len(rows), "runs": rows}
    else:
        path = safe_run_path(args.run_id)
        data = read_run(path)
        result = {
            "mode": "run_detail",
            "summary": summarize_run(path, data),
            "run": data,
        }
    write_json(result, destination=args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
