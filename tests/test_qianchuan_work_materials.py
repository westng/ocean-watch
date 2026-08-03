import copy
import threading
import unittest
from unittest import mock

from ocean_watch.core.errors import ApiError
from ocean_watch.materials import qianchuan_creator_accounts, qianchuan_work_materials
from ocean_watch.materials.query_qianchuan_creator_videos import (
    QIANCHUAN_AWEME_VIDEO_PATH,
)


def video(item_id):
    return {
        "aweme_item_id": int(item_id),
        "image_mode": "VIDEO_VERTICAL",
        "video_id": f"video-{item_id}",
        "material_id": int(item_id) + 1,
        "title": f"work-{item_id}",
    }


class RoutingClient:
    def __init__(self):
        self.calls = []
        self.lock = threading.Lock()

    def get(self, path, params=None):
        with self.lock:
            self.calls.append((path, copy.deepcopy(params)))
        if path == qianchuan_creator_accounts.QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH:
            return {
                "code": 0,
                "data": {
                    "aweme_id_list": [
                        {
                            "aweme_id": 9001,
                            "aweme_show_id": "creator-one",
                            "aweme_name": "Creator One",
                            "has_authorized": True,
                            "is_product_uni_prom_disabled": False,
                        },
                        {
                            "aweme_id": 9002,
                            "aweme_show_id": "creator-two",
                            "aweme_name": "Creator Two",
                            "has_authorized": True,
                            "is_product_uni_prom_disabled": False,
                        },
                    ],
                    "page_info": {"total_page": 1},
                },
            }
        self.assert_video_path(path)
        aweme_id = params["aweme_id"]
        item_ids = {str(value) for value in params["filtering"]["aweme_item_ids"]}
        product_id = params["filtering"].get("product_id")
        returned = []
        if aweme_id == 9001 and "101" in item_ids and product_id in {None, 1001}:
            returned.append(video("101"))
        if aweme_id == 9002 and "102" in item_ids and product_id is None:
            returned.append(video("102"))
        return {
            "code": 0,
            "data": {
                "video_list": returned,
                "page_info": {"has_more": 0, "cursor": 0},
            },
        }

    def assert_video_path(self, path):
        if path != QIANCHUAN_AWEME_VIDEO_PATH:
            raise AssertionError(path)


class QianchuanWorkMaterialTests(unittest.TestCase):
    def test_product_hints_prevent_thirty_product_query_amplification(self):
        product_ids = [str(1000 + index) for index in range(30)]
        work_rows = [
            {
                "input_index": index,
                "aweme_item_id": str(900000 + index),
                "product_hint": {"product_id": product_ids[index % len(product_ids)]},
            }
            for index in range(55)
        ]
        video_queries = []

        def fetch(_client, _advertiser_id, aweme_id, **kwargs):
            video_queries.append({"aweme_id": str(aweme_id), **kwargs})
            return {
                "videos": [
                    {
                        "aweme_item_id": item_id,
                        "image_mode": "VIDEO_LARGE",
                        "video_id": f"video-{item_id}",
                        "material_id": f"material-{item_id}",
                        "title": f"title-{item_id}",
                    }
                    for item_id in kwargs["aweme_item_ids"]
                ],
                "has_more": False,
            }

        owner_hints = {
            row["aweme_item_id"]: {
                "aweme_id": "8001",
                "aweme_show_id": "creator-one",
            }
            for row in work_rows
        }
        with mock.patch.object(
            qianchuan_work_materials,
            "resolve_authorized_aweme",
            return_value={
                "aweme_id": "8001",
                "aweme_show_id": "creator-one",
                "aweme_name": "Creator One",
            },
        ), mock.patch.object(
            qianchuan_work_materials,
            "fetch_creator_videos",
            side_effect=fetch,
        ):
            result = qianchuan_work_materials.resolve_work_materials(
                object(),
                object(),
                "1234567890123456",
                product_ids,
                work_rows,
                concurrency=8,
                owner_hints=owner_hints,
            )

        product_queries = [row for row in video_queries if row.get("product_id")]
        self.assertEqual(len(result["matched"]), 55)
        self.assertEqual(len(product_queries), 30)
        self.assertEqual(sum(len(row["aweme_item_ids"]) for row in product_queries), 55)
        self.assertEqual(result["owner_hint_summary"]["product_video_query_count"], 30)

    def test_rate_limit_detection_accepts_numeric_and_string_codes(self):
        for code in (40100, "40100"):
            with self.subTest(code=code):
                self.assertTrue(qianchuan_work_materials.is_rate_limit_error(
                    ApiError("limited", {"code": code})
                ))
        self.assertFalse(qianchuan_work_materials.is_rate_limit_error(
            ApiError("invalid", {"code": 40000})
        ))

    def test_resolves_owner_then_filters_each_template_product(self):
        client = RoutingClient()
        works = [
            {"input_index": 0, "aweme_item_id": "101", "canonical_url": "url-101"},
            {"input_index": 1, "aweme_item_id": "102", "canonical_url": "url-102"},
            {"input_index": 2, "aweme_item_id": "103", "canonical_url": "url-103"},
        ]
        result = qianchuan_work_materials.resolve_work_materials(
            client,
            client,
            "1234567890123456",
            ["1001", "1002"],
            works,
            concurrency=2,
        )
        self.assertEqual([row["aweme_item_id"] for row in result["matched"]], ["101"])
        self.assertEqual(result["matched"][0]["aweme_id"], "9001")
        self.assertEqual(result["matched"][0]["matched_product_ids"], ["1001"])
        skipped = {row["aweme_item_id"]: row["reason"] for row in result["skipped"]}
        self.assertEqual(skipped["102"], "product_mismatch")
        self.assertEqual(skipped["103"], "not_found_under_authorized_creators")

        authorized_call = next(
            params for path, params in client.calls
            if path == qianchuan_creator_accounts.QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH
        )
        self.assertNotIn("search_key_words", authorized_call["filtering"])
        product_calls = [
            params for path, params in client.calls
            if path == QIANCHUAN_AWEME_VIDEO_PATH
            and params["filtering"].get("product_id") is not None
        ]
        self.assertEqual({row["filtering"]["product_id"] for row in product_calls}, {1001, 1002})

    def test_unrelated_private_creator_failure_does_not_fail_matched_work(self):
        class PrivateCreatorClient(RoutingClient):
            def get(self, path, params=None):
                if path == QIANCHUAN_AWEME_VIDEO_PATH and params["aweme_id"] == 9002:
                    return {
                        "code": 40000,
                        "message": "private creator",
                        "request_id": "private-request",
                    }
                return super().get(path, params=params)

        client = PrivateCreatorClient()
        result = qianchuan_work_materials.resolve_work_materials(
            client,
            client,
            "1234567890123456",
            ["1001"],
            [{"input_index": 0, "aweme_item_id": "101", "canonical_url": "url-101"}],
            concurrency=2,
        )
        self.assertEqual([row["aweme_item_id"] for row in result["matched"]], ["101"])
        self.assertEqual(result["query_failures"], [])

    def test_verified_owner_hint_avoids_scanning_unrelated_creators(self):
        client = RoutingClient()
        result = qianchuan_work_materials.resolve_work_materials(
            client,
            client,
            "1234567890123456",
            ["1001"],
            [{"input_index": 0, "aweme_item_id": "101", "canonical_url": "url-101"}],
            concurrency=2,
            owner_hints={
                "101": {"aweme_id": "9001", "aweme_show_id": "creator-one"}
            },
        )

        ownership_calls = [
            params
            for path, params in client.calls
            if path == QIANCHUAN_AWEME_VIDEO_PATH
            and params["filtering"].get("product_id") is None
        ]
        self.assertEqual([row["aweme_id"] for row in ownership_calls], [9001])
        self.assertEqual(result["resolved_owner_hints"], {
            "101": {"aweme_id": "9001", "aweme_show_id": "creator-one"},
        })
        self.assertEqual(result["owner_hint_summary"], {
            "supplied": 1,
            "eligible": 1,
            "verified": 1,
            "stale": 0,
            "broad_scan_work_count": 0,
            "authorized_hint_query_count": 1,
            "authorized_hint_failure_count": 0,
            "official_video_query_count": 1,
            "product_video_query_count": 1,
        })
        self.assertEqual(result["authorized_creator_query"]["mode"], "targeted_hint")

    def test_stale_owner_hint_falls_back_to_other_authorized_creators(self):
        client = RoutingClient()
        result = qianchuan_work_materials.resolve_work_materials(
            client,
            client,
            "1234567890123456",
            ["1001"],
            [{"input_index": 0, "aweme_item_id": "101", "canonical_url": "url-101"}],
            concurrency=2,
            owner_hints={
                "101": {"aweme_id": "9002", "aweme_show_id": "creator-two"}
            },
        )

        ownership_calls = [
            params
            for path, params in client.calls
            if path == QIANCHUAN_AWEME_VIDEO_PATH
            and params["filtering"].get("product_id") is None
        ]
        self.assertEqual({row["aweme_id"] for row in ownership_calls}, {9001, 9002})
        self.assertEqual([row["aweme_item_id"] for row in result["matched"]], ["101"])
        self.assertEqual(result["resolved_owner_hints"], {
            "101": {"aweme_id": "9001", "aweme_show_id": "creator-one"},
        })
        self.assertEqual(result["owner_hint_summary"]["stale"], 1)
        self.assertEqual(result["owner_hint_summary"]["broad_scan_work_count"], 1)


if __name__ == "__main__":
    unittest.main()
