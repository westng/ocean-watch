#!/usr/bin/env python3
"""Create candidate-bound evidence for the protected release trust root."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import canonical_json, release_public_key_sha256
    from .p5 import AcceptanceError, verify_candidate
except ImportError:
    from candidate_identity import (  # type: ignore[no-redef]
        canonical_json,
        release_public_key_sha256,
    )
    from p5 import AcceptanceError, verify_candidate  # type: ignore[no-redef]

REPOSITORY = "westng/ocean-watch"
MAIN_REF = "refs/heads/main"
PROTECTED_ENVIRONMENT = "g5-release-candidate"
HEX_40 = re.compile(r"^[a-f0-9]{40}$")
HEX_64 = re.compile(r"^[a-f0-9]{64}$")


class ProtectedReleaseError(RuntimeError):
    pass


def build_evidence(
    *,
    candidate_dir: Path,
    expected_public_key_hex: str,
    git_sha: str,
    repository: str,
    ref: str,
    run_id: str,
    run_attempt: str,
    environment_name: str,
    actor: str,
    ref_protected: bool,
) -> dict[str, Any]:
    if not HEX_40.fullmatch(git_sha) or git_sha == "0" * 40:
        raise ProtectedReleaseError("protected release commit is malformed")
    if repository != REPOSITORY or ref != MAIN_REF:
        raise ProtectedReleaseError("formal release evidence must run from westng/ocean-watch main")
    if environment_name != PROTECTED_ENVIRONMENT:
        raise ProtectedReleaseError("formal signing must use the protected G5 environment")
    if ref_protected is not True:
        raise ProtectedReleaseError("formal signing requires a protected main ref")
    if not run_id.isdigit() or int(run_id) <= 0:
        raise ProtectedReleaseError("GitHub workflow run ID is malformed")
    if not run_attempt.isdigit() or int(run_attempt) <= 0:
        raise ProtectedReleaseError("GitHub workflow run attempt is malformed")
    if not actor.strip() or actor.strip().lower() in {"pending", "unknown", "none"}:
        raise ProtectedReleaseError("GitHub workflow actor is missing")
    if not HEX_64.fullmatch(expected_public_key_hex):
        raise ProtectedReleaseError("approved release public key must be 32-byte lowercase hex")
    verified = verify_candidate(
        candidate_dir,
        verify_signatures=True,
        require_release=True,
        expected_commit=git_sha,
    )
    try:
        candidate_public_key = (candidate_dir / "release-public-key.txt").read_text(
            encoding="utf-8"
        ).strip()
    except (OSError, UnicodeDecodeError) as error:
        raise ProtectedReleaseError("candidate public key is unreadable") from error
    if candidate_public_key != expected_public_key_hex:
        raise ProtectedReleaseError("candidate signing key does not match the approved trust root")
    trust_root_digest = release_public_key_sha256(expected_public_key_hex)
    identity = verified["candidate_identity"]
    if identity.get("release_public_key_sha256") != trust_root_digest:
        raise ProtectedReleaseError("candidate identity does not bind the approved trust root")
    return {
        "schema_version": 1,
        "acceptance": "AC-125",
        "kind": "protected_ci",
        "status": "passed",
        "git_sha": git_sha,
        "candidate_identity": identity,
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "release_public_key_sha256": trust_root_digest,
        "repository": repository,
        "ref": ref,
        "ref_protected": True,
        "protected_environment": environment_name,
        "workflow_run_id": int(run_id),
        "workflow_run_attempt": int(run_attempt),
        "workflow_actor": actor.strip(),
        "signatures_verified": True,
        "approved_public_key_matched": True,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate-dir", type=Path, required=True)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--environment-name", default=PROTECTED_ENVIRONMENT)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    expected_public_key = os.environ.get("OCEAN_WATCH_RELEASE_PUBLIC_KEY", "").strip()
    try:
        result = build_evidence(
            candidate_dir=args.candidate_dir,
            expected_public_key_hex=expected_public_key,
            git_sha=args.expected_commit,
            repository=os.environ.get("GITHUB_REPOSITORY", ""),
            ref=os.environ.get("GITHUB_REF", ""),
            run_id=os.environ.get("GITHUB_RUN_ID", ""),
            run_attempt=os.environ.get("GITHUB_RUN_ATTEMPT", ""),
            environment_name=args.environment_name,
            actor=os.environ.get("GITHUB_ACTOR", ""),
            ref_protected=os.environ.get("GITHUB_REF_PROTECTED", "").lower() == "true",
        )
    except (AcceptanceError, ProtectedReleaseError, OSError, ValueError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
