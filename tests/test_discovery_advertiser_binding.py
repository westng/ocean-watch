import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.discovery import query_projects, query_promotions


class DiscoveryAdvertiserBindingTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.config_path = Path(self.directory.name) / "config.json"
        self.config_path.write_text(json.dumps({
            "api": {"base_url": "https://api.example.test/open_api"},
            "account": {"advertiser_id": "REPLACE_WITH_ADVERTISER_ID"},
        }), encoding="utf-8")

    def tearDown(self):
        self.directory.cleanup()

    def run_query(self, module, extra_args):
        captured = {}

        def ensure_access_token(config_path, config, **kwargs):
            captured["token_advertiser_id"] = kwargs["advertiser_id"]
            runtime = copy.deepcopy(config)
            runtime.setdefault("api", {})["access_token"] = "test-token"
            return runtime

        def get_json(base_url, token, path, params):
            captured.update({
                "base_url": base_url,
                "token": token,
                "path": path,
                "params": params,
            })
            return {"code": 0, "message": "OK", "data": {"list": []}}

        with mock.patch.object(
            module.token_manager,
            "ensure_access_token",
            side_effect=ensure_access_token,
        ), mock.patch.object(module, "get_json", side_effect=get_json), redirect_stdout(StringIO()):
            exit_code = module.main([
                "--config",
                str(self.config_path),
                "--advertiser-id",
                "1234567890123456",
                *extra_args,
            ])
        return exit_code, captured

    def test_project_query_binds_explicit_advertiser_to_token_and_request(self):
        exit_code, captured = self.run_query(query_projects, [])
        self.assertEqual(exit_code, 0)
        self.assertEqual(captured["token_advertiser_id"], "1234567890123456")
        self.assertEqual(captured["params"]["advertiser_id"], "1234567890123456")

    def test_promotion_query_binds_explicit_advertiser_to_token_and_request(self):
        exit_code, captured = self.run_query(
            query_promotions,
            ["--project-id", "7654321098765432"],
        )
        self.assertEqual(exit_code, 0)
        self.assertEqual(captured["token_advertiser_id"], "1234567890123456")
        self.assertEqual(captured["params"]["advertiser_id"], "1234567890123456")
        self.assertEqual(captured["params"]["filtering"]["project_id"], 7654321098765432)
