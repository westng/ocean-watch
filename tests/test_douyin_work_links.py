import unittest

from ocean_watch.materials import douyin_work_links


class FakeResponse:
    def __init__(self, url, payload=None):
        self.url = url
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *_):
        return None

    def geturl(self):
        return self.url

    def read(self, _=None):
        return self.payload or b""


class FakeOpener:
    def __init__(self, resolved_url, payload=None):
        self.resolved_url = resolved_url
        self.payload = payload
        self.calls = []

    def open(self, request, timeout=None):
        self.calls.append((request.full_url, timeout))
        return FakeResponse(self.resolved_url, self.payload)


class FakeMetadataResolver:
    def __init__(self, result=None, error=None):
        self.result = result
        self.error = error
        self.calls = []

    def resolve_many(self, work_ids, *, concurrency):
        self.calls.append((work_ids, concurrency))
        if self.error:
            raise self.error
        return self.result


class DouyinWorkLinkTests(unittest.TestCase):
    def test_full_work_url_is_parsed_without_network(self):
        opener = FakeOpener("unused")
        resolver = douyin_work_links.DouyinWorkLinkResolver(opener=opener)
        result = resolver.resolve(
            "https://www.douyin.com/video/7000000000000000001?previous_page=web_code_link"
        )
        self.assertEqual(result["aweme_item_id"], "7000000000000000001")
        self.assertEqual(
            result["canonical_url"],
            "https://www.douyin.com/video/7000000000000000001",
        )
        self.assertEqual(opener.calls, [])

    def test_short_link_follows_redirect_and_extracts_work_id(self):
        opener = FakeOpener(
            "https://www.douyin.com/video/7000000000000000001?previous_page=web_code_link"
        )
        resolver = douyin_work_links.DouyinWorkLinkResolver(opener=opener)
        result = resolver.resolve("https://v.douyin.com/abc123/")
        self.assertEqual(result["aweme_item_id"], "7000000000000000001")
        self.assertEqual(len(opener.calls), 1)

    def test_short_link_ignores_trailing_command_noise(self):
        noisy_links = [
            "https://v.douyin.com/abc123/:3pm",
            "https://v.douyin.com/abc123/05/24",
            "https://v.douyin.com/abc123/C@u.SY:4pm",
        ]
        for noisy_link in noisy_links:
            with self.subTest(noisy_link=noisy_link):
                opener = FakeOpener(
                    "https://www.douyin.com/video/7000000000000000001"
                )
                resolver = douyin_work_links.DouyinWorkLinkResolver(opener=opener)

                result = resolver.resolve(noisy_link)

                self.assertEqual(result["aweme_item_id"], "7000000000000000001")
                self.assertEqual(result["input_url"], "https://v.douyin.com/abc123/")
                self.assertEqual(opener.calls[0][0], "https://v.douyin.com/abc123/")

    def test_non_douyin_host_is_rejected(self):
        resolver = douyin_work_links.DouyinWorkLinkResolver(opener=FakeOpener("unused"))
        with self.assertRaisesRegex(Exception, "不属于受信任的抖音域名"):
            resolver.resolve("https://example.com/video/7000000000000000001")

    def test_short_link_rejects_untrusted_redirect_target(self):
        resolver = douyin_work_links.DouyinWorkLinkResolver(
            opener=FakeOpener("https://example.com/video/7000000000000000001")
        )
        with self.assertRaisesRegex(Exception, "不属于受信任的抖音域名"):
            resolver.resolve("https://v.douyin.com/abc123/")

    def test_official_iesdouyin_redirect_is_allowed(self):
        resolver = douyin_work_links.DouyinWorkLinkResolver(
            opener=FakeOpener(
                "https://www.iesdouyin.com/share/video/7000000000000000001/"
            )
        )
        result = resolver.resolve("https://v.douyin.com/abc123/")
        self.assertEqual(result["aweme_item_id"], "7000000000000000001")

    def test_plain_http_link_is_rejected(self):
        resolver = douyin_work_links.DouyinWorkLinkResolver(opener=FakeOpener("unused"))
        with self.assertRaisesRegex(Exception, "HTTPS"):
            resolver.resolve("http://v.douyin.com/abc123/")

    def test_duplicate_works_are_skipped_after_resolution(self):
        resolver = douyin_work_links.DouyinWorkLinkResolver(opener=FakeOpener("unused"))
        result = douyin_work_links.resolve_work_links([
            "https://www.douyin.com/video/7000000000000000001",
            "https://www.douyin.com/video/7000000000000000001?from=share",
        ], resolver=resolver)
        self.assertEqual(len(result["resolved"]), 1)
        self.assertEqual(result["skipped"][0]["reason"], "duplicate_input")

    def test_f2_runs_once_and_fills_complete_identity(self):
        metadata = FakeMetadataResolver({
            "results": {
                "7000000000000000001": {
                    "aweme_item_id": "7000000000000000001",
                    "creator_name_hint": "达人甲",
                    "owner_hint": {
                        "aweme_id": "9001",
                        "aweme_show_id": "creator-one",
                        "source": "f2_cli",
                    },
                    "product_hint": {
                        "product_id": "1001",
                        "product_name": "商品甲",
                        "source": "f2_cli",
                    },
                    "metadata": {
                        "code": 200,
                        "message": "数据获取成功",
                        "data": {"author": {}, "product": {}, "video": {}},
                    },
                },
            },
            "errors": {},
        })

        result = douyin_work_links.resolve_work_links(
            [
                "https://www.douyin.com/video/7000000000000000001",
                "https://www.douyin.com/video/7000000000000000001?from=share",
            ],
            resolver=douyin_work_links.DouyinWorkLinkResolver(opener=FakeOpener("unused")),
            concurrency=2,
            metadata_resolver=metadata,
        )

        self.assertEqual(metadata.calls, [(["7000000000000000001"], 2)])
        self.assertEqual(result["resolved"][0]["owner_hint"]["source"], "f2_cli")
        self.assertEqual(result["resolved"][0]["creator_name_hint"], "达人甲")
        self.assertEqual(result["resolved"][0]["product_hint"]["product_id"], "1001")
        self.assertEqual(result["resolved"][0]["metadata"]["code"], 200)

    def test_f2_failure_is_compact_and_keeps_resolved_work(self):
        metadata = FakeMetadataResolver(
            error=RuntimeError("cookie=secret https://private.example.test")
        )

        result = douyin_work_links.resolve_work_links(
            ["https://www.douyin.com/video/7000000000000000001"],
            resolver=douyin_work_links.DouyinWorkLinkResolver(opener=FakeOpener("unused")),
            metadata_resolver=metadata,
        )

        self.assertEqual(len(result["resolved"]), 1)
        warning = result["resolved"][0]["hint_warning"]
        self.assertEqual(warning["code"], "f2_metadata_query_failed")
        self.assertNotIn("secret", str(warning))
        self.assertNotIn("private.example.test", str(warning))


if __name__ == "__main__":
    unittest.main()
