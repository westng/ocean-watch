import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PROBE = ROOT / "scripts" / "acceptance" / "probe_state_compatibility.py"


class StateCompatibilityTests(unittest.TestCase):
    def test_cross_process_lock_and_atomic_write_probe(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence = Path(temporary) / "state-probe.json"
            completed = subprocess.run(
                [sys.executable, str(PROBE), "--out", str(evidence)],
                cwd=ROOT,
                check=False,
                capture_output=True,
                text=True,
                timeout=30,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr + completed.stdout)
            result = json.loads(evidence.read_text(encoding="utf-8"))
            self.assertTrue(result["passed"])
            self.assertEqual(result["lock"]["final_counter"], 80)
            self.assertTrue(result["atomic_write"]["target_survives_crash"])


if __name__ == "__main__":
    unittest.main()
