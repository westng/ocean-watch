import json
import multiprocessing
import tempfile
import time
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

from ocean_watch.accounts import manage_accounts, managed_accounts
from ocean_watch.core.errors import ConfigurationError


def add_account_in_process(config_path, advertiser_id, ready, start):
    original_upsert = managed_accounts.upsert

    def delayed_upsert(*args, **kwargs):
        result = original_upsert(*args, **kwargs)
        time.sleep(0.05)
        return result

    managed_accounts.upsert = delayed_upsert
    ready.put(True)
    if not start.wait(timeout=10):
        raise RuntimeError("timed out waiting to start managed-account mutation")
    with redirect_stdout(StringIO()):
        manage_accounts.main([
            "add",
            "--config",
            config_path,
            "--channel",
            "marketing",
            "--advertiser-id",
            advertiser_id,
            "--name",
            f"Account {advertiser_id}",
        ])


class ManagedAccountTests(unittest.TestCase):
    def test_list_presentation_is_mandatory_and_membership_only(self):
        accounts = [
            {
                "channel": "qianchuan",
                "advertiser_id": "1234567890123456",
                "name": "常用|账户",
                "enabled": True,
            },
        ]

        presentation = manage_accounts.list_presentation(accounts)

        self.assertTrue(presentation["required"])
        self.assertFalse(presentation["allow_column_omission"])
        self.assertEqual(
            [column["label"] for column in presentation["columns"]],
            ["渠道", "账户名称", "广告主 ID", "启用状态"],
        )
        markdown = presentation["rendered_markdown"]
        self.assertIn("共 1 个；仅展示已启用账户", markdown)
        self.assertIn("| 巨量千川 | 常用\\|账户 | 1234567890123456 | 已启用 |", markdown)
        self.assertNotIn("消耗", markdown)
        self.assertNotIn("ROI", markdown)

    def test_same_advertiser_can_exist_in_both_channels_and_update_in_place(self):
        config = {"config_schema_version": 2}
        config, marketing, created = managed_accounts.upsert(
            config,
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Marketing account",
        )
        self.assertTrue(created)
        config, qianchuan, created = managed_accounts.upsert(
            config,
            channel="qianchuan",
            advertiser_id_value="1234567890123456",
            name="Qianchuan account",
        )
        self.assertTrue(created)
        self.assertNotEqual(marketing["channel"], qianchuan["channel"])

        config, updated, created = managed_accounts.upsert(
            config,
            channel="qianchuan",
            advertiser_id_value="1234567890123456",
            name="Renamed account",
        )
        self.assertFalse(created)
        self.assertEqual(updated["name"], "Renamed account")
        self.assertEqual(len(managed_accounts.list_accounts(config)), 2)

    def test_disabled_accounts_are_excluded_by_default_query(self):
        config, _, _ = managed_accounts.upsert(
            {},
            channel="qianchuan",
            advertiser_id_value="1234567890123456",
            name="Account",
        )
        config, record = managed_accounts.set_enabled(
            config,
            channel="qianchuan",
            advertiser_id_value="1234567890123456",
            enabled=False,
        )
        self.assertFalse(record["enabled"])
        self.assertEqual(managed_accounts.list_accounts(config, enabled_only=True), [])
        self.assertEqual(len(managed_accounts.list_accounts(config)), 1)

    def test_rename_preserves_disabled_state_and_authorization_binding(self):
        config, _, _ = managed_accounts.upsert(
            {},
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Original name",
            enabled=False,
            auth_account_id="987654321",
        )
        config, renamed, created = managed_accounts.upsert(
            config,
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Renamed account",
        )
        self.assertFalse(created)
        self.assertFalse(renamed["enabled"])
        self.assertEqual(renamed["auth_account_id"], "987654321")
        self.assertEqual(managed_accounts.list_accounts(config), [renamed])

    def test_enabled_values_must_be_booleans(self):
        for value in (0, 1, None, "", "false", "true", [], {}):
            with self.subTest(value=value), self.assertRaisesRegex(
                ConfigurationError,
                "enabled must be a boolean",
            ):
                managed_accounts.migrate({
                    "managed_accounts": {
                        "marketing": [{
                            "advertiser_id": "1234567890123456",
                            "name": "Account",
                            "enabled": value,
                        }],
                    },
                })

        with self.assertRaisesRegex(ConfigurationError, "enabled must be a boolean"):
            managed_accounts.upsert(
                {},
                channel="marketing",
                advertiser_id_value="1234567890123456",
                name="Account",
                enabled="false",
            )

    def test_advertiser_ids_are_canonical_bounded_decimals(self):
        maximum = "9223372036854775807"
        self.assertEqual(managed_accounts.advertiser_id(maximum), maximum)
        self.assertEqual(managed_accounts.advertiser_id(123), "123")
        for value in (
            None,
            False,
            0,
            "",
            "0",
            "01",
            "+1",
            " 1",
            "1 ",
            "1.0",
            "9223372036854775808",
        ):
            with self.subTest(value=value), self.assertRaises(ConfigurationError):
                managed_accounts.advertiser_id(value)

    def test_auth_account_id_is_optional_and_must_be_canonical(self):
        config, unbound, _ = managed_accounts.upsert(
            {},
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Unbound",
        )
        self.assertNotIn("auth_account_id", unbound)
        config, bound, _ = managed_accounts.upsert(
            config,
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Bound",
            auth_account_id="987654321",
        )
        self.assertEqual(bound["auth_account_id"], "987654321")
        with self.assertRaisesRegex(ConfigurationError, "auth_account_id"):
            managed_accounts.upsert(
                config,
                channel="marketing",
                advertiser_id_value="1234567890123456",
                name="Invalid binding",
                auth_account_id="0987654321",
            )

    def test_remove_requires_an_existing_channel_account_pair(self):
        config, _, _ = managed_accounts.upsert(
            {},
            channel="marketing",
            advertiser_id_value="1234567890123456",
            name="Account",
        )
        with self.assertRaisesRegex(Exception, "not found"):
            managed_accounts.remove(
                config,
                channel="qianchuan",
                advertiser_id_value="1234567890123456",
            )
        config, _ = managed_accounts.remove(
            config,
            channel="marketing",
            advertiser_id_value="1234567890123456",
        )
        self.assertEqual(managed_accounts.list_accounts(config), [])

    def test_cli_add_writes_only_non_sensitive_account_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps({"channels": {}}), encoding="utf-8")
            with redirect_stdout(StringIO()) as output:
                code = manage_accounts.main([
                    "add",
                    "--config",
                    str(path),
                    "--channel",
                    "qianchuan",
                    "--advertiser-id",
                    "1234567890123456",
                    "--auth-account-id",
                    "987654321",
                    "--name",
                    "Managed account",
                ])
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(output.getvalue())["action"], "created")
            stored = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(
                stored["managed_accounts"]["qianchuan"][0]["name"],
                "Managed account",
            )
            self.assertEqual(
                stored["managed_accounts"]["qianchuan"][0]["auth_account_id"],
                "987654321",
            )
            self.assertNotIn("access_token", json.dumps(stored))

    def test_cli_list_returns_membership_presentation(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps({
                "managed_accounts": {
                    "qianchuan": [{
                        "advertiser_id": "1234567890123456",
                        "name": "Managed account",
                        "enabled": True,
                    }],
                },
            }), encoding="utf-8")

            with redirect_stdout(StringIO()) as output:
                code = manage_accounts.main(["list", "--config", str(path)])

            self.assertEqual(code, 0)
            result = json.loads(output.getvalue())
            self.assertTrue(result["presentation"]["required"])
            self.assertIn("Managed account", result["presentation"]["rendered_markdown"])
            self.assertNotIn("query_status", result["accounts"][0])

    def test_concurrent_cli_mutations_preserve_every_account(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps({"channels": {}}), encoding="utf-8")
            context = multiprocessing.get_context("spawn")
            ready = context.Queue()
            start = context.Event()
            identifiers = ["1001", "1002", "1003", "1004"]
            processes = [
                context.Process(
                    target=add_account_in_process,
                    args=(str(path), identifier, ready, start),
                )
                for identifier in identifiers
            ]
            for process in processes:
                process.start()
            for _ in processes:
                self.assertTrue(ready.get(timeout=10))
            start.set()
            for process in processes:
                process.join(timeout=15)
                self.assertFalse(process.is_alive())
                self.assertEqual(process.exitcode, 0)

            stored = json.loads(path.read_text(encoding="utf-8"))
            accounts = managed_accounts.list_accounts(stored, channel="marketing")
            self.assertEqual(
                {account["advertiser_id"] for account in accounts},
                set(identifiers),
            )


if __name__ == "__main__":
    unittest.main()
