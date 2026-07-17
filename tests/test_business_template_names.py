import unittest

from ocean_watch.templates import business_template_names


class BusinessTemplateNameTests(unittest.TestCase):
    def test_marketing_name_uses_shared_structure(self):
        self.assertEqual(
            business_template_names.format_business_template_name(
                "marketing",
                "1234567890123456",
                "示例商品",
                "7123456789012345678",
                "混剪素材",
            ),
            "巨量营销-1234567890123456-示例商品-7123456789012345678-混剪素材",
        )

    def test_qianchuan_name_keeps_multiple_product_ids_in_order(self):
        self.assertEqual(
            business_template_names.format_business_template_name(
                "qianchuan",
                "2345678901234567",
                "示例饮品",
                ["8234567890123456780", "8234567890123456781"],
                "商品全域",
            ),
            "巨量千川-2345678901234567-示例饮品-"
            "8234567890123456780/8234567890123456781-商品全域",
        )

    def test_qianchuan_live_name_uses_creator_binding(self):
        self.assertEqual(
            business_template_names.format_qianchuan_live_template_name(
                "2345678901234567",
                "示例账号",
                "9988776655",
            ),
            "巨量千川-2345678901234567-示例账号-9988776655-直播全域",
        )

    def test_unknown_channel_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "unsupported business template channel"):
            business_template_names.format_business_template_name(
                "unknown",
                "1",
                "商品",
                "2",
                "类型",
            )
