#!/usr/bin/env python3
"""Merge independent Skill evaluation shards, preferring the latest case result."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

try:
    from .run_skill_eval import _redact
except ImportError:  # Executed directly from this directory.
    from run_skill_eval import _redact


def merge(inputs: list[Path], out: Path) -> dict:
    documents = [json.loads(path.read_text(encoding="utf-8")) for path in inputs]
    if not documents:
        raise ValueError("at least one evidence file is required")
    identity = {
        key: documents[0].get(key)
        for key in ("model", "plugin_version", "git_commit", "reasoning")
    }
    by_case: dict[tuple[str, int], dict] = {}
    for document in documents:
        for key in identity:
            if document.get(key) != identity[key]:
                raise ValueError(f"evidence identity mismatch for {key}")
        for row in document.get("results", []):
            by_case[(row["case_id"], row["trial"])] = row
    rows = sorted(by_case.values(), key=lambda row: (row["trial"], row["case_id"]))
    summary = {
        "total": len(rows),
        "passed": sum(row["status"] == "passed" for row in rows),
        "failed": sum(row["status"] == "failed" for row in rows),
        "blocked": sum(row["status"] == "blocked" for row in rows),
        "not_run": sum(row["status"] == "not_run" for row in rows),
    }
    merged = {
        "schema_version": 1,
        "suite": "skill-eval",
        **identity,
        "driver": "merged-shards",
        "trials": max(row["trial"] for row in rows),
        "summary": summary,
        "results": rows,
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(_redact(merged), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return merged


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("inputs", nargs="+", type=Path)
    parser.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    merged = merge(args.inputs, args.out)
    print(json.dumps(merged["summary"], ensure_ascii=False))
    return 1 if merged["summary"]["failed"] or merged["summary"]["blocked"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
