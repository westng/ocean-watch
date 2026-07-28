import copy
import datetime as dt
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance.verify_gosec_report import code_fingerprint, verify_report

MODULE = "prototype/ocean-watch-go"
NOW = dt.datetime(2026, 7, 26, tzinfo=dt.timezone.utc)
CODE = "9: before\n10: dangerous()\n11: after\n"
FINGERPRINT = hashlib.sha256(b"before\ndangerous()\nafter\n").hexdigest()


def finding(**overrides):
    value = {
        "rule_id": "G204",
        "file": f"/checkout/{MODULE}/internal/example.go",
        "line": "10",
        "severity": "MEDIUM",
        "confidence": "HIGH",
        "code": CODE,
    }
    value.update(overrides)
    return value


def report(issues=None, **overrides):
    values = [finding()] if issues is None else issues
    value = {
        "GosecVersion": "dev",
        "Golang errors": {},
        "Issues": values,
        "Stats": {"files": 1, "lines": 11, "nosec": 0, "found": len(values)},
    }
    value.update(overrides)
    return value


def controls(**overrides):
    value = {
        "schema_version": 1,
        "kind": "gosec_control_inventory",
        "scanner_install_version": "v2.22.10",
        "does_not_grant_gate_signoff": True,
        "controls": [
            {
                "id": "GOSEC-TEST-G204",
                "module": MODULE,
                "rule_id": "G204",
                "severity": "MEDIUM",
                "confidence": "HIGH",
                "owner": "QO",
                "security_reviewer": "SO",
                "expires_at": "2026-10-31T23:59:59Z",
                "rationale": "Fixture process execution is isolated.",
                "controls": ["Executable is absolute.", "No shell is used."],
                "removal_condition": "Remove the fixture process.",
                "findings": [
                    {
                        "file": f"{MODULE}/internal/example.go",
                        "code_sha256": FINGERPRINT,
                        "occurrence": 1,
                    }
                ],
            }
        ],
    }
    value.update(overrides)
    return value


class GosecControlTests(unittest.TestCase):
    def write_controls(self, root, value):
        path = Path(root) / "controls.json"
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_code_fingerprint_ignores_report_line_numbers(self):
        self.assertEqual(code_fingerprint(CODE), FINGERPRINT)
        self.assertEqual(
            code_fingerprint("99: before\n100: dangerous()\n101: after\n"),
            FINGERPRINT,
        )

    def test_exact_documented_report_passes_without_granting_gate_signoff(self):
        with tempfile.TemporaryDirectory() as directory:
            result = verify_report(
                report(), self.write_controls(directory, controls()), MODULE, now=NOW
            )
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["documented_finding_count"], 1)
        self.assertFalse(result["gate_signoff_granted"])

    def test_new_and_stale_findings_both_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.write_controls(directory, controls())
            changed = finding(code="9: before\n10: changed()\n11: after\n")
            result = verify_report(report([changed]), path, MODULE, now=NOW)
        self.assertEqual(result["unregistered_finding_count"], 1)
        self.assertEqual(result["stale_finding_count"], 1)
        self.assertEqual(result["status"], "failed")

    def test_expired_unknown_or_high_controls_are_rejected(self):
        value = controls()
        value["controls"][0]["expires_at"] = "2026-07-25T23:59:59Z"
        value["controls"][0]["rule_id"] = "G999"
        high = finding(rule_id="G999", severity="HIGH")
        with tempfile.TemporaryDirectory() as directory:
            result = verify_report(
                report([high]), self.write_controls(directory, value), MODULE, now=NOW
            )
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["high_or_critical_count"], 1)
        self.assertTrue(any("unknown" in error for error in result["errors"]))
        self.assertTrue(any("unexpired" in error for error in result["errors"]))

    def test_scanner_errors_suppressions_and_count_mismatch_fail(self):
        broken = report()
        broken["Golang errors"] = {"package": "failed"}
        broken["Stats"] = {"files": 1, "lines": 11, "nosec": 1, "found": 2}
        with tempfile.TemporaryDirectory() as directory:
            result = verify_report(
                broken, self.write_controls(directory, controls()), MODULE, now=NOW
            )
        self.assertEqual(result["status"], "failed")
        self.assertTrue(any("analysis errors" in error for error in result["errors"]))
        self.assertTrue(any("suppressions" in error for error in result["errors"]))
        self.assertTrue(any("finding count" in error for error in result["errors"]))

    def test_duplicate_source_context_uses_explicit_occurrence(self):
        duplicate = finding(line="20")
        value = controls()
        second = copy.deepcopy(value["controls"][0]["findings"][0])
        second["occurrence"] = 2
        value["controls"][0]["findings"].append(second)
        with tempfile.TemporaryDirectory() as directory:
            result = verify_report(
                report([finding(), duplicate]),
                self.write_controls(directory, value),
                MODULE,
                now=NOW,
            )
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["finding_count"], 2)


if __name__ == "__main__":
    unittest.main()
