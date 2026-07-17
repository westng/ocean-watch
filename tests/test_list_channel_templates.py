import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

from ocean_watch.templates import (
    list_channel_templates,
    qianchuan_product_templates,
)

from tests.support import business_template_config


def all_channel_config():
    config = business_template_config()
    qianchuan = qianchuan_product_templates.build_business_template(
        "2345678901234567",
        "example drink",
        ["8234567890123456780", "8234567890123456781"],
        template_id="qcpt_example",
    )
    config[qianchuan_product_templates.SCHEMA_VERSION_KEY] = (
        qianchuan_product_templates.SCHEMA_VERSION
    )
    config[qianchuan_product_templates.DEFAULT_TEMPLATE_KEY] = (
        qianchuan_product_templates.default_template()
    )
    config[qianchuan_product_templates.TEMPLATES_KEY] = {
        qianchuan["template_id"]: qianchuan,
    }
    return config


class ListChannelTemplatesTests(unittest.TestCase):
    def test_lists_all_channels_from_one_config_read(self):
        result = list_channel_templates.list_all_templates(all_channel_config())

        self.assertEqual(result["source"], "local_config")
        self.assertEqual(result["summary"], {
            "business_template_count": 2,
            "default_skeleton_count": 2,
            "by_channel": {"marketing": 1, "qianchuan": 1},
        })
        self.assertEqual(set(result["channels"]), {"marketing", "qianchuan"})
        marketing = result["channels"]["marketing"]["templates"][0]
        self.assertEqual(marketing["template_type"], "混剪素材")
        self.assertEqual(marketing["copy_title_count"], 0)
        self.assertNotIn("copy_materials", marketing)
        qianchuan = result["channels"]["qianchuan"]["templates"][0]
        self.assertEqual(qianchuan["template_type"], "商品全域")
        self.assertEqual(qianchuan["daily_budget"], 5000)
        self.assertEqual(qianchuan["roi_goal"], 1.7)

    def test_channel_filter_omits_unrequested_channel(self):
        result = list_channel_templates.list_all_templates(
            all_channel_config(),
            channel="qianchuan",
        )

        self.assertEqual(set(result["channels"]), {"qianchuan"})
        self.assertEqual(result["summary"]["business_template_count"], 1)
        self.assertEqual(result["summary"]["default_skeleton_count"], 1)

    def test_shows_one_marketing_template_with_full_details(self):
        config = all_channel_config()
        name = next(iter(config["plan_templates"]))

        result = list_channel_templates.show_template(
            config,
            channel="marketing",
            selector=name,
        )

        self.assertEqual(result["channel"], "marketing")
        self.assertTrue(result["ready_for_plan_creation"])
        self.assertEqual(result["template"]["name"], name)
        self.assertIn("delivery_settings", result["template"])
        self.assertIn("material_strategy", result["template"])

    def test_shows_one_qianchuan_template_by_id(self):
        result = list_channel_templates.show_template(
            all_channel_config(),
            channel="qianchuan",
            selector="qcpt_example",
        )

        self.assertEqual(result["channel"], "qianchuan")
        self.assertTrue(result["ready_for_plan_creation"])
        self.assertEqual(result["template"]["template_id"], "qcpt_example")
        self.assertIn("delivery_setting", result["template"])
        self.assertIn("material_strategy", result["template"])

    def test_show_rejects_unknown_template(self):
        with self.assertRaisesRegex(Exception, "not found"):
            list_channel_templates.show_template(
                all_channel_config(),
                channel="marketing",
                selector="missing-template",
            )

    def test_cli_is_read_only_and_details_are_opt_in(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            config = all_channel_config()
            path.write_text(json.dumps(config), encoding="utf-8")
            before = path.read_bytes()

            with redirect_stdout(StringIO()) as output:
                code = list_channel_templates.main([
                    "--config",
                    str(path),
                    "--include-details",
                ])

            result = json.loads(output.getvalue())
            self.assertEqual(code, 0)
            self.assertEqual(path.read_bytes(), before)
            marketing = result["channels"]["marketing"]["templates"][0]
            self.assertIn("copy_materials", marketing)
            qianchuan = result["channels"]["qianchuan"]["templates"][0]
            self.assertIn("delivery_setting", qianchuan)

    def test_show_cli_is_read_only(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            config = all_channel_config()
            path.write_text(json.dumps(config), encoding="utf-8")
            before = path.read_bytes()

            with redirect_stdout(StringIO()) as output:
                code = list_channel_templates.show_main([
                    "--config",
                    str(path),
                    "--channel",
                    "qianchuan",
                    "--template",
                    "qcpt_example",
                ])

            result = json.loads(output.getvalue())
            self.assertEqual(code, 0)
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(result["template"]["template_id"], "qcpt_example")
