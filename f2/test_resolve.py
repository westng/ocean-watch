import asyncio
import importlib.util
import io
import json
import os
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("ocean_watch_f2_resolve", Path(__file__).with_name("resolve.py"))
resolve = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(resolve)
WORK_ID = "7000000000000000001"


def fake_video(work_id=WORK_ID, *, product_id="1001"):
    detail = {
        "aweme_id": work_id,
        "aweme_type": 0,
        "preview_title": "作品标题",
        "author": {
            "nickname": "达人甲",
            "unique_id": "creator-one",
            "short_id": "short-one",
            "uid": "9001",
            "avatar_thumb": {"url_list": ["https://example.test/avatar.jpg"]},
        },
        "anchor_info": {
            "extra": json.dumps([{
                "product_id": product_id,
                "title": "商品甲",
                "elastic_images": [{"uri": "https://example.test/product.jpg"}],
            }]),
        },
        "video": {
            "cover": {"url_list": ["https://example.test/cover.jpg"]},
            "bit_rate": [{"play_addr": {"url_list": ["https://example.test/video.mp4"]}}],
        },
        "music": {"play_url": {"uri": "music-id"}},
    }
    return SimpleNamespace(_to_raw=lambda: {"aweme_detail": detail, "status_code": 0})


class ResolveTests(unittest.TestCase):
    def test_detail_gateway_header_allows_metadata_with_saved_cookie(self):
        def factory(kwargs):
            async def fetch(work_id):
                if not kwargs["headers"].get("x-tt-argus"):
                    error = RuntimeError("Blocked by ArgusSecurityPlugin Uifid Not Found")
                    error.status_code = 403
                    raise error
                self.assertEqual(kwargs["cookie"], "sessionid=fixture-secret;")
                return fake_video(work_id)
            return fetch

        result = asyncio.run(resolve.fetch_many(
            [WORK_ID], cookie="sessionid=fixture-secret;", fetch_factory=factory,
        ))
        self.assertTrue(result["ok"])
        self.assertEqual(result["performance"]["retry_count"], 0)
        self.assertNotIn("fixture-secret", json.dumps(result))

    def test_stdin_cookie_is_bounded_and_never_falls_back(self):
        value = "sessionid=fixture-secret; token=a=b+==;"
        self.assertEqual(resolve.read_credentials(io.BytesIO(json.dumps({"cookie": value}).encode())), value)
        for payload in [b"{}", b'{"cookie":""}', b'{"cookie":"sid=x\\r\\nHeader=x"}', b"x" * 200705]:
            with self.assertRaises((ValueError, UnicodeDecodeError)):
                resolve.read_credentials(io.BytesIO(payload))

        def factory(kwargs):
            self.assertEqual(kwargs["cookie"], value)
            async def fetch(work_id):
                return fake_video(work_id)
            return fetch

        with patch.dict(os.environ, {resolve.DOUYIN_COOKIE_ENV: "sid=stale-environment"}):
            result = asyncio.run(resolve.fetch_many([WORK_ID], cookie=value, fetch_factory=factory,
                ttwid_factory=lambda: self.fail("system Cookie must not initialize a visitor")))
        self.assertTrue(result["ok"])
        self.assertNotIn("fixture-secret", json.dumps(result))

    def test_f2_exception_http_status_without_response_is_classified(self):
        error = RuntimeError("sensitive fixture-secret")
        error.status_code = 403
        failure = resolve.metadata_failure(error)
        self.assertEqual(failure["code"], "f2_http_error")
        self.assertNotIn("fixture-secret", str(failure))

    def test_maps_author_product_and_video_contract(self):
        self.assertEqual(
            resolve.map_video_response(fake_video(), WORK_ID)["data"],
            {
                "author": {
                    "nickname": "达人甲",
                    "unique_id": "creator-one",
                    "uid": "9001",
                    "avatar": "https://example.test/avatar.jpg",
                },
                "product": {
                    "product_info_id": "1001",
                    "product_info_img": "https://example.test/product.jpg",
                    "product_info_name": "商品甲",
                },
                "products": [{"product_info_id": "1001", "product_info_name": "商品甲"}],
                "video": {
                    "video_info_cover": "https://example.test/cover.jpg",
                    "video_info_id": WORK_ID,
                    "video_info_title": "作品标题",
                    "video_info_url": "https://example.test/video.mp4",
                    "play_url": "music-id",
                },
            },
        )

    def test_batch_retries_only_failed_rows(self):
        calls = {}

        def fetch_factory(kwargs):
            self.assertEqual(kwargs["cookie"], "ttwid=visitor-id;")

            async def fetch(work_id):
                calls[work_id] = calls.get(work_id, 0) + 1
                if work_id.endswith("2") and calls[work_id] == 1:
                    raise RuntimeError("cookie=secret")
                return fake_video(work_id)

            return fetch

        other = "7000000000000000002"
        result = asyncio.run(resolve.fetch_many(
            [WORK_ID, other],
            concurrency=2,
            fetch_factory=fetch_factory,
            ttwid_factory=lambda: "visitor-id",
        ))
        self.assertTrue(result["ok"])
        self.assertEqual(calls, {WORK_ID: 1, other: 2})
        self.assertEqual(result["performance"]["retry_count"], 1)
        self.assertNotIn("secret", str(result))

    def test_runtime_gate_requires_pinned_f2(self):
        self.assertEqual(resolve.validate_f2_runtime(lambda _: "0.0.1.7"), "0.0.1.7")
        with self.assertRaisesRegex(RuntimeError, "0.0.1.7"):
            resolve.validate_f2_runtime(lambda _: "0.0.1.6")

    def test_all_anchor_products_are_preserved_for_ambiguous_routing(self):
        detail = {"anchor_info": {"extra": json.dumps([
            {"product_id": "1001", "title": "甲"},
            {"product_id": "1002", "title": "乙"},
            {"product_id": "1001", "title": "重复甲"},
            {"product_id": "invalid", "title": "无效"},
        ])}}
        self.assertEqual(resolve.parse_products(detail), [
            {"product_info_id": "1001", "product_info_name": "甲"},
            {"product_info_id": "1002", "product_info_name": "乙"},
        ])

    def test_failures_preserve_observable_categories_without_leaking_errors(self):
        failures = [
            (OSError("private proxy and cookie"), "f2_network_error"),
            (json.JSONDecodeError("private response", "secret", 0), "f2_response_parse_failed"),
            (resolve.MetadataValidationError("f2_empty_response"), "f2_empty_response"),
            (RuntimeError("private unknown response"), "f2_metadata_query_failed"),
        ]
        for error, code in failures:
            with self.subTest(code=code):
                result = resolve.metadata_failure(error)
                self.assertEqual(result["code"], code)
                self.assertNotIn("private", str(result))
                self.assertNotIn("secret", str(result))

    def test_http_error_does_not_claim_anti_bot_or_login_failure(self):
        error = RuntimeError("cookie=private")
        error.response = SimpleNamespace(status_code=403)
        self.assertEqual(resolve.metadata_failure(error)["code"], "f2_http_error")


if __name__ == "__main__":
    unittest.main()
