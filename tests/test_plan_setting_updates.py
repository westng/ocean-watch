import unittest

from ocean_watch.core.errors import ConfigurationError
from ocean_watch.plans import update_plan_settings


class PlanSettingUpdateTests(unittest.TestCase):
    def test_marketing_payloads_match_official_contracts(self):
        status = update_plan_settings.marketing_payload(
            "project-status",
            "123",
            ["10", "11"],
            status="DISABLE",
        )
        self.assertEqual(status, {
            "advertiser_id": 123,
            "data": [
                {"project_id": 10, "opt_status": "DISABLE"},
                {"project_id": 11, "opt_status": "DISABLE"},
            ],
        })
        bid = update_plan_settings.marketing_payload(
            "bid",
            "123",
            ["20"],
            value="1.50",
        )
        self.assertEqual(bid["data"], [{"promotion_id": 20, "bid": 1.5}])

    def test_qianchuan_payloads_match_official_contracts(self):
        budget = update_plan_settings.qianchuan_payload(
            "budget",
            "123",
            ["20"],
            value="5000",
        )
        self.assertEqual(budget, {
            "advertiser_id": 123,
            "update_budget_infos": [{"ad_id": 20, "budget": 5000.0}],
        })
        roi = update_plan_settings.qianchuan_payload(
            "roi",
            "123",
            ["20"],
            value="1.7",
            deep_external_action="AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
        )
        self.assertEqual(
            roi["update_roi2_infos"][0]["deep_external_action"],
            "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
        )

    def test_rejects_large_or_invalid_batches(self):
        with self.assertRaises(ConfigurationError):
            update_plan_settings.positive_ids(range(1, 12), "ad_id")
        with self.assertRaises(ConfigurationError):
            update_plan_settings.positive_decimal("nan", "budget")

    def test_detects_marketing_and_qianchuan_partial_failures(self):
        failed, errors = update_plan_settings.response_failed({
            "code": 0,
            "data": {"errors": [{"project_id": 1}]},
        })
        self.assertTrue(failed)
        self.assertEqual(len(errors), 1)
        failed, errors = update_plan_settings.response_failed({
            "code": 0,
            "data": {"results": [{"ad_id": 1, "status": "FAILED"}]},
        })
        self.assertTrue(failed)
        self.assertEqual(errors[0]["ad_id"], 1)
        failed, _ = update_plan_settings.response_failed({
            "code": 0,
            "data": {"results": [{"ad_id": 1, "flag": True}]},
        })
        self.assertFalse(failed)


if __name__ == "__main__":
    unittest.main()
