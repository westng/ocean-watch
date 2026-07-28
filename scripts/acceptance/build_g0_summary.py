#!/usr/bin/env python3
"""Build a deterministic, commit-bound G0 evidence summary."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

import yaml

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_STATUS = ROOT / "contracts" / "p0-status.yaml"
DEFAULT_OUT = ROOT / "artifacts" / "go-sdk-acceptance" / "p0" / "summary.json"


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _inside_root(root: Path, path: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True


def _evidence_files(root: Path, value: str) -> list[Path]:
    relative = Path(value)
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError(f"evidence path escapes repository root: {value}")
    candidate = root / relative
    resolved = candidate.resolve()
    if not _inside_root(root.resolve(), resolved):
        raise ValueError(f"evidence path escapes repository root: {value}")
    if not candidate.exists():
        return []
    if candidate.is_symlink():
        raise ValueError(f"evidence path cannot be a symlink: {value}")
    if candidate.is_file():
        return [candidate]
    files = []
    for path in sorted(candidate.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"evidence tree contains a symlink: {path.relative_to(root)}")
        if path.is_file():
            files.append(path)
    return files


def _git_commit(root: Path) -> str:
    completed = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=root,
        capture_output=True,
        text=True,
        check=True,
    )
    return completed.stdout.strip()


def _git_dirty(root: Path) -> bool:
    completed = subprocess.run(
        ["git", "status", "--porcelain", "--untracked-files=all"],
        cwd=root,
        capture_output=True,
        text=True,
        check=True,
    )
    return bool(completed.stdout.strip())


def _skill_model_summary(path: Path, relative: str) -> dict[str, Any] | None:
    if not path.name.startswith("skill-model-") or path.suffix != ".json":
        return None
    document = json.loads(path.read_text(encoding="utf-8"))
    summary = document.get("summary")
    if not isinstance(summary, dict):
        return None
    return {
        "path": relative,
        "model": document.get("model"),
        "reasoning": document.get("reasoning"),
        "plugin_version": document.get("plugin_version"),
        "git_commit": document.get("git_commit"),
        "summary": summary,
    }


def build_summary(
    root: Path,
    status_path: Path,
    *,
    git_commit: str | None = None,
    dirty: bool | None = None,
) -> dict[str, Any]:
    root = root.resolve()
    status = yaml.safe_load(status_path.read_text(encoding="utf-8"))
    if not isinstance(status, dict) or status.get("schema_version") != 1:
        raise ValueError("P0 status must use schema_version 1")
    commit = git_commit or _git_commit(root)
    if len(commit) != 40 or any(character not in "0123456789abcdef" for character in commit):
        raise ValueError("git commit must be a full lowercase SHA-1")
    working_tree_dirty = _git_dirty(root) if dirty is None else dirty

    blockers = list((status.get("g0") or {}).get("blockers") or [])
    if working_tree_dirty:
        blockers.append("working tree is dirty; evidence is not bound to an immutable commit")

    records: dict[str, dict[str, Any]] = {}
    model_runs: list[dict[str, Any]] = []
    missing: list[str] = []
    for task_id, task in (status.get("tasks") or {}).items():
        for evidence in task.get("evidence", []):
            paths = _evidence_files(root, evidence)
            if not paths:
                missing.append(evidence)
                continue
            for path in paths:
                relative = path.relative_to(root).as_posix()
                if relative not in records:
                    records[relative] = {
                        "path": relative,
                        "sha256": _sha256(path),
                        "size": path.stat().st_size,
                        "tasks": [],
                    }
                    model_summary = _skill_model_summary(path, relative)
                    if model_summary:
                        model_runs.append(model_summary)
                records[relative]["tasks"].append(task_id)

    status_relative = status_path.resolve().relative_to(root).as_posix()
    records[status_relative] = {
        "path": status_relative,
        "sha256": _sha256(status_path),
        "size": status_path.stat().st_size,
        "tasks": ["G0"],
    }
    for record in records.values():
        record["tasks"] = sorted(set(record["tasks"]))
    for evidence in sorted(set(missing)):
        blockers.append(f"missing evidence: {evidence}")

    blockers = list(dict.fromkeys(str(blocker) for blocker in blockers))
    ready = not blockers
    return {
        "schema_version": 1,
        "gate": "G0",
        "git_commit": commit,
        "branch": status.get("branch"),
        "stage": status.get("stage"),
        "status": "ready" if ready else "blocked",
        "ready": ready,
        "working_tree_dirty": working_tree_dirty,
        "task_statuses": {
            task_id: task.get("status")
            for task_id, task in sorted((status.get("tasks") or {}).items())
        },
        "blockers": blockers,
        "evidence": [records[path] for path in sorted(records)],
        "skill_model_runs": sorted(model_runs, key=lambda row: row["path"]),
    }


def encode_summary(summary: dict[str, Any]) -> bytes:
    return (
        json.dumps(summary, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"
    ).encode("utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--status", type=Path, default=DEFAULT_STATUS)
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--git-commit")
    parser.add_argument("--require-ready", action="store_true")
    args = parser.parse_args()
    summary = build_summary(ROOT, args.status, git_commit=args.git_commit)
    payload = encode_summary(summary)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(payload)
    result = {
        "path": str(args.out),
        "status": summary["status"],
        "blocker_count": len(summary["blockers"]),
        "evidence_sha256": hashlib.sha256(payload).hexdigest(),
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 1 if args.require_ready and not summary["ready"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
