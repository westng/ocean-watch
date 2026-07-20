import json
import sys
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from ocean_watch.auth import authorization_store, credential_store
from ocean_watch.integrations import configure_official_mcp, oceanengine_mcp_bridge


class OfficialMcpTests(unittest.TestCase):
    def bridge_server(self):
        return {
            "transport": {
                "type": "stdio",
                "command": sys.executable,
                "args": [str(configure_official_mcp.bridge_path())],
            }
        }

    def test_bridge_url_encodes_ids(self):
        url = oceanengine_mcp_bridge.build_url("app id", "developer/id")
        self.assertIn("app_id=app+id", url)
        self.assertIn("developer_id=developer%2Fid", url)

    def test_bridge_rejects_untrusted_message_endpoint(self):
        with self.assertRaisesRegex(RuntimeError, "untrusted"):
            oceanengine_mcp_bridge.validated_message_endpoint(
                "https://open.oceanengine.com/sse", "https://example.com/messages"
            )

    def test_bridge_rejects_untrusted_origin(self):
        with self.assertRaisesRegex(ValueError, "open.oceanengine.com"):
            oceanengine_mcp_bridge.LegacySseBridge("https://example.com/sse")

    def test_bridge_default_transport_refuses_redirects(self):
        request = mock.Mock()
        with mock.patch.object(urllib.request, "build_opener") as build:
            opener = build.return_value
            opener.open.side_effect = urllib.error.HTTPError(
                "https://open.oceanengine.com/sse",
                302,
                "redirect",
                {},
                None,
            )
            with self.assertRaises(urllib.error.HTTPError):
                oceanengine_mcp_bridge.default_opener(request, 30)
        handler = build.call_args.args[0]
        self.assertIsNone(
            handler.redirect_request(None, None, 302, "redirect", {}, "https://example.com")
        )

    def test_bridge_dispatches_final_sse_line_without_newline(self):
        response = mock.MagicMock()
        response.__enter__.return_value = response
        response.__exit__.return_value = False
        response.readline.side_effect = [
            b"event: message\n",
            b'data: {"jsonrpc":"2.0","id":"final","result":{}}',
            b"",
        ]
        messages = []
        bridge = oceanengine_mcp_bridge.LegacySseBridge(
            "https://open.oceanengine.com/sse",
            message_handler=messages.append,
            opener=lambda _request, timeout: response,
        )

        bridge.read_sse()

        self.assertEqual(messages, [{"jsonrpc": "2.0", "id": "final", "result": {}}])

    def test_bridge_rejects_oversized_event_and_post_response(self):
        event_response = mock.MagicMock()
        event_response.__enter__.return_value = event_response
        event_response.__exit__.return_value = False
        event_response.readline.side_effect = [b"data: " + (b"x" * 32), b""]
        bridge = oceanengine_mcp_bridge.LegacySseBridge(
            "https://open.oceanengine.com/sse",
            opener=lambda _request, timeout: event_response,
            max_event_bytes=16,
        )
        bridge.read_sse()
        self.assertIn("size limit", bridge.failure)

        post_response = mock.MagicMock()
        post_response.__enter__.return_value = post_response
        post_response.__exit__.return_value = False
        post_response.headers = {"Content-Length": "32"}
        post_response.read.return_value = b""
        bridge = oceanengine_mcp_bridge.LegacySseBridge(
            "https://open.oceanengine.com/sse",
            opener=lambda _request, timeout: post_response,
            max_response_bytes=16,
        )
        bridge.message_endpoint = "https://open.oceanengine.com/messages"
        with self.assertRaisesRegex(RuntimeError, "size limit"):
            bridge.send(b"{}")

    def test_status_is_redacted(self):
        with mock.patch.object(
            configure_official_mcp,
            "get_server",
            return_value=self.bridge_server(),
        ), mock.patch.object(configure_official_mcp.shutil, "which", return_value="codex"):
            result = configure_official_mcp.status(
                {"app_id": "app-1", "developer_id": "developer-1"}
            )
        self.assertTrue(result["ready"])
        self.assertNotIn("app-1", json.dumps(result))
        self.assertNotIn("developer-1", json.dumps(result))

    def test_capabilities_uses_runtime_tool_listing_and_is_redacted(self):
        with mock.patch.object(
            authorization_store,
            "read_app",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "read_credentials",
            return_value={"developer_id": "developer-1"},
        ), mock.patch.object(
            oceanengine_mcp_bridge,
            "discover_tools",
            return_value=[
                {
                    "name": "qianchuan_report_material_get_v1",
                    "description": "material report",
                    "inputSchema": {"type": "object"},
                },
                {
                    "name": "qianchuan_video_get_v1",
                    "description": "video list",
                    "inputSchema": {"type": "object"},
                },
            ],
        ) as discover:
            result = configure_official_mcp.capabilities()

        self.assertEqual(result["source"], "runtime_tools_list")
        self.assertEqual(result["tool_count"], 2)
        self.assertEqual(result["tools"], [
            {
                "name": "qianchuan_report_material_get_v1",
                "description": "material report",
            },
            {"name": "qianchuan_video_get_v1", "description": "video list"},
        ])
        self.assertNotIn("app-1", json.dumps(result))
        self.assertNotIn("developer-1", json.dumps(result))
        discover.assert_called_once_with("app-1", "developer-1")

    def test_capabilities_returns_exact_runtime_tool_schema(self):
        tool = {
            "name": "qianchuan_uni_promotion_list_v1",
            "description": "plan list",
            "inputSchema": {
                "type": "object",
                "properties": {"advertiser_id": {"type": "integer"}},
                "required": ["advertiser_id"],
            },
        }
        with mock.patch.object(
            authorization_store,
            "read_app",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "read_credentials",
            return_value={"developer_id": "developer-1"},
        ), mock.patch.object(
            oceanengine_mcp_bridge,
            "discover_tools",
            return_value=[tool],
        ):
            result = configure_official_mcp.capabilities(tool["name"])
        self.assertEqual(result["tool_count"], 1)
        self.assertEqual(result["tools"], [tool])

    def test_capabilities_requires_configured_developer_id(self):
        with mock.patch.object(
            authorization_store,
            "read_app",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "read_credentials",
            return_value={},
        ), mock.patch.object(oceanengine_mcp_bridge, "discover_tools") as discover:
            with self.assertRaisesRegex(RuntimeError, "developer_id is missing"):
                configure_official_mcp.capabilities()
        discover.assert_not_called()

    def test_configure_registers_stdio_bridge(self):
        credentials = {"app_id": "app-1"}
        calls = []

        def fake_run(arguments, check=False):
            calls.append(arguments)
            return SimpleNamespace(returncode=0, stdout="{}", stderr="")

        with mock.patch.object(authorization_store, "read_app", return_value=credentials), \
                mock.patch.object(credential_store, "read_credentials", return_value={}), \
                mock.patch.object(credential_store, "configure_developer_id") as save, \
                mock.patch.object(oceanengine_mcp_bridge, "probe", return_value=["tool-1"]), \
                mock.patch.object(configure_official_mcp, "get_server", side_effect=[None, self.bridge_server()]), \
                mock.patch.object(configure_official_mcp, "run_codex", side_effect=fake_run), \
                mock.patch.object(configure_official_mcp.shutil, "which", return_value="codex"):
            result = configure_official_mcp.configure("developer-1")

        self.assertTrue(result["ready"])
        self.assertEqual(result["verified_tool_count"], 1)
        self.assertEqual(calls[0][:4], ["mcp", "add", configure_official_mcp.SERVER_NAME, "--"])
        self.assertEqual(Path(calls[0][4]).resolve(), Path(sys.executable).resolve())
        self.assertEqual(Path(calls[0][5]).resolve(), configure_official_mcp.bridge_path())
        self.assertNotIn("app-1", json.dumps(calls))
        self.assertNotIn("developer-1", json.dumps(calls))
        save.assert_called_once_with("developer-1")

    def test_failed_registration_does_not_store_developer_id(self):
        with mock.patch.object(
            authorization_store,
            "read_app",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "configure_developer_id",
        ) as save, mock.patch.object(
            configure_official_mcp,
            "get_server",
            return_value=None,
        ), mock.patch.object(
            oceanengine_mcp_bridge,
            "probe",
            return_value=["tool-1"],
        ), mock.patch.object(
            configure_official_mcp,
            "run_codex",
            return_value=SimpleNamespace(returncode=1, stdout="", stderr="failed"),
        ):
            with self.assertRaisesRegex(RuntimeError, "Unable to register"):
                configure_official_mcp.configure("developer-1")
        save.assert_not_called()

    def test_failed_probe_does_not_change_local_state(self):
        with mock.patch.object(
            authorization_store,
            "read_app",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "configure_developer_id",
        ) as save, mock.patch.object(
            oceanengine_mcp_bridge,
            "probe",
            side_effect=RuntimeError("Official MCP rejected the configured developer credentials"),
        ), mock.patch.object(
            configure_official_mcp,
            "run_codex",
        ) as run_codex:
            with self.assertRaisesRegex(RuntimeError, "rejected"):
                configure_official_mcp.configure("developer-1")
        save.assert_not_called()
        run_codex.assert_not_called()
