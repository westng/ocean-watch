#!/usr/bin/env python3
"""Measure the formal Linux candidate against the frozen Python local-command baseline."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

try:
    from .candidate_identity import canonical_json
    from .p5 import ROOT, AcceptanceError, target_name, verify_candidate
except ImportError:
    from candidate_identity import canonical_json  # type: ignore[no-redef]
    from p5 import ROOT, AcceptanceError, target_name, verify_candidate  # type: ignore[no-redef]

GO_MODULE = ROOT / "prototype" / "ocean-watch-go"
PYTHON_SOURCE = ROOT / "skills" / "ads-plan-monitor" / "src"
ACCOUNT_FIXTURE = ROOT / "testdata" / "contracts" / "python" / "account-book" / "config.json"
TRIALS = 30
LOCAL_P95_LIMIT_MS = 150.0
GO_TO_PYTHON_P95_LIMIT = 1.15


class BenchmarkError(RuntimeError):
    pass


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _percentile(values: list[float], percentile: float) -> float:
    if not values:
        raise BenchmarkError("benchmark has no samples")
    ordered = sorted(values)
    index = max(0, math.ceil(percentile * len(ordered)) - 1)
    return ordered[index]


def _invoke(command: list[str], environment: dict[str, str]) -> float:
    started = time.perf_counter()
    try:
        completed = subprocess.run(
            command,
            cwd=ROOT,
            env=environment,
            capture_output=True,
            text=True,
            check=False,
            timeout=10,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise BenchmarkError("benchmark command could not complete") from error
    elapsed_ms = (time.perf_counter() - started) * 1000
    if completed.returncode != 0:
        raise BenchmarkError(f"benchmark command exited {completed.returncode}")
    try:
        payload = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise BenchmarkError("benchmark command did not return JSON") from error
    if not isinstance(payload, dict) or payload.get("ok") is False:
        raise BenchmarkError("benchmark command returned a failure envelope")
    presentation = payload.get("presentation")
    if not isinstance(presentation, dict) or presentation.get("required") is not True:
        raise BenchmarkError("accounts list lost its mandatory Presentation contract")
    return elapsed_ms


def _measure(command: list[str], environment: dict[str, str], trials: int) -> dict[str, Any]:
    for _ in range(2):
        _invoke(command, environment)
    samples = [_invoke(command, environment) for _ in range(trials)]
    return {
        "trials": trials,
        "p50_ms": round(statistics.median(samples), 3),
        "p95_ms": round(_percentile(samples, 0.95), 3),
        "max_ms": round(max(samples), 3),
    }


def _run_go_checks() -> dict[str, Any]:
    environment = {
        **os.environ,
        "GOTOOLCHAIN": "go1.26.5",
        "TZ": "UTC",
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    budget = subprocess.run(
        [
            "go",
            "test",
            "./internal/performance",
            "-run",
            "^TestRequestBudgets$",
            "-count=30",
        ],
        cwd=GO_MODULE,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
        timeout=1800,
    )
    if budget.returncode != 0:
        raise BenchmarkError("30-run request-budget suite failed")
    benchmark = subprocess.run(
        [
            "go",
            "test",
            "./internal/performance",
            "-run",
            "^$",
            "-bench",
            "^BenchmarkRequestBudgets$",
            "-benchmem",
            "-count=5",
        ],
        cwd=GO_MODULE,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
        timeout=1800,
    )
    if benchmark.returncode != 0 or "BenchmarkRequestBudgets" not in benchmark.stdout:
        raise BenchmarkError("Go benchmark suite failed or returned no benchmark samples")
    return {
        "request_budget_runs": 30,
        "accounts_list_network_attempts": 0,
        "two_account_report_plan_list_calls": 0,
        "qianchuan_failed_page_restart_count": 0,
        "fifty_five_link_broad_creator_scans": 1,
        "go_benchmark_runs": 5,
        "go_benchmark_output_sha256": hashlib.sha256(
            benchmark.stdout.encode("utf-8")
        ).hexdigest(),
    }


def benchmark(
    *,
    candidate_dir: Path,
    python: Path,
    expected_commit: str,
    trials: int = TRIALS,
) -> dict[str, Any]:
    if trials != TRIALS:
        raise BenchmarkError(f"formal AC-122 requires exactly {TRIALS} trials")
    verified = verify_candidate(
        candidate_dir,
        verify_signatures=True,
        require_release=True,
        expected_commit=expected_commit,
    )
    runtime = candidate_dir.resolve() / target_name("ocean-watch", "linux", "amd64")
    if not runtime.is_file() or runtime.is_symlink():
        raise BenchmarkError("formal benchmark requires the Linux amd64 runtime")
    runtime.chmod(0o700)
    fixture_before = _sha256(ACCOUNT_FIXTURE)
    with tempfile.TemporaryDirectory(prefix="ocean-watch-ac122-") as temporary:
        environment = {
            **os.environ,
            "CODEX_HOME": str(Path(temporary) / "codex-home"),
            "PYTHONPATH": str(PYTHON_SOURCE),
            "PYTHONDONTWRITEBYTECODE": "1",
            "PYTHONNOUSERSITE": "1",
            "TZ": "UTC",
        }
        arguments = ["accounts", "list", "--config", str(ACCOUNT_FIXTURE)]
        python_result = _measure(
            [str(python), "-I", "-m", "ocean_watch", *arguments],
            environment,
            trials,
        )
        go_result = _measure([str(runtime), *arguments], environment, trials)
    if _sha256(ACCOUNT_FIXTURE) != fixture_before:
        raise BenchmarkError("performance measurement changed the account fixture")
    request_checks = _run_go_checks()
    python_p95 = float(python_result["p95_ms"])
    go_p95 = float(go_result["p95_ms"])
    ratio = go_p95 / python_p95 if python_p95 > 0 else float("inf")
    if python_p95 > LOCAL_P95_LIMIT_MS or go_p95 > LOCAL_P95_LIMIT_MS:
        raise BenchmarkError("local command p95 exceeded 150 ms")
    if ratio > GO_TO_PYTHON_P95_LIMIT:
        raise BenchmarkError("Go fixture p95 exceeded the Python baseline by more than 1.15x")
    return {
        "schema_version": 1,
        "acceptance": "AC-122",
        "status": "passed",
        "git_sha": verified["git_commit"],
        "candidate_identity": verified["candidate_identity"],
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "scenario": "responsible-account-membership-local-fixture",
        "python": python_result,
        "go": go_result,
        "go_to_python_p95_ratio": round(ratio, 4),
        "thresholds": {
            "trials": TRIALS,
            "local_p95_ms": LOCAL_P95_LIMIT_MS,
            "go_to_python_p95_ratio": GO_TO_PYTHON_P95_LIMIT,
        },
        "request_budget": request_checks,
        "fixture_hash_unchanged": True,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate-dir", type=Path, required=True)
    parser.add_argument("--python", type=Path, default=Path(sys.executable))
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--trials", type=int, default=TRIALS)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        result = benchmark(
            candidate_dir=args.candidate_dir,
            python=args.python,
            expected_commit=args.expected_commit,
            trials=args.trials,
        )
    except (AcceptanceError, BenchmarkError, OSError, ValueError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_bytes(canonical_json(result))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
