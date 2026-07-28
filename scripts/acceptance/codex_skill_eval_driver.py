#!/usr/bin/env python3
"""Codex-backed driver for the offline Skill semantic evaluation protocol."""

from __future__ import annotations

import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
MODEL_SCHEMA = ROOT / "contracts" / "skill-evals" / "model-output.schema.json"
TOOL_FIXTURES = ROOT / "contracts" / "skill-evals" / "tool-fixtures.json"


def _codex_version() -> str:
    completed = subprocess.run(
        ["codex", "--version"], capture_output=True, text=True, check=False, timeout=10
    )
    return (completed.stdout or completed.stderr).strip() or "unknown"


def _event_summary(stdout: str) -> list[dict[str, Any]]:
    summaries: list[dict[str, Any]] = []
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        summary: dict[str, Any] = {"type": event.get("type", "unknown")}
        item = event.get("item")
        if isinstance(item, dict):
            summary["item_type"] = item.get("type")
            if isinstance(item.get("command"), str):
                summary["command"] = item["command"]
            if item.get("exit_code") is not None:
                summary["exit_code"] = item["exit_code"]
            if isinstance(item.get("status"), str):
                summary["status"] = item["status"]
        summaries.append(summary)
    return summaries[-100:]


def _prompt(payload: dict[str, Any]) -> str:
    turns = json.dumps(payload["case"]["turns"], ensure_ascii=False, indent=2)
    return f"""You are evaluating the checked-out Ocean Watch Codex Skills in a sealed fixture directory.

Read both `skills/ads-plan-monitor/SKILL.md` and `skills/qc-plan-monitor/SKILL.md`. Interpret the full conversation semantically. Examples in a Skill are illustrative, never an exhaustive keyword list. Do not require a canonical user phrase.

Conversation turns:
{turns}

Select exactly one Skill and one local CLI command that the current Plugin should use now. Return the command as only `domain action`, without executable names or flags. Put a channel filter in `channel`: marketing, qianchuan, or any.

This is an offline read-only evaluation. Do not access a real config, credentials, network, or business API, and do not execute `ocean-watch` or `run.py`. `tool-fixtures.json` contains synthetic results keyed by command. After selecting the command, use only its matching fixture to construct the response. When that fixture has `presentation.required=true`, set source to `rendered_markdown` and copy its `rendered_markdown` byte-for-byte into both the presentation and `assistant_response`. When it is false, set source to `none`, rendered_markdown to an empty string, and provide only a concise response suitable for the chosen route.

Return only the JSON required by the supplied output schema."""


def run(payload: dict[str, Any]) -> dict[str, Any]:
    if "expected" in payload.get("case", {}):
        raise ValueError("evaluation driver must not receive expected answers")
    model = payload.get("model")
    if not isinstance(model, str) or not model or model == "unspecified":
        raise ValueError("a fixed --model is required for Codex model evaluation")
    with tempfile.TemporaryDirectory(prefix="ocean-watch-codex-eval-") as temporary:
        workspace = Path(temporary)
        for skill_name in ("ads-plan-monitor", "qc-plan-monitor"):
            destination = workspace / "skills" / skill_name
            destination.mkdir(parents=True)
            shutil.copy2(ROOT / "skills" / skill_name / "SKILL.md", destination / "SKILL.md")
        shutil.copy2(TOOL_FIXTURES, workspace / "tool-fixtures.json")
        schema = workspace / "model-output.schema.json"
        shutil.copy2(MODEL_SCHEMA, schema)
        output = workspace / "final.json"
        command = [
            "codex",
            "-a",
            "never",
            "exec",
            "--ephemeral",
            "--json",
            "--sandbox",
            "read-only",
            "--skip-git-repo-check",
            "--model",
            model,
            "--cd",
            str(workspace),
            "--output-schema",
            str(schema),
            "--output-last-message",
            str(output),
        ]
        if os.environ.get("OCEAN_WATCH_EVAL_USE_USER_CONFIG") != "1":
            command.insert(9, "--ignore-user-config")
        reasoning = payload.get("reasoning")
        if isinstance(reasoning, str) and reasoning not in {"", "unspecified"}:
            command.extend(["--config", f'model_reasoning_effort="{reasoning}"'])
        command.append(_prompt(payload))
        process = subprocess.Popen(
            command,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            cwd=workspace,
            start_new_session=True,
        )
        timeout = int(os.environ.get("OCEAN_WATCH_CODEX_TIMEOUT_SECONDS", "180"))
        try:
            stdout, stderr = process.communicate(timeout=timeout)
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
            raise TimeoutError(f"codex exec exceeded {timeout} seconds") from None
        if process.returncode != 0:
            raise RuntimeError(
                f"codex exec failed ({process.returncode}): {stderr[-2000:]}"
            )
        model_result = json.loads(output.read_text(encoding="utf-8"))
        return {
            "model": model,
            "codex_version": _codex_version(),
            "plugin_version": payload.get("plugin_version", "unknown"),
            "config_mode": "user-provider" if os.environ.get("OCEAN_WATCH_EVAL_USE_USER_CONFIG") == "1" else "isolated-default",
            "tool_calls": model_result["tool_calls"],
            "assistant_response": model_result["assistant_response"],
            "presentation": model_result["presentation"],
            "codex_events": _event_summary(stdout),
        }


def main() -> int:
    payload = json.load(sys.stdin)
    result = run(payload)
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
