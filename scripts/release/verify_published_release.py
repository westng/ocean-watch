#!/usr/bin/env python3
"""Verify that an existing GitHub Release is an exact idempotent publication."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

HEX_40 = re.compile(r"^[a-f0-9]{40}$")
TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


class PublishedReleaseError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def regular_files(root: Path) -> dict[str, Path]:
    try:
        entries = list(root.iterdir())
    except OSError as error:
        raise PublishedReleaseError(f"release asset directory is unreadable: {root}") from error
    if not entries or any(path.is_symlink() or not path.is_file() for path in entries):
        raise PublishedReleaseError("release asset directory must contain regular files only")
    result = {path.name: path for path in entries}
    if len(result) != len(entries):
        raise PublishedReleaseError("release asset names are not unique")
    return result


def load_metadata(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise PublishedReleaseError("GitHub Release metadata is invalid") from error
    if not isinstance(value, dict):
        raise PublishedReleaseError("GitHub Release metadata must be an object")
    return value


def verify(
    candidate_dir: Path,
    published_dir: Path,
    metadata_path: Path,
    notes_path: Path,
    tag: str,
    commit: str,
) -> dict:
    if TAG.fullmatch(tag) is None or HEX_40.fullmatch(commit) is None:
        raise PublishedReleaseError("expected release identity is malformed")
    candidate = regular_files(candidate_dir)
    published = regular_files(published_dir)
    if set(candidate) != set(published):
        raise PublishedReleaseError("published Release asset names differ from the candidate")
    mismatches = [
        name
        for name in sorted(candidate)
        if candidate[name].stat().st_size != published[name].stat().st_size
        or sha256_file(candidate[name]) != sha256_file(published[name])
    ]
    if mismatches:
        raise PublishedReleaseError(f"published Release assets differ: {mismatches}")
    metadata = load_metadata(metadata_path)
    metadata_assets = metadata.get("assets")
    if not isinstance(metadata_assets, list):
        raise PublishedReleaseError("GitHub Release metadata has no asset list")
    metadata_names = [item.get("name") for item in metadata_assets if isinstance(item, dict)]
    if len(metadata_names) != len(metadata_assets) or set(metadata_names) != set(candidate):
        raise PublishedReleaseError("GitHub Release metadata asset set differs from the candidate")
    if (
        metadata.get("tagName") != tag
        or metadata.get("name") != tag
        or metadata.get("targetCommitish") != commit
        or metadata.get("isDraft") is not False
        or metadata.get("isPrerelease") is not False
    ):
        raise PublishedReleaseError("GitHub Release identity or publication state differs")
    try:
        notes = notes_path.read_text(encoding="utf-8").rstrip("\r\n")
    except (OSError, UnicodeDecodeError) as error:
        raise PublishedReleaseError("expected Release notes are unreadable") from error
    if str(metadata.get("body") or "").rstrip("\r\n") != notes:
        raise PublishedReleaseError("GitHub Release notes differ from CHANGELOG.md")
    return {
        "schema_version": 1,
        "status": "passed",
        "tag": tag,
        "git_commit": commit,
        "asset_count": len(candidate),
        "asset_differences": mismatches,
        "notes_match": True,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate-dir", type=Path, required=True)
    parser.add_argument("--published-dir", type=Path, required=True)
    parser.add_argument("--metadata", type=Path, required=True)
    parser.add_argument("--notes", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()
    try:
        result = verify(
            args.candidate_dir,
            args.published_dir,
            args.metadata,
            args.notes,
            args.tag,
            args.commit,
        )
    except PublishedReleaseError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
