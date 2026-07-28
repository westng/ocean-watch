#!/usr/bin/env python3
"""Expand six untrusted run IDs into the fixed G5 acquisition policy."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import canonical_json
    from .g5_source_policy import (
        RUN_KEYS,
        expected_artifact_name,
        expected_workflow_path,
    )
except ImportError:
    from candidate_identity import canonical_json  # type: ignore[no-redef]
    from g5_source_policy import (  # type: ignore[no-redef]
        RUN_KEYS,
        expected_artifact_name,
        expected_workflow_path,
    )


class SourceArtifactError(RuntimeError):
    pass


def validate(document: object) -> list[str]:
    if not isinstance(document, dict) or set(document) != set(RUN_KEYS):
        return ["source artifact plan must contain the exact six evidence keys"]
    errors: list[str] = []
    run_ids: list[int] = []
    for key in RUN_KEYS:
        run_id = document[key]
        if not isinstance(run_id, int) or isinstance(run_id, bool) or run_id <= 0:
            errors.append(f"{key} run ID is malformed")
        else:
            run_ids.append(run_id)
    if len(run_ids) != len(set(run_ids)):
        errors.append("source artifact workflow run IDs must be distinct")
    return errors


def normalize(document: object) -> dict[str, dict[str, Any]]:
    errors = validate(document)
    if errors:
        raise SourceArtifactError("; ".join(errors))
    assert isinstance(document, dict)
    result: dict[str, dict[str, Any]] = {}
    for key in RUN_KEYS:
        run_id = document[key]
        result[key] = {
            "run_id": run_id,
            "workflow_path": expected_workflow_path(key),
            "artifact_name": expected_artifact_name(key, run_id),
        }
    return result


def load(path: Path) -> object:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise SourceArtifactError("source artifact plan is not valid UTF-8 JSON") from error


def write_tsv(path: Path, document: dict[str, dict[str, Any]]) -> None:
    rows = [
        "\t".join(
            (
                key,
                str(document[key]["run_id"]),
                str(document[key]["workflow_path"]),
                str(document[key]["artifact_name"]),
            )
        )
        for key in RUN_KEYS
    ]
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(rows) + "\n", encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--tsv-out", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        result = normalize(load(args.input))
    except SourceArtifactError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(canonical_json(result))
    write_tsv(args.tsv_out, result)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
