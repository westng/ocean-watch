import hashlib
import json
import unittest
from pathlib import Path

import yaml
from ocean_watch.accounts import manage_accounts
from ocean_watch.cli import main as cli
from ocean_watch.plans import batch_qianchuan_work_plans

ROOT = Path(__file__).resolve().parents[1]


class MigrationContractTests(unittest.TestCase):
    def test_command_manifest_covers_current_python_cli(self):
        manifest = yaml.safe_load((ROOT / "contracts" / "commands.yaml").read_text(encoding="utf-8"))
        expected = [f"{domain} {action}" for domain, action in cli.COMMANDS]
        actual = [row["command"] for row in manifest["commands"]]

        self.assertEqual(manifest["command_count"], 75)
        self.assertEqual(actual, expected)
        for row in manifest["commands"]:
            help_result = row["help"]
            self.assertEqual(help_result["exit_code"], 0, row["command"])
            self.assertEqual(help_result["stderr"], "", row["command"])
            self.assertEqual(
                help_result["stdout_sha256"],
                hashlib.sha256(help_result["stdout"].encode("utf-8")).hexdigest(),
                row["command"],
            )

    def test_mandatory_presentation_goldens_match_current_renderers(self):
        managed = manage_accounts.list_presentation([], include_disabled=False)["rendered_markdown"]
        batch = batch_qianchuan_work_plans.render_presentation_table([])

        self.assertEqual(
            managed + "\n",
            (ROOT / "contracts" / "presentation" / "managed-accounts-empty.md").read_text(
                encoding="utf-8"
            ),
        )
        self.assertEqual(
            batch + "\n",
            (ROOT / "contracts" / "presentation" / "qianchuan-batch-empty.md").read_text(
                encoding="utf-8"
            ),
        )

    def test_contract_schemas_are_valid_json_documents(self):
        for path in sorted((ROOT / "contracts" / "output").glob("*.schema.json")):
            with self.subTest(path=path.name):
                schema = json.loads(path.read_text(encoding="utf-8"))
                self.assertEqual(schema["$schema"], "https://json-schema.org/draft/2020-12/schema")
                self.assertEqual(schema["type"], "object")


if __name__ == "__main__":
    unittest.main()
