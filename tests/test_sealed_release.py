import copy
import datetime as dt
import hashlib
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.acceptance import g5_source_policy, source_runs
from scripts.acceptance.candidate_identity import (
    candidate_identity_sha256,
    canonical_json,
)
from scripts.release import sealed_release

COMMIT = "a" * 40
IDENTITY = {
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


class SealedReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.candidate = self.root / "candidate"
        self.candidate.mkdir()
        (self.candidate / "asset.bin").write_bytes(b"candidate")
        self.evidence = self.root / "evidence"
        (self.evidence / "contracts").mkdir(parents=True)
        self.evidence_file = self.evidence / "contracts" / "report.json"
        self.evidence_file.write_bytes(b'{"status":"passed"}\n')
        self.summary_path = self.root / "summary.json"
        self.signoff_path = self.root / "signoff.json"
        self.prepared_run = self.root / "prepared-run.json"
        self.signoff_run = self.root / "signoff-run.json"
        self.out = self.root / "sealed"
        summary = {
            "schema_version": 1,
            "gate": "G5",
            "git_sha": COMMIT,
            "sdk_version": "v1.1.92",
            "evaluated_at": "2026-07-27T00:00:00Z",
            "status": "passed",
            "ready": True,
            "counts": {
                "failed": 0,
                "blocking": 0,
                "missing": 0,
                "not_run": 0,
                "expired_exceptions": 0,
            },
            "blockers": [],
            "exceptions": {"active": [], "expired": [], "closed": []},
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": candidate_identity_sha256(IDENTITY),
            "source_runs": SOURCE_RUNS,
            "source_runs_sha256": source_runs.digest(SOURCE_RUNS),
            "evidence": [
                {
                    "path": "contracts/report.json",
                    "sha256": hashlib.sha256(self.evidence_file.read_bytes()).hexdigest(),
                    "size": self.evidence_file.stat().st_size,
                }
            ],
        }
        self.summary_path.write_bytes(canonical_json(summary))
        roles = ["MT", "AO", "QO", "SO", "RO", "SCO"]
        signoff = {
            "schema_version": 1,
            "gate": "G5",
            "git_sha": COMMIT,
            "sdk_version": "v1.1.92",
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": candidate_identity_sha256(IDENTITY),
            "evidence_sha256": hashlib.sha256(self.summary_path.read_bytes()).hexdigest(),
            "approvals": [
                {
                    "role": role,
                    "identity": f"reviewer-{index}",
                    "decision": "approved",
                    "approved_at": "2026-07-27T01:00:00Z",
                }
                for index, role in enumerate(roles)
            ],
            "exceptions": [],
        }
        self.signoff_path.write_bytes(canonical_json(signoff))
        self.write_run(
            self.prepared_run,
            run_id=100,
            workflow=sealed_release.PREPARE_WORKFLOW,
        )
        self.write_run(
            self.signoff_run,
            run_id=200,
            workflow=sealed_release.SIGNOFF_WORKFLOW,
        )

    def write_run(self, path: Path, *, run_id: int, workflow: str) -> None:
        path.write_bytes(
            canonical_json(
                {
                    "schema_version": 1,
                    "status": "passed",
                    "run_id": run_id,
                    "repository": "westng/ocean-watch",
                    "workflow_path": workflow,
                    "head_sha": COMMIT,
                    "run_attempt": 1,
                }
            )
        )

    @staticmethod
    def candidate_result() -> dict:
        return {
            "product_version": "0.9.1",
            "plugin_version": "0.9.1+codex.test",
            "git_commit": COMMIT,
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": candidate_identity_sha256(IDENTITY),
        }

    def seal(self) -> dict:
        return sealed_release.seal(
            candidate_dir=self.candidate,
            evidence_root=self.evidence,
            summary_path=self.summary_path,
            signoff_path=self.signoff_path,
            prepared_run_path=self.prepared_run,
            signoff_run_path=self.signoff_run,
            out_dir=self.out,
            expected_commit=COMMIT,
        )

    @mock.patch.object(sealed_release, "verify_candidate", return_value=candidate_result())
    def test_sealed_release_is_self_contained_and_reverifiable(self, verify_candidate):
        manifest = self.seal()
        self.assertEqual(manifest["status"], "sealed")
        self.assertEqual(manifest["tag"], "v0.9.1")
        self.assertTrue((self.out / "candidate" / "asset.bin").is_file())
        self.assertTrue((self.out / "evidence" / "files" / "contracts/report.json").is_file())
        verified = sealed_release.verify(
            self.out,
            expected_commit=COMMIT,
            expected_tag="v0.9.1",
        )
        self.assertEqual(verified, manifest)
        self.assertEqual(verify_candidate.call_count, 2)

    @mock.patch.object(sealed_release, "verify_candidate", return_value=candidate_result())
    def test_tampered_evidence_or_manifest_is_rejected(self, _verify_candidate):
        self.seal()
        evidence = self.out / "evidence" / "files" / "contracts/report.json"
        evidence.write_bytes(b"tampered")
        with self.assertRaisesRegex(sealed_release.SealedReleaseError, "evidence differs"):
            sealed_release.verify(self.out, expected_commit=COMMIT)

        evidence.write_bytes(self.evidence_file.read_bytes())
        manifest_path = self.out / "seal.json"
        manifest = sealed_release._load_json(manifest_path)
        changed = copy.deepcopy(manifest)
        changed["tag"] = "v0.9.2"
        manifest_path.write_bytes(canonical_json(changed))
        with self.assertRaisesRegex(sealed_release.SealedReleaseError, "identity differs"):
            sealed_release.verify(self.out, expected_commit=COMMIT)

    @mock.patch.object(sealed_release, "verify_candidate", return_value=candidate_result())
    def test_signoff_must_hash_exact_summary(self, _verify_candidate):
        summary = sealed_release._load_json(self.summary_path)
        summary["evaluated_at"] = dt.datetime.now(dt.timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        self.summary_path.write_bytes(canonical_json(summary))
        with self.assertRaisesRegex(sealed_release.SealedReleaseError, "signoff is invalid"):
            self.seal()


if __name__ == "__main__":
    unittest.main()
