import hashlib
import tempfile
import unittest
import warnings
import zipfile
from pathlib import Path

from scripts import release

ROOT = Path(__file__).resolve().parents[1]


class ReleasePackagingTests(unittest.TestCase):
    def test_versions_match_across_project_package_and_plugin(self):
        result = release.validate_versions(ROOT)
        self.assertRegex(result["project"], r"^[0-9]+\.[0-9]+\.[0-9]+$")
        self.assertEqual(result["package"], result["project"])
        self.assertEqual(result["plugin_base"], result["project"])

    def test_version_contract_rejects_non_release_version(self):
        with self.assertRaisesRegex(release.ReleaseError, "MAJOR.MINOR.PATCH"):
            release.validate_release_version("0.9")

    def test_plugin_archive_is_deterministic_and_allowlisted(self):
        with tempfile.TemporaryDirectory() as first, tempfile.TemporaryDirectory() as second:
            first_result = release.build_plugin_archive(ROOT, first)
            second_result = release.build_plugin_archive(ROOT, second)
            first_path = Path(first_result["archive"])
            second_path = Path(second_result["archive"])
            self.assertEqual(
                hashlib.sha256(first_path.read_bytes()).hexdigest(),
                hashlib.sha256(second_path.read_bytes()).hexdigest(),
            )
            verified = release.verify_plugin_archive(first_path)
            self.assertEqual(verified["file_count"], first_result["file_count"])
            with zipfile.ZipFile(first_path) as archive:
                names = archive.namelist()
            self.assertTrue(any(name.endswith("/.codex-plugin/plugin.json") for name in names))
            self.assertTrue(any(name.endswith("/skills/qc-plan-monitor/SKILL.md") for name in names))
            self.assertFalse(any("/__pycache__/" in name for name in names))
            self.assertFalse(any("/config/" in name for name in names))
            self.assertFalse(any("/tests/" in name for name in names))

    def test_plugin_archive_rejects_duplicate_entries(self):
        with tempfile.TemporaryDirectory() as directory:
            result = release.build_plugin_archive(ROOT, directory)
            archive_path = Path(result["archive"])
            with zipfile.ZipFile(archive_path) as archive:
                duplicate_name = archive.namelist()[0]
            with warnings.catch_warnings():
                warnings.simplefilter("ignore", UserWarning)
                with zipfile.ZipFile(archive_path, "a") as archive:
                    archive.writestr(duplicate_name, b"duplicate")
            with self.assertRaisesRegex(release.ReleaseError, "duplicate entries"):
                release.verify_plugin_archive(archive_path)

    def test_checksums_cover_every_artifact_and_detect_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            (directory / "one.whl").write_bytes(b"one")
            (directory / "two.zip").write_bytes(b"two")
            result = release.write_checksums(directory)
            self.assertEqual(result["artifact_count"], 2)
            self.assertEqual(release.verify_checksums(result["checksum_file"])["artifact_count"], 2)
            (directory / "two.zip").write_bytes(b"changed")
            with self.assertRaisesRegex(release.ReleaseError, "checksum mismatch"):
                release.verify_checksums(result["checksum_file"])

    def test_release_tag_rejects_pending_unreleased_changes_and_wrong_version(self):
        version = release.project_version(ROOT)
        with self.assertRaisesRegex(release.ReleaseError, "Unreleased section"):
            release.validate_versions(ROOT, tag=f"v{version}")
        major, minor, patch = (int(part) for part in version.split("."))
        mismatched_tag = f"v{major}.{minor}.{patch + 1}"
        with self.assertRaisesRegex(release.ReleaseError, "does not match"):
            release.validate_versions(ROOT, tag=mismatched_tag)

    def test_release_changelog_requires_date_and_empty_unreleased_section(self):
        valid = """# Changelog

## Unreleased

### Added

## 0.9.1 - 2026-07-16

- Released.
"""
        release.validate_release_changelog(valid, "0.9.1")
        with self.assertRaisesRegex(release.ReleaseError, "Unreleased section"):
            release.validate_release_changelog(
                valid.replace("### Added\n", "### Added\n\n- Pending change.\n"),
                "0.9.1",
            )
        with self.assertRaisesRegex(release.ReleaseError, "dated release heading"):
            release.validate_release_changelog(valid.replace(" - 2026-07-16", ""), "0.9.1")

    def test_release_workflow_publishes_verified_tag_artifacts(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
        build_job, publish_job = workflow.split("\n  publish:\n", maxsplit=1)
        self.assertIn('"v[0-9]*.[0-9]*.[0-9]*"', workflow)
        self.assertIn("scripts/release.py check --tag", workflow)
        self.assertIn("git merge-base --is-ancestor", workflow)
        self.assertIn("actions/attest-build-provenance@v2", workflow)
        self.assertIn("gh release create", workflow)
        self.assertIn("dist/SHA256SUMS", workflow)
        self.assertNotIn("contents: write", build_job)
        self.assertIn("needs: build", publish_job)
        self.assertIn("contents: write", publish_job)
        self.assertIn("sha256sum --check SHA256SUMS", publish_job)


if __name__ == "__main__":
    unittest.main()
