import copy
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import (
    authorization_store,
    channels,
    credential_store,
    token_manager,
)

from tests.support import valid_config


class TokenRefreshTests(unittest.TestCase):
    def expiring_config(self):
        config = valid_config()
        config["api"].update({
            "app_id": "123",
            "secret": "secret",
            "refresh_token": "refresh-token",
            "access_token_expires_at": "2000-01-01T00:00:00+00:00",
            "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
        })
        return config

    def test_expired_access_token_refreshes_before_api_use(self):
        config = self.expiring_config()
        refreshed = copy.deepcopy(config)
        refreshed["api"]["access_token"] = "new-access-token"
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text("{}", encoding="utf-8")
            with mock.patch.object(token_manager, "load_config", return_value=config), \
                    mock.patch.object(token_manager, "refresh_access_token", return_value=(refreshed, {})) as refresh:
                result = token_manager.ensure_access_token(config_path, config)
        self.assertEqual(result["api"]["access_token"], "new-access-token")
        refresh.assert_called_once()

    def test_valid_access_token_does_not_refresh(self):
        config = self.expiring_config()
        config["api"]["access_token_expires_at"] = "2999-01-01T00:00:00+00:00"
        with mock.patch.object(token_manager, "refresh_access_token") as refresh:
            result = token_manager.ensure_access_token("unused.json", config)
        self.assertEqual(result["api"]["access_token"], "test-access-token")
        refresh.assert_not_called()

    def test_attaching_channel_credentials_preserves_runtime_template_state(self):
        runtime = self.expiring_config()
        runtime["api"].pop("access_token")
        runtime["defaults"]["runtime_template_marker"] = "preserved"
        credentials = copy.deepcopy(runtime)
        credentials["defaults"].pop("runtime_template_marker")
        credentials["api"]["access_token"] = "stored-access-token"
        credentials["api"]["access_token_expires_at"] = "2999-01-01T00:00:00+00:00"
        credentials["_authorization"] = {
            "channel": "marketing",
            "authorization_id": "authorization-1",
        }
        with mock.patch.object(token_manager, "load_config", return_value=credentials), \
                mock.patch.object(token_manager, "refresh_access_token") as refresh:
            result = token_manager.ensure_access_token(
                "unused.json",
                runtime,
                channel="marketing",
                advertiser_id="1234567890",
            )
        self.assertEqual(result["defaults"]["runtime_template_marker"], "preserved")
        self.assertEqual(result["api"]["access_token"], "stored-access-token")
        self.assertEqual(result["_authorization"]["authorization_id"], "authorization-1")
        refresh.assert_not_called()

    def test_unresolved_target_cannot_reuse_a_previous_channel_token(self):
        stale = channels.runtime_config(valid_config(), "marketing")
        stale["api"].update({
            "app_id": "123",
            "secret": "marketing-secret",
            "access_token": "marketing-token",
            "refresh_token": "marketing-refresh",
            "access_token_expires_at": "2999-01-01T00:00:00+00:00",
            "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
        })
        stale["_authorization"] = {
            "channel": "marketing",
            "authorization_id": "marketing-authorization",
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(
                json.dumps(channels.migrate_config(valid_config())),
                encoding="utf-8",
            )
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                    mock.patch.object(credential_store, "read_entry", return_value={}):
                with self.assertRaisesRegex(RuntimeError, "missing OAuth refresh fields"):
                    token_manager.ensure_access_token(
                        config_path,
                        stale,
                        channel="qianchuan",
                        advertiser_id="201",
                    )

    def test_expired_refresh_token_requires_authorization(self):
        config = self.expiring_config()
        config["api"]["refresh_token_expires_at"] = "2000-01-01T00:00:00+00:00"
        with mock.patch.object(token_manager, "post_json") as post:
            with self.assertRaisesRegex(RuntimeError, "authorize again"):
                token_manager.refresh_access_token("unused.json", config)
        post.assert_not_called()

    def test_refresh_saves_rotated_tokens(self):
        config = self.expiring_config()
        response = {
            "code": 0,
            "data": {
                "access_token": "new-access-token",
                "refresh_token": "new-refresh-token",
                "expires_in": 3600,
                "refresh_token_expires_in": 7200,
            },
        }
        with mock.patch.object(token_manager, "post_json", return_value=response), \
                mock.patch.object(token_manager, "save_credentials") as save:
            updated, _ = token_manager.refresh_access_token("unused.json", config)
        saved = save.call_args.args[0]
        self.assertEqual(saved["api"]["access_token"], "new-access-token")
        self.assertEqual(saved["api"]["refresh_token"], "new-refresh-token")
        save.assert_called_once_with(updated)

    def test_channel_refresh_returns_persisted_revision(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_store.write_app("marketing", "123", "secret")
            authorization_id = authorization_store.save_authorization(
                "marketing",
                {
                    "access_token": "old-access",
                    "refresh_token": "old-refresh",
                    "access_token_expires_at": "2000-01-01T00:00:00+00:00",
                    "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
                },
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            config = channels.runtime_config(valid_config(), "marketing")
            config["account"]["advertiser_id"] = "201"
            config = authorization_store.attach_runtime(config, "marketing", advertiser_id="201")
            response = {
                "code": 0,
                "data": {
                    "access_token": "new-access",
                    "refresh_token": "new-refresh",
                    "expires_in": 3600,
                    "refresh_token_expires_in": 7200,
                },
            }
            with mock.patch.object(token_manager, "post_json", return_value=response):
                updated, _ = token_manager.refresh_access_token("unused.json", config)
            state = authorization_store.load_channel_state("marketing")
            revision = state["authorizations"][authorization_id]["token_revision"]
        self.assertEqual(updated["api"]["access_token"], "new-access")
        self.assertEqual(updated["api"]["refresh_token"], "new-refresh")
        self.assertEqual(revision, 2)

    def test_refresh_updates_only_the_selected_authorization(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(
                json.dumps(channels.migrate_config(valid_config())),
                encoding="utf-8",
            )
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                    mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                    mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
                authorization_store.write_app("marketing", "123", "secret")
                first = authorization_store.save_authorization(
                    "marketing",
                    {
                        "access_token": "access-one",
                        "refresh_token": "refresh-one",
                        "access_token_expires_at": "2000-01-01T00:00:00+00:00",
                        "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
                    },
                    [{"account_id": "101", "advertiser_ids": ["201"]}],
                )
                second = authorization_store.save_authorization(
                    "marketing",
                    {
                        "access_token": "access-two",
                        "refresh_token": "refresh-two",
                        "access_token_expires_at": "2000-01-01T00:00:00+00:00",
                        "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
                    },
                    [{"account_id": "102", "advertiser_ids": ["202"]}],
                )
                response = {
                    "code": 0,
                    "data": {
                        "access_token": "new-access-one",
                        "refresh_token": "new-refresh-one",
                        "expires_in": 3600,
                        "refresh_token_expires_in": 7200,
                    },
                }
                with mock.patch.object(token_manager, "post_json", return_value=response) as post:
                    updated = token_manager.ensure_access_token(
                        config_path,
                        channel="marketing",
                        advertiser_id="201",
                    )
                _, _, first_token = authorization_store.resolve(
                    "marketing",
                    advertiser_id="201",
                )
                _, _, second_token = authorization_store.resolve(
                    "marketing",
                    advertiser_id="202",
                )
                state = authorization_store.load_channel_state("marketing")
        self.assertEqual(updated["account"]["advertiser_id"], "201")
        self.assertEqual(first_token["access_token"], "new-access-one")
        self.assertEqual(second_token["access_token"], "access-two")
        self.assertEqual(state["authorizations"][first]["token_revision"], 2)
        self.assertEqual(state["authorizations"][second]["token_revision"], 1)
        self.assertEqual(post.call_args.args[2]["refresh_token"], "refresh-one")

    def test_token_refresh_does_not_resync_accounts(self):
        config = self.expiring_config()
        response = {
            "code": 0,
            "data": {"access_token": "new-access-token", "expires_in": 3600},
        }
        with mock.patch.object(token_manager, "post_json", return_value=response), \
                mock.patch.object(token_manager, "update_accounts_after_token") as sync, \
                mock.patch.object(token_manager, "save_credentials"):
            token_manager.refresh_access_token("unused.json", config)
        sync.assert_not_called()

    def test_refresh_response_requires_access_token(self):
        config = self.expiring_config()
        with mock.patch.object(token_manager, "post_json", return_value={"code": 0, "data": {}}):
            with self.assertRaisesRegex(RuntimeError, "did not include access_token"):
                token_manager.refresh_access_token("unused.json", config)

    def test_status_next_action(self):
        config = self.expiring_config()
        self.assertEqual(token_manager.token_next_action(config), "refresh")
        config["api"]["refresh_token_expires_at"] = "2000-01-01T00:00:00+00:00"
        self.assertEqual(token_manager.token_next_action(config), "reauthorize")

    def test_status_exposes_pending_authorization_without_using_it(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, {"CODEX_HOME": directory}), \
                mock.patch.object(credential_store, "write_entry", side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test"), \
                mock.patch.object(credential_store, "read_entry", side_effect=lambda account: copy.deepcopy(entries.get(account, {}))):
            authorization_id = authorization_store.save_authorization(
                "marketing",
                {
                    "access_token": "legacy-token",
                    "pending_account_sync": True,
                },
                [],
            )
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(channels.migrate_config(valid_config())), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(sys, "argv", [
                "ocean-watch auth status",
                "--config",
                str(config_path),
                "--channel",
                "marketing",
                "--status",
            ]), redirect_stdout(output):
                exit_code = token_manager.main()
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["resolution_error"]["code"], "legacy_authorization_pending_sync")
        self.assertEqual(
            result["authorization_status"]["authorizations"][0]["authorization_id"],
            authorization_id,
        )
        self.assertFalse(result["has_access_token"])

    def test_authorized_advertiser_ids_ignore_json_number_type(self):
        self.assertTrue(token_manager.advertiser_is_authorized(123456, ["123456"]))
        self.assertTrue(token_manager.advertiser_is_authorized("123456", [123456]))
        self.assertFalse(token_manager.advertiser_is_authorized(123456, [654321]))

    def test_authorized_account_sync_keeps_only_valid_advertisers(self):
        config = self.expiring_config()
        response = {
            "code": 0,
            "data": {
                "list": [
                    {"advertiser_id": 1, "advertiser_name": "one", "account_type": "ADVERTISER", "is_valid": True},
                    {"advertiser_id": 2, "advertiser_name": "two", "account_type": "ADVERTISER", "is_valid": False},
                    {"advertiser_id": 3, "advertiser_name": "center", "account_type": "CUSTOMER_ADMIN", "is_valid": True},
                ]
            },
        }
        snapshot = [{
            "account_id": 1,
            "advertiser_name": "one",
            "account_type": "ADVERTISER",
            "is_valid": True,
            "advertiser_ids": ["1"],
        }]
        with mock.patch.object(token_manager, "get_oauth_json", return_value=response), \
                mock.patch.object(
                    token_manager,
                    "build_authorized_account_snapshot",
                    return_value=(snapshot, ["1"], []),
                ):
            updated, summary = token_manager.update_authorized_accounts(config)
        self.assertEqual(updated["api"]["authorized_advertiser_ids"], [1])
        self.assertEqual(len(updated["api"]["oauth_authorized_accounts"]), 1)
        self.assertNotIn("company_list", updated["api"]["oauth_authorized_accounts"][0])
        self.assertEqual(summary["oauth_authorized_account_count"], 1)
        self.assertEqual(summary["authorized_advertiser_count"], 1)
        self.assertEqual(summary["account_types"], {"ADVERTISER": 2, "CUSTOMER_ADMIN": 1})

    def test_customer_center_role_expands_with_account_source(self):
        config = self.expiring_config()
        account = {"account_role": "CUSTOMER_ADMIN", "account_id": 101}
        response = {
            "code": 0,
            "data": {
                "list": [{"advertiser_id": 201}, {"advertiser_id": 202}],
                "page_info": {"total_page": 1},
            },
        }
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            identifiers, result = token_manager.fetch_role_advertiser_ids(config, account)
        self.assertEqual(identifiers, [201, 202])
        self.assertEqual(result["status"], "ok")
        params = request.call_args.args[2]
        self.assertEqual(params["cc_account_id"], 101)
        self.assertEqual(params["account_source"], "AD")

    def test_ebp_role_expands_advertiser_accounts(self):
        config = self.expiring_config()
        account = {"account_role": "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "account_id": 301}
        response = {
            "code": 0,
            "data": {
                "account_list": [{"account_id": 401}, {"account_id": 402}],
                "page_info": {"total_page": 1},
            },
        }
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            identifiers, result = token_manager.fetch_role_advertiser_ids(config, account)
        self.assertEqual(identifiers, [401, 402])
        self.assertEqual(result["status"], "ok")
        params = request.call_args.args[2]
        self.assertEqual(params["enterprise_organization_id"], 301)
        self.assertEqual(params["account_source"], "AD")

    def test_role_expansion_reads_every_declared_page_after_empty_page(self):
        config = self.expiring_config()
        account = {"account_role": "CUSTOMER_ADMIN", "account_id": 101}
        responses = [
            {
                "code": 0,
                "data": {"list": [], "page_info": {"total_page": 2}},
            },
            {
                "code": 0,
                "data": {
                    "list": [{"advertiser_id": 201}],
                    "page_info": {"total_page": 2},
                },
            },
        ]
        with mock.patch.object(token_manager, "get_api_json", side_effect=responses) as request:
            identifiers, result = token_manager.fetch_role_advertiser_ids(config, account)
        self.assertEqual(identifiers, [201])
        self.assertEqual(result["pages"], 2)
        self.assertEqual(request.call_count, 2)

    def test_role_expansion_rejects_malformed_pagination_metadata(self):
        config = self.expiring_config()
        account = {"account_role": "CUSTOMER_ADMIN", "account_id": 101}
        responses = (
            {"code": 0, "data": {"list": [{"advertiser_id": 201}]}},
            {
                "code": 0,
                "data": {
                    "list": [{"advertiser_id": 201}],
                    "page_info": {"total_page": "invalid"},
                },
            },
        )
        for response in responses:
            with self.subTest(response=response), mock.patch.object(
                token_manager,
                "get_api_json",
                return_value=response,
            ):
                with self.assertRaisesRegex(RuntimeError, "Malformed pagination metadata"):
                    token_manager.fetch_role_advertiser_ids(config, account)

    def test_role_expansion_rejects_total_pages_above_safety_cap(self):
        config = self.expiring_config()
        account = {"account_role": "CUSTOMER_ADMIN", "account_id": 101}
        response = {
            "code": 0,
            "data": {
                "list": [{"advertiser_id": 201}],
                "page_info": {
                    "total_page": token_manager.MAX_ROLE_EXPANSION_PAGES + 1,
                },
            },
        }
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            with self.assertRaisesRegex(RuntimeError, "pagination safety cap"):
                token_manager.fetch_role_advertiser_ids(config, account)
        request.assert_called_once()

    def test_advertiser_verification_batches_fifty_ids(self):
        config = self.expiring_config()

        def response_for_chunk(_config, _path, params):
            identifiers = json.loads(params["advertiser_ids"])
            return {"code": 0, "data": [{"id": identifier} for identifier in identifiers]}

        with mock.patch.object(token_manager, "get_api_json", side_effect=response_for_chunk) as request:
            verified, errors = token_manager.verify_advertiser_ids(config, list(range(1, 122)))
        self.assertEqual(verified, list(range(1, 122)))
        self.assertEqual(errors, [])
        self.assertEqual(request.call_count, 3)

    def test_total_verification_failure_preserves_previous_cache(self):
        config = self.expiring_config()
        accounts = [{"account_role": "ADVERTISER", "advertiser_id": 1}]
        with mock.patch.object(token_manager, "get_api_json", return_value={"code": 40002}):
            with self.assertRaisesRegex(RuntimeError, "Unable to verify"):
                token_manager.expand_authorized_advertisers(config, accounts)

    def test_token_update_does_not_trust_response_advertiser_ids(self):
        config = self.expiring_config()
        updated = token_manager.update_token_fields(config, {
            "access_token": "new-access",
            "refresh_token": "new-refresh",
            "advertiser_ids": [999],
        })
        self.assertNotEqual(updated["api"].get("authorized_advertiser_ids"), [999])

    def test_account_sync_failure_preserves_new_token(self):
        config = self.expiring_config()
        config["api"]["access_token"] = "new-access"
        with mock.patch.object(token_manager, "update_authorized_accounts", side_effect=RuntimeError("sync failed")):
            updated, summary = token_manager.update_accounts_after_token(config)
        self.assertEqual(updated["api"]["access_token"], "new-access")
        self.assertTrue(summary["sync_failed"])
