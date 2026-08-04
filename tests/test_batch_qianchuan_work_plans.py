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
from ocean_watch.plans import qianchuan_plan_gateway
from ocean_watch.templates import qianchuan_product_templates


def template():
    return qianchuan_product_templates.build_business_template(
        advertiser_id="1234567890123456",
        product_name="Test Product Full Name",
        product_short_name="Test Product",
        product_ids="1001",
        template_id="qcpt_test",
        template_name="测试千川模板",
    )


def material(
    item_id,
    aweme_id="9001",
    product_ids=None,
    *,
    material_id=None,
    title=None,
):
    return {
        "input_index": int(item_id),
        "aweme_item_id": str(item_id),
        "aweme_id": str(aweme_id),
        "creator_name_hint": f"Creator {aweme_id}",
        "creator": {
            "aweme_id": str(aweme_id),
            "aweme_show_id": f"show-{aweme_id}",
            "aweme_name": f"Creator {aweme_id}",
        },
        "material": {
            "aweme_item_id": str(item_id),
            "image_mode": "VIDEO_VERTICAL",
            "video_id": f"video-{item_id}",
            "material_id": material_id,
            "title": title,
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


class QianchuanPlanNameTemplateTests(unittest.TestCase):
    def test_default_plan_name_template_uses_runtime_type_and_business(self):
        name = batch.build_plan_name(
            template(),
            {
                "aweme_id": "9001",
                "aweme_show_id": "show-9001",
                "aweme_name": "Creator 9001",
            },
            plan_type="随手po",
            business="刘研",
            now=dt.datetime(2026, 8, 4, 12, 30, 45),
        )

        self.assertEqual(name, "8.4-Creator 9001-Test Product-随手po-刘研")

    def test_default_plan_name_template_omits_missing_optional_fields(self):
        self.assertEqual(
            batch.build_plan_name(
                template(),
                {"aweme_id": "9001", "aweme_name": "官方昵称"},
                creator_name="第三方昵称",
                now=dt.datetime(2026, 8, 4, 12, 30, 45),
            ),
            "8.4-第三方昵称-Test Product",
        )
        self.assertEqual(
            batch.build_plan_name(
                template(),
                {"aweme_id": "9001", "aweme_name": "达人甲"},
                plan_type="随手po",
                now=dt.datetime(2026, 8, 4, 12, 30, 45),
            ),
            "8.4-达人甲-Test Product-随手po",
        )

    def test_work_entry_parser_accepts_supported_user_formats(self):
        markdown = batch.parse_work_entry(
            "[https://v.douyin.com/bad/:code](https://v.douyin.com/abc/)\t真人口播营销\t刘岛"
        )
        command = batch.parse_work_entry(
            "4.87 口令 https://v.douyin.com/xyz/ 复制打开\t9386\t暖身,口播\t刘研"
        )
        bare = batch.parse_work_entry("https://v.douyin.com/only/")

        self.assertEqual(markdown, {
            "work_url": "https://v.douyin.com/abc/",
            "plan_type": "真人口播营销",
            "business": "刘岛",
        })
        self.assertEqual(
            command["work_url"],
            "4.87 口令 https://v.douyin.com/xyz/ 复制打开",
        )
        self.assertEqual(command["plan_type"], "暖身,口播")
        self.assertEqual(command["business"], "刘研")
        self.assertEqual(bare, {
            "work_url": "https://v.douyin.com/only/",
            "plan_type": "",
            "business": "",
        })

    def test_group_plan_name_fields_rejects_conflicting_rows(self):
        rows = [
            {**material("101"), "plan_type": "随手po", "business": "刘岛"},
            {**material("102"), "plan_type": "人设", "business": "刘岛"},
        ]
        with self.assertRaisesRegex(ValueError, "类型"):
            batch.group_plan_name_fields(rows)

    def test_legacy_plan_name_template_does_not_require_runtime_fields(self):
        selected = template()
        selected["plan_name_template"] = (
            qianchuan_product_templates.LEGACY_PLAN_NAME_TEMPLATE
        )

        name = batch.build_plan_name(
            selected,
            {"aweme_id": "9001", "aweme_name": "Creator 9001"},
            now=dt.datetime(2026, 8, 4, 12, 30, 45),
        )

        self.assertEqual(
            name,
            "Test Product Full Name-Creator 9001-20260804123045",
        )

    def test_custom_plan_name_template_renders_supported_placeholders(self):
        selected = template()
        selected["plan_name_template"] = (
            "{creator_name}_{douyin_id}_{aweme_id}_{product_name}_{date}_{time}_{datetime}"
        )

        name = batch.build_plan_name(
            selected,
            {
                "aweme_id": "9001",
                "aweme_show_id": "show-9001",
                "aweme_name": "达人甲",
            },
            now=dt.datetime(2026, 8, 4, 12, 30, 45),
        )

        self.assertEqual(
            name,
            "达人甲_show-9001_9001_Test Product Full Name_20260804_123045_20260804123045",
        )

    def test_rendered_plan_name_uses_weighted_character_limit(self):
        selected = template()
        selected["plan_name_template"] = "{creator_name}"

        name = batch.build_plan_name(
            selected,
            {"aweme_id": "9001", "aweme_name": "达" * 60},
            now=dt.datetime(2026, 8, 4, 12, 30, 45),
        )

        self.assertEqual(name, "达" * 50)

    def test_rendered_plan_name_removes_emoji_before_weighted_truncation(self):
        selected = template()
        selected["bindings"]["product_short_name"] = "奶酪✨产品"

        name = batch.build_plan_name(
            selected,
            {"aweme_id": "9001", "aweme_name": "达人🧀甲"},
            creator_name="达人🧀甲",
            plan_type="实况🎬",
            business="戴高兰",
            now=dt.datetime(2026, 8, 4, 12, 30, 45),
        )

        self.assertEqual(name, "8.4-达人甲-奶酪产品-实况-戴高兰")

    def test_empty_rendered_plan_name_is_rejected(self):
        selected = template()
        selected["plan_name_template"] = "{douyin_id}"

        with self.assertRaisesRegex(ValueError, "rendered plan name is empty"):
            batch.build_plan_name(
                selected,
                {"aweme_id": "9001", "aweme_name": "达人甲"},
                now=dt.datetime(2026, 8, 4, 12, 30, 45),
            )

    def test_rendered_plan_name_empty_after_sanitation_is_rejected(self):
        selected = template()
        selected["plan_name_template"] = "{creator_name}"

        with self.assertRaisesRegex(ValueError, "rendered plan name is empty"):
            batch.build_plan_name(
                selected,
                {"aweme_id": "9001", "aweme_name": "🧀✨"},
                creator_name="🧀✨",
                now=dt.datetime(2026, 8, 4, 12, 30, 45),
            )


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


class TrackingLock:
    def __init__(self, state, path, timeout):
        self.state = state
        self.state["path"] = path
        self.state["timeout"] = timeout

    def __enter__(self):
        self.state["held"] = True
        return self

    def __exit__(self, *_args):
        self.state["held"] = False


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

    def test_multiple_creator_plans_are_filtered_by_material_products(self):
        gateway = FakeGateway(plans={
            "9001": [
                existing_plan("7001", product_ids=["1001"]),
                existing_plan("7002", product_ids=["2002"]),
            ],
        })

        results, _ = batch.execute_plan_actions(
            template(),
            [material("101", product_ids=["1001"])],
            gateway,
            FakeExecutor(),
            concurrency=2,
            submit=False,
        )

        self.assertEqual(results[0]["status"], "would_append")
        self.assertEqual(results[0]["ad_id"], "7001")
        self.assertEqual(results[0]["product_ids"], ["1001"])

    def test_multiple_product_matching_plans_remain_ambiguous(self):
        gateway = FakeGateway(plans={
            "9001": [
                existing_plan("7001", product_ids=["1001"]),
                existing_plan("7002", product_ids=["1001", "2002"]),
                existing_plan("7003", product_ids=["3003"]),
            ],
        })

        results, _ = batch.execute_plan_actions(
            template(),
            [material("101", product_ids=["1001"])],
            gateway,
            FakeExecutor(),
            concurrency=2,
            submit=False,
        )

        self.assertEqual(results[0]["status"], "failed")
        self.assertEqual(results[0]["reason"], "multiple_existing_plans")
        self.assertEqual(results[0]["candidate_ad_ids"], ["7001", "7002"])

    def test_fifty_five_works_scan_the_plan_list_once_for_all_creators(self):
        calls = []

        class CountingPlanClient:
            def get(self, path, params=None):
                calls.append((path, copy.deepcopy(params)))
                if path == qianchuan_plan_gateway.QIANCHUAN_PLAN_LIST_PATH:
                    return {
                        "code": 0,
                        "data": {
                            "ad_list": [],
                            "page_info": {"total_page": 1},
                        },
                    }
                raise AssertionError(path)

        materials = [
            material(str(1000 + index), str(9000 + (index % 5)))
            for index in range(55)
        ]
        results, skipped = batch.execute_plan_actions(
            template(),
            materials,
            qianchuan_plan_gateway.QianchuanPlanGateway(CountingPlanClient()),
            FakeExecutor(),
            concurrency=8,
            submit=False,
            plan_type="随手po",
            business="刘研",
            now=dt.datetime(2026, 7, 15, 12, 30, 45),
        )

        list_calls = [
            row for row in calls
            if row[0] == qianchuan_plan_gateway.QIANCHUAN_PLAN_LIST_PATH
        ]
        self.assertEqual(len(list_calls), 1)
        self.assertEqual(len(results), 5)
        self.assertEqual(sum(len(row["input_item_ids"]) for row in results), 55)
        self.assertEqual({row["status"] for row in results}, {"would_create"})
        self.assertEqual(skipped, [])

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
            plan_type="随手po",
            business="刘研",
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
        self.assertEqual(
            payload["name"],
            "7.15-Creator 9001-Test Product-随手po-刘研",
        )

    def test_new_creator_requires_third_party_creator_name(self):
        row = material("101")
        row.pop("creator_name_hint")
        results, _ = batch.execute_plan_actions(
            template(),
            [row],
            FakeGateway(),
            FakeExecutor(),
            concurrency=2,
            submit=False,
        )

        self.assertEqual(results[0]["status"], "failed")
        self.assertIn("第三方解析接口未返回达人名称", results[0]["message"])

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

    def test_summary_requires_fixed_five_column_material_table(self):
        material_result = {
            "matched": [
                material("101", material_id="501", title="标题|一\n续行"),
                material("102", material_id=None, title="标题二"),
                material("201", "9002", material_id="601", title="标题三"),
            ],
            "skipped": [{"aweme_item_id": "999", "reason": "product_mismatch"}],
            "query_failures": [{"aweme_id": "9003", "reason": "timeout"}],
        }
        group_results = [
            {
                "aweme_id": "9001",
                "creator_name": "达人一",
                "ad_id": "7001",
                "product_ids": ["1001"],
                "input_item_ids": ["101", "102"],
                "already_present_item_ids": ["101"],
                "batches": [{"status": "appended", "item_ids": ["102"]}],
                "status": "appended",
            },
            {
                "aweme_id": "9002",
                "creator_name": "达人二",
                "ad_id": "8001",
                "product_ids": ["1001"],
                "input_item_ids": ["201"],
                "created_item_ids": ["201"],
                "status": "created",
            },
            {
                "aweme_id": "9003",
                "creator_name": "失败达人",
                "ad_id": None,
                "product_ids": ["1001"],
                "input_item_ids": ["301"],
                "status": "failed",
                "reason": "timeout",
            },
        ]

        result = batch.summarize(
            "submit",
            template(),
            {"resolved": [], "skipped": []},
            material_result,
            group_results,
            [],
        )

        presentation = result["presentation"]
        self.assertTrue(presentation["required"])
        self.assertFalse(presentation["allow_column_omission"])
        self.assertFalse(presentation["allow_column_reordering"])
        self.assertEqual(
            [detail["field"] for detail in presentation["required_details"]],
            ["skipped", "query_failures", "failed_results"],
        )
        self.assertEqual(
            [column["label"] for column in presentation["columns"]],
            ["计划ID", "达人昵称", "商品ID", "素材ID", "素材标题"],
        )
        self.assertEqual(
            [row["material_id"] for row in presentation["rows"]],
            ["501", "102", "601"],
        )
        self.assertEqual(
            [row["material_id_source"] for row in presentation["rows"]],
            ["material_id", "aweme_item_id", "material_id"],
        )
        markdown = presentation["rendered_markdown"]
        self.assertEqual(
            markdown.splitlines()[0],
            "| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |",
        )
        self.assertIn("| 7001 | 达人一 | 1001 | 501 | 标题\\|一 续行 |", markdown)
        self.assertNotIn("product_mismatch", markdown)
        self.assertNotIn("失败达人", markdown)
        self.assertEqual(result["failed_results"], [group_results[2]])

    def test_empty_summary_still_returns_fixed_table_header(self):
        result = batch.summarize(
            "submit",
            template(),
            {"resolved": [], "skipped": []},
            {"matched": [], "skipped": [], "query_failures": []},
            [],
            [],
        )

        self.assertEqual(result["presentation"]["rows"], [])
        self.assertEqual(
            result["presentation"]["rendered_markdown"],
            "| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |\n| --- | --- | --- | --- | --- |",
        )

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
            lock_state = {"held": False}

            def lock_factory(path, timeout):
                return TrackingLock(lock_state, path, timeout)

            def resolve_materials(*_args, **_kwargs):
                self.assertTrue(lock_state["held"])
                return material_result

            def execute_actions(*_args, **_kwargs):
                self.assertTrue(lock_state["held"])
                return [{"aweme_id": "9001", "status": "would_create"}], []

            with mock.patch.object(
                batch,
                "resolve_work_links",
                return_value=link_result,
            ), mock.patch.object(
                batch,
                "resolve_work_materials",
                side_effect=resolve_materials,
            ), mock.patch.object(
                batch,
                "execute_plan_actions",
                side_effect=execute_actions,
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
                    lock_factory=lock_factory,
                )
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["counts"]["would_create"], 1)
        self.assertEqual(result["counts"]["input_links"], 1)
        self.assertEqual(
            result["performance"]["request_budget"],
            {
                "limit": batch.BATCH_REQUEST_LIMIT,
                "used": 0,
                "remaining": batch.BATCH_REQUEST_LIMIT,
            },
        )
        cache = result["performance"]["owner_hint_cache"]
        self.assertEqual(cache["loaded"], 1)
        self.assertEqual(cache["stored"], 0)
        self.assertEqual(cache["warning"]["code"], "owner_hint_cache_write_failed")
        self.assertFalse(lock_state["held"])
        self.assertEqual(lock_state["timeout"], batch.BATCH_LOCK_TIMEOUT_SECONDS)
        self.assertEqual(
            lock_state["path"].name,
            "qianchuan-advertiser-1234567890123456.lock",
        )
        ensure_token.assert_not_called()

    def test_empty_batch_exposes_full_unused_request_budget(self):
        config = qianchuan_product_templates.ensure_config({})
        config[qianchuan_product_templates.TEMPLATES_KEY] = {"qcpt_test": template()}
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            args = SimpleNamespace(
                config=str(config_path),
                plan_template="qcpt_test",
                work_url=["https://v.douyin.com/invalid/"],
                concurrency=2,
                auth_account_id=None,
                submit=False,
                include_payloads=False,
                no_link_metadata_api=False,
                out=None,
            )
            with mock.patch.object(
                batch,
                "resolve_work_links",
                return_value={
                    "resolved": [],
                    "skipped": [{"input_index": 0, "reason": "invalid_work_url"}],
                },
            ):
                result, exit_code = batch.execute(args)

        self.assertEqual(exit_code, 0)
        self.assertEqual(
            result["performance"]["request_budget"],
            {
                "limit": batch.BATCH_REQUEST_LIMIT,
                "used": 0,
                "remaining": batch.BATCH_REQUEST_LIMIT,
            },
        )


if __name__ == "__main__":
    unittest.main()
