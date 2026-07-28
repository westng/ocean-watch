#!/usr/bin/env python3
"""Verify independent Gate approvals against the exact final summary bytes."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

import yaml

try:
    from .candidate_identity import (
        candidate_identity_sha256,
        compare_candidate_identities,
        validate_candidate_identity,
    )
except ImportError:
    from candidate_identity import (  # type: ignore[no-redef]
        candidate_identity_sha256,
        compare_candidate_identities,
        validate_candidate_identity,
    )

try:
    from .source_runs import digest as source_runs_sha256
    from .source_runs import validate as validate_source_runs
except ImportError:
    from source_runs import (  # type: ignore[no-redef]
        digest as source_runs_sha256,
    )
    from source_runs import (
        validate as validate_source_runs,
    )

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "contracts" / "acceptance" / "ac-manifest.yaml"
ZERO_COMMIT = "0" * 40
ZERO_DIGEST = "0" * 64
PLACEHOLDER_IDENTITIES = {"", "pending", "todo", "tbd", "reviewer_id"}


def _parse_time(value: object) -> dt.datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed.astimezone(dt.timezone.utc) if parsed.tzinfo is not None else None


def _required_roles(manifest: dict[str, Any], gate: str) -> set[str]:
    gates = manifest.get("gates")
    contract = gates.get(gate) if isinstance(gates, dict) else None
    roles = contract.get("required_roles") if isinstance(contract, dict) else None
    return set(roles) if isinstance(roles, list) and all(isinstance(role, str) for role in roles) else set()


def verify(
    document: dict[str, Any],
    summary_bytes: bytes | None,
    *,
    manifest: dict[str, Any] | None = None,
    expected_git_sha: str | None = None,
    now: dt.datetime | None = None,
) -> list[str]:
    errors: list[str] = []
    manifest = manifest or yaml.safe_load(MANIFEST.read_text(encoding="utf-8"))
    gate = document.get("gate")
    required_roles = _required_roles(manifest, str(gate))
    if document.get("schema_version") != 1 or not required_roles:
        errors.append("invalid Gate signoff identity")
    commit = document.get("git_sha", document.get("git_commit"))
    if not re.fullmatch(r"[a-f0-9]{40}", str(commit or "")):
        errors.append("git_sha must be a full lowercase SHA-1")
    elif commit == ZERO_COMMIT:
        errors.append("git_sha cannot use the all-zero placeholder")
    if expected_git_sha is not None and commit != expected_git_sha:
        errors.append("signoff git_sha does not match the expected release commit")
    sdk_version = document.get("sdk_version")
    if gate != "G0" and sdk_version != manifest.get("sdk_version"):
        errors.append("signoff sdk_version does not match the acceptance manifest")
    signoff_candidate = document.get("candidate_identity")
    if gate == "G5":
        errors.extend(
            validate_candidate_identity(
                signoff_candidate,
                expected_git_sha=str(commit or ""),
                expected_sdk_version=str(manifest.get("sdk_version") or ""),
                require_release=True,
            )
        )
        try:
            signoff_candidate_digest = candidate_identity_sha256(signoff_candidate)
        except ValueError:
            signoff_candidate_digest = None
        if document.get("candidate_identity_sha256") != signoff_candidate_digest:
            errors.append(
                "signoff candidate_identity_sha256 does not match candidate_identity"
            )
    digest = document.get("evidence_sha256")
    if not re.fullmatch(r"[a-f0-9]{64}", str(digest or "")):
        errors.append("evidence_sha256 must be a lowercase SHA-256")
    elif digest == ZERO_DIGEST:
        errors.append("evidence_sha256 cannot use the all-zero placeholder")
    approvals = document.get("approvals")
    if not isinstance(approvals, list):
        return errors + ["approvals must be a list"]
    by_role: dict[str, dict[str, Any]] = {}
    for approval in approvals:
        role = approval.get("role") if isinstance(approval, dict) else None
        if role not in required_roles:
            errors.append(f"unknown approval role: {role}")
            continue
        if role in by_role:
            errors.append(f"duplicate approval role: {role}")
        by_role[role] = approval
    missing_roles = required_roles - set(by_role)
    if missing_roles:
        errors.append(f"missing approval roles: {sorted(missing_roles)}")
    identities: set[str] = set()
    approval_times: dict[str, dt.datetime] = {}
    now = (now or dt.datetime.now(dt.timezone.utc)).astimezone(dt.timezone.utc)
    for role in required_roles & set(by_role):
        approval = by_role[role]
        if approval.get("decision") != "approved":
            errors.append(f"{role} is not approved")
        identity = str(approval.get("identity", approval.get("approver", ""))).strip()
        if identity.lower() in PLACEHOLDER_IDENTITIES:
            errors.append(f"{role} identity is missing or a placeholder")
        else:
            identities.add(identity)
        approved_at = _parse_time(approval.get("approved_at"))
        if approved_at is None:
            errors.append(f"{role} approved_at must be an RFC3339 timestamp with timezone")
        elif approved_at > now:
            errors.append(f"{role} approved_at cannot be in the future")
        else:
            approval_times[role] = approved_at
    if len(identities) < 2:
        errors.append(f"{gate} requires at least two distinct approvers")
    if gate == "G5" and {"SO", "RO"} <= set(by_role):
        security_identity = str(by_role["SO"].get("identity", "")).strip()
        release_identity = str(by_role["RO"].get("identity", "")).strip()
        if security_identity and security_identity == release_identity:
            errors.append("G5 requires distinct Security and Release Owner approvers")
    if summary_bytes is None:
        errors.append("Gate evidence summary is required")
        return errors
    if hashlib.sha256(summary_bytes).hexdigest() != digest:
        errors.append("evidence_sha256 does not match the supplied summary")
    try:
        summary = json.loads(summary_bytes)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return errors + ["Gate evidence summary is not valid UTF-8 JSON"]
    if not isinstance(summary, dict) or summary.get("schema_version") != 1:
        errors.append("Gate evidence summary must use schema_version 1")
        return errors
    summary_commit = summary.get("git_sha", summary.get("git_commit"))
    if summary.get("gate") != gate:
        errors.append("signoff gate does not match the evidence summary")
    if summary_commit != commit:
        errors.append("signoff git_sha does not match the evidence summary")
    if gate != "G0" and summary.get("sdk_version") != sdk_version:
        errors.append("signoff sdk_version does not match the evidence summary")
    if gate == "G5":
        errors.extend(
            compare_candidate_identities(
                summary.get("candidate_identity"), signoff_candidate
            )
        )
        if summary.get("candidate_identity_sha256") != document.get(
            "candidate_identity_sha256"
        ):
            errors.append(
                "signoff candidate_identity_sha256 does not match the evidence summary"
            )
        summary_source_runs = summary.get("source_runs")
        source_run_errors = validate_source_runs(
            summary_source_runs,
            expected_git_sha=str(commit or ""),
        )
        errors.extend(f"source_runs: {error}" for error in source_run_errors)
        if not source_run_errors:
            expected_source_runs_sha256 = source_runs_sha256(summary_source_runs)
            if summary.get("source_runs_sha256") != expected_source_runs_sha256:
                errors.append(
                    "source_runs_sha256 does not match the evidence summary source runs"
                )
    ready_status = "ready" if gate == "G0" else "passed"
    if summary.get("ready") is not True or summary.get("status") != ready_status:
        errors.append("Gate evidence summary is not ready")
    counts = summary.get("counts")
    if gate != "G0":
        if not isinstance(counts, dict):
            errors.append("Gate evidence summary counts are missing")
        else:
            for key in ("failed", "blocking", "missing", "not_run", "expired_exceptions"):
                if counts.get(key) != 0:
                    errors.append(f"Gate evidence summary {key} must be zero")
    if summary.get("blockers"):
        errors.append("Gate evidence summary still contains blockers")
    evaluated_at = _parse_time(summary.get("evaluated_at"))
    if gate != "G0" and evaluated_at is None:
        errors.append("Gate evidence summary evaluated_at is invalid")
    elif evaluated_at is not None:
        for role, approved_at in approval_times.items():
            if approved_at < evaluated_at:
                errors.append(f"{role} approval predates the evidence summary")
    summary_exceptions = (summary.get("exceptions") or {}).get("active", [])
    signoff_exceptions = document.get("exceptions", [])
    if not isinstance(signoff_exceptions, list):
        errors.append("signoff exceptions must be a list")
    elif signoff_exceptions != summary_exceptions:
        errors.append("signoff exceptions do not match the evidence summary")
    return errors


def _is_tracked(path: Path) -> bool:
    try:
        relative = path.resolve().relative_to(ROOT.resolve())
    except ValueError:
        return False
    completed = subprocess.run(
        ["git", "ls-files", "--error-unmatch", relative.as_posix()],
        cwd=ROOT,
        capture_output=True,
        check=False,
    )
    return completed.returncode == 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("signoff", type=Path)
    parser.add_argument("--summary", type=Path, required=True)
    parser.add_argument("--expected-git-sha")
    parser.add_argument("--reject-tracked-signoff", action="store_true")
    args = parser.parse_args(argv)
    if args.reject_tracked_signoff and _is_tracked(args.signoff):
        print("Gate signoff must be a restricted CI artifact, not a tracked source file")
        return 1
    try:
        document = json.loads(args.signoff.read_text(encoding="utf-8"))
        summary_bytes = args.summary.read_bytes()
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        print(f"Gate input is unreadable: {error}")
        return 1
    errors = verify(
        document,
        summary_bytes,
        expected_git_sha=args.expected_git_sha,
    )
    if errors:
        print("\n".join(errors))
        return 1
    print(f"{document['gate']} signoff verified")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
