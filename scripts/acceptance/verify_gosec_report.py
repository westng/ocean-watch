#!/usr/bin/env python3
"""Verify a full gosec report against the narrow, expiring control inventory."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
from collections import Counter
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CONTROLS = ROOT / "contracts" / "security" / "gosec-controls.json"
ALLOWED_RULES = {"G204", "G302", "G304"}
ALLOWED_SEVERITIES = {"LOW", "MEDIUM"}
ALLOWED_CONFIDENCE = {"LOW", "MEDIUM", "HIGH"}
ROLE_IDS = {"AO", "AP", "MA", "MT", "QA-API", "QO", "RL", "RO", "SCO", "SO"}
HEX_64 = re.compile(r"^[0-9a-f]{64}$")
CONTROL_ID = re.compile(r"^GOSEC-[A-Z0-9-]+$")
LINE_PREFIX = re.compile(r"^[ \t]*[0-9]+:[ \t]?", re.MULTILINE)


class GosecControlError(ValueError):
    pass


def parse_rfc3339(value: object) -> dt.datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None else None


def code_fingerprint(code: object) -> str:
    if not isinstance(code, str) or not code:
        raise GosecControlError("gosec finding is missing source code context")
    normalized = LINE_PREFIX.sub("", code)
    return hashlib.sha256(normalized.encode("utf-8")).hexdigest()


def normalize_report_path(value: object, module: str) -> str:
    if not isinstance(value, str) or not value:
        raise GosecControlError("gosec finding is missing a file path")
    normalized = value.replace("\\", "/")
    module_prefix = module.rstrip("/") + "/"
    if normalized.startswith(module_prefix):
        return normalized
    marker = "/" + module_prefix
    index = normalized.rfind(marker)
    if index < 0:
        raise GosecControlError(f"gosec finding path is outside {module}: {value}")
    return normalized[index + 1 :]


def finding_keys(issues: list[dict[str, Any]], module: str) -> list[tuple[str, ...]]:
    provisional: list[tuple[str, str, str, str, str, int]] = []
    for issue in issues:
        if not isinstance(issue, dict):
            raise GosecControlError("gosec Issues must contain objects")
        try:
            line = int(issue.get("line") or 0)
        except (TypeError, ValueError) as error:
            raise GosecControlError("gosec finding line must be an integer") from error
        provisional.append(
            (
                str(issue.get("rule_id") or ""),
                normalize_report_path(issue.get("file"), module),
                str(issue.get("severity") or "").upper(),
                str(issue.get("confidence") or "").upper(),
                code_fingerprint(issue.get("code")),
                line,
            )
        )
    provisional.sort(key=lambda row: (row[:5], row[5]))
    occurrences: Counter[tuple[str, ...]] = Counter()
    result: list[tuple[str, ...]] = []
    for rule_id, file, severity, confidence, fingerprint, _line in provisional:
        base = (rule_id, file, severity, confidence, fingerprint)
        occurrences[base] += 1
        result.append((*base, str(occurrences[base])))
    return result


def load_controls(
    path: Path,
    module: str,
    now: dt.datetime,
) -> tuple[set[tuple[str, ...]], list[str], list[str]]:
    document = json.loads(path.read_text(encoding="utf-8"))
    errors: list[str] = []
    control_ids: list[str] = []
    if document.get("schema_version") != 1 or document.get("kind") != "gosec_control_inventory":
        errors.append("gosec control inventory must use schema 1 and the expected kind")
    if document.get("scanner_install_version") != "v2.22.10":
        errors.append("gosec control inventory must pin scanner_install_version v2.22.10")
    if document.get("does_not_grant_gate_signoff") is not True:
        errors.append("gosec control inventory must state that it does not grant Gate signoff")
    controls = document.get("controls")
    if not isinstance(controls, list):
        return set(), errors + ["gosec control inventory controls must be a list"], []

    expected: set[tuple[str, ...]] = set()
    seen_ids: set[str] = set()
    for control in controls:
        if not isinstance(control, dict):
            errors.append("gosec control entries must be objects")
            continue
        if control.get("module") != module:
            continue
        control_id = str(control.get("id") or "")
        control_ids.append(control_id)
        if not CONTROL_ID.fullmatch(control_id) or control_id in seen_ids:
            errors.append(f"invalid or duplicate gosec control id: {control_id or '<missing>'}")
        seen_ids.add(control_id)
        rule_id = str(control.get("rule_id") or "")
        severity = str(control.get("severity") or "").upper()
        confidence = str(control.get("confidence") or "").upper()
        if rule_id not in ALLOWED_RULES:
            errors.append(f"{control_id} uses an unknown or non-exemptible rule: {rule_id}")
        if severity not in ALLOWED_SEVERITIES:
            errors.append(f"{control_id} cannot document severity {severity or '<missing>'}")
        if confidence not in ALLOWED_CONFIDENCE:
            errors.append(f"{control_id} has invalid confidence {confidence or '<missing>'}")
        if control.get("owner") not in ROLE_IDS:
            errors.append(f"{control_id} is missing a recognized implementation owner")
        if control.get("security_reviewer") != "SO":
            errors.append(f"{control_id} must name SO as security reviewer")
        expires_at = parse_rfc3339(control.get("expires_at"))
        if expires_at is None or expires_at <= now:
            errors.append(f"{control_id} is missing an unexpired RFC3339 expires_at")
        for field in ("rationale", "removal_condition"):
            if not isinstance(control.get(field), str) or not control[field].strip():
                errors.append(f"{control_id} is missing {field}")
        safeguards = control.get("controls")
        if not isinstance(safeguards, list) or len(safeguards) < 2 or not all(
            isinstance(item, str) and item.strip() for item in safeguards
        ):
            errors.append(f"{control_id} must document at least two control measures")
        findings = control.get("findings")
        if not isinstance(findings, list) or not findings:
            errors.append(f"{control_id} must bind at least one finding")
            continue
        for finding in findings:
            if not isinstance(finding, dict):
                errors.append(f"{control_id} findings must be objects")
                continue
            file = str(finding.get("file") or "").replace("\\", "/")
            fingerprint = str(finding.get("code_sha256") or "")
            occurrence = finding.get("occurrence")
            if not file.startswith(module.rstrip("/") + "/") or ".." in Path(file).parts:
                errors.append(f"{control_id} finding escapes its module: {file}")
            if not HEX_64.fullmatch(fingerprint):
                errors.append(f"{control_id} finding has an invalid code_sha256")
            if not isinstance(occurrence, int) or isinstance(occurrence, bool) or occurrence < 1:
                errors.append(f"{control_id} finding occurrence must be a positive integer")
                continue
            key = (rule_id, file, severity, confidence, fingerprint, str(occurrence))
            if key in expected:
                errors.append(f"{control_id} duplicates a finding binding")
            expected.add(key)
    if not control_ids:
        errors.append(f"gosec control inventory has no entries for {module}")
    return expected, errors, control_ids


def verify_report(
    report: dict[str, Any],
    controls_path: Path,
    module: str,
    *,
    now: dt.datetime | None = None,
) -> dict[str, Any]:
    current_time = now or dt.datetime.now(dt.timezone.utc)
    if current_time.tzinfo is None:
        raise GosecControlError("verification time must be timezone-aware")
    errors: list[str] = []
    scanner_errors = report.get("Golang errors")
    if scanner_errors not in (None, {}, []):
        errors.append("gosec report contains Go analysis errors")
    issues_value = report.get("Issues")
    issues = [] if issues_value is None else issues_value
    if not isinstance(issues, list):
        raise GosecControlError("gosec report Issues must be a list or null")
    stats = report.get("Stats")
    if not isinstance(stats, dict):
        errors.append("gosec report is missing Stats")
        stats = {}
    if stats.get("nosec") != 0:
        errors.append("gosec source suppressions are forbidden")
    if stats.get("found") != len(issues):
        errors.append("gosec report finding count does not match Issues")

    try:
        actual = set(finding_keys(issues, module))
    except GosecControlError as error:
        errors.append(str(error))
        actual = set()
    expected, inventory_errors, control_ids = load_controls(controls_path, module, current_time)
    errors.extend(inventory_errors)
    high_or_critical = sorted(
        key for key in actual if key[2] in {"HIGH", "CRITICAL"}
    )
    if high_or_critical:
        errors.append(f"gosec High/Critical findings cannot be documented: {len(high_or_critical)}")
    unregistered = sorted(actual - expected)
    stale = sorted(expected - actual)
    if unregistered:
        errors.append(f"gosec report contains unregistered findings: {len(unregistered)}")
    if stale:
        errors.append(f"gosec control inventory contains stale findings: {len(stale)}")
    status = "passed" if not errors else "failed"
    return {
        "schema_version": 1,
        "acceptance": "AC-125",
        "kind": "gosec-control-audit",
        "status": status,
        "module": module,
        "scanner_install_version": "v2.22.10",
        "finding_count": len(actual),
        "documented_finding_count": len(actual & expected),
        "unregistered_finding_count": len(unregistered),
        "stale_finding_count": len(stale),
        "high_or_critical_count": len(high_or_critical),
        "nosec_count": stats.get("nosec"),
        "control_ids": sorted(control_ids),
        "gate_signoff_granted": False,
        "errors": errors,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--module", required=True)
    parser.add_argument("--controls", type=Path, default=DEFAULT_CONTROLS)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    report = json.loads(args.report.read_text(encoding="utf-8"))
    result = verify_report(report, args.controls, args.module)
    payload = json.dumps(result, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"
    if args.out:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(payload, encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
