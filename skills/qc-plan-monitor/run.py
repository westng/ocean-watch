#!/usr/bin/env python3
from pathlib import Path
import sys


PLUGIN_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(PLUGIN_ROOT / "scripts"))

from runtime_launcher import launch  # noqa: E402


if __name__ == "__main__":
    raise SystemExit(launch(PLUGIN_ROOT))
