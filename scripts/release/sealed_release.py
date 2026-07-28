#!/usr/bin/env python3
"""Seal and reverify a self-contained G5 release artifact."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import sys
from pathlib import Path
from typing import Any

try:
    from scripts.acceptance.candidate_identity import (
        candidate_identity_sha256,
        canonical_json,
        compare_candidate_identities,
    )
    from scripts.acceptance.p5 import AcceptanceError, verify_candidate
    from scripts.acceptance.source_runs import digest as source_runs_sha256
    from scripts.acceptance.source_runs import validate as validate_source_runs
    from scripts.acceptance.verify_gate_signoff import verify as verify_gate_signoff
except ModuleNotFoundError:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.acceptance.candidate_identity import (  # type: ignore[no-redef]
        candidate_identity_sha256,
        canonical_json,
        compare_candidate_identities,
    )
    from scripts.acceptance.p5 import (  # type: ignore[no-redef]
        AcceptanceError,
        verify_candidate,
    )
    from scripts.acceptance.source_runs import (  # type: ignore[no-redef]
        digest as source_runs_sha256,
    )
    from scripts.acceptance.source_runs import (
        validate as validate_source_runs,
    )
    from scripts.acceptance.verify_gate_signoff import (  # type: ignore[no-redef]
        verify as verify_gate_signoff,
    )

HEX_40 = re.compile(r"^[a-f0-9]{40}$")
REPOSITORY = "westng/ocean-watch"
PREPARE_WORKFLOW = ".github/workflows/g5-seal.yml"
SIGNOFF_WORKFLOW = ".github/workflows/g5-signoff.yml"
WORKFLOW_REPORT_FIELDS = {
    "schema_version",
    "status",
    "run_id",
    "repository",
    "workflow_path",
    "head_sha",
    "run_attempt",
}
SEAL_FIELDS = {
    "schema_version",
    "status",
    "repository",
    "git_sha",
    "tag",
    "product_version",
    "plugin_version",
    "sdk_version",
    "candidate_identity",
    "candidate_identity_sha256",
    "source_runs_sha256",
    "summary_sha256",
    "signoff_sha256",
    "evidence_tree_sha256",
    "evidence_file_count",
    "prepared_run",
    "signoff_run",
}


class SealedReleaseError(RuntimeError):
    pass


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise SealedReleaseError(f"invalid JSON input: {path}") from error
    if not isinstance(value, dict):
        raise SealedReleaseError(f"JSON input must be an object: {path}")
    return value


def _regular_files(root: Path) -> list[Path]:
    if root.is_symlink() or not root.is_dir():
        raise SealedReleaseError(f"artifact tree is not a regular directory: {root}")
    files: list[Path] = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise SealedReleaseError(f"artifact tree contains a symlink: {path}")
        if path.is_dir():
            continue
        if not path.is_file():
            raise SealedReleaseError(f"artifact tree contains a special file: {path}")
        files.append(path)
    if not files:
        raise SealedReleaseError(f"artifact tree contains no files: {root}")
    return files


def _tree_identity(root: Path) -> tuple[str, int]:
    digest = hashlib.sha256()
    files = _regular_files(root)
    for path in files:
        relative = path.relative_to(root).as_posix().encode("utf-8")
        payload = path.read_bytes()
        digest.update(len(relative).to_bytes(4, "big"))
        digest.update(relative)
        digest.update(len(payload).to_bytes(8, "big"))
        digest.update(payload)
    return digest.hexdigest(), len(files)


def _safe_relative_path(value: object) -> Path | None:
    if not isinstance(value, str) or not value:
        return None
    path = Path(value)
    if path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        return None
    return path


def _verify_summary_evidence(summary: dict[str, Any], evidence_root: Path) -> None:
    rows = summary.get("evidence")
    if not isinstance(rows, list) or not rows:
        raise SealedReleaseError("G5 summary has no evidence index")
    seen: set[str] = set()
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"path", "sha256", "size"}:
            raise SealedReleaseError("G5 summary evidence row is malformed")
        relative = _safe_relative_path(row.get("path"))
        if relative is None or relative.as_posix() in seen:
            raise SealedReleaseError("G5 summary evidence path is unsafe or duplicated")
        seen.add(relative.as_posix())
        path = evidence_root / relative
        if path.is_symlink() or not path.is_file():
            raise SealedReleaseError(f"indexed G5 evidence is missing: {relative}")
        if path.stat().st_size != row.get("size") or _sha256_file(path) != row.get("sha256"):
            raise SealedReleaseError(f"indexed G5 evidence differs: {relative}")


def _workflow_report(
    path: Path,
    *,
    expected_workflow: str,
    expected_commit: str,
    expected_repository: str,
) -> dict[str, Any]:
    document = _load_json(path)
    if set(document) != WORKFLOW_REPORT_FIELDS:
        raise SealedReleaseError("workflow source report fields differ")
    if (
        document.get("schema_version") != 1
        or document.get("status") != "passed"
        or document.get("repository") != expected_repository
        or document.get("workflow_path") != expected_workflow
        or document.get("head_sha") != expected_commit
    ):
        raise SealedReleaseError("workflow source report identity differs")
    for field in ("run_id", "run_attempt"):
        value = document.get(field)
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            raise SealedReleaseError(f"workflow source report {field} is malformed")
    return document


def _validate_release_inputs(
    *,
    candidate_dir: Path,
    evidence_root: Path,
    summary_path: Path,
    signoff_path: Path,
    prepared_run_path: Path,
    signoff_run_path: Path,
    expected_commit: str,
    expected_repository: str,
) -> dict[str, Any]:
    if not HEX_40.fullmatch(expected_commit) or expected_commit == "0" * 40:
        raise SealedReleaseError("expected release commit is malformed")
    if expected_repository != REPOSITORY:
        raise SealedReleaseError("sealed release belongs to another repository")
    try:
        candidate = verify_candidate(
            candidate_dir,
            verify_signatures=True,
            require_release=True,
            expected_commit=expected_commit,
        )
    except (AcceptanceError, OSError, ValueError) as error:
        raise SealedReleaseError(f"release candidate verification failed: {error}") from error
    summary_bytes = summary_path.read_bytes()
    summary = _load_json(summary_path)
    if summary_bytes != canonical_json(summary):
        raise SealedReleaseError("G5 summary is not canonical JSON")
    signoff = _load_json(signoff_path)
    signoff_errors = verify_gate_signoff(
        signoff,
        summary_bytes,
        expected_git_sha=expected_commit,
    )
    if signoff_errors:
        raise SealedReleaseError("G5 signoff is invalid: " + "; ".join(signoff_errors))
    identity_errors = compare_candidate_identities(
        summary.get("candidate_identity"),
        candidate.get("candidate_identity"),
    )
    if identity_errors:
        raise SealedReleaseError("G5 summary candidate differs: " + "; ".join(identity_errors))
    candidate_digest = candidate_identity_sha256(candidate["candidate_identity"])
    if summary.get("candidate_identity_sha256") != candidate_digest:
        raise SealedReleaseError("G5 summary candidate identity digest differs")
    source_runs = summary.get("source_runs")
    source_errors = validate_source_runs(
        source_runs,
        expected_git_sha=expected_commit,
        expected_repository=expected_repository,
    )
    if source_errors:
        raise SealedReleaseError("G5 source runs are invalid: " + "; ".join(source_errors))
    source_digest = source_runs_sha256(source_runs)
    if summary.get("source_runs_sha256") != source_digest:
        raise SealedReleaseError("G5 source run digest differs")
    _verify_summary_evidence(summary, evidence_root)
    prepared_run = _workflow_report(
        prepared_run_path,
        expected_workflow=PREPARE_WORKFLOW,
        expected_commit=expected_commit,
        expected_repository=expected_repository,
    )
    signoff_run = _workflow_report(
        signoff_run_path,
        expected_workflow=SIGNOFF_WORKFLOW,
        expected_commit=expected_commit,
        expected_repository=expected_repository,
    )
    evidence_digest, evidence_count = _tree_identity(evidence_root)
    return {
        "candidate": candidate,
        "summary": summary,
        "summary_bytes": summary_bytes,
        "signoff": signoff,
        "source_runs": source_runs,
        "source_runs_sha256": source_digest,
        "prepared_run": prepared_run,
        "signoff_run": signoff_run,
        "evidence_tree_sha256": evidence_digest,
        "evidence_file_count": evidence_count,
    }


def _prepare_output(path: Path) -> Path:
    resolved = path.resolve()
    if len(resolved.parts) < 4 or resolved in {Path.home().resolve(), Path.cwd().resolve()}:
        raise SealedReleaseError("refusing unsafe sealed release output path")
    if resolved.exists():
        shutil.rmtree(resolved)
    resolved.mkdir(parents=True, mode=0o700)
    return resolved


def _copy_tree(source: Path, destination: Path) -> None:
    _regular_files(source)
    shutil.copytree(source, destination)
    for path in destination.rglob("*"):
        if path.is_file():
            path.chmod(0o600)
        elif path.is_dir():
            path.chmod(0o700)


def seal(
    *,
    candidate_dir: Path,
    evidence_root: Path,
    summary_path: Path,
    signoff_path: Path,
    prepared_run_path: Path,
    signoff_run_path: Path,
    out_dir: Path,
    expected_commit: str,
    expected_repository: str = REPOSITORY,
) -> dict[str, Any]:
    validated = _validate_release_inputs(
        candidate_dir=candidate_dir,
        evidence_root=evidence_root,
        summary_path=summary_path,
        signoff_path=signoff_path,
        prepared_run_path=prepared_run_path,
        signoff_run_path=signoff_run_path,
        expected_commit=expected_commit,
        expected_repository=expected_repository,
    )
    output = _prepare_output(out_dir)
    _copy_tree(candidate_dir, output / "candidate")
    _copy_tree(evidence_root, output / "evidence" / "files")
    (output / "evidence").mkdir(parents=True, exist_ok=True, mode=0o700)
    (output / "approvals").mkdir(parents=True, mode=0o700)
    (output / "provenance").mkdir(parents=True, mode=0o700)
    (output / "evidence" / "summary.json").write_bytes(validated["summary_bytes"])
    (output / "evidence" / "source-runs.json").write_bytes(
        canonical_json(validated["source_runs"])
    )
    shutil.copyfile(signoff_path, output / "approvals" / "signoff.json")
    (output / "provenance" / "prepared-run.json").write_bytes(
        canonical_json(validated["prepared_run"])
    )
    (output / "provenance" / "signoff-run.json").write_bytes(
        canonical_json(validated["signoff_run"])
    )
    candidate = validated["candidate"]
    manifest = {
        "schema_version": 1,
        "status": "sealed",
        "repository": expected_repository,
        "git_sha": expected_commit,
        "tag": f"v{candidate['product_version']}",
        "product_version": candidate["product_version"],
        "plugin_version": candidate["plugin_version"],
        "sdk_version": candidate["candidate_identity"]["sdk_version"],
        "candidate_identity": candidate["candidate_identity"],
        "candidate_identity_sha256": candidate["candidate_identity_sha256"],
        "source_runs_sha256": validated["source_runs_sha256"],
        "summary_sha256": hashlib.sha256(validated["summary_bytes"]).hexdigest(),
        "signoff_sha256": _sha256_file(signoff_path),
        "evidence_tree_sha256": validated["evidence_tree_sha256"],
        "evidence_file_count": validated["evidence_file_count"],
        "prepared_run": validated["prepared_run"],
        "signoff_run": validated["signoff_run"],
    }
    (output / "seal.json").write_bytes(canonical_json(manifest))
    return manifest


def verify(
    root: Path,
    *,
    expected_commit: str,
    expected_repository: str = REPOSITORY,
    expected_tag: str | None = None,
) -> dict[str, Any]:
    if root.is_symlink() or not root.is_dir():
        raise SealedReleaseError("sealed release root is not a regular directory")
    top_level = {path.name for path in root.iterdir()}
    if top_level != {"candidate", "evidence", "approvals", "provenance", "seal.json"}:
        raise SealedReleaseError("sealed release top-level entries differ")
    expected_files = {
        root / "evidence" / "summary.json",
        root / "evidence" / "source-runs.json",
        root / "approvals" / "signoff.json",
        root / "provenance" / "prepared-run.json",
        root / "provenance" / "signoff-run.json",
        root / "seal.json",
    }
    if any(path.is_symlink() or not path.is_file() for path in expected_files):
        raise SealedReleaseError("sealed release metadata file is missing")
    if {path.name for path in (root / "approvals").iterdir()} != {"signoff.json"}:
        raise SealedReleaseError("sealed release approval files differ")
    if {path.name for path in (root / "provenance").iterdir()} != {
        "prepared-run.json",
        "signoff-run.json",
    }:
        raise SealedReleaseError("sealed release provenance files differ")
    if {path.name for path in (root / "evidence").iterdir()} != {
        "files",
        "source-runs.json",
        "summary.json",
    }:
        raise SealedReleaseError("sealed release evidence entries differ")
    validated = _validate_release_inputs(
        candidate_dir=root / "candidate",
        evidence_root=root / "evidence" / "files",
        summary_path=root / "evidence" / "summary.json",
        signoff_path=root / "approvals" / "signoff.json",
        prepared_run_path=root / "provenance" / "prepared-run.json",
        signoff_run_path=root / "provenance" / "signoff-run.json",
        expected_commit=expected_commit,
        expected_repository=expected_repository,
    )
    manifest_path = root / "seal.json"
    manifest_bytes = manifest_path.read_bytes()
    manifest = _load_json(manifest_path)
    if manifest_bytes != canonical_json(manifest) or set(manifest) != SEAL_FIELDS:
        raise SealedReleaseError("sealed release manifest is not canonical or fields differ")
    candidate = validated["candidate"]
    expected = {
        "schema_version": 1,
        "status": "sealed",
        "repository": expected_repository,
        "git_sha": expected_commit,
        "tag": f"v{candidate['product_version']}",
        "product_version": candidate["product_version"],
        "plugin_version": candidate["plugin_version"],
        "sdk_version": candidate["candidate_identity"]["sdk_version"],
        "candidate_identity": candidate["candidate_identity"],
        "candidate_identity_sha256": candidate["candidate_identity_sha256"],
        "source_runs_sha256": validated["source_runs_sha256"],
        "summary_sha256": hashlib.sha256(validated["summary_bytes"]).hexdigest(),
        "signoff_sha256": _sha256_file(root / "approvals" / "signoff.json"),
        "evidence_tree_sha256": validated["evidence_tree_sha256"],
        "evidence_file_count": validated["evidence_file_count"],
        "prepared_run": validated["prepared_run"],
        "signoff_run": validated["signoff_run"],
    }
    if manifest != expected:
        raise SealedReleaseError("sealed release manifest identity differs")
    source_runs_path = root / "evidence" / "source-runs.json"
    if source_runs_path.read_bytes() != canonical_json(validated["source_runs"]):
        raise SealedReleaseError("sealed source run manifest differs from the G5 summary")
    if expected_tag is not None and manifest["tag"] != expected_tag:
        raise SealedReleaseError("sealed release Tag differs")
    return manifest


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    seal_parser = subparsers.add_parser("seal")
    seal_parser.add_argument("--candidate-dir", type=Path, required=True)
    seal_parser.add_argument("--evidence-root", type=Path, required=True)
    seal_parser.add_argument("--summary", type=Path, required=True)
    seal_parser.add_argument("--signoff", type=Path, required=True)
    seal_parser.add_argument("--prepared-run", type=Path, required=True)
    seal_parser.add_argument("--signoff-run", type=Path, required=True)
    seal_parser.add_argument("--out-dir", type=Path, required=True)
    seal_parser.add_argument("--expected-commit", required=True)
    seal_parser.add_argument("--expected-repository", default=REPOSITORY)
    verify_parser = subparsers.add_parser("verify")
    verify_parser.add_argument("--sealed-dir", type=Path, required=True)
    verify_parser.add_argument("--expected-commit", required=True)
    verify_parser.add_argument("--expected-repository", default=REPOSITORY)
    verify_parser.add_argument("--expected-tag")
    args = parser.parse_args(argv)
    try:
        if args.command == "seal":
            result = seal(
                candidate_dir=args.candidate_dir,
                evidence_root=args.evidence_root,
                summary_path=args.summary,
                signoff_path=args.signoff,
                prepared_run_path=args.prepared_run,
                signoff_run_path=args.signoff_run,
                out_dir=args.out_dir,
                expected_commit=args.expected_commit,
                expected_repository=args.expected_repository,
            )
        else:
            result = verify(
                args.sealed_dir,
                expected_commit=args.expected_commit,
                expected_repository=args.expected_repository,
                expected_tag=args.expected_tag,
            )
    except (OSError, SealedReleaseError, ValueError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
