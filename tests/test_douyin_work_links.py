import json
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

    def resolve(self, value):
        self.calls.append(value)
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

    def test_metadata_hint_avoids_short_link_redirect(self):
        opener = FakeOpener("unused")
        metadata = FakeMetadataResolver({
            "aweme_item_id": "7000000000000000001",
            "owner_hint": {
                "aweme_id": "9001",
                "aweme_show_id": "creator-one",
                "source": "configured_metadata_api",
            },
            "product_hint": {
                "product_id": "1001",
                "product_name": "Example Product",
                "source": "configured_metadata_api",
            },
        })
        resolver = douyin_work_links.DouyinWorkLinkResolver(
            opener=opener,
            metadata_resolver=metadata,
        )

        result = resolver.resolve("https://v.douyin.com/abc123/")

        self.assertEqual(result["aweme_item_id"], "7000000000000000001")
        self.assertEqual(result["owner_hint"]["aweme_show_id"], "creator-one")
        self.assertEqual(result["product_hint"]["product_id"], "1001")
        self.assertEqual(opener.calls, [])

    def test_metadata_failure_falls_back_to_safe_redirect(self):
        opener = FakeOpener(
            "https://www.douyin.com/video/7000000000000000001"
        )
        metadata = FakeMetadataResolver(
            error=douyin_work_links.WorkLinkError("metadata_failed", "unavailable")
        )
        resolver = douyin_work_links.DouyinWorkLinkResolver(
            opener=opener,
            metadata_resolver=metadata,
        )

        result = resolver.resolve("https://v.douyin.com/abc123/")

        self.assertEqual(result["aweme_item_id"], "7000000000000000001")
        self.assertEqual(result["hint_warning"]["code"], "metadata_failed")
        self.assertEqual(len(opener.calls), 1)

    def test_metadata_api_extracts_only_validated_identity_hint(self):
        payload = json.dumps({
            "code": 200,
            "data": {
                "author": {
                    "unique_id": "creator-one",
                    "uid": "9001",
                    "nickname": "达人甲",
                },
                "product": {
                    "product_info_id": "1001",
                    "product_info_name": "Example Product",
                },
                "video": {"video_info_id": "7000000000000000001"},
            },
        }).encode()
        resolver = douyin_work_links.DouyinWorkMetadataResolver(
            "https://edge.example.test/api",
            opener=FakeOpener("unused", payload=payload),
        )

        result = resolver.resolve("https://v.douyin.com/abc123/")

        self.assertEqual(result, {
            "aweme_item_id": "7000000000000000001",
            "creator_name_hint": "达人甲",
            "owner_hint": {
                "aweme_id": "9001",
                "aweme_show_id": "creator-one",
                "source": "configured_metadata_api",
            },
            "product_hint": {
                "product_id": "1001",
                "product_name": "Example Product",
                "source": "configured_metadata_api",
            },
        })


if __name__ == "__main__":
    unittest.main()
