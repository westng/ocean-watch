import argparse
import io
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from ocean_watch.core.errors import ApiError
from ocean_watch.reports import query_qianchuan_unified_report as reports


class FakeClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def get(self, endpoint, params=None):
        self.calls.append((endpoint, params))
        return self.responses.pop(0)


class RaisingClient:
    def __init__(self, errors_and_responses):
        self.errors_and_responses = list(errors_and_responses)
        self.calls = []

    def get(self, endpoint, params=None):
        self.calls.append((endpoint, params))
        result = self.errors_and_responses.pop(0)
        if isinstance(result, Exception):
            raise result
        return result


class QianchuanUnifiedReportTests(unittest.TestCase):
    def test_all_and_uni_account_use_distinct_official_contracts(self):
        client = FakeClient([
            {"code": 0, "request_id": "all", "data": {"stat_cost_for_roi2": 12}},
            {"code": 0, "request_id": "uni", "data": {"stat_cost": 8}},
        ])

        all_result = reports.aggregate_report(
            client, "account", "1000000000000001", "2026-08-01", "2026-08-02",
            ["stat_cost_for_roi2"], "ALL", "QIANCHUAN", "OVERALL_PROJECT", "ALL_DATA",
        )
        uni_result = reports.aggregate_report(
            client, "uni-account", "1000000000000001", "2026-08-01", "2026-08-02",
            ["stat_cost"], "ALL", "QIANCHUAN",
        )

        self.assertEqual(client.calls[0][0], reports.ALL_PROMOTION_PATH)
        self.assertEqual(client.calls[0][1]["start_time"], "2026-08-01 00:00:00")
        self.assertEqual(client.calls[0][1]["end_time"], "2026-08-02 23:59:59")
        self.assertEqual(client.calls[0][1]["adlab_scene"], "OVERALL_PROJECT")
        self.assertEqual(client.calls[0][1]["data_period"], "ALL_DATA")
        self.assertEqual(client.calls[1][0], reports.UNI_PROMOTION_PATH)
        self.assertEqual(client.calls[1][1]["start_date"], "2026-08-01")
        self.assertEqual(client.calls[1][1]["end_date"], "2026-08-02")
        self.assertEqual(all_result["data"]["stat_cost_for_roi2"], 12)
        self.assertEqual(uni_result["data"]["stat_cost"], 8)

    def test_custom_report_preserves_exact_dimension_and_metric_containers(self):
        client = FakeClient([
            {
                "code": 0,
                "request_id": "page-1",
                "data": {
                    "rows": [{
                        "dimensions": {
                            "product_id": {
                                "Value": 3747851714615705000,
                                "ValueStr": "3747851714615705603",
                            },
                        },
                        "metrics": {"stat_cost": {"Value": 12.5, "ValueStr": "12.50"}},
                    }],
                    "page_info": {"page": 1, "total_page": 1, "total_number": 1},
                },
            },
        ])

        result = reports.custom_report(
            client, "1000000000000001", "2026-08-04", "2026-08-04",
            "SITE_PROMOTION_PRODUCT_PRODUCT", ["product_id"], ["stat_cost"],
            [{"field": "product_id", "operator": 7, "values": ["3747851714615705603"]}],
            None, "stat_cost", "DESC", 100, 10, 100,
        )

        request = client.calls[0][1]
        self.assertEqual(request["data_topic"], "SITE_PROMOTION_PRODUCT_PRODUCT")
        self.assertEqual(request["filters"][0]["operator"], 7)
        self.assertEqual(request["order_by"], [{"field": "stat_cost", "type": 2}])
        row = result["rows"][0]
        self.assertEqual(row["flat"]["product_id"], "3747851714615705603")
        self.assertEqual(row["flat"]["stat_cost"], 12.5)
        self.assertEqual(row["dimensions"]["product_id"]["ValueStr"], "3747851714615705603")

    def test_products_cli_selects_official_uni_and_overall_topics(self):
        runtime = {"api": {"base_url": "https://api.oceanengine.com/open_api", "access_token": "secret"}}
        client = mock.Mock()
        factory = mock.Mock()
        factory.return_value.client.return_value = client
        result = {"rows": []}
        with mock.patch.object(reports.config_paths, "resolve_config_path", return_value="config.json"), \
             mock.patch.object(reports.token_manager, "ensure_access_token", return_value=runtime), \
             mock.patch.object(reports, "QianchuanClientFactory", factory), \
             mock.patch.object(reports, "custom_report", return_value=result) as query, \
             mock.patch.object(reports, "write_json"):
            code = reports.main([
                "products", "--advertiser-id", "1000000000000001",
                "--report-mode", "overall", "--data-period", "ALL_DATA",
            ])

        self.assertEqual(code, 0)
        self.assertEqual(query.call_args.args[4], reports.PRODUCT_TOPICS["overall"])
        self.assertEqual(query.call_args.args[5], ["product_id"])
        self.assertEqual(query.call_args.args[8], "ALL_DATA")
        self.assertEqual(result["mode"], "qianchuan_product_dimension_report")

    def test_schema_batches_topics_and_preserves_data_period(self):
        client = FakeClient([{
            "code": 0,
            "request_id": "schema",
            "data": {"custom_config_datas": [
                {"data_topic": reports.PRODUCT_TOPICS["uni"]},
                {"data_topic": reports.PRODUCT_TOPICS["overall"]},
            ]},
        }])

        result = reports.schema_report(
            client,
            "1000000000000001",
            [reports.PRODUCT_TOPICS["uni"], reports.PRODUCT_TOPICS["overall"]],
            "ALL_DATA",
        )

        self.assertEqual(len(client.calls), 1)
        self.assertEqual(client.calls[0][1]["data_period"], "ALL_DATA")
        self.assertEqual(result["data_period"], "ALL_DATA")

    def test_schema_cli_defaults_to_data_topic_list(self):
        runtime = {"api": {"base_url": "https://api.oceanengine.com/open_api", "access_token": "secret"}}
        client = FakeClient([{
            "code": 0,
            "request_id": "schema",
            "data": {"custom_config_datas": []},
        }])
        factory = mock.Mock()
        factory.return_value.client.return_value = client

        with mock.patch.object(reports.config_paths, "resolve_config_path", return_value="config.json"), \
             mock.patch.object(reports.token_manager, "ensure_access_token", return_value=runtime), \
             mock.patch.object(reports, "QianchuanClientFactory", factory), \
             mock.patch.object(reports, "write_json"):
            code = reports.main([
                "schema", "--advertiser-id", "1000000000000001",
            ])

        self.assertEqual(code, 0)
        requested_topics = client.calls[0][1]["data_topics"]
        self.assertIn("SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO", requested_topics)
        self.assertIn("OVERALL_ROI_PRODUCT_MATERIAL", requested_topics)

    def test_schema_multi_account_keeps_failed_accounts_in_scope_order(self):
        captured = {}

        def fake_query(_config_path, account, topics, data_period):
            self.assertEqual(topics, ["SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO"])
            self.assertIsNone(data_period)
            if account["advertiser_id"].endswith("2"):
                raise ApiError("failed", {"code": "40103", "message": "token invalid"})
            return {
                "mode": "qianchuan_unified_report_schema",
                "endpoint": reports.CONFIG_PATH,
                "advertiser_id": account["advertiser_id"],
                "data_topics": topics,
                "data_period": data_period,
                "schemas": [{"data_topic": "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO"}],
                "request_ids": ["schema-one"],
            }

        with mock.patch.object(reports.config_paths, "resolve_config_path", return_value="config.json"), \
             mock.patch.object(reports, "query_schema_account", side_effect=fake_query), \
             mock.patch.object(reports, "write_json", side_effect=lambda value, _out=None: captured.setdefault("result", value)):
            code = reports.main([
                "schema",
                "--advertiser-id", "1000000000000001",
                "--advertiser-id", "1000000000000002",
                "--data-topic", "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO",
            ])

        result = captured["result"]
        self.assertEqual(code, 1)
        self.assertFalse(result["ok"])
        self.assertEqual(result["advertiser_ids"], ["1000000000000001", "1000000000000002"])
        self.assertEqual([account["query_status"] for account in result["accounts"]], ["ok", "failed"])
        self.assertEqual(result["accounts"][1]["advertiser_id"], "1000000000000002")

    def test_schema_managed_accounts_reads_enabled_qianchuan_scope(self):
        captured = {}

        def fake_query(_config_path, account, _topics, _data_period):
            return {
                "mode": "qianchuan_unified_report_schema",
                "endpoint": reports.CONFIG_PATH,
                "advertiser_id": account["advertiser_id"],
                "data_topics": [],
                "data_period": None,
                "schemas": [],
                "request_ids": [],
            }

        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(
                """
                {
                  "managed_accounts": {
                    "qianchuan": [
                      {"advertiser_id": "1000000000000001", "name": "enabled", "enabled": true},
                      {"advertiser_id": "1000000000000002", "name": "disabled", "enabled": false}
                    ]
                  }
                }
                """,
                encoding="utf-8",
            )
            with mock.patch.object(reports.config_paths, "resolve_config_path", return_value=config_path), \
                 mock.patch.object(reports, "query_schema_account", side_effect=fake_query), \
                 mock.patch.object(reports, "write_json", side_effect=lambda value, _out=None: captured.setdefault("result", value)):
                code = reports.main(["schema", "--managed-accounts"])

        self.assertEqual(code, 0)
        self.assertEqual(captured["result"]["advertiser_id"], "1000000000000001")

    def test_custom_rejects_multiple_data_topics(self):
        with mock.patch("sys.stderr", new_callable=io.StringIO), \
             self.assertRaises(SystemExit) as raised:
            reports.main([
                "custom",
                "--advertiser-id", "1000000000000001",
                "--data-topic", "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO,SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE",
                "--dimension", "material_id",
                "--metric", "stat_cost_for_roi2",
            ])

        self.assertEqual(raised.exception.code, 2)

    def test_custom_cli_aggregates_multiple_advertisers_by_dimensions(self):
        account_rows = [
            {
                "mode": "qianchuan_unified_report",
                "endpoint": reports.DATA_PATH,
                "advertiser_id": "1000000000000001",
                "displayed_count": 1,
                "total_row_count": 1,
                "page_count": 1,
                "request_ids": ["one"],
                "rows": [],
                "all_rows": [{
                    "dimensions": {},
                    "metrics": {},
                    "flat": {"material_id": "m1", "stat_cost_for_roi2": "10.25", "product_cpc_for_roi2": "2.1"},
                }],
            },
            {
                "mode": "qianchuan_unified_report",
                "endpoint": reports.DATA_PATH,
                "advertiser_id": "1000000000000002",
                "displayed_count": 1,
                "total_row_count": 1,
                "page_count": 1,
                "request_ids": ["two"],
                "rows": [],
                "all_rows": [{
                    "dimensions": {},
                    "metrics": {},
                    "flat": {"material_id": "m1", "stat_cost_for_roi2": "2.75", "product_cpc_for_roi2": "2.5"},
                }],
            },
        ]
        captured = {}

        def fake_query(_config_path, account, *_args, **_kwargs):
            return account_rows[0] if account["advertiser_id"].endswith("1") else account_rows[1]

        with mock.patch.object(reports.config_paths, "resolve_config_path", return_value="config.json"), \
             mock.patch.object(reports, "query_custom_account", side_effect=fake_query), \
             mock.patch.object(reports, "write_json", side_effect=lambda value, _out=None: captured.setdefault("result", value)):
            code = reports.main([
                "custom",
                "--advertiser-id", "1000000000000001,1000000000000002",
                "--data-topic", "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO",
                "--dimension", "material_id",
                "--metric", "stat_cost_for_roi2",
                "--metric", "product_cpc_for_roi2",
                "--order-field", "stat_cost_for_roi2",
                "--top", "0",
            ])

        result = captured["result"]
        self.assertEqual(code, 0)
        self.assertEqual(result["mode"], "qianchuan_unified_report_multi_account")
        self.assertEqual(result["rows"][0]["flat"]["material_id"], "m1")
        self.assertEqual(result["rows"][0]["flat"]["stat_cost_for_roi2"], 13)
        self.assertIsNone(result["rows"][0]["flat"]["product_cpc_for_roi2"])
        self.assertIn("product_cpc_for_roi2", result["aggregation"]["non_additive_metrics"])
        self.assertEqual(result["aggregation"]["sort_field"], "stat_cost_for_roi2")
        self.assertNotIn("all_rows", result["account_results"][0])

    def test_unknown_metrics_are_not_summed_across_accounts(self):
        self.assertFalse(reports.metric_is_additive("custom_unit_metric"))

    def test_custom_multi_account_keeps_successful_results_when_one_account_fails(self):
        captured = {}

        def fake_query(_config_path, account, *_args, **_kwargs):
            if account["advertiser_id"].endswith("2"):
                raise ApiError("failed", {"code": "40103", "message": "token invalid"})
            return {
                "mode": "qianchuan_unified_report",
                "endpoint": reports.DATA_PATH,
                "advertiser_id": account["advertiser_id"],
                "displayed_count": 1,
                "total_row_count": 1,
                "page_count": 1,
                "request_ids": ["one"],
                "rows": [],
                "all_rows": [{
                    "dimensions": {},
                    "metrics": {},
                    "flat": {"material_id": "m1", "stat_cost_for_roi2": "10"},
                }],
            }

        with mock.patch.object(reports.config_paths, "resolve_config_path", return_value="config.json"), \
             mock.patch.object(reports, "query_custom_account", side_effect=fake_query), \
             mock.patch.object(reports, "write_json", side_effect=lambda value, _out=None: captured.setdefault("result", value)):
            code = reports.main([
                "custom",
                "--advertiser-id", "1000000000000001",
                "--advertiser-id", "1000000000000002",
                "--data-topic", "SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO",
                "--dimension", "material_id",
                "--metric", "stat_cost_for_roi2",
            ])

        result = captured["result"]
        self.assertEqual(code, 1)
        self.assertFalse(result["ok"])
        self.assertEqual(result["summary"]["successful_account_count"], 1)
        self.assertEqual(result["summary"]["failed_account_count"], 1)
        self.assertEqual(result["rows"][0]["flat"]["stat_cost_for_roi2"], 10)
        self.assertEqual(result["accounts"][1]["query_status"], "failed")

    def test_dimension_filters_are_sent_and_transport_timeout_retries(self):
        client = RaisingClient([
            ApiError("temporary timeout", {"reason": "socket timed out"}),
            {
                "code": 0,
                "request_id": "room",
                "data": {
                    "list": [],
                    "page_info": {"page": 1, "total_page": 1, "total_number": 0},
                },
            },
        ])

        result = reports.dimension_report(
            client, "rooms", "1000000000000001", "2026-08-04", "2026-08-04",
            "2000000000000001", ["stat_cost_for_roi2"], "TIME_GRANULARITY_DAILY",
            "ALL", "ECP_AWEME", "SMART_BID_CUSTOM", "stat_cost_for_roi2", "DESC",
            100, 10, 100,
        )

        self.assertEqual(len(client.calls), 2)
        self.assertEqual(result["filtering"], {
            "order_platform": "ECP_AWEME",
            "smart_bid_type": "SMART_BID_CUSTOM",
        })

    def test_dimension_report_retries_current_page_and_traverses_all_pages(self):
        client = FakeClient([
            {"code": 40100, "message": "limited"},
            {
                "code": 0, "request_id": "one",
                "data": {"list": [{"room_id": 1}], "page_info": {"page": 1, "total_page": 2, "total_number": 2}},
            },
            {
                "code": 0, "request_id": "two",
                "data": {"list": [{"room_id": 1}], "page_info": {"page": 2, "total_page": 2, "total_number": 2}},
            },
        ])
        with mock.patch.object(reports.time, "sleep"):
            result = reports.dimension_report(
                client, "rooms", "1000000000000001", "2026-08-04", "2026-08-04",
                "2000000000000001", ["stat_cost_for_roi2"], "TIME_GRANULARITY_HOURLY",
                "ALL", "QIANCHUAN", None, "stat_cost_for_roi2", "DESC", 100, 0, 100,
            )

        self.assertEqual([call[1]["page"] for call in client.calls], [1, 1, 2])
        self.assertEqual(result["total_row_count"], 2)
        self.assertEqual(result["request_ids"], ["one", "two"])

    def test_pagination_metadata_changes_fail_closed(self):
        client = FakeClient([
            {
                "code": 0,
                "data": {"rows": [], "page_info": {"page": 1, "total_page": 2, "total_number": 1}},
            },
            {
                "code": 0,
                "data": {"rows": [], "page_info": {"page": 2, "total_page": 2, "total_number": 2}},
            },
        ])
        with self.assertRaises(ApiError):
            reports.paged_report(
                client, reports.DATA_PATH, {"page_size": 100}, top=10, max_pages=100,
                row_key="rows", custom_rows=True,
            )

    def test_filter_parser_accepts_natural_shorthand_and_rejects_wrong_operator(self):
        self.assertEqual(
            reports.parse_filter("product_id=1,2"),
            {"field": "product_id", "operator": 7, "values": ["1", "2"]},
        )
        with self.assertRaises(argparse.ArgumentTypeError):
            reports.parse_filter('{"field":"product_id","operator":1,"values":["1"]}')


if __name__ == "__main__":
    unittest.main()
