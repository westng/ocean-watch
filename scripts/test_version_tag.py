import json
import tempfile
import unittest
from pathlib import Path

from scripts.version_tag import (
    VersionTagError,
    prepare_release,
    release_notes,
    validate_release_candidate,
    validate_versions,
    version_tuple,
)


class HarnessVersionTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="ocean-watch-version-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        (self.root / ".codex-plugin").mkdir()
        (self.root / "pyproject.toml").write_text(
            '[project]\nversion = "1.0.9"\n', encoding="utf-8"
        )
        (self.root / ".codex-plugin" / "plugin.json").write_text(
            json.dumps({"name": "ocean-watch", "version": "1.0.9+codex.fixture"}),
            encoding="utf-8",
        )
        self.harness = {
            "name": "ocean-watch",
            "version": "1.0.9",
            "peerDependencies": {"@deepseek-ai/dsh-mcp-client": "0.1.7-rc.2"},
        }
        self.write_harness()
        (self.root / "CHANGELOG.md").write_text(
            "# 更新日志\n\n## 未发布\n\n### 新增\n\n- Harness support.\n"
            "\n## 1.0.9 - 2026-09-24\n\n- Previous release.\n",
            encoding="utf-8",
        )

    def write_harness(self):
        (self.root / "package.json").write_text(json.dumps(self.harness), encoding="utf-8")

    def test_release_updates_product_version_without_changing_host_compatibility(self):
        result = prepare_release(self.root, "v1.0.9", "2026-09-27", "fixture")
        package = json.loads((self.root / "package.json").read_text(encoding="utf-8"))
        self.assertEqual(result["version"], "1.0.10")
        self.assertEqual(package["version"], "1.0.10")
        self.assertEqual(package["peerDependencies"], self.harness["peerDependencies"])
        self.assertEqual(validate_versions(self.root)["harness"], "1.0.10")

    def test_mismatched_harness_version_stops_release_before_mutating_files(self):
        self.harness["version"] = "1.0.8"
        self.write_harness()
        before = (self.root / "CHANGELOG.md").read_bytes()
        with self.assertRaises(VersionTagError):
            prepare_release(self.root, "v1.0.9", "2026-09-27", "fixture")
        self.assertEqual((self.root / "CHANGELOG.md").read_bytes(), before)

    def test_alpha_preparation_maps_python_and_semver_and_release_notes(self):
        result = prepare_release(self.root, "v1.0.9", "2026-11-07", "fixture", "1.1.0-alpha.1")
        self.assertEqual(result["tag"], "v1.1.0-alpha.1")
        self.assertEqual(validate_versions(self.root)["project"], "1.1.0a1")
        self.assertEqual(validate_versions(self.root)["harness"], "1.1.0-alpha.1")
        self.assertTrue(validate_release_candidate(self.root, "v1.0.9")["prerelease"])
        self.assertIn("Harness support.", release_notes(self.root, result["tag"]))
        repeated = prepare_release(self.root, "v1.0.9", "2026-11-07", "fixture", "1.1.0-alpha.1")
        self.assertTrue(repeated["already_prepared"])

    def test_prereleases_order_before_stable_and_serial_is_numeric(self):
        versions = ["1.1.0-alpha.2", "1.1.0-alpha.10", "1.1.0-beta.1", "1.1.0-rc.1", "1.1.0", "1.1.1-alpha.1"]
        self.assertEqual(sorted(reversed(versions), key=version_tuple), versions)

    def test_invalid_or_older_prerelease_does_not_mutate(self):
        for target in ("1.0.9-alpha.1", "1.1.0-alpha.0", "1.1.0-alpha.01", "1.01.0-alpha.1", "1.1.0-dev.1"):
            with self.subTest(target=target):
                before = (self.root / "CHANGELOG.md").read_bytes()
                with self.assertRaises(VersionTagError):
                    prepare_release(self.root, "v1.0.9", "2026-11-07", "fixture", target)
                self.assertEqual((self.root / "CHANGELOG.md").read_bytes(), before)

    def test_release_preparation_updates_claude_and_marketplace(self):
        directory = self.root / ".claude-plugin"
        directory.mkdir()
        (directory / "plugin.json").write_text(json.dumps({"name":"ocean-watch", "version":"1.0.9+codex.fixture"}))
        (directory / "marketplace.json").write_text(json.dumps({"plugins":[{"name":"ocean-watch", "version":"1.0.9+codex.fixture"}]}))
        prepare_release(self.root, "v1.0.9", "2026-11-07", "fixture", "1.1.0-alpha.1")
        self.assertEqual(json.loads((directory / "plugin.json").read_text())["version"], "1.1.0-alpha.1+codex.fixture")
        self.assertEqual(json.loads((directory / "marketplace.json").read_text())["plugins"][0]["version"], "1.1.0-alpha.1+codex.fixture")


if __name__ == "__main__":
    unittest.main()
