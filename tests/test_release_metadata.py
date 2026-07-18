import tempfile
import unittest
from pathlib import Path

from scripts import release

ROOT = Path(__file__).resolve().parents[1]


class ReleaseMetadataTests(unittest.TestCase):
    def test_versions_match_across_project_package_and_plugin(self):
        result = release.validate_versions(ROOT)
        self.assertRegex(result["project"], r"^[0-9]+\.[0-9]+\.[0-9]+$")
        self.assertEqual(result["package"], result["project"])
        self.assertEqual(result["plugin_base"], result["project"])

    def test_release_tag_is_derived_from_project_version(self):
        self.assertEqual(release.derive_release_tag(ROOT), "v0.9.1")

    def test_version_contract_rejects_non_release_version(self):
        with self.assertRaisesRegex(release.ReleaseError, "MAJOR.MINOR.PATCH"):
            release.validate_release_version("0.9")

    def test_release_tag_rejects_pending_unreleased_changes_and_wrong_version(self):
        version = release.project_version(ROOT)
        with self.assertRaisesRegex(release.ReleaseError, "未发布段落"):
            release.validate_versions(ROOT, tag=f"v{version}")
        major, minor, patch = (int(part) for part in version.split("."))
        mismatched_tag = f"v{major}.{minor}.{patch + 1}"
        with self.assertRaisesRegex(release.ReleaseError, "does not match"):
            release.validate_versions(ROOT, tag=mismatched_tag)

    def test_release_changelog_requires_date_and_empty_unreleased_section(self):
        valid = """# Changelog

## 未发布

### Added

## 0.9.1 - 2026-07-16

- Released.
"""
        notes = release.validate_release_changelog(valid, "0.9.1")
        self.assertIn("## 0.9.1 - 2026-07-16", notes)
        self.assertIn("- Released.", notes)
        with self.assertRaisesRegex(release.ReleaseError, "未发布段落"):
            release.validate_release_changelog(
                valid.replace("### Added\n", "### Added\n\n- Pending change.\n"),
                "0.9.1",
            )
        with self.assertRaisesRegex(release.ReleaseError, "dated release heading"):
            release.validate_release_changelog(valid.replace(" - 2026-07-16", ""), "0.9.1")
        with self.assertRaisesRegex(release.ReleaseError, "release section.*empty"):
            release.validate_release_changelog(valid.replace("\n- Released.\n", "\n"), "0.9.1")

    def test_release_notes_file_contains_only_the_selected_version_section(self):
        changelog = """# Changelog

## 未发布

## 1.2.3 - 2026-07-18

### Added

- Selected release note.

## 1.2.2 - 2026-07-17

- Previous release note.
"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            package = root / "skills/ads-plan-monitor/src/ocean_watch"
            package.mkdir(parents=True)
            (root / ".codex-plugin").mkdir()
            (root / "pyproject.toml").write_text(
                '[project]\nname = "ocean-watch"\nversion = "1.2.3"\n',
                encoding="utf-8",
            )
            (package / "__init__.py").write_text('__version__ = "1.2.3"\n', encoding="utf-8")
            (root / ".codex-plugin/plugin.json").write_text(
                '{"name":"ocean-watch","version":"1.2.3+codex.test"}\n',
                encoding="utf-8",
            )
            (root / "CHANGELOG.md").write_text(changelog, encoding="utf-8")
            result = release.write_release_notes(root, "v1.2.3", "notes/RELEASE_NOTES.md")
            notes = Path(result["notes_file"]).read_text(encoding="utf-8")
            self.assertIn("Selected release note", notes)
            self.assertNotIn("Previous release note", notes)
            self.assertEqual(result["version"], "1.2.3")

    def test_release_workflow_publishes_notes_without_release_assets(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
        validate_job, publish_job = workflow.split("\n  publish:\n", maxsplit=1)
        self.assertIn("workflow_dispatch:", workflow)
        self.assertNotIn("\n  push:\n", workflow)
        self.assertIn("scripts/release.py tag", workflow)
        self.assertIn("scripts/release.py check --tag", workflow)
        self.assertIn("scripts/release.py notes --tag", workflow)
        self.assertIn('GITHUB_REF}" != "refs/heads/main', workflow)
        self.assertIn('refs/tags/${RELEASE_TAG}^{commit}', workflow)
        self.assertIn("gh release create", workflow)
        self.assertIn("gh release edit", workflow)
        self.assertIn('--target "${GITHUB_SHA}"', workflow)
        self.assertIn("--notes-file release-notes/RELEASE_NOTES.md", workflow)
        self.assertIn("Publish GitHub Release without custom assets", workflow)
        self.assertNotIn("gh release upload", workflow)
        self.assertNotIn("attest-build-provenance", workflow)
        self.assertNotIn("SHA256SUMS", workflow)
        self.assertNotIn("python -m build", workflow)
        self.assertNotIn("scripts/release.py plugin", workflow)
        self.assertNotIn("upload-artifact", workflow)
        self.assertNotIn("download-artifact", workflow)
        self.assertNotIn("contents: write", validate_job)
        self.assertIn("needs: validate", publish_job)
        self.assertIn("contents: write", publish_job)


if __name__ == "__main__":
    unittest.main()
