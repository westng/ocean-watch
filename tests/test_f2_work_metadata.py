import asyncio
import io
import json
import subprocess
import unittest
from contextlib import redirect_stdout
from types import SimpleNamespace
from unittest import mock

from ocean_watch.materials import (
    f2_work_metadata,
    f2_work_metadata_cli,
)

WORK_ID = "7000000000000000001"


class FakeHandler:
    calls = []

    def __init__(self, kwargs):
        self.__class__.calls.append(kwargs)

    async def fetch_one_video(self, work_id):
        if work_id.endswith("2"):
            raise RuntimeError("cookie=secret")
        return fake_video(work_id)


class RetryHandler:
    calls = {}

    def __init__(self, _kwargs):
        pass

    async def fetch_one_video(self, work_id):
        calls = self.__class__.calls.get(work_id, 0) + 1
        self.__class__.calls[work_id] = calls
        if work_id.endswith("2") and calls == 1:
            raise RuntimeError("first pass failed")
        return fake_video(work_id)


class MixedSpeedHandler:
    def __init__(self, _kwargs):
        pass

    async def fetch_one_video(self, work_id):
        if work_id.endswith("2"):
            await asyncio.sleep(1)
        return fake_video(work_id)


def fake_video(work_id=WORK_ID, *, unique_id="creator-one", product_id="1001"):
    detail = {
        "aweme_id": work_id,
        "aweme_type": 0,
        "preview_title": "作品标题",
        "author": {
            "nickname": "达人甲",
            "unique_id": unique_id,
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
            "bit_rate": [{
                "play_addr": {"url_list": ["https://example.test/video.mp4"]},
            }],
        },
        "music": {"play_url": {"uri": "music-id"}},
    }
    return SimpleNamespace(_to_raw=lambda: {"aweme_detail": detail, "status_code": 0})


class F2WorkMetadataCliTests(unittest.TestCase):
    def setUp(self):
        FakeHandler.calls = []
        RetryHandler.calls = {}

    def test_maps_f2_raw_response_to_legacy_contract(self):
        self.assertEqual(
            f2_work_metadata_cli.map_video_response(fake_video(), WORK_ID),
            {
                "code": 200,
                "message": "数据获取成功",
                "data": {
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
            },
        )

    def test_invalid_product_json_keeps_legacy_empty_product_shape(self):
        video = fake_video()
        video._to_raw()["aweme_detail"]["anchor_info"]["extra"] = "not-json"
        result = f2_work_metadata_cli.map_video_response(video, WORK_ID)
        self.assertEqual(result["data"]["product"], {
            "product_info_id": "",
            "product_info_img": "",
            "product_info_name": "",
        })

    def test_fetch_many_returns_compact_errors_without_exception_details(self):
        failed_work_id = "7000000000000000002"
        result = asyncio.run(f2_work_metadata_cli.fetch_many(
            [WORK_ID, failed_work_id],
            concurrency=2,
            handler_factory=FakeHandler,
            ttwid_factory=lambda: "visitor-id",
        ))

        self.assertEqual(
            result["results"][WORK_ID]["data"]["product"]["product_info_id"],
            "1001",
        )
        self.assertEqual(FakeHandler.calls[0]["cookie"], "ttwid=visitor-id;")
        self.assertEqual(
            result["errors"][failed_work_id],
            {
                "code": "f2_metadata_query_failed",
                "message": "F2 did not return usable public work metadata",
            },
        )
        self.assertNotIn("secret", str(result))

    def test_fetch_many_retries_only_failed_works_in_same_batch(self):
        retried_work_id = "7000000000000000002"

        result = asyncio.run(f2_work_metadata_cli.fetch_many(
            [WORK_ID, retried_work_id],
            concurrency=2,
            handler_factory=RetryHandler,
            ttwid_factory=lambda: "visitor-id",
        ))

        self.assertTrue(result["ok"])
        self.assertEqual(set(result["results"]), {WORK_ID, retried_work_id})
        self.assertEqual(RetryHandler.calls, {WORK_ID: 1, retried_work_id: 2})
        self.assertEqual(result["performance"]["retry_count"], 1)
        self.assertEqual(result["performance"]["failure_count"], 0)

    def test_fetch_many_bounds_slow_work_and_keeps_completed_result(self):
        slow_work_id = "7000000000000000002"

        result = asyncio.run(f2_work_metadata_cli.fetch_many(
            [WORK_ID, slow_work_id],
            concurrency=2,
            handler_factory=MixedSpeedHandler,
            ttwid_factory=lambda: "visitor-id",
            per_work_timeout=0.01,
            batch_timeout=0.03,
        ))

        self.assertIn(WORK_ID, result["results"])
        self.assertIn(slow_work_id, result["errors"])
        self.assertLess(result["performance"]["total_seconds"], 0.1)
        self.assertEqual(result["performance"]["timed_out_count"], 1)

    def test_runtime_gate_requires_pinned_f2_version(self):
        self.assertEqual(
            f2_work_metadata_cli.validate_f2_runtime(
                lambda _name: f2_work_metadata_cli.EXPECTED_F2_VERSION
            ),
            f2_work_metadata_cli.EXPECTED_F2_VERSION,
        )
        with self.assertRaisesRegex(RuntimeError, "0.0.1.7"):
            f2_work_metadata_cli.validate_f2_runtime(lambda _name: "0.0.1.6")

    def test_main_writes_exactly_one_json_document(self):
        result = {
            "ok": True,
            "mode": "f2_work_metadata",
            "results": {},
            "errors": {},
        }
        output = io.StringIO()
        with mock.patch.object(
            f2_work_metadata_cli,
            "execute",
            return_value=result,
        ), redirect_stdout(output):
            exit_code = f2_work_metadata_cli.main(["--work-id", WORK_ID])

        self.assertEqual(exit_code, 0)
        self.assertEqual(json.loads(output.getvalue()), result)

    def test_main_hides_startup_exception_details(self):
        output = io.StringIO()
        with mock.patch.object(
            f2_work_metadata_cli,
            "execute",
            side_effect=RuntimeError("cookie=secret"),
        ), redirect_stdout(output):
            exit_code = f2_work_metadata_cli.main(["--work-id", WORK_ID])

        self.assertEqual(exit_code, 2)
        self.assertNotIn("secret", output.getvalue())
        self.assertEqual(json.loads(output.getvalue())["error"]["code"], "f2_cli_failed")


class F2WorkMetadataResolverTests(unittest.TestCase):
    def test_resolver_uses_module_cli_and_bounded_timeout(self):
        calls = []

        def runner(command, **kwargs):
            calls.append((command, kwargs))
            results = {
                work_id: {
                    "code": 200,
                    "message": "数据获取成功",
                    "data": {
                        "author": {
                            "uid": "9001",
                            "unique_id": "creator-one",
                            "nickname": "达人甲",
                        },
                        "product": {
                            "product_info_id": "1001",
                            "product_info_name": "商品甲",
                        },
                        "video": {"video_info_id": work_id},
                    },
                }
                for work_id in work_ids
            }
            return subprocess.CompletedProcess(
                command,
                0,
                stdout=json.dumps({
                    "results": results,
                    "errors": {},
                    "performance": {
                        "requested_count": len(work_ids),
                        "success_count": len(work_ids),
                        "failure_count": 0,
                        "concurrency": 8,
                        "first_pass_seconds": 1.2,
                        "retry_count": 0,
                        "retry_seconds": 0.0,
                        "total_seconds": 1.2,
                        "deadline_seconds": 20,
                        "timed_out_count": 0,
                        "slowest_work_id": work_ids[0],
                        "slowest_seconds": 0.8,
                        "unsafe": "cookie=secret",
                    },
                }).encode(),
                stderr=b"cookie=secret",
            )

        work_ids = [str(7000000000000000000 + index) for index in range(1, 51)]
        resolver = f2_work_metadata.F2WorkMetadataCliResolver(runner=runner)
        result = resolver.resolve_many(work_ids, concurrency=8)

        command, kwargs = calls[0]
        self.assertEqual(
            command[:3],
            [
                f2_work_metadata.sys.executable,
                "-m",
                "ocean_watch.materials.f2_work_metadata_cli",
            ],
        )
        self.assertEqual(kwargs["timeout"], 25)
        self.assertIn("skills/ads-plan-monitor/src", kwargs["env"]["PYTHONPATH"])
        self.assertEqual(len(result["results"]), 50)
        self.assertEqual(
            result["results"][work_ids[0]]["product_hint"]["product_id"],
            "1001",
        )
        self.assertEqual(result["performance"]["deadline_seconds"], 20)
        self.assertEqual(result["performance"]["slowest_work_id"], work_ids[0])
        self.assertNotIn("unsafe", result["performance"])
        self.assertNotIn("secret", str(result))

    def test_resolver_rejects_incomplete_identity_and_hides_child_errors(self):
        def runner(command, **_kwargs):
            return subprocess.CompletedProcess(
                command,
                1,
                stdout=json.dumps({
                    "results": {
                        WORK_ID: {
                            "code": 200,
                            "data": {
                                "author": {"uid": "9001", "unique_id": ""},
                                "product": {},
                                "video": {"video_info_id": WORK_ID},
                            },
                        },
                    },
                    "errors": {WORK_ID: {"message": "cookie=secret"}},
                }).encode(),
                stderr=b"cookie=secret",
            )

        result = f2_work_metadata.F2WorkMetadataCliResolver(
            runner=runner,
            timeout=1,
        ).resolve_many([WORK_ID])

        self.assertEqual(result["results"], {})
        self.assertEqual(
            result["errors"][WORK_ID]["message"],
            "F2 未返回可用的公开作品元数据",
        )
        self.assertNotIn("secret", str(result))

    def test_resolver_reports_timeout_without_command_details(self):
        def runner(command, **_kwargs):
            raise subprocess.TimeoutExpired(command, 1, stderr=b"cookie=secret")

        resolver = f2_work_metadata.F2WorkMetadataCliResolver(runner=runner, timeout=1)
        with self.assertRaisesRegex(Exception, "F2 作品解析超时") as raised:
            resolver.resolve_many([WORK_ID])
        self.assertNotIn("secret", str(raised.exception))


if __name__ == "__main__":
    unittest.main()
