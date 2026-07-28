#!/usr/bin/env python3
"""Run and assemble commit-bound AC-101 through AC-128 evidence."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
from collections import Counter
from pathlib import Path
from typing import Any

import yaml

try:
    from .candidate_identity import (
        CandidateIdentityError,
        compare_candidate_identities,
        load_candidate_identity,
    )
except ImportError:
    from candidate_identity import (  # type: ignore[no-redef]
        CandidateIdentityError,
        compare_candidate_identities,
        load_candidate_identity,
    )

ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = ROOT / "contracts" / "acceptance" / "ac-manifest.yaml"
GO_MODULE = ROOT / "prototype" / "ocean-watch-go"
BOOTSTRAP_MODULE = ROOT / "prototype" / "runtime-bootstrap"
EXPECTED_IDS = [f"AC-{number}" for number in range(101, 129)]
TEST_GROUPS = {"normal", "race", "bootstrap"}
TERMINAL_ACTIONS = {"pass", "fail", "skip"}
EVIDENCE_KINDS = {"contract_report", "p5_acceptance"}


class AcceptanceError(RuntimeError):
    pass


def canonical_json(value: object) -> bytes:
    return (
        json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"
    ).encode("utf-8")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def load_manifest(path: Path = MANIFEST_PATH) -> dict[str, Any]:
    document = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(document, dict):
        raise AcceptanceError("acceptance manifest must be an object")
    return document


def _declared_go_tests(module: Path) -> set[str]:
    tests: set[str] = set()
    pattern = re.compile(r"^func (Test[A-Za-z0-9_]+)\(")
    for path in sorted(module.rglob("*_test.go")):
        for line in path.read_text(encoding="utf-8").splitlines():
            match = pattern.match(line)
            if match:
                tests.add(match.group(1))
    return tests


def validate_manifest(document: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if document.get("schema_version") != 1:
        errors.append("schema_version must be 1")
    acceptance = document.get("acceptance")
    if not isinstance(acceptance, dict):
        return errors + ["acceptance must be an object"]
    if list(acceptance) != EXPECTED_IDS:
        errors.append("acceptance IDs must be ordered AC-101 through AC-128")
    main_tests = _declared_go_tests(GO_MODULE)
    bootstrap_tests = _declared_go_tests(BOOTSTRAP_MODULE)
    for acceptance_id, contract in acceptance.items():
        if not isinstance(contract, dict):
            errors.append(f"{acceptance_id} must be an object")
            continue
        tests = contract.get("tests", {})
        if not isinstance(tests, dict):
            errors.append(f"{acceptance_id}.tests must be an object")
            continue
        unknown_groups = set(tests) - TEST_GROUPS
        if unknown_groups:
            errors.append(f"{acceptance_id} has unknown test groups: {sorted(unknown_groups)}")
        for group, names in tests.items():
            if not isinstance(names, list) or not names or not all(
                isinstance(name, str) and name.startswith("Test") for name in names
            ):
                errors.append(f"{acceptance_id}.tests.{group} must contain test names")
                continue
            if len(names) != len(set(names)):
                errors.append(
                    f"{acceptance_id}.tests.{group} must not repeat test names"
                )
            available = bootstrap_tests if group == "bootstrap" else main_tests
            for name in names:
                if name not in available:
                    errors.append(f"{acceptance_id} references missing {group} test: {name}")
        evidence = contract.get("evidence")
        if not isinstance(evidence, str) or Path(evidence).is_absolute() or ".." in Path(evidence).parts:
            errors.append(f"{acceptance_id}.evidence must be a safe relative path")
        required_evidence = contract.get("required_evidence", [])
        if not isinstance(required_evidence, list):
            errors.append(f"{acceptance_id}.required_evidence must be a list")
        else:
            evidence_ids: set[str] = set()
            for requirement in required_evidence:
                if not isinstance(requirement, dict):
                    errors.append(f"{acceptance_id} has malformed required evidence")
                    continue
                requirement_id = requirement.get("id")
                if not isinstance(requirement_id, str) or not requirement_id:
                    errors.append(f"{acceptance_id} has required evidence without id")
                elif requirement_id in evidence_ids:
                    errors.append(f"{acceptance_id} repeats required evidence {requirement_id}")
                else:
                    evidence_ids.add(requirement_id)
                kind = requirement.get("kind")
                if kind not in EVIDENCE_KINDS:
                    errors.append(f"{acceptance_id}.{requirement_id} has unknown kind {kind}")
                path = requirement.get("evidence")
                if not _safe_relative_path(path):
                    errors.append(f"{acceptance_id}.{requirement_id} has an unsafe evidence path")
                if not isinstance(requirement.get("platform_bound", False), bool):
                    errors.append(f"{acceptance_id}.{requirement_id}.platform_bound must be boolean")
                values = requirement.get("required_values", {})
                if not isinstance(values, dict) or not all(
                    isinstance(key, str) and key for key in values
                ):
                    errors.append(f"{acceptance_id}.{requirement_id}.required_values must be an object")
        requirements = contract.get("external_requirements", [])
        if not isinstance(requirements, list):
            errors.append(f"{acceptance_id}.external_requirements must be a list")
        else:
            requirement_ids: set[str] = set()
            for requirement in requirements:
                if not isinstance(requirement, dict):
                    errors.append(f"{acceptance_id} has a malformed external requirement")
                    continue
                requirement_id = requirement.get("id")
                if not isinstance(requirement_id, str) or not requirement_id:
                    errors.append(f"{acceptance_id} has an external requirement without id")
                elif requirement_id in requirement_ids:
                    errors.append(f"{acceptance_id} repeats external requirement {requirement_id}")
                else:
                    requirement_ids.add(requirement_id)
                path = requirement.get("evidence")
                if not _safe_relative_path(path):
                    errors.append(f"{acceptance_id}.{requirement_id} has an unsafe evidence path")
        if not tests and not requirements:
            errors.append(f"{acceptance_id} has neither executable tests nor external requirements")
    gates = document.get("gates")
    if not isinstance(gates, dict) or list(gates) != [f"G{number}" for number in range(6)]:
        errors.append("gates must be ordered G0 through G5")
    else:
        for gate, contract in gates.items():
            referenced = contract.get("acceptance", []) if isinstance(contract, dict) else []
            unknown = set(referenced) - set(EXPECTED_IDS)
            if unknown:
                errors.append(f"{gate} references unknown acceptance IDs: {sorted(unknown)}")
    return errors


def _safe_relative_path(value: object) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and not Path(value).is_absolute()
        and ".." not in Path(value).parts
    )


def git_sha(root: Path = ROOT) -> str:
    completed = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=root, capture_output=True, text=True, check=True
    )
    value = completed.stdout.strip()
    if not re.fullmatch(r"[a-f0-9]{40}", value):
        raise AcceptanceError("git did not return a full lowercase SHA-1")
    return value


def git_dirty(root: Path = ROOT) -> bool:
    completed = subprocess.run(
        ["git", "status", "--porcelain", "--untracked-files=all"],
        cwd=root,
        capture_output=True,
        text=True,
        check=True,
    )
    return bool(completed.stdout.strip())


def native_platform() -> tuple[str, str, str]:
    if sys.platform == "darwin":
        goos = "darwin"
    elif sys.platform.startswith("linux"):
        goos = "linux"
    elif sys.platform in {"win32", "cygwin"}:
        goos = "windows"
    else:
        goos = sys.platform.lower().replace("_", "-")
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        goarch = "amd64"
    elif machine in {"arm64", "aarch64"}:
        goarch = "arm64"
    else:
        goarch = machine.replace("_", "-")
    return goos, goarch, f"{goos}-{goarch}"


def _run_go_suite(group: str, out_dir: Path, toolchain: str) -> dict[str, Any]:
    module = BOOTSTRAP_MODULE if group == "bootstrap" else GO_MODULE
    command = ["go", "test"]
    if group == "race":
        command.append("-race")
    command.extend(["-json", "./..."])
    environment = {
        **os.environ,
        "GOTOOLCHAIN": toolchain,
        "TZ": "UTC",
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    completed = subprocess.run(
        command,
        cwd=module,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
        timeout=1800,
    )
    raw_dir = out_dir / "raw"
    raw_dir.mkdir(parents=True, exist_ok=True)
    stdout_path = raw_dir / f"go-{group}.jsonl"
    stderr_path = raw_dir / f"go-{group}.stderr.txt"
    stdout_path.write_text(completed.stdout, encoding="utf-8")
    stderr_path.write_text(completed.stderr, encoding="utf-8")
    return {
        "group": group,
        "applicable": True,
        "command": command,
        "returncode": completed.returncode,
        "stdout": stdout_path.relative_to(out_dir).as_posix(),
        "stderr": stderr_path.relative_to(out_dir).as_posix(),
    }


def _parse_go_events(path: Path) -> tuple[dict[str, list[dict[str, Any]]], list[str]]:
    terminals: dict[str, list[dict[str, Any]]] = {}
    errors: list[str] = []
    for line_number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not raw_line.strip():
            continue
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError:
            errors.append(f"invalid go test JSON at line {line_number}")
            continue
        test_name = event.get("Test")
        action = event.get("Action")
        if isinstance(test_name, str) and action in TERMINAL_ACTIONS:
            terminals.setdefault(test_name, []).append(
                {
                    "package": event.get("Package"),
                    "status": "passed" if action == "pass" else "failed" if action == "fail" else "not_run",
                    "elapsed_seconds": event.get("Elapsed", 0),
                }
            )
    return terminals, errors


def _result_for_test(name: str, group: str, events: dict[str, list[dict[str, Any]]]) -> dict[str, Any]:
    matches = events.get(name, [])
    if not matches:
        return {"name": name, "group": group, "status": "not_run", "matches": []}
    statuses = {match["status"] for match in matches}
    if "failed" in statuses:
        status = "failed"
    elif statuses == {"passed"}:
        status = "passed"
    else:
        status = "not_run"
    return {"name": name, "group": group, "status": status, "matches": matches}


def _load_json_object(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("evidence must be a JSON object")
    return value


def _bound_commit(document: dict[str, Any]) -> str | None:
    for key in ("git_sha", "git_commit"):
        value = document.get(key)
        if isinstance(value, str):
            return value
    candidate = document.get("candidate_identity")
    if isinstance(candidate, dict):
        value = candidate.get("git_commit")
        if isinstance(value, str):
            return value
    return None


def _required_value(document: dict[str, Any], dotted_key: str) -> object:
    value: object = document
    for part in dotted_key.split("."):
        if not isinstance(value, dict) or part not in value:
            raise KeyError(dotted_key)
        value = value[part]
    return value


def evaluate_required_evidence(
    requirement: dict[str, Any],
    evidence_root: Path,
    commit: str,
    platform_id: str,
    acceptance_id: str,
    expected_candidate_identity: dict[str, Any] | None = None,
) -> dict[str, Any]:
    relative = Path(requirement["evidence"])
    path = evidence_root / relative
    result = {
        "id": requirement["id"],
        "kind": requirement["kind"],
        "evidence": relative.as_posix(),
        "status": "blocked",
        "errors": [],
    }
    if not path.is_file():
        result["errors"].append("required evidence is missing")
        return result
    try:
        document = _load_json_object(path)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
        result.update(status="failed", errors=[f"required evidence is invalid: {error}"])
        return result
    if _bound_commit(document) != commit:
        result.update(
            status="failed",
            errors=["required evidence is not bound to the target git SHA"],
        )
        return result
    if requirement.get("platform_bound") and document.get("platform") != platform_id:
        result.update(
            status="failed",
            errors=["required evidence is not bound to the native platform"],
        )
        return result
    if expected_candidate_identity is not None:
        identity_errors = compare_candidate_identities(
            document.get("candidate_identity"), expected_candidate_identity
        )
        if identity_errors:
            result.update(status="failed", errors=identity_errors)
            return result
    kind = requirement["kind"]
    errors: list[str] = []
    if kind == "contract_report":
        total = document.get("total")
        cases = document.get("cases")
        if document.get("kind") != "contract-comparison":
            errors.append("contract evidence has the wrong kind")
        if not isinstance(total, int) or total <= 0:
            errors.append("contract evidence total must be positive")
        if document.get("failed") != 0 or document.get("passed") != total:
            errors.append("contract comparison contains failures or incomplete cases")
        if not isinstance(cases, list) or len(cases) != total:
            errors.append("contract comparison case count is inconsistent")
        elif any(
            not isinstance(case, dict)
            or case.get("passed") is not True
            or case.get("differences") not in ([], None)
            for case in cases
        ):
            errors.append("contract comparison contains a failed or differing case")
    elif kind == "p5_acceptance":
        if document.get("schema_version") != 1:
            errors.append("P5 evidence has the wrong schema version")
        if document.get("acceptance") != acceptance_id:
            errors.append("P5 evidence belongs to a different acceptance ID")
        if document.get("status") != "passed":
            errors.append("P5 evidence status is not passed")
    for dotted_key, expected in requirement.get("required_values", {}).items():
        try:
            actual = _required_value(document, dotted_key)
        except KeyError:
            errors.append(f"required value is missing: {dotted_key}")
            continue
        if actual != expected:
            errors.append(f"required value differs: {dotted_key}")
    if errors:
        result.update(status="failed", errors=errors)
        return result
    result.update(status="passed", sha256=sha256_file(path))
    return result


def evaluate_external_requirement(
    requirement: dict[str, Any],
    external_root: Path,
    commit: str,
    expected_candidate_identity: dict[str, Any] | None = None,
) -> dict[str, Any]:
    relative = Path(requirement["evidence"])
    path = external_root / relative
    result = {
        "id": requirement["id"],
        "kind": requirement.get("kind"),
        "evidence": relative.as_posix(),
        "status": "blocked",
        "errors": [],
    }
    if not path.is_file():
        result["errors"].append("evidence is missing")
        return result
    try:
        document = _load_json_object(path)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
        result.update(status="failed", errors=[f"evidence is invalid: {error}"])
        return result
    bound = _bound_commit(document)
    if bound != commit:
        result.update(
            status="failed",
            errors=["evidence is not bound to the target git SHA"],
        )
        return result
    if expected_candidate_identity is not None:
        identity_errors = compare_candidate_identities(
            document.get("candidate_identity"), expected_candidate_identity
        )
        if identity_errors:
            result.update(status="failed", errors=identity_errors)
            return result
    kind = requirement.get("kind")
    if kind == "model_eval":
        summary = document.get("summary")
        minimum_trials = int(requirement.get("minimum_trials", 1))
        if not isinstance(summary, dict):
            result["errors"].append("model evidence summary is missing")
        else:
            if int(summary.get("failed", 0)) != 0:
                result.update(status="failed")
                result["errors"].append("model evidence contains failed trials")
            if any(int(summary.get(key, 0)) != 0 for key in ("blocked", "not_run")):
                result["errors"].append("model evidence contains blocked or not-run trials")
            if int(document.get("trials", 0)) < minimum_trials:
                result["errors"].append("model evidence has too few trials")
    elif kind == "native_platform_matrix":
        required = set(requirement.get("required_platforms", []))
        platforms = document.get("platforms")
        statuses: dict[str, str] = {}
        if isinstance(platforms, list):
            for row in platforms:
                if isinstance(row, dict):
                    identifier = row.get("platform") or row.get("id")
                    status = row.get("status")
                    if isinstance(identifier, str) and isinstance(status, str):
                        statuses[identifier] = status
        elif isinstance(platforms, dict):
            for identifier, row in platforms.items():
                status = row.get("status") if isinstance(row, dict) else row
                if isinstance(identifier, str) and isinstance(status, str):
                    statuses[identifier] = status
        failed = sorted(
            identifier
            for identifier in required
            if statuses.get(identifier) in {"failed", "error"}
        )
        incomplete = sorted(
            identifier
            for identifier in required
            if statuses.get(identifier) not in {"passed", "failed", "error"}
        )
        if failed:
            result["status"] = "failed"
            result["errors"].append(f"native platforms failed: {failed}")
        if incomplete:
            result["errors"].append(f"native platforms are incomplete: {incomplete}")
    elif kind == "rollout":
        required = set(requirement.get("required_cohorts", []))
        cohorts = document.get("cohorts")
        statuses = {
            str(row.get("id")): str(row.get("status"))
            for row in cohorts or []
            if isinstance(row, dict) and isinstance(row.get("id"), str)
        }
        failed = sorted(
            identifier
            for identifier in required
            if statuses.get(identifier) in {"failed", "error"}
        )
        if failed:
            result["status"] = "failed"
            result["errors"].append(f"rollout cohorts failed: {failed}")
        if any(status in {"failed", "error"} for status in (document.get("status"),)):
            result["status"] = "failed"
            result["errors"].append("rollout evidence status is failed")
        if any(statuses.get(identifier) not in {"passed", "failed", "error"} for identifier in required):
            result["errors"].append("rollout cohorts are incomplete")
        if int(document.get("released_versions", 0)) < int(
            requirement.get("minimum_released_versions", 0)
        ):
            result["errors"].append("released-version observation windows are incomplete")
    else:
        status = document.get("status")
        if status in {"failed", "error"}:
            result["status"] = "failed"
            result["errors"].append("evidence status is failed")
        elif status not in {"passed", "ready", "complete"}:
            result["errors"].append("evidence status is not passed")
    if not result["errors"]:
        result["status"] = "passed"
        result["sha256"] = sha256_file(path)
    return result


def _status(*groups: list[dict[str, Any]]) -> str:
    statuses = [row["status"] for group in groups for row in group]
    if "failed" in statuses:
        return "failed"
    if "blocked" in statuses:
        return "blocked"
    if not statuses or "not_run" in statuses:
        return "not_run"
    return "passed"


def _write_junit(path: Path, acceptance: dict[str, Any]) -> None:
    rows = (
        acceptance["tests"]
        + acceptance["required_evidence"]
        + acceptance["external_requirements"]
    )
    suite = ET.Element(
        "testsuite",
        name=acceptance["acceptance_id"],
        tests=str(len(rows)),
        failures=str(sum(row["status"] == "failed" for row in rows)),
        skipped=str(sum(row["status"] in {"blocked", "not_run"} for row in rows)),
    )
    for row in rows:
        case = ET.SubElement(
            suite,
            "testcase",
            classname=acceptance["acceptance_id"],
            name=str(row.get("name") or row.get("id")),
        )
        if row["status"] == "failed":
            failure = ET.SubElement(case, "failure", message="; ".join(row.get("errors", [])))
            failure.text = json.dumps(row, ensure_ascii=False, sort_keys=True)
        elif row["status"] in {"blocked", "not_run"}:
            skipped = ET.SubElement(case, "skipped", message="; ".join(row.get("errors", [])))
            skipped.text = row["status"]
    path.parent.mkdir(parents=True, exist_ok=True)
    ET.ElementTree(suite).write(path, encoding="utf-8", xml_declaration=True)


def run_acceptance(
    out_dir: Path,
    *,
    external_root: Path | None = None,
    execute: bool = True,
    commit: str | None = None,
    dirty: bool | None = None,
    candidate_identity: dict[str, Any] | None = None,
) -> tuple[dict[str, Any], int]:
    manifest = load_manifest()
    validation_errors = validate_manifest(manifest)
    if validation_errors:
        raise AcceptanceError("; ".join(validation_errors))
    commit = commit or git_sha()
    working_tree_dirty = git_dirty() if dirty is None else dirty
    goos, goarch, platform_id = native_platform()
    out_dir.mkdir(parents=True, exist_ok=True)
    external_root = external_root or out_dir
    suite_runs: dict[str, dict[str, Any]] = {}
    event_sets: dict[str, dict[str, list[dict[str, Any]]]] = {}
    parse_errors: dict[str, list[str]] = {}
    groups = sorted(
        {
            group
            for contract in manifest["acceptance"].values()
            for group in contract.get("tests", {})
        }
    )
    for group in groups:
        if group == "race" and goos == "windows":
            suite_runs[group] = {
                "group": group,
                "applicable": False,
                "reason": "Go race execution is collected on native Darwin and Linux runners",
                "returncode": None,
            }
            event_sets[group] = {}
            parse_errors[group] = []
            continue
        raw_path = out_dir / "raw" / f"go-{group}.jsonl"
        if execute:
            suite_runs[group] = _run_go_suite(group, out_dir, manifest["go_toolchain"])
        elif not raw_path.is_file():
            raise AcceptanceError(f"missing pre-recorded event stream: {raw_path}")
        else:
            suite_runs[group] = {
                "group": group,
                "applicable": True,
                "returncode": 0,
                "stdout": raw_path.relative_to(out_dir).as_posix(),
            }
        event_sets[group], parse_errors[group] = _parse_go_events(raw_path)
    environment = {
        "schema_version": 1,
        "git_sha": commit,
        "working_tree_dirty": working_tree_dirty,
        "platform": platform_id,
        "goos": goos,
        "goarch": goarch,
        "python": platform.python_version(),
        "go_toolchain": manifest["go_toolchain"],
        "sdk_version": manifest["sdk_version"],
        "manifest_sha256": sha256_file(MANIFEST_PATH),
    }
    if candidate_identity is not None:
        environment["candidate_identity"] = candidate_identity
    (out_dir / "environment.json").write_bytes(canonical_json(environment))
    results: list[dict[str, Any]] = []
    for acceptance_id, contract in manifest["acceptance"].items():
        test_results: list[dict[str, Any]] = []
        for group, names in contract.get("tests", {}).items():
            if not suite_runs[group]["applicable"]:
                continue
            test_results.extend(_result_for_test(name, group, event_sets[group]) for name in names)
        requirements = [
            evaluate_external_requirement(
                requirement,
                external_root,
                commit,
                candidate_identity,
            )
            for requirement in contract.get("external_requirements", [])
        ]
        required_evidence = [
            evaluate_required_evidence(
                requirement,
                external_root,
                commit,
                platform_id,
                acceptance_id,
                candidate_identity,
            )
            for requirement in contract.get("required_evidence", [])
        ]
        status = _status(test_results, required_evidence, requirements)
        result = {
            "schema_version": 1,
            "acceptance_id": acceptance_id,
            "title": contract["title"],
            "owners": contract["owners"],
            "gates": contract["gates"],
            "git_sha": commit,
            "platform": platform_id,
            "working_tree_dirty": working_tree_dirty,
            "status": status,
            "blocking": status != "passed",
            "tests": test_results,
            "required_evidence": required_evidence,
            "external_requirements": requirements,
            "declared_evidence": contract["evidence"],
        }
        if candidate_identity is not None:
            result["candidate_identity"] = candidate_identity
        result_path = out_dir / "ac-results" / f"{acceptance_id.lower()}.json"
        result_path.parent.mkdir(parents=True, exist_ok=True)
        result_path.write_bytes(canonical_json(result))
        _write_junit(out_dir / "junit" / f"{acceptance_id.lower()}.xml", result)
        results.append(result)
    counts = Counter(result["status"] for result in results)
    runner_errors = [
        f"go {group} suite exited {run['returncode']}"
        for group, run in suite_runs.items()
        if run["applicable"] and run["returncode"] != 0
    ]
    for group, errors in parse_errors.items():
        runner_errors.extend(f"{group}: {error}" for error in errors)
    summary = {
        "schema_version": 1,
        "suite": "ac-101-ac-128",
        "git_sha": commit,
        "platform": platform_id,
        "working_tree_dirty": working_tree_dirty,
        "status": "failed" if runner_errors or counts["failed"] else "blocked" if counts["blocked"] or counts["not_run"] else "passed",
        "counts": {status: counts[status] for status in ("passed", "failed", "blocked", "not_run")},
        "runner_errors": runner_errors,
        "suite_runs": suite_runs,
        "results": [
            {
                "acceptance_id": result["acceptance_id"],
                "status": result["status"],
                "path": f"ac-results/{result['acceptance_id'].lower()}.json",
            }
            for result in results
        ],
    }
    if candidate_identity is not None:
        summary["candidate_identity"] = candidate_identity
    (out_dir / "runner-summary.json").write_bytes(canonical_json(summary))
    exit_code = 1 if runner_errors or counts["failed"] or counts["not_run"] else 0
    return summary, exit_code


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, default=MANIFEST_PATH)
    parser.add_argument("--out-dir", type=Path)
    parser.add_argument("--external-root", type=Path)
    parser.add_argument("--process-existing", action="store_true")
    parser.add_argument("--require-complete", action="store_true")
    parser.add_argument("--validate", action="store_true")
    parser.add_argument("--candidate-identity", type=Path)
    args = parser.parse_args(argv)
    document = load_manifest(args.manifest)
    errors = validate_manifest(document)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    if args.validate:
        print("acceptance manifest verified")
        return 0
    commit = git_sha()
    _, _, platform_id = native_platform()
    out_dir = args.out_dir or ROOT / "artifacts" / "go-sdk-acceptance" / commit / platform_id
    try:
        candidate_identity = (
            load_candidate_identity(args.candidate_identity)
            if args.candidate_identity is not None
            else None
        )
    except CandidateIdentityError as error:
        print(str(error), file=sys.stderr)
        return 1
    if candidate_identity is not None and candidate_identity["git_sha"] != commit:
        print("candidate identity is not bound to the current git SHA", file=sys.stderr)
        return 1
    summary, exit_code = run_acceptance(
        out_dir,
        external_root=args.external_root,
        execute=not args.process_existing,
        commit=commit,
        candidate_identity=candidate_identity,
    )
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if args.require_complete and summary["status"] != "passed":
        return 2 if exit_code == 0 else exit_code
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
