#!/usr/bin/env python3
"""Attest and verify role-bound external G5 evidence bundles."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

try:
    from .ac import (
        evaluate_external_requirement,
        load_manifest,
        sha256_file,
    )
    from .candidate_identity import (
        CandidateIdentityError,
        candidate_identity_sha256,
        canonical_json,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )
    from .g5_source_policy import (
        ARTIFACT_NAME,
        EXTERNAL_WORKFLOW,
        REQUIRED_EVIDENCE_BY_ROLE,
        attestation_path,
        expected_source_artifact_name,
        key_for_role,
    )
except ImportError:
    from ac import (  # type: ignore[no-redef]
        evaluate_external_requirement,
        load_manifest,
        sha256_file,
    )
    from candidate_identity import (  # type: ignore[no-redef]
        CandidateIdentityError,
        candidate_identity_sha256,
        canonical_json,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )
    from g5_source_policy import (  # type: ignore[no-redef]
        ARTIFACT_NAME,
        EXTERNAL_WORKFLOW,
        REQUIRED_EVIDENCE_BY_ROLE,
        attestation_path,
        expected_source_artifact_name,
        key_for_role,
    )

WORKFLOW_PATH = re.compile(r"^\.github/workflows/[A-Za-z0-9._-]+\.ya?ml$")
VERIFIED_RUN_FIELDS = {
    "schema_version",
    "status",
    "run_id",
    "repository",
    "workflow_path",
    "head_sha",
    "run_attempt",
}
ATTESTATION_FIELDS = {
    "schema_version",
    "kind",
    "status",
    "source_key",
    "role",
    "repository",
    "git_sha",
    "candidate_identity",
    "candidate_identity_sha256",
    "producer_run",
    "source_run",
    "source_artifact_name",
    "files",
}
VERIFICATION_FIELDS = {
    "schema_version",
    "status",
    "source_key",
    "role",
    "producer_run_id",
    "source_run_id",
    "source_workflow_path",
    "source_artifact_name",
    "candidate_identity_sha256",
    "files",
}


class ExternalEvidenceError(RuntimeError):
    pass


def _load_json(path: Path, label: str) -> dict[str, Any]:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ExternalEvidenceError(f"{label} is not valid UTF-8 JSON") from error
    if not isinstance(document, dict):
        raise ExternalEvidenceError(f"{label} must be an object")
    return document


def _validate_verified_run(
    document: object,
    *,
    expected_repository: str,
    expected_git_sha: str,
    expected_workflow_path: str | None = None,
) -> dict[str, Any]:
    if not isinstance(document, dict) or set(document) != VERIFIED_RUN_FIELDS:
        raise ExternalEvidenceError("verified workflow-run metadata fields differ")
    if document.get("schema_version") != 1 or document.get("status") != "passed":
        raise ExternalEvidenceError("verified workflow-run metadata is not passed schema 1")
    if document.get("repository") != expected_repository:
        raise ExternalEvidenceError("verified workflow run belongs to another repository")
    if document.get("head_sha") != expected_git_sha:
        raise ExternalEvidenceError("verified workflow run belongs to another commit")
    workflow_path = document.get("workflow_path")
    if not isinstance(workflow_path, str) or not WORKFLOW_PATH.fullmatch(workflow_path):
        raise ExternalEvidenceError("verified workflow path is malformed")
    if expected_workflow_path is not None and workflow_path != expected_workflow_path:
        raise ExternalEvidenceError("verified workflow path differs from the trusted producer")
    for field in ("run_id", "run_attempt"):
        value = document.get(field)
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            raise ExternalEvidenceError(f"verified workflow {field} is malformed")
    return dict(document)


def _candidate(path: Path, expected_git_sha: str) -> dict[str, Any]:
    try:
        identity = load_candidate_identity(path)
    except CandidateIdentityError as error:
        raise ExternalEvidenceError(str(error)) from error
    errors = validate_candidate_identity(
        identity,
        expected_git_sha=expected_git_sha,
        require_release=True,
    )
    if errors:
        raise ExternalEvidenceError("; ".join(errors))
    return identity


def _bundle_files(root: Path) -> list[str]:
    if not root.is_dir():
        raise ExternalEvidenceError("external evidence root is not a directory")
    result: list[str] = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ExternalEvidenceError("external evidence bundle contains a symlink")
        if path.is_dir():
            continue
        if not path.is_file():
            raise ExternalEvidenceError("external evidence bundle contains a non-regular file")
        result.append(path.relative_to(root).as_posix())
    return result


def _requirements(role: str) -> list[tuple[str, dict[str, Any]]]:
    expected = set(REQUIRED_EVIDENCE_BY_ROLE[role])
    result: dict[str, tuple[str, dict[str, Any]]] = {}
    for acceptance_id, contract in load_manifest()["acceptance"].items():
        for requirement in contract.get("external_requirements", []):
            path = requirement.get("evidence")
            if path not in expected:
                continue
            if path in result:
                raise ExternalEvidenceError(f"external evidence path is declared twice: {path}")
            result[path] = (acceptance_id, requirement)
    if set(result) != expected:
        missing = sorted(expected - set(result))
        raise ExternalEvidenceError(f"role policy references undeclared evidence: {missing}")
    return [result[path] for path in REQUIRED_EVIDENCE_BY_ROLE[role]]


def _verify_evidence_files(
    role: str,
    root: Path,
    git_sha: str,
    candidate_identity: dict[str, Any],
    *,
    attested: bool = False,
) -> list[dict[str, str]]:
    required = list(REQUIRED_EVIDENCE_BY_ROLE[role])
    allowed = [*required, attestation_path(role)] if attested else required
    if _bundle_files(root) != sorted(allowed):
        raise ExternalEvidenceError(
            f"{role} bundle must contain only its fixed evidence files: {required}"
        )
    for acceptance_id, requirement in _requirements(role):
        try:
            result = evaluate_external_requirement(
                requirement,
                root,
                git_sha,
                candidate_identity,
            )
        except (TypeError, ValueError) as error:
            raise ExternalEvidenceError(
                f"{requirement['evidence']} could not be evaluated: {error}"
            ) from error
        if result.get("status") != "passed":
            errors = result.get("errors") or ["evidence did not pass"]
            raise ExternalEvidenceError(
                f"{acceptance_id}.{requirement['id']}: {'; '.join(map(str, errors))}"
            )
    return [
        {"path": path, "sha256": sha256_file(root / path)}
        for path in required
    ]


def attest(
    *,
    role: str,
    evidence_root: Path,
    candidate_identity_path: Path,
    expected_git_sha: str,
    repository: str,
    producer_run_id: int,
    producer_run_attempt: int,
    source_run_metadata: Path,
    source_artifact_name: str,
) -> dict[str, Any]:
    if role not in REQUIRED_EVIDENCE_BY_ROLE:
        raise ExternalEvidenceError("external evidence role is not supported")
    for label, value in (
        ("producer run ID", producer_run_id),
        ("producer run attempt", producer_run_attempt),
    ):
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            raise ExternalEvidenceError(f"{label} is malformed")
    identity = _candidate(candidate_identity_path, expected_git_sha)
    source_run = _validate_verified_run(
        _load_json(source_run_metadata, "source run metadata"),
        expected_repository=repository,
        expected_git_sha=expected_git_sha,
    )
    if source_run["run_id"] == producer_run_id:
        raise ExternalEvidenceError("source and intake producer run IDs must differ")
    expected_source_name = expected_source_artifact_name(role, source_run["run_id"])
    if source_artifact_name != expected_source_name:
        raise ExternalEvidenceError("source artifact name differs from the role policy")
    files = _verify_evidence_files(role, evidence_root, expected_git_sha, identity)
    producer_run = {
        "schema_version": 1,
        "status": "passed",
        "run_id": producer_run_id,
        "repository": repository,
        "workflow_path": EXTERNAL_WORKFLOW,
        "head_sha": expected_git_sha,
        "run_attempt": producer_run_attempt,
    }
    document = {
        "schema_version": 1,
        "kind": "g5_external_evidence_attestation",
        "status": "passed",
        "source_key": key_for_role(role),
        "role": role,
        "repository": repository,
        "git_sha": expected_git_sha,
        "candidate_identity": identity,
        "candidate_identity_sha256": candidate_identity_sha256(identity),
        "producer_run": producer_run,
        "source_run": source_run,
        "source_artifact_name": source_artifact_name,
        "files": files,
    }
    destination = evidence_root / attestation_path(role)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(canonical_json(document))
    return document


def verify(
    *,
    role: str,
    evidence_root: Path,
    candidate_identity_path: Path,
    expected_git_sha: str,
    repository: str,
    producer_run_metadata: Path,
) -> dict[str, Any]:
    if role not in REQUIRED_EVIDENCE_BY_ROLE:
        raise ExternalEvidenceError("external evidence role is not supported")
    identity = _candidate(candidate_identity_path, expected_git_sha)
    producer_run = _validate_verified_run(
        _load_json(producer_run_metadata, "producer run metadata"),
        expected_repository=repository,
        expected_git_sha=expected_git_sha,
        expected_workflow_path=EXTERNAL_WORKFLOW,
    )
    attestation_relative = attestation_path(role)
    expected_paths = sorted((*REQUIRED_EVIDENCE_BY_ROLE[role], attestation_relative))
    if _bundle_files(evidence_root) != expected_paths:
        raise ExternalEvidenceError(
            f"{role} attested bundle differs from its fixed file set"
        )
    document = _load_json(
        evidence_root / attestation_relative,
        f"{role} attestation",
    )
    if set(document) != ATTESTATION_FIELDS:
        raise ExternalEvidenceError("external evidence attestation fields differ")
    expected_scalars = {
        "schema_version": 1,
        "kind": "g5_external_evidence_attestation",
        "status": "passed",
        "source_key": key_for_role(role),
        "role": role,
        "repository": repository,
        "git_sha": expected_git_sha,
    }
    for field, expected in expected_scalars.items():
        if document.get(field) != expected:
            raise ExternalEvidenceError(f"external evidence attestation {field} differs")
    identity_errors = compare_candidate_identities(
        document.get("candidate_identity"), identity
    )
    if identity_errors:
        raise ExternalEvidenceError("; ".join(identity_errors))
    if document.get("candidate_identity_sha256") != candidate_identity_sha256(identity):
        raise ExternalEvidenceError("external evidence candidate identity hash differs")
    if document.get("producer_run") != producer_run:
        raise ExternalEvidenceError("external evidence producer run differs")
    _validate_verified_run(
        document.get("source_run"),
        expected_repository=repository,
        expected_git_sha=expected_git_sha,
    )
    source_artifact_name = document.get("source_artifact_name")
    if not isinstance(source_artifact_name, str) or not ARTIFACT_NAME.fullmatch(
        source_artifact_name
    ):
        raise ExternalEvidenceError("external evidence source artifact name is malformed")
    expected_files = _verify_evidence_files(
        role,
        evidence_root,
        expected_git_sha,
        identity,
        attested=True,
    )
    if document.get("files") != expected_files:
        raise ExternalEvidenceError("external evidence file hashes differ")
    return {
        "schema_version": 1,
        "status": "passed",
        "source_key": key_for_role(role),
        "role": role,
        "producer_run_id": producer_run["run_id"],
        "source_run_id": document["source_run"]["run_id"],
        "source_workflow_path": document["source_run"]["workflow_path"],
        "source_artifact_name": document["source_artifact_name"],
        "candidate_identity_sha256": candidate_identity_sha256(identity),
        "files": expected_files,
    }


def verify_set(documents: list[dict[str, Any]]) -> dict[str, Any]:
    expected_roles = set(REQUIRED_EVIDENCE_BY_ROLE)
    for document in documents:
        if set(document) != VERIFICATION_FIELDS:
            raise ExternalEvidenceError("external verification fields differ")
        role = document.get("role")
        if role not in expected_roles:
            raise ExternalEvidenceError("external verification role is unsupported")
        if document.get("schema_version") != 1 or document.get("status") != "passed":
            raise ExternalEvidenceError("external verification is not passed schema 1")
        if document.get("source_key") != key_for_role(role):
            raise ExternalEvidenceError("external verification source key differs from its role")
        if document.get("source_workflow_path") == EXTERNAL_WORKFLOW:
            raise ExternalEvidenceError("external source workflow cannot be the intake workflow")
        expected_name = expected_source_artifact_name(role, document.get("source_run_id"))
        if document.get("source_artifact_name") != expected_name:
            raise ExternalEvidenceError("external verification artifact name differs")
    roles = [document.get("role") for document in documents]
    if len(documents) != len(expected_roles) or set(roles) != expected_roles:
        raise ExternalEvidenceError("external verification set must contain every role exactly once")
    source_run_ids = [document.get("source_run_id") for document in documents]
    if any(
        not isinstance(value, int) or isinstance(value, bool) or value <= 0
        for value in source_run_ids
    ):
        raise ExternalEvidenceError("external verification set has a malformed source run ID")
    if len(source_run_ids) != len(set(source_run_ids)):
        raise ExternalEvidenceError("external evidence source workflow run IDs must be distinct")
    identity_hashes = {document.get("candidate_identity_sha256") for document in documents}
    if len(identity_hashes) != 1:
        raise ExternalEvidenceError("external evidence candidate identities differ")
    return {
        "schema_version": 1,
        "status": "passed",
        "roles": sorted(expected_roles),
        "source_run_ids": sorted(source_run_ids),
        "candidate_identity_sha256": identity_hashes.pop(),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("attest", "verify"):
        subparser = commands.add_parser(command)
        subparser.add_argument("--role", choices=sorted(REQUIRED_EVIDENCE_BY_ROLE), required=True)
        subparser.add_argument("--evidence-root", type=Path, required=True)
        subparser.add_argument("--candidate-identity", type=Path, required=True)
        subparser.add_argument("--expected-git-sha", required=True)
        subparser.add_argument("--repository", required=True)
        subparser.add_argument("--out", type=Path)
    attest_parser = commands.choices["attest"]
    attest_parser.add_argument("--producer-run-id", type=int, required=True)
    attest_parser.add_argument("--producer-run-attempt", type=int, required=True)
    attest_parser.add_argument("--source-run-metadata", type=Path, required=True)
    attest_parser.add_argument("--source-artifact-name", required=True)
    verify_parser = commands.choices["verify"]
    verify_parser.add_argument("--producer-run-metadata", type=Path, required=True)
    set_parser = commands.add_parser("verify-set")
    set_parser.add_argument("--verification", type=Path, action="append", required=True)
    set_parser.add_argument("--out", type=Path)
    args = parser.parse_args(argv)
    if args.command == "verify-set":
        try:
            result = verify_set(
                [_load_json(path, "external verification") for path in args.verification]
            )
        except ExternalEvidenceError as error:
            print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
            return 2
        if args.out is not None:
            args.out.parent.mkdir(parents=True, exist_ok=True)
            args.out.write_bytes(canonical_json(result))
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    common = {
        "role": args.role,
        "evidence_root": args.evidence_root,
        "candidate_identity_path": args.candidate_identity,
        "expected_git_sha": args.expected_git_sha,
        "repository": args.repository,
    }
    try:
        if args.command == "attest":
            result = attest(
                **common,
                producer_run_id=args.producer_run_id,
                producer_run_attempt=args.producer_run_attempt,
                source_run_metadata=args.source_run_metadata,
                source_artifact_name=args.source_artifact_name,
            )
        else:
            result = verify(
                **common,
                producer_run_metadata=args.producer_run_metadata,
            )
    except ExternalEvidenceError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    if args.out is not None:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
