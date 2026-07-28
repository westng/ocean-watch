import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.acceptance import native_candidate

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


class NativeCandidateTests(unittest.TestCase):
    def verified(self):
        return {
            "git_commit": COMMIT,
            "candidate_identity": IDENTITY,
            "candidate_identity_sha256": "4" * 64,
            "signatures_verified": True,
        }

    def test_orchestrator_consumes_native_binary_and_binds_contract_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            candidate = root / "candidate"
            candidate.mkdir()
            (candidate / "ocean-watch_linux_amd64").write_bytes(b"candidate")
            out = root / "evidence" / "linux-amd64"
            commands = []

            def record(command, _label):
                commands.append(command)

            with mock.patch.object(native_candidate.ac, "git_sha", return_value=COMMIT), mock.patch.object(
                native_candidate.ac, "git_dirty", return_value=False
            ), mock.patch.object(
                native_candidate.ac,
                "native_platform",
                return_value=("linux", "amd64", "linux-amd64"),
            ), mock.patch.object(
                native_candidate.p5, "verify_candidate", return_value=self.verified()
            ) as verify, mock.patch.object(
                native_candidate.p5, "current_target", return_value=("linux", "amd64")
            ), mock.patch.object(
                native_candidate.p5, "launcher_acceptance"
            ) as launcher, mock.patch.object(
                native_candidate.p5, "upgrade_rollback_acceptance"
            ), mock.patch.object(
                native_candidate.p5, "user_journey_acceptance"
            ), mock.patch.object(
                native_candidate.ac,
                "run_acceptance",
                return_value=({"status": "blocked"}, 0),
            ) as run_acceptance, mock.patch.object(
                native_candidate, "_run", side_effect=record
            ), mock.patch.object(
                native_candidate, "scan", return_value=[]
            ):
                result = native_candidate.run_native_candidate(
                    candidate_dir=candidate,
                    out_dir=out,
                    python=Path("python"),
                    expected_platform="linux-amd64",
                    expected_commit=COMMIT,
                    require_release=True,
                )

            self.assertEqual(result["status"], "blocked")
            verify.assert_called_once_with(
                candidate,
                verify_signatures=True,
                require_release=True,
                expected_commit=COMMIT,
            )
            self.assertEqual(len(commands), 2)
            compare = commands[1]
            self.assertIn("--candidate-identity", compare)
            candidate_argument = Path(compare[compare.index("--candidate") + 1])
            self.assertEqual(
                candidate_argument.resolve(),
                (candidate / "ocean-watch_linux_amd64").resolve(),
            )
            self.assertNotIn("build", compare)
            self.assertEqual(
                (out / "candidate-identity.json").read_text(encoding="utf-8"),
                native_candidate.canonical_json(IDENTITY).decode("utf-8"),
            )
            self.assertEqual(
                run_acceptance.call_args.kwargs["candidate_identity"], IDENTITY
            )
            self.assertFalse(run_acceptance.call_args.kwargs["dirty"])
            self.assertTrue(launcher.call_args.kwargs["require_release"])
            self.assertEqual(launcher.call_args.kwargs["expected_commit"], COMMIT)

    def test_orchestrator_rejects_dirty_checkout_before_candidate_use(self):
        with tempfile.TemporaryDirectory() as temporary, mock.patch.object(
            native_candidate.ac, "git_sha", return_value=COMMIT
        ), mock.patch.object(
            native_candidate.ac, "git_dirty", return_value=True
        ), mock.patch.object(
            native_candidate.p5, "verify_candidate"
        ) as verify:
            with self.assertRaisesRegex(
                native_candidate.NativeCandidateError, "clean source checkout"
            ):
                native_candidate.run_native_candidate(
                    candidate_dir=Path(temporary) / "candidate",
                    out_dir=Path(temporary) / "evidence" / "linux-amd64",
                    python=Path("python"),
                )
            verify.assert_not_called()

    def test_orchestrator_rejects_candidate_from_another_commit(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            candidate = root / "candidate"
            candidate.mkdir()
            with mock.patch.object(
                native_candidate.ac, "git_sha", return_value=COMMIT
            ), mock.patch.object(
                native_candidate.ac, "git_dirty", return_value=False
            ), mock.patch.object(
                native_candidate.p5,
                "verify_candidate",
                return_value={**self.verified(), "git_commit": "b" * 40},
            ):
                with self.assertRaisesRegex(
                    native_candidate.NativeCandidateError,
                    "checked-out source commit",
                ):
                    native_candidate.run_native_candidate(
                        candidate_dir=candidate,
                        out_dir=root / "evidence" / "linux-amd64",
                        python=Path("python"),
                    )


if __name__ == "__main__":
    unittest.main()
