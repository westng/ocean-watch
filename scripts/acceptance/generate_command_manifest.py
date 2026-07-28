#!/usr/bin/env python3
"""Capture the current Python CLI command/help contract for migration tests."""

from __future__ import annotations

import hashlib
import os
import subprocess
import sys
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError as error:  # pragma: no cover - setup failure
    raise SystemExit("PyYAML is required; install the project dev dependencies first") from error


ROOT = Path(__file__).resolve().parents[2]
SOURCE_ROOT = ROOT / "skills" / "ads-plan-monitor" / "src"
RUNNER = ROOT / "skills" / "ads-plan-monitor" / "run.py"
OUTPUT = ROOT / "contracts" / "commands.yaml"


def normalized(text: str) -> str:
    return text.replace("\r\n", "\n").replace("\r", "\n")


def capture(argv: list[str]) -> dict[str, object]:
    env = os.environ.copy()
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    env["PYTHONPATH"] = os.pathsep.join(
        value for value in (str(SOURCE_ROOT), env.get("PYTHONPATH")) if value
    )
    process = subprocess.run(
        [sys.executable, str(RUNNER), *argv],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    stdout = normalized(process.stdout)
    stderr = normalized(process.stderr)
    help_lines = [line.rstrip() for line in stdout.splitlines() if line.lstrip().startswith("--")]
    return {
        "argv": argv,
        "exit_code": process.returncode,
        "stdout": stdout,
        "stderr": stderr,
        "parameter_lines": help_lines,
        "stdout_sha256": hashlib.sha256(stdout.encode("utf-8")).hexdigest(),
        "stderr_sha256": hashlib.sha256(stderr.encode("utf-8")).hexdigest(),
    }


def git_commit() -> str:
    process = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=True
    )
    return process.stdout.strip()


def main() -> int:
    from ocean_watch.cli import main as cli

    commands = []
    for (domain, action), (_, prefix, description) in cli.COMMANDS.items():
        captured = capture([domain, action, "--help"])
        commands.append(
            {
                "domain": domain,
                "action": action,
                "command": f"{domain} {action}",
                "description": description,
                "dispatch_prefix": list(prefix),
                "help": captured,
            }
        )

    global_help = capture(["--help"])
    manifest = {
        "schema_version": 1,
        "source": "skills/ads-plan-monitor/src/ocean_watch/cli/main.py",
        "source_commit": git_commit(),
        "command_count": len(commands),
        "global_help": global_help,
        "exit_codes": {
            "success": 0,
            "operation_error": 1,
            "configuration_or_input_error": 2,
            "interrupted": 130,
        },
        "commands": commands,
    }
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(
        yaml.safe_dump(manifest, allow_unicode=True, sort_keys=False, width=120),
        encoding="utf-8",
    )
    print(f"wrote {OUTPUT} ({len(commands)} commands, source {manifest['source_commit']})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
