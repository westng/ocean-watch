import json
import os
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import authorization_store, credential_store
from ocean_watch.core import config_paths, config_store, process_lock
from ocean_watch.core.errors import ConfigurationConflictError
from ocean_watch.onboarding import first_run

ROOT = Path(__file__).resolve().parents[1]
MARKETING_SKILL = ROOT / "skills" / "ads-plan-monitor"
QIANCHUAN_SKILL = ROOT / "skills" / "qc-plan-monitor"


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
            if os.name != "nt":
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                self.assertEqual(
                    path.with_suffix(".json.bak").stat().st_mode & 0o777,
                    0o600,
                )

    def test_compare_and_swap_rejects_stale_config(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            original = {"version": 1}
            config_store.atomic_write_json(path, original)
            revision = config_store.json_revision(original)
            config_store.atomic_write_json(path, {"version": 2})
            with self.assertRaisesRegex(ConfigurationConflictError, "changed") as raised:
                config_store.compare_and_swap_json(path, revision, {"version": 3})
            self.assertEqual(raised.exception.code, "configuration_conflict")
            self.assertEqual(config_store.load_json(path), {"version": 2})

    def test_initialize_json_does_not_replace_an_existing_file(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            config_store.atomic_write_json(path, {"owner": "existing"})

            created = config_store.initialize_json(
                path,
                lambda: {"owner": "initializer"},
            )

            self.assertFalse(created)
            self.assertEqual(config_store.load_json(path), {"owner": "existing"})

    def test_initialize_json_creates_missing_file_with_private_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "nested" / "config.json"

            created = config_store.initialize_json(path, lambda: {"version": 1})

            self.assertTrue(created)
            self.assertEqual(config_store.load_json(path), {"version": 1})
            if os.name != "nt":
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)

class ConfigAndCredentialTests(unittest.TestCase):
    def test_skill_metadata_has_required_frontmatter(self):
        for skill, name in (
            (MARKETING_SKILL, "ads-plan-monitor"),
            (QIANCHUAN_SKILL, "qc-plan-monitor"),
        ):
            text = (skill / "SKILL.md").read_text(encoding="utf-8")
            self.assertTrue(text.startswith(f"---\nname: {name}\ndescription:"))
            self.assertGreaterEqual(text.count("\n---\n"), 1)

    def test_qianchuan_assets_belong_to_qc_skill(self):
        for filename in (
            "qianchuan-product-plan.example.json",
            "qianchuan-live-plan.example.json",
        ):
            self.assertTrue((QIANCHUAN_SKILL / "assets" / filename).is_file())
            self.assertFalse((MARKETING_SKILL / "assets" / filename).exists())

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

    def test_installed_plugin_cache_ignores_copied_repository_config(self):
        with tempfile.TemporaryDirectory() as directory:
            codex_home = Path(directory) / ".codex"
            plugin = codex_home / "plugins" / "cache" / "market" / "ocean-watch" / "version"
            module = (
                plugin
                / "skills"
                / "ads-plan-monitor"
                / "src"
                / "ocean_watch"
                / "core"
                / "config_paths.py"
            )
            module.parent.mkdir(parents=True)
            (plugin / ".git").mkdir()
            project_config = plugin / "config" / "ads-plan-monitor" / "config.json"
            project_config.parent.mkdir(parents=True)
            project_config.write_text("{}\n", encoding="utf-8")

            with mock.patch.dict(os.environ, {config_paths.CODEX_HOME_ENV: str(codex_home)}), \
                    mock.patch.object(config_paths, "__file__", str(module)):
                self.assertIsNone(config_paths.repository_root())
                self.assertEqual(
                    config_paths.resolve_config_path(),
                    codex_home / "ads-plan-monitor" / "config.json",
                )

    def test_custom_codex_home_owns_config_and_authorization_state(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {config_paths.CODEX_HOME_ENV: directory},
            clear=False,
        ):
            codex_home = Path(directory)
            self.assertEqual(
                config_paths.home_config_path(),
                codex_home / "ads-plan-monitor" / "config.json",
            )
            self.assertEqual(
                authorization_store.state_root(),
                codex_home / "ads-plan-monitor" / "state",
            )
            self.assertEqual(credential_store.credentials_dir(), codex_home / "ads-plan-monitor")

    def test_credential_commands_use_resolved_absolute_paths(self):
        completed = mock.Mock(returncode=1, stdout="")
        with mock.patch.object(
            credential_store.shutil,
            "which",
            return_value="/trusted/bin/security",
        ), mock.patch.object(
            credential_store.subprocess,
            "run",
            return_value=completed,
        ) as run:
            self.assertEqual(credential_store.macos_read(), {})
        self.assertEqual(run.call_args.args[0][0], "/trusted/bin/security")

    def test_wheel_inside_unrelated_git_repository_uses_home_config(self):
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory) / "consumer"
            module = (
                repository
                / ".venv"
                / "lib"
                / "python3.12"
                / "site-packages"
                / "ocean_watch"
                / "core"
                / "config_paths.py"
            )
            module.parent.mkdir(parents=True)
            (repository / ".git").mkdir()
            with mock.patch.object(config_paths, "__file__", str(module)):
                self.assertIsNone(config_paths.repository_root())
                self.assertEqual(
                    config_paths.resolve_config_path(),
                    config_paths.home_config_path(),
                )

    def test_packaged_config_template_matches_plugin_asset(self):
        expected = json.loads(
            (MARKETING_SKILL / "assets" / "config.example.json").read_text(encoding="utf-8")
        )
        with mock.patch.object(Path, "is_file", return_value=False):
            self.assertEqual(first_run.load_config_template(), expected)

    def test_installed_first_run_uses_console_command(self):
        with mock.patch.object(first_run, "skill_root", return_value=Path("/installed/package")):
            self.assertEqual(first_run.cli_command(), "ocean-watch")

    def test_checkout_first_run_uses_local_runner(self):
        with mock.patch.object(first_run, "skill_root", return_value=MARKETING_SKILL):
            command = first_run.cli_command()
        self.assertIn("python3", command)
        self.assertIn(str(MARKETING_SKILL / "run.py"), command)

    def test_packaged_first_run_accepts_unconfigured_advertiser_placeholder(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {config_paths.CODEX_HOME_ENV: directory},
            clear=False,
        ), mock.patch.object(
            Path,
            "is_file",
            return_value=False,
        ), mock.patch.object(
            authorization_store,
            "status",
            return_value={"authorization_count": 0},
        ) as status, mock.patch.object(
            first_run.configure_official_mcp,
            "status",
            return_value={"ready": False},
        ), redirect_stdout(StringIO()) as output:
            exit_code = first_run.main(["--home-config"])

        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertTrue(result["created_config_from_template"])
        self.assertEqual(result["next_action"], "edit_config")
        self.assertTrue(result["template_setup"]["list_command"].startswith("ocean-watch "))
        self.assertTrue(all(call.kwargs["advertiser_id"] is None for call in status.call_args_list))

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

    def test_sensitive_config_migration_does_not_keep_plaintext_backup(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(
                json.dumps({"api": {"access_token": "sensitive", "base_url": "url"}}),
                encoding="utf-8",
            )
            backup = path.with_suffix(".json.bak")
            backup.write_text("old sensitive backup", encoding="utf-8")
            real_lock = config_store.json_file_lock
            with mock.patch.object(
                credential_store,
                "read_credentials",
                return_value={},
            ), mock.patch.object(
                credential_store,
                "write_credentials",
                return_value="test-backend",
            ), mock.patch.object(
                config_store,
                "json_file_lock",
                wraps=real_lock,
            ) as lock:
                credential_store.migrate_from_config(path)
            lock.assert_called_once_with(path)
            self.assertFalse(backup.exists())
            self.assertNotIn("access_token", path.read_text(encoding="utf-8"))

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
