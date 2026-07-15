import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.cli import main as cli
from ocean_watch.templates import manage_plan_templates, manage_qianchuan_templates

from tests.support import business_template_config


class CliTests(unittest.TestCase):
    def test_forwards_domain_arguments(self):
        handler = mock.Mock(return_value=0)
        with mock.patch.dict(cli.COMMANDS, {
            ("setup", "validate"): (handler, (), "Validate configuration readiness"),
        }):
            code = cli.main(["setup", "validate", "--config", "example.json", "--mode", "query"])
        self.assertEqual(code, 0)
        handler.assert_called_once_with(["--config", "example.json", "--mode", "query"])

    def test_prefixes_action_arguments(self):
        handler = mock.Mock(return_value=0)
        with mock.patch.dict(cli.COMMANDS, {
            ("templates", "list"): (handler, ("list",), "List plan templates"),
        }):
            code = cli.main(["templates", "list", "--config", "example.json"])
        self.assertEqual(code, 0)
        handler.assert_called_once_with(["list", "--config", "example.json"])

    def test_structures_unexpected_errors(self):
        handler = mock.Mock(side_effect=RuntimeError("failed"))
        with mock.patch.dict(cli.COMMANDS, {
            ("setup", "validate"): (handler, (), "Validate configuration readiness"),
        }), redirect_stdout(StringIO()) as output:
            code = cli.main(["setup", "validate"])
        self.assertEqual(code, 1)
        payload = json.loads(output.getvalue())
        self.assertEqual(payload["error"]["code"], "unexpected_error")

    def test_marketing_template_wizard_rejects_stale_config(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            initial = business_template_config()
            path.write_text(json.dumps(initial), encoding="utf-8")

            def concurrent_wizard(config):
                concurrent = copy.deepcopy(config)
                concurrent["concurrent_update"] = "preserved"
                path.write_text(json.dumps(concurrent), encoding="utf-8")
                updated = copy.deepcopy(config)
                updated["wizard_update"] = "must-not-win"
                return updated, {"changed": True}

            with mock.patch.object(
                manage_plan_templates,
                "run_create_wizard",
                side_effect=concurrent_wizard,
            ), redirect_stdout(StringIO()) as output:
                code = cli.main([
                    "templates",
                    "create",
                    "--config",
                    str(path),
                ])

            result = json.loads(output.getvalue())
            persisted = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(code, 2)
            self.assertEqual(result["error"]["code"], "configuration_conflict")
            self.assertIn("changed", result["error"]["message"])
            self.assertEqual(persisted["concurrent_update"], "preserved")
            self.assertNotIn("wizard_update", persisted)

    def test_qianchuan_template_wizard_rejects_stale_config(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text("{}\n", encoding="utf-8")

            def concurrent_wizard(config):
                path.write_text(
                    json.dumps({"concurrent_update": "preserved"}),
                    encoding="utf-8",
                )
                updated = copy.deepcopy(config)
                updated["wizard_update"] = "must-not-win"
                return updated, {"created": True}

            with mock.patch.object(
                manage_qianchuan_templates,
                "run_create_wizard",
                side_effect=concurrent_wizard,
            ), redirect_stdout(StringIO()) as output:
                code = cli.main([
                    "qc-templates",
                    "create",
                    "--config",
                    str(path),
                ])

            result = json.loads(output.getvalue())
            persisted = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(code, 2)
            self.assertEqual(result["error"]["code"], "configuration_conflict")
            self.assertIn("changed", result["error"]["message"])
            self.assertEqual(persisted, {"concurrent_update": "preserved"})


if __name__ == "__main__":
    unittest.main()
