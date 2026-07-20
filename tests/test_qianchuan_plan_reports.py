import copy
import unittest
from unittest import mock

from ocean_watch.core.errors import ApiError
from ocean_watch.reports import query_qianchuan_plan_report


def plan_row(ad_id, name, *, status="DELIVERY_OK"):
    return {
        "ad_info": {
            "id": ad_id,
            "name": name,
            "status": status,
            "opt_status": "ENABLE",
            "budget": 5000,
            "budget_mode": "BUDGET_MODE_DAY",
            "roi2_goal": 1.7,
            "smart_bid_type": "SMART_BID_CUSTOM",
            "compensate_info": {
                "compensate_status": "IN_EFFECT",
                "status": "SUCCESS",
                "reason": "covered",
            },
        },
        "product_info": [{"product_id": ad_id + 1000, "product_name": f"product-{ad_id}"}],
        "room_info": [{"anchor_id": ad_id + 2000, "anchor_name": f"creator-{ad_id}"}],
        "stats_info": {"stat_cost": 500000},
    }


def report_row(ad_id, cost, gmv, settled, orders):
    def value(raw, rendered=None):
        return {"Value": raw, "ValueStr": str(rendered if rendered is not None else raw)}

    return {
        "dimensions": {"ad_id": value(str(ad_id))},
        "metrics": {
            "stat_cost": value(cost, f"{cost:.2f}"),
            "total_pay_order_count_for_roi2": value(orders),
            "total_pay_order_gmv_include_coupon_for_roi2": value(gmv, f"{gmv:.2f}"),
            "total_prepay_and_pay_order_roi2": value(gmv / cost if cost else 0),
            "total_order_settle_amount_for_roi2_1h": value(settled, f"{settled:.2f}"),
            "total_order_settle_count_for_roi2_1h": value(orders),
            "total_prepay_and_pay_settle_roi2_1h": value(settled / cost if cost else 0),
        },
    }


class FakeMcpClient:
    def __init__(
        self,
        plan_pages,
        report_pages,
        *,
        response_hook=None,
    ):
        self.plan_pages = plan_pages
        self.report_pages = report_pages
        self.response_hook = response_hook
        self.calls = []

    def call_tool(self, name, arguments):
        self.calls.append((name, copy.deepcopy(arguments)))
        page = arguments["page"]
        if name == query_qianchuan_plan_report.PLAN_LIST_TOOL:
            self.assert_plan_call(arguments)
            pages = self.plan_pages
            list_key = "ad_list"
            total_key = "total_num"
            request_prefix = "plans"
        else:
            self.assert_report_call(name, arguments)
            pages = self.report_pages
            list_key = "rows"
            total_key = "total_number"
            request_prefix = "report"
        response = {
            "code": 0,
            "message": "OK",
            "request_id": f"{request_prefix}-{page}",
            "data": {
                list_key: copy.deepcopy(pages[page - 1]),
                "page_info": {
                    "page": page,
                    "page_size": 100,
                    total_key: sum(len(rows) for rows in pages),
                    "total_page": len(pages),
                },
            },
        }
        if self.response_hook:
            return self.response_hook(name, arguments, response)
        return response

    @staticmethod
    def assert_plan_call(arguments):
        filtering = arguments["filtering"]
        if filtering != {"status": "ALL", "having_cost": "ALL"}:
            raise AssertionError(filtering)

    @staticmethod
    def assert_report_call(name, arguments):
        if name != query_qianchuan_plan_report.REPORT_DATA_TOOL:
            raise AssertionError(name)
        if arguments["data_topic"] != "SITE_PROMOTION_PRODUCT_AD":
            raise AssertionError(arguments["data_topic"])


class QianchuanPlanReportTests(unittest.TestCase):
    def test_cli_defaults_report_and_metadata_queries_to_today(self):
        result = {"ok": True}
        with mock.patch.object(
            query_qianchuan_plan_report,
            "today",
            return_value="2026-07-20",
        ), mock.patch.object(
            query_qianchuan_plan_report.config_paths,
            "resolve_config_path",
            return_value="config.json",
        ), mock.patch.object(
            query_qianchuan_plan_report.token_manager,
            "ensure_access_token",
            return_value={"api": {"access_token": "secret"}},
        ), mock.patch.object(
            query_qianchuan_plan_report,
            "StreamableHttpMcpClient",
        ) as client_class, mock.patch.object(
            query_qianchuan_plan_report,
            "query_plan_report",
            return_value=result,
        ) as query_report, mock.patch.object(
            query_qianchuan_plan_report,
            "write_json",
        ) as write_json:
            code = query_qianchuan_plan_report.main([
                "--advertiser-id",
                "1234567890123456",
            ])

        self.assertEqual(code, 0)
        query_report.assert_called_once_with(
            client_class.return_value,
            "1234567890123456",
            start_date="2026-07-20",
            end_date="2026-07-20",
            top=10,
            status="ALL",
        )
        write_json.assert_called_once_with(result, None)

    def test_uses_report_values_without_guessing_scale_and_builds_weighted_summary(self):
        client = FakeMcpClient(
            [
                [plan_row(1, "one")],
                [plan_row(2, "two", status="DISABLE"), plan_row(3, "zero")],
            ],
            [
                [report_row(1, 5.0, 10.0, 9.0, 2)],
                [report_row(2, 3.5, 3.5, 3.0, 1)],
            ],
        )

        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
            top=2,
        )

        self.assertEqual(len(client.calls), 4)
        self.assertEqual(client.calls[0][0], query_qianchuan_plan_report.REPORT_DATA_TOOL)
        self.assertEqual(client.calls[0][1]["order_by"], [{"field": "stat_cost", "type": 2}])
        self.assertEqual(client.calls[2][0], query_qianchuan_plan_report.PLAN_LIST_TOOL)
        self.assertEqual(client.calls[2][1]["adlab_scene"], "UNI_PROJECT")
        self.assertEqual(client.calls[2][1]["order_field"], "create_time")
        self.assertTrue(client.calls[2][1]["need_compensate_info"])
        self.assertEqual(client.calls[2][1]["page"], 1)
        self.assertEqual(client.calls[3][1]["page"], 2)
        self.assertEqual(result["summary"]["plan_count"], 2)
        self.assertEqual(result["summary"]["plans_with_cost"], 2)
        self.assertEqual(result["summary"]["total_cost"], 8.5)
        self.assertEqual(result["summary"]["total_pay_order_gmv"], 13.5)
        self.assertEqual(result["summary"]["total_pay_roi"], 1.5882)
        self.assertEqual(result["summary"]["total_settled_amount_1h"], 12.0)
        self.assertTrue(result["presentation"]["required"])
        self.assertFalse(result["presentation"]["allow_column_omission"])
        self.assertEqual(
            [column["field"] for column in result["presentation"]["columns"]],
            [field for field, _ in query_qianchuan_plan_report.DEFAULT_PRESENTATION_COLUMNS],
        )
        self.assertEqual(
            [column["label"] for column in result["presentation"]["columns"]],
            [label for _, label in query_qianchuan_plan_report.DEFAULT_PRESENTATION_COLUMNS],
        )
        self.assertEqual(
            [detail["field"] for detail in result["presentation"]["required_details"]],
            [field for field, _ in query_qianchuan_plan_report.DEFAULT_PRESENTATION_DETAILS],
        )
        for column in result["presentation"]["columns"]:
            self.assertIn(column["field"], result["rows"][0])
        for detail in result["presentation"]["required_details"]:
            self.assertIn(detail["field"], result["rows"][0])
        markdown = result["presentation"]["rendered_markdown"]
        self.assertEqual(markdown.count("\n"), len(result["rows"]) + 1)
        self.assertIn("| 排名 | 计划 | 达人 | 商品 |", markdown)
        self.assertIn("| 1 | one | creator-1 | product-1 |", markdown)
        self.assertIn("| ¥5,000.00 | ¥5.00 | 2 | ¥10.00 | 2 | ¥9.00 |", markdown)
        self.assertEqual(result["rows"][0]["stat_cost"], 5.0)
        self.assertEqual(result["rows"][0]["ad_id"], "1")
        self.assertEqual(result["rows"][0]["creator_ids"], ["2001"])
        self.assertEqual(result["rows"][0]["status_label"], "投放中")
        self.assertEqual(result["rows"][0]["cost_guarantee_status_label"], "生效中")
        self.assertEqual(result["rows"][0]["bid"], 1.7)
        self.assertEqual(result["rows"][0]["budget"], 5000)
        self.assertEqual(result["rows"][0]["budget_mode_label"], "日预算")
        self.assertEqual(result["rows"][0]["roi"], 2.0)
        self.assertEqual(result["rows"][1]["status"], "DISABLE")
        self.assertEqual(result["displayed_count"], 2)
        self.assertEqual(result["total_row_count"], 2)
        self.assertEqual(
            result["request_ids"],
            ["report-1", "report-2", "plans-1", "plans-2"],
        )

    def test_top_zero_returns_every_row(self):
        client = FakeMcpClient(
            [[plan_row(1, "one")]],
            [[report_row(1, 1.0, 1.0, 1.0, 1)]],
        )
        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
            top=0,
        )
        self.assertEqual(len(result["rows"]), 1)

    def test_status_filter_uses_plan_metadata_status(self):
        client = FakeMcpClient(
            [[
                plan_row(1, "running", status="DELIVERY_OK"),
                plan_row(2, "paused", status="DISABLE"),
            ]],
            [[
                report_row(1, 5.0, 10.0, 8.0, 2),
                report_row(2, 7.0, 7.0, 6.0, 1),
            ]],
        )

        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
            status="DELIVERY_OK",
        )

        self.assertEqual([row["ad_id"] for row in result["rows"]], ["1"])
        self.assertEqual(result["summary"]["plan_count"], 1)
        self.assertEqual(result["summary"]["total_cost"], 5.0)

    def test_value_str_is_used_when_metric_value_is_null(self):
        row = report_row(1, 1.0, 1.0, 1.0, 1)
        row["metrics"]["stat_cost"] = {"Value": None, "ValueStr": "4.25"}
        client = FakeMcpClient([[plan_row(1, "one")]], [[row]])

        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
        )

        self.assertEqual(result["rows"][0]["stat_cost"], 4.25)
        self.assertEqual(result["summary"]["total_cost"], 4.25)

    def test_invalid_and_non_finite_metrics_raise_structured_error(self):
        for invalid in ("not-a-number", "NaN", "Infinity", "-Infinity"):
            with self.subTest(invalid=invalid):
                row = report_row(1, 1.0, 1.0, 1.0, 1)
                row["metrics"]["stat_cost"] = {
                    "Value": invalid,
                    "ValueStr": "12.34",
                }
                client = FakeMcpClient([[plan_row(1, "one")]], [[row]])

                with self.assertRaises(ApiError) as caught:
                    query_qianchuan_plan_report.query_plan_report(
                        client,
                        "1234567890123456",
                        start_date="2026-07-15",
                        end_date="2026-07-15",
                    )

                self.assertEqual(caught.exception.code, "api_error")
                self.assertEqual(caught.exception.details["field"], "stat_cost")
                self.assertEqual(caught.exception.details["value"], invalid)
                self.assertFalse(caught.exception.as_dict()["ok"])

    def test_missing_required_metrics_fail_closed(self):
        for field in query_qianchuan_plan_report.DEFAULT_FIELDS:
            with self.subTest(field=field):
                row = report_row(1, 1.0, 1.0, 1.0, 1)
                row["metrics"].pop(field)
                client = FakeMcpClient([[plan_row(1, "one")]], [[row]])
                with self.assertRaisesRegex(ApiError, "required metric") as raised:
                    query_qianchuan_plan_report.query_plan_report(
                        client,
                        "1234567890123456",
                        start_date="2026-07-15",
                        end_date="2026-07-15",
                    )
                self.assertEqual(raised.exception.details["field"], field)

    def test_null_required_metric_values_fail_closed(self):
        row = report_row(1, 1.0, 1.0, 1.0, 1)
        row["metrics"]["stat_cost"] = {"Value": None, "ValueStr": None}
        client = FakeMcpClient([[plan_row(1, "one")]], [[row]])
        with self.assertRaisesRegex(ApiError, "required metric"):
            query_qianchuan_plan_report.query_plan_report(
                client,
                "1234567890123456",
                start_date="2026-07-15",
                end_date="2026-07-15",
            )

    def test_missing_and_malformed_pagination_fail_closed(self):
        def missing_page_info(name, arguments, response):
            if name == query_qianchuan_plan_report.REPORT_DATA_TOOL:
                response["data"].pop("page_info")
            return response

        def malformed_total_page(name, arguments, response):
            if name == query_qianchuan_plan_report.REPORT_DATA_TOOL:
                response["data"]["page_info"]["total_page"] = "many"
            return response

        for response_hook in (missing_page_info, malformed_total_page):
            with self.subTest(response_hook=response_hook.__name__):
                client = FakeMcpClient(
                    [[plan_row(1, "one")]],
                    [[report_row(1, 1.0, 1.0, 1.0, 1)]],
                    response_hook=response_hook,
                )

                with self.assertRaises(ApiError) as caught:
                    query_qianchuan_plan_report.query_plan_report(
                        client,
                        "1234567890123456",
                        start_date="2026-07-15",
                        end_date="2026-07-15",
                    )

                self.assertEqual(caught.exception.details["source"], "report_data")
                self.assertFalse(
                    any(
                        name == query_qianchuan_plan_report.PLAN_LIST_TOOL
                        for name, _ in client.calls
                    )
                )

    def test_missing_plan_metadata_pagination_fails_closed(self):
        def missing_page_info(name, arguments, response):
            if name == query_qianchuan_plan_report.PLAN_LIST_TOOL:
                response["data"]["page_info"] = {}
            return response

        client = FakeMcpClient(
            [[plan_row(1, "one")]],
            [[report_row(1, 1.0, 1.0, 1.0, 1)]],
            response_hook=missing_page_info,
        )

        with self.assertRaises(ApiError) as caught:
            query_qianchuan_plan_report.query_plan_report(
                client,
                "1234567890123456",
                start_date="2026-07-15",
                end_date="2026-07-15",
            )

        self.assertEqual(caught.exception.details["source"], "plan_metadata")

    def test_report_and_metadata_page_caps_fail_closed(self):
        report_pages = [
            [report_row(1, 1.0, 1.0, 1.0, 1)],
            [report_row(2, 1.0, 1.0, 1.0, 1)],
        ]
        report_client = FakeMcpClient(
            [[plan_row(1, "one")], [plan_row(2, "two")]],
            report_pages,
        )
        with self.assertRaises(ApiError) as report_error:
            query_qianchuan_plan_report.query_plan_report(
                report_client,
                "1234567890123456",
                start_date="2026-07-15",
                end_date="2026-07-15",
                max_pages=1,
            )
        self.assertEqual(report_error.exception.details["source"], "report_data")

        metadata_client = FakeMcpClient(
            [[plan_row(1, "one")], [plan_row(1, "one")]],
            [[report_row(1, 1.0, 1.0, 1.0, 1)]],
        )
        with self.assertRaises(ApiError) as metadata_error:
            query_qianchuan_plan_report.query_plan_report(
                metadata_client,
                "1234567890123456",
                start_date="2026-07-15",
                end_date="2026-07-15",
                max_pages=1,
            )
        self.assertEqual(metadata_error.exception.details["source"], "plan_metadata")

    def test_report_pagination_rejects_duplicate_ids_wrong_pages_and_totals(self):
        def duplicate_page(name, arguments, response):
            if name == query_qianchuan_plan_report.REPORT_DATA_TOOL and arguments["page"] == 2:
                response["data"]["rows"] = [report_row(1, 2.0, 2.0, 2.0, 1)]
            return response

        def wrong_page(name, arguments, response):
            if name == query_qianchuan_plan_report.REPORT_DATA_TOOL:
                response["data"]["page_info"]["page"] = arguments["page"] + 1
            return response

        def wrong_total(name, arguments, response):
            if name == query_qianchuan_plan_report.REPORT_DATA_TOOL:
                response["data"]["page_info"]["total_number"] = 99
            return response

        for hook, message in (
            (duplicate_page, "duplicate"),
            (wrong_page, "unexpected page"),
            (wrong_total, "incomplete row count"),
        ):
            with self.subTest(hook=hook.__name__):
                client = FakeMcpClient(
                    [[plan_row(1, "one")], [plan_row(2, "two")]],
                    [
                        [report_row(1, 1.0, 1.0, 1.0, 1)],
                        [report_row(2, 2.0, 2.0, 2.0, 1)],
                    ],
                    response_hook=hook,
                )
                with self.assertRaisesRegex(ApiError, message):
                    query_qianchuan_plan_report.query_plan_report(
                        client,
                        "1234567890123456",
                        start_date="2026-07-15",
                        end_date="2026-07-15",
                    )

    def test_all_status_keeps_financial_row_when_metadata_is_missing(self):
        client = FakeMcpClient(
            [[]],
            [[report_row(99, 3.0, 6.0, 5.0, 2)]],
        )
        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
        )
        self.assertEqual(result["rows"][0]["ad_id"], "99")
        self.assertFalse(result["rows"][0]["metadata_available"])
        self.assertEqual(result["summary"]["metadata_missing_count"], 1)
        self.assertEqual(result["summary"]["total_cost"], 3.0)

    def test_specific_status_requires_resolved_metadata(self):
        client = FakeMcpClient(
            [[]],
            [[report_row(99, 3.0, 6.0, 5.0, 2)]],
        )
        with self.assertRaisesRegex(ApiError, "metadata could not be resolved"):
            query_qianchuan_plan_report.query_plan_report(
                client,
                "1234567890123456",
                start_date="2026-07-15",
                end_date="2026-07-15",
                status="DELIVERY_OK",
            )

    def test_summary_aggregates_raw_decimal_values_before_rounding(self):
        first = report_row(1, 0.005, 0.005, 0.005, 1)
        second = report_row(2, 0.005, 0.005, 0.005, 1)
        client = FakeMcpClient(
            [[plan_row(1, "one"), plan_row(2, "two")]],
            [[first, second]],
        )
        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2026-07-15",
            end_date="2026-07-15",
        )
        self.assertEqual(result["summary"]["total_cost"], 0.01)
        self.assertEqual(result["summary"]["total_pay_order_gmv"], 0.01)
        self.assertEqual(result["summary"]["total_pay_roi"], 1.0)

    def test_metadata_search_uses_report_date_range(self):
        client = FakeMcpClient(
            [[plan_row(77, "older-plan")]],
            [[report_row(77, 2.0, 3.0, 2.5, 1)]],
        )

        result = query_qianchuan_plan_report.query_plan_report(
            client,
            "1234567890123456",
            start_date="2025-01-01",
            end_date="2025-01-02",
        )

        metadata_call = next(
            arguments
            for name, arguments in client.calls
            if name == query_qianchuan_plan_report.PLAN_LIST_TOOL
        )
        filtering = metadata_call["filtering"]
        self.assertEqual(filtering, {"status": "ALL", "having_cost": "ALL"})
        self.assertNotIn("create_start_date", filtering)
        self.assertNotIn("create_end_date", filtering)
        self.assertEqual(metadata_call["start_time"], "2025-01-01 00:00:00")
        self.assertEqual(metadata_call["end_time"], "2025-01-02 23:59:59")
        self.assertEqual(result["rows"][0]["name"], "older-plan")


if __name__ == "__main__":
    unittest.main()
