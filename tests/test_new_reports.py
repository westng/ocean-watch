import unittest

from ocean_watch.core.errors import ApiError
from ocean_watch.reports import (
    query_marketing_plan_report,
    query_qianchuan_material_report,
)


class FakeClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def get(self, path, params=None):
        self.calls.append((path, params))
        return self.responses.pop(0)


class NewReportTests(unittest.TestCase):
    def test_marketing_contract_uses_only_account_supported_fields(self):
        response = {
            "code": 0,
            "data": {"list": [{
                "data_topic": "UNI_PROJECT_DATA",
                "dimensions": [
                    {"field": "project_id"},
                    {"field": "project_name"},
                ],
                "metrics": [
                    {"field": "stat_cost"},
                    {"field": "show_cnt"},
                ],
            }]},
        }
        contract = query_marketing_plan_report.select_contract(response)
        self.assertEqual(contract["dimensions"], ["project_id", "project_name"])
        self.assertEqual(contract["metrics"], ["stat_cost", "show_cnt"])
        self.assertIn("click_cnt", contract["omitted_default_metrics"])

    def test_marketing_plan_report_paginates_and_summarizes(self):
        client = FakeClient([
            {
                "code": 0,
                "request_id": "one",
                "data": {
                    "rows": [{
                        "dimensions": {"project_id": 1},
                        "metrics": {"stat_cost": 2, "in_app_order_gmv": 6},
                    }],
                    "page_info": {"total_page": 2},
                },
            },
            {
                "code": 0,
                "request_id": "two",
                "data": {
                    "rows": [{
                        "dimensions": {"project_id": 2},
                        "metrics": {"stat_cost": 3, "in_app_order_gmv": 4},
                    }],
                    "page_info": {"total_page": 2},
                },
            },
        ])
        result = query_marketing_plan_report.query_plan_rows(
            client,
            123,
            "2026-07-17",
            "2026-07-17",
            {
                "dimensions": ["project_id"],
                "metrics": ["stat_cost", "in_app_order_gmv"],
            },
        )
        self.assertEqual(len(result["rows"]), 2)
        self.assertEqual(result["rows"][0]["project_id"], "1")
        summary = query_marketing_plan_report.summarize(result["rows"])
        self.assertEqual(summary["total_spend"], 5.0)
        self.assertEqual(summary["weighted_roi"], 2.0)

    def test_marketing_summary_does_not_invent_unqueried_metrics(self):
        summary = query_marketing_plan_report.summarize(
            [{"stat_cost": 5}],
            ["stat_cost"],
        )
        self.assertIsNone(summary["total_gmv"])
        self.assertIsNone(summary["total_orders"])
        self.assertIsNone(summary["weighted_roi"])
        with self.assertRaises(ApiError):
            query_marketing_plan_report.summarize(
                [{"stat_cost": 5, "in_app_order_count": "1.5"}],
                ["stat_cost", "in_app_order_count"],
            )

    def test_qianchuan_material_report_flattens_and_aggregates_all_pages(self):
        client = FakeClient([
            {
                "code": 0,
                "request_id": "one",
                "data": {
                    "list": [{
                        "material_id": 10,
                        "material_type": "video",
                        "fields": {
                            "stat_cost": 2,
                            "pay_order_amount": 5,
                            "pay_order_count": 1,
                        },
                    }],
                    "page_info": {"total_page": 1},
                },
            },
        ])
        result = query_qianchuan_material_report.query_material_report(
            client,
            "123",
            "2026-07-17",
            "2026-07-17",
        )
        self.assertEqual(result["rows"][0]["material_id"], "10")
        summary = query_qianchuan_material_report.summarize(result["rows"])
        self.assertEqual(summary["total_spend"], 2.0)
        self.assertEqual(summary["total_pay_order_count"], 1)
        self.assertEqual(summary["weighted_roi"], 2.5)

    def test_qianchuan_summary_does_not_invent_unqueried_metrics(self):
        summary = query_qianchuan_material_report.summarize(
            [{"stat_cost": 5}],
            ["stat_cost"],
        )
        self.assertIsNone(summary["total_pay_order_amount"])
        self.assertIsNone(summary["total_pay_order_count"])
        self.assertIsNone(summary["weighted_roi"])
        with self.assertRaises(ApiError):
            query_qianchuan_material_report.summarize(
                [{"pay_order_count": "0.5"}],
                ["pay_order_count"],
            )


if __name__ == "__main__":
    unittest.main()
