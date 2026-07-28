import datetime as dt
import json
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

from scripts import runtime_launcher
from scripts.acceptance import p5

COMMIT = "1" * 40
PRODUCT_VERSION = "0.9.1"
ADVERTISER_HASH = "2" * 64
OBJECT_HASH = "3" * 64
CANDIDATE_IDENTITY_DIGEST = "4" * 64
CANDIDATE_IDENTITY = {
    "schema_version": 1,
    "git_sha": COMMIT,
    "product_version": PRODUCT_VERSION,
    "plugin_version": "0.9.1+codex.test",
    "sdk_version": "v1.1.92",
    "source_tree_sha256": "5" * 64,
    "candidate_checksums_sha256": "6" * 64,
    "release_public_key_sha256": "7" * 64,
    "release": True,
}


def approval(**overrides):
    now = dt.datetime.now(dt.timezone.utc)
    value = {
        "schema_version": 1,
        "kind": "ac127_write_canary",
        "approval_id": "AC127-TEST-0001",
        "git_commit": COMMIT,
        "product_version": PRODUCT_VERSION,
        "candidate_identity_sha256": CANDIDATE_IDENTITY_DIGEST,
        "expires_at": (now + dt.timedelta(hours=1)).isoformat(),
        "advertiser_hash": ADVERTISER_HASH,
        "max_objects": 1,
        "max_spend": 0,
        "commands": ["plans create-qianchuan"],
        "endpoints": ["/v1.0/qianchuan/uni_aweme/ad/create/"],
        "stop_owner": "release-owner",
        "approvals": [
            {
                "role": "MT",
                "approver": "maintainer",
                "decision": "approved",
                "approved_at": (now - dt.timedelta(minutes=2)).isoformat(),
            },
            {
                "role": "SO",
                "approver": "security-owner",
                "decision": "approved",
                "approved_at": (now - dt.timedelta(minutes=1)).isoformat(),
            },
        ],
    }
    value.update(overrides)
    return value


def driver_result(**overrides):
    value = {
        "approval_id": "AC127-TEST-0001",
        "git_commit": COMMIT,
        "product_version": PRODUCT_VERSION,
        "candidate_identity_sha256": CANDIDATE_IDENTITY_DIGEST,
        "command": "plans create-qianchuan",
        "advertiser_hash": ADVERTISER_HASH,
        "write_calls": 1,
        "spend": 0,
        "endpoints": ["/v1.0/qianchuan/uni_aweme/ad/create/"],
        "duplicate_objects": 0,
        "wrong_account_writes": 0,
        "reconciled": True,
        "paused_or_cleaned": True,
        "object_hashes": [OBJECT_HASH],
    }
    value.update(overrides)
    return value


class P5AcceptanceTests(unittest.TestCase):
    def test_current_dual_approval_is_accepted(self):
        self.assertEqual(
            p5.verify_canary_approval(
                approval(), COMMIT, PRODUCT_VERSION, CANDIDATE_IDENTITY_DIGEST
            ),
            [],
        )

    def test_expired_duplicate_or_future_approval_is_rejected(self):
        now = dt.datetime.now(dt.timezone.utc)
        value = approval(
            expires_at=(now - dt.timedelta(minutes=1)).isoformat(),
            commands=["plans create-qianchuan", "plans create-qianchuan"],
        )
        value["approvals"][1]["approver"] = value["approvals"][0]["approver"]
        value["approvals"][0]["approved_at"] = (now + dt.timedelta(minutes=5)).isoformat()
        errors = p5.verify_canary_approval(
            value, COMMIT, PRODUCT_VERSION, CANDIDATE_IDENTITY_DIGEST
        )
        self.assertTrue(any("unexpired" in error for error in errors))
        self.assertTrue(any("duplicates" in error for error in errors))
        self.assertTrue(any("future" in error for error in errors))
        self.assertTrue(any("distinct" in error for error in errors))

    def test_example_cannot_authorize_a_canary(self):
        example = p5.load_json(
            p5.ROOT / "contracts" / "gates" / "ac127-write-canary.example.json"
        )
        errors = p5.verify_canary_approval(
            example, COMMIT, PRODUCT_VERSION, CANDIDATE_IDENTITY_DIGEST
        )
        self.assertTrue(errors)
        self.assertTrue(any("unexpired" in error for error in errors))
        self.assertTrue(any("has not approved" in error for error in errors))

    def test_pre_driver_block_records_provable_zero_writes(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory) / "blocked.json"
            result = p5.blocked_canary(out, COMMIT, PRODUCT_VERSION, ["not approved"])
            self.assertEqual(result["write_state"], "not_started")
            self.assertEqual(result["write_calls"], 0)
            self.assertEqual(result["object_ids_recorded"], 0)
            self.assertFalse(result["driver_started"])
            self.assertEqual(json.loads(out.read_text(encoding="utf-8")), result)

    def test_post_driver_failure_records_unknown_write_state(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory) / "blocked.json"
            result = p5.blocked_canary(
                out,
                COMMIT,
                PRODUCT_VERSION,
                ["driver failed"],
                approval_valid=True,
                approval_id="AC127-TEST-0001",
                driver_started=True,
                reported_write_calls=1,
            )
            self.assertEqual(result["write_state"], "unknown")
            self.assertIsNone(result["write_calls"])
            self.assertEqual(result["reported_write_calls"], 1)
            self.assertIsNone(result["object_ids_recorded"])

    def test_canary_accepts_only_fully_bound_driver_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            approval_path = root / "approval.json"
            approval_path.write_text(json.dumps(approval()), encoding="utf-8")
            completed = subprocess.CompletedProcess(
                ["driver"], 0, stdout=json.dumps(driver_result()), stderr=""
            )
            with mock.patch.object(
                p5,
                "verify_candidate",
                return_value={
                    "git_commit": COMMIT,
                    "product_version": PRODUCT_VERSION,
                    "candidate_identity": CANDIDATE_IDENTITY,
                    "candidate_identity_sha256": CANDIDATE_IDENTITY_DIGEST,
                },
            ), mock.patch.object(p5, "run", return_value=completed):
                result, exit_code = p5.canary_acceptance(
                    root, approval_path, "driver", root / "result.json"
                )
            self.assertEqual(exit_code, 0)
            self.assertEqual(result["status"], "passed")
            self.assertEqual(result["write_state"], "reconciled")
            self.assertEqual(result["object_ids_recorded"], 1)

    def test_canary_rejects_driver_evidence_outside_approval(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            approval_path = root / "approval.json"
            approval_path.write_text(json.dumps(approval()), encoding="utf-8")
            completed = subprocess.CompletedProcess(
                ["driver"],
                0,
                stdout=json.dumps(
                    driver_result(
                        command="plans batch-qianchuan-works",
                        advertiser_hash="4" * 64,
                        object_hashes=[OBJECT_HASH, "5" * 64],
                    )
                ),
                stderr="",
            )
            with mock.patch.object(
                p5,
                "verify_candidate",
                return_value={
                    "git_commit": COMMIT,
                    "product_version": PRODUCT_VERSION,
                    "candidate_identity": CANDIDATE_IDENTITY,
                    "candidate_identity_sha256": CANDIDATE_IDENTITY_DIGEST,
                },
            ), mock.patch.object(p5, "run", return_value=completed):
                result, exit_code = p5.canary_acceptance(
                    root, approval_path, "driver", root / "result.json"
                )
            self.assertEqual(exit_code, 2)
            self.assertEqual(result["status"], "blocked")
            self.assertEqual(result["write_state"], "unknown")
            self.assertTrue(result["driver_started"])

    def test_extract_plugin_rejects_parent_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            candidate = root / "candidate"
            candidate.mkdir()
            archive = candidate / "ocean-watch-plugin_v0.9.1.zip"
            with zipfile.ZipFile(archive, "w") as bundle:
                bundle.writestr("../escaped", b"unsafe")
            with self.assertRaisesRegex(p5.AcceptanceError, "unsafe"):
                p5.extract_plugin(candidate, root / "install", PRODUCT_VERSION)

    def test_launcher_converts_missing_metadata_and_spawn_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(runtime_launcher.LauncherError, "unreadable"):
                runtime_launcher._read_json(Path(directory) / "missing.json")
        with mock.patch.object(
            runtime_launcher.subprocess, "run", side_effect=OSError("cannot execute")
        ):
            with self.assertRaisesRegex(runtime_launcher.LauncherError, "could not be started"):
                runtime_launcher._probe_bootstrap(Path("bootstrap"), "accounts list")


if __name__ == "__main__":
    unittest.main()
