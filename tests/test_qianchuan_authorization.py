import copy
import http.client
import json
import os
import socket
import tempfile
import threading
import unittest
import urllib.parse
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import (
    authorization_store,
    channel_adapters,
    channels,
    credential_store,
    oauth_local_authorize,
    token_manager,
)

from tests.support import valid_config


def qianchuan_runtime():
    config = channels.migrate_config(valid_config())
    config["channels"]["qianchuan"] = {
        "api": {"base_url": "https://api.oceanengine.com/open_api"},
        "oauth": {
            "redirect_uri": "http://127.0.0.1:8787/oauth/callback",
            "token_base_url": "https://ad.oceanengine.com/open_api",
        },
    }
    runtime = channels.runtime_config(config, "qianchuan", capability="oauth")
    runtime["api"].update({
        "app_id": "123",
        "secret": "secret",
        "access_token": "access",
        "refresh_token": "refresh",
    })
    return runtime


class QianchuanAuthorizationTests(unittest.TestCase):
    def request_local_server(self, server, method, path, body=None, headers=None):
        worker = threading.Thread(target=server.handle_request)
        worker.start()
        connection = http.client.HTTPConnection(*server.server_address, timeout=2)
        connection.request(method, path, body=body, headers=headers or {})
        response = connection.getresponse()
        result = response.status, dict(response.getheaders()), response.read().decode("utf-8")
        connection.close()
        worker.join(timeout=2)
        self.assertFalse(worker.is_alive())
        return result

    def test_app_setup_page_collects_both_credentials_once(self):
        config = qianchuan_runtime()
        config["api"].pop("app_id")
        config["api"].pop("secret")
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            config,
            requires_app_configuration=True,
        )
        try:
            path = urllib.parse.urlparse(oauth_local_authorize.app_setup_url(server))
            status, headers, body = self.request_local_server(
                server,
                "GET",
                path.path + "?" + path.query,
            )
        finally:
            server.server_close()
        self.assertEqual(status, 200)
        self.assertIn('name="app_id"', body)
        self.assertIn('name="secret" type="password"', body)
        self.assertIn("巨量千川应用配置", body)
        self.assertEqual(headers["Cache-Control"], "no-store")
        self.assertIn("form-action 'self'", headers["Content-Security-Policy"])
        self.assertIn(
            "https://qianchuan.jinritemai.com",
            headers["Content-Security-Policy"],
        )

    def test_marketing_app_setup_allows_the_official_redirect_chain(self):
        config = channels.runtime_config(
            channels.migrate_config(valid_config()),
            "marketing",
            capability="oauth",
        )
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "AD.nonce",
            "marketing",
            config,
            requires_app_configuration=True,
        )
        try:
            path = urllib.parse.urlparse(oauth_local_authorize.app_setup_url(server))
            status, headers, _ = self.request_local_server(
                server,
                "GET",
                path.path + "?" + path.query,
            )
        finally:
            server.server_close()

        self.assertEqual(status, 200)
        content_security_policy = headers["Content-Security-Policy"]
        self.assertIn("https://ad.oceanengine.com", content_security_policy)
        self.assertIn("https://open.oceanengine.com", content_security_policy)

    def test_root_is_a_diagnostic_page_and_keeps_session_alive(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
        )
        try:
            status, _, body = self.request_local_server(server, "GET", "/")
            self.assertEqual(status, 200)
            self.assertIn("本地授权服务已启动", body)
            self.assertIn("不是配置或授权入口", body)
            self.assertIsNone(server.result)
        finally:
            server.server_close()

    def test_empty_callback_is_diagnostic_and_keeps_session_alive(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
        )
        try:
            status, _, body = self.request_local_server(
                server,
                "GET",
                "/oauth/callback",
            )
            self.assertEqual(status, 200)
            self.assertIn("正在等待官方授权回调", body)
            self.assertIn("回调地址不是授权入口", body)
            self.assertIsNone(server.result)
        finally:
            server.server_close()

    def test_callback_accepts_trailing_slash_from_official_redirect(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
        )
        query = urllib.parse.urlencode({"state": "QC.nonce", "auth_code": "code-1"})
        try:
            status, _, body = self.request_local_server(
                server,
                "GET",
                f"/oauth/callback/?{query}",
            )
            self.assertEqual(status, 200)
            self.assertIn("授权成功", body)
            self.assertEqual(server.result["auth_code"], "code-1")
        finally:
            server.server_close()

    def test_unknown_callback_path_returns_clear_diagnostic(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
        )
        try:
            status, _, body = self.request_local_server(server, "GET", "/callback")
            self.assertEqual(status, 404)
            self.assertIn("授权地址无效", body)
            self.assertNotIn("Not found", body)
            self.assertIsNone(server.result)
        finally:
            server.server_close()

    def test_unknown_post_path_returns_clear_diagnostic(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
        )
        try:
            status, _, body = self.request_local_server(
                server,
                "POST",
                "/oauth/callback",
                body="x=1",
                headers={
                    "Content-Type": "application/x-www-form-urlencoded",
                    "Content-Length": "3",
                },
            )
            self.assertEqual(status, 404)
            self.assertIn("授权地址无效", body)
            self.assertNotIn("Not found", body)
            self.assertIsNone(server.result)
        finally:
            server.server_close()

    def test_no_open_output_distinguishes_start_url_from_callback_uri(self):
        config = qianchuan_runtime()
        config["api"].pop("app_id")
        config["api"].pop("secret")
        output = StringIO()
        with mock.patch.object(
            token_manager,
            "load_config",
            return_value=config,
        ), mock.patch.object(
            authorization_store,
            "read_app",
            return_value={},
        ), mock.patch.object(
            oauth_local_authorize.webbrowser,
            "open",
        ) as open_browser, redirect_stdout(output), self.assertRaises(TimeoutError):
            oauth_local_authorize.main([
                "--channel",
                "qianchuan",
                "--redirect-uri",
                "http://127.0.0.1:0/oauth/callback",
                "--print-url",
                "--no-open",
                "--timeout",
                "0",
            ])

        result = json.loads(output.getvalue())
        start_url = urllib.parse.urlparse(result["start_url"])
        self.assertEqual(start_url.path, oauth_local_authorize.APP_SETUP_PATH)
        self.assertNotEqual(result["start_url"], result["redirect_uri"])
        self.assertEqual(result["start_url_usage"], "open_this_url_to_begin")
        self.assertEqual(
            result["redirect_uri_usage"],
            "official_registration_and_callback_only_do_not_open_directly",
        )
        open_browser.assert_not_called()

    def test_app_setup_rejects_invalid_session_without_storing_credentials(self):
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            qianchuan_runtime(),
            requires_app_configuration=True,
        )
        body = urllib.parse.urlencode({
            "setup_token": "wrong-token",
            "app_id": "app",
            "secret": "secret",
        })
        try:
            with mock.patch.object(credential_store, "configure_app") as configure:
                status, _, _ = self.request_local_server(
                    server,
                    "POST",
                    oauth_local_authorize.APP_SETUP_PATH,
                    body=body,
                    headers={
                        "Content-Type": "application/x-www-form-urlencoded",
                        "Content-Length": str(len(body.encode("utf-8"))),
                    },
                )
        finally:
            server.server_close()
        self.assertEqual(status, 403)
        configure.assert_not_called()

    def test_app_setup_stores_credentials_and_redirects_to_official_oauth(self):
        config = qianchuan_runtime()
        config["api"].pop("app_id")
        config["api"].pop("secret")
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            config,
            requires_app_configuration=True,
        )
        body = urllib.parse.urlencode({
            "setup_token": server.setup_token,
            "app_id": "new-app",
            "secret": "new-secret",
        })
        try:
            with mock.patch.object(
                credential_store,
                "configure_app",
                return_value={"backend": "test"},
            ) as configure:
                status, headers, _ = self.request_local_server(
                    server,
                    "POST",
                    oauth_local_authorize.APP_SETUP_PATH,
                    body=body,
                    headers={
                        "Content-Type": "application/x-www-form-urlencoded",
                        "Content-Length": str(len(body.encode("utf-8"))),
                    },
                )
        finally:
            server.server_close()
        self.assertEqual(status, 303)
        self.assertEqual(headers["Content-Length"], "0")
        self.assertEqual(headers["Connection"], "close")
        configure.assert_called_once_with("new-app", "new-secret", channel="qianchuan")
        redirect = urllib.parse.urlparse(headers["Location"])
        params = urllib.parse.parse_qs(redirect.query)
        self.assertEqual(redirect.netloc, "qianchuan.jinritemai.com")
        self.assertEqual(params["app_id"], ["new-app"])
        self.assertEqual(params["state"], ["QC.nonce"])
        self.assertNotIn("new-secret", headers["Location"])

    def test_repeated_app_setup_submission_redirects_without_storing_twice(self):
        config = qianchuan_runtime()
        config["api"].pop("app_id")
        config["api"].pop("secret")
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            config,
            requires_app_configuration=True,
        )
        body = urllib.parse.urlencode({
            "setup_token": server.setup_token,
            "app_id": "new-app",
            "secret": "new-secret",
        })
        headers = {
            "Content-Type": "application/x-www-form-urlencoded",
            "Content-Length": str(len(body.encode("utf-8"))),
        }
        try:
            with mock.patch.object(
                credential_store,
                "configure_app",
                return_value={"backend": "test"},
            ) as configure:
                first_status, first_headers, _ = self.request_local_server(
                    server,
                    "POST",
                    oauth_local_authorize.APP_SETUP_PATH,
                    body=body,
                    headers=headers,
                )
                second_status, second_headers, second_body = self.request_local_server(
                    server,
                    "POST",
                    oauth_local_authorize.APP_SETUP_PATH,
                    body=body,
                    headers=headers,
                )
        finally:
            server.server_close()

        self.assertEqual(first_status, 303)
        self.assertEqual(second_status, 303)
        self.assertEqual(first_headers["Location"], second_headers["Location"])
        self.assertNotIn("授权地址无效", second_body)
        configure.assert_called_once_with("new-app", "new-secret", channel="qianchuan")

    def test_idle_browser_connection_does_not_block_app_setup(self):
        config = qianchuan_runtime()
        config["api"].pop("app_id")
        config["api"].pop("secret")
        server = oauth_local_authorize.create_local_server(
            "http://127.0.0.1:0/oauth/callback",
            "QC.nonce",
            "qianchuan",
            config,
            requires_app_configuration=True,
        )
        idle_connection = socket.create_connection(server.server_address, timeout=2)
        idle_worker = threading.Thread(target=server.handle_request)
        idle_worker.start()
        idle_worker.join(timeout=1)
        body = urllib.parse.urlencode({
            "setup_token": server.setup_token,
            "app_id": "new-app",
            "secret": "new-secret",
        })
        try:
            self.assertFalse(idle_worker.is_alive())
            with mock.patch.object(
                credential_store,
                "configure_app",
                return_value={"backend": "test"},
            ):
                status, _, _ = self.request_local_server(
                    server,
                    "POST",
                    oauth_local_authorize.APP_SETUP_PATH,
                    body=body,
                    headers={
                        "Content-Type": "application/x-www-form-urlencoded",
                        "Content-Length": str(len(body.encode("utf-8"))),
                    },
                )
        finally:
            idle_connection.close()
            idle_worker.join(timeout=2)
            server.server_close()
        self.assertEqual(status, 303)

    def test_app_setup_server_rejects_non_loopback_redirect(self):
        with self.assertRaisesRegex(ValueError, "loopback"):
            oauth_local_authorize.create_local_server(
                "http://example.com/oauth/callback",
                "QC.nonce",
                "qianchuan",
                qianchuan_runtime(),
                requires_app_configuration=True,
            )

    def test_channel_exposes_isolated_qianchuan_capabilities(self):
        channels.get("qianchuan", capability="oauth")
        channels.get("qianchuan", capability="accounts")
        channels.get("qianchuan", capability="qianchuan_create")
        channels.get("qianchuan", capability="qianchuan_materials")
        for capability in ("create", "query", "report"):
            with self.assertRaises(channels.ChannelError) as raised:
                channels.get("qianchuan", capability=capability)
            self.assertEqual(raised.exception.code, "channel_capability_not_implemented")

    def test_qianchuan_runtime_backfills_public_official_endpoints(self):
        config = channels.migrate_config(valid_config())
        config["channels"]["qianchuan"] = {"status": "not_implemented"}
        runtime = channels.runtime_config(
            config,
            "qianchuan",
            capability="qianchuan_materials",
        )
        self.assertEqual(
            runtime["api"]["base_url"],
            "https://api.oceanengine.com/open_api",
        )
        self.assertEqual(
            runtime["api"]["legacy_base_url"],
            "https://ad.oceanengine.com/open_api",
        )
        self.assertEqual(
            runtime["oauth"]["authorize_url"],
            "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html",
        )

    def test_status_accepts_unconfigured_advertiser_placeholder(self):
        config = channels.migrate_config(valid_config())
        config["account"]["advertiser_id"] = "REPLACE_WITH_ADVERTISER_ID"
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            with mock.patch.dict(
                os.environ,
                {"CODEX_HOME": directory},
            ), mock.patch.object(authorization_store, "read_app", return_value={}), mock.patch.object(
                credential_store,
                "read_entry",
                return_value={},
            ), redirect_stdout(StringIO()) as output:
                exit_code = token_manager.main([
                    "--config",
                    str(config_path),
                    "--channel",
                    "qianchuan",
                    "--status",
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertFalse(result["has_access_token"])
        self.assertFalse(result["advertiser_id_authorized"])

    def test_authorize_url_uses_qianchuan_contract(self):
        url = oauth_local_authorize.get_authorize_url(
            qianchuan_runtime(),
            "http://127.0.0.1:8787/oauth/callback",
            "QC.nonce",
        )
        parsed = urllib.parse.urlparse(url)
        params = urllib.parse.parse_qs(parsed.query)
        self.assertEqual(parsed.netloc, "qianchuan.jinritemai.com")
        self.assertEqual(params["material_auth"], ["1"])
        self.assertEqual(params["state"], ["QC.nonce"])

    def test_marketing_authorize_url_remains_unchanged(self):
        config = channels.runtime_config(valid_config(), "marketing", capability="oauth")
        config["api"]["app_id"] = "123"
        url = oauth_local_authorize.get_authorize_url(config, "http://localhost/callback", "AD.nonce")
        parsed = urllib.parse.urlparse(url)
        self.assertEqual(parsed.netloc, "ad.oceanengine.com")
        self.assertNotIn("material_auth", urllib.parse.parse_qs(parsed.query))

    def test_authorize_url_rejects_non_official_override(self):
        config = qianchuan_runtime()
        config["oauth"]["authorize_url"] = "https://example.com/oauth"

        with self.assertRaisesRegex(RuntimeError, "official HTTPS"):
            oauth_local_authorize.get_authorize_url(
                config,
                "http://127.0.0.1:8787/oauth/callback",
                "QC.nonce",
            )

    def test_authorize_url_rejects_preconfigured_query(self):
        config = qianchuan_runtime()
        config["oauth"]["authorize_url"] = (
            "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html?next=unexpected"
        )

        with self.assertRaisesRegex(RuntimeError, "query or fragment"):
            oauth_local_authorize.get_authorize_url(
                config,
                "http://127.0.0.1:8787/oauth/callback",
                "QC.nonce",
            )

    def test_shop_role_uses_qianchuan_permission_and_paginates(self):
        responses = [
            {
                "code": 0,
                "data": {
                    "list": [{"advertiser_id": 201}],
                    "page_info": {"total_page": 2},
                },
            },
            {
                "code": 0,
                "data": {
                    "list": [{"advertiser_id": 202}],
                    "page_info": {"total_page": 2},
                },
            },
        ]
        account = {"account_role": "PLATFORM_ROLE_SHOP_ACCOUNT", "account_id": 101}
        with mock.patch.object(token_manager, "get_api_json", side_effect=responses) as request:
            identifiers, result = token_manager.fetch_role_advertiser_ids(qianchuan_runtime(), account)
        self.assertEqual(identifiers, [201, 202])
        self.assertEqual(result["pages"], 2)
        self.assertEqual(request.call_args_list[0].args[1], channel_adapters.QIANCHUAN_SHOP_ADVERTISER_PATH)
        self.assertEqual(request.call_args_list[0].args[2]["permission"], ["QC_AWEME"])

    def test_empty_role_expansion_accepts_official_zero_page_contract(self):
        response = {
            "code": 0,
            "data": {
                "list": [],
                "page_info": {
                    "page": 1,
                    "page_size": 100,
                    "total_number": 0,
                    "total_page": 0,
                },
            },
        }
        account = {"account_role": "CUSTOMER_OPERATOR", "account_id": 101}
        with mock.patch.object(token_manager, "get_api_json", return_value=response):
            identifiers, result = token_manager.fetch_role_advertiser_ids(
                qianchuan_runtime(),
                account,
            )

        self.assertEqual(identifiers, [])
        self.assertEqual(result["status"], "ok")
        self.assertEqual(result["pages"], 1)

    def test_zero_page_role_expansion_rejects_inconsistent_data(self):
        account = {"account_role": "CUSTOMER_OPERATOR", "account_id": 101}
        invalid_data = [
            {
                "list": [{"advertiser_id": 201}],
                "page_info": {"total_number": 1, "total_page": 0},
            },
            {
                "list": [],
                "page_info": {"total_number": 1, "total_page": 0},
            },
            {
                "list": [],
                "page_info": {"total_page": 0},
            },
        ]
        for data in invalid_data:
            with self.subTest(data=data), mock.patch.object(
                token_manager,
                "get_api_json",
                return_value={"code": 0, "data": data},
            ), self.assertRaisesRegex(RuntimeError, "Malformed pagination metadata"):
                token_manager.fetch_role_advertiser_ids(qianchuan_runtime(), account)

    def test_qianchuan_agent_role_expands_managed_advertisers(self):
        response = {
            "code": 0,
            "data": {
                "list": [201, 202],
                "page_info": {"total_page": 1},
            },
        }
        account = {"account_role": "PLATFORM_ROLE_QIANCHUAN_AGENT", "account_id": 101}
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            identifiers, result = token_manager.fetch_role_advertiser_ids(
                qianchuan_runtime(),
                account,
            )
        self.assertEqual(identifiers, [201, 202])
        self.assertEqual(result["status"], "ok")
        self.assertEqual(request.call_args.args[1], channel_adapters.AGENT_ADVERTISER_PATH)
        self.assertEqual(request.call_args.args[2]["advertiser_id"], 101)
        self.assertEqual(
            request.call_args.kwargs["base_url"],
            channel_adapters.DEFAULT_TOKEN_BASE_URL,
        )

    def test_missing_optional_agent_permission_keeps_other_accounts(self):
        accounts = [
            {"account_role": "ADVERTISER", "advertiser_id": 201},
            {
                "account_role": "PLATFORM_ROLE_QIANCHUAN_AGENT",
                "account_id": 101,
            },
        ]
        results = [
            ([201], {"role": "ADVERTISER", "status": "ok", "count": 1}),
            ([], {
                "role": "PLATFORM_ROLE_QIANCHUAN_AGENT",
                "status": "api_error",
                "code": 40002,
                "count": 0,
                "permission_optional": True,
            }),
        ]
        with mock.patch.object(
            token_manager,
            "fetch_role_advertiser_ids",
            side_effect=results,
        ), mock.patch.object(
            token_manager,
            "verify_advertiser_ids",
            return_value=([201], []),
        ):
            snapshot, advertiser_ids, issues = (
                token_manager.build_authorized_account_snapshot(
                    qianchuan_runtime(),
                    accounts,
                )
            )
        self.assertEqual(advertiser_ids, ["201"])
        self.assertEqual(snapshot[1]["advertiser_ids"], [])
        self.assertEqual(issues[0]["reason"], "app_permission_missing")

    def test_authorized_subject_normalization_preserves_shop_identity(self):
        rows = token_manager.normalize_authorized_accounts([{
            "shop_id": "101",
            "account_string_id": "shop-account",
            "account_role": "PLATFORM_ROLE_SHOP_ACCOUNT",
        }])
        self.assertEqual(rows[0]["shop_id"], "101")
        self.assertEqual(rows[0]["account_string_id"], "shop-account")

    def test_shop_subject_can_be_saved_without_account_id(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {"CODEX_HOME": directory},
        ), mock.patch.object(
            credential_store,
            "write_entry",
            side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
        ), mock.patch.object(
            credential_store,
            "read_entry",
            side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
        ):
            authorization_id = authorization_store.save_authorization(
                "qianchuan",
                {"access_token": "token"},
                [{
                    "shop_id": "101",
                    "account_role": "PLATFORM_ROLE_SHOP_ACCOUNT",
                    "advertiser_ids": ["201"],
                }],
            )
            _, metadata, _ = authorization_store.resolve(
                "qianchuan",
                authorization_id=authorization_id,
            )
        self.assertEqual(metadata["authorized_accounts"][0]["account_id"], "101")
        self.assertEqual(metadata["authorized_accounts"][0]["shop_id"], "101")

    def test_customer_and_ebp_roles_use_qianchuan_source(self):
        response = {"code": 0, "data": {"list": [], "page_info": {"total_page": 1}}}
        customer = {"account_role": "CUSTOMER_ADMIN", "account_id": 101}
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            token_manager.fetch_role_advertiser_ids(qianchuan_runtime(), customer)
        self.assertEqual(request.call_args.args[2]["account_source"], "QIANCHUAN")

        response["data"] = {"account_list": [], "page_info": {"total_page": 1}}
        ebp = {"account_role": "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN", "account_id": 102}
        with mock.patch.object(token_manager, "get_api_json", return_value=response) as request:
            token_manager.fetch_role_advertiser_ids(qianchuan_runtime(), ebp)
        self.assertEqual(request.call_args.args[2]["account_source"], "QIANCHUAN")

    def test_direct_advertiser_prefers_advertiser_id(self):
        account = {
            "account_role": "ADVERTISER",
            "account_id": 101,
            "advertiser_id": 202,
        }
        identifiers, _ = token_manager.fetch_role_advertiser_ids(qianchuan_runtime(), account)
        self.assertEqual(identifiers, [202])

    def test_app_credentials_are_channel_isolated(self):
        entries = {}
        with mock.patch.object(
            credential_store,
            "write_entry",
            side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
        ), mock.patch.object(
            credential_store,
            "read_entry",
            side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
        ):
            credential_store.configure_app("marketing-app", "marketing-secret", "marketing")
            credential_store.configure_app("qianchuan-app", "qianchuan-secret", "qianchuan")
            marketing = authorization_store.read_app("marketing")
            qianchuan = authorization_store.read_app("qianchuan")
        self.assertEqual(marketing["app_id"], "marketing-app")
        self.assertEqual(qianchuan["app_id"], "qianchuan-app")

    def test_qianchuan_token_cannot_resolve_as_marketing(self):
        entries = {}
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {"CODEX_HOME": directory},
        ), mock.patch.object(
            credential_store,
            "write_entry",
            side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
        ), mock.patch.object(
            credential_store,
            "read_entry",
            side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
        ):
            authorization_store.save_authorization(
                "qianchuan",
                {"access_token": "qianchuan-token"},
                [{"account_id": "101", "advertiser_ids": ["201"]}],
            )
            with self.assertRaises(authorization_store.AuthorizationError) as raised:
                authorization_store.resolve("marketing", advertiser_id="201")
        self.assertEqual(raised.exception.code, "authorization_not_found")

    def test_refresh_uses_shared_qianchuan_token_endpoint(self):
        config = qianchuan_runtime()
        config["api"].update({
            "access_token_expires_at": "2000-01-01T00:00:00+00:00",
            "refresh_token_expires_at": "2999-01-01T00:00:00+00:00",
        })
        response = {"code": 0, "data": {"access_token": "new-access"}}
        with mock.patch.object(token_manager, "post_json", return_value=response) as post, mock.patch.object(
            token_manager,
            "save_credentials",
            side_effect=lambda value: value,
        ):
            token_manager.refresh_access_token("unused.json", config)
        self.assertEqual(post.call_args.args[0], "https://ad.oceanengine.com/open_api")
        self.assertEqual(post.call_args.args[1], channel_adapters.REFRESH_TOKEN_PATH)

    def test_authorized_subjects_use_oauth_host_with_access_token_header(self):
        response = {"code": 0, "data": {"list": []}}
        with mock.patch.object(
            token_manager,
            "get_business_json",
            return_value=response,
        ) as request:
            accounts, advertiser_ids, _, _ = token_manager.fetch_authorized_accounts(
                qianchuan_runtime()
            )
        self.assertEqual(accounts, [])
        self.assertEqual(advertiser_ids, [])
        self.assertEqual(request.call_args.args[0], "https://ad.oceanengine.com/open_api")
        self.assertEqual(request.call_args.args[1], "access")
        self.assertEqual(
            request.call_args.args[2],
            channel_adapters.AUTHORIZED_ACCOUNT_PATH,
        )

    def test_exchange_persists_pending_token_before_sync_failure(self):
        entries = {}
        response = {
            "code": 0,
            "data": {
                "access_token": "new-access",
                "refresh_token": "new-refresh",
                "expires_in": 3600,
                "refresh_token_expires_in": 7200,
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(channels.migrate_config(valid_config())), encoding="utf-8")
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), mock.patch.object(
                credential_store,
                "write_entry",
                side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
            ), mock.patch.object(
                credential_store,
                "read_entry",
                side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
            ), mock.patch.object(token_manager, "post_json", return_value=response), mock.patch.object(
                token_manager,
                "update_authorized_accounts",
                side_effect=RuntimeError("sync failed"),
            ):
                updated, summary = token_manager.exchange_auth_code(
                    config_path,
                    "auth-code",
                    config=qianchuan_runtime(),
                    channel="qianchuan",
                )
                authorization_id = updated["_authorization"]["authorization_id"]
                _, metadata, stored = authorization_store.resolve(
                    "qianchuan",
                    authorization_id=authorization_id,
                    allow_pending=True,
                )
        self.assertTrue(summary["authorized_accounts"]["sync_failed"])
        self.assertTrue(metadata["pending_account_sync"])
        self.assertEqual(stored["access_token"], "new-access")

    def test_successful_exchange_activates_snapshot(self):
        entries = {}
        response = {
            "code": 0,
            "data": {"access_token": "access", "refresh_token": "refresh"},
        }
        updated_accounts = qianchuan_runtime()
        updated_accounts["api"]["oauth_authorized_accounts"] = [
            {"account_id": "101", "account_role": "ADVERTISER", "advertiser_ids": ["201"]}
        ]
        updated_accounts["api"]["authorized_advertiser_ids"] = [201]
        updated_accounts["api"]["last_authorized_account_sync_at"] = "2026-07-14T00:00:00+00:00"
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(channels.migrate_config(valid_config())), encoding="utf-8")
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), mock.patch.object(
                credential_store,
                "write_entry",
                side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
            ), mock.patch.object(
                credential_store,
                "read_entry",
                side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
            ), mock.patch.object(token_manager, "post_json", return_value=response), mock.patch.object(
                token_manager,
                "update_authorized_accounts",
                return_value=(updated_accounts, {"authorized_advertiser_count": 1}),
            ):
                updated, _ = token_manager.exchange_auth_code(
                    config_path,
                    "auth-code",
                    config=qianchuan_runtime(),
                    channel="qianchuan",
                )
                authorization_id = updated["_authorization"]["authorization_id"]
                resolved, metadata, _ = authorization_store.resolve(
                    "qianchuan",
                    advertiser_id="201",
                )
        self.assertEqual(resolved, authorization_id)
        self.assertFalse(metadata["pending_account_sync"])

    def test_pending_authorization_retry_replaces_snapshot(self):
        entries = {}
        snapshot = [
            {"account_id": "101", "account_role": "ADVERTISER", "advertiser_ids": ["201"]}
        ]
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(channels.migrate_config(valid_config())), encoding="utf-8")
            with mock.patch.dict(os.environ, {"CODEX_HOME": directory}), mock.patch.object(
                credential_store,
                "write_entry",
                side_effect=lambda account, data: entries.__setitem__(account, copy.deepcopy(data)) or "test",
            ), mock.patch.object(
                credential_store,
                "read_entry",
                side_effect=lambda account: copy.deepcopy(entries.get(account, {})),
            ):
                authorization_id = authorization_store.save_authorization(
                    "qianchuan",
                    {"access_token": "access", "pending_account_sync": True},
                    [],
                )
                runtime = qianchuan_runtime()
                runtime["_authorization"] = {
                    "channel": "qianchuan",
                    "authorization_id": authorization_id,
                    "legacy": False,
                }
                synchronized = copy.deepcopy(runtime)
                synchronized["api"]["oauth_authorized_accounts"] = snapshot
                synchronized["api"]["authorized_advertiser_ids"] = [201]
                synchronized["api"]["last_authorized_account_sync_at"] = (
                    "2026-07-14T00:00:00+00:00"
                )
                with mock.patch.object(
                    token_manager,
                    "update_authorized_accounts",
                    return_value=(synchronized, {"authorized_advertiser_count": 1}),
                ):
                    token_manager.sync_authorized_accounts(config_path, runtime)
                resolved, metadata, _ = authorization_store.resolve(
                    "qianchuan",
                    advertiser_id="201",
                )
        self.assertEqual(resolved, authorization_id)
        self.assertFalse(metadata["pending_account_sync"])


if __name__ == "__main__":
    unittest.main()
