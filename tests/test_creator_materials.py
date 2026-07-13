import datetime as dt
import copy
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "skills" / "ads-plan-monitor" / "scripts"
sys.path.insert(0, str(SCRIPTS))

import create_plan
import create_creator_plan
import creator_materials
import manage_plan_templates
import plan_templates
import query_creator_materials
import template_workflow
import token_manager


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
    name = "示例平台-CID-示例商品-product-1-达人素材"
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
    config["active_plan_template"] = name
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
        answers = iter([
            "0",
            "1234567890123456",
            "示例平台",
            "",
            "新商品",
            "product-2",
            "2",
            "1",
            "5",
            "",
            "1",
            "",
            "这是一条达人模板文案",
            "",
            "test source",
            "https://landing.test/page",
            "testapp://open",
            "https://tracking.test/impression",
            "https://tracking.test/click",
            "y",
            "n",
        ])
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        name = "示例平台-CID-新商品-product-2-达人素材"
        strategy = updated["plan_templates"][name]["material_strategy"]
        self.assertTrue(result["confirmed"])
        self.assertEqual(strategy["source_type"], "CREATOR_AUTHORIZED")
        self.assertEqual(strategy["selection_mode"], "MANUAL")
        self.assertEqual(strategy["creator_filters"]["auth_types"], ["VIDEO_ITEM"])
        self.assertNotIn("materials", updated["plan_templates"][name]["overrides"])

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
        self.assertTrue(name.endswith("-达人素材"))

    def test_standard_create_entry_rejects_creator_template_before_token_use(self):
        config, _ = creator_template_config()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            argv = [
                "create_plan.py",
                "--config", str(path),
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
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=attach_token), \
                    mock.patch.object(creator_materials, "fetch_candidates", return_value=query_result), \
                    mock.patch.object(create_plan, "post_json", side_effect=responses) as post, \
                    redirect_stdout(StringIO()):
                code = create_creator_plan.main([
                    "--config", str(path),
                    "--plan-template", name,
                    "--item-id", "8101",
                    "--submit",
                ])
        self.assertEqual(code, 0)
        self.assertEqual(post.call_count, 2)
        self.assertEqual(post.call_args_list[0].args[2], "/v3.0/project/create/")
        self.assertEqual(post.call_args_list[1].args[2], "/v3.0/promotion/create/")
        self.assertEqual(post.call_args_list[1].args[3]["project_id"], "project-1")


if __name__ == "__main__":
    unittest.main()
