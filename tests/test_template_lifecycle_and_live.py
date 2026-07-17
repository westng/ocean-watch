import copy
import unittest
from types import SimpleNamespace
from unittest import mock

from ocean_watch.core.errors import ConfigurationError
from ocean_watch.plans import create_qianchuan_plan
from ocean_watch.templates import (
    manage_qianchuan_live_templates,
    manage_template_lifecycle,
    qianchuan_live_templates,
    qianchuan_product_templates,
    template_channel_router,
)

from tests.support import business_template_config, only_plan_template_name


class TemplateLifecycleAndLiveTests(unittest.TestCase):
    def test_marketing_delete_is_pure_and_reports_target(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        original = copy.deepcopy(config)
        updated, result = manage_template_lifecycle.delete_template(
            config,
            "marketing",
            name,
        )
        self.assertEqual(config, original)
        self.assertNotIn(name, updated["plan_templates"])
        self.assertEqual(result["template"], name)

    def test_marketing_delete_blocks_referenced_template(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        dependent = copy.deepcopy(config["plan_templates"][name])
        dependent_name = name.replace("test product", "other product")
        dependent["display_name"] = dependent_name
        dependent["bindings"]["product_name"] = "other product"
        dependent["created_from"] = {"template": name}
        config["plan_templates"][dependent_name] = dependent
        with self.assertRaisesRegex(ConfigurationError, "referenced"):
            manage_template_lifecycle.delete_template(
                config,
                "marketing",
                name,
            )

    def test_marketing_delete_blocks_corrupt_dependent_template(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        config["plan_templates"]["invalid-dependent"] = {"bindings": "invalid"}
        with self.assertRaisesRegex(ConfigurationError, "dependent Marketing template"):
            manage_template_lifecycle.delete_template(config, "marketing", name)

    def test_validation_reports_corrupt_templates_without_crashing(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        config["plan_templates"][name]["bindings"] = "invalid"
        result = manage_template_lifecycle.validate_templates(
            config,
            channel="marketing",
        )
        self.assertFalse(result["ok"])
        self.assertFalse(result["channels"][0]["templates"][0]["valid"])

    def test_qianchuan_validation_includes_both_default_skeletons(self):
        config = qianchuan_live_templates.ensure_config({})
        config = qianchuan_product_templates.ensure_config(config)
        result = manage_template_lifecycle.validate_templates(
            config,
            channel="qianchuan",
        )
        channel = result["channels"][0]
        self.assertTrue(result["ok"])
        self.assertEqual(len(channel["default_skeletons"]), 2)
        self.assertTrue(all(row["valid"] for row in channel["default_skeletons"]))

    def test_live_template_builds_valid_official_payload(self):
        template = qianchuan_live_templates.build_business_template(
            "123456",
            "creator",
            "987654",
        )
        payload = qianchuan_live_templates.payload_from_template(template)
        normalized, blockers = create_qianchuan_plan.normalize_and_validate(payload)
        self.assertEqual(blockers, ())
        self.assertEqual(normalized["marketing_goal"], "LIVE_PROM_GOODS")
        self.assertNotIn("name", normalized)
        self.assertEqual(normalized["creative_setting"]["smart_select_material"], True)

    def test_live_template_rejects_material_free_manual_selection(self):
        source = qianchuan_live_templates.default_template()
        source["creative_setting"]["smart_select_material"] = False
        with self.assertRaisesRegex(ConfigurationError, "require smart_select_material"):
            qianchuan_live_templates.build_business_template(
                "123456",
                "creator",
                "987654",
                source=source,
            )

    def test_live_template_wizard_collects_delivery_settings(self):
        answers = iter([
            "0",
            "123456",
            "creator",
            "987654",
            "6000",
            "0",
            "2.0",
            "y",
        ])
        config, result = manage_qianchuan_live_templates.run_create_wizard(
            {},
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        self.assertTrue(result["created"])
        template = config[qianchuan_live_templates.TEMPLATES_KEY][
            result["template_id"]
        ]
        self.assertEqual(template["delivery_setting"]["budget"], 6000.0)
        self.assertEqual(template["delivery_setting"]["roi2_goal"], 2.0)
        self.assertNotIn("daily_delivery_time", template["delivery_setting"])

    def test_create_payload_source_resolves_live_template(self):
        template = qianchuan_live_templates.build_business_template(
            "123456",
            "creator",
            "987654",
            template_id="qclt_test",
        )
        config = qianchuan_live_templates.ensure_config({})
        config[qianchuan_live_templates.TEMPLATES_KEY]["qclt_test"] = template
        args = SimpleNamespace(
            payload_file=None,
            payload_json=None,
            plan_template=None,
            live_template="qclt_test",
            name=None,
        )
        payload, summary = create_qianchuan_plan.load_payload_source(args, config)
        self.assertEqual(payload["aweme_id"], 987654)
        self.assertEqual(summary["template_type"], qianchuan_live_templates.TEMPLATE_TYPE)

    def test_router_sends_live_template_to_live_manager(self):
        with mock.patch.object(
            template_channel_router.manage_qianchuan_live_templates,
            "main",
            return_value=0,
        ) as handler:
            code = template_channel_router.main([
                "create",
                "--channel",
                "qianchuan",
                "--template-type",
                "live",
                "--config",
                "example.json",
            ])
        self.assertEqual(code, 0)
        handler.assert_called_once_with(["create-wizard", "--config", "example.json"])


if __name__ == "__main__":
    unittest.main()
