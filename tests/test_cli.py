import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.cli import main as cli
from ocean_watch.integrations import qianchuan_work_metadata
from ocean_watch.onboarding import environment_check
from ocean_watch.templates import (
    list_channel_templates,
    manage_plan_templates,
    manage_qianchuan_templates,
    template_channel_router,
)

from tests.support import business_template_config


class CliTests(unittest.TestCase):
    def test_exposes_local_qianchuan_work_metadata_configuration(self):
        handler, prefix, description = cli.COMMANDS[("setup", "work-metadata")]
        self.assertIs(handler, qianchuan_work_metadata.main)
        self.assertEqual(prefix, ())
        self.assertIn("local Qianchuan work metadata", description)

    def test_exposes_environment_doctor(self):
        handler, prefix, description = cli.COMMANDS[("setup", "doctor")]
        self.assertIs(handler, environment_check.main)
        self.assertEqual(prefix, ())
        self.assertIn("runtime", description)

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

    def test_template_list_uses_single_all_channel_reader(self):
        handler, prefix, description = cli.COMMANDS[("templates", "list")]
        self.assertIs(handler, list_channel_templates.main)
        self.assertEqual(prefix, ())
        self.assertIn("Marketing and Qianchuan", description)

    def test_template_show_uses_shared_channel_reader(self):
        handler, prefix, description = cli.COMMANDS[("templates", "show")]
        self.assertIs(handler, list_channel_templates.show_main)
        self.assertEqual(prefix, ())
        self.assertIn("one Marketing or Qianchuan", description)

    def test_template_create_routes_explicit_marketing_channel(self):
        with mock.patch.object(
            template_channel_router.manage_plan_templates,
            "main",
            return_value=0,
        ) as handler:
            code = cli.main([
                "templates",
                "create",
                "--channel",
                "marketing",
                "--material-source-type",
                "ACCOUNT_UPLOAD",
                "--config",
                "example.json",
            ])

        self.assertEqual(code, 0)
        handler.assert_called_once_with([
            "create-wizard",
            "--config",
            "example.json",
            "--material-source-type",
            "ACCOUNT_UPLOAD",
        ])

    def test_template_create_routes_explicit_qianchuan_channel(self):
        with mock.patch.object(
            template_channel_router.manage_qianchuan_templates,
            "main",
            return_value=0,
        ) as handler:
            code = cli.main([
                "templates",
                "create",
                "--channel",
                "qianchuan",
                "--config",
                "example.json",
            ])

        self.assertEqual(code, 0)
        handler.assert_called_once_with(["create-wizard", "--config", "example.json"])

    def test_template_create_prompts_for_channel_before_source_template(self):
        output = []
        statuses = {
            "marketing": {
                "authorization_count": 1,
                "authorized_advertiser_count": 196,
            },
            "qianchuan": {
                "authorization_count": 0,
                "authorized_advertiser_count": 0,
            },
        }
        with mock.patch.object(
            template_channel_router.authorization_store,
            "load_channel_state",
            side_effect=lambda channel: {
                "authorizations": {
                    str(index): {}
                    for index in range(statuses[channel]["authorization_count"])
                },
                "advertiser_index": {
                    str(index + 1): []
                    for index in range(statuses[channel]["authorized_advertiser_count"])
                },
            },
        ):
            selected = template_channel_router.select_channel(
                input_fn=lambda _: "1",
                output_fn=output.append,
            )

        self.assertEqual(selected, "qianchuan")
        rendered = "\n".join(output)
        self.assertIn("巨量营销（已授权，196 个广告主）", rendered)
        self.assertIn("巨量千川（未授权，可先创建模板，投放前需授权）", rendered)

    def test_marketing_material_mode_is_selected_before_source_wizard(self):
        answers = iter(["0", "1"])
        with mock.patch.object(
            template_channel_router.authorization_store,
            "load_channel_state",
            return_value={},
        ), mock.patch.object(
            template_channel_router.manage_plan_templates,
            "main",
            return_value=0,
        ) as handler:
            code = template_channel_router.main(
                ["create", "--config", "example.json"],
                input_fn=lambda _: next(answers),
                output_fn=lambda _: None,
            )

        self.assertEqual(code, 0)
        handler.assert_called_once_with([
            "create-wizard",
            "--config",
            "example.json",
            "--material-source-type",
            "CREATOR_AUTHORIZED",
        ])

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

            def concurrent_wizard(
                config,
                material_source_type=None,
                authorization_state=None,
            ):
                self.assertEqual(material_source_type, "ACCOUNT_UPLOAD")
                self.assertIsInstance(authorization_state, dict)
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
                    "--channel",
                    "marketing",
                    "--material-source-type",
                    "ACCOUNT_UPLOAD",
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

            def concurrent_wizard(config, authorization_state=None):
                self.assertIsInstance(authorization_state, dict)
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
