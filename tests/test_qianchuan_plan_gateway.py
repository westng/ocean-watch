import copy
import datetime as dt
import unittest
from unittest import mock

from ocean_watch.plans import qianchuan_plan_gateway


class FakeClient:
    def __init__(self):
        self.calls = []

    def get(self, path, params=None):
        self.calls.append(("GET", path, copy.deepcopy(params)))
        if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_LIST_PATH:
            return {
                "code": 0,
                "data": {
                    "ad_list": [{
                        "ad_info": {"id": 7001, "name": "paused", "status": "DISABLE"},
                        "room_info": [{"anchor_id": "9001", "anchor_name": "Creator"}],
                    }],
                    "page_info": {"total_page": 1},
                },
            }
        if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_DETAIL_PATH:
            return {
                "code": 0,
                "data": {
                    "ad_id": 7001,
                    "aweme_id": 9001,
                    "name": "paused",
                    "status": "DISABLE",
                    "product_infos": [{"product_id": 1001}],
                },
            }
        if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_MATERIALS_PATH:
            return {
                "code": 0,
                "data": {
                    "ad_material_infos": [{
                        "material_info": {
                            "video_material": {"aweme_item_id": 101},
                        },
                    }],
                    "page_info": {"total_page": 1},
                },
            }
        raise AssertionError(path)

    def post(self, path, payload):
        self.calls.append(("POST", path, copy.deepcopy(payload)))
        return {"code": 0, "request_id": "add-request"}


class QianchuanPlanGatewayTests(unittest.TestCase):
    def test_paused_plan_is_found_and_existing_materials_are_deduplicated(self):
        client = FakeClient()
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        found = gateway.find_creator_plans("1234567890123456", ["9001"])
        plan = found["matches"]["9001"][0]
        self.assertEqual(plan["status"], "DISABLE")
        self.assertEqual(plan["product_ids"], ["1001"])
        materials = gateway.list_plan_video_materials("1234567890123456", "7001")
        self.assertEqual(
            qianchuan_plan_gateway.existing_aweme_item_ids(materials["materials"]),
            {"101"},
        )
        list_call = next(row for row in client.calls if row[1] == qianchuan_plan_gateway.QIANCHUAN_PLAN_LIST_PATH)
        self.assertEqual(list_call[2]["filtering"]["status"], "ALL")
        self.assertEqual(list_call[2]["adlab_scene"], "UNI_PROJECT")

    def test_add_materials_uses_dedicated_official_endpoint(self):
        client = FakeClient()
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        creatives = [{"product_id": 1001, "video_material": [{"aweme_item_id": 102}]}]
        payload, response = gateway.add_materials(
            "1234567890123456",
            "7001",
            creatives,
        )
        self.assertEqual(response["code"], 0)
        self.assertEqual(payload["multi_product_creative_list"], creatives)
        self.assertEqual(client.calls[-1][1], qianchuan_plan_gateway.QIANCHUAN_ADD_MATERIALS_PATH)

    def test_creator_reconciliation_uses_recent_data_period_and_all_pages(self):
        today = dt.date(2026, 7, 15)
        old_plan = {
            "ad_info": {"id": 7001, "name": "old plan"},
            "room_info": [{"anchor_id": "9001"}],
        }
        other_plan = {
            "ad_info": {"id": 8001, "name": "other plan"},
            "room_info": [{"anchor_id": "9002"}],
        }

        def get(path, params=None):
            if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_DETAIL_PATH:
                return {
                    "code": 0,
                    "data": {"ad_id": 7001, "aweme_id": 9001, "name": "old plan"},
                }
            rows = [] if params["page"] == 1 else [other_plan, old_plan, old_plan]
            return {
                "code": 0,
                "data": {"ad_list": rows, "page_info": {"total_page": 2}},
            }

        client = mock.Mock()
        client.get.side_effect = get
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        found = gateway.find_creator_plans(
            "1234567890123456",
            ["9001"],
            today=today,
        )

        self.assertEqual([plan["ad_id"] for plan in found["matches"]["9001"]], ["7001"])
        self.assertEqual(
            [row["ad_info"]["id"] for row in found["list_query"]["plans"]],
            [8001, 7001],
        )
        self.assertEqual(found["list_query"]["page_count"], 2)
        self.assertEqual(found["list_query"]["data_period"], {
            "start_date": "2026-01-17",
            "end_date": "2026-07-15",
        })
        list_calls = [
            call.kwargs["params"]
            for call in client.get.call_args_list
            if call.args[0] == qianchuan_plan_gateway.QIANCHUAN_PLAN_LIST_PATH
        ]
        self.assertEqual([params["page"] for params in list_calls], [1, 2])
        for params in list_calls:
            start = dt.date.fromisoformat(params["start_time"][:10])
            end = dt.date.fromisoformat(params["end_time"][:10])
            self.assertEqual((end - start).days, 179)

    def test_creator_reconciliation_fails_closed_at_page_cap(self):
        client = mock.Mock()
        client.get.return_value = {
            "code": 0,
            "data": {
                "ad_list": [],
                "page_info": {"total_page": 2},
            },
        }
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        today = dt.date(2026, 7, 15)
        with self.assertRaisesRegex(Exception, "truncated"):
            gateway.find_creator_plans(
                "1234567890123456",
                ["9001"],
                today=today,
                max_pages=1,
            )
        client.get.assert_called_once()

    def test_invalid_plan_pagination_fails_closed(self):
        invalid_values = (None, -1, True, 1.5, "many")
        for invalid in invalid_values:
            with self.subTest(total_page=invalid):
                client = mock.Mock()
                client.get.return_value = {
                    "code": 0,
                    "data": {"ad_list": [], "page_info": {"total_page": invalid}},
                }
                gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
                with self.assertRaisesRegex(Exception, "invalid total_page"):
                    gateway.find_creator_plans(
                        "1234567890123456",
                        ["9001"],
                        today=dt.date(2026, 7, 15),
                    )

    def test_nonempty_plan_page_cannot_report_zero_pages(self):
        client = mock.Mock()
        client.get.return_value = {
            "code": 0,
            "data": {
                "ad_list": [{"ad_info": {"id": 7001}}],
                "page_info": {"total_page": 0},
            },
        }
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        with self.assertRaisesRegex(Exception, "contradicts"):
            gateway.list_product_plans(
                "1234567890123456",
                today=dt.date(2026, 7, 15),
            )

    def test_plan_pagination_change_fails_closed(self):
        client = mock.Mock()
        client.get.side_effect = [
            {"code": 0, "data": {"ad_list": [], "page_info": {"total_page": 2}}},
            {"code": 0, "data": {"ad_list": [], "page_info": {"total_page": 3}}},
        ]
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        with self.assertRaisesRegex(Exception, "changed"):
            gateway.list_product_plans(
                "1234567890123456",
                today=dt.date(2026, 7, 15),
            )

    def test_invalid_material_pagination_fails_closed(self):
        client = mock.Mock()
        client.get.return_value = {
            "code": 0,
            "data": {"ad_material_infos": [], "page_info": {}},
        }
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        with self.assertRaisesRegex(Exception, "invalid total_page"):
            gateway.list_plan_video_materials("1234567890123456", "7001")

    def test_deleted_material_does_not_block_readding_work(self):
        deleted = {
            "material_status": "DELETED",
            "material_info": {"video_material": {"aweme_item_id": 101}},
        }
        active = {
            "material_status": "DELIVERY_OK",
            "material_info": {"video_material": {"aweme_item_id": 102}},
        }
        self.assertEqual(
            qianchuan_plan_gateway.existing_aweme_item_ids([deleted, active]),
            {"102"},
        )

    def test_active_and_deleted_rows_still_recognize_active_work(self):
        rows = [
            {
                "material_status": "DELETED",
                "material_info": {"video_material": {"aweme_item_id": 101}},
            },
            {
                "material_status": "DELIVERY_OK",
                "material_info": {"video_material": {"aweme_item_id": 101}},
            },
        ]
        self.assertEqual(qianchuan_plan_gateway.existing_aweme_item_ids(rows), {"101"})

    def test_delete_materials_uses_material_ids_and_official_limit(self):
        client = FakeClient()
        gateway = qianchuan_plan_gateway.QianchuanPlanGateway(client)
        payload, response = gateway.delete_materials(
            "1234567890123456",
            "7001",
            [8001, "8001", 8002],
        )
        self.assertEqual(response["code"], 0)
        self.assertEqual(payload["material_ids"], [8001, 8002])
        self.assertEqual(
            client.calls[-1][1],
            qianchuan_plan_gateway.QIANCHUAN_DELETE_MATERIALS_PATH,
        )
        with self.assertRaisesRegex(Exception, "between 1 and 100"):
            gateway.delete_materials("1234567890123456", "7001", range(1, 102))


if __name__ == "__main__":
    unittest.main()
