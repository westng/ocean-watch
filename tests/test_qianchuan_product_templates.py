import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import channels
from ocean_watch.plans import create_qianchuan_plan
from ocean_watch.templates import manage_qianchuan_templates
from ocean_watch.templates import qianchuan_product_templates as product_templates

from tests.support import valid_config


class QianchuanProductTemplateTests(unittest.TestCase):
    def test_default_template_matches_confirmed_business_defaults(self):
        template = product_templates.default_template()
        self.assertFalse(template["business_usable"])
        self.assertEqual(template["bindings"]["product_ids"], [])
        self.assertEqual(template["delivery_setting"], {
            "smart_bid_type": "SMART_BID_CUSTOM",
            "roi2_goal": 1.7,
            "qcpx_mode": "QCPX_MODE_ON",
            "budget": 5000,
            "video_schedule_type": "SCHEDULE_FROM_NOW",
            "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
        })
        self.assertEqual(
            template["material_strategy"]["source_type"],
            "CREATOR_RUNTIME_QUERY",
        )
        self.assertEqual(
            template["plan_name_template"],
            "{month_day}-{creator_name}-{product_name}-{type}-{business}",
        )

    def test_product_ids_use_slash_format_and_official_limit(self):
        values = [str(1000 + index) for index in range(30)]
        normalized = product_templates.normalize_product_ids("/".join(values))
        self.assertEqual(normalized, values)
        with self.assertRaisesRegex(Exception, "at most 30"):
            product_templates.normalize_product_ids(values + ["9999"])

    def test_product_ids_are_deduplicated_in_input_order(self):
        self.assertEqual(
            product_templates.normalize_product_ids("123/456/123/789"),
            ["123", "456", "789"],
        )

    def test_business_template_name_contains_all_product_ids(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123/1231231231",
            template_id="qcpt_test",
            template_name="用户自定义模板",
        )
        self.assertEqual(
            template["display_name"],
            "用户自定义模板",
        )
        self.assertEqual(
            template["bindings"]["product_ids"],
            ["12123123123", "1231231231"],
        )

    def test_business_template_never_persists_runtime_or_channel_fields(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_test",
            template_name="用户自定义模板",
        )
        rendered = json.dumps(template)
        for forbidden in (
            "aweme_id",
            "product_channel_info",
            "channel_id",
            "channel_type",
            "multi_product_creative_list",
        ):
            self.assertNotIn(forbidden, rendered)

    def test_clone_inherits_delivery_but_rebinds_business_fields(self):
        source = product_templates.build_business_template(
            advertiser_id="111",
            product_name="来源产品",
            product_ids="1001",
            template_id="qcpt_source",
            template_name="来源模板",
        )
        source["delivery_setting"]["roi2_goal"] = 2.0
        target = product_templates.build_business_template(
            advertiser_id="222",
            product_name="目标产品",
            product_ids="2001/2002",
            source=source,
            template_id="qcpt_target",
            template_name="目标模板",
            plan_name_template="{creator_name}-{product_name}-{date}",
        )
        self.assertEqual(target["delivery_setting"]["roi2_goal"], 2.0)
        self.assertEqual(target["bindings"]["advertiser_id"], "222")
        self.assertEqual(target["bindings"]["product_ids"], ["2001", "2002"])
        self.assertEqual(
            target["plan_name_template"],
            "{creator_name}-{product_name}-{date}",
        )

    def test_unknown_plan_name_placeholder_is_rejected(self):
        with self.assertRaisesRegex(Exception, "unsupported placeholders"):
            product_templates.build_business_template(
                advertiser_id="1234567890123456",
                product_name="示例商品",
                product_ids="12123123123",
                template_id="qcpt_test",
                template_name="用户自定义模板",
                plan_name_template="{product_name}-{unknown_value}",
            )

    def test_wizard_creates_template_without_default_business_state(self):
        answers = iter([
            "0",
            "1234567890123456",
            "示例商品",
            "12123123123/1231231231",
            "用户自定义模板",
            "{product_name}-{creator_name}-{datetime}",
            "y",
        ])
        config, result = manage_qianchuan_templates.run_create_wizard(
            {},
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        self.assertTrue(result["created"])
        self.assertIn(result["template_id"], config[product_templates.TEMPLATES_KEY])
        self.assertNotIn(product_templates.LEGACY_ACTIVE_TEMPLATE_KEY, config)
        self.assertEqual(result["template"]["bindings"]["product_ids"], [
            "12123123123",
            "1231231231",
        ])
        self.assertEqual(result["name"], "用户自定义模板")

    def test_default_template_cannot_resolve_for_plan_creation(self):
        with self.assertRaisesRegex(Exception, "not found"):
            product_templates.resolve_template(
                product_templates.ensure_config({}),
                "default_qianchuan_product_template",
            )

    def test_template_resolution_requires_explicit_selector(self):
        with self.assertRaisesRegex(Exception, "explicit Qianchuan product template"):
            product_templates.resolve_template(product_templates.ensure_config({}))

    def test_template_generates_material_free_official_payload(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123/1231231231",
            template_id="qcpt_test",
            template_name="用户自定义模板",
        )
        payload = product_templates.payload_from_template(template, name="千川商品计划")
        self.assertEqual(payload["marketing_goal"], "VIDEO_PROM_GOODS")
        self.assertEqual(payload["product_ids"], [12123123123, 1231231231])
        self.assertEqual(payload["name"], "千川商品计划")
        self.assertNotIn("aweme_id", payload)
        self.assertNotIn("product_channel_info", payload)
        self.assertNotIn("multi_product_creative_list", payload)

    def test_plan_cli_dry_run_uses_template_without_credentials(self):
        config = channels.migrate_config(valid_config())
        config = product_templates.ensure_config(config)
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123/1231231231",
            template_id="qcpt_test",
            template_name="用户自定义模板",
        )
        config[product_templates.TEMPLATES_KEY] = {"qcpt_test": template}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(
                create_qianchuan_plan.token_manager,
                "ensure_access_token",
            ) as ensure_token, redirect_stdout(output):
                exit_code = create_qianchuan_plan.main([
                    "--config",
                    str(config_path),
                    "--plan-template",
                    "qcpt_test",
                    "--name",
                    "千川商品计划",
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["plan_template"]["template_id"], "qcpt_test")
        self.assertEqual(result["payload"]["product_ids"], [12123123123, 1231231231])
        self.assertEqual(result["blocking_fields"], ["runtime_creator_materials"])
        ensure_token.assert_not_called()

    def test_template_submit_blocks_before_credentials_without_runtime_materials(self):
        config = channels.migrate_config(valid_config())
        config = product_templates.ensure_config(config)
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_test",
            template_name="用户自定义模板",
        )
        config[product_templates.TEMPLATES_KEY] = {"qcpt_test": template}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(
                create_qianchuan_plan.token_manager,
                "ensure_access_token",
            ) as ensure_token, redirect_stdout(output):
                exit_code = create_qianchuan_plan.main([
                    "--config",
                    str(config_path),
                    "--plan-template",
                    "qcpt_test",
                    "--submit",
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 1)
        self.assertTrue(result["submit_blocked"])
        self.assertEqual(result["blocking_fields"], ["runtime_creator_materials"])
        ensure_token.assert_not_called()

    def test_schema_v1_template_migrates_to_shared_business_name(self):
        config = {
            product_templates.SCHEMA_VERSION_KEY: 1,
            product_templates.TEMPLATES_KEY: {
                "qcpt_test": {
                    "template_id": "qcpt_test",
                    "display_name": "旧店铺-商品全域-示例商品-12123123123",
                    "template_type": product_templates.TEMPLATE_TYPE,
                    "status": "active",
                    "bindings": {
                        "channel": "qianchuan",
                        "advertiser_id": "1234567890123456",
                        "shop_name": "旧店铺",
                        "product_name": "示例商品",
                        "product_ids": ["12123123123"],
                    },
                    "delivery_setting": product_templates.DEFAULT_DELIVERY_SETTING,
                    "material_strategy": {
                        "source_type": product_templates.MATERIAL_SOURCE_TYPE,
                        "persist_material_ids": False,
                    },
                }
            },
        }
        migrated = product_templates.ensure_config(config)
        template = migrated[product_templates.TEMPLATES_KEY]["qcpt_test"]
        self.assertEqual(migrated[product_templates.SCHEMA_VERSION_KEY], 6)
        self.assertNotIn("shop_name", template["bindings"])
        self.assertEqual(
            template["display_name"],
            "巨量千川-1234567890123456-示例商品-12123123123-商品全域",
        )
        self.assertEqual(
            template["plan_name_template"],
            product_templates.LEGACY_PLAN_NAME_TEMPLATE,
        )

    def test_schema_v2_template_migrates_name_without_changing_identity(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_test",
            template_name="临时模板",
        )
        template["display_name"] = "1234567890123456-商品全域-示例商品-12123123123"
        config = {
            product_templates.SCHEMA_VERSION_KEY: 2,
            product_templates.LEGACY_ACTIVE_TEMPLATE_KEY: "qcpt_test",
            product_templates.TEMPLATES_KEY: {"qcpt_test": template},
        }

        migrated = product_templates.ensure_config(config)

        self.assertEqual(migrated[product_templates.SCHEMA_VERSION_KEY], 6)
        self.assertNotIn(product_templates.LEGACY_ACTIVE_TEMPLATE_KEY, migrated)
        migrated_template = migrated[product_templates.TEMPLATES_KEY]["qcpt_test"]
        self.assertEqual(migrated_template["template_id"], "qcpt_test")
        self.assertEqual(
            migrated_template["display_name"],
            "巨量千川-1234567890123456-示例商品-12123123123-商品全域",
        )

    def test_schema_v2_name_migration_rejects_collisions(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_one",
            template_name="临时模板",
        )
        duplicate = copy.deepcopy(template)
        duplicate["template_id"] = "qcpt_two"
        config = {
            product_templates.SCHEMA_VERSION_KEY: 2,
            product_templates.TEMPLATES_KEY: {
                "qcpt_one": template,
                "qcpt_two": duplicate,
            },
        }

        with self.assertRaisesRegex(Exception, "naming collision"):
            product_templates.ensure_config(config)

    def test_schema_v4_migration_preserves_name_and_plan_name_behavior(self):
        template = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_test",
            template_name="用户旧千川模板",
        )
        template.pop("plan_name_template")
        config = {
            product_templates.SCHEMA_VERSION_KEY: 4,
            product_templates.TEMPLATES_KEY: {"qcpt_test": template},
        }

        migrated = product_templates.ensure_config(config)
        migrated_template = migrated[product_templates.TEMPLATES_KEY]["qcpt_test"]

        self.assertEqual(migrated_template["display_name"], "用户旧千川模板")
        self.assertEqual(
            migrated_template["plan_name_template"],
            product_templates.LEGACY_PLAN_NAME_TEMPLATE,
        )

    def test_schema_v5_upgrades_only_default_plan_name_template(self):
        default = product_templates.default_template()
        default["plan_name_template"] = product_templates.LEGACY_PLAN_NAME_TEMPLATE
        business = product_templates.build_business_template(
            advertiser_id="1234567890123456",
            product_name="示例商品",
            product_ids="12123123123",
            template_id="qcpt_test",
            template_name="用户旧千川模板",
            plan_name_template=product_templates.LEGACY_PLAN_NAME_TEMPLATE,
        )
        config = {
            product_templates.SCHEMA_VERSION_KEY: 5,
            product_templates.DEFAULT_TEMPLATE_KEY: default,
            product_templates.TEMPLATES_KEY: {"qcpt_test": business},
        }

        migrated = product_templates.ensure_config(config)

        self.assertEqual(migrated[product_templates.SCHEMA_VERSION_KEY], 6)
        self.assertEqual(
            migrated[product_templates.DEFAULT_TEMPLATE_KEY]["plan_name_template"],
            product_templates.DEFAULT_PLAN_NAME_TEMPLATE,
        )
        self.assertEqual(
            migrated[product_templates.TEMPLATES_KEY]["qcpt_test"][
                "plan_name_template"
            ],
            product_templates.LEGACY_PLAN_NAME_TEMPLATE,
        )

    def test_future_schema_is_not_downgraded(self):
        with self.assertRaisesRegex(Exception, "newer than supported"):
            product_templates.ensure_config({
                product_templates.SCHEMA_VERSION_KEY: 999,
            })


if __name__ == "__main__":
    unittest.main()
