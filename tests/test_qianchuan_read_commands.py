import tempfile
import unittest
from pathlib import Path
from unittest import mock

from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.materials import (
    inspect_qianchuan_work,
    qianchuan_creator_accounts,
    query_qianchuan_products,
)
from ocean_watch.plans import query_qianchuan_plans, query_run_history


class FakeClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def get(self, path, params=None):
        self.calls.append((path, params))
        return self.responses.pop(0)


class QianchuanReadCommandTests(unittest.TestCase):
    def test_work_inspection_uses_f2_metadata_integration(self):
        fake = {
            "resolved": [{"aweme_item_id": "7000000000000000001"}],
            "skipped": [],
        }
        with mock.patch.object(
            inspect_qianchuan_work,
            "resolve_work_links",
            return_value=fake,
        ) as resolve_links:
            result = inspect_qianchuan_work.inspect(
                {},
                ["https://v.douyin.com/example/"],
            )

        self.assertEqual(result["metadata_integration"], "f2_cli")
        self.assertEqual(result["resolved_count"], 1)
        self.assertEqual(
            resolve_links.call_args.kwargs["metadata_resolver"].__class__.__name__,
            "F2WorkMetadataCliResolver",
        )

    def test_authorized_creator_list_accepts_empty_zero_page(self):
        client = FakeClient([{
            "code": 0,
            "request_id": "request-1",
            "data": {"aweme_id_list": [], "page_info": {"total_page": 0}},
        }])
        result = qianchuan_creator_accounts.list_authorized_awemes(
            client,
            "123",
            search_keywords="creator",
        )
        self.assertEqual(result["creators"], [])
        self.assertFalse(result["truncated"])
        self.assertEqual(
            client.calls[0][1]["filtering"]["search_key_words"],
            "creator",
        )

    def test_product_query_paginates_and_compacts_rows(self):
        client = FakeClient([
            {
                "code": 0,
                "request_id": "one",
                "data": {
                    "product_list": [{"id": 10, "name": "A", "stock_num": 2}],
                    "page_info": {"total_page": 2},
                },
            },
            {
                "code": 0,
                "request_id": "two",
                "data": {
                    "product_list": [{"id": 11, "name": "B", "stock_num": 3}],
                    "page_info": {"total_page": 2},
                },
            },
        ])
        result = query_qianchuan_products.query_products(
            client,
            "123",
            product_name="protein",
        )
        self.assertEqual(result["product_count"], 2)
        self.assertEqual(result["products"][0]["product_id"], "10")
        self.assertEqual(client.calls[1][1]["page"], 2)
        self.assertEqual(client.calls[0][1]["filtering"]["product_name"], "protein")

    def test_plan_compaction_never_treats_stats_as_report_money(self):
        row = {
            "ad_info": {"id": 1, "name": "plan", "status": "DELIVERY_OK"},
            "room_info": [{"anchor_id": "creator"}],
            "delivery_setting": {"budget": 5000, "roi2_goal": 1.7},
            "stats_info": {"stat_cost": 999999},
        }
        result = query_qianchuan_plans.compact_plan(row)
        self.assertEqual(result["ad_id"], "1")
        self.assertNotIn("stat_cost", result)

    def test_run_history_rejects_path_traversal_and_summarizes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "creator-batch-abc.json"
            path.write_text(
                '{"schema_version":1,"created_at":"now","jobs":'
                '{"one":{"status":"completed"},"two":{"status":"failed"}}}',
                encoding="utf-8",
            )
            rows = query_run_history.list_runs(root=root)
            self.assertEqual(rows[0]["status_counts"], {"completed": 1, "failed": 1})
            with self.assertRaisesRegex(Exception, "unsupported characters"):
                query_run_history.safe_run_path("../secret", root=root)

    def test_run_history_rejects_invalid_schema_and_symbolic_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            invalid = root / "creator-batch-invalid.json"
            invalid.write_text('{"jobs":[]}', encoding="utf-8")
            with self.assertRaisesRegex(ConfigurationError, "invalid schema"):
                query_run_history.read_run(invalid, root=root)
            invalid.write_text('{"jobs":{"one":"invalid"}}', encoding="utf-8")
            with self.assertRaisesRegex(ConfigurationError, "invalid schema"):
                query_run_history.read_run(invalid, root=root)

            target = root.parent / f"{root.name}-outside-run.json"
            target.write_text('{"jobs":{}}', encoding="utf-8")
            link = root / "creator-batch-link.json"
            try:
                link.symlink_to(target)
            except OSError:
                self.skipTest("symbolic links are unavailable on this platform")
            with self.assertRaisesRegex(ConfigurationError, "symbolic links"):
                query_run_history.read_run(link, root=root)
            target.unlink()

    def test_declared_pagination_rejects_fractional_total_pages(self):
        client = FakeClient([{
            "code": 0,
            "data": {
                "product_list": [],
                "page_info": {"total_page": 1.5},
            },
        }])
        with self.assertRaises(ApiError):
            query_qianchuan_products.query_products(client, "123")


if __name__ == "__main__":
    unittest.main()
