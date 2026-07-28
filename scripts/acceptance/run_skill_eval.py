#!/usr/bin/env python3
"""Run semantic Skill contract cases with an optional model driver.

The runner owns deterministic assertions; the model owns natural-language intent
resolution. Keeping those responsibilities separate prevents a Skill from being
reduced to a list of trigger phrases.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shlex
import signal
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
CASES = ROOT / "contracts" / "skill-evals" / "cases.json"
PLUGIN_MANIFEST = ROOT / ".codex-plugin" / "plugin.json"


def _load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def _validate_cases(document: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if document.get("schema_version") != 1:
        errors.append("schema_version must be 1")
    cases = document.get("cases")
    if not isinstance(cases, list) or not cases:
        return errors + ["cases must be a non-empty list"]
    seen: set[str] = set()
    for index, case in enumerate(cases):
        prefix = f"cases[{index}]"
        if not isinstance(case, dict):
            errors.append(f"{prefix} must be an object")
            continue
        case_id = case.get("id")
        if not isinstance(case_id, str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]+", case_id):
            errors.append(f"{prefix}.id is invalid")
        elif case_id in seen:
            errors.append(f"duplicate case id: {case_id}")
        else:
            seen.add(case_id)
        turns = case.get("turns")
        if not isinstance(turns, list) or not turns:
            errors.append(f"{prefix}.turns must be non-empty")
        else:
            for turn_index, turn in enumerate(turns):
                if not isinstance(turn, dict) or turn.get("role") not in {"user", "assistant"}:
                    errors.append(f"{prefix}.turns[{turn_index}] role is invalid")
                elif not isinstance(turn.get("content"), str) or not turn["content"].strip():
                    errors.append(f"{prefix}.turns[{turn_index}] content is empty")
        expected = case.get("expected")
        if not isinstance(expected, dict) or not expected.get("command"):
            errors.append(f"{prefix}.expected.command is required")
    return errors


def _commands(value: Any) -> set[str]:
    if isinstance(value, str):
        return {value}
    if isinstance(value, list) and all(isinstance(item, str) for item in value):
        return set(value)
    return set()


def _normalise_command(value: Any) -> str:
    if isinstance(value, dict):
        value = value.get("command") or value.get("cmd") or ""
    if not isinstance(value, str):
        return ""
    try:
        tokens = shlex.split(value)
    except ValueError:
        tokens = value.split()
    for index in range(len(tokens) - 1):
        if tokens[index] in {"ocean-watch", "run.py"} and index + 2 < len(tokens):
            return f"{tokens[index + 1]} {tokens[index + 2]}"
    if len(tokens) >= 2:
        for index in range(len(tokens) - 1):
            candidate = f"{tokens[index]} {tokens[index + 1]}"
            if re.fullmatch(r"[a-z][a-z0-9-]* [a-z][a-z0-9-]*", candidate):
                return candidate
    return value.strip()


_SECRET_PATTERNS = (
    re.compile(r"(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+"),
    re.compile(r"(?i)((?:access|refresh)[_-]?token\s*[:=]\s*)[^\s,}]+"),
    re.compile(r"(?i)((?:app_?secret|auth_?code|client_?secret)\s*[:=]\s*)[^\s,}]+"),
    re.compile(r"https?://[^\s]+(?:mcp|token)[^\s]*", re.IGNORECASE),
)


_EVIDENCE_METADATA_KEYS = {
    "model",
    "codex_version",
    "plugin_version",
    "git_commit",
    "reasoning",
    "case_id",
    "case_set",
    "skill",
    "status",
    "type",
    "item_type",
}


def _redact(value: Any, key: str | None = None) -> Any:
    if isinstance(value, dict):
        return {str(item_key): _redact(item, str(item_key)) for item_key, item in value.items()}
    if isinstance(value, list):
        return [_redact(item, key) for item in value]
    if not isinstance(value, str):
        return value
    redacted = value
    for pattern in _SECRET_PATTERNS:
        redacted = pattern.sub(lambda match: f"{match.group(1) if match.lastindex else ''}[REDACTED]", redacted)
    # Hash long numeric IDs so evidence cannot become a business-data export.
    if key not in _EVIDENCE_METADATA_KEYS:
        redacted = re.sub(
            r"\b\d{13,20}\b",
            lambda match: f"id:{hashlib.sha256(match.group(0).encode()).hexdigest()[:12]}",
            redacted,
        )
    return redacted


def _run_driver(command: list[str], payload: dict[str, Any], timeout: float) -> dict[str, Any]:
    process = subprocess.Popen(
        command,
        text=True,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=ROOT,
        env={**os.environ, "OCEAN_WATCH_SKILL_EVAL": "1"},
        start_new_session=True,
    )
    try:
        stdout, stderr = process.communicate(
            json.dumps(payload, ensure_ascii=False) + "\n", timeout=timeout
        )
    except subprocess.TimeoutExpired:
        if os.name == "nt":
            subprocess.run(
                ["taskkill", "/F", "/T", "/PID", str(process.pid)],
                capture_output=True,
                check=False,
            )
        else:
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=10)
        raise TimeoutError(f"model driver exceeded {timeout} seconds") from None
    if process.returncode != 0:
        raise RuntimeError(
            f"driver exited {process.returncode}: {stderr[-1000:]}"
        )
    lines = [line for line in stdout.splitlines() if line.strip()]
    if len(lines) != 1:
        raise ValueError("driver must print exactly one JSON object")
    result = json.loads(lines[0])
    if not isinstance(result, dict):
        raise ValueError("driver result must be a JSON object")
    return result


def _assert_result(case: dict[str, Any], result: dict[str, Any]) -> list[str]:
    expected = case["expected"]
    errors: list[str] = []
    allowed_commands = _commands(expected.get("command"))
    allowed_skills = _commands(expected.get("skill"))
    calls = result.get("tool_calls")
    if not isinstance(calls, list) or not calls:
        errors.append("no tool_calls observed")
        calls = []
    elif len(calls) != 1:
        errors.append(f"expected exactly one tool call, observed {len(calls)}")
    normalised = [_normalise_command(call) for call in calls]
    if allowed_commands and not allowed_commands.intersection(normalised):
        errors.append(f"expected command {sorted(allowed_commands)}, observed {normalised}")
    forbidden = _commands(expected.get("forbidden_commands"))
    for command in forbidden:
        if command in normalised:
            errors.append(f"forbidden command observed: {command}")
    observed_skills = {
        call.get("skill") for call in calls if isinstance(call, dict) and isinstance(call.get("skill"), str)
    }
    if allowed_skills and not observed_skills:
        errors.append("selected Skill is missing from tool_calls")
    elif allowed_skills and not allowed_skills.intersection(observed_skills):
        errors.append(f"expected skill {sorted(allowed_skills)}, observed {sorted(observed_skills)}")
    expected_channel = expected.get("channel")
    observed_channels = {
        call.get("channel")
        for call in calls
        if isinstance(call, dict) and isinstance(call.get("channel"), str)
    }
    if expected_channel:
        if not observed_channels:
            errors.append("selected channel is missing from tool_calls")
        elif expected_channel != "any" and expected_channel not in observed_channels:
            errors.append(f"expected channel {expected_channel}, observed {sorted(observed_channels)}")
    presentation = expected.get("presentation") or {}
    if presentation.get("required"):
        actual = result.get("presentation")
        if not isinstance(actual, dict) or actual.get("required") is not True:
            errors.append("mandatory presentation flag is missing")
        else:
            rendered = actual.get("rendered_markdown")
            if not isinstance(rendered, str) or not rendered.strip():
                errors.append("mandatory presentation is empty")
                rendered = ""
            if actual.get("source") != "rendered_markdown":
                errors.append("presentation source is not rendered_markdown")
            if presentation.get("verbatim_response") and result.get("assistant_response") != rendered:
                errors.append("assistant response did not preserve rendered_markdown verbatim")
            columns = presentation.get("columns", [])
            if columns:
                table_rows = [
                    [cell.strip() for cell in line.strip().strip("|").split("|")]
                    for line in rendered.splitlines()
                    if line.strip().startswith("|") and line.strip().endswith("|")
                ]
                header = next(
                    (cells for cells in table_rows if all(column in cells for column in columns)),
                    None,
                )
                if header is None:
                    missing = [column for column in columns if column not in rendered]
                    if missing:
                        errors.extend(
                            f"mandatory column missing: {column}" for column in missing
                        )
                    else:
                        errors.append("mandatory columns do not share one table header")
                elif [header.index(column) for column in columns] != sorted(
                    header.index(column) for column in columns
                ):
                    errors.append("mandatory columns are reordered")
            for fragment in presentation.get("required_fragments", []):
                if fragment not in rendered:
                    errors.append(f"mandatory presentation fragment missing: {fragment}")
            fixture = presentation.get("fixture")
            if fixture:
                expected_markdown = (ROOT / fixture).read_text(encoding="utf-8").rstrip("\n")
                if rendered != expected_markdown:
                    errors.append("rendered_markdown differs from the mandatory fixture")
    return errors


def run_suite(
    *,
    case_set: str | None,
    case_ids: set[str] | None = None,
    driver: list[str] | None,
    jobs: int,
    trials: int,
    trial_start: int,
    timeout: float,
    model: str,
    reasoning: str,
    out: Path | None,
) -> dict[str, Any]:
    document = _load_json(CASES)
    contract_errors = _validate_cases(document)
    if contract_errors:
        raise ValueError("invalid skill-eval contract: " + "; ".join(contract_errors))
    cases = [
        case
        for case in document["cases"]
        if (not case_set or case["case_set"] == case_set)
        and (not case_ids or case["id"] in case_ids)
    ]
    selected_ids = {case["id"] for case in cases}
    if case_ids and selected_ids != case_ids:
        raise ValueError(f"unknown case ids: {sorted(case_ids - selected_ids)}")
    if not cases:
        raise ValueError(f"no cases for case set: {case_set}")
    manifest = _load_json(PLUGIN_MANIFEST)
    plugin_version = manifest.get("version", "unknown")
    commit = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=False
    ).stdout.strip()
    def evaluate(task: tuple[int, dict[str, Any]]) -> dict[str, Any]:
        trial, case = task
        started = time.monotonic()
        payload = {
            "case": {
                "id": case["id"],
                "case_set": case["case_set"],
                "turns": case["turns"],
            },
            "skill_roots": ["skills/ads-plan-monitor", "skills/qc-plan-monitor"],
            "model": model,
            "reasoning": reasoning,
            "plugin_version": plugin_version,
        }
        if driver:
            try:
                result = _run_driver(driver, payload, timeout)
                errors = _assert_result(case, result)
                status = "passed" if not errors else "failed"
            except Exception as exc:  # noqa: BLE001 - evidence records driver failures
                result = {"error": str(exc)}
                errors = [str(exc)]
                status = "blocked"
        else:
            result = {"mode": "contract-only"}
            errors = []
            status = "not_run"
        return {
            "case_id": case["id"],
            "case_set": case["case_set"],
            "trial": trial,
            "status": status,
            "errors": errors,
            "elapsed_seconds": round(time.monotonic() - started, 3),
            "trace": _redact(result),
        }

    tasks = [
        (trial, case)
        for trial in range(trial_start, trial_start + trials)
        for case in cases
    ]
    if jobs == 1 or len(tasks) == 1:
        results = [evaluate(task) for task in tasks]
    else:
        with ThreadPoolExecutor(max_workers=min(jobs, len(tasks))) as executor:
            results = list(executor.map(evaluate, tasks))
    passed = sum(row["status"] == "passed" for row in results)
    failed = sum(row["status"] == "failed" for row in results)
    blocked = sum(row["status"] == "blocked" for row in results)
    not_run = sum(row["status"] == "not_run" for row in results)
    evidence = {
        "schema_version": 1,
        "suite": "skill-eval",
        "case_set": case_set or "all",
        "model": model,
        "reasoning": reasoning,
        "plugin_version": plugin_version,
        "git_commit": commit,
        "driver": driver,
        "trials": trials,
        "trial_start": trial_start,
        "jobs": jobs,
        "summary": {
            "total": len(results),
            "passed": passed,
            "failed": failed,
            "blocked": blocked,
            "not_run": not_run,
        },
        "results": results,
    }
    if out:
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(_redact(evidence), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return evidence


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case-set")
    parser.add_argument("--case", action="append", dest="case_ids")
    parser.add_argument("--driver-command")
    parser.add_argument("--trials", type=int, default=1)
    parser.add_argument("--trial-start", type=int, default=1)
    parser.add_argument("--jobs", type=int, default=1)
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--model", default=os.environ.get("OCEAN_WATCH_EVAL_MODEL", "unspecified"))
    parser.add_argument("--reasoning", default=os.environ.get("OCEAN_WATCH_EVAL_REASONING", "unspecified"))
    parser.add_argument("--out", type=Path)
    parser.add_argument("--allow-not-run", action="store_true")
    args = parser.parse_args(argv)
    if args.trials < 1:
        parser.error("--trials must be positive")
    if args.trial_start < 1:
        parser.error("--trial-start must be positive")
    if args.jobs < 1:
        parser.error("--jobs must be positive")
    driver = shlex.split(args.driver_command) if args.driver_command else None
    evidence = run_suite(
        case_set=args.case_set,
        case_ids=set(args.case_ids or []),
        driver=driver,
        jobs=args.jobs,
        trials=args.trials,
        trial_start=args.trial_start,
        timeout=args.timeout,
        model=args.model,
        reasoning=args.reasoning,
        out=args.out,
    )
    print(json.dumps(evidence, ensure_ascii=False, indent=2))
    if evidence["summary"]["failed"] or evidence["summary"]["blocked"]:
        return 1
    if evidence["summary"]["not_run"] and not args.allow_not_run:
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
