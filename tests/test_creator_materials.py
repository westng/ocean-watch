import copy
import datetime as dt
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from ocean_watch.auth import token_manager
from ocean_watch.materials import creator_materials, query_creator_materials
from ocean_watch.plans import create_creator_plan, create_plan
from ocean_watch.plans import executor as plan_executor
from ocean_watch.templates import (
    manage_plan_templates,
    plan_templates,
    template_workflow,
)

from tests.support import PromptAnswers

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = json.loads(
    (ROOT / "tests" / "fixtures" / "creator_authorizations.json").read_text(encoding="utf-8")
)
NOW = dt.datetime(2026, 7, 13, 0, 0, 0)


def payload_config():
    return {
        "api": {"base_url": "https://api.example.test/open_api"},
        "account": {"advertiser_id": "1234567890123456"},
        "defaults": {
            "operation": "ENABLE",
            "project_name_template": "project_{material_date}_{suffix}",
            "promotion_name_template": "promotion_{material_date}_{suffix}",
            "product_name": "test product",
            "product_id": "product-1",
            "daily_budget": 300,
            "cpa_bid": 100,
            "source": "test source",
            "landing_type": "SHOP",
            "marketing_goal": "VIDEO_AND_IMAGE",
            "delivery_mode": "PROCEDURAL",
            "ad_type": "ALL",
            "gender": "NONE",
            "ages": [],
            "location_type": "CURRENT",
            "district": "REGION",
            "region_version": "2.3.2",
            "hide_if_converted": "NO_EXCLUDE",
            "schedule_type": "SCHEDULE_FROM_NOW",
            "budget_mode": "BUDGET_MODE_DAY",
            "pricing": "PRICING_OCPM",
            "deep_bid_type": "NET_ORDER_ROI",
            "roi_goal": 1.5,
            "video_image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
            "product_info": {"product_image_type": "CUSTOM"},
        },
        "materials": {
            "video_ids": ["9101", "9102"],
            "video_cover_ids": {"9101": "9201", "9102": "9202"},
        },
        "resolved_ids": {
            "city_ids": [1],
            "unique_product_id": "product-1",
            "product_image_ids": ["image-1"],
        },
        "tracking_urls": {
            "track_url": ["https://tracking.test/impression"],
            "action_track_url": ["https://tracking.test/click"],
        },
        "links": {
            "landing_page_url": "https://landing.test/page",
            "open_url": "testapp://open",
        },
        "titles": ["这是达人素材测试文案"],
    }


def payload_args():
    return SimpleNamespace(
        advertiser_id=None,
        budget=None,
        bid=None,
        roi_goal=None,
        video_id=None,
        material_date="7.13",
        product_name=None,
        product_id=None,
        project_name=None,
        promotion_name=None,
        project_id=None,
    )


def creator_template_config():
    source = payload_config()
    config = plan_templates.migrate(source)
    name = "巨量营销-1234567890123456-test product-product-1-原生素材"
    config["plan_templates"] = {
        name: {
            "display_name": name,
            "bindings": {
                "channel": "marketing",
                "advertiser_id": "1234567890123456",
                "platform": "示例平台",
                "traffic_source": "CID",
                "product_id": "product-1",
                "product_name": "test product",
            },
            "copy_materials": {"titles": ["这是达人素材测试文案"]},
            "material_strategy": {
                "source_type": "CREATOR_AUTHORIZED",
                "selection_mode": "MANUAL",
                "max_materials_per_unit": 5,
                "creator_filters": {
                    "creator_ids": [],
                    "auth_types": ["VIDEO_ITEM"],
                    "authorization_status": "VALID",
                    "minimum_remaining_days": 1,
                },
            },
            "overrides": {
                "defaults": {"source": "test source"},
                "resolved_ids": {
                    "unique_product_id": "product-1",
                    "product_image_ids": ["image-1"],
                },
                "links": copy.deepcopy(source["links"]),
                "tracking_urls": copy.deepcopy(source["tracking_urls"]),
            },
        }
    }
    return config, name


class CreatorMaterialDevelopmentTests(unittest.TestCase):
    def fetch_fixture(self):
        calls = []

        def fetch_json(base_url, token, path, params):
            calls.append((base_url, token, path, params))
            return FIXTURE["pages"][int(params["page"]) - 1]

        result = creator_materials.fetch_candidates(
            fetch_json,
            "https://api.example.test/open_api",
            "test-token",
            "1234567890123456",
            minimum_remaining_days=1,
            page_size=2,
            now=NOW,
        )
        return result, calls

    def test_fetches_all_authorized_video_pages_and_normalizes_ids(self):
        result, calls = self.fetch_fixture()
        self.assertEqual(result["endpoint"], "/2/tools/aweme_auth_list/")
        self.assertEqual(result["page_count"], 2)
        self.assertEqual(len(result["candidates"]), 4)
        self.assertEqual(calls[0][3]["filtering"]["auth_type"], ["VIDEO_ITEM"])
        self.assertEqual(calls[0][3]["filtering"]["auth_status"], ["AUTHRIZED"])
        first = result["candidates"][0]
        self.assertEqual(first["item_id"], "8101")
        self.assertEqual(
            first["source_key"]["canonical"],
            "marketing:1234567890123456:CREATOR_AUTHORIZED:ITEM_ID:8101",
        )
        self.assertTrue(first["usable"])
        self.assertEqual(first["authorization_status"], "VALID")
        self.assertEqual(first["raw_authorization_status"], "AUTHRIZED")

    def test_authorized_video_and_cover_ids_are_opaque_strings(self):
        row = copy.deepcopy(FIXTURE["pages"][0]["data"]["list"][0])
        row["video_info"]["video_id"] = "v2800fgi0000example"
        row["video_info"]["video_cover_id"] = "tos-cn-p/example-cover"
        candidate = creator_materials.normalize_relation(
            row,
            "1234567890123456",
            now=NOW,
        )
        self.assertEqual(candidate["video_id"], "v2800fgi0000example")
        self.assertEqual(candidate["video_cover_id"], "tos-cn-p/example-cover")
        self.assertTrue(candidate["usable"])

    def test_authorized_query_locally_enforces_ignored_aweme_filter(self):
        result, _ = self.fetch_fixture()
        target = result["candidates"][0]["creator_id"]

        def fetch_json(base_url, token, path, params):
            return FIXTURE["pages"][int(params["page"]) - 1]

        filtered = creator_materials.fetch_candidates(
            fetch_json,
            "https://api.example.test/open_api",
            "test-token",
            "1234567890123456",
            aweme_ids=[target],
            now=NOW,
        )
        self.assertTrue(filtered["candidates"])
        self.assertEqual({row["creator_id"] for row in filtered["candidates"]}, {target})

    def test_homepage_query_uses_top_level_aweme_id(self):
        calls = []

        def fetch_json(base_url, token, path, params):
            calls.append((path, params))
            return {
                "code": 0,
                "request_id": "homepage-request",
                "data": {
                    "page_info": {"total_page": 1},
                    "list": [{
                        "item_id": 8101,
                        "video_id": "v-homepage",
                        "video_cover_id": "tos-cover",
                        "image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
                    }],
                },
            }

        result = creator_materials.fetch_homepage_videos(
            fetch_json,
            "https://api.example.test/open_api",
            "test-token",
            "1234567890123456",
            "creator-name-1",
        )
        self.assertEqual(calls[0][0], "/2/file/video/aweme/get/")
        self.assertEqual(calls[0][1]["aweme_id"], "creator-name-1")
        self.assertNotIn("filtering", calls[0][1])
        self.assertEqual(result["candidates"][0]["video_id"], "v-homepage")
        self.assertTrue(result["candidates"][0]["usable"])

    def test_expiring_authorization_is_not_usable(self):
        result, _ = self.fetch_fixture()
        expiring = next(row for row in result["candidates"] if row["item_id"] == "8104")
        self.assertFalse(expiring["usable"])
        self.assertIn("authorization_expires_too_soon", expiring["unusable_reasons"])

    def test_expired_authorization_is_rejected_when_no_remaining_days_are_required(self):
        row = copy.deepcopy(FIXTURE["pages"][0]["data"]["list"][0])
        row["end_time"] = "2026-07-12 23:59:59"
        candidate = creator_materials.normalize_relation(
            row,
            "1234567890123456",
            minimum_remaining_days=0,
            now=NOW,
        )
        self.assertFalse(candidate["usable"])
        self.assertIn("authorization_expired", candidate["unusable_reasons"])

    def test_one_unit_rejects_materials_from_different_creators(self):
        result, _ = self.fetch_fixture()
        with self.assertRaisesRegex(creator_materials.CreatorMaterialError, "one aweme_id"):
            creator_materials.select_candidates(result["candidates"], ["8101", "8103"])

    def test_unlimited_selection_accepts_all_selected_materials_from_one_creator(self):
        result, _ = self.fetch_fixture()
        selected = creator_materials.select_candidates(
            result["candidates"],
            ["8101", "8102"],
            max_materials=None,
        )
        self.assertEqual([candidate["item_id"] for candidate in selected], ["8101", "8102"])

    def test_selection_rejects_material_not_in_current_authorization_snapshot(self):
        result, _ = self.fetch_fixture()
        with self.assertRaises(creator_materials.CreatorMaterialError) as raised:
            creator_materials.select_candidates(result["candidates"], ["9999"])
        self.assertEqual(raised.exception.code, "creator_material_not_found")

    def test_latest_selection_keeps_one_creator_identity(self):
        result, _ = self.fetch_fixture()
        selected = creator_materials.select_latest_candidates(
            result["candidates"],
            max_materials=5,
        )
        self.assertEqual([candidate["item_id"] for candidate in selected], ["8103"])
        self.assertEqual({candidate["creator_id"] for candidate in selected}, {"7002"})

    def test_development_flow_builds_native_creator_promotion_payload(self):
        result, _ = self.fetch_fixture()
        selected = creator_materials.select_candidates(
            result["candidates"],
            ["8101", "8102"],
            max_materials=5,
        )
        project, upload_promotion = create_plan.build_payloads(payload_config(), payload_args())
        promotion = creator_materials.apply_to_promotion_payload(upload_promotion, selected)

        self.assertEqual(project["advertiser_id"], "1234567890123456")
        self.assertEqual(promotion["native_setting"], {"aweme_id": "7001"})
        self.assertEqual(
            promotion["promotion_materials"]["video_material_list"],
            [
                {
                    "image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
                    "video_id": "9101",
                    "video_cover_id": "9201",
                    "item_id": 8101,
                },
                {
                    "image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
                    "video_id": "9102",
                    "video_cover_id": "9202",
                    "item_id": 8102,
                },
            ],
        )
        self.assertEqual(
            create_plan.missing_fields(payload_config(), project, promotion, False),
            [],
        )

    def test_payload_rejects_materials_from_another_advertiser(self):
        result, _ = self.fetch_fixture()
        selected = creator_materials.select_candidates(result["candidates"], ["8101"])
        _, promotion = create_plan.build_payloads(payload_config(), payload_args())
        promotion["advertiser_id"] = "9999999999999999"
        with self.assertRaises(creator_materials.CreatorMaterialError) as raised:
            creator_materials.apply_to_promotion_payload(promotion, selected)
        self.assertEqual(raised.exception.code, "creator_material_owner_mismatch")

    def test_template_wizard_creates_creator_bound_template(self):
        config, _ = creator_template_config()
        answers = PromptAnswers({
            "请选择来源编号": "0",
            "广告主 ID": "1234567890123456",
            "平台": "示例平台",
            "流量来源": "",
            "商品名称": "新商品",
            "商品 ID": "product-2",
            "产品卖点": "新商品值得推荐",
            "日预算": "",
            "净成交 ROI 出价": "",
            "性别": "",
            "年龄": "",
            "素材来源": "2",
            "素材选择方式": "1",
            "每单元素材数量": "5",
            "达人 ID 白名单": "",
            "授权至少剩余天数": "1",
            "输入文案标题": ["这是一条达人模板文案", ""],
            "计划来源": "test source",
            "落地页链接": "https://landing.test/page",
            "直达链接": "testapp://open",
            "展示监测链接": "https://tracking.test/impression",
            "点击/有效触点监测链接": "https://tracking.test/click",
            "确认创建此业务模板": "y",
        })
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        name = "巨量营销-1234567890123456-新商品-product-2-原生素材"
        strategy = updated["plan_templates"][name]["material_strategy"]
        self.assertTrue(result["confirmed"])
        self.assertEqual(strategy["source_type"], "CREATOR_AUTHORIZED")
        self.assertEqual(strategy["selection_mode"], "MANUAL")
        self.assertEqual(strategy["creator_filters"]["auth_types"], ["VIDEO_ITEM"])
        self.assertEqual(
            updated["plan_templates"][name]["overrides"]["defaults"]["project_name_template"],
            "{material_date}_原生素材roi_详情页",
        )
        self.assertEqual(
            updated["plan_templates"][name]["overrides"]["defaults"]["promotion_name_template"],
            "自动投放单元_{product_name}_{material_date}日_原生",
        )
        self.assertNotIn("materials", updated["plan_templates"][name]["overrides"])

    def test_template_wizard_accepts_unlimited_creator_materials(self):
        config, _ = creator_template_config()
        answers = PromptAnswers({
            "请选择来源编号": "0",
            "广告主 ID": "1234567890123456",
            "平台": "示例平台",
            "流量来源": "",
            "商品名称": "不限素材商品",
            "商品 ID": "product-3",
            "产品卖点": "",
            "日预算": "",
            "净成交 ROI 出价": "",
            "性别": "",
            "年龄": "",
            "素材来源": "2",
            "素材选择方式": "1",
            "每单元素材数量": "不限",
            "达人 ID 白名单": "",
            "授权至少剩余天数": "1",
            "输入文案标题": ["这是一条不限素材模板文案", ""],
            "计划来源": "test source",
            "落地页链接": "https://landing.test/page",
            "直达链接": "testapp://open",
            "展示监测链接": "https://tracking.test/impression",
            "点击/有效触点监测链接": "https://tracking.test/click",
            "确认创建此业务模板": "y",
        })
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        name = "巨量营销-1234567890123456-不限素材商品-product-3-原生素材"
        self.assertTrue(result["confirmed"])
        self.assertIsNone(
            updated["plan_templates"][name]["material_strategy"][
                "max_materials_per_unit"
            ]
        )
        self.assertIsNone(
            plan_templates.material_strategy_error(
                updated["plan_templates"][name]["material_strategy"]
            )
        )

    def test_cross_advertiser_clone_clears_inherited_creator_ids_but_keeps_new_ids(self):
        config, source_name = creator_template_config()
        config["plan_templates"][source_name]["material_strategy"]["creator_filters"][
            "creator_ids"
        ] = ["7001"]
        name, template = template_workflow.build_template(
            config,
            {
                "advertiser_id": "2222222222222222",
                "platform": "示例平台",
                "traffic_source": "CID",
                "product_name": "test product",
                "product_id": "product-1",
                "material_source_type": "CREATOR_AUTHORIZED",
                "selection_mode": "LATEST",
                "max_materials_per_unit": 3,
                "creator_ids": ["8001"],
                "creator_auth_types": ["VIDEO_ITEM"],
                "minimum_remaining_days": 2,
            },
            source_name,
        )
        self.assertEqual(
            template["material_strategy"]["creator_filters"]["creator_ids"],
            ["8001"],
        )
        self.assertEqual(template["material_strategy"]["selection_mode"], "LATEST")
        self.assertIn(
            "material_strategy.creator_filters.creator_ids",
            template["created_from"]["cleared_fields"],
        )
        self.assertTrue(name.endswith("-原生素材"))

    def test_standard_create_entry_rejects_creator_template_before_token_use(self):
        config, name = creator_template_config()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            argv = [
                "ocean-watch plans create",
                "--config", str(path),
                "--plan-template", name,
                "--submit",
            ]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(token_manager, "ensure_access_token") as ensure_token, \
                    redirect_stdout(StringIO()):
                code = create_plan.main()
            self.assertEqual(code, 2)
            ensure_token.assert_not_called()

    def test_creator_create_entry_builds_dry_run_from_current_authorization_snapshot(self):
        config, name = creator_template_config()
        query_result, _ = self.fetch_fixture()

        def attach_token(path, runtime, **kwargs):
            updated = copy.deepcopy(runtime)
            updated.setdefault("api", {})["access_token"] = "test-token"
            return updated

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=attach_token), \
                    mock.patch.object(creator_materials, "fetch_candidates", return_value=query_result), \
                    redirect_stdout(output):
                code = create_creator_plan.main([
                    "--config", str(path),
                    "--plan-template", name,
                    "--item-id", "8101",
                    "--item-id", "8102",
                ])
        result = json.loads(output.getvalue())
        self.assertEqual(code, 0)
        self.assertEqual(result["mode"], "dry_run")
        self.assertEqual(result["promotion_payload"]["native_setting"]["aweme_id"], "7001")
        self.assertEqual(
            [row["item_id"] for row in result["promotion_payload"]["promotion_materials"]["video_material_list"]],
            [8101, 8102],
        )

    def test_query_entry_hides_unusable_candidates_by_default(self):
        config, _ = creator_template_config()
        query_result, _ = self.fetch_fixture()

        def attach_token(path, runtime, **kwargs):
            updated = copy.deepcopy(runtime)
            updated.setdefault("api", {})["access_token"] = "test-token"
            return updated

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            output = StringIO()
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=attach_token), \
                    mock.patch.object(creator_materials, "fetch_candidates", return_value=query_result), \
                    redirect_stdout(output):
                code = query_creator_materials.main(["--config", str(path)])
        result = json.loads(output.getvalue())
        self.assertEqual(code, 0)
        self.assertEqual(result["candidate_count"], 3)
        self.assertTrue(all(candidate["usable"] for candidate in result["candidates"]))

    def test_creator_submit_creates_project_before_promotion(self):
        config, name = creator_template_config()
        query_result, _ = self.fetch_fixture()
        responses = [
            {"code": 0, "data": {"project_id": "project-1"}},
            {"code": 0, "data": {"promotion_id": "promotion-1"}},
        ]

        def attach_token(path, runtime, **kwargs):
            updated = copy.deepcopy(runtime)
            updated.setdefault("api", {})["access_token"] = "test-token"
            return updated

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            client = mock.Mock()
            client.post.side_effect = responses
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=attach_token), \
                    mock.patch.object(creator_materials, "fetch_candidates", return_value=query_result), \
                    mock.patch.object(plan_executor.OceanEngineClient, "__new__", return_value=client), \
                    redirect_stdout(StringIO()):
                code = create_creator_plan.main([
                    "--config", str(path),
                    "--plan-template", name,
                    "--item-id", "8101",
                    "--submit",
                ])
        self.assertEqual(code, 0)
        self.assertEqual(client.post.call_count, 2)
        self.assertEqual(client.post.call_args_list[0].args[0], "/v3.0/project/create/")
        self.assertEqual(client.post.call_args_list[1].args[0], "/v3.0/promotion/create/")
        self.assertEqual(client.post.call_args_list[1].args[1]["project_id"], "project-1")

    def test_creator_execute_reports_project_and_promotion_progress(self):
        config, name = creator_template_config()
        query_result, _ = self.fetch_fixture()
        responses = [
            {"code": 0, "data": {"project_id": "project-1"}},
            {"code": 0, "data": {"promotion_id": "promotion-1"}},
        ]
        events = []

        def attach_token(path, runtime, **kwargs):
            updated = copy.deepcopy(runtime)
            updated.setdefault("api", {})["access_token"] = "test-token"
            return updated

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            parsed = create_creator_plan.build_parser().parse_args([
                "--config", str(path),
                "--plan-template", name,
                "--item-id", "8101",
                "--submit",
            ])
            client = mock.Mock()
            client.post.side_effect = responses
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=attach_token), \
                    mock.patch.object(creator_materials, "fetch_candidates", return_value=query_result), \
                    mock.patch.object(plan_executor.OceanEngineClient, "__new__", return_value=client):
                result, code = create_creator_plan.execute(
                    parsed,
                    progress_callback=events.append,
                )
        self.assertEqual(code, 0)
        self.assertEqual(result["promotion_response"]["data"]["promotion_id"], "promotion-1")
        self.assertEqual([event["status"] for event in events], [
            "project_created",
            "completed",
        ])
        self.assertEqual(events[0]["project_id"], "project-1")
        self.assertEqual(events[1]["promotion_id"], "promotion-1")


if __name__ == "__main__":
    unittest.main()
