#!/usr/bin/env python3
"""Run one native acceptance shard against an already-built signed candidate."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any

try:
    from . import ac, p5
    from .candidate_identity import canonical_json, validate_candidate_identity
    from .scan_evidence import scan
except ImportError:
    import ac  # type: ignore[no-redef]
    import p5  # type: ignore[no-redef]
    from candidate_identity import (  # type: ignore[no-redef]
        canonical_json,
        validate_candidate_identity,
    )
    from scan_evidence import scan  # type: ignore[no-redef]

ROOT = Path(__file__).resolve().parents[2]
GO_MODULE = ROOT / "prototype" / "ocean-watch-go"
COMMAND_MANIFEST = ROOT / "contracts" / "commands.yaml"
FIXTURES = ROOT / "testdata" / "contracts" / "python"
GO_TOOLCHAIN = "go1.26.5"


class NativeCandidateError(RuntimeError):
    pass


def _prepare_output(path: Path) -> Path:
    resolved = path.resolve()
    if resolved in {ROOT, ROOT.parent, Path.home().resolve()} or len(resolved.parts) < 4:
        raise NativeCandidateError("refusing unsafe native evidence output path")
    if resolved.exists():
        shutil.rmtree(resolved)
    resolved.mkdir(parents=True, mode=0o700)
    return resolved


def _run(command: list[str], label: str) -> None:
    environment = {
        **os.environ,
        "GOTOOLCHAIN": GO_TOOLCHAIN,
        "PYTHONDONTWRITEBYTECODE": "1",
        "TZ": "UTC",
    }
    try:
        completed = subprocess.run(
            command,
            cwd=ROOT,
            env=environment,
            capture_output=True,
            text=True,
            check=False,
            timeout=1800,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise NativeCandidateError(f"{label} could not complete") from error
    if completed.returncode != 0:
        raise NativeCandidateError(
            f"{label} failed with exit code {completed.returncode}"
        )


def _contract_command(
    action: str,
    *,
    python: Path,
    output: Path,
    git_sha: str,
    baseline: Path | None = None,
    candidate: Path | None = None,
    candidate_identity: Path | None = None,
) -> list[str]:
    command = [
        "go",
        "-C",
        str(GO_MODULE),
        "run",
        "./cmd/contract-runner",
        action,
        "--manifest",
        str(COMMAND_MANIFEST),
        "--out",
        str(output),
        "--git-sha",
        git_sha,
        "--python",
        str(python),
    ]
    if action == "capture-python":
        command.extend(["--fixtures", str(FIXTURES)])
    elif action == "compare":
        if baseline is None or candidate is None or candidate_identity is None:
            raise NativeCandidateError("contract comparison inputs are incomplete")
        command.extend(
            [
                "--baseline",
                str(baseline),
                "--candidate",
                str(candidate),
                "--candidate-identity",
                str(candidate_identity),
            ]
        )
    else:
        raise NativeCandidateError(f"unsupported contract action: {action}")
    return command


def run_native_candidate(
    *,
    candidate_dir: Path,
    out_dir: Path,
    python: Path,
    expected_platform: str | None = None,
    expected_commit: str | None = None,
    require_release: bool = False,
) -> dict[str, Any]:
    checkout_commit = ac.git_sha()
    if ac.git_dirty():
        raise NativeCandidateError(
            "native candidate evidence requires a clean source checkout"
        )
    verified = p5.verify_candidate(
        candidate_dir,
        verify_signatures=True,
        require_release=require_release,
        expected_commit=expected_commit or checkout_commit,
    )
    identity = verified.get("candidate_identity")
    identity_errors = validate_candidate_identity(
        identity,
        expected_git_sha=expected_commit or checkout_commit,
        require_release=require_release,
    )
    if identity_errors:
        raise NativeCandidateError(
            "downloaded candidate identity is invalid: " + "; ".join(identity_errors)
        )
    if verified.get("git_commit") != checkout_commit:
        raise NativeCandidateError(
            "downloaded candidate does not match the checked-out source commit"
        )
    goos, goarch = p5.current_target()
    platform_id = f"{goos}-{goarch}"
    if expected_platform and expected_platform != platform_id:
        raise NativeCandidateError(
            f"native runner platform mismatch: expected {expected_platform}, got {platform_id}"
        )
    _, _, acceptance_platform = ac.native_platform()
    if acceptance_platform != platform_id:
        raise NativeCandidateError("P5 and AC native platform identities disagree")
    candidate_binary = candidate_dir.resolve() / p5.target_name(
        "ocean-watch", goos, goarch
    )
    if not candidate_binary.is_file() or candidate_binary.is_symlink():
        raise NativeCandidateError(
            f"downloaded candidate is missing the native runtime: {candidate_binary.name}"
        )
    if os.name != "nt":
        candidate_binary.chmod(0o700)

    output = _prepare_output(out_dir)
    identity_path = output / "candidate-identity.json"
    identity_path.write_bytes(canonical_json(identity))
    contracts = output / "contracts"
    baseline = contracts / "python"
    comparison = contracts / "go"
    _run(
        _contract_command(
            "capture-python",
            python=python,
            output=baseline,
            git_sha=checkout_commit,
        ),
        "Python contract capture",
    )
    _run(
        _contract_command(
            "compare",
            python=python,
            output=comparison,
            git_sha=checkout_commit,
            baseline=baseline,
            candidate=candidate_binary,
            candidate_identity=identity_path,
        ),
        "native candidate contract comparison",
    )

    p5.launcher_acceptance(
        candidate_dir,
        output / "release" / "ac-124-platform.json",
        platform_id,
        require_release=require_release,
        expected_commit=expected_commit or checkout_commit,
    )
    p5.upgrade_rollback_acceptance(
        candidate_dir,
        output / "release" / "ac-126-upgrade-rollback.json",
        require_release=require_release,
        expected_commit=expected_commit or checkout_commit,
    )
    p5.user_journey_acceptance(
        candidate_dir,
        output / "contracts" / "ac-128-user-journeys.json",
        require_release=require_release,
        expected_commit=expected_commit or checkout_commit,
    )
    runner_summary, exit_code = ac.run_acceptance(
        output,
        external_root=output,
        execute=True,
        commit=checkout_commit,
        dirty=False,
        candidate_identity=identity,
    )
    if exit_code != 0 or runner_summary.get("status") == "failed":
        raise NativeCandidateError("native AC runner contains failed or not-run checks")
    findings = scan(output)
    if findings:
        raise NativeCandidateError("native candidate evidence failed redaction scanning")
    result = {
        "schema_version": 1,
        "suite": "native-candidate",
        "status": runner_summary["status"],
        "git_sha": checkout_commit,
        "platform": platform_id,
        "candidate_identity": identity,
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "signatures_verified": verified["signatures_verified"],
        "evidence": {
            "contract_report": "contracts/go/report.json",
            "launcher": "release/ac-124-platform.json",
            "upgrade_rollback": "release/ac-126-upgrade-rollback.json",
            "user_journeys": "contracts/ac-128-user-journeys.json",
            "runner_summary": "runner-summary.json",
        },
    }
    (output / "native-candidate-summary.json").write_bytes(canonical_json(result))
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate-dir", type=Path, required=True)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--python", type=Path, default=Path(sys.executable))
    parser.add_argument("--expected-platform")
    parser.add_argument("--expected-commit")
    parser.add_argument("--require-release", action="store_true")
    return parser


def main() -> int:
    args = build_parser().parse_args()
    try:
        result = run_native_candidate(
            candidate_dir=args.candidate_dir,
            out_dir=args.out_dir,
            python=args.python,
            expected_platform=args.expected_platform,
            expected_commit=args.expected_commit,
            require_release=args.require_release,
        )
    except (NativeCandidateError, p5.AcceptanceError, OSError, ValueError) as error:
        print(
            json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False),
            file=sys.stderr,
        )
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
