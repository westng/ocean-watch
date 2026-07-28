#!/usr/bin/env python3
"""Merge downloaded evidence artifacts without allowing path replacement."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import sys
from pathlib import Path


class EvidenceMergeError(RuntimeError):
    pass


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def merge(sources: list[Path], destination: Path) -> dict:
    if not sources:
        raise EvidenceMergeError("at least one evidence source is required")
    resolved_destination = destination.resolve()
    resolved_destination.mkdir(parents=True, exist_ok=True, mode=0o700)
    copied = 0
    duplicates = 0
    for source in sources:
        resolved_source = source.resolve()
        if not resolved_source.is_dir() or source.is_symlink():
            raise EvidenceMergeError(f"evidence source is not a regular directory: {source}")
        if resolved_source == resolved_destination or resolved_destination in resolved_source.parents:
            raise EvidenceMergeError("evidence source and destination overlap")
        for path in sorted(resolved_source.rglob("*")):
            if path.is_symlink():
                raise EvidenceMergeError(f"evidence artifact contains a symlink: {path}")
            if path.is_dir():
                continue
            if not path.is_file():
                raise EvidenceMergeError(f"evidence artifact contains a special file: {path}")
            relative = path.relative_to(resolved_source)
            if not relative.parts or any(part in {"", ".", ".."} for part in relative.parts):
                raise EvidenceMergeError(f"evidence artifact path is unsafe: {relative}")
            target = resolved_destination / relative
            target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if target.exists():
                if target.is_symlink() or not target.is_file():
                    raise EvidenceMergeError(f"evidence target is not a regular file: {relative}")
                if target.stat().st_size != path.stat().st_size or _sha256(target) != _sha256(path):
                    raise EvidenceMergeError(f"evidence artifacts conflict at: {relative}")
                duplicates += 1
                continue
            shutil.copyfile(path, target)
            target.chmod(0o600)
            copied += 1
    if copied == 0:
        raise EvidenceMergeError("evidence artifacts contained no new regular files")
    files = [path for path in resolved_destination.rglob("*") if path.is_file()]
    return {
        "schema_version": 1,
        "status": "passed",
        "source_count": len(sources),
        "copied_files": copied,
        "identical_duplicates": duplicates,
        "combined_files": len(files),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, action="append", required=True)
    parser.add_argument("--destination", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        result = merge(args.source, args.destination)
    except (EvidenceMergeError, OSError, ValueError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
