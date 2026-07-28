#!/usr/bin/env python3
"""Canonical G5 workflow, artifact, role, and evidence-source policy."""

from __future__ import annotations

import re

FORMAL_KEY = "formal_evidence"
FORMAL_WORKFLOW = ".github/workflows/g5-evidence.yml"
EXTERNAL_WORKFLOW = ".github/workflows/g5-external-evidence.yml"
ARTIFACT_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")

EXTERNAL_ROLES = {
    "model_evidence": "model",
    "canary_evidence": "canary",
    "marketplace_evidence": "marketplace",
    "rollback_evidence": "rollback",
    "rollout_evidence": "rollout",
}
RUN_KEYS = (FORMAL_KEY, *EXTERNAL_ROLES)

REQUIRED_EVIDENCE_BY_ROLE = {
    "model": (
        "contracts/ac-103-skill-eval.json",
        "contracts/ac-105-skill-eval.json",
        "contracts/ac-128-skill-eval.json",
    ),
    "canary": ("canary/ac-127-summary.json",),
    "marketplace": (
        "release/ac-124-marketplace-install.json",
        "release/ac-128-marketplace-journey.json",
    ),
    "rollback": ("release/ac-126-previous-version-matrix.json",),
    "rollout": ("release/ac-128-rollout-observations.json",),
}


def role_for_key(key: str) -> str | None:
    return EXTERNAL_ROLES.get(key)


def key_for_role(role: str) -> str:
    for key, configured_role in EXTERNAL_ROLES.items():
        if configured_role == role:
            return key
    raise KeyError(role)


def expected_workflow_path(key: str) -> str:
    if key == FORMAL_KEY:
        return FORMAL_WORKFLOW
    if key in EXTERNAL_ROLES:
        return EXTERNAL_WORKFLOW
    raise KeyError(key)


def expected_artifact_name(key: str, run_id: int) -> str:
    if key == FORMAL_KEY:
        return "-"
    role = role_for_key(key)
    if role is None:
        raise KeyError(key)
    return f"g5-{role}-evidence-{run_id}"


def expected_source_artifact_name(role: str, run_id: int) -> str:
    if role not in REQUIRED_EVIDENCE_BY_ROLE:
        raise KeyError(role)
    return f"g5-{role}-source-{run_id}"


def attestation_path(role: str) -> str:
    if role not in REQUIRED_EVIDENCE_BY_ROLE:
        raise KeyError(role)
    return f"provenance/g5-external/{role}.json"
