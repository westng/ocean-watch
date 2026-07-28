import copy
import json
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance.ac import (
    evaluate_external_requirement,
    evaluate_required_evidence,
    load_manifest,
    validate_manifest,
)

COMMIT = "a" * 40
PLATFORM = "linux-amd64"


class AcceptanceRunnerEvidenceTests(unittest.TestCase):
    def write(self, root: Path, relative: str, value: dict) -> Path:
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_manifest_declares_commit_bound_contract_and_p5_evidence(self):
        manifest = load_manifest()
        self.assertEqual(validate_manifest(manifest), [])
        for acceptance_id in ("AC-101", "AC-102", "AC-105"):
            requirement = manifest["acceptance"][acceptance_id]["required_evidence"][0]
            self.assertEqual(requirement["kind"], "contract_report")
        for acceptance_id in ("AC-124", "AC-125", "AC-126", "AC-128"):
            requirement = manifest["acceptance"][acceptance_id]["required_evidence"][0]
            self.assertEqual(requirement["kind"], "p5_acceptance")

    def test_manifest_rejects_duplicates_within_one_test_group(self):
        manifest = copy.deepcopy(load_manifest())
        names = manifest["acceptance"]["AC-101"]["tests"]["normal"]
        names.append(names[0])
        self.assertIn(
            "AC-101.tests.normal must not repeat test names",
            validate_manifest(manifest),
        )

    def test_missing_required_evidence_blocks_but_stale_evidence_fails(self):
        requirement = {
            "id": "contract",
            "kind": "contract_report",
            "evidence": "contracts/go/report.json",
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            missing = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-101"
            )
            self.assertEqual(missing["status"], "blocked")
            self.write(
                root,
                requirement["evidence"],
                {
                    "git_sha": "b" * 40,
                    "platform": PLATFORM,
                    "kind": "contract-comparison",
                    "total": 1,
                    "passed": 1,
                    "failed": 0,
                    "cases": [{"passed": True, "differences": []}],
                },
            )
            stale = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-101"
            )
            self.assertEqual(stale["status"], "failed")

    def test_contract_report_must_be_complete_and_difference_free(self):
        requirement = {
            "id": "contract",
            "kind": "contract_report",
            "evidence": "report.json",
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            report = {
                "git_sha": COMMIT,
                "platform": PLATFORM,
                "kind": "contract-comparison",
                "total": 2,
                "passed": 2,
                "failed": 0,
                "cases": [
                    {"passed": True, "differences": []},
                    {"passed": True, "differences": []},
                ],
            }
            self.write(root, "report.json", report)
            result = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-101"
            )
            self.assertEqual(result["status"], "passed")
            report["cases"][1] = {
                "passed": False,
                "differences": [{"field": "presentation"}],
            }
            self.write(root, "report.json", report)
            result = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-101"
            )
            self.assertEqual(result["status"], "failed")

    def test_p5_evidence_enforces_acceptance_platform_and_required_values(self):
        requirement = {
            "id": "launcher",
            "kind": "p5_acceptance",
            "evidence": "release/ac-124-platform.json",
            "platform_bound": True,
            "required_values": {"offline_verified_cache": True},
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evidence = {
                "schema_version": 1,
                "acceptance": "AC-124",
                "status": "passed",
                "git_commit": COMMIT,
                "platform": PLATFORM,
                "offline_verified_cache": True,
            }
            self.write(root, requirement["evidence"], evidence)
            result = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-124"
            )
            self.assertEqual(result["status"], "passed")
            evidence["platform"] = "darwin-arm64"
            self.write(root, requirement["evidence"], evidence)
            result = evaluate_required_evidence(
                requirement, root, COMMIT, PLATFORM, "AC-124"
            )
            self.assertEqual(result["status"], "failed")

    def test_external_evidence_wrong_sha_is_failed_not_pending(self):
        requirement = {
            "id": "matrix",
            "kind": "native_platform_matrix",
            "evidence": "matrix.json",
            "required_platforms": [PLATFORM],
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.write(
                root,
                "matrix.json",
                {
                    "git_sha": "b" * 40,
                    "platforms": [{"platform": PLATFORM, "status": "passed"}],
                },
            )
            result = evaluate_external_requirement(requirement, root, COMMIT)
            self.assertEqual(result["status"], "failed")

    def test_external_native_matrix_distinguishes_failure_from_incomplete(self):
        requirement = {
            "id": "matrix",
            "kind": "native_platform_matrix",
            "evidence": "matrix.json",
            "required_platforms": [PLATFORM, "darwin-arm64"],
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.write(
                root,
                "matrix.json",
                {
                    "git_sha": COMMIT,
                    "platforms": [
                        {"platform": PLATFORM, "status": "failed"},
                        {"platform": "darwin-arm64", "status": "not_run"},
                    ],
                },
            )
            result = evaluate_external_requirement(requirement, root, COMMIT)
            self.assertEqual(result["status"], "failed")
            self.assertTrue(any("linux-amd64" in error for error in result["errors"]))

            self.write(
                root,
                "matrix.json",
                {
                    "git_sha": COMMIT,
                    "platforms": [{"platform": PLATFORM, "status": "passed"}],
                },
            )
            result = evaluate_external_requirement(requirement, root, COMMIT)
            self.assertEqual(result["status"], "blocked")

    def test_external_rollout_failure_is_not_reported_as_pending(self):
        requirement = {
            "id": "rollout",
            "kind": "rollout",
            "evidence": "rollout.json",
            "required_cohorts": ["internal", "invited"],
            "minimum_released_versions": 2,
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.write(
                root,
                "rollout.json",
                {
                    "git_sha": COMMIT,
                    "status": "failed",
                    "cohorts": [
                        {"id": "internal", "status": "passed"},
                        {"id": "invited", "status": "failed"},
                    ],
                    "released_versions": 1,
                },
            )
            result = evaluate_external_requirement(requirement, root, COMMIT)
            self.assertEqual(result["status"], "failed")
            self.assertTrue(any("invited" in error for error in result["errors"]))


if __name__ == "__main__":
    unittest.main()
