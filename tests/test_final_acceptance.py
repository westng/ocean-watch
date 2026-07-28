import copy
import datetime as dt
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance import g5_source_policy, source_runs
from scripts.acceptance.ac import (
    MANIFEST_PATH,
    canonical_json,
    load_manifest,
    sha256_file,
)
from scripts.acceptance.build_final_summary import FinalSummaryError, build_summary
from scripts.acceptance.candidate_identity import candidate_identity_sha256
from scripts.acceptance.verify_gate_signoff import verify

COMMIT = "a" * 40
NOW = dt.datetime(2026, 7, 26, 12, 0, tzinfo=dt.timezone.utc)
CANDIDATE_IDENTITY = {
    "schema_version": 1,
    "git_sha": COMMIT,
    "product_version": "0.9.1",
    "plugin_version": "0.9.1+codex.test",
    "sdk_version": "v1.1.92",
    "source_tree_sha256": "1" * 64,
    "candidate_checksums_sha256": "2" * 64,
    "release_public_key_sha256": "3" * 64,
    "release": True,
}
SOURCE_RUNS = source_runs.build(
    COMMIT,
    "westng/ocean-watch",
    {
        key: {
            "run_id": index + 1,
            "workflow_path": g5_source_policy.expected_workflow_path(key),
            "head_sha": COMMIT,
            "run_attempt": 1,
        }
        for index, key in enumerate(source_runs.RUN_KEYS)
    },
)


class FinalAcceptanceTests(unittest.TestCase):
    def write(self, path: Path, value: dict) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(canonical_json(value))

    def build_g5_summary(self, **kwargs) -> dict:
        return build_summary(source_runs=SOURCE_RUNS, **kwargs)

    def external_evidence(self, requirement: dict) -> dict:
        kind = requirement.get("kind")
        result = {
            "schema_version": 1,
            "git_sha": COMMIT,
            "status": "passed",
            "candidate_identity": CANDIDATE_IDENTITY,
        }
        if kind == "model_eval":
            minimum = int(requirement.get("minimum_trials", 1))
            result.update(
                trials=minimum,
                summary={
                    "total": minimum,
                    "passed": minimum,
                    "failed": 0,
                    "blocked": 0,
                    "not_run": 0,
                },
            )
        elif kind == "native_platform_matrix":
            result["platforms"] = [
                {"platform": platform, "status": "passed"}
                for platform in requirement["required_platforms"]
            ]
        elif kind == "rollout":
            result.update(
                cohorts=[
                    {"id": cohort, "status": "passed"}
                    for cohort in requirement["required_cohorts"]
                ],
                released_versions=requirement["minimum_released_versions"],
            )
        return result

    def required_evidence(
        self,
        acceptance_id: str,
        requirement: dict,
        platform: str,
    ) -> dict:
        if requirement["kind"] == "contract_report":
            return {
                "schema_version": 1,
                "kind": "contract-comparison",
                "git_sha": COMMIT,
                "platform": platform,
                "candidate_identity": CANDIDATE_IDENTITY,
                "total": 1,
                "passed": 1,
                "failed": 0,
                "cases": [{"id": "fixture", "passed": True, "differences": []}],
            }
        value = {
            "schema_version": 1,
            "acceptance": acceptance_id,
            "git_commit": COMMIT,
            "platform": platform,
            "status": "passed",
            "candidate_identity": CANDIDATE_IDENTITY,
        }
        value.update(copy.deepcopy(requirement.get("required_values", {})))
        return value

    def create_complete_evidence(self, root: Path) -> tuple[dict, Path, Path]:
        manifest = load_manifest()
        shard_root = root / "native"
        manifest_digest = sha256_file(MANIFEST_PATH)
        for platform in manifest["required_platforms"]:
            platform_root = shard_root / platform
            self.write(
                platform_root / "environment.json",
                {
                    "schema_version": 1,
                    "git_sha": COMMIT,
                    "platform": platform,
                    "working_tree_dirty": False,
                    "manifest_sha256": manifest_digest,
                    "sdk_version": manifest["sdk_version"],
                    "candidate_identity": CANDIDATE_IDENTITY,
                },
            )
            result_index = []
            for acceptance_id, contract in manifest["acceptance"].items():
                tests = []
                for group, names in contract.get("tests", {}).items():
                    if group == "race" and platform.startswith("windows-"):
                        continue
                    tests.extend(
                        {"name": name, "group": group, "status": "passed", "matches": []}
                        for name in names
                    )
                result = {
                    "schema_version": 1,
                    "acceptance_id": acceptance_id,
                    "git_sha": COMMIT,
                    "platform": platform,
                    "working_tree_dirty": False,
                    "status": "passed",
                    "blocking": False,
                    "tests": tests,
                    "required_evidence": [],
                    "external_requirements": [],
                    "candidate_identity": CANDIDATE_IDENTITY,
                }
                self.write(
                    platform_root / "ac-results" / f"{acceptance_id.lower()}.json",
                    result,
                )
                result_index.append(
                    {
                        "acceptance_id": acceptance_id,
                        "status": "passed",
                        "path": f"ac-results/{acceptance_id.lower()}.json",
                    }
                )
                for requirement in contract.get("required_evidence", []):
                    if requirement.get("platform_bound"):
                        self.write(
                            platform_root / requirement["evidence"],
                            self.required_evidence(
                                acceptance_id,
                                requirement,
                                platform,
                            ),
                        )
            self.write(
                platform_root / "runner-summary.json",
                {
                    "schema_version": 1,
                    "suite": "ac-101-ac-128",
                    "git_sha": COMMIT,
                    "platform": platform,
                    "working_tree_dirty": False,
                    "status": "blocked",
                    "runner_errors": [],
                    "candidate_identity": CANDIDATE_IDENTITY,
                    "results": result_index,
                },
            )
        for acceptance_id, contract in manifest["acceptance"].items():
            for requirement in contract.get("required_evidence", []):
                if not requirement.get("platform_bound"):
                    self.write(
                        root / requirement["evidence"],
                        self.required_evidence(
                            acceptance_id,
                            requirement,
                            manifest["required_platforms"][0],
                        ),
                    )
            for requirement in contract.get("external_requirements", []):
                self.write(
                    root / requirement["evidence"],
                    self.external_evidence(requirement),
                )
        return manifest, shard_root, root

    def test_complete_five_platform_evidence_builds_deterministic_ready_summary(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest, shard_root, external_root = self.create_complete_evidence(root)
            first = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            second = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            self.assertTrue(first["ready"])
            self.assertEqual(first["status"], "passed")
            self.assertEqual(first["counts"]["passed"], 28)
            for key in ("failed", "blocked", "missing", "not_run", "blocking", "expired_exceptions"):
                self.assertEqual(first["counts"][key], 0)
            self.assertEqual(first["required_platforms"], manifest["required_platforms"])
            self.assertEqual(canonical_json(first), canonical_json(second))

    def test_missing_shard_stale_evidence_and_expired_exception_block_or_fail(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest, shard_root, external_root = self.create_complete_evidence(root)
            missing_platform = manifest["required_platforms"][0]
            (shard_root / missing_platform / "environment.json").unlink()
            summary = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            self.assertFalse(summary["ready"])
            self.assertGreater(summary["counts"]["missing"], 0)
            self.assertEqual(summary["status"], "blocked")

            self.write(
                shard_root / missing_platform / "environment.json",
                {
                    "schema_version": 1,
                    "git_sha": "b" * 40,
                    "platform": missing_platform,
                    "working_tree_dirty": False,
                    "manifest_sha256": sha256_file(MANIFEST_PATH),
                    "sdk_version": manifest["sdk_version"],
                    "candidate_identity": CANDIDATE_IDENTITY,
                },
            )
            exceptions = root / "exceptions.json"
            self.write(
                exceptions,
                {
                    "schema_version": 1,
                    "git_sha": COMMIT,
                    "exceptions": [
                        {
                            "id": "EX-EXPIRED",
                            "acceptance_id": "AC-101",
                            "owner": "QO",
                            "impact": "non-blocking fixture note",
                            "rollback_condition": "remove the fixture note",
                            "expires_at": "2026-07-26T11:00:00Z",
                            "status": "open",
                            "blocking": False,
                        }
                    ],
                },
            )
            summary = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                exceptions_path=exceptions,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            self.assertEqual(summary["status"], "failed")
            self.assertEqual(summary["counts"]["expired_exceptions"], 1)
            self.assertTrue(any("target git SHA" in blocker for blocker in summary["blockers"]))

    def test_g5_signoff_requires_exact_ready_summary_and_independent_current_approvals(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, shard_root, external_root = self.create_complete_evidence(root)
            summary = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            payload = canonical_json(summary)
            roles = ["MT", "AO", "QO", "SO", "RO", "SCO"]
            signoff = {
                "schema_version": 1,
                "gate": "G5",
                "git_sha": COMMIT,
                "sdk_version": "v1.1.92",
                "candidate_identity": CANDIDATE_IDENTITY,
                "candidate_identity_sha256": candidate_identity_sha256(
                    CANDIDATE_IDENTITY
                ),
                "evidence_sha256": hashlib.sha256(payload).hexdigest(),
                "approvals": [
                    {
                        "role": role,
                        "identity": f"reviewer-{index}",
                        "decision": "approved",
                        "approved_at": "2026-07-26T12:05:00Z",
                    }
                    for index, role in enumerate(roles)
                ],
                "exceptions": [],
            }
            self.assertEqual(
                verify(
                    signoff,
                    payload,
                    expected_git_sha=COMMIT,
                    now=dt.datetime(2026, 7, 26, 13, 0, tzinfo=dt.timezone.utc),
                ),
                [],
            )
            tampered = payload.replace(b'"ready":true', b'"ready":false')
            errors = verify(
                signoff,
                tampered,
                expected_git_sha=COMMIT,
                now=dt.datetime(2026, 7, 26, 13, 0, tzinfo=dt.timezone.utc),
            )
            self.assertIn("evidence_sha256 does not match the supplied summary", errors)
            signoff["approvals"][4]["identity"] = signoff["approvals"][3]["identity"]
            errors = verify(
                signoff,
                payload,
                expected_git_sha=COMMIT,
                now=dt.datetime(2026, 7, 26, 13, 0, tzinfo=dt.timezone.utc),
            )
            self.assertIn("G5 requires distinct Security and Release Owner approvers", errors)

            changed_summary = copy.deepcopy(summary)
            changed_summary["source_runs"]["runs"]["canary_evidence"]["run_id"] += 100
            changed_payload = canonical_json(changed_summary)
            signoff["approvals"][4]["identity"] = "reviewer-4"
            signoff["evidence_sha256"] = hashlib.sha256(changed_payload).hexdigest()
            errors = verify(
                signoff,
                changed_payload,
                expected_git_sha=COMMIT,
                now=dt.datetime(2026, 7, 26, 13, 0, tzinfo=dt.timezone.utc),
            )
            self.assertIn(
                "source_runs_sha256 does not match the evidence summary source runs",
                errors,
            )

    def test_g5_requires_exact_source_workflow_runs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, shard_root, external_root = self.create_complete_evidence(root)
            with self.assertRaisesRegex(
                FinalSummaryError,
                "exact source workflow runs",
            ):
                build_summary(
                    shard_root=shard_root,
                    external_root=external_root,
                    git_sha=COMMIT,
                    evaluated_at=NOW,
                    candidate_identity=CANDIDATE_IDENTITY,
                )

            changed = copy.deepcopy(SOURCE_RUNS)
            changed["runs"]["model_evidence"]["head_sha"] = "b" * 40
            with self.assertRaisesRegex(FinalSummaryError, "run head SHA differs"):
                build_summary(
                    shard_root=shard_root,
                    external_root=external_root,
                    git_sha=COMMIT,
                    evaluated_at=NOW,
                    candidate_identity=CANDIDATE_IDENTITY,
                    source_runs=changed,
                )

    def test_g5_rejects_same_commit_evidence_from_a_different_candidate(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest, shard_root, external_root = self.create_complete_evidence(root)
            platform = manifest["required_platforms"][0]
            environment_path = shard_root / platform / "environment.json"
            environment = json.loads(environment_path.read_text(encoding="utf-8"))
            environment["candidate_identity"] = dict(
                CANDIDATE_IDENTITY,
                candidate_checksums_sha256="4" * 64,
            )
            self.write(environment_path, environment)
            summary = self.build_g5_summary(
                shard_root=shard_root,
                external_root=external_root,
                git_sha=COMMIT,
                evaluated_at=NOW,
                candidate_identity=CANDIDATE_IDENTITY,
            )
            self.assertFalse(summary["ready"])
            self.assertEqual(summary["status"], "failed")
            self.assertTrue(
                any("candidate_identity differs" in blocker for blocker in summary["blockers"])
            )

    def test_g5_placeholder_example_cannot_authorize_release(self):
        example = json.loads(
            (Path(__file__).parents[1] / "contracts/gates/g5-signoff.example.json").read_text(
                encoding="utf-8"
            )
        )
        errors = verify(example, b"{}\n", now=NOW)
        self.assertIn("git_sha cannot use the all-zero placeholder", errors)
        self.assertIn("evidence_sha256 cannot use the all-zero placeholder", errors)
        self.assertTrue(any("placeholder" in error for error in errors))


if __name__ == "__main__":
    unittest.main()
