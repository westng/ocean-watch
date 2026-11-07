#!/usr/bin/env python3
"""Report the repository-local V3 Alpha gate without claiming external acceptance."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def check(name: str, passed: bool, detail: str) -> dict[str, object]:
    return {"name": name, "status": "passed" if passed else "blocked", "detail": detail}


def local_checks() -> list[dict[str, object]]:
    checks: list[dict[str, object]] = []
    required = [
        ROOT / "runtime/ocean-watch-go/internal/adapters/sqlite/journal.go",
        ROOT / "runtime/ocean-watch-go/internal/bootstrap/qianchuan.go",
        ROOT / "docs/ai/plans/v3-architecture-remediation-plan.md",
        ROOT / ".codex-plugin/runtime-manifest.json",
    ]
    checks.append(
        check(
            "repository_artifacts",
            all(path.is_file() for path in required),
            "SQLite journal bridge, bootstrap wiring, V3 remediation plan, and runtime manifest are present",
        )
    )

    plugin = json.loads((ROOT / ".codex-plugin/plugin.json").read_text(encoding="utf-8"))
    claude = json.loads((ROOT / ".claude-plugin/plugin.json").read_text(encoding="utf-8"))
    package = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))
    base_version = str(plugin.get("version", "")).split("+", 1)[0]
    versions_match = (
        plugin.get("version") == claude.get("version")
        and package.get("version") == base_version
    )
    checks.append(check("version_contract", versions_match, "Codex/Claude/Harness version fields agree"))

    bootstrap = (ROOT / "runtime/ocean-watch-go/internal/bootstrap/qianchuan.go").read_text(encoding="utf-8")
    sqlite_wired = "sqlite.SQLiteJournalStore" in bootstrap and "ocean-watch.db" in bootstrap
    checks.append(check("sqlite_preflight_wiring", sqlite_wired, "Qianchuan preflight defaults to SQLite state"))

    manifest = json.loads((ROOT / ".codex-plugin/runtime-manifest.json").read_text(encoding="utf-8"))
    binaries = manifest.get("sha256", {})
    checks.append(check("runtime_manifest", len(binaries) == 5 and all(binaries.values()), "five packaged Runtime hashes are recorded"))
    return checks


def external_checks() -> list[dict[str, object]]:
    return [
        check("github_ci", False, "requires a pushed release-candidate commit and green CI for its exact SHA"),
        check("host_acceptance", False, "requires new-task loading evidence for Codex, Claude, Harness, Doubao, and CLI"),
        check("real_business_acceptance", False, "requires the user-provided 25-link read-only Qianchuan acceptance"),
        check("five_platform_sqlite", False, "requires actual engine/concurrency/crash/backup evidence on the supported platform matrix"),
    ]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true", help="emit machine-readable results")
    parser.add_argument("--strict", action="store_true", help="fail unless external acceptance evidence is available")
    args = parser.parse_args()

    result = {"local": local_checks(), "external": external_checks()}
    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        for section, checks in result.items():
            print(f"{section}:")
            for item in checks:
                print(f"  [{item['status'].upper()}] {item['name']}: {item['detail']}")
    if any(item["status"] != "passed" for item in result["local"]):
        return 1
    if args.strict and any(item["status"] != "passed" for item in result["external"]):
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
