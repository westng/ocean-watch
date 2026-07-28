#!/usr/bin/env python3
"""Validate repository-local Markdown links used by migration documentation."""

from __future__ import annotations

import argparse
import re
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[2]
LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")


def check(paths: list[Path]) -> list[str]:
    errors: list[str] = []
    for path in paths:
        content = path.read_text(encoding="utf-8")
        for line_number, line in enumerate(content.splitlines(), start=1):
            for match in LINK.finditer(line):
                target = match.group(1).strip().strip("<>").split("#", 1)[0]
                target = target.split(" ", 1)[0]
                if not target or target.startswith(("http://", "https://", "mailto:", "codex://")):
                    continue
                resolved = (path.parent / unquote(target)).resolve()
                if not resolved.exists():
                    errors.append(f"{path.relative_to(ROOT)}:{line_number}: missing {target}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("paths", nargs="*", type=Path)
    args = parser.parse_args()
    paths = args.paths or sorted((ROOT / "docs").rglob("*.md"))
    failures = check(paths)
    if failures:
        print("\n".join(failures))
        return 1
    print(f"validated {len(paths)} Markdown files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
