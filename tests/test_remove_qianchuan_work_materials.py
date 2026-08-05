import copy
import unittest
from contextlib import nullcontext
from types import SimpleNamespace

from ocean_watch.plans import qianchuan_plan_gateway
from ocean_watch.plans import remove_qianchuan_work_materials as remove_work


def material(
    aweme_item_id,
    material_id,
    *,
    select_type="CUSTOM",
    status="DELIVERY_OK",
):
    return {
        "material_select_type": select_type,
        "material_status": status,
        "material_info": {
            "material_type": "VIDEO",
            "video_material": {
                "aweme_item_id": int(aweme_item_id),
                "material_id": int(material_id),
            },
        },
    }


def arguments(*item_ids, submit=False, confirm_delete=False):
    return SimpleNamespace(
        config=None,
        advertiser_id="1234567890123456",
        ad_id="7001",
        work_url=[f"https://www.douyin.com/video/{item_id}" for item_id in item_ids],
        concurrency=2,
        auth_account_id=None,
        submit=submit,
        confirm_delete=confirm_delete,
        out=None,
    )


class FakeClient:
    def __init__(self, materials=None, *, delete_code=0, apply_delete=True):
        self.materials = copy.deepcopy(materials or [])
        self.delete_code = delete_code
        self.apply_delete = apply_delete
        self.calls = []

    def get(self, path, params=None):
        self.calls.append(("GET", path, copy.deepcopy(params)))
        if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_DETAIL_PATH:
            return {
                "code": 0,
                "data": {
                    "ad_id": 7001,
                    "name": "test-plan",
                    "aweme_id": 9001,
                    "status": "DELIVERY_OK",
                },
            }
        if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_MATERIALS_PATH:
            return {
                "code": 0,
                "data": {
                    "ad_material_infos": copy.deepcopy(self.materials),
                    "page_info": {"total_page": 1},
                },
            }
        raise AssertionError(path)

    def post(self, path, payload):
        self.calls.append(("POST", path, copy.deepcopy(payload)))
        if path != qianchuan_plan_gateway.QIANCHUAN_DELETE_MATERIALS_PATH:
            raise AssertionError(path)
        response = {
            "code": self.delete_code,
            "message": "OK" if self.delete_code == 0 else "delete failed",
            "request_id": "delete-request",
        }
        if self.delete_code == 0 and self.apply_delete:
            target_ids = {str(value) for value in payload["material_ids"]}
            for row in self.materials:
                reference = remove_work.material_reference(row)
                if reference["material_id"] in target_ids:
                    row["material_status"] = "DELETED"
        return response


class FakeDeleteGateway:
    def __init__(self):
        self.calls = []

    def delete_materials(self, advertiser_id, ad_id, material_ids):
        self.calls.append(list(material_ids))
        return {
            "advertiser_id": int(advertiser_id),
            "ad_id": int(ad_id),
            "material_ids": [int(value) for value in material_ids],
        }, {"code": 0, "request_id": f"batch-{len(self.calls)}"}


class RemoveQianchuanWorkMaterialTests(unittest.TestCase):
    def execute(self, args, client):
        return remove_work.execute(
            args,
            client=client,
            lock_factory=lambda _path: nullcontext(),
        )

    def test_dry_run_maps_work_to_nested_material_id_without_writing(self):
        client = FakeClient([material(101, 8001)])
        lock_state = {"held": False, "entered": 0}

        class TrackingLock:
            def __enter__(self):
                lock_state["held"] = True
                lock_state["entered"] += 1

            def __exit__(self, *_args):
                lock_state["held"] = False

        original_get = client.get

        def guarded_get(path, params=None):
            self.assertTrue(lock_state["held"])
            return original_get(path, params)

        client.get = guarded_get
        result, exit_code = remove_work.execute(
            arguments(101),
            client=client,
            lock_factory=lambda _path: TrackingLock(),
        )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["results"][0]["status"], "would_delete")
        self.assertEqual(result["results"][0]["material_id"], "8001")
        self.assertEqual(lock_state["entered"], 1)
        self.assertFalse(lock_state["held"])
        self.assertFalse(any(method == "POST" for method, _, _ in client.calls))
        self.assertEqual(
            result["performance"]["request_budget"],
            {"limit": None, "used": 0, "remaining": None},
        )

    def test_submit_deletes_custom_material_and_verifies_status(self):
        client = FakeClient([material(101, 8001), material(102, 8002)])
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["results"][0]["status"], "deleted")
        self.assertEqual(result["results"][0]["verified_material_statuses"], ["DELETED"])
        delete_call = next(row for row in client.calls if row[0] == "POST")
        self.assertEqual(delete_call[1], qianchuan_plan_gateway.QIANCHUAN_DELETE_MATERIALS_PATH)
        self.assertEqual(delete_call[2]["material_ids"], [8001])

    def test_auto_material_is_skipped(self):
        client = FakeClient([material(101, 8001, select_type="AUTO")])
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["results"][0]["reason"], "unsupported_material_select_type")
        self.assertFalse(any(method == "POST" for method, _, _ in client.calls))

    def test_already_deleted_material_is_idempotent(self):
        client = FakeClient([material(101, 8001, status="DELETED")])
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["results"][0]["status"], "already_deleted")
        self.assertFalse(any(method == "POST" for method, _, _ in client.calls))

    def test_multiple_distinct_material_ids_for_one_work_are_blocked(self):
        client = FakeClient([material(101, 8001), material(101, 8002)])
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 1)
        self.assertEqual(result["results"][0]["reason"], "ambiguous_material_match")
        self.assertFalse(any(method == "POST" for method, _, _ in client.calls))

    def test_repeated_rows_for_same_material_id_are_deduplicated(self):
        rows = [material(101, 8001), material(101, 8001)]
        results, candidates = remove_work.reconcile_work_materials(
            [{"input_index": 0, "aweme_item_id": "101"}],
            rows,
        )
        self.assertEqual(results[0]["material_id"], "8001")
        self.assertEqual(len(candidates), 1)

    def test_official_delete_failure_is_returned(self):
        client = FakeClient([material(101, 8001)], delete_code=40000)
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 1)
        self.assertEqual(result["results"][0]["reason"], "official_delete_failed")
        self.assertEqual(result["results"][0]["response"]["code"], 40000)

    def test_success_response_without_deleted_status_fails_verification(self):
        client = FakeClient([material(101, 8001)], apply_delete=False)
        result, exit_code = self.execute(
            arguments(101, submit=True, confirm_delete=True), client
        )
        self.assertEqual(exit_code, 1)
        self.assertEqual(result["results"][0]["reason"], "delete_verification_failed")

    def test_submit_without_confirm_delete_stops_before_link_or_api_calls(self):
        client = FakeClient([material(101, 8001)])
        link_calls = []

        with self.assertRaisesRegex(
            remove_work.ConfigurationError, "--submit --confirm-delete"
        ):
            remove_work.execute(
                arguments(101, submit=True),
                client=client,
                link_resolver=lambda url: link_calls.append(url),
                lock_factory=lambda _path: nullcontext(),
            )

        self.assertEqual(link_calls, [])
        self.assertEqual(client.calls, [])

    def test_confirm_delete_without_submit_stops_before_link_or_api_calls(self):
        client = FakeClient([material(101, 8001)])
        link_calls = []

        with self.assertRaisesRegex(
            remove_work.ConfigurationError, "valid only with --submit"
        ):
            remove_work.execute(
                arguments(101, confirm_delete=True),
                client=client,
                link_resolver=lambda url: link_calls.append(url),
                lock_factory=lambda _path: nullcontext(),
            )

        self.assertEqual(link_calls, [])
        self.assertEqual(client.calls, [])

    def test_delete_batches_follow_official_one_hundred_limit(self):
        gateway = FakeDeleteGateway()
        candidates = [
            {"material_id": str(8000 + index), "status": "would_delete"}
            for index in range(101)
        ]
        batches, submitted = remove_work.submit_delete_batches(
            gateway,
            "1234567890123456",
            "7001",
            candidates,
        )
        self.assertEqual([len(row) for row in gateway.calls], [100, 1])
        self.assertEqual(len(batches), 2)
        self.assertEqual(len(submitted), 101)


if __name__ == "__main__":
    unittest.main()
