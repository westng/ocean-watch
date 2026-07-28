import unittest
from pathlib import Path
from unittest import mock

import yaml

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

    def test_release_notes_are_the_matching_chinese_changelog_section(self):
        valid = """# 更新日志

## 未发布

### 新增

## 0.9.1 - 2026-07-16

### 修复

- 修复发布流程。

## 0.9.0 - 2026-07-15

- 旧版本。
"""
        with mock.patch.object(Path, "read_text", return_value=valid), mock.patch.object(
            version_tag, "validate_versions", return_value={"project": "0.9.1"}
        ):
            notes = version_tag.release_notes(ROOT, "v0.9.1")
        self.assertTrue(notes.startswith("## 0.9.1 - 2026-07-16"))
        self.assertIn("- 修复发布流程。", notes)
        self.assertNotIn("旧版本", notes)

    def test_tag_workflow_only_publishes_a_verified_sealed_release(self):
        workflow = (ROOT / ".github/workflows/tag.yml").read_text(encoding="utf-8")
        validate_job, publish_job = workflow.split("\n  publish:\n", maxsplit=1)
        parsed = yaml.load(workflow, Loader=yaml.BaseLoader)
        inputs = parsed["on"]["workflow_dispatch"]["inputs"]

        self.assertIn("name: Publish Sealed Release", workflow)
        self.assertEqual(set(inputs), {"sealed_run_id"})
        self.assertNotIn("\n  push:\n", workflow)
        self.assertIn("scripts/version_tag.py tag", workflow)
        self.assertIn("scripts/version_tag.py check --tag", workflow)
        self.assertIn("github.ref == 'refs/heads/main'", workflow)
        self.assertIn("GITHUB_REF_PROTECTED", validate_job)
        self.assertIn('refs/tags/${VERSION_TAG}^{commit}', workflow)
        self.assertIn("Publish immutable version Tag", workflow)
        self.assertIn('git push origin "${GITHUB_SHA}:refs/tags/${VERSION_TAG}"', workflow)
        self.assertNotIn("scripts/release/build_candidate.py", workflow)
        self.assertEqual(workflow.count("gh run download"), 2)
        self.assertEqual(workflow.count("sealed_release.py verify"), 2)
        self.assertEqual(workflow.count("verify_workflow_run.py"), 2)
        self.assertEqual(workflow.count("verify_gate_signoff.py"), 2)
        self.assertEqual(workflow.count("--reject-tracked-signoff"), 2)
        self.assertEqual(workflow.count("--expected-workflow-path .github/workflows/g5-seal.yml"), 2)
        self.assertIn("scripts/version_tag.py notes", workflow)
        self.assertIn('gh release create "${VERSION_TAG}"', workflow)
        self.assertIn('gh release download "${VERSION_TAG}"', workflow)
        self.assertIn("scripts/release/verify_published_release.py", workflow)
        self.assertNotIn("--clobber", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_PUBLIC_KEY", workflow)
        self.assertEqual(workflow.count("contents: write"), 1)
        self.assertNotIn("contents: write", validate_job)
        self.assertIn("needs: validate", publish_job)
        self.assertIn("contents: write", publish_job)
        self.assertIn("environment: g5-release-publish", publish_job)
        self.assertIn("persist-credentials: true", publish_job)
        self.assertNotIn("persist-credentials: false", publish_job)


if __name__ == "__main__":
    unittest.main()
