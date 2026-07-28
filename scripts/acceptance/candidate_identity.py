#!/usr/bin/env python3
"""Validate and compare immutable Ocean Watch release candidate identities."""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path
from typing import Any

SDK_VERSION = "v1.1.92"
HEX_40 = re.compile(r"^[a-f0-9]{40}$")
HEX_64 = re.compile(r"^[a-f0-9]{64}$")
SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
CANDIDATE_IDENTITY_FIELDS = (
    "schema_version",
    "git_sha",
    "product_version",
    "plugin_version",
    "sdk_version",
    "source_tree_sha256",
    "candidate_checksums_sha256",
    "release_public_key_sha256",
    "release",
)


class CandidateIdentityError(ValueError):
    pass


def canonical_json(value: object) -> bytes:
    return (
        json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
        + "\n"
    ).encode("utf-8")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def release_public_key_sha256(public_key_hex: str) -> str:
    if not HEX_64.fullmatch(public_key_hex):
        raise CandidateIdentityError("release public key must be 32-byte lowercase hex")
    return hashlib.sha256(bytes.fromhex(public_key_hex)).hexdigest()


def build_candidate_identity(
    *,
    git_sha: str,
    product_version: str,
    plugin_version: str,
    sdk_version: str,
    source_tree_sha256: str,
    candidate_checksums_sha256: str,
    release_public_key_hex: str,
    release: bool,
) -> dict[str, Any]:
    identity = {
        "schema_version": 1,
        "git_sha": git_sha,
        "product_version": product_version,
        "plugin_version": plugin_version,
        "sdk_version": sdk_version,
        "source_tree_sha256": source_tree_sha256,
        "candidate_checksums_sha256": candidate_checksums_sha256,
        "release_public_key_sha256": release_public_key_sha256(
            release_public_key_hex
        ),
        "release": release,
    }
    errors = validate_candidate_identity(identity)
    if errors:
        raise CandidateIdentityError("; ".join(errors))
    return identity


def validate_candidate_identity(
    identity: object,
    *,
    expected_git_sha: str | None = None,
    expected_sdk_version: str = SDK_VERSION,
    expected_public_key_sha256: str | None = None,
    require_release: bool = False,
) -> list[str]:
    if not isinstance(identity, dict):
        return ["candidate_identity must be an object"]
    errors: list[str] = []
    keys = set(identity)
    expected_keys = set(CANDIDATE_IDENTITY_FIELDS)
    if keys != expected_keys:
        missing = sorted(expected_keys - keys)
        unexpected = sorted(keys - expected_keys)
        if missing:
            errors.append(f"candidate_identity fields are missing: {missing}")
        if unexpected:
            errors.append(f"candidate_identity fields are unexpected: {unexpected}")
    if identity.get("schema_version") != 1:
        errors.append("candidate_identity schema_version must be 1")
    git_sha = identity.get("git_sha")
    if not isinstance(git_sha, str) or not HEX_40.fullmatch(git_sha) or git_sha == "0" * 40:
        errors.append("candidate_identity git_sha must be a non-zero full lowercase SHA-1")
    elif expected_git_sha is not None and git_sha != expected_git_sha:
        errors.append("candidate_identity git_sha does not match the expected release commit")
    product_version = identity.get("product_version")
    if not isinstance(product_version, str) or not SEMVER.fullmatch(product_version):
        errors.append("candidate_identity product_version must be SemVer core")
    plugin_version = identity.get("plugin_version")
    if (
        not isinstance(plugin_version, str)
        or not isinstance(product_version, str)
        or not plugin_version.startswith(product_version + "+codex.")
    ):
        errors.append("candidate_identity plugin_version does not match the product version")
    if identity.get("sdk_version") != expected_sdk_version:
        errors.append("candidate_identity sdk_version does not match the pinned SDK")
    for field in (
        "source_tree_sha256",
        "candidate_checksums_sha256",
        "release_public_key_sha256",
    ):
        value = identity.get(field)
        if not isinstance(value, str) or not HEX_64.fullmatch(value) or value == "0" * 64:
            errors.append(f"candidate_identity {field} must be a non-zero lowercase SHA-256")
    if (
        expected_public_key_sha256 is not None
        and identity.get("release_public_key_sha256") != expected_public_key_sha256
    ):
        errors.append(
            "candidate_identity release_public_key_sha256 does not match the approved trust root"
        )
    if type(identity.get("release")) is not bool:
        errors.append("candidate_identity release must be a boolean")
    elif require_release and identity.get("release") is not True:
        errors.append("candidate_identity is not a formal release candidate")
    return errors


def extract_candidate_identity(document: object) -> dict[str, Any]:
    if not isinstance(document, dict):
        raise CandidateIdentityError("candidate identity document must be an object")
    identity = document.get("candidate_identity", document)
    errors = validate_candidate_identity(identity)
    if errors:
        raise CandidateIdentityError("; ".join(errors))
    return dict(identity)


def load_candidate_identity(path: Path) -> dict[str, Any]:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise CandidateIdentityError(f"candidate identity is unreadable: {path}") from error
    return extract_candidate_identity(document)


def candidate_identity_sha256(identity: dict[str, Any]) -> str:
    errors = validate_candidate_identity(identity)
    if errors:
        raise CandidateIdentityError("; ".join(errors))
    return hashlib.sha256(canonical_json(identity)).hexdigest()


def compare_candidate_identities(
    actual: object,
    expected: object,
) -> list[str]:
    errors = [
        f"actual {error}" for error in validate_candidate_identity(actual)
    ]
    errors.extend(
        f"sealed {error}" for error in validate_candidate_identity(expected)
    )
    if errors:
        return errors
    assert isinstance(actual, dict)
    assert isinstance(expected, dict)
    if actual != expected:
        differing = [
            field
            for field in CANDIDATE_IDENTITY_FIELDS
            if actual.get(field) != expected.get(field)
        ]
        return [f"candidate_identity differs from the sealed candidate: {differing}"]
    return []
