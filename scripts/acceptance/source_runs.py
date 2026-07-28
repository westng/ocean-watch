#!/usr/bin/env python3
"""Create and validate the canonical workflow-run provenance for a G5 summary."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import canonical_json
    from .g5_source_policy import RUN_KEYS, expected_workflow_path
except ImportError:
    from candidate_identity import canonical_json  # type: ignore[no-redef]
    from g5_source_policy import (  # type: ignore[no-redef]
        RUN_KEYS,
        expected_workflow_path,
    )

HEX_40 = re.compile(r"^[a-f0-9]{40}$")
RUN_FIELDS = {"run_id", "workflow_path", "head_sha", "run_attempt"}


class SourceRunError(RuntimeError):
    pass


def validate(
    document: object,
    *,
    expected_git_sha: str | None = None,
    expected_repository: str = "westng/ocean-watch",
) -> list[str]:
    if not isinstance(document, dict):
        return ["source run manifest must be an object"]
    errors: list[str] = []
    if set(document) != {"schema_version", "candidate_git_sha", "repository", "runs"}:
        errors.append("source run manifest fields differ")
    if document.get("schema_version") != 1:
        errors.append("source run manifest schema_version must be 1")
    git_sha = document.get("candidate_git_sha")
    if not isinstance(git_sha, str) or not HEX_40.fullmatch(git_sha) or git_sha == "0" * 40:
        errors.append("source run candidate git SHA is malformed")
    elif expected_git_sha is not None and git_sha != expected_git_sha:
        errors.append("source run candidate git SHA differs")
    if document.get("repository") != expected_repository:
        errors.append("source runs belong to another repository")
    runs = document.get("runs")
    if not isinstance(runs, dict) or set(runs) != set(RUN_KEYS):
        errors.append("source run set is incomplete")
        return errors
    run_ids: list[int] = []
    for key in RUN_KEYS:
        row = runs[key]
        if not isinstance(row, dict) or set(row) != RUN_FIELDS:
            errors.append(f"{key} source run fields differ")
            continue
        run_id = row.get("run_id")
        run_attempt = row.get("run_attempt")
        workflow_path = row.get("workflow_path")
        head_sha = row.get("head_sha")
        if not isinstance(run_id, int) or isinstance(run_id, bool) or run_id <= 0:
            errors.append(f"{key} run ID is malformed")
        else:
            run_ids.append(run_id)
        if not isinstance(run_attempt, int) or isinstance(run_attempt, bool) or run_attempt <= 0:
            errors.append(f"{key} run attempt is malformed")
        if workflow_path != expected_workflow_path(key):
            errors.append(f"{key} workflow path differs from the trusted source policy")
        if not isinstance(head_sha, str) or head_sha != git_sha:
            errors.append(f"{key} run head SHA differs")
    if len(run_ids) != len(set(run_ids)):
        errors.append("source workflow run IDs must be distinct")
    return errors


def build(candidate_git_sha: str, repository: str, runs: dict[str, dict[str, Any]]) -> dict:
    normalized_runs = {
        key: {field: runs[key].get(field) for field in RUN_FIELDS}
        for key in RUN_KEYS
        if key in runs and isinstance(runs[key], dict)
    }
    document = {
        "schema_version": 1,
        "candidate_git_sha": candidate_git_sha,
        "repository": repository,
        "runs": normalized_runs,
    }
    errors = validate(
        document,
        expected_git_sha=candidate_git_sha,
        expected_repository=repository,
    )
    if errors:
        raise SourceRunError("; ".join(errors))
    return document


def load(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise SourceRunError(f"source run metadata is invalid: {path}") from error
    if not isinstance(value, dict):
        raise SourceRunError(f"source run metadata must be an object: {path}")
    return value


def digest(document: dict[str, Any]) -> str:
    errors = validate(document)
    if errors:
        raise SourceRunError("; ".join(errors))
    return hashlib.sha256(canonical_json(document)).hexdigest()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate-git-sha", required=True)
    parser.add_argument("--repository", required=True)
    for key in RUN_KEYS:
        parser.add_argument(
            f"--{key.replace('_', '-')}-metadata",
            type=Path,
            required=True,
        )
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        runs = {
            key: load(getattr(args, f"{key}_metadata"))
            for key in RUN_KEYS
        }
        result = build(args.candidate_git_sha, args.repository, runs)
    except SourceRunError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
