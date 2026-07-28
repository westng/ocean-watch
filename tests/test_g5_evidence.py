import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.acceptance import (
    assemble_native_matrix,
    benchmark_candidate,
    protected_release_evidence,
)
from scripts.acceptance.ac import canonical_json, load_manifest

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


class G5EvidenceTests(unittest.TestCase):
    def write(self, path: Path, value: dict) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(canonical_json(value))

    def create_shards(self, root: Path) -> None:
        manifest = load_manifest()
        contract = manifest["acceptance"]["AC-107"]
        for platform in manifest["required_platforms"]:
            shard = root / platform
            common = {
                "schema_version": 1,
                "git_sha": COMMIT,
                "platform": platform,
                "working_tree_dirty": False,
                "candidate_identity": IDENTITY,
            }
            self.write(shard / "environment.json", common)
            self.write(
                shard / "runner-summary.json",
                {**common, "status": "blocked", "runner_errors": []},
            )
            tests = []
            for group, names in contract["tests"].items():
                if group == "race" and platform.startswith("windows-"):
                    continue
                tests.extend(
                    {"group": group, "name": name, "status": "passed"}
                    for name in names
                )
            self.write(
                shard / "ac-results" / "ac-107.json",
                {
                    **common,
                    "acceptance_id": "AC-107",
                    "status": "blocked",
                    "tests": tests,
                },
            )

    def test_native_matrix_requires_five_matching_formal_candidate_shards(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.create_shards(root)
            result = assemble_native_matrix.assemble(
                shard_root=root,
                candidate_identity=IDENTITY,
            )
            self.assertEqual(result["status"], "passed")
            self.assertEqual(len(result["platforms"]), 5)
            self.assertTrue(all(row["status"] == "passed" for row in result["platforms"]))

            environment = root / "linux-arm64" / "environment.json"
            changed = {**self.load(environment), "candidate_identity": {**IDENTITY, "candidate_checksums_sha256": "4" * 64}}
            self.write(environment, changed)
            with self.assertRaisesRegex(
                assemble_native_matrix.NativeMatrixError,
                "candidate_identity differs",
            ):
                assemble_native_matrix.assemble(
                    shard_root=root,
                    candidate_identity=IDENTITY,
                )

    def test_native_matrix_rejects_missing_platform(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.create_shards(root)
            (root / "windows-amd64" / "environment.json").unlink()
            with self.assertRaises(assemble_native_matrix.NativeMatrixError):
                assemble_native_matrix.assemble(
                    shard_root=root,
                    candidate_identity=IDENTITY,
                )

    def test_benchmark_binds_release_identity_and_enforces_thresholds(self):
        verified = {
            "git_commit": COMMIT,
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": "4" * 64,
        }
        with tempfile.TemporaryDirectory() as temporary:
            candidate = Path(temporary)
            (candidate / "ocean-watch_linux_amd64").write_bytes(b"runtime")
            with mock.patch.object(
                benchmark_candidate, "verify_candidate", return_value=verified
            ), mock.patch.object(
                benchmark_candidate,
                "_measure",
                side_effect=[
                    {"trials": 30, "p50_ms": 20.0, "p95_ms": 40.0, "max_ms": 50.0},
                    {"trials": 30, "p50_ms": 10.0, "p95_ms": 30.0, "max_ms": 35.0},
                ],
            ), mock.patch.object(
                benchmark_candidate,
                "_run_go_checks",
                return_value={"request_budget_runs": 30},
            ):
                result = benchmark_candidate.benchmark(
                    candidate_dir=candidate,
                    python=Path("python"),
                    expected_commit=COMMIT,
                )
            self.assertEqual(result["status"], "passed")
            self.assertEqual(result["candidate_identity"], IDENTITY)
            self.assertLess(result["go_to_python_p95_ratio"], 1.15)

    def test_benchmark_rejects_non_formal_trial_count_and_slow_candidate(self):
        with self.assertRaisesRegex(benchmark_candidate.BenchmarkError, "exactly 30"):
            benchmark_candidate.benchmark(
                candidate_dir=Path("candidate"),
                python=Path("python"),
                expected_commit=COMMIT,
                trials=29,
            )
        verified = {
            "git_commit": COMMIT,
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": "4" * 64,
        }
        with tempfile.TemporaryDirectory() as temporary:
            candidate = Path(temporary)
            (candidate / "ocean-watch_linux_amd64").write_bytes(b"runtime")
            with mock.patch.object(
                benchmark_candidate, "verify_candidate", return_value=verified
            ), mock.patch.object(
                benchmark_candidate,
                "_measure",
                side_effect=[
                    {"trials": 30, "p50_ms": 20.0, "p95_ms": 40.0, "max_ms": 50.0},
                    {"trials": 30, "p50_ms": 40.0, "p95_ms": 50.0, "max_ms": 55.0},
                ],
            ), mock.patch.object(benchmark_candidate, "_run_go_checks", return_value={}):
                with self.assertRaisesRegex(benchmark_candidate.BenchmarkError, "1.15x"):
                    benchmark_candidate.benchmark(
                        candidate_dir=candidate,
                        python=Path("python"),
                        expected_commit=COMMIT,
                    )

    def test_protected_release_evidence_binds_main_run_and_trust_root(self):
        public_key = "4" * 64
        verified = {
            "candidate_identity": {
                **IDENTITY,
                "release_public_key_sha256": protected_release_evidence.release_public_key_sha256(
                    public_key
                ),
            },
            "candidate_identity_sha256": "5" * 64,
        }
        with tempfile.TemporaryDirectory() as temporary:
            candidate = Path(temporary)
            (candidate / "release-public-key.txt").write_text(
                public_key + "\n", encoding="utf-8"
            )
            with mock.patch.object(
                protected_release_evidence,
                "verify_candidate",
                return_value=verified,
            ) as verify:
                result = protected_release_evidence.build_evidence(
                    candidate_dir=candidate,
                    expected_public_key_hex=public_key,
                    git_sha=COMMIT,
                    repository="westng/ocean-watch",
                    ref="refs/heads/main",
                    run_id="123",
                    run_attempt="2",
                    environment_name="g5-release-candidate",
                    actor="release-owner",
                    ref_protected=True,
                )
            self.assertEqual(result["status"], "passed")
            self.assertNotIn(public_key, str(result))
            self.assertTrue(result["approved_public_key_matched"])
            verify.assert_called_once_with(
                candidate,
                verify_signatures=True,
                require_release=True,
                expected_commit=COMMIT,
            )

    def test_protected_release_evidence_rejects_unprotected_or_wrong_environment(self):
        arguments = {
            "candidate_dir": Path("candidate"),
            "expected_public_key_hex": "4" * 64,
            "git_sha": COMMIT,
            "repository": "westng/ocean-watch",
            "ref": "refs/heads/main",
            "run_id": "123",
            "run_attempt": "1",
            "environment_name": "g5-release-candidate",
            "actor": "release-owner",
            "ref_protected": False,
        }
        with self.assertRaisesRegex(
            protected_release_evidence.ProtectedReleaseError,
            "protected main ref",
        ):
            protected_release_evidence.build_evidence(**arguments)
        arguments["ref_protected"] = True
        arguments["environment_name"] = "ordinary-ci"
        with self.assertRaisesRegex(
            protected_release_evidence.ProtectedReleaseError,
            "protected G5 environment",
        ):
            protected_release_evidence.build_evidence(**arguments)

    @staticmethod
    def load(path: Path) -> dict:
        import json

        return json.loads(path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
