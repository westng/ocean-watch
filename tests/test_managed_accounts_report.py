import unittest
from unittest import mock

from ocean_watch.core.errors import ApiError
from ocean_watch.reports import query_managed_accounts_report


def account(channel, advertiser_id, name):
    return {
        "channel": channel,
        "advertiser_id": advertiser_id,
        "name": name,
        "enabled": True,
    }


class FakeMarketingClient:
    def __init__(self, *_args, **_kwargs):
        self.calls = []

    def get(self, path, params):
        self.calls.append((path, params))
        return {
            "code": 0,
            "request_id": "marketing-request",
            "data": {
                "total_metrics": {
                    "stat_cost": "12.34",
                    "in_app_order_count": "2",
                    "in_app_order_gmv": "30.00",
                    "in_app_order_roi": "2.43",
                    "in_app_order_net_count_1h": "1",
                    "in_app_order_net_gmv_1h": "20.00",
                    "in_app_order_net_roi_1h": "1.62",
                },
            },
        }


class ManagedAccountReportTests(unittest.TestCase):
    def test_marketing_account_uses_dimensionless_basic_data_total(self):
        client = FakeMarketingClient()
        runtime = {"api": {"base_url": "https://api.test", "access_token": "token"}}
        with mock.patch.object(
            query_managed_accounts_report.token_manager,
            "ensure_access_token",
            return_value=runtime,
        ), mock.patch.object(
            query_managed_accounts_report,
            "OceanEngineClient",
            return_value=client,
        ):
            result = query_managed_accounts_report.marketing_account_report(
                "config.json",
                account("marketing", "1234567890123456", "Account"),
                "2026-07-15",
                "2026-07-15",
            )
        self.assertEqual(result["spend"], 12.34)
        self.assertEqual(result["orders"], 2)
        self.assertEqual(result["gmv"], 30.0)
        self.assertEqual(result["metric_basis"]["gmv"], "in_app_order_gmv")
        self.assertEqual(client.calls[0][1]["dimensions"], [])
        self.assertEqual(client.calls[0][1]["data_topic"], "BASIC_DATA")

    def test_account_authorization_binding_is_forwarded(self):
        runtime = {"api": {"base_url": "https://api.test", "access_token": "token"}}
        bound = {
            **account("marketing", "1234567890123456", "Account"),
            "auth_account_id": "987654321",
        }
        with mock.patch.object(
            query_managed_accounts_report.token_manager,
            "ensure_access_token",
            return_value=runtime,
        ) as ensure_token, mock.patch.object(
            query_managed_accounts_report,
            "OceanEngineClient",
            return_value=FakeMarketingClient(),
        ):
            query_managed_accounts_report.marketing_account_report(
                "config.json",
                bound,
                "2026-07-15",
                "2026-07-15",
            )
        self.assertEqual(ensure_token.call_args.kwargs["auth_account_id"], "987654321")

    def test_batch_preserves_order_and_one_failure_does_not_stop_others(self):
        accounts = [
            account("marketing", "1001", "First"),
            account("qianchuan", "1002", "Second"),
        ]

        def query_fn(_path, current, _start, _end):
            if current["advertiser_id"] == "1001":
                return {
                    **current,
                    "channel_name": "巨量营销",
                    "query_status": "ok",
                    "spend": 10,
                    "gmv": 20,
                    "roi": 2,
                }
            raise ApiError("failed", {"code": 40000})

        rows = query_managed_accounts_report.query_accounts(
            "config.json",
            accounts,
            "2026-07-15",
            "2026-07-15",
            query_fn=query_fn,
            retry_delays=(),
        )
        self.assertEqual([row["advertiser_id"] for row in rows], ["1001", "1002"])
        self.assertEqual(rows[0]["query_status"], "ok")
        self.assertEqual(rows[1]["query_status"], "failed")
        summary = query_managed_accounts_report.build_summary(rows)
        self.assertEqual(summary["successful_account_count"], 1)
        self.assertEqual(summary["failed_account_count"], 1)
        self.assertEqual(summary["total_spend"], 10.0)
        self.assertTrue(summary["aggregate_gmv_comparable"])
        self.assertIn("marketing", summary["channel_summaries"])

    def test_mixed_channel_summary_does_not_merge_incompatible_gmv(self):
        rows = [
            {
                **account("marketing", "1001", "Marketing"),
                "query_status": "ok",
                "spend": 10,
                "gmv": 20,
            },
            {
                **account("qianchuan", "1002", "Qianchuan"),
                "query_status": "ok",
                "spend": 5,
                "gmv": 15,
            },
        ]
        summary = query_managed_accounts_report.build_summary(rows)
        self.assertEqual(summary["total_spend"], 15.0)
        self.assertIsNone(summary["total_gmv"])
        self.assertIsNone(summary["weighted_roi"])
        self.assertFalse(summary["aggregate_gmv_comparable"])
        self.assertEqual(summary["channel_summaries"]["marketing"]["weighted_roi"], 2.0)
        self.assertEqual(summary["channel_summaries"]["qianchuan"]["weighted_roi"], 3.0)

    def test_presentation_is_mandatory_and_preserves_failures_and_metric_basis(self):
        rows = [
            {
                **account("marketing", "1001", "营销|账户"),
                "channel_name": "巨量营销",
                "query_status": "ok",
                "spend": 10,
                "orders": 2,
                "gmv": 20,
                "roi": 2,
                "net_orders_1h": 1,
                "net_gmv_1h": 12,
                "net_roi_1h": 1.2,
            },
            {
                **account("qianchuan", "1002", "千川账户"),
                "channel_name": "巨量千川",
                "query_status": "failed",
                "error": {
                    "code": "api_error",
                    "message": "remote failure",
                    "details": {"code": 40100, "message": "系统请求频率超限"},
                },
            },
        ]

        result = query_managed_accounts_report.build_result(
            rows,
            "2026-07-20",
            "2026-07-20",
        )

        presentation = result["presentation"]
        self.assertTrue(presentation["required"])
        self.assertFalse(presentation["allow_column_omission"])
        self.assertFalse(presentation["allow_column_reordering"])
        self.assertEqual(
            presentation["required_sections"],
            ["date_range", "summary", "accounts", "channel_summaries", "metric_basis"],
        )
        markdown = presentation["rendered_markdown"]
        self.assertIn("**查询日期：** 2026-07-20", markdown)
        self.assertIn("共 2 个；成功 1 个；失败 1 个；总消耗 ¥10.00", markdown)
        self.assertIn("| 渠道 | 账户名称 | 广告主 ID | 启用状态 | 查询状态 |", markdown)
        self.assertIn("营销\\|账户", markdown)
        self.assertIn("| 巨量千川 | 千川账户 | 1002 | 已启用 | 失败 |", markdown)
        self.assertIn("40100: 系统请求频率超限", markdown)
        self.assertIn("| 巨量营销 | GMV | in_app_order_gmv |", markdown)
        self.assertIn("| 巨量千川 | GMV | total_pay_order_gmv_include_coupon_for_roi2 |", markdown)

    def test_channel_summary_includes_all_standard_metrics(self):
        summary = query_managed_accounts_report.summarize_metrics([
            {
                "spend": 10,
                "orders": 2,
                "gmv": 20,
                "net_orders_1h": 1,
                "net_gmv_1h": 12,
            },
        ])
        self.assertEqual(summary["total_orders"], 2)
        self.assertEqual(summary["total_net_orders_1h"], 1)
        self.assertEqual(summary["total_net_gmv_1h"], 12.0)
        self.assertEqual(summary["weighted_net_roi_1h"], 1.2)

    def test_transient_api_errors_are_retried(self):
        def retrying_query(error_code, calls):
            def query_fn(_path, current, _start, _end):
                calls.append(current["advertiser_id"])
                if len(calls) < 3:
                    raise ApiError("transient", {"code": error_code})
                return {"query_status": "ok"}

            return query_fn

        for code in (40100, 51010):
            with self.subTest(code=code):
                calls = []
                sleeps = []

                result = query_managed_accounts_report.query_with_retry(
                    retrying_query(code, calls),
                    "config.json",
                    account("qianchuan", "1001", "Account"),
                    "2026-07-15",
                    "2026-07-15",
                    retry_delays=(1, 2),
                    sleep_fn=sleeps.append,
                )
                self.assertEqual(result["query_status"], "ok")
                self.assertEqual(sleeps, [1, 2])

    def test_non_retryable_api_error_is_not_retried(self):
        calls = []

        def query_fn(_path, current, _start, _end):
            calls.append(current["advertiser_id"])
            raise ApiError("fatal", {"code": 40000})

        with self.assertRaises(ApiError):
            query_managed_accounts_report.query_with_retry(
                query_fn,
                "config.json",
                account("qianchuan", "1001", "Account"),
                "2026-07-15",
                "2026-07-15",
                retry_delays=(1, 2),
                sleep_fn=lambda _delay: self.fail("unexpected sleep"),
            )
        self.assertEqual(calls, ["1001"])

    def test_marketing_http_status_is_forwarded_for_retry(self):
        runtime = {"api": {"base_url": "https://api.test", "access_token": "token"}}
        client = FakeMarketingClient()
        client.get = mock.Mock(return_value={
            "code": 429,
            "http_status": 429,
            "message": "rate limited",
        })
        with mock.patch.object(
            query_managed_accounts_report.token_manager,
            "ensure_access_token",
            return_value=runtime,
        ), mock.patch.object(
            query_managed_accounts_report,
            "OceanEngineClient",
            return_value=client,
        ):
            with self.assertRaises(ApiError) as raised:
                query_managed_accounts_report.marketing_account_report(
                    "config.json",
                    account("marketing", "1234567890123456", "Account"),
                    "2026-07-15",
                    "2026-07-15",
                )
        self.assertEqual(raised.exception.details["http_status"], 429)

    def test_retryable_transport_errors_are_retried(self):
        def transient_query(error_details, calls):
            def query_fn(_path, _current, _start, _end):
                calls.append(1)
                if len(calls) == 1:
                    raise ApiError("transient", error_details)
                return {"query_status": "ok"}

            return query_fn

        for details in (
            {"http_status": 429},
            {"http_status": 503},
            {"retryable": True, "transport_error": "timeout"},
        ):
            with self.subTest(details=details):
                calls = []

                result = query_managed_accounts_report.query_with_retry(
                    transient_query(details, calls),
                    "config.json",
                    account("qianchuan", "1001", "Account"),
                    "2026-07-15",
                    "2026-07-15",
                    retry_delays=(0,),
                    sleep_fn=lambda _delay: None,
                )
                self.assertEqual(result["query_status"], "ok")

    def test_incomplete_qianchuan_report_is_rejected(self):
        runtime = {"api": {"access_token": "token"}}
        with mock.patch.object(
            query_managed_accounts_report.token_manager,
            "ensure_access_token",
            return_value=runtime,
        ), mock.patch.object(
            query_managed_accounts_report.query_qianchuan_plan_report,
            "query_plan_report",
            return_value={"ok": True, "truncated": True},
        ):
            with self.assertRaisesRegex(ApiError, "incomplete"):
                query_managed_accounts_report.qianchuan_account_report(
                    "config.json",
                    account("qianchuan", "1001", "Account"),
                    "2026-07-15",
                    "2026-07-15",
                )

    def test_malformed_summary_metric_is_rejected(self):
        with self.assertRaisesRegex(ApiError, "non-finite"):
            query_managed_accounts_report.build_summary([
                {"query_status": "ok", "spend": "NaN", "gmv": 1},
            ])


if __name__ == "__main__":
    unittest.main()
