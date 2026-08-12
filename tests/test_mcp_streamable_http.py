import io
import json
import unittest
import urllib.error
from unittest import mock

from ocean_watch.core.errors import ApiError
from ocean_watch.integrations import mcp_streamable_http


class FakeResponse:
    def __init__(
        self,
        payload,
        *,
        content_type="application/json",
        headers=None,
        status=200,
    ):
        if isinstance(payload, (dict, list)):
            payload = json.dumps(payload)
        if payload is None:
            payload = b""
        elif not isinstance(payload, bytes):
            payload = str(payload).encode("utf-8")
        self.body = payload
        self.stream = io.BytesIO(self.body)
        self.headers = {"Content-Type": content_type, **(headers or {})}
        self.status = status
        self.read_sizes = []
        self.readline_sizes = []

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, size=-1):
        self.read_sizes.append(size)
        return self.stream.read(size)

    def readline(self, size=-1):
        self.readline_sizes.append(size)
        return self.stream.readline(size)


class SequentialOpener:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def __call__(self, request, timeout):
        self.calls.append((request, timeout))
        response = self.responses.pop(0)
        if isinstance(response, BaseException):
            raise response
        return response


class RecordingThrottle:
    def __init__(self):
        self.events = []

    def acquire(self):
        self.events.append("acquire")

    def observe(self, payload, *, headers=None):
        self.events.append(("observe", payload, headers))

    def release(self):
        self.events.append("release")


class StreamableHttpMcpClientTests(unittest.TestCase):
    def test_throttle_observes_business_rate_limit_before_release(self):
        throttle = RecordingThrottle()
        response = FakeResponse({
            "jsonrpc": "2.0",
            "id": "ocean-watch-1",
            "result": {
                "content": [{
                    "type": "text",
                    "text": '{"code":40100,"message":"limited"}',
                }],
            },
        })
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=SequentialOpener([response]),
            request_throttle=throttle,
        )
        client.initialized = True

        with self.assertRaises(ApiError):
            client.call_tool("report_tool", {})

        self.assertEqual(throttle.events[0], "acquire")
        self.assertEqual(throttle.events[1][0], "observe")
        self.assertEqual(throttle.events[1][1]["code"], 40100)
        self.assertEqual(throttle.events[2], "release")

    def test_initializes_and_calls_only_allowed_tool(self):
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {
                    "protocolVersion": "2025-03-26",
                    "capabilities": {"tools": {}},
                },
            }),
            FakeResponse(None, status=202),
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-2",
                "result": {
                    "content": [{"type": "text", "text": '{"code":0,"data":{"rows":[]}}'}],
                },
            }),
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        result = client.call_tool("report_tool", {"advertiser_id": 123})

        self.assertEqual(result["data"]["rows"], [])
        self.assertEqual(len(opener.calls), 3)
        request = opener.calls[-1][0]
        self.assertEqual(request.get_header("Access-token"), "secret-token")
        self.assertEqual(json.loads(request.get_header("Tool-range")), ["report_tool"])
        payload = json.loads(request.data.decode("utf-8"))
        self.assertEqual(payload["method"], "tools/call")
        self.assertEqual(payload["params"]["name"], "report_tool")

    def test_stateless_initialize_sends_notification_before_marking_initialized(self):
        notification_failure = urllib.error.URLError(TimeoutError("timed out"))
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            notification_failure,
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        with self.assertRaises(ApiError):
            client.initialize()

        self.assertFalse(client.initialized)
        self.assertEqual(len(opener.calls), 2)
        notification = opener.calls[-1][0]
        payload = json.loads(notification.data.decode("utf-8"))
        self.assertEqual(payload["method"], "notifications/initialized")
        self.assertIsNone(notification.get_header("Mcp-session-id"))

    def test_stateless_initialize_marks_initialized_after_notification(self):
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        client.initialize()

        self.assertTrue(client.initialized)
        self.assertEqual(len(opener.calls), 2)

    def test_decodes_streamable_http_event_stream(self):
        body = (
            "event: message\n"
            'data: {"jsonrpc":"2.0","id":"1","result":{"tools":[]}}\n\n'
        )
        decoded = mcp_streamable_http._decode_response(body, "text/event-stream")
        self.assertEqual(decoded["result"]["tools"], [])

    def test_event_stream_stops_on_matching_json_rpc_response(self):
        stream = FakeResponse(
            "".join([
                'data: {"jsonrpc":"2.0","id":"another","result":{"tools":[]}}\n\n',
                'data: {"jsonrpc":"2.0","id":"ocean-watch-2",',
                '"result":{"tools":[{"name":"report_tool"}]}}\n\n',
                "data: this trailing event must not be read\n\n",
            ]),
            content_type="text/event-stream",
        )
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
            stream,
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        tools = client.list_tools()

        self.assertEqual(tools, [{"name": "report_tool"}])
        self.assertLess(stream.stream.tell(), len(stream.body))

    def test_event_stream_supports_json_larger_than_read_chunk(self):
        large_value = "x" * (mcp_streamable_http.READ_CHUNK_BYTES + 1024)
        stream = FakeResponse(
            "data: " + json.dumps({
                "jsonrpc": "2.0",
                "id": "ocean-watch-2",
                "result": {"tools": [{"name": large_value}]},
            }) + "\n\n",
            content_type="text/event-stream",
        )
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
            stream,
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        self.assertEqual(client.list_tools()[0]["name"], large_value)

    def test_event_stream_decodes_final_data_line_without_newline(self):
        stream = FakeResponse(
            'data: {"jsonrpc":"2.0","id":"ocean-watch-2",'
            '"result":{"tools":[{"name":"report_tool"}]}}',
            content_type="text/event-stream",
        )
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
            stream,
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            opener=opener,
        )

        self.assertEqual(client.list_tools(), [{"name": "report_tool"}])

    def test_rejects_response_over_size_limit_with_bounded_reads(self):
        response = FakeResponse('{"result":"' + ("x" * 80) + '"}')
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "secret-token",
            tool_range=["report_tool"],
            max_response_bytes=32,
            opener=SequentialOpener([response]),
        )

        with self.assertRaisesRegex(Exception, "size limit") as raised:
            client.initialize()

        self.assertEqual(raised.exception.details["max_response_bytes"], 32)
        self.assertTrue(response.read_sizes)
        self.assertLessEqual(max(response.read_sizes), 33)

    def test_rejects_untrusted_endpoint_and_unlisted_tool(self):
        with self.assertRaisesRegex(Exception, "open.oceanengine.com"):
            mcp_streamable_http.StreamableHttpMcpClient(
                "https://example.com/mcp",
                "token",
                tool_range=["tool"],
            )
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "token",
            tool_range=["allowed"],
        )
        with self.assertRaisesRegex(Exception, "Tool-Range"):
            client.call_tool("not-allowed", {})

    def test_json_response_requires_matching_id_and_version(self):
        for response, message in (
            ({
                "jsonrpc": "2.0",
                "id": "wrong-id",
                "result": {"protocolVersion": "2025-03-26"},
            }, "mismatched"),
            ({
                "jsonrpc": "1.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }, "version"),
        ):
            with self.subTest(message=message):
                client = mcp_streamable_http.StreamableHttpMcpClient(
                    "https://open.oceanengine.com/qianchuan/mcp",
                    "secret-token",
                    tool_range=["report_tool"],
                    opener=SequentialOpener([FakeResponse(response)]),
                )
                with self.assertRaisesRegex(ApiError, message):
                    client.initialize()

    def test_default_transport_refuses_redirects_before_reusing_headers(self):
        request = mock.Mock()
        with mock.patch.object(urllib.request, "build_opener") as build:
            opener = build.return_value
            opener.open.side_effect = urllib.error.HTTPError(
                "https://open.oceanengine.com/qianchuan/mcp",
                302,
                "redirect",
                {},
                None,
            )
            with self.assertRaises(urllib.error.HTTPError):
                mcp_streamable_http.default_opener(request, 30)
        handler = build.call_args.args[0]
        self.assertIsInstance(handler, mcp_streamable_http.RejectRedirects)
        self.assertIsNone(
            handler.redirect_request(None, None, 302, "redirect", {}, "https://example.com")
        )

    def test_business_error_does_not_expose_access_token(self):
        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-2",
                "result": {
                    "content": [{
                        "type": "text",
                        "text": '{"code":40001,"message":"invalid request","request_id":"r1"}',
                    }],
                },
            }),
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "never-print-me",
            tool_range=["report_tool"],
            opener=opener,
        )
        with self.assertRaises(Exception) as raised:
            client.call_tool("report_tool", {})
        self.assertNotIn("never-print-me", str(raised.exception))
        self.assertEqual(raised.exception.details["request_id"], "r1")

    def test_protocol_and_business_errors_redact_access_token(self):
        protocol_client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "never-print-me",
            tool_range=["report_tool"],
            opener=SequentialOpener([FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "error": {
                    "code": -32000,
                    "message": "token never-print-me rejected",
                },
            })]),
        )
        with self.assertRaises(ApiError) as protocol_error:
            protocol_client.initialize()
        self.assertNotIn("never-print-me", json.dumps(protocol_error.exception.details))
        self.assertIn("[REDACTED]", protocol_error.exception.details["message"])

        opener = SequentialOpener([
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-1",
                "result": {"protocolVersion": "2025-03-26"},
            }),
            FakeResponse(None, status=202),
            FakeResponse({
                "jsonrpc": "2.0",
                "id": "ocean-watch-2",
                "result": {
                    "content": [{
                        "type": "text",
                        "text": '{"code":40001,"message":"never-print-me invalid"}',
                    }],
                },
            }),
        ])
        business_client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "never-print-me",
            tool_range=["report_tool"],
            opener=opener,
        )
        with self.assertRaises(ApiError) as business_error:
            business_client.call_tool("report_tool", {})
        self.assertNotIn("never-print-me", json.dumps(business_error.exception.details))
        self.assertIn("[REDACTED]", business_error.exception.details["message"])

    def test_timeout_transport_error_has_retry_details_without_token(self):
        opener = SequentialOpener([
            urllib.error.URLError(TimeoutError("never-print-me timed out")),
        ])
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "never-print-me",
            tool_range=["report_tool"],
            opener=opener,
        )

        with self.assertRaises(Exception) as raised:
            client.initialize()

        self.assertEqual(raised.exception.details["transport_error"], "timeout")
        self.assertTrue(raised.exception.details["retryable"])
        self.assertNotIn("never-print-me", json.dumps(raised.exception.details))

    def test_http_error_has_normalized_retry_details_and_redacts_token(self):
        body = io.BytesIO(b'{"message":"token never-print-me was rejected"}')
        error = urllib.error.HTTPError(
            "https://open.oceanengine.com/qianchuan/mcp",
            503,
            "service unavailable",
            {"Content-Type": "application/json"},
            body,
        )
        client = mcp_streamable_http.StreamableHttpMcpClient(
            "https://open.oceanengine.com/qianchuan/mcp",
            "never-print-me",
            tool_range=["report_tool"],
            opener=SequentialOpener([error]),
        )

        with self.assertRaises(Exception) as raised:
            client.initialize()

        details = raised.exception.details
        self.assertEqual(details["transport_error"], "http")
        self.assertEqual(details["http_status"], 503)
        self.assertTrue(details["retryable"])
        self.assertIn("[REDACTED]", details["message"])
        self.assertNotIn("never-print-me", json.dumps(details))


if __name__ == "__main__":
    unittest.main()
