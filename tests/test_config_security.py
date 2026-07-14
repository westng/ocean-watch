import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from ocean_watch.auth import credential_store
from ocean_watch.core import config_paths, config_store, process_lock

ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "ads-plan-monitor"


class ConfigStoreTests(unittest.TestCase):
    def test_atomic_write_replaces_json_and_keeps_backup(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text('{"version": 1}\n', encoding="utf-8")
            config_store.atomic_write_json(path, {"version": 2})
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), {"version": 2})
            self.assertEqual(
                json.loads(path.with_suffix(".json.bak").read_text(encoding="utf-8")),
                {"version": 1},
            )
            self.assertEqual(list(path.parent.glob(".config.json.*.tmp")), [])

class ConfigAndCredentialTests(unittest.TestCase):
    def test_skill_metadata_has_required_frontmatter(self):
        text = (SKILL / "SKILL.md").read_text(encoding="utf-8")
        self.assertTrue(text.startswith("---\nname: ads-plan-monitor\ndescription:"))
        self.assertGreaterEqual(text.count("\n---\n"), 1)

    def test_repository_config_resolves_from_plugin_checkout(self):
        self.assertEqual(
            config_paths.project_config_path(),
            ROOT / "config" / "ads-plan-monitor" / "config.json",
        )

    def test_installed_skill_without_git_uses_home_config(self):
        with tempfile.TemporaryDirectory() as directory:
            start = Path(directory) / "skills" / "ads-plan-monitor" / "scripts"
            start.mkdir(parents=True)
            with mock.patch.object(config_paths, "repository_root", return_value=None):
                self.assertEqual(
                    config_paths.resolve_config_path(),
                    config_paths.home_config_path(),
                )

    def test_environment_config_precedence(self):
        with mock.patch.dict(os.environ, {config_paths.CONFIG_ENV: "~/env-config.json"}, clear=False):
            self.assertEqual(config_paths.resolve_config_path(), Path("~/env-config.json").expanduser())
            self.assertEqual(config_paths.resolve_config_path("explicit.json"), Path("explicit.json"))

    def test_plaintext_fallback_requires_opt_in(self):
        with mock.patch.object(credential_store.platform, "system", return_value="Linux"), \
                mock.patch.object(credential_store, "has_command", return_value=False), \
                mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(credential_store.backend_name(), "unavailable")
        with mock.patch.object(credential_store.platform, "system", return_value="Linux"), \
                mock.patch.object(credential_store, "has_command", return_value=False), \
                mock.patch.dict(os.environ, {credential_store.INSECURE_FALLBACK_ENV: "1"}, clear=True):
            self.assertEqual(credential_store.backend_name(), "file-fallback")

    def test_sensitive_fields_are_removed(self):
        config = {"api": {"access_token": "a", "refresh_token": "r", "secret": "s", "base_url": "u"}}
        cleaned = credential_store.strip_sensitive_config(config)
        self.assertEqual(cleaned["api"], {"base_url": "u"})

    def test_developer_id_is_not_merged_into_business_api_config(self):
        merged = credential_store.merge_credentials(
            {"api": {"base_url": "https://example.com"}},
            {"access_token": "token", "developer_id": "developer-1"},
        )
        self.assertEqual(merged["api"]["access_token"], "token")
        self.assertNotIn("developer_id", merged["api"])

    def test_macos_hex_encoded_credentials_are_decoded(self):
        data = {"access_token": "token", "refresh_token": "refresh"}
        encoded = json.dumps(data).encode("utf-8").hex()
        self.assertEqual(credential_store.decode_stored_credentials(encoded), data)

    def test_invalid_stored_credentials_raise_clear_error(self):
        with self.assertRaisesRegex(RuntimeError, "not valid JSON"):
            credential_store.decode_stored_credentials("not-json")

class FileLockTests(unittest.TestCase):
    def test_process_lock_records_owner_metadata_and_releases_handle(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token.lock"
            lock = process_lock.ProcessLock(path, timeout=0.1)
            with lock:
                self.assertIsNotNone(lock.handle)
            self.assertIsNone(lock.handle)
            metadata = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(metadata["pid"], os.getpid())
            self.assertEqual(metadata["nonce"], lock.nonce)

    def test_process_lock_times_out_when_same_file_is_held(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token.lock"
            with process_lock.ProcessLock(path, timeout=0.1):
                with self.assertRaises(TimeoutError):
                    with process_lock.ProcessLock(path, timeout=0.01):
                        pass
