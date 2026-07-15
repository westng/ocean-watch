import copy
import unittest

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
