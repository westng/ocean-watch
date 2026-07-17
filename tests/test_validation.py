import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import (
    authorization_store,
    channels,
    credential_store,
)
from ocean_watch.onboarding import first_run, validate_config
from ocean_watch.plans import batch_create_from_today_videos
from ocean_watch.templates import plan_templates

from tests.support import business_template_config, only_plan_template_name, valid_config


class ExitAndValidationTests(unittest.TestCase):
    def legacy_template_config(self):
        config = valid_config()
        config["defaults"]["product_id"] = "unique-product-1"
        name = "legacy"
        config["plan_templates"] = {
            name: {**plan_templates.section_bundle(config)},
        }
        return config, name

    def test_no_qualified_videos_is_failure(self):
        self.assertEqual(batch_create_from_today_videos.batch_exit_code([{"status": "no_qualified_videos"}]), 1)
        self.assertEqual(batch_create_from_today_videos.batch_exit_code([{"status": "completed"}]), 0)

    def test_validator_modes_are_independent(self):
        config, name = self.legacy_template_config()
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(config, credentials, plan_template=name)
        self.assertTrue(validate_config.mode_is_ready(result, "all"))

        incomplete = copy.deepcopy(config)
        incomplete["plan_templates"][name]["links"]["open_url"] = (
            "https://example.com/open"
        )
        result = validate_config.validate_config(
            incomplete,
            credentials,
            plan_template=name,
        )
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-submit"))
        self.assertFalse(validate_config.mode_is_ready(result, "all"))

    def test_v2_without_business_template_keeps_query_ready(self):
        config = plan_templates.migrate(valid_config())
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(config, credentials)
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-preview"))
        self.assertIn("explicit plan template", result["plan_template_error"])

    def test_v2_mismatched_template_keeps_query_ready(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        config["account"]["advertiser_id"] = 999
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(
            config,
            credentials,
            plan_template=name,
        )
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-submit"))
        self.assertIn("bound to advertiser 1234567890", result["plan_template_error"])

    def test_first_run_uses_same_unique_product_rule(self):
        config = valid_config()
        credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
        with mock.patch.object(credential_store, "read_credentials", return_value=credentials):
            _, create_missing, _ = first_run.check_fields(config)
        self.assertNotIn("resolved_ids.product_platform_id", create_missing)

    def test_first_run_blocks_unknown_template(self):
        config = valid_config()
        credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
        with mock.patch.object(credential_store, "read_credentials", return_value=credentials):
            _, create_missing, _ = first_run.check_fields(
                config,
                plan_template="missing-template",
            )
        self.assertTrue(any(item.startswith("plan template:") for item in create_missing))

    def test_first_run_requires_template_selection(self):
        self.assertEqual(
            first_run.next_action(["account.advertiser_id"], []),
            "edit_config",
        )
        self.assertEqual(
            first_run.next_action([], []),
            "create_business_template",
        )
        self.assertEqual(
            first_run.next_action([], [{"name": "ready"}]),
            "select_business_template",
        )

    def test_validator_main_returns_selected_mode_status(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = valid_config()
            config["links"]["open_url"] = "https://example.com/open"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
            runtime = channels.runtime_config(config, "marketing")
            runtime["api"].update(credentials)
            with mock.patch.object(authorization_store, "attach_runtime", return_value=runtime), \
                    mock.patch.object(authorization_store, "read_app", return_value=credentials), \
                    mock.patch.object(credential_store, "status", return_value={}), \
                    redirect_stdout(StringIO()):
                self.assertEqual(
                    validate_config.main(["--config", str(config_path), "--mode", "query"]),
                    0,
                )
                self.assertEqual(
                    validate_config.main(
                        ["--config", str(config_path), "--mode", "create-submit"]
                    ),
                    1,
                )

    def test_validator_accepts_qianchuan_material_query_capability(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = channels.migrate_config(valid_config())
            config["account"]["channel"] = "qianchuan"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
            runtime = channels.runtime_config(
                config,
                "qianchuan",
                capability="qianchuan_materials",
            )
            runtime["api"].update(credentials)
            output = StringIO()
            with mock.patch.object(
                authorization_store,
                "attach_runtime",
                return_value=runtime,
            ), mock.patch.object(
                authorization_store,
                "read_app",
                return_value=credentials,
            ), mock.patch.object(
                credential_store,
                "status",
                return_value={},
            ), redirect_stdout(output):
                exit_code = validate_config.main(
                    ["--config", str(config_path), "--mode", "query"]
                )
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertTrue(result["selected_mode_ready"])
        self.assertEqual(result["channel"], "qianchuan")
