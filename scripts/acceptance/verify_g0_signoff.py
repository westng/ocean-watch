#!/usr/bin/env python3
"""Verify that G0 has complete, independent, commit-bound approvals."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
from pathlib import Path

REQUIRED_ROLES = {"MT", "AO", "QO", "SO", "SCO"}
ZERO_COMMIT = "0" * 40
ZERO_DIGEST = "0" * 64


def _valid_approval_time(value: object) -> bool:
    if not isinstance(value, str) or not value:
        return False
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return False
    return parsed.tzinfo is not None


def verify(document: dict, summary_bytes: bytes | None = None) -> list[str]:
    errors: list[str] = []
    if document.get("schema_version") != 1 or document.get("gate") != "G0":
        errors.append("invalid G0 signoff identity")
    if not re.fullmatch(r"[a-f0-9]{40}", str(document.get("git_commit", ""))):
        errors.append("git_commit must be a full lowercase SHA-1")
    elif document["git_commit"] == ZERO_COMMIT:
        errors.append("git_commit cannot use the all-zero placeholder")
    if not re.fullmatch(r"[a-f0-9]{64}", str(document.get("evidence_sha256", ""))):
        errors.append("evidence_sha256 must be a lowercase SHA-256")
    elif document["evidence_sha256"] == ZERO_DIGEST:
        errors.append("evidence_sha256 cannot use the all-zero placeholder")
    approvals = document.get("approvals")
    if not isinstance(approvals, list):
        return errors + ["approvals must be a list"]
    by_role = {}
    for approval in approvals:
        role = approval.get("role") if isinstance(approval, dict) else None
        if role not in REQUIRED_ROLES:
            errors.append(f"unknown approval role: {role}")
            continue
        if role in by_role:
            errors.append(f"duplicate approval role: {role}")
        by_role[role] = approval
    missing = REQUIRED_ROLES - set(by_role)
    if missing:
        errors.append(f"missing approval roles: {sorted(missing)}")
    approvers = set()
    for role in REQUIRED_ROLES & set(by_role):
        approval = by_role[role]
        if approval.get("decision") != "approved":
            errors.append(f"{role} is not approved")
        if not str(approval.get("approver", "")).strip():
            errors.append(f"{role} approver is missing")
        else:
            approvers.add(approval["approver"].strip())
        if not _valid_approval_time(approval.get("approved_at")):
            errors.append(f"{role} approved_at must be an RFC3339 timestamp with timezone")
    if len(approvers) < 2:
        errors.append("G0 requires at least two distinct approvers")
    if summary_bytes is None:
        errors.append("G0 evidence summary is required")
        return errors
    actual_digest = hashlib.sha256(summary_bytes).hexdigest()
    if document.get("evidence_sha256") != actual_digest:
        errors.append("evidence_sha256 does not match the supplied summary")
    try:
        summary = json.loads(summary_bytes)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return errors + ["G0 evidence summary is not valid UTF-8 JSON"]
    if not isinstance(summary, dict) or summary.get("schema_version") != 1:
        errors.append("G0 evidence summary must use schema_version 1")
        return errors
    if summary.get("gate") != "G0":
        errors.append("G0 evidence summary has the wrong gate")
    if summary.get("git_commit") != document.get("git_commit"):
        errors.append("signoff git_commit does not match the evidence summary")
    if summary.get("ready") is not True or summary.get("status") != "ready":
        errors.append("G0 evidence summary is not ready")
    if summary.get("blockers"):
        errors.append("G0 evidence summary still contains blockers")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("signoff", type=Path)
    parser.add_argument("--summary", type=Path, required=True)
    args = parser.parse_args()
    errors = verify(
        json.loads(args.signoff.read_text(encoding="utf-8")),
        args.summary.read_bytes(),
    )
    if errors:
        print("\n".join(errors))
        return 1
    print("G0 signoff verified")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
