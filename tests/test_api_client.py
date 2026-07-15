import io
import json
import unittest
import urllib.error
import urllib.request
from unittest import mock

from ocean_watch.api import client
from ocean_watch.core.errors import ApiError


class FakeResponse:
    def __init__(self, payload, headers=None):
        self.body = json.dumps(payload).encode("utf-8")
        self.headers = headers or {}

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, size=-1):
        return self.body[:size]


class OceanEngineClientTests(unittest.TestCase):
    def test_default_client_requires_official_https_host(self):
        for base_url in (
            "http://api.oceanengine.com/open_api",
            "https://example.com/open_api",
            "file:///tmp/open_api",
        ):
            with self.subTest(base_url=base_url), self.assertRaises(ApiError):
                client.OceanEngineClient(base_url, "token")

    def test_default_transport_refuses_redirects(self):
        request = urllib.request.Request("https://api.oceanengine.com/open_api/test")
        with mock.patch.object(urllib.request, "build_opener") as build:
            build.return_value.open.side_effect = urllib.error.HTTPError(
                request.full_url,
                302,
                "redirect",
                {},
                None,
            )
            with self.assertRaises(urllib.error.HTTPError):
                client.default_opener(request, 30)
        self.assertIsInstance(build.call_args.args[0], client.RejectRedirects)

    def test_injected_transport_is_bounded_and_keeps_query_encoding(self):
        calls = []

        def opener(request, timeout):
            calls.append((request, timeout))
            return FakeResponse({"code": 0})

        api = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=opener,
            max_response_bytes=128,
        )
        self.assertEqual(api.get("/path", {"ids": [1, 2]}), {"code": 0})
        self.assertIn("ids=%5B1%2C+2%5D", calls[0][0].full_url)
        self.assertEqual(calls[0][0].get_header("Access-token"), "token")

    def test_http_error_payload_is_bounded_and_token_is_redacted(self):
        token = "never-print-token"
        error = urllib.error.HTTPError(
            "https://api.oceanengine.com/open_api/path",
            503,
            "unavailable",
            {},
            io.BytesIO(json.dumps({"message": f"token {token}"}).encode("utf-8")),
        )

        def opener(_request, timeout):
            self.assertEqual(timeout, 30)
            raise error

        result = client.OceanEngineClient(
            "https://api.test/open_api",
            token,
            opener=opener,
        ).get("/path")
        self.assertEqual(result["code"], 503)
        self.assertNotIn(token, json.dumps(result))


if __name__ == "__main__":
    unittest.main()
