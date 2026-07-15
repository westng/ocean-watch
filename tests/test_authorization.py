import copy
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from ocean_watch.auth import (
    authorization_store,
    channels,
    credential_store,
    migrate_channels,
)
from ocean_watch.core import config_store
from ocean_watch.templates import plan_templates

from tests.support import business_template_config, valid_config


class ChannelAuthorizationTests(unittest.TestCase):
    def test_legacy_config_migrates_to_marketing(self):
        migrated = channels.migrate_config({
            "api": {"base_url": "https://api.example.test/open_api"},
            "oauth": {"redirect_uri": "http://127.0.0.1/callback"},
            "account": {"advertiser_id": "9007199254740993"},
            "plan_templates": {
                "template": {"bindings": {"advertiser_id": "9007199254740993"}},
            },
        })
        self.assertEqual(migrated["default_channel"], "marketing")
        self.assertEqual(migrated["account"]["channel"], "marketing")
        self.assertEqual(migrated["plan_templates"]["template"]["bindings"]["channel"], "marketing")
        self.assertEqual(
            migrated["channels"]["marketing"]["api"]["base_url"],
            "https://api.example.test/open_api",
        )
        self.assertNotIn("api", migrated)
        self.assertEqual(channels.migrate_config(migrated), migrated)

    def test_channel_config_drops_all_legacy_credential_metadata(self):
        config = valid_config()
        config["api"].update({
            "access_token_expires_at": "2099-01-01T00:00:00+00:00",
            "oauth_authorized_accounts": [{"account_id": "1"}],
            "authorized_advertiser_ids": ["1"],
        })
        migrated = channels.migrate_config(config)
        marketing_api = migrated["channels"]["marketing"]["api"]
        self.assertEqual(marketing_api, {"base_url": "https://api.oceanengine.com/open_api"})

    def test_schema_v1_template_migrates_before_channel_binding(self):
        config = valid_config()
        config["materials"] = {}
        config["plan_templates"] = {
            "legacy": {
                "platform": "示例平台",
                "traffic_source": "CID",
                "product_id": "product-1",
                "defaults": {"product_name": "test product"},
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                    mock.patch.object(credential_store, "read_credentials", return_value={}), \
                    mock.patch.object(credential_store, "write_credentials"), \
                    mock.patch.object(credential_store, "read_entry", return_value={}):
                migrate_channels.migrate(config_path)
            migrated = json.loads(config_path.read_text(encoding="utf-8"))
        bindings = migrated["plan_templates"]["legacy"]["bindings"]
        self.assertEqual(bindings["channel"], "marketing")
        self.assertEqual(bindings["platform"], "示例平台")
        self.assertEqual(bindings["product_id"], "product-1")

    def test_qianchuan_never_uses_marketing_runtime(self):
        with self.assertRaises(channels.ChannelError) as raised:
            channels.runtime_config(valid_config(), channel="qianchuan", capability="query")
        self.assertEqual(raised.exception.code, "channel_capability_not_implemented")

    def test_template_channel_mismatch_is_rejected(self):
        config = business_template_config()
        config["account"]["channel"] = "marketing"
        name = config["active_plan_template"]
        config["plan_templates"][name]["bindings"]["channel"] = "qianchuan"
        with self.assertRaisesRegex(ValueError, "bound to channel qianchuan"):
            plan_templates.apply(config)

    def test_authorizations_resolve_by_advertiser_without_overwrite(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            first = authorization_store.save_authorization(
                "marketing",
                {"access_token": "one", "refresh_token": "one-r"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            second = authorization_store.save_authorization(
                "marketing",
                {"access_token": "two", "refresh_token": "two-r"},
                [{"account_id": "102", "advertiser_ids": ["202"]}],
            )
            resolved_first, _, token_first = authorization_store.resolve("marketing", "201")
            resolved_second, _, token_second = authorization_store.resolve("marketing", "202")
        self.assertEqual((resolved_first, token_first["access_token"]), (first, "one"))
        self.assertEqual((resolved_second, token_second["access_token"]), (second, "two"))

    def test_explicit_account_must_cover_advertiser(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_store.save_authorization(
                "marketing",
                {"access_token": "one"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            with self.assertRaises(authorization_store.AuthorizationError) as raised:
                authorization_store.resolve("marketing", "999", auth_account_id="101")
        self.assertEqual(raised.exception.code, "authorized_account_not_found")

    def test_duplicate_account_requires_explicit_rebind(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_store.save_authorization(
                "marketing",
                {"access_token": "one"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            with self.assertRaises(authorization_store.AuthorizationError) as raised:
                authorization_store.save_authorization(
                    "marketing",
                    {"access_token": "two"},
                    [{"account_id": "101", "advertiser_ids": ["202"]}],
                )
        self.assertEqual(raised.exception.code, "authorized_account_conflict")

    def test_official_ids_require_lossless_decimal_form(self):
        self.assertEqual(
            authorization_store.normalize_id("9007199254740993"),
            "9007199254740993",
        )
        for value in ("01", " 1", "+1", "-1", "1.0"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    authorization_store.normalize_id(value)

    def test_legacy_marketing_credentials_migrate_once(self):
        entries = {}
        legacy = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
            "refresh_token": "refresh",
            "oauth_authorized_accounts": [
                {"account_id": "101", "account_role": "ADVERTISER"},
            ],
            "authorized_advertiser_ids": ["101"],
        }
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "read_credentials", return_value=legacy), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            first = authorization_store.migrate_legacy_marketing()
            second = authorization_store.migrate_legacy_marketing()
            state = authorization_store.load_state()
        self.assertTrue(first["migrated"])
        self.assertFalse(second["migrated"])
        self.assertEqual(len(state["channels"]["marketing"]["authorizations"]), 1)

    def test_runtime_resolves_different_tokens_for_different_advertisers(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_store.write_app("marketing", "app", "secret")
            authorization_store.save_authorization(
                "marketing",
                {"access_token": "one"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            authorization_store.save_authorization(
                "marketing",
                {"access_token": "two"},
                [{"account_id": "102", "advertiser_ids": ["202"]}],
            )
            base = channels.runtime_config(valid_config(), "marketing")
            first = authorization_store.attach_runtime(base, "marketing", advertiser_id="201")
            second = authorization_store.attach_runtime(base, "marketing", advertiser_id="202")
        self.assertEqual(first["api"]["access_token"], "one")
        self.assertEqual(second["api"]["access_token"], "two")

    def test_channel_migration_updates_temp_config_idempotently(self):
        entries = {}
        legacy = {"app_id": "app", "secret": "secret"}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(valid_config()), encoding="utf-8")
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                    mock.patch.object(credential_store, "read_credentials", return_value=legacy), \
                    mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                    mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
                first = migrate_channels.migrate(config_path)
                downgraded = json.loads(config_path.read_text(encoding="utf-8"))
                downgraded["plan_template_schema_version"] = 2
                config_path.write_text(json.dumps(downgraded), encoding="utf-8")
                second = migrate_channels.migrate(config_path)
            migrated = json.loads(config_path.read_text(encoding="utf-8"))
        self.assertEqual(first["activation"], "schema_v2_active")
        self.assertEqual(second["activation"], "schema_v2_active")
        self.assertEqual(migrated["account"]["channel"], "marketing")
        self.assertEqual(
            migrated["plan_template_schema_version"],
            plan_templates.SCHEMA_VERSION,
        )
        self.assertNotIn("api", migrated)

    def test_channel_manifest_commit_keeps_previous_generation(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_id = authorization_store.save_authorization(
                "marketing",
                {"access_token": "one", "refresh_token": "refresh-one"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            first_manifest = authorization_store.channel_manifest_path("marketing", 1)
            authorization_store.update_authorization_tokens(
                "marketing",
                authorization_id,
                {"access_token": "two", "refresh_token": "refresh-two"},
            )
            current = json.loads(
                authorization_store.channel_current_path("marketing").read_text(encoding="utf-8")
            )
            state = authorization_store.load_channel_state("marketing")
            first_manifest_exists = first_manifest.exists()
        self.assertTrue(first_manifest_exists)
        self.assertEqual(current["generation"], 2)
        self.assertEqual(state["generation"], 2)
        self.assertEqual(state["authorizations"][authorization_id]["token_revision"], 2)

    def test_manifest_pointer_failure_retries_with_new_generation(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            real_atomic_write = config_store.atomic_write_json
            failed_once = False

            def fail_current_once(path, data, backup=True):
                nonlocal failed_once
                if Path(path).name == "current.json" and not failed_once:
                    failed_once = True
                    raise OSError("injected current pointer failure")
                return real_atomic_write(path, data, backup=backup)

            with mock.patch.object(config_store, "atomic_write_json", side_effect=fail_current_once):
                with self.assertRaisesRegex(OSError, "pointer"):
                    authorization_store.save_authorization(
                        "marketing",
                        {"access_token": "one"},
                        [{"account_id": "101", "advertiser_ids": ["201"]}],
                        authorization_id="stable",
                    )
                authorization_store.save_authorization(
                    "marketing",
                    {"access_token": "one"},
                    [{"account_id": "101", "advertiser_ids": ["201"]}],
                    authorization_id="stable",
                )
            state = authorization_store.load_channel_state("marketing")
        self.assertEqual(state["generation"], 1)
        self.assertEqual(list(state["authorizations"]), ["stable"])

    def test_qianchuan_app_configuration_uses_qianchuan_store(self):
        with mock.patch.object(authorization_store, "read_app", return_value={}), \
                mock.patch.object(authorization_store, "write_app", return_value="test") as write_app:
            result = credential_store.configure_app("app", "secret", channel="qianchuan")
        self.assertEqual(result["channel"], "qianchuan")
        write_app.assert_called_once_with("qianchuan", "app", "secret")

    def test_business_runtime_never_falls_back_to_legacy_token(self):
        legacy = {"access_token": "legacy-token", "refresh_token": "legacy-refresh"}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "read_credentials", return_value=legacy), \
                mock.patch.object(credential_store, "read_entry", return_value={}):
            runtime = authorization_store.attach_runtime(
                channels.runtime_config(valid_config(), "marketing"),
                "marketing",
                advertiser_id="1234567890",
            )
        self.assertNotIn("access_token", runtime["api"])
        self.assertNotIn("refresh_token", runtime["api"])

    def test_pending_legacy_authorization_can_only_be_selected_for_sync(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_id = authorization_store.save_authorization(
                "marketing",
                {
                    "access_token": "legacy-token",
                    "refresh_token": "legacy-refresh",
                    "pending_account_sync": True,
                },
                [],
            )
            with self.assertRaises(authorization_store.AuthorizationError) as raised:
                authorization_store.resolve(
                    "marketing",
                    authorization_id=authorization_id,
                )
            resolved, _, token = authorization_store.resolve(
                "marketing",
                authorization_id=authorization_id,
                allow_pending=True,
            )
        self.assertEqual(raised.exception.code, "legacy_authorization_pending_sync")
        self.assertEqual(resolved, authorization_id)
        self.assertEqual(token["access_token"], "legacy-token")

    def test_migration_resumes_after_config_commit_failure(self):
        entries = {}
        legacy = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
            "refresh_token": "refresh",
            "oauth_authorized_accounts": [
                {"account_id": "101", "account_role": "ADVERTISER"},
            ],
            "authorized_advertiser_ids": ["101"],
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(valid_config()), encoding="utf-8")
            real_atomic_write = config_store.atomic_write_json
            failed_once = False

            def fail_first_config_commit(path, data, backup=True):
                nonlocal failed_once
                if Path(path) == config_path and not failed_once:
                    failed_once = True
                    raise OSError("injected config commit failure")
                return real_atomic_write(path, data, backup=backup)

            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                    mock.patch.object(credential_store, "read_credentials", return_value=legacy), \
                    mock.patch.object(credential_store, "write_credentials", return_value="test"), \
                    mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                    mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))), \
                    mock.patch.object(config_store, "atomic_write_json", side_effect=fail_first_config_commit):
                with self.assertRaisesRegex(OSError, "injected"):
                    migrate_channels.migrate(config_path)
                interrupted = json.loads(migrate_channels.journal_path().read_text(encoding="utf-8"))
                result = migrate_channels.migrate(config_path)
                completed = json.loads(migrate_channels.journal_path().read_text(encoding="utf-8"))
                state = authorization_store.load_channel_state("marketing")
        self.assertEqual(interrupted["credentials"], "committed")
        self.assertEqual(interrupted["config"], "pending")
        self.assertEqual(completed["migration_id"], interrupted["migration_id"])
        self.assertEqual(completed["authorization_id"], interrupted["authorization_id"])
        self.assertEqual(result["activation"], "schema_v2_active")
        self.assertEqual(list(state["authorizations"]), [interrupted["authorization_id"]])

    def test_channel_sensitive_fields_are_reported_with_full_paths(self):
        config = channels.migrate_config(valid_config())
        config["channels"]["marketing"]["api"]["access_token"] = "leaked"
        self.assertEqual(
            credential_store.sensitive_config_fields(config),
            ["channels.marketing.api.access_token"],
        )
