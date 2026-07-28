#!/usr/bin/env python3
"""Cross-build the P0 bootstrap for the five supported release targets."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MODULE = ROOT / "prototype" / "runtime-bootstrap"
TARGETS = (
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
)
TEST_PUBLIC_KEY = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
TEST_COMMIT = "a" * 40


def build_matrix(output: Path) -> dict:
    output.mkdir(parents=True, exist_ok=True)
    assets = []
    ldflags = " ".join(
        [
            "-s",
            "-w",
            "-X main.productVersion=0.0.0",
            "-X main.pluginVersion=0.0.0+codex.test",
            f"-X main.gitCommit={TEST_COMMIT}",
            f"-X main.trustedPublicKeyHex={TEST_PUBLIC_KEY}",
        ]
    )
    for goos, goarch in TARGETS:
        suffix = ".exe" if goos == "windows" else ""
        name = f"ocean-watch-bootstrap_{goos}_{goarch}{suffix}"
        destination = output / name
        environment = {
            **os.environ,
            "CGO_ENABLED": "0",
            "GOOS": goos,
            "GOARCH": goarch,
            "GOTOOLCHAIN": "go1.26.5",
        }
        completed = subprocess.run(
            [
                "go",
                "build",
                "-trimpath",
                "-ldflags",
                ldflags,
                "-o",
                str(destination),
                "./cmd/ocean-watch-bootstrap",
            ],
            cwd=MODULE,
            env=environment,
            capture_output=True,
            text=True,
            check=False,
        )
        if completed.returncode != 0:
            raise RuntimeError(f"{goos}-{goarch} build failed: {completed.stderr}")
        payload = destination.read_bytes()
        assets.append(
            {
                "platform": f"{goos}-{goarch}",
                "name": name,
                "size": len(payload),
                "sha256": hashlib.sha256(payload).hexdigest(),
            }
        )
    return {
        "schema_version": 1,
        "prototype": "runtime-bootstrap",
        "go_toolchain": "go1.26.5",
        "cgo_enabled": False,
        "targets": assets,
        "passed": len(assets) == len(TARGETS),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--out-dir",
        type=Path,
        default=ROOT / "artifacts" / "go-sdk-acceptance" / "p0" / "bootstrap",
    )
    parser.add_argument("--evidence", type=Path)
    args = parser.parse_args()
    result = build_matrix(args.out_dir)
    evidence = args.evidence or args.out_dir / "matrix.json"
    evidence.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
