#!/usr/bin/env python3
"""Assemble candidate-bound native platform evidence from five runner shards."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

try:
    from .ac import MANIFEST_PATH, canonical_json, load_manifest
    from .candidate_identity import (
        CandidateIdentityError,
        candidate_identity_sha256,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )
except ImportError:
    from ac import MANIFEST_PATH, canonical_json, load_manifest  # type: ignore[no-redef]
    from candidate_identity import (  # type: ignore[no-redef]
        CandidateIdentityError,
        candidate_identity_sha256,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )


class NativeMatrixError(RuntimeError):
    pass


def _load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise NativeMatrixError(f"invalid native evidence: {path}") from error
    if not isinstance(value, dict):
        raise NativeMatrixError(f"native evidence must be an object: {path}")
    return value


def _expected_tests(contract: dict[str, Any], platform_id: str) -> set[tuple[str, str]]:
    expected: set[tuple[str, str]] = set()
    for group, names in contract.get("tests", {}).items():
        if group == "race" and platform_id.startswith("windows-"):
            continue
        expected.update((group, name) for name in names)
    return expected


def _validate_shard(
    shard: Path,
    platform_id: str,
    acceptance_id: str,
    contract: dict[str, Any],
    candidate_identity: dict[str, Any],
) -> list[str]:
    errors: list[str] = []
    environment = _load_json(shard / "environment.json")
    runner = _load_json(shard / "runner-summary.json")
    result = _load_json(shard / "ac-results" / f"{acceptance_id.lower()}.json")
    for label, document in (
        ("environment", environment),
        ("runner", runner),
        (acceptance_id, result),
    ):
        if document.get("git_sha") != candidate_identity["git_sha"]:
            errors.append(f"{label} git SHA differs")
        if document.get("platform") != platform_id:
            errors.append(f"{label} platform differs")
        errors.extend(
            f"{label}: {error}"
            for error in compare_candidate_identities(
                document.get("candidate_identity"), candidate_identity
            )
        )
    if environment.get("working_tree_dirty") is not False:
        errors.append("environment was produced from a dirty checkout")
    if runner.get("working_tree_dirty") is not False:
        errors.append("runner was produced from a dirty checkout")
    if runner.get("runner_errors") not in ([], None):
        errors.append("runner contains execution or parse errors")
    if runner.get("status") == "failed":
        errors.append("runner status is failed")
    if result.get("acceptance_id") != acceptance_id:
        errors.append("acceptance result identity differs")
    expected = _expected_tests(contract, platform_id)
    observed: dict[tuple[str, str], str] = {}
    for row in result.get("tests", []):
        if not isinstance(row, dict):
            errors.append("acceptance test row is malformed")
            continue
        key = (str(row.get("group") or ""), str(row.get("name") or ""))
        if key in observed:
            errors.append(f"duplicate test result: {key[0]}/{key[1]}")
        observed[key] = str(row.get("status") or "")
    if set(observed) != expected:
        errors.append("native acceptance test set differs")
    failed = sorted(f"{group}/{name}" for (group, name), status in observed.items() if status != "passed")
    if failed:
        errors.append(f"native acceptance tests did not pass: {failed}")
    return errors


def assemble(
    *,
    shard_root: Path,
    candidate_identity: dict[str, Any],
    acceptance_id: str = "AC-107",
) -> dict[str, Any]:
    identity_errors = validate_candidate_identity(candidate_identity, require_release=True)
    if identity_errors:
        raise NativeMatrixError("invalid formal candidate identity: " + "; ".join(identity_errors))
    manifest = load_manifest(MANIFEST_PATH)
    platforms = manifest.get("required_platforms")
    acceptance = manifest.get("acceptance")
    contract = acceptance.get(acceptance_id) if isinstance(acceptance, dict) else None
    if not isinstance(platforms, list) or not platforms or not isinstance(contract, dict):
        raise NativeMatrixError("acceptance manifest does not define the native matrix")
    rows = []
    all_errors: list[str] = []
    for platform_id in platforms:
        if not isinstance(platform_id, str):
            raise NativeMatrixError("acceptance manifest contains an invalid platform")
        shard = shard_root / platform_id
        try:
            errors = _validate_shard(
                shard,
                platform_id,
                acceptance_id,
                contract,
                candidate_identity,
            )
        except NativeMatrixError as error:
            errors = [str(error)]
        rows.append(
            {
                "platform": platform_id,
                "status": "failed" if errors else "passed",
                "errors": errors,
            }
        )
        all_errors.extend(f"{platform_id}: {error}" for error in errors)
    if all_errors:
        raise NativeMatrixError("; ".join(all_errors))
    return {
        "schema_version": 1,
        "acceptance": acceptance_id,
        "status": "passed",
        "git_sha": candidate_identity["git_sha"],
        "candidate_identity": candidate_identity,
        "candidate_identity_sha256": candidate_identity_sha256(candidate_identity),
        "platforms": rows,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--shard-root", type=Path, required=True)
    parser.add_argument("--candidate-identity", type=Path, required=True)
    parser.add_argument("--acceptance-id", default="AC-107")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        identity = load_candidate_identity(args.candidate_identity)
        result = assemble(
            shard_root=args.shard_root,
            candidate_identity=identity,
            acceptance_id=args.acceptance_id,
        )
    except (CandidateIdentityError, NativeMatrixError, OSError, ValueError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
