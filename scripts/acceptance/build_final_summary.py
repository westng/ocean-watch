#!/usr/bin/env python3
"""Assemble deterministic commit-bound native shards into a final Gate summary."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
import subprocess
from collections import Counter
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import (
        CandidateIdentityError,
        candidate_identity_sha256,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )
except ImportError:
    from candidate_identity import (  # type: ignore[no-redef]
        CandidateIdentityError,
        candidate_identity_sha256,
        compare_candidate_identities,
        load_candidate_identity,
        validate_candidate_identity,
    )

try:
    from .ac import (
        EXPECTED_IDS,
        MANIFEST_PATH,
        canonical_json,
        evaluate_external_requirement,
        evaluate_required_evidence,
        load_manifest,
        sha256_file,
        validate_manifest,
    )
except ImportError:
    from ac import (  # type: ignore[no-redef]
        EXPECTED_IDS,
        MANIFEST_PATH,
        canonical_json,
        evaluate_external_requirement,
        evaluate_required_evidence,
        load_manifest,
        sha256_file,
        validate_manifest,
    )

try:
    from .source_runs import (
        SourceRunError,
    )
    from .source_runs import (
        digest as source_runs_sha256,
    )
    from .source_runs import (
        validate as validate_source_runs,
    )
except ImportError:
    from source_runs import (  # type: ignore[no-redef]
        SourceRunError,
    )
    from source_runs import (
        digest as source_runs_sha256,
    )
    from source_runs import (
        validate as validate_source_runs,
    )

ROOT = Path(__file__).resolve().parents[2]
HEX_40 = re.compile(r"^[a-f0-9]{40}$")
RFC3339_UTC = "%Y-%m-%dT%H:%M:%SZ"


class FinalSummaryError(RuntimeError):
    pass


def _load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise FinalSummaryError(f"invalid JSON evidence {path}: {error}") from error
    if not isinstance(value, dict):
        raise FinalSummaryError(f"JSON evidence must be an object: {path}")
    return value


def _parse_time(value: object) -> dt.datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed.astimezone(dt.timezone.utc) if parsed.tzinfo is not None else None


def _git_sha() -> str:
    completed = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    return completed.stdout.strip()


def _record_evidence(records: dict[str, dict[str, Any]], path: Path, root: Path) -> None:
    resolved = path.resolve()
    try:
        relative = resolved.relative_to(root.resolve()).as_posix()
    except ValueError:
        relative = resolved.as_posix()
    records[relative] = {
        "path": relative,
        "sha256": sha256_file(path),
        "size": path.stat().st_size,
    }


def _exceptions(
    path: Path | None,
    commit: str,
    evaluated_at: dt.datetime,
    records: dict[str, dict[str, Any]],
    evidence_root: Path,
) -> tuple[dict[str, list[dict[str, Any]]], list[str]]:
    result: dict[str, list[dict[str, Any]]] = {"active": [], "expired": [], "closed": []}
    errors: list[str] = []
    if path is None:
        return result, errors
    document = _load_json(path)
    _record_evidence(records, path, evidence_root)
    if document.get("schema_version") != 1 or document.get("git_sha") != commit:
        errors.append("exception register is not schema 1 and bound to the target git SHA")
        return result, errors
    rows = document.get("exceptions")
    if not isinstance(rows, list):
        errors.append("exception register exceptions must be a list")
        return result, errors
    seen: set[str] = set()
    for index, row in enumerate(rows):
        prefix = f"exception[{index}]"
        if not isinstance(row, dict):
            errors.append(f"{prefix} must be an object")
            continue
        exception_id = row.get("id")
        if not isinstance(exception_id, str) or not re.fullmatch(r"EX-[A-Z0-9-]+", exception_id):
            errors.append(f"{prefix}.id is invalid")
            continue
        if exception_id in seen:
            errors.append(f"duplicate exception: {exception_id}")
            continue
        seen.add(exception_id)
        if row.get("acceptance_id") not in EXPECTED_IDS:
            errors.append(f"{exception_id} has an unknown acceptance_id")
        if any(not str(row.get(key, "")).strip() for key in ("owner", "impact", "rollback_condition")):
            errors.append(f"{exception_id} is missing owner, impact, or rollback_condition")
        if row.get("blocking") is not False:
            errors.append(f"{exception_id} cannot waive a blocking difference")
        expires_at = _parse_time(row.get("expires_at"))
        if expires_at is None:
            errors.append(f"{exception_id} expires_at must be RFC3339 with timezone")
        status = row.get("status")
        if status not in {"open", "closed"}:
            errors.append(f"{exception_id} status is invalid")
            continue
        normalized = dict(row)
        if status == "closed":
            result["closed"].append(normalized)
        elif expires_at is None or expires_at <= evaluated_at:
            result["expired"].append(normalized)
        else:
            result["active"].append(normalized)
    for rows_for_status in result.values():
        rows_for_status.sort(key=lambda row: str(row.get("id")))
    return result, errors


def _load_shard(
    path: Path,
    commit: str,
    platform_id: str,
    records: dict[str, dict[str, Any]],
    evidence_root: Path,
    manifest_sha256: str,
    sdk_version: str,
    candidate_identity: dict[str, Any] | None,
) -> tuple[dict[str, dict[str, Any]] | None, list[str]]:
    errors: list[str] = []
    environment_path = path / "environment.json"
    summary_path = path / "runner-summary.json"
    if not environment_path.is_file() or not summary_path.is_file():
        return None, [f"missing native shard metadata for {platform_id}"]
    environment = _load_json(environment_path)
    runner = _load_json(summary_path)
    _record_evidence(records, environment_path, evidence_root)
    _record_evidence(records, summary_path, evidence_root)
    for name, document in (("environment", environment), ("runner", runner)):
        if document.get("git_sha") != commit:
            errors.append(f"{platform_id} {name} is not bound to the target git SHA")
        if document.get("platform") != platform_id:
            errors.append(f"{platform_id} {name} has the wrong native platform")
        if candidate_identity is not None:
            errors.extend(
                f"{platform_id} {name}: {error}"
                for error in compare_candidate_identities(
                    document.get("candidate_identity"), candidate_identity
                )
            )
    if environment.get("working_tree_dirty") is not False or runner.get("working_tree_dirty") is not False:
        errors.append(f"{platform_id} shard was produced from a dirty working tree")
    if environment.get("manifest_sha256") != manifest_sha256:
        errors.append(f"{platform_id} environment uses a different acceptance manifest")
    if environment.get("sdk_version") != sdk_version:
        errors.append(f"{platform_id} environment uses a different SDK version")
    if runner.get("runner_errors") not in ([], None):
        errors.append(f"{platform_id} runner contains execution or parse errors")
    if runner.get("status") == "failed":
        errors.append(f"{platform_id} runner status is failed")
    results: dict[str, dict[str, Any]] = {}
    result_dir = path / "ac-results"
    for acceptance_id in EXPECTED_IDS:
        result_path = result_dir / f"{acceptance_id.lower()}.json"
        if not result_path.is_file():
            errors.append(f"{platform_id} is missing {acceptance_id}")
            continue
        result = _load_json(result_path)
        _record_evidence(records, result_path, evidence_root)
        if (
            result.get("acceptance_id") != acceptance_id
            or result.get("git_sha") != commit
            or result.get("platform") != platform_id
        ):
            errors.append(f"{platform_id} {acceptance_id} identity is invalid")
            continue
        if candidate_identity is not None:
            identity_errors = compare_candidate_identities(
                result.get("candidate_identity"), candidate_identity
            )
            if identity_errors:
                errors.extend(
                    f"{platform_id} {acceptance_id}: {error}"
                    for error in identity_errors
                )
                continue
        results[acceptance_id] = result
    declared_results = runner.get("results")
    if not isinstance(declared_results, list) or {
        row.get("acceptance_id")
        for row in declared_results
        if isinstance(row, dict)
    } != set(EXPECTED_IDS):
        errors.append(f"{platform_id} runner result index is incomplete")
    return results, errors


def _test_component(
    result: dict[str, Any],
    group: str,
    name: str,
) -> tuple[str, str | None]:
    matches = [
        row
        for row in result.get("tests", [])
        if isinstance(row, dict) and row.get("group") == group and row.get("name") == name
    ]
    if len(matches) != 1:
        return "missing", f"{group}/{name} result is missing or duplicated"
    status = matches[0].get("status")
    if status not in {"passed", "failed", "not_run"}:
        return "failed", f"{group}/{name} has an invalid status"
    return status, None


def _record_component(
    components: list[dict[str, Any]],
    *,
    component_id: str,
    status: str,
    platform_id: str | None = None,
    errors: list[str] | None = None,
) -> None:
    row: dict[str, Any] = {"id": component_id, "status": status}
    if platform_id is not None:
        row["platform"] = platform_id
    if errors:
        row["errors"] = errors
    components.append(row)


def build_summary(
    *,
    shard_root: Path,
    external_root: Path,
    git_sha: str,
    gate: str = "G5",
    evaluated_at: dt.datetime | None = None,
    exceptions_path: Path | None = None,
    manifest_path: Path = MANIFEST_PATH,
    candidate_identity: dict[str, Any] | None = None,
    source_runs: dict[str, Any] | None = None,
) -> dict[str, Any]:
    if not HEX_40.fullmatch(git_sha) or git_sha == "0" * 40:
        raise FinalSummaryError("git SHA must be a non-zero full lowercase SHA-1")
    manifest = load_manifest(manifest_path)
    manifest_errors = validate_manifest(manifest)
    if manifest_errors:
        raise FinalSummaryError("invalid acceptance manifest: " + "; ".join(manifest_errors))
    if gate not in manifest["gates"]:
        raise FinalSummaryError(f"unknown gate: {gate}")
    if gate == "G5" and candidate_identity is None:
        raise FinalSummaryError("G5 requires an immutable release candidate identity")
    if gate == "G5" and source_runs is None:
        raise FinalSummaryError("G5 requires the exact source workflow runs")
    if candidate_identity is not None:
        identity_errors = validate_candidate_identity(
            candidate_identity,
            expected_git_sha=git_sha,
            expected_sdk_version=manifest["sdk_version"],
            require_release=gate == "G5",
        )
        if identity_errors:
            raise FinalSummaryError(
                "invalid release candidate identity: " + "; ".join(identity_errors)
            )
    if source_runs is not None:
        source_run_errors = validate_source_runs(
            source_runs,
            expected_git_sha=git_sha,
        )
        if source_run_errors:
            raise FinalSummaryError(
                "invalid source workflow runs: " + "; ".join(source_run_errors)
            )
    evaluated_at = (evaluated_at or dt.datetime.now(dt.timezone.utc)).astimezone(
        dt.timezone.utc
    )
    required_platforms = list(manifest.get("required_platforms") or [])
    if not required_platforms or len(set(required_platforms)) != len(required_platforms):
        raise FinalSummaryError("manifest required_platforms must be unique and non-empty")
    acceptance_ids = list(manifest["gates"][gate]["acceptance"])
    manifest_digest = sha256_file(manifest_path)
    try:
        shard_root.resolve().relative_to(external_root.resolve())
    except ValueError as error:
        raise FinalSummaryError("shard root must be inside the external evidence root") from error
    evidence: dict[str, dict[str, Any]] = {}
    shard_results: dict[str, dict[str, dict[str, Any]]] = {}
    platform_rows: list[dict[str, Any]] = []
    global_errors: list[str] = []
    for platform_id in required_platforms:
        result, errors = _load_shard(
            shard_root / platform_id,
            git_sha,
            platform_id,
            evidence,
            external_root,
            manifest_digest,
            manifest["sdk_version"],
            candidate_identity,
        )
        if result is not None:
            shard_results[platform_id] = result
        platform_rows.append(
            {
                "platform": platform_id,
                "status": "failed" if errors and result is not None else "missing" if result is None else "passed",
                "errors": errors,
            }
        )
        global_errors.extend(errors)
    exception_rows, exception_errors = _exceptions(
        exceptions_path,
        git_sha,
        evaluated_at,
        evidence,
        external_root,
    )
    global_errors.extend(exception_errors)
    acceptance_rows: list[dict[str, Any]] = []
    component_counts: Counter[str] = Counter()
    for acceptance_id in acceptance_ids:
        contract = manifest["acceptance"][acceptance_id]
        components: list[dict[str, Any]] = []
        for platform_id in required_platforms:
            platform_result = shard_results.get(platform_id, {}).get(acceptance_id)
            if platform_result is None:
                for group, names in contract.get("tests", {}).items():
                    if group == "race" and platform_id.startswith("windows-"):
                        continue
                    for name in names:
                        _record_component(
                            components,
                            component_id=f"test:{group}:{name}",
                            status="missing",
                            platform_id=platform_id,
                            errors=["native shard result is missing"],
                        )
                continue
            for group, names in contract.get("tests", {}).items():
                if group == "race" and platform_id.startswith("windows-"):
                    continue
                for name in names:
                    status, error = _test_component(platform_result, group, name)
                    _record_component(
                        components,
                        component_id=f"test:{group}:{name}",
                        status=status,
                        platform_id=platform_id,
                        errors=[error] if error else None,
                    )
        for requirement in contract.get("required_evidence", []):
            if requirement.get("platform_bound"):
                for platform_id in required_platforms:
                    row = evaluate_required_evidence(
                        requirement,
                        shard_root / platform_id,
                        git_sha,
                        platform_id,
                        acceptance_id,
                        candidate_identity,
                    )
                    _record_component(
                        components,
                        component_id=f"evidence:{requirement['id']}",
                        status=row["status"],
                        platform_id=platform_id,
                        errors=list(row.get("errors") or []),
                    )
                    evidence_path = shard_root / platform_id / requirement["evidence"]
                    if evidence_path.is_file():
                        _record_evidence(evidence, evidence_path, external_root)
            else:
                row = evaluate_required_evidence(
                    requirement,
                    external_root,
                    git_sha,
                    required_platforms[0],
                    acceptance_id,
                    candidate_identity,
                )
                _record_component(
                    components,
                    component_id=f"evidence:{requirement['id']}",
                    status=row["status"],
                    errors=list(row.get("errors") or []),
                )
                path = external_root / requirement["evidence"]
                if path.is_file():
                    _record_evidence(evidence, path, external_root)
        for requirement in contract.get("external_requirements", []):
            row = evaluate_external_requirement(
                requirement,
                external_root,
                git_sha,
                candidate_identity,
            )
            _record_component(
                components,
                component_id=f"external:{requirement['id']}",
                status=row["status"],
                errors=list(row.get("errors") or []),
            )
            path = external_root / requirement["evidence"]
            if path.is_file():
                _record_evidence(evidence, path, external_root)
        statuses = [str(component["status"]) for component in components]
        for status in statuses:
            component_counts[status] += 1
        if "failed" in statuses:
            status = "failed"
        elif any(value in {"blocked", "missing", "not_run"} for value in statuses) or not statuses:
            status = "blocked"
        else:
            status = "passed"
        acceptance_rows.append(
            {
                "acceptance_id": acceptance_id,
                "status": status,
                "blocking": status != "passed",
                "components": components,
            }
        )
    acceptance_counts = Counter(row["status"] for row in acceptance_rows)
    blocking = sum(row["blocking"] for row in acceptance_rows)
    counts = {
        "passed": acceptance_counts["passed"],
        "failed": acceptance_counts["failed"],
        "blocked": acceptance_counts["blocked"],
        "missing": component_counts["missing"] + sum(row["status"] == "missing" for row in platform_rows),
        "not_run": component_counts["not_run"],
        "blocking": blocking,
        "active_exceptions": len(exception_rows["active"]),
        "expired_exceptions": len(exception_rows["expired"]),
    }
    blockers = list(global_errors)
    for row in acceptance_rows:
        if row["blocking"]:
            blockers.append(f"{row['acceptance_id']} is {row['status']}")
    blockers.extend(
        f"expired exception: {row['id']}" for row in exception_rows["expired"]
    )
    blockers = list(dict.fromkeys(blockers))
    ready = (
        counts["failed"] == 0
        and counts["blocking"] == 0
        and counts["missing"] == 0
        and counts["not_run"] == 0
        and counts["expired_exceptions"] == 0
        and not exception_errors
        and not global_errors
    )
    platform_failed = any(row["status"] == "failed" for row in platform_rows)
    status = (
        "passed"
        if ready
        else "failed"
        if counts["failed"] or exception_errors or platform_failed
        else "blocked"
    )
    summary = {
        "schema_version": 1,
        "gate": gate,
        "git_sha": git_sha,
        "sdk_version": manifest["sdk_version"],
        "evaluated_at": evaluated_at.strftime(RFC3339_UTC),
        "manifest_sha256": manifest_digest,
        "status": status,
        "ready": ready,
        "counts": counts,
        "component_counts": {
            key: component_counts[key]
            for key in ("passed", "failed", "blocked", "missing", "not_run")
        },
        "required_platforms": required_platforms,
        "platforms": platform_rows,
        "acceptance": acceptance_rows,
        "exceptions": exception_rows,
        "blockers": blockers,
        "evidence": [evidence[path] for path in sorted(evidence)],
    }
    if candidate_identity is not None:
        summary["candidate_identity"] = candidate_identity
        summary["candidate_identity_sha256"] = candidate_identity_sha256(
            candidate_identity
        )
    if source_runs is not None:
        summary["source_runs"] = source_runs
        summary["source_runs_sha256"] = source_runs_sha256(source_runs)
    return summary


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--shard-root", type=Path, required=True)
    parser.add_argument("--external-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--git-sha", default=None)
    parser.add_argument("--gate", default="G5")
    parser.add_argument("--evaluated-at")
    parser.add_argument("--exceptions", type=Path)
    parser.add_argument("--require-ready", action="store_true")
    parser.add_argument("--candidate-identity", type=Path)
    parser.add_argument("--source-runs", type=Path)
    args = parser.parse_args(argv)
    evaluated_at = _parse_time(args.evaluated_at) if args.evaluated_at else None
    if args.evaluated_at and evaluated_at is None:
        parser.error("--evaluated-at must be RFC3339 with timezone")
    try:
        candidate_identity = (
            load_candidate_identity(args.candidate_identity)
            if args.candidate_identity is not None
            else None
        )
        source_runs = (
            _load_json(args.source_runs) if args.source_runs is not None else None
        )
        summary = build_summary(
            shard_root=args.shard_root,
            external_root=args.external_root,
            git_sha=args.git_sha or _git_sha(),
            gate=args.gate,
            evaluated_at=evaluated_at,
            exceptions_path=args.exceptions,
            candidate_identity=candidate_identity,
            source_runs=source_runs,
        )
    except (
        CandidateIdentityError,
        FinalSummaryError,
        OSError,
        SourceRunError,
        ValueError,
    ) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False))
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    payload = canonical_json(summary)
    args.out.write_bytes(payload)
    print(
        json.dumps(
            {
                "path": str(args.out),
                "status": summary["status"],
                "ready": summary["ready"],
                "counts": summary["counts"],
                "evidence_sha256": hashlib.sha256(payload).hexdigest(),
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 1 if args.require_ready and not summary["ready"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
