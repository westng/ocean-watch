import json
import tempfile
import unittest
from pathlib import Path

from scripts.version_tag import VersionTagError, prepare_release, validate_versions


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


if __name__ == "__main__":
    unittest.main()
