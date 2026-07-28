import json
import tempfile
import unittest
import zipfile
from pathlib import Path

from scripts.acceptance.candidate_identity import (
    build_candidate_identity,
    candidate_identity_sha256,
    compare_candidate_identities,
    release_public_key_sha256,
    validate_candidate_identity,
)
from scripts.release.build_candidate import (
    TARGETS,
    canonical_json,
    prepare_output,
    target_name,
    write_build_summary,
)


class ReleaseCandidateTests(unittest.TestCase):
    def candidate_identity(self):
        return build_candidate_identity(
            git_sha="1" * 40,
            product_version="0.9.1",
            plugin_version="0.9.1+codex.test",
            sdk_version="v1.1.92",
            source_tree_sha256="2" * 64,
            candidate_checksums_sha256="3" * 64,
            release_public_key_hex="4" * 64,
            release=True,
        )

    def test_canonical_manifest_encoding_matches_go_json_contract(self):
        self.assertEqual(
            canonical_json({"routes": {"accounts list": "python"}, "manifest_version": 1}),
            b'{"manifest_version":1,"routes":{"accounts list":"python"}}',
        )

    def test_target_matrix_has_exact_five_asset_names(self):
        self.assertEqual(
            [target_name("ocean-watch", goos, goarch) for goos, goarch in TARGETS],
            [
                "ocean-watch_darwin_arm64",
                "ocean-watch_darwin_amd64",
                "ocean-watch_linux_amd64",
                "ocean-watch_linux_arm64",
                "ocean-watch_windows_amd64.exe",
            ],
        )

    def test_prepare_output_rejects_repository_root(self):
        with self.assertRaisesRegex(RuntimeError, "unsafe"):
            prepare_output(Path(__file__).resolve().parents[1])

    def test_candidate_zip_policy_shape_is_parseable(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "candidate.zip"
            policy = canonical_json(
                {
                    "schema_version": 1,
                    "enabled": True,
                    "product_version": "0.0.0",
                    "plugin_version": "0.0.0+codex.test",
                    "commands": ["accounts list"],
                }
            )
            with zipfile.ZipFile(archive, "w") as bundle:
                bundle.writestr("ocean-watch/.codex-plugin/runtime-policy.json", policy)
            with zipfile.ZipFile(archive) as bundle:
                loaded = json.loads(
                    bundle.read("ocean-watch/.codex-plugin/runtime-policy.json")
                )
            self.assertTrue(loaded["enabled"])
            self.assertEqual(loaded["commands"], ["accounts list"])

    def test_build_summary_is_reproducible_and_uses_relative_output(self):
        with tempfile.TemporaryDirectory() as first, tempfile.TemporaryDirectory() as second:
            arguments = {
                "release": True,
                "product_version": "0.9.1",
                "plugin_version": "0.9.1+codex.test",
                "commit": "1" * 40,
                "source_digest": "2" * 64,
                "dirty_paths": [],
                "route_data": {
                    "route_manifest_version": 5,
                    "routes": {"accounts list": "python"},
                },
                "runtime_count": 5,
                "bootstrap_count": 5,
                "signed_file_count": 17,
            }
            first_path, first_result = write_build_summary(Path(first), **arguments)
            second_path, second_result = write_build_summary(Path(second), **arguments)
            self.assertEqual(first_path.read_bytes(), second_path.read_bytes())
            self.assertEqual(first_result, second_result)
            self.assertEqual(first_result["output"], ".")
            self.assertEqual(first_result["signed_file_count"], 17)

    def test_candidate_identity_binds_signed_assets_source_and_trust_root(self):
        identity = self.candidate_identity()
        self.assertEqual(validate_candidate_identity(identity, require_release=True), [])
        self.assertEqual(
            identity["release_public_key_sha256"],
            release_public_key_sha256("4" * 64),
        )
        self.assertEqual(len(candidate_identity_sha256(identity)), 64)
        changed = dict(identity, candidate_checksums_sha256="5" * 64)
        errors = compare_candidate_identities(changed, identity)
        self.assertTrue(any("candidate_checksums_sha256" in error for error in errors))

    def test_candidate_identity_rejects_test_candidate_for_g5(self):
        identity = dict(self.candidate_identity(), release=False)
        errors = validate_candidate_identity(identity, require_release=True)
        self.assertIn("candidate_identity is not a formal release candidate", errors)

    def test_candidate_identity_comparison_validates_both_sides(self):
        identity = self.candidate_identity()
        self.assertEqual(
            compare_candidate_identities(None, identity),
            ["actual candidate_identity must be an object"],
        )
        self.assertEqual(
            compare_candidate_identities(identity, None),
            ["sealed candidate_identity must be an object"],
        )


if __name__ == "__main__":
    unittest.main()
