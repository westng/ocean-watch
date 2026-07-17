import unittest

from ocean_watch.templates import template_advertiser_binding


class TemplateAdvertiserBindingTests(unittest.TestCase):
    def test_authorized_advertiser_must_match_channel_index(self):
        answers = iter(["999", "456"])
        output = []
        advertiser_id, verification = template_advertiser_binding.prompt_advertiser_id(
            "marketing",
            ("REPLACE_WITH_ADVERTISER_ID",),
            input_fn=lambda _: next(answers),
            output_fn=output.append,
            channel_state={
                "authorizations": {"auth-1": {}},
                "advertiser_index": {"123": ["auth-1"], "456": ["auth-1"]},
            },
        )

        self.assertEqual(advertiser_id, "456")
        self.assertEqual(verification["status"], "VERIFIED")
        self.assertIn("不在当前巨量营销授权范围内", "\n".join(output))

    def test_single_authorized_advertiser_is_the_only_automatic_default(self):
        advertiser_id, verification = template_advertiser_binding.prompt_advertiser_id(
            "marketing",
            ("REPLACE_WITH_ADVERTISER_ID",),
            input_fn=lambda _: "",
            output_fn=lambda _: None,
            channel_state={
                "authorizations": {"auth-1": {}},
                "advertiser_index": {"123": ["auth-1"]},
            },
        )

        self.assertEqual(advertiser_id, "123")
        self.assertEqual(verification["authorized_advertiser_count"], 1)

    def test_unauthorized_channel_allows_unverified_template_binding(self):
        advertiser_id, verification = template_advertiser_binding.prompt_advertiser_id(
            "qianchuan",
            (),
            input_fn=lambda _: "789",
            output_fn=lambda _: None,
            channel_state={},
        )

        self.assertEqual(advertiser_id, "789")
        self.assertEqual(verification["status"], "UNVERIFIED")
        self.assertEqual(verification["reason"], "CHANNEL_NOT_AUTHORIZED")


if __name__ == "__main__":
    unittest.main()
