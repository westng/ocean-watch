import json
import re
import unittest
from importlib import resources
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover - Python 3.9/3.10 only
    import tomli as tomllib

import yaml
from ocean_watch import __version__
from ocean_watch.integrations.mcp_streamable_http import StreamableHttpMcpClient

REPO_ROOT = Path(__file__).resolve().parents[1]
SKILL_FRONTMATTER = re.compile(r"\A---\n(.*?)\n---(?:\n|\Z)", re.DOTALL)


def load_json(path):
    with path.open(encoding="utf-8") as stream:
        return json.load(stream)


def load_yaml(path):
    with path.open(encoding="utf-8") as stream:
        return yaml.safe_load(stream)


class PluginMetadataTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = load_json(REPO_ROOT / ".codex-plugin" / "plugin.json")
        cls.marketplace = load_json(REPO_ROOT / ".agents" / "plugins" / "marketplace.json")
        with (REPO_ROOT / "pyproject.toml").open("rb") as stream:
            cls.project = tomllib.load(stream)["project"]

    def test_release_versions_are_consistent(self):
        self.assertEqual(self.project["version"], __version__)
        plugin_version = self.manifest["version"]
        self.assertEqual(plugin_version.split("+", 1)[0], __version__)
        self.assertRegex(plugin_version, rf"^{re.escape(__version__)}\+codex\.[a-z0-9-]+$")

        client = StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "test-token",
            tool_range=["test-tool"],
        )
        self.assertEqual(client.client_version, __version__)

    def test_plugin_manifest_starter_prompts_respect_contract(self):
        self.assertEqual(self.manifest["name"], "ocean-watch")
        self.assertEqual(self.manifest["skills"], "./skills/")
        prompts = self.manifest["interface"]["defaultPrompt"]
        self.assertGreaterEqual(len(prompts), 1)
        self.assertLessEqual(len(prompts), 3)
        self.assertTrue(all(isinstance(prompt, str) and 1 <= len(prompt) <= 128 for prompt in prompts))

    def test_marketplace_points_to_this_plugin(self):
        entries = [
            entry
            for entry in self.marketplace["plugins"]
            if entry.get("name") == self.manifest["name"]
        ]
        self.assertEqual(len(entries), 1)
        entry = entries[0]
        self.assertEqual(entry["source"], {"source": "local", "path": "."})
        self.assertEqual(entry["category"], self.manifest["interface"]["category"])
        self.assertIn(entry["policy"]["installation"], {"AVAILABLE", "INSTALLED_BY_DEFAULT"})
        self.assertIn(entry["policy"]["authentication"], {"ON_INSTALL", "ON_USE"})

    def test_each_skill_has_consistent_agent_metadata(self):
        skill_roots = sorted(path.parent for path in (REPO_ROOT / "skills").glob("*/SKILL.md"))
        self.assertEqual(
            [path.name for path in skill_roots],
            ["ads-plan-monitor", "qc-plan-monitor"],
        )
        for skill_root in skill_roots:
            with self.subTest(skill=skill_root.name):
                content = (skill_root / "SKILL.md").read_text(encoding="utf-8")
                match = SKILL_FRONTMATTER.match(content)
                self.assertIsNotNone(match)
                frontmatter = yaml.safe_load(match.group(1))
                self.assertEqual(frontmatter["name"], skill_root.name)
                self.assertTrue(frontmatter["description"].strip())
                self.assertLessEqual(len(frontmatter["description"]), 1024)

                agent = load_yaml(skill_root / "agents" / "openai.yaml")
                interface = agent["interface"]
                self.assertTrue(interface["display_name"].strip())
                self.assertGreaterEqual(len(interface["short_description"]), 25)
                self.assertLessEqual(len(interface["short_description"]), 64)
                self.assertIn(f"${skill_root.name}", interface["default_prompt"])

    def test_packaged_first_run_resource_is_valid_json(self):
        resource = resources.files("ocean_watch.resources").joinpath("config.example.json")
        with resource.open(encoding="utf-8") as stream:
            payload = json.load(stream)
        self.assertIsInstance(payload, dict)
        self.assertIn("channels", payload)


if __name__ == "__main__":
    unittest.main()
