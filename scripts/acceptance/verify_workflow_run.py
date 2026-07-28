#!/usr/bin/env python3
"""Verify GitHub Actions run metadata before accepting an artifact."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import canonical_json
except ImportError:
    from candidate_identity import canonical_json  # type: ignore[no-redef]

HEX_40 = re.compile(r"^[a-f0-9]{40}$")


class WorkflowRunError(RuntimeError):
    pass


def verify(
    document: dict[str, Any],
    *,
    expected_run_id: int,
    expected_repository: str,
    expected_workflow_path: str | None = None,
    expected_head_sha: str | None = None,
) -> dict[str, Any]:
    if expected_run_id <= 0:
        raise WorkflowRunError("expected workflow run ID is malformed")
    repository = document.get("repository")
    repository_name = repository.get("full_name") if isinstance(repository, dict) else None
    workflow_path = str(document.get("path") or "").split("@", 1)[0]
    if document.get("id") != expected_run_id:
        raise WorkflowRunError("workflow run ID differs")
    if repository_name != expected_repository:
        raise WorkflowRunError("workflow run belongs to another repository")
    if document.get("event") != "workflow_dispatch":
        raise WorkflowRunError("evidence must come from an explicit workflow dispatch")
    if document.get("status") != "completed" or document.get("conclusion") != "success":
        raise WorkflowRunError("workflow run did not complete successfully")
    head_sha = str(document.get("head_sha") or "")
    if not HEX_40.fullmatch(head_sha) or head_sha == "0" * 40:
        raise WorkflowRunError("workflow run head SHA is malformed")
    if expected_head_sha is not None and head_sha != expected_head_sha:
        raise WorkflowRunError("workflow run head SHA differs")
    if expected_workflow_path is not None and workflow_path != expected_workflow_path:
        raise WorkflowRunError("artifact came from an unexpected workflow")
    run_attempt = document.get("run_attempt")
    if (
        not isinstance(run_attempt, int)
        or isinstance(run_attempt, bool)
        or run_attempt <= 0
    ):
        raise WorkflowRunError("workflow run attempt is malformed")
    return {
        "schema_version": 1,
        "status": "passed",
        "run_id": expected_run_id,
        "repository": expected_repository,
        "workflow_path": workflow_path,
        "head_sha": head_sha,
        "run_attempt": run_attempt,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--metadata", type=Path, required=True)
    parser.add_argument("--expected-run-id", type=int, required=True)
    parser.add_argument("--expected-repository", required=True)
    parser.add_argument("--expected-workflow-path")
    parser.add_argument("--expected-head-sha")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args(argv)
    try:
        document = json.loads(args.metadata.read_text(encoding="utf-8"))
        if not isinstance(document, dict):
            raise WorkflowRunError("workflow run metadata must be an object")
        result = verify(
            document,
            expected_run_id=args.expected_run_id,
            expected_repository=args.expected_repository,
            expected_workflow_path=args.expected_workflow_path,
            expected_head_sha=args.expected_head_sha,
        )
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, WorkflowRunError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    if args.out is not None:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
