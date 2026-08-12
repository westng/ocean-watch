import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import yaml

from scripts import version_tag

ROOT = Path(__file__).resolve().parents[1]


class VersionTagMetadataTests(unittest.TestCase):
    def release_fixture(self, root, *, pending=True):
        (root / "skills/ads-plan-monitor/src/ocean_watch").mkdir(parents=True)
        (root / ".codex-plugin").mkdir()
        (root / "pyproject.toml").write_text(
            '[project]\nname = "ocean-watch"\nversion = "1.0.0"\n',
            encoding="utf-8",
        )
        (root / "skills/ads-plan-monitor/src/ocean_watch/__init__.py").write_text(
            '__version__ = "1.0.0"\n',
            encoding="utf-8",
        )
        (root / ".codex-plugin/plugin.json").write_text(
            json.dumps(
                {"name": "ocean-watch", "version": "1.0.0+codex.old"},
                ensure_ascii=False,
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
        pending_section = "\n### 修复\n\n- 修复自动发布。\n" if pending else ""
        (root / "CHANGELOG.md").write_text(
            f"""# 更新日志

## 未发布
{pending_section}
## 1.0.0 - 2026-07-28

### 新增

- 首次发布。
""",
            encoding="utf-8",
        )

    def test_versions_match_across_project_package_and_plugin(self):
        result = version_tag.validate_versions(ROOT)
        self.assertRegex(result["project"], r"^[0-9]+\.[0-9]+\.[0-9]+$")
        self.assertEqual(result["package"], result["project"])
        self.assertEqual(result["plugin_base"], result["project"])

    def test_version_tag_is_derived_from_project_version(self):
        project_version = version_tag.project_version(ROOT)
        self.assertEqual(version_tag.derive_version_tag(ROOT), f"v{project_version}")

    def test_version_contract_rejects_non_semantic_version(self):
        with self.assertRaisesRegex(version_tag.VersionTagError, "MAJOR.MINOR.PATCH"):
            version_tag.validate_tag_version("0.9")

    def test_version_tag_rejects_pending_changes_and_wrong_version(self):
        version = version_tag.project_version(ROOT)
        pending = f"""# Changelog

## 未发布

- Pending change.

## {version} - 2026-07-28

- Released.
"""
        with self.assertRaisesRegex(version_tag.VersionTagError, "未发布段落"):
            version_tag.validate_tag_changelog(pending, version)
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

    def test_prepare_release_increments_patch_and_updates_all_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root)

            result = version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="20260729040208",
            )

            self.assertEqual(
                result,
                {"version": "1.0.1", "tag": "v1.0.1", "already_prepared": False},
            )
            self.assertEqual(version_tag.project_version(root), "1.0.1")
            self.assertEqual(version_tag.package_version(root), "1.0.1")
            self.assertEqual(
                version_tag.plugin_version(root),
                "1.0.1+codex.20260729040208",
            )
            changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
            self.assertIn("## 未发布\n\n## 1.0.1 - 2026-07-29", changelog)
            self.assertIn("- 修复自动发布。", changelog)
            version_tag.validate_versions(root, tag="v1.0.1")

    def test_prepare_release_rerun_reuses_prepared_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root)
            version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="first",
            )
            before = {
                path: (root / path).read_bytes()
                for path in (
                    "CHANGELOG.md",
                    "pyproject.toml",
                    "skills/ads-plan-monitor/src/ocean_watch/__init__.py",
                    ".codex-plugin/plugin.json",
                )
            }

            result = version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="second",
            )

            self.assertEqual(
                result,
                {"version": "1.0.1", "tag": "v1.0.1", "already_prepared": True},
            )
            self.assertEqual(
                before,
                {path: (root / path).read_bytes() for path in before},
            )

    def test_prepare_release_without_pending_changes_does_not_increment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root, pending=False)

            result = version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="unused",
            )

            self.assertEqual(
                result,
                {"version": "1.0.0", "tag": "v1.0.0", "already_prepared": True},
            )
            self.assertEqual(version_tag.project_version(root), "1.0.0")

    def test_release_candidate_requires_prepared_next_patch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root)
            version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="prepared",
            )
            before = {
                path: (root / path).read_bytes()
                for path in (
                    "CHANGELOG.md",
                    "pyproject.toml",
                    "skills/ads-plan-monitor/src/ocean_watch/__init__.py",
                    ".codex-plugin/plugin.json",
                )
            }

            result = version_tag.validate_release_candidate(root, "v1.0.0")

            self.assertEqual(result["tag"], "v1.0.1")
            self.assertFalse(result["already_released"])
            self.assertEqual(before, {path: (root / path).read_bytes() for path in before})

    def test_release_candidate_allows_idempotent_rerun(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root, pending=False)

            result = version_tag.validate_release_candidate(root, "v1.0.0")

            self.assertEqual(result["tag"], "v1.0.0")
            self.assertTrue(result["already_released"])

    def test_release_candidate_rejects_unprepared_or_skipped_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.release_fixture(root)
            with self.assertRaisesRegex(version_tag.VersionTagError, "未发布段落"):
                version_tag.validate_release_candidate(root, "v1.0.0")

            version_tag.prepare_release(
                root,
                latest_tag="v1.0.0",
                release_date="2026-07-29",
                cachebuster="prepared",
            )
            (root / "pyproject.toml").write_text(
                '[project]\nname = "ocean-watch"\nversion = "1.0.2"\n',
                encoding="utf-8",
            )
            (root / "skills/ads-plan-monitor/src/ocean_watch/__init__.py").write_text(
                '__version__ = "1.0.2"\n',
                encoding="utf-8",
            )
            plugin = json.loads((root / ".codex-plugin/plugin.json").read_text(encoding="utf-8"))
            plugin["version"] = "1.0.2+codex.prepared"
            (root / ".codex-plugin/plugin.json").write_text(
                json.dumps(plugin) + "\n",
                encoding="utf-8",
            )
            with self.assertRaisesRegex(version_tag.VersionTagError, "next patch"):
                version_tag.validate_release_candidate(root, "v1.0.0")

    def test_release_workflow_publishes_an_immutable_changelog_release(self):
        workflow = (ROOT / ".github/workflows/tag.yml").read_text(encoding="utf-8")
        parsed = yaml.load(workflow, Loader=yaml.BaseLoader)

        self.assertIn("name: Release", workflow)
        self.assertIn(parsed["on"]["workflow_dispatch"], (None, ""))
        self.assertNotIn("\n  push:\n", workflow)
        self.assertIn("scripts/version_tag.py release-check", workflow)
        self.assertIn("releases/latest", workflow)
        self.assertIn("github.ref == 'refs/heads/main'", workflow)
        self.assertNotIn("git commit", workflow)
        self.assertNotIn("git push origin HEAD:main", workflow)
        self.assertNotIn("github-actions[bot]", workflow)
        self.assertIn("Verify immutable release source", workflow)
        self.assertIn('git status --porcelain', workflow)
        self.assertIn('git/ref/heads/main', workflow)
        self.assertIn('refs/tags/${VERSION_TAG}^{commit}', workflow)
        self.assertIn("Publish immutable version Tag", workflow)
        self.assertNotIn("git push", workflow)
        self.assertIn('repos/${GITHUB_REPOSITORY}/git/refs', workflow)
        self.assertIn('-f "ref=refs/tags/${VERSION_TAG}"', workflow)
        self.assertIn('-f "sha=${RELEASE_SHA}"', workflow)
        self.assertNotIn("scripts/release/build_candidate.py", workflow)
        self.assertIn("scripts/version_tag.py notes", workflow)
        self.assertIn('gh release create "${VERSION_TAG}"', workflow)
        self.assertIn("Existing Release notes do not match CHANGELOG.md.", workflow)
        self.assertNotIn("sealed_run_id", workflow)
        self.assertNotIn("gh run download", workflow)
        self.assertNotIn("sealed_release.py", workflow)
        self.assertNotIn("environment:", workflow)
        self.assertNotIn("--clobber", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_PUBLIC_KEY", workflow)
        self.assertEqual(workflow.count("contents: write"), 1)
        self.assertEqual(set(parsed["jobs"]), {"release"})
        self.assertEqual(parsed["jobs"]["release"]["permissions"]["contents"], "write")
        self.assertIn("persist-credentials: false", workflow)
        self.assertNotIn("persist-credentials: true", workflow)


if __name__ == "__main__":
    unittest.main()
