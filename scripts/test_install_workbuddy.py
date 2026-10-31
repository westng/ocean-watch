"""Synthetic-only installer regression checks; never touches an actual profile."""
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("install_workbuddy", Path(__file__).with_name("install-workbuddy.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="workbuddy fixture ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "source"
        self.host = Path(self.temporary.name) / "host"
        self.state = Path(self.temporary.name) / "shared state"
        for path, content in {
            ".codex-plugin/plugin.json": '{"name":"ocean-watch","version":"1.0.9+fixture"}',
            ".codex-plugin/bin/ocean-watch_darwin_arm64": "fixture binary, not executable",
            "f2/resolve.py": "# fixture",
            "hosts/workbuddy/skill.md": (Path(__file__).resolve().parents[1] / "hosts/workbuddy/skill.md").read_text(encoding="utf-8"),
        }.items():
            self.write(self.root / path, content)
        for name in ("ads-plan-monitor", "qc-plan-monitor", "xt-plan-monitor"):
            tool = "get_star_map_authorization" if name == "xt-plan-monitor" else "list_templates"
            self.write(self.root / "skills" / name / "SKILL.md", f"---\nname: {name}\ndescription: fixture\n---\n<!-- capability-routes:start -->\n| fixture | MCP `{tool}` |\n| unsupported | `./run unsupported` |\n<!-- capability-routes:end -->")
        resources = {path: installer.digest(self.root / path) for path in ("f2/resolve.py", "skills/ads-plan-monitor/SKILL.md", "skills/qc-plan-monitor/SKILL.md", "skills/xt-plan-monitor/SKILL.md")}
        self.write(self.root / ".codex-plugin/runtime-manifest.json", json.dumps({
            "schema_version":1, "version":"1.0.9+fixture", "plugin":{"name":"ocean-watch", "version":"1.0.9+fixture", "sha256":installer.digest(self.root / ".codex-plugin/plugin.json")},
            "sha256":{"ocean-watch_darwin_arm64":installer.digest(self.root / ".codex-plugin/bin/ocean-watch_darwin_arm64")}, "resources":resources,
        }))
        self.write(self.host / "mcp.json", json.dumps({"mcpServers":{"other":{"command":"keep"},"ocean-watch-preview":{"command":"preview"}},"extra":"keep"}))
        self.write(self.host / "skills/ocean-watch-preview/SKILL.md", "preview fixture")
        self.write(self.host / "mcp-tool-policy.json", "native policy fixture")

    @staticmethod
    def write(path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def install(self):
        with patch.object(installer.platform, "system", return_value="Darwin"), patch.object(installer.platform, "machine", return_value="arm64"):
            return installer.install(self.root, self.host, self.state)

    def test_verified_install_preserves_other_services_trust_and_shared_root(self):
        result = self.install()
        config = json.loads((self.host / "mcp.json").read_text(encoding="utf-8"))
        self.assertEqual(config["mcpServers"]["other"], {"command":"keep"})
        self.assertEqual(config["extra"], "keep")
        self.assertNotIn("ocean-watch-preview", config["mcpServers"])
        service = config["mcpServers"]["ocean-watch"]
        self.assertEqual(service["env"]["OCEAN_WATCH_HOME"], str(self.state))
        self.assertEqual(service["args"], ["mcp", "serve", "--stdio"])
        self.assertTrue(Path(service["command"]).is_file())
        self.assertEqual((self.host / "mcp-tool-policy.json").read_text(encoding="utf-8"), "native policy fixture")
        self.assertFalse(self.state.exists())
        self.assertFalse((self.host / "skills/ocean-watch-preview").exists())
        self.assertTrue((Path(result["backup"]) / "ocean-watch-preview/SKILL.md").is_file())
        skill = (self.host / "skills/qc-plan-monitor/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("MCP `start_authorization`", skill)
        self.assertIn("MCP `list_templates`", skill)
        self.assertIn("channel=qianchuan", skill)
        self.assertNotIn("./run unsupported", skill)
        star = (self.host / "skills/xt-plan-monitor/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("channel=star_map", star)
        self.assertIn("MCP `get_star_map_authorization`", star)
        self.assertNotIn("channel=marketing", star)
        self.assertNotIn("{{", star)
        self.assertNotIn("./run unsupported", star)
        self.assertEqual(self.install()["install"], result["install"])

    def test_tampered_runtime_is_rejected_before_profile_mutation(self):
        before = (self.host / "mcp.json").read_bytes()
        self.write(self.root / ".codex-plugin/bin/ocean-watch_darwin_arm64", "tampered fixture")
        with self.assertRaises(ValueError):
            self.install()
        self.assertEqual((self.host / "mcp.json").read_bytes(), before)
        self.assertFalse((self.host / "skills/qc-plan-monitor").exists())

    def test_resource_change_creates_new_pinned_installation(self):
        first = self.install()
        manifest_path = self.root / ".codex-plugin/runtime-manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.write(self.root / "f2/resolve.py", "# changed resource fixture")
        manifest["resources"]["f2/resolve.py"] = installer.digest(self.root / "f2/resolve.py")
        self.write(manifest_path, json.dumps(manifest))
        second = self.install()
        self.assertEqual(first["sha256"], second["sha256"])
        self.assertNotEqual(first["manifest_sha256"], second["manifest_sha256"])
        self.assertNotEqual(first["install"], second["install"])
        self.assertEqual((Path(first["install"]) / "f2/resolve.py").read_text(encoding="utf-8"), "# fixture")
        self.assertEqual((Path(second["install"]) / "f2/resolve.py").read_text(encoding="utf-8"), "# changed resource fixture")
        self.assertEqual(self.install()["install"], second["install"])

    def test_tampered_star_skill_is_rejected_before_profile_mutation(self):
        before = (self.host / "mcp.json").read_bytes()
        self.write(self.root / "skills/xt-plan-monitor/SKILL.md", "tampered star fixture")
        with self.assertRaisesRegex(ValueError, "resources are stale"):
            self.install()
        self.assertEqual((self.host / "mcp.json").read_bytes(), before)
        self.assertFalse((self.host / "binaries").exists())


if __name__ == "__main__":
    unittest.main()
