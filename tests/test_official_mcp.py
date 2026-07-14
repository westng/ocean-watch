import json
import sys
import unittest
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

    def test_configure_registers_stdio_bridge(self):
        credentials = {"app_id": "app-1"}
        calls = []

        def fake_run(arguments, check=False):
            calls.append(arguments)
            return SimpleNamespace(returncode=0, stdout="{}", stderr="")

        with mock.patch.object(authorization_store, "read_app", return_value=credentials), \
                mock.patch.object(credential_store, "read_credentials", return_value={}), \
                mock.patch.object(credential_store, "configure_developer_id"), \
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
