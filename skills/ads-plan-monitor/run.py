#!/usr/bin/env python3
from pathlib import Path
import sys


SOURCE_ROOT = Path(__file__).resolve().parent / "src"
sys.path.insert(0, str(SOURCE_ROOT))

from ocean_watch.cli.main import main  # noqa: E402


if __name__ == "__main__":
    raise SystemExit(main())
