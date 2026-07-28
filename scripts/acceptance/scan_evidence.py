#!/usr/bin/env python3
"""Fail closed when acceptance evidence contains credential-like material."""

from __future__ import annotations

import argparse
import re
from pathlib import Path

ALLOWLIST = {
    "TEST_ACCESS_TOKEN_DO_NOT_USE",
    "TEST_REFRESH_TOKEN_DO_NOT_USE",
    "TEST_APP_SECRET_DO_NOT_USE",
    "TEST_AUTH_CODE_DO_NOT_USE",
}
ALLOWED_PUBLIC_URLS = {
    "https://open.oceanengine.com/qianchuan/mcp",
}
SECRET_VALUE = r"[A-Za-z0-9._~+/=-]{12,}"
PATTERNS = (
    re.compile(rf"(?i)bearer\s+{SECRET_VALUE}"),
    re.compile(rf"(?i)(?:access|refresh)[_-]?token[\"']?\s*[:=]\s*[\"']?({SECRET_VALUE})"),
    re.compile(rf"(?i)(?:app[_-]?secret|client[_-]?secret|auth[_-]?code)[\"']?\s*[:=]\s*[\"']?({SECRET_VALUE})"),
    re.compile(r"https?://[^\s\"']*(?:mcp|streamable)[^\s\"']*", re.IGNORECASE),
)


def scan(root: Path) -> list[str]:
    findings: list[str] = []
    paths = [root] if root.is_file() else sorted(path for path in root.rglob("*") if path.is_file())
    for path in paths:
        try:
            content = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        for line_number, line in enumerate(content.splitlines(), start=1):
            for pattern in PATTERNS:
                for match in pattern.finditer(line):
                    value = match.group(0)
                    if any(allowed in value for allowed in ALLOWLIST):
                        continue
                    captured = match.group(1) if match.lastindex else ""
                    if captured.lower() in {
                        "access_token",
                        "refresh_token",
                        "app_secret",
                        "client_secret",
                        "auth_code",
                        "self.access_token",
                        "self.refresh_token",
                    }:
                        continue
                    if value.rstrip("`.,);]") in ALLOWED_PUBLIC_URLS:
                        continue
                    findings.append(f"{path}:{line_number}:{pattern.pattern}")
    return findings


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("path", type=Path)
    args = parser.parse_args()
    findings = scan(args.path)
    if findings:
        print("\n".join(findings))
        return 1
    print(f"no credential-like evidence found under {args.path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
