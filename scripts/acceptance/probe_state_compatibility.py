#!/usr/bin/env python3
"""Probe the Python state contract without touching a user's real state.

The probe is deliberately a subprocess-based executable so it exercises the
same lock file from independent interpreters.  It is also used by the future
Go implementation as a black-box compatibility reference.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path

SOURCE_ROOT = Path(__file__).resolve().parents[2] / "skills" / "ads-plan-monitor" / "src"
if str(SOURCE_ROOT) not in sys.path:
    sys.path.insert(0, str(SOURCE_ROOT))

from ocean_watch.core.config_store import (  # noqa: E402
    atomic_write_json,
    json_file_lock,
    load_json,
    update_json,
)


def _child_hold(path: Path, seconds: float, ready: Path) -> int:
    with json_file_lock(path, lock_timeout=5):
        ready.write_text("ready\n", encoding="utf-8")
        time.sleep(seconds)
    return 0


def _child_increment(path: Path, count: int) -> int:
    for _ in range(count):
        def bump(payload):
            updated = dict(payload)
            updated["counter"] = int(updated.get("counter", 0)) + 1
            return updated, updated["counter"]

        update_json(path, bump, backup=False, lock_timeout=10)
    return 0


def _child_crash_before_replace(path: Path) -> int:
    """Crash at the replace boundary and leave the old target readable."""
    import tempfile as _tempfile

    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = _tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write('{"counter": 999}\n')
        handle.flush()
        os.fsync(handle.fileno())
    os._exit(73)  # noqa: PLW1510 - intentional fault injection


def _run_child(mode: str, path: Path, *args: str) -> subprocess.Popen:
    command = [sys.executable, __file__, "--child", mode, str(path), *args]
    return subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)


def run_probe(out: Path | None = None) -> dict:
    with tempfile.TemporaryDirectory(prefix="ocean-watch-state-probe-") as temporary:
        root = Path(temporary)
        state = root / "state.json"
        atomic_write_json(state, {"schema_version": 2, "counter": 0, "unknown": {"keep": True}}, backup=False)

        ready = root / "ready"
        holder = _run_child("hold", state, "0.8", str(ready))
        deadline = time.monotonic() + 5
        while not ready.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        if not ready.exists():
            holder.kill()
            raise RuntimeError("lock holder did not become ready")

        started = time.monotonic()
        timed_out = False
        try:
            with json_file_lock(state, lock_timeout=0.15):
                pass
        except TimeoutError:
            timed_out = True
        timeout_elapsed = time.monotonic() - started
        holder.wait(timeout=5)
        if holder.returncode != 0:
            raise RuntimeError(holder.stderr.read() if holder.stderr else "lock holder failed")

        workers = [_run_child("increment", state, "10") for _ in range(8)]
        worker_codes = [worker.wait(timeout=10) for worker in workers]
        final_state = load_json(state)

        crash = _run_child("crash", state)
        crash.wait(timeout=5)
        after_crash = load_json(state)
        abandoned_temps = sorted(path.name for path in root.glob(".state.json.*.tmp"))
        for temporary_path in root.glob(".state.json.*.tmp"):
            temporary_path.unlink(missing_ok=True)

        result = {
            "schema_version": 1,
            "platform": sys.platform,
            "python": sys.version.split()[0],
            "lock": {
                "same_path": str(state.name) + ".lock",
                "contender_timed_out": timed_out,
                "timeout_elapsed_seconds": round(timeout_elapsed, 3),
                "holder_exit_code": holder.returncode,
                "worker_exit_codes": worker_codes,
                "final_counter": final_state["counter"],
            },
            "atomic_write": {
                "unknown_field_preserved": final_state["unknown"] == {"keep": True},
                "target_survives_crash": after_crash == final_state,
                "crash_exit_code": crash.returncode,
                "abandoned_temp_count_before_cleanup": len(abandoned_temps),
            },
            "passed": (
                timed_out
                and holder.returncode == 0
                and all(code == 0 for code in worker_codes)
                and final_state["counter"] == 80
                and final_state["unknown"] == {"keep": True}
                and after_crash == final_state
                and crash.returncode == 73
            ),
        }
    if out:
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path)
    parser.add_argument("--child", choices=["hold", "increment", "crash"])
    parser.add_argument("child_path", nargs="?")
    parser.add_argument("child_arg", nargs="*")
    args = parser.parse_args(argv)
    if args.child:
        path = Path(args.child_path)
        if args.child == "hold":
            return _child_hold(path, float(args.child_arg[0]), Path(args.child_arg[1]))
        if args.child == "increment":
            return _child_increment(path, int(args.child_arg[0]))
        return _child_crash_before_replace(path)
    result = run_probe(args.out)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
