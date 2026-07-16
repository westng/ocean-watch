import subprocess
import unittest
from unittest import mock

from ocean_watch.onboarding import environment_check


class FakeSocket:
    def __init__(self, *, bind_error=None):
        self.bind_error = bind_error
        self.bound_address = None
        self.closed = False

    def setsockopt(self, *_values):
        return None

    def bind(self, address):
        self.bound_address = address
        if self.bind_error:
            raise self.bind_error

    def close(self):
        self.closed = True


class EnvironmentCheckTests(unittest.TestCase):
    def test_current_python_is_supported(self):
        result = environment_check.check_python()
        self.assertEqual(result["status"], "ready")
        self.assertTrue(result["required"])
        self.assertTrue(result["executable"])

    def test_codex_version_parser_accepts_cli_output(self):
        self.assertEqual(
            environment_check.parse_codex_version("codex-cli 0.144.4"),
            (0, 144, 4),
        )
        self.assertIsNone(environment_check.parse_codex_version("unknown"))

    def test_missing_codex_cli_is_a_nonblocking_warning(self):
        with mock.patch.object(environment_check.shutil, "which", return_value=None):
            result = environment_check.check_codex_cli()
        self.assertEqual(result["status"], "warning")
        self.assertFalse(result["required"])
        self.assertFalse(result["available"])

    def test_old_codex_cli_is_reported(self):
        completed = subprocess.CompletedProcess(
            ["codex", "--version"],
            0,
            stdout="codex-cli 0.100.0\n",
            stderr="",
        )
        with mock.patch.object(
            environment_check.shutil,
            "which",
            return_value="/test/codex",
        ):
            result = environment_check.check_codex_cli(runner=mock.Mock(return_value=completed))
        self.assertEqual(result["status"], "warning")
        self.assertEqual(result["version"], "0.100.0")

    def test_unavailable_credential_backend_is_blocking(self):
        with mock.patch.object(
            environment_check.credential_store,
            "backend_name",
            return_value="unavailable",
        ):
            result = environment_check.check_credential_backend()
        self.assertEqual(result["status"], "blocked")
        self.assertTrue(result["required"])

    def test_callback_check_binds_and_closes_probe(self):
        probe = FakeSocket()
        with mock.patch.object(
            environment_check.socket,
            "getaddrinfo",
            return_value=[
                (
                    environment_check.socket.AF_INET,
                    environment_check.socket.SOCK_STREAM,
                    6,
                    "",
                    ("127.0.0.1", 8787),
                )
            ],
        ):
            result = environment_check.check_callback(
                "http://127.0.0.1:8787/oauth/callback",
                socket_factory=mock.Mock(return_value=probe),
            )
        self.assertEqual(result["status"], "ready")
        self.assertEqual(probe.bound_address, ("127.0.0.1", 8787))
        self.assertTrue(probe.closed)

    def test_busy_callback_port_is_blocking(self):
        probe = FakeSocket(bind_error=OSError("address already in use"))
        with mock.patch.object(
            environment_check.socket,
            "getaddrinfo",
            return_value=[
                (
                    environment_check.socket.AF_INET,
                    environment_check.socket.SOCK_STREAM,
                    6,
                    "",
                    ("127.0.0.1", 8787),
                )
            ],
        ):
            result = environment_check.check_callback(
                "http://127.0.0.1:8787/oauth/callback",
                socket_factory=mock.Mock(return_value=probe),
            )
        self.assertEqual(result["status"], "blocked")
        self.assertIn("unavailable", result["message"])
        self.assertTrue(probe.closed)

    def test_non_loopback_callback_is_blocking(self):
        with mock.patch.object(
            environment_check.socket,
            "getaddrinfo",
            return_value=[
                (
                    environment_check.socket.AF_INET,
                    environment_check.socket.SOCK_STREAM,
                    6,
                    "",
                    ("203.0.113.1", 8787),
                )
            ],
        ):
            result = environment_check.check_callback(
                "http://example.test:8787/oauth/callback"
            )
        self.assertEqual(result["status"], "blocked")
        self.assertIn("loopback", result["message"])

    def test_report_separates_blockers_and_warnings(self):
        ready = {"id": "ready", "required": True, "status": "ready"}
        blocked = {"id": "blocked", "required": True, "status": "blocked"}
        warning = {"id": "warning", "required": False, "status": "warning"}
        with mock.patch.object(environment_check, "check_python", return_value=ready), \
                mock.patch.object(environment_check, "check_platform", return_value=ready), \
                mock.patch.object(environment_check, "check_codex_cli", return_value=warning), \
                mock.patch.object(
                    environment_check,
                    "check_credential_backend",
                    return_value=blocked,
                ), mock.patch.object(environment_check, "check_callback", return_value=ready):
            result = environment_check.environment_report(
                channel="marketing",
                redirect_uri="http://127.0.0.1:8787/oauth/callback",
            )
        self.assertFalse(result["ok"])
        self.assertEqual(result["blocking_checks"], ["blocked"])
        self.assertEqual(result["warnings"], ["warning"])


if __name__ == "__main__":
    unittest.main()
