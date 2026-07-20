import unittest
from pathlib import Path

from scripts import version_tag

ROOT = Path(__file__).resolve().parents[1]


class VersionTagMetadataTests(unittest.TestCase):
    def test_versions_match_across_project_package_and_plugin(self):
        result = version_tag.validate_versions(ROOT)
        self.assertRegex(result["project"], r"^[0-9]+\.[0-9]+\.[0-9]+$")
        self.assertEqual(result["package"], result["project"])
        self.assertEqual(result["plugin_base"], result["project"])

    def test_version_tag_is_derived_from_project_version(self):
        self.assertEqual(version_tag.derive_version_tag(ROOT), "v0.9.1")

    def test_version_contract_rejects_non_semantic_version(self):
        with self.assertRaisesRegex(version_tag.VersionTagError, "MAJOR.MINOR.PATCH"):
            version_tag.validate_tag_version("0.9")

    def test_version_tag_rejects_pending_changes_and_wrong_version(self):
        version = version_tag.project_version(ROOT)
        with self.assertRaisesRegex(version_tag.VersionTagError, "未发布段落"):
            version_tag.validate_versions(ROOT, tag=f"v{version}")
        major, minor, patch = (int(part) for part in version.split("."))
        mismatched_tag = f"v{major}.{minor}.{patch + 1}"
        with self.assertRaisesRegex(version_tag.VersionTagError, "does not match"):
            version_tag.validate_versions(ROOT, tag=mismatched_tag)

    def test_tag_changelog_requires_date_and_empty_unreleased_section(self):
        valid = """# Changelog

## 未发布

### Added

## 0.9.1 - 2026-07-16

- Released.
"""
        notes = version_tag.validate_tag_changelog(valid, "0.9.1")
        self.assertIn("## 0.9.1 - 2026-07-16", notes)
        self.assertIn("- Released.", notes)
        with self.assertRaisesRegex(version_tag.VersionTagError, "未发布段落"):
            version_tag.validate_tag_changelog(
                valid.replace("### Added\n", "### Added\n\n- Pending change.\n"),
                "0.9.1",
            )
        with self.assertRaisesRegex(version_tag.VersionTagError, "dated version heading"):
            version_tag.validate_tag_changelog(valid.replace(" - 2026-07-16", ""), "0.9.1")
        with self.assertRaisesRegex(version_tag.VersionTagError, "version section.*empty"):
            version_tag.validate_tag_changelog(valid.replace("\n- Released.\n", "\n"), "0.9.1")

    def test_tag_workflow_only_publishes_version_tag(self):
        workflow = (ROOT / ".github/workflows/tag.yml").read_text(encoding="utf-8")
        validate_job, publish_job = workflow.split("\n  publish:\n", maxsplit=1)
        self.assertIn("name: Publish Tag", workflow)
        self.assertIn("workflow_dispatch:", workflow)
        self.assertNotIn("\n  push:\n", workflow)
        self.assertIn("scripts/version_tag.py tag", workflow)
        self.assertIn("scripts/version_tag.py check --tag", workflow)
        self.assertIn('GITHUB_REF}" != "refs/heads/main', workflow)
        self.assertIn('refs/tags/${VERSION_TAG}^{commit}', workflow)
        self.assertIn("Publish version tag", workflow)
        self.assertIn('git tag "${VERSION_TAG}" "${GITHUB_SHA}"', workflow)
        self.assertIn('git push origin "refs/tags/${VERSION_TAG}"', workflow)
        self.assertNotIn("gh release", workflow)
        self.assertNotIn("RELEASE_NOTES", workflow)
        self.assertNotIn("scripts/release.py", workflow)
        self.assertNotIn("attest-build-provenance", workflow)
        self.assertNotIn("SHA256SUMS", workflow)
        self.assertNotIn("python -m build", workflow)
        self.assertNotIn("scripts/version_tag.py plugin", workflow)
        self.assertNotIn("upload-artifact", workflow)
        self.assertNotIn("download-artifact", workflow)
        self.assertNotIn("contents: write", validate_job)
        self.assertIn("needs: validate", publish_job)
        self.assertIn("contents: write", publish_job)


if __name__ == "__main__":
    unittest.main()
