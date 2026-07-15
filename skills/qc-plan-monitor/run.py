#!/usr/bin/env python3
import importlib
import sys
from pathlib import Path

SOURCE_ROOT = Path(__file__).resolve().parents[1] / "ads-plan-monitor" / "src"
sys.path.insert(0, str(SOURCE_ROOT))

main = importlib.import_module("ocean_watch.cli.main").main


if __name__ == "__main__":
    raise SystemExit(main())
