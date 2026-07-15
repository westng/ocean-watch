import copy
import datetime as dt
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import channels
from ocean_watch.plans import create_qianchuan_plan
from ocean_watch.plans.qianchuan_executor import (
    QIANCHUAN_CREATE_PATH,
    QianchuanPlanExecutionRequest,
    QianchuanPlanExecutor,
)

from tests.support import valid_config


def product_payload():
    return {
        "advertiser_id": "1234567890123456",
        "name": "商品全域测试计划",
        "marketing_goal": "VIDEO_PROM_GOODS",
        "product_ids": ["9876543210987654"],
        "delivery_setting": {
            "smart_bid_type": "SMART_BID_CUSTOM",
            "roi2_goal": 1.5,
            "budget": 5000,
            "video_schedule_type": "SCHEDULE_FROM_NOW",
            "deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
        },
    }


def live_payload():
    return {
        "advertiser_id": "1234567890123456",
        "aweme_id": "48917855087",
        "marketing_goal": "LIVE_PROM_GOODS",
        "delivery_setting": {
            "smart_bid_type": "SMART_BID_CONSERVATIVE",
            "budget": 5000,
            "live_schedule_type": "SCHEDULE_FROM_NOW",
            "daily_delivery_time": 8.5,
        },
        "creative_setting": {"smart_select_material": True},
    }


class FakeClient:
    def __init__(self, response):
        self.response = response
        self.calls = []

    def post(self, path, payload):
        self.calls.append((path, payload))
        return self.response


class QianchuanPlanCreationTests(unittest.TestCase):
    def test_product_plan_accepts_official_minimum_combination(self):
        payload, blocking = create_qianchuan_plan.normalize_and_validate(product_payload())
        self.assertEqual(blocking, ())
        self.assertEqual(payload["advertiser_id"], 1234567890123456)
        self.assertEqual(payload["product_ids"], [9876543210987654])
        self.assertEqual(payload["delivery_setting"]["roi2_goal"], 1.5)

    def test_product_plan_name_is_optional(self):
        payload = product_payload()
        payload.pop("name")
        _, blocking = create_qianchuan_plan.normalize_and_validate(payload)
        self.assertNotIn("name", blocking)

    def test_live_plan_requires_aweme_id_and_rejects_name(self):
        payload = live_payload()
        payload.pop("aweme_id")
        payload["name"] = "unsupported"
        _, blocking = create_qianchuan_plan.normalize_and_validate(payload)
        self.assertIn("aweme_id", blocking)
        self.assertIn("name", blocking)

    def test_custom_bid_requires_roi_and_conservative_bid_rejects_it(self):
        custom = product_payload()
        custom["delivery_setting"].pop("roi2_goal")
        _, custom_blocking = create_qianchuan_plan.normalize_and_validate(custom)
        self.assertIn("delivery_setting.roi2_goal", custom_blocking)

        conservative = product_payload()
        conservative["delivery_setting"]["smart_bid_type"] = "SMART_BID_CONSERVATIVE"
        _, conservative_blocking = create_qianchuan_plan.normalize_and_validate(conservative)
        self.assertIn("delivery_setting.roi2_goal", conservative_blocking)

    def test_product_plan_rejects_more_than_thirty_products(self):
        payload = product_payload()
        payload["product_ids"] = list(range(1, 32))
        _, blocking = create_qianchuan_plan.normalize_and_validate(payload)
        self.assertIn("product_ids", blocking)

    def test_scheduled_plan_validates_dates(self):
        payload = product_payload()
        payload["delivery_setting"].update({
            "video_schedule_type": "SCHEDULE_START_END",
            "start_time": "2026-07-15",
            "end_time": "2026-07-14",
        })
        _, blocking = create_qianchuan_plan.normalize_and_validate(
            payload,
            today=dt.date(2026, 7, 14),
        )
        self.assertIn("delivery_setting.end_time", blocking)

    def test_product_materials_validate_title_and_card_rules(self):
        payload = product_payload()
        payload["aweme_id"] = "48917855087"
        payload["multi_product_creative_list"] = [{
            "product_id": "9876543210987654",
            "creative_type": "PROGRAMMATIC_CREATIVE",
            "image_material": [{"image_mode": "SQUARE", "image_ids": ["image-1"]}],
            "title_material": [{"title": "太短", "title_type": "CUSTOM"}],
        }]
        _, blocking = create_qianchuan_plan.normalize_and_validate(payload)
        self.assertIn("multi_product_creative_list[0].title_material[0].title", blocking)
        self.assertIn(
            "multi_product_creative_list[0].title_material.COMMODITY_CARD",
            blocking,
        )
        self.assertNotIn("multi_product_creative_list[0].creative_card", blocking)

    def test_unknown_top_level_field_is_rejected(self):
        payload = product_payload()
        payload["unexpected"] = True
        with self.assertRaisesRegex(Exception, "unknown fields"):
            create_qianchuan_plan.normalize_and_validate(payload)

    def test_payload_advertiser_must_match_cli_override(self):
        with self.assertRaisesRegex(Exception, "does not match"):
            create_qianchuan_plan.normalize_and_validate(
                product_payload(),
                advertiser_id="999",
            )

    def test_dry_run_never_calls_official_api(self):
        client = FakeClient({})
        request = QianchuanPlanExecutionRequest(product_payload(), submit=False)
        result = QianchuanPlanExecutor(client).execute(request)
        self.assertEqual(client.calls, [])
        self.assertEqual(result["endpoint"], QIANCHUAN_CREATE_PATH)

    def test_submit_returns_official_ad_id(self):
        client = FakeClient({"code": 0, "data": {"ad_id": 987654321}})
        request = QianchuanPlanExecutionRequest(product_payload(), submit=True)
        result = QianchuanPlanExecutor(client).execute(request)
        self.assertEqual(result["ad_id"], "987654321")
        self.assertEqual(client.calls[0][0], QIANCHUAN_CREATE_PATH)

    def test_submit_failure_is_explicit(self):
        client = FakeClient({"code": 40000, "message": "invalid"})
        request = QianchuanPlanExecutionRequest(product_payload(), submit=True)
        result = QianchuanPlanExecutor(client).execute(request)
        self.assertTrue(result["submit_failed"])
        self.assertEqual(result["failure_stage"], "qianchuan_plan_create")

    def test_nonzero_response_code_cannot_be_successful(self):
        client = FakeClient({"code": 40000, "data": {"ad_id": 987654321}})
        request = QianchuanPlanExecutionRequest(product_payload(), submit=True)
        result = QianchuanPlanExecutor(client).execute(request)
        self.assertTrue(result["submit_failed"])
        self.assertNotIn("ad_id", result)

    def test_cli_dry_run_does_not_resolve_real_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = channels.migrate_config(valid_config())
            config_path.write_text(json.dumps(config), encoding="utf-8")
            payload_path = Path(directory) / "payload.json"
            payload_path.write_text(json.dumps(product_payload()), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(
                create_qianchuan_plan.token_manager,
                "ensure_access_token",
            ) as ensure_token, redirect_stdout(output):
                exit_code = create_qianchuan_plan.main([
                    "--config",
                    str(config_path),
                    "--payload-file",
                    str(payload_path),
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["mode"], "dry_run")
        self.assertEqual(result["endpoint"], QIANCHUAN_CREATE_PATH)
        ensure_token.assert_not_called()

    def test_submit_resolves_only_qianchuan_authorization(self):
        runtime = channels.runtime_config(
            channels.migrate_config(valid_config()),
            "qianchuan",
            capability="qianchuan_create",
        )
        runtime["api"].update({"access_token": "token"})
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(channels.migrate_config(valid_config())), encoding="utf-8")
            payload_path = Path(directory) / "payload.json"
            payload_path.write_text(json.dumps(product_payload()), encoding="utf-8")
            executor = mock.Mock()
            executor.execute.return_value = {"ad_id": "1"}
            with mock.patch.object(
                create_qianchuan_plan.token_manager,
                "ensure_access_token",
                return_value=copy.deepcopy(runtime),
            ) as ensure_token, mock.patch.object(
                create_qianchuan_plan.QianchuanPlanExecutor,
                "from_credentials",
                return_value=executor,
            ), redirect_stdout(StringIO()):
                exit_code = create_qianchuan_plan.main([
                    "--config",
                    str(config_path),
                    "--payload-file",
                    str(payload_path),
                    "--submit",
                ])
        self.assertEqual(exit_code, 0)
        self.assertEqual(ensure_token.call_args.kwargs["channel"], "qianchuan")
        self.assertEqual(
            ensure_token.call_args.kwargs["advertiser_id"],
            "1234567890123456",
        )

    def test_blocked_submit_does_not_resolve_credentials(self):
        payload = product_payload()
        payload["delivery_setting"].pop("roi2_goal")
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(
                json.dumps(channels.migrate_config(valid_config())),
                encoding="utf-8",
            )
            payload_path = Path(directory) / "payload.json"
            payload_path.write_text(json.dumps(payload), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(
                create_qianchuan_plan.token_manager,
                "ensure_access_token",
            ) as ensure_token, redirect_stdout(output):
                exit_code = create_qianchuan_plan.main([
                    "--config",
                    str(config_path),
                    "--payload-file",
                    str(payload_path),
                    "--submit",
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(exit_code, 1)
        self.assertTrue(result["submit_blocked"])
        ensure_token.assert_not_called()


if __name__ == "__main__":
    unittest.main()
