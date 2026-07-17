import copy
import datetime as dt
import json
import tempfile
import threading
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from ocean_watch.plans import batch_qianchuan_work_plans as batch
from ocean_watch.templates import qianchuan_product_templates


def template():
    return qianchuan_product_templates.build_business_template(
        advertiser_id="1234567890123456",
        product_name="Test Product",
        product_ids="1001",
        template_id="qcpt_test",
    )


def material(item_id, aweme_id="9001", product_ids=None):
    return {
        "input_index": int(item_id),
        "aweme_item_id": str(item_id),
        "aweme_id": str(aweme_id),
        "creator": {
            "aweme_id": str(aweme_id),
            "aweme_show_id": f"show-{aweme_id}",
            "aweme_name": f"Creator {aweme_id}",
        },
        "material": {
            "aweme_item_id": str(item_id),
            "image_mode": "VIDEO_VERTICAL",
            "video_id": f"video-{item_id}",
        },
        "matched_product_ids": product_ids or ["1001"],
    }


class FakeExecutor:
    def __init__(self, response=None):
        self.response = response or {"ad_id": "8001", "response": {"code": 0}}
        self.requests = []
        self.lock = threading.Lock()

    def execute(self, request):
        with self.lock:
            self.requests.append(copy.deepcopy(request))
        return copy.deepcopy(self.response)


class FakeGateway:
    def __init__(self, plans=None, existing=None):
        self.plans = plans or {}
        self.existing = existing or {}
        self.add_calls = []
        self.fail_material_ad_ids = set()
        self.lock = threading.Lock()

    def find_creator_plans(self, advertiser_id, aweme_ids, aweme_show_ids=None):
        self.aweme_show_ids = aweme_show_ids
        return {
            "matches": {
                str(aweme_id): copy.deepcopy(self.plans.get(str(aweme_id), []))
                for aweme_id in aweme_ids
            },
            "list_query": {"truncated": False},
        }

    def list_plan_video_materials(self, advertiser_id, ad_id):
        if str(ad_id) in self.fail_material_ad_ids:
            raise RuntimeError("material query failed")
        return {
            "materials": [
                {"material_info": {"video_material": {"aweme_item_id": int(item_id)}}}
                for item_id in self.existing.get(str(ad_id), [])
            ],
            "truncated": False,
        }

    def add_materials(self, advertiser_id, ad_id, creatives):
        payload = {
            "advertiser_id": int(advertiser_id),
            "ad_id": int(ad_id),
            "multi_product_creative_list": copy.deepcopy(creatives),
        }
        with self.lock:
            self.add_calls.append(payload)
        return payload, {"code": 0, "request_id": f"add-{ad_id}"}


def existing_plan(ad_id, status="DISABLE", product_ids=None):
    return {
        "ad_id": str(ad_id),
        "name": f"plan-{ad_id}",
        "status": status,
        "opt_status": "DISABLE",
        "product_ids": product_ids or ["1001"],
    }


class BatchQianchuanWorkPlanTests(unittest.TestCase):
    def test_default_concurrency_is_tuned_for_creator_discovery(self):
        args = batch.build_parser().parse_args([
            "--plan-template",
            "qcpt_test",
            "--work-url",
            "https://v.douyin.com/test/",
        ])
        self.assertEqual(args.concurrency, 8)

    def test_link_product_mismatch_is_skipped_before_material_queries(self):
        result = batch.filter_link_product_hints(
            {
                "resolved": [{
                    "input_index": 0,
                    "aweme_item_id": "101",
                    "product_hint": {"product_id": "2002"},
                }],
                "skipped": [],
            },
            ["1001"],
        )

        self.assertEqual(result["resolved"], [])
        self.assertEqual(result["skipped"][0]["reason"], "link_metadata_product_mismatch")
        self.assertEqual(result["skipped"][0]["hinted_product_id"], "2002")

    def test_empty_link_product_hint_continues_to_official_validation(self):
        row = {"input_index": 0, "aweme_item_id": "101"}
        result = batch.filter_link_product_hints(
            {"resolved": [row], "skipped": []},
            ["1001"],
        )
        self.assertEqual(result["resolved"], [row])

    def test_link_product_hint_matching_any_template_product_continues(self):
        row = {
            "input_index": 0,
            "aweme_item_id": "101",
            "product_hint": {"product_id": "2002"},
        }
        result = batch.filter_link_product_hints(
            {"resolved": [row], "skipped": []},
            ["1001", "2002"],
        )

        self.assertEqual(result["resolved"], [row])
        self.assertEqual(result["skipped"], [])

    def test_product_mismatch_stops_before_credentials_and_material_queries(self):
        config = qianchuan_product_templates.ensure_config({})
        config[qianchuan_product_templates.TEMPLATES_KEY] = {"qcpt_test": template()}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            args = SimpleNamespace(
                config=str(config_path),
                plan_template="qcpt_test",
                work_url=["https://v.douyin.com/test/"],
                concurrency=2,
                auth_account_id=None,
                submit=False,
                include_payloads=False,
                out=None,
                no_link_metadata_api=False,
            )
            link_result = {
                "resolved": [{
                    "input_index": 0,
                    "input_url": args.work_url[0],
                    "aweme_item_id": "101",
                    "product_hint": {"product_id": "2002"},
                }],
                "skipped": [],
            }
            with mock.patch.object(
                batch,
                "resolve_work_links",
                return_value=link_result,
            ), mock.patch.object(
                batch.token_manager,
                "ensure_access_token",
            ) as ensure_token, mock.patch.object(
                batch,
                "resolve_work_materials",
            ) as resolve_materials:
                result, exit_code = batch.execute(args)

        self.assertEqual(exit_code, 0)
        self.assertEqual(result["counts"]["matched_links"], 0)
        self.assertEqual(result["counts"]["skipped_links"], 1)
        self.assertEqual(
            result["skipped"][0]["reason"],
            "link_metadata_product_mismatch",
        )
        ensure_token.assert_not_called()
        resolve_materials.assert_not_called()
        self.assertEqual(result["performance"]["link_metadata"], {
            "configured": False,
            "enabled": False,
        })

    def test_local_metadata_endpoint_enables_resolver_without_exposing_default(self):
        config = qianchuan_product_templates.ensure_config({})
        config[qianchuan_product_templates.TEMPLATES_KEY] = {"qcpt_test": template()}
        config["integrations"] = {
            "qianchuan_work_metadata": {
                "endpoint": "https://metadata.example.test/api",
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            args = SimpleNamespace(
                config=str(config_path),
                plan_template="qcpt_test",
                work_url=["https://v.douyin.com/test/"],
                concurrency=2,
                auth_account_id=None,
                submit=False,
                include_payloads=False,
                out=None,
                no_link_metadata_api=False,
            )
            with mock.patch.object(
                batch,
                "DouyinWorkMetadataResolver",
            ) as metadata_resolver, mock.patch.object(
                batch,
                "DouyinWorkLinkResolver",
            ) as link_resolver, mock.patch.object(
                batch,
                "resolve_work_links",
                return_value={"resolved": [], "skipped": []},
            ):
                result, exit_code = batch.execute(args)

        self.assertEqual(exit_code, 0)
        metadata_resolver.assert_called_once_with(
            "https://metadata.example.test/api"
        )
        link_resolver.assert_called_once_with(
            metadata_resolver=metadata_resolver.return_value
        )
        self.assertEqual(result["performance"]["link_metadata"], {
            "configured": True,
            "enabled": True,
        })

    def test_disable_flag_ignores_invalid_local_metadata_endpoint(self):
        config = qianchuan_product_templates.ensure_config({})
        config[qianchuan_product_templates.TEMPLATES_KEY] = {"qcpt_test": template()}
        config["integrations"] = {
            "qianchuan_work_metadata": {"endpoint": "http://invalid.test/api"},
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            args = SimpleNamespace(
                config=str(config_path),
                plan_template="qcpt_test",
                work_url=["https://v.douyin.com/test/"],
                concurrency=2,
                auth_account_id=None,
                submit=False,
                include_payloads=False,
                out=None,
                no_link_metadata_api=True,
            )
            with mock.patch.object(
                batch,
                "resolve_work_links",
                return_value={"resolved": [], "skipped": []},
            ), mock.patch.object(
                batch,
                "DouyinWorkMetadataResolver",
            ) as metadata_resolver:
                result, exit_code = batch.execute(args)

        self.assertEqual(exit_code, 0)
        metadata_resolver.assert_not_called()
        self.assertEqual(result["performance"]["link_metadata"], {
            "configured": True,
            "enabled": False,
        })

    def test_paused_plan_only_appends_material_difference(self):
        gateway = FakeGateway(
            plans={"9001": [existing_plan("7001")]},
            existing={"7001": ["101"]},
        )
        executor = FakeExecutor()
        results, skipped = batch.execute_plan_actions(
            template(),
            [material("101"), material("102")],
            gateway,
            executor,
            concurrency=2,
            submit=True,
        )
        self.assertEqual(skipped, [])
        self.assertEqual(results[0]["status"], "appended")
        self.assertEqual(results[0]["plan_status"], "DISABLE")
        self.assertEqual(results[0]["already_present_item_ids"], ["101"])
        self.assertEqual(results[0]["appended_item_ids"], ["102"])
        self.assertEqual(executor.requests, [])
        self.assertEqual(gateway.aweme_show_ids, {"9001": "show-9001"})
        videos = gateway.add_calls[0]["multi_product_creative_list"][0]["video_material"]
        self.assertEqual([row["aweme_item_id"] for row in videos], [102])

    def test_new_creator_uses_template_and_runtime_homepage_materials(self):
        gateway = FakeGateway()
        executor = FakeExecutor()
        results, _ = batch.execute_plan_actions(
            template(),
            [material("101"), material("102")],
            gateway,
            executor,
            concurrency=2,
            submit=True,
            now=dt.datetime(2026, 7, 15, 12, 30, 45),
        )
        self.assertEqual(results[0]["status"], "created")
        request = executor.requests[0]
        payload = request.payload
        self.assertEqual(payload["advertiser_id"], 1234567890123456)
        self.assertEqual(payload["aweme_id"], 9001)
        self.assertEqual(payload["delivery_setting"]["budget"], 5000.0)
        self.assertEqual(payload["delivery_setting"]["roi2_goal"], 1.7)
        creative = payload["multi_product_creative_list"][0]
        self.assertNotIn("creative_card", creative)
        self.assertEqual(
            [row["aweme_item_id"] for row in creative["video_material"]],
            [101, 102],
        )
        self.assertTrue(payload["name"].endswith("20260715123045"))

    def test_one_creator_failure_does_not_stop_other_creator(self):
        gateway = FakeGateway(plans={
            "9001": [existing_plan("7001")],
            "9002": [existing_plan("7002")],
        })
        gateway.fail_material_ad_ids.add("7001")
        results, _ = batch.execute_plan_actions(
            template(),
            [material("101", "9001"), material("201", "9002")],
            gateway,
            FakeExecutor(),
            concurrency=2,
            submit=True,
        )
        by_creator = {row["aweme_id"]: row for row in results}
        self.assertEqual(by_creator["9001"]["status"], "failed")
        self.assertEqual(by_creator["9002"]["status"], "appended")
        self.assertEqual(len(gateway.add_calls), 1)

    def test_command_returns_one_final_summary_without_local_files(self):
        config = qianchuan_product_templates.ensure_config({})
        config[qianchuan_product_templates.TEMPLATES_KEY] = {"qcpt_test": template()}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            args = SimpleNamespace(
                config=str(config_path),
                plan_template="qcpt_test",
                work_url=["https://v.douyin.com/test/"],
                concurrency=2,
                auth_account_id=None,
                submit=False,
                include_payloads=False,
                out=None,
            )
            link_result = {
                "resolved": [{
                    "input_index": 0,
                    "input_url": args.work_url[0],
                    "aweme_item_id": "101",
                }],
                "skipped": [],
            }
            material_result = {
                "matched": [material("101")],
                "skipped": [],
                "query_failures": [],
                "resolved_owner_hints": {
                    "101": {"aweme_id": "9001", "aweme_show_id": "creator-one"}
                },
                "owner_hint_summary": {"verified": 1},
            }
            with mock.patch.object(
                batch,
                "resolve_work_links",
                return_value=link_result,
            ), mock.patch.object(
                batch,
                "resolve_work_materials",
                return_value=material_result,
            ), mock.patch.object(
                batch,
                "execute_plan_actions",
                return_value=([{"aweme_id": "9001", "status": "would_create"}], []),
            ), mock.patch.object(
                batch.token_manager,
                "ensure_access_token",
            ) as ensure_token, mock.patch.object(
                batch.qianchuan_work_owner_cache,
                "load_owner_hints",
                return_value={
                    "101": {"aweme_id": "9001", "aweme_show_id": "creator-one"}
                },
            ), mock.patch.object(
                batch.qianchuan_work_owner_cache,
                "update_owner_hints",
                side_effect=OSError("cache unavailable"),
            ):
                result, exit_code = batch.execute(
                    args,
                    clients=(object(), object()),
                )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["counts"]["would_create"], 1)
        self.assertEqual(result["counts"]["input_links"], 1)
        cache = result["performance"]["owner_hint_cache"]
        self.assertEqual(cache["loaded"], 1)
        self.assertEqual(cache["stored"], 0)
        self.assertEqual(cache["warning"]["code"], "owner_hint_cache_write_failed")
        ensure_token.assert_not_called()


if __name__ == "__main__":
    unittest.main()
