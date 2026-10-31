import json
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class AlphaGateTests(unittest.TestCase):
    def test_local_gate_passes_and_external_gate_is_explicit(self):
        result = subprocess.run(
            [sys.executable, "scripts/alpha_gate.py", "--json"],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        payload = json.loads(result.stdout)
        self.assertTrue(all(item["status"] == "passed" for item in payload["local"]))
        self.assertTrue(all(item["status"] == "blocked" for item in payload["external"]))

    def test_strict_gate_does_not_claim_external_acceptance(self):
        result = subprocess.run(
            [sys.executable, "scripts/alpha_gate.py", "--strict"],
            cwd=ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 2)


if __name__ == "__main__":
    unittest.main()
