import asyncio
import importlib.util
import json
import unittest
from pathlib import Path
from types import SimpleNamespace


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


if __name__ == "__main__":
    unittest.main()
