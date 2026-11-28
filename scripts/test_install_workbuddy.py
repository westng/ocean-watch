"""Synthetic-only installer regression checks; never touches an actual profile."""
import importlib.util
import json
import os
import sys
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
        for tool in ("prepare_qianchuan_operation", "request_operation_confirmation",
                     "execute_operation", "get_operation", "reconcile_operation"):
            self.assertIn(tool, skill)
        self.assertIn("STATE_INCOMPATIBLE", skill)
        self.assertIn("available_topics", skill)
        self.assertIn("不猜 AUTHOR", skill)
        self.assertIn("official_sdk", skill)
        self.assertIn("旧引用立即作废", skill)
        self.assertNotIn("references/operations.md", skill)
        self.assertNotIn("创建计划、提交预检、预算/ROI/状态及删除不在", skill)
        marketing = (self.host / "skills/ads-plan-monitor/SKILL.md").read_text(encoding="utf-8")
        self.assertNotIn("execute_operation", marketing)
        star = (self.host / "skills/xt-plan-monitor/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("channel=star_map", star)
        self.assertIn("MCP `get_star_map_authorization`", star)
        self.assertNotIn("channel=marketing", star)
        self.assertNotIn("{{", star)
        self.assertNotIn("./run unsupported", star)
        self.assertNotIn("execute_operation", star)
        self.assertEqual(self.install()["install"], result["install"])

    def test_cli_upgrade_preserves_service_customizations_and_previously_selected_state(self):
        path = self.host / "mcp.json"
        self.install()
        config = json.loads(path.read_text(encoding="utf-8"))
        service = config["mcpServers"]["ocean-watch"]
        service.update(disabled=True, timeout=123, command="old-binary")
        custom_env = {"PATH": "/fixture/bin", "HOME": "/fixture/home", "OCEAN_WATCH_PYTHON": "/fixture/venv/python"}
        service["env"].update(custom_env)
        self.write(path, json.dumps(config))

        def run_cli(*args):
            with patch.object(installer, "__file__", str(self.root / "scripts/install-workbuddy.py")), \
                 patch.object(sys, "argv", ["install-workbuddy.py", "--workbuddy-home", str(self.host), *args]), \
                 patch.object(installer.platform, "system", return_value="Darwin"), \
                 patch.object(installer.platform, "machine", return_value="arm64"), \
                 patch.dict(os.environ, {"OCEAN_WATCH_HOME": str(self.host / "unrelated")}), patch("builtins.print"):
                installer.main()
            actual = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(actual["mcpServers"]["other"], {"command": "keep"})
            self.assertEqual(actual["extra"], "keep")
            current = actual["mcpServers"]["ocean-watch"]
            self.assertTrue(Path(current["command"]).is_file())
            self.assertEqual((current["disabled"], current["timeout"]), (True, 123))
            self.assertEqual({key: current["env"][key] for key in custom_env}, custom_env)
            self.assertEqual(current["env"]["OCEAN_WATCH_HOME"], current["env"]["CODEX_HOME"])
            return current["env"]["OCEAN_WATCH_HOME"]

        self.assertEqual(run_cli(), str(self.state))
        new_state = self.host / "another state"
        self.assertEqual(run_cli("--state-home", str(new_state)), str(new_state))
        self.assertEqual(run_cli(), str(new_state))

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
