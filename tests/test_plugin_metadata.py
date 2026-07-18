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
        self.assertEqual(entry["policy"]["authentication"], "ON_USE")

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

    def test_responsible_account_skills_keep_semantic_intent_contract(self):
        for skill_name in ("ads-plan-monitor", "qc-plan-monitor"):
            with self.subTest(skill=skill_name):
                content = (
                    REPO_ROOT / "skills" / skill_name / "SKILL.md"
                ).read_text(encoding="utf-8")
                self.assertIn("semantic responsible-account intent", content)
                self.assertIn("not an exact or exhaustive keyword list", content)
                self.assertIn("during the current turn", content)
                self.assertIn("fixed sentence, field list, or Markdown layout", content)

    def test_qianchuan_plan_report_keeps_default_presentation_contract(self):
        content = (
            REPO_ROOT / "skills" / "qc-plan-monitor" / "SKILL.md"
        ).read_text(encoding="utf-8")
        self.assertIn("presentation.rendered_markdown", content)
        self.assertIn("Do not omit, merge, rename, reorder", content)
        self.assertIn("generic request for plan spend does not authorize simplification", content)

    def test_packaged_first_run_resource_is_valid_json(self):
        resource = resources.files("ocean_watch.resources").joinpath("config.example.json")
        with resource.open(encoding="utf-8") as stream:
            payload = json.load(stream)
        self.assertIsInstance(payload, dict)
        self.assertIn("channels", payload)

    def test_default_marketing_regions_exclude_requested_top_level_regions(self):
        asset = load_json(
            REPO_ROOT / "skills" / "ads-plan-monitor" / "assets" / "config.example.json"
        )
        resource = resources.files("ocean_watch.resources").joinpath("config.example.json")
        with resource.open(encoding="utf-8") as stream:
            packaged = json.load(stream)
        self.assertEqual(packaged, asset)

        resolved = asset["default_plan_template"]["resolved_ids"]
        product_info = asset["default_plan_template"]["defaults"]["product_info"]
        self.assertEqual(product_info["product_image_type"], "DPA")
        self.assertEqual(product_info["product_image_fields"], ["images_url"])
        self.assertNotIn("product_image_ids", resolved)
        expected = [
            (11, "北京"),
            (12, "天津"),
            (13, "河北"),
            (14, "山西"),
            (15, "内蒙古"),
            (21, "辽宁"),
            (22, "吉林"),
            (23, "黑龙江"),
            (31, "上海"),
            (32, "江苏"),
            (33, "浙江"),
            (34, "安徽"),
            (35, "福建"),
            (36, "江西"),
            (37, "山东"),
            (41, "河南"),
            (42, "湖北"),
            (43, "湖南"),
            (44, "广东"),
            (45, "广西"),
            (46, "海南"),
            (50, "重庆"),
            (51, "四川"),
            (52, "贵州"),
            (53, "云南"),
            (61, "陕西"),
            (62, "甘肃"),
            (63, "青海"),
            (64, "宁夏"),
        ]
        self.assertEqual(len(resolved["city_ids"]), len(resolved["city_names"]))
        self.assertEqual(list(zip(resolved["city_ids"], resolved["city_names"])), expected)
        self.assertTrue({54, 65, 71, 81, 82}.isdisjoint(resolved["city_ids"]))


if __name__ == "__main__":
    unittest.main()
