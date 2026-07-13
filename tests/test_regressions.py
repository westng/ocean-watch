import copy
import json
import os
import sys
import tempfile
import time
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "ads-plan-monitor"
SCRIPTS = SKILL / "scripts"
sys.path.insert(0, str(SCRIPTS))

import batch_create_from_today_videos
import config_store
import config_paths
import configure_official_mcp
import create_plan
import credential_store
import first_run
import oceanengine_mcp_bridge
import manage_plan_templates
import plan_templates
import template_workflow
import token_manager
import validate_config


def valid_config():
    return {
        "api": {
            "base_url": "https://api.oceanengine.com/open_api",
            "access_token": "test-access-token",
        },
        "account": {"advertiser_id": 1234567890},
        "defaults": {
            "operation": "ENABLE",
            "project_name_template": "project_{material_date}_{suffix}",
            "promotion_name_template": "promotion_{material_date}_{suffix}",
            "product_name": "test product",
            "product_id": "product-1",
            "daily_budget": 300,
            "cpa_bid": 100,
            "roi_goal": 1.5,
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
            "video_image_mode": "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL",
            "product_info": {"product_image_type": "CUSTOM"},
        },
        "materials": {"video_ids": ["video-1"], "video_cover_ids": []},
        "resolved_ids": {
            "city_ids": [1],
            "unique_product_id": "unique-product-1",
            "product_platform_id": None,
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
        "titles": ["test title"],
    }


def args(**overrides):
    values = {
        "advertiser_id": None,
        "budget": None,
        "bid": None,
        "roi_goal": None,
        "video_id": None,
        "material_date": "7.10",
        "product_name": None,
        "product_id": None,
        "project_name": None,
        "promotion_name": None,
        "project_id": None,
    }
    values.update(overrides)
    return SimpleNamespace(**values)


class CreatePlanTests(unittest.TestCase):
    def v2_config(self):
        config = valid_config()
        migrated = plan_templates.migrate(config)
        name = "平台-CID-商品-product-1"
        migrated["plan_templates"] = {
            name: {
                "display_name": name,
                "bindings": {
                    "advertiser_id": "1234567890",
                    "platform": "平台",
                    "traffic_source": "CID",
                    "product_id": "unique-product-1",
                    "product_name": "test product",
                },
                "overrides": {},
            }
        }
        migrated["active_plan_template"] = name
        return migrated

    def test_single_create_resolves_suffix(self):
        project, promotion = create_plan.build_payloads(valid_config(), args())
        self.assertNotIn("{suffix}", project["name"])
        self.assertNotIn("{suffix}", promotion["name"])
        self.assertTrue(promotion["name"].endswith("_01"))

    def test_example_links_block_submission(self):
        config = valid_config()
        config["links"]["landing_page_url"] = "https://example.com/landing"
        project, promotion = create_plan.build_payloads(config, args())
        missing = create_plan.missing_fields(config, project, promotion, True)
        self.assertIn("links.landing_page_url", missing)

    def test_double_brace_and_todo_values_are_unresolved(self):
        self.assertTrue(create_plan.contains_unresolved_value("https://test.invalid/{{click_id}}"))
        self.assertTrue(create_plan.contains_unresolved_value("https://test.invalid/TODO"))

    def test_unique_product_does_not_require_platform_id(self):
        config = valid_config()
        project, promotion = create_plan.build_payloads(config, args())
        missing = create_plan.missing_fields(config, project, promotion, True)
        self.assertNotIn("resolved_ids.product_platform_id", missing)

    def test_roi_goal_override(self):
        project, _ = create_plan.build_payloads(valid_config(), args(roi_goal=2.25))
        self.assertEqual(project["delivery_setting"]["roi_goal"], 2.25)

    def test_v2_template_binds_advertiser(self):
        effective = plan_templates.apply(self.v2_config(), advertiser_id=1234567890)
        self.assertEqual(effective["account"]["advertiser_id"], "1234567890")
        self.assertEqual(
            effective["_selected_plan_template"]["bindings"]["advertiser_id"],
            "1234567890",
        )

    def test_v2_template_rejects_other_advertiser(self):
        with self.assertRaisesRegex(ValueError, "bound to advertiser 1234567890"):
            plan_templates.apply(self.v2_config(), advertiser_id=999)

    def test_submit_rejects_other_advertiser_before_token_refresh(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(self.v2_config()), encoding="utf-8")
            argv = [
                "create_plan.py",
                "--config", str(config_path),
                "--advertiser-id", "999",
                "--submit",
            ]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(token_manager, "ensure_access_token") as ensure_token, \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main(), 2)
            ensure_token.assert_not_called()

    def test_v2_default_template_cannot_create_directly(self):
        config = self.v2_config()
        config["active_plan_template"] = None
        with self.assertRaisesRegex(ValueError, "default template cannot create plans"):
            plan_templates.apply(config)

    def test_v2_template_requires_all_bindings(self):
        config = self.v2_config()
        del config["plan_templates"][config["active_plan_template"]]["bindings"]["platform"]
        with self.assertRaisesRegex(ValueError, "template bindings missing: platform"):
            plan_templates.apply(config)

    def test_template_migration_preserves_payloads(self):
        config = valid_config()
        name = "平台-CID-商品-unique-product-1"
        config["active_plan_template"] = name
        config["plan_templates"] = {
            name: {
                "display_name": name,
                "platform": "平台",
                "traffic_source": "CID",
                "product_label": "test product",
                "product_id": "unique-product-1",
                **plan_templates.section_bundle(config),
            }
        }
        before = create_plan.build_payloads(create_plan.apply_plan_template(config), args())
        migrated = plan_templates.migrate(config)
        after = create_plan.build_payloads(create_plan.apply_plan_template(migrated), args())
        self.assertEqual(before, after)
        self.assertEqual(migrated["default_plan_template"]["materials"], {})
        self.assertEqual(migrated["default_plan_template"]["links"], {})
        self.assertEqual(migrated["default_plan_template"]["tracking_urls"], {})
        self.assertEqual(migrated["default_plan_template"]["titles"], [])
        self.assertNotIn(
            "unique_product_id",
            migrated["default_plan_template"]["resolved_ids"],
        )

    def test_create_template_records_advertiser_binding(self):
        config = plan_templates.migrate(valid_config())
        arguments = SimpleNamespace(
            advertiser_id="456",
            platform="天猫",
            traffic_source="CID",
            product_id="product-2",
            product_name="新商品",
            name=None,
            source_name=None,
            landing_page_url=None,
            open_url=None,
            track_url="https://tracking.test/new-impression",
            action_track_url="https://tracking.test/new-click",
            title=["第一条测试文案", "第二条测试文案", "第一条测试文案"],
            from_template=None,
            activate=False,
            force=False,
        )
        updated, name = manage_plan_templates.create_template(config, arguments)
        template = updated["plan_templates"][name]
        self.assertEqual(template["bindings"]["advertiser_id"], "456")
        self.assertEqual(template["bindings"]["product_id"], "product-2")
        self.assertEqual(
            template["overrides"]["resolved_ids"]["unique_product_id"],
            "product-2",
        )
        self.assertEqual(
            template["overrides"]["tracking_urls"]["track_url"],
            ["https://tracking.test/new-impression"],
        )
        self.assertEqual(
            template["overrides"]["tracking_urls"]["action_track_url"],
            ["https://tracking.test/new-click"],
        )
        self.assertEqual(
            template["copy_materials"]["titles"],
            ["第一条测试文案", "第二条测试文案"],
        )

    def test_template_list_exposes_advertiser_as_primary_field(self):
        config = self.v2_config()
        row = manage_plan_templates.list_templates(config)[0]
        self.assertEqual(row["advertiser_id"], "1234567890")
        self.assertEqual(row["platform"], "平台")
        self.assertEqual(row["product_id"], "unique-product-1")

    def test_cross_advertiser_template_clone_is_rejected(self):
        config = self.v2_config()
        arguments = SimpleNamespace(
            advertiser_id="456",
            platform="京东",
            traffic_source="CID",
            product_id="product-2",
            product_name="new product",
            name=None,
            source_name=None,
            landing_page_url=None,
            open_url=None,
            track_url=None,
            action_track_url=None,
            title=None,
            from_template=config["active_plan_template"],
            activate=False,
            force=False,
        )
        with self.assertRaisesRegex(ValueError, "cross-advertiser template cloning"):
            manage_plan_templates.create_template(config, arguments)

    def test_set_copy_materials_updates_business_template(self):
        config = self.v2_config()
        name = config["active_plan_template"]
        updated = manage_plan_templates.set_copy_materials(
            config,
            name,
            ["第一条文案", "第二条文案", "第一条文案"],
        )
        self.assertEqual(
            updated["plan_templates"][name]["copy_materials"]["titles"],
            ["第一条文案", "第二条文案"],
        )
        row = manage_plan_templates.list_templates(updated)[0]
        self.assertEqual(row["copy_materials"]["title_count"], 2)

    def test_copy_materials_can_copy_across_advertisers(self):
        config = self.v2_config()
        source_name = config["active_plan_template"]
        config["plan_templates"][source_name]["copy_materials"] = {
            "titles": ["第一条来源文案", "第二条来源文案"],
        }
        target_name = "京东-CID-同商品-product-1"
        config["plan_templates"][target_name] = {
            "display_name": target_name,
            "bindings": {
                "advertiser_id": "456",
                "platform": "京东",
                "traffic_source": "CID",
                "product_id": "product-1",
                "product_name": "同商品",
            },
            "copy_materials": {"titles": []},
            "overrides": {},
        }
        updated = manage_plan_templates.set_copy_materials(
            config,
            target_name,
            from_template=source_name,
        )
        copied = updated["plan_templates"][target_name]["copy_materials"]
        self.assertEqual(copied["titles"], ["第一条来源文案", "第二条来源文案"])
        self.assertEqual(copied["copied_from_template"], source_name)
        self.assertEqual(
            updated["plan_templates"][target_name]["bindings"]["advertiser_id"],
            "456",
        )

    def test_default_template_is_listed_as_non_business_base(self):
        summary = manage_plan_templates.default_template_summary(self.v2_config())
        self.assertEqual(summary["name"], "default_plan_template")
        self.assertFalse(summary["business_usable"])
        self.assertFalse(summary["selectable_for_plan_creation"])

    def test_template_source_options_show_business_bindings(self):
        config = self.v2_config()
        output = []
        selected = manage_plan_templates.select_template_source(
            config,
            input_fn=lambda _: "0",
            output_fn=output.append,
        )
        rendered = "\n".join(output)
        self.assertIsNone(selected)
        self.assertIn("广告主 1234567890", rendered)
        self.assertIn("平台 平台", rendered)
        self.assertIn("商品 test product", rendered)
        self.assertIn("商品 ID unique-product-1", rendered)

    def test_create_wizard_from_default_requires_confirmation(self):
        config = self.v2_config()
        original = copy.deepcopy(config)
        answers = iter([
            "0",
            "456",
            "京东",
            "",
            "新商品",
            "product-2",
            "",
            "第一条新文案",
            "",
            "新来源",
            "https://landing.test/new",
            "testapp://new",
            "https://tracking.test/new-impression",
            "https://tracking.test/new-click",
            "n",
        ])
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        self.assertEqual(updated, original)
        self.assertFalse(result["confirmed"])
        self.assertFalse(result["changed"])

    def test_create_wizard_clones_business_template_and_clears_account_assets(self):
        config = self.v2_config()
        source_name = config["active_plan_template"]
        source = config["plan_templates"][source_name]
        source["copy_materials"] = {"titles": ["来源文案"]}
        source["overrides"] = {
            "defaults": {"source": "来源渠道"},
            "materials": {"video_ids": ["source-video"]},
            "resolved_ids": {
                "unique_product_id": "unique-product-1",
                "city_ids": [1],
                "event_asset_ids": [100],
                "product_image_ids": ["source-image"],
            },
            "links": {
                "landing_page_url": "https://landing.test/source",
                "open_url": "testapp://source",
            },
            "tracking_urls": {
                "track_url": ["https://tracking.test/source-impression"],
                "action_track_url": ["https://tracking.test/source-click"],
            },
        }
        answers = iter([
            "1",
            "456",
            "京东",
            "",
            "同款商品",
            "product-2",
            "",
            "",
            "",
            "",
            "",
            "",
            "",
            "y",
            "n",
        ])
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        name = "京东-CID-同款商品-product-2"
        template = updated["plan_templates"][name]
        self.assertTrue(result["confirmed"])
        self.assertFalse(result["activate"])
        self.assertEqual(template["copy_materials"]["titles"], [])
        self.assertEqual(template["overrides"]["materials"], {})
        self.assertNotIn("event_asset_ids", template["overrides"]["resolved_ids"])
        self.assertNotIn("product_image_ids", template["overrides"]["resolved_ids"])
        self.assertEqual(template["overrides"]["resolved_ids"]["city_ids"], [1])
        self.assertEqual(template["created_from"]["template"], source_name)
        self.assertEqual(
            template["created_from"]["policy"],
            "cross_advertiser_new_product",
        )
        self.assertIn(
            "resolved_ids.event_asset_ids",
            template["created_from"]["cleared_fields"],
        )
        self.assertEqual(config["active_plan_template"], source_name)

    def test_same_advertiser_new_product_clears_product_assets_and_copy(self):
        config = self.v2_config()
        source_name = config["active_plan_template"]
        source = plan_templates.normalize_template(
            config,
            source_name,
            config["plan_templates"][source_name],
        )
        source["copy_materials"] = {"titles": ["这是来源商品文案"]}
        source["overrides"] = {
            "resolved_ids": {
                "unique_product_id": "unique-product-1",
                "product_image_ids": ["old-image"],
                "product_platform_id": "old-platform-product",
            },
            "links": {"landing_page_url": "https://old.test/product"},
        }
        config["plan_templates"][source_name] = source
        values = {
            "advertiser_id": "1234567890",
            "platform": "平台",
            "traffic_source": "CID",
            "product_id": "product-2",
            "product_name": "新商品",
            "name": None,
            "titles": None,
        }
        name, template = template_workflow.build_template(config, values, source_name)
        self.assertEqual(name, "平台-CID-新商品-product-2")
        self.assertEqual(template["copy_materials"]["titles"], [])
        self.assertNotIn("product_image_ids", template["overrides"]["resolved_ids"])
        self.assertNotIn("product_platform_id", template["overrides"]["resolved_ids"])
        self.assertNotIn("landing_page_url", template["overrides"].get("links", {}))
        self.assertEqual(template["created_from"]["policy"], "same_advertiser_new_product")

    def test_cross_advertiser_same_product_keeps_copy_but_clears_links(self):
        config = self.v2_config()
        source_name = config["active_plan_template"]
        source = config["plan_templates"][source_name]
        source["copy_materials"] = {"titles": ["这是同款商品文案"]}
        source["overrides"] = {
            "links": {"landing_page_url": "https://old.test/product"},
            "tracking_urls": {"track_url": ["https://old.test/impression"]},
        }
        values = {
            "advertiser_id": "456",
            "platform": "平台",
            "traffic_source": "CID",
            "product_id": "unique-product-1",
            "product_name": "test product",
            "name": None,
            "titles": None,
        }
        _, template = template_workflow.build_template(config, values, source_name)
        self.assertEqual(template["copy_materials"]["titles"], ["这是同款商品文案"])
        self.assertEqual(template["overrides"].get("links"), {})
        self.assertEqual(template["overrides"].get("tracking_urls"), {})
        self.assertEqual(template["created_from"]["policy"], "cross_advertiser_same_product")

    def test_copy_title_length_is_validated_locally(self):
        with self.assertRaisesRegex(ValueError, "5-30"):
            template_workflow.normalize_titles(["太短"])
        with self.assertRaisesRegex(ValueError, "5-30"):
            template_workflow.normalize_titles(["超" * 31])

    def test_candidate_validation_reports_invalid_inherited_copy(self):
        config = self.v2_config()
        name = config["active_plan_template"]
        template = copy.deepcopy(config["plan_templates"][name])
        template["copy_materials"] = {"titles": ["太短"]}
        result = template_workflow.validate_candidate(config, name, template)
        self.assertFalse(result["ready_for_plan_creation"])
        self.assertTrue(
            any(field.startswith("copy_materials.titles:") for field in result["template_missing_fields"])
        )

    def test_incomplete_wizard_draft_cannot_activate(self):
        config = self.v2_config()
        answers = iter([
            "0",
            "456",
            "京东",
            "",
            "新商品",
            "product-2",
            "",
            "",
            "",
            "",
            "",
            "",
            "",
            "y",
        ])
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        self.assertTrue(result["confirmed"])
        self.assertFalse(result["activate"])
        self.assertFalse(result["validation"]["ready_for_plan_creation"])
        self.assertEqual(updated["active_plan_template"], config["active_plan_template"])

    def test_direct_create_cannot_activate_incomplete_template(self):
        config = self.v2_config()
        original = copy.deepcopy(config)
        arguments = SimpleNamespace(
            advertiser_id="456",
            platform="京东",
            traffic_source="CID",
            product_id="product-2",
            product_name="新商品",
            name=None,
            source_name=None,
            landing_page_url=None,
            open_url=None,
            track_url=None,
            action_track_url=None,
            title=["这是一条有效测试文案"],
            from_template=None,
            activate=True,
            force=False,
        )
        with self.assertRaisesRegex(ValueError, "incomplete plan template cannot be activated"):
            manage_plan_templates.create_template(config, arguments)
        self.assertEqual(config, original)

    def test_wizard_preview_contains_field_level_changes(self):
        config = self.v2_config()
        answers = iter([
            "0",
            "456",
            "京东",
            "",
            "新商品",
            "product-2",
            "",
            "这是一条有效测试文案",
            "",
            "新来源",
            "https://landing.test/new",
            "testapp://new",
            "https://tracking.test/new-impression",
            "https://tracking.test/new-click",
            "n",
        ])
        _, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=lambda _: next(answers),
            output_fn=lambda _: None,
        )
        changed_fields = {change["field"] for change in result["changes"]}
        self.assertIn("bindings.advertiser_id", changed_fields)
        self.assertIn("overrides.links.landing_page_url", changed_fields)

    def test_failed_project_submission_returns_nonzero(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(valid_config()), encoding="utf-8")
            argv = ["create_plan.py", "--config", str(config_path), "--submit"]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(token_manager, "ensure_access_token", side_effect=lambda path, config: config), \
                    mock.patch.object(create_plan, "post_json", return_value={"code": 500}), \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main(), 1)

    def test_failed_promotion_submission_returns_nonzero(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(valid_config()), encoding="utf-8")
            responses = [
                {"data": {"project_id": 42}},
                {"code": 500},
            ]
            argv = ["create_plan.py", "--config", str(config_path), "--submit"]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(token_manager, "ensure_access_token", side_effect=lambda path, config: config), \
                    mock.patch.object(create_plan, "post_json", side_effect=responses), \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main(), 1)


class TemplateBatchMappingTests(unittest.TestCase):
    def config_with_two_accounts(self):
        config = CreatePlanTests().v2_config()
        second = "京东-CID-商品-product-2"
        config["plan_templates"][second] = {
            "display_name": second,
            "bindings": {
                "advertiser_id": "456",
                "platform": "京东",
                "traffic_source": "CID",
                "product_id": "product-2",
                "product_name": "商品",
            },
            "copy_materials": {"titles": ["这是第二账户文案"]},
            "overrides": {"resolved_ids": {"unique_product_id": "product-2"}},
        }
        return config, second

    def test_multi_account_jobs_use_bound_templates(self):
        config, second = self.config_with_two_accounts()
        first = config["active_plan_template"]
        jobs = batch_create_from_today_videos.resolve_account_jobs(
            config,
            ["1234567890,456"],
            [f"1234567890={first}", f"456={second}"],
        )
        self.assertEqual(jobs, [
            {"advertiser_id": "1234567890", "plan_template": first},
            {"advertiser_id": "456", "plan_template": second},
        ])

    def test_multi_account_single_template_is_rejected(self):
        config, _ = self.config_with_two_accounts()
        with self.assertRaisesRegex(ValueError, "one account"):
            batch_create_from_today_videos.resolve_account_jobs(
                config,
                ["1234567890,456"],
                None,
                fallback_template=config["active_plan_template"],
            )

    def test_ambiguous_account_requires_explicit_mapping(self):
        config, _ = self.config_with_two_accounts()
        duplicate = copy.deepcopy(config["plan_templates"][config["active_plan_template"]])
        config["plan_templates"]["duplicate"] = duplicate
        with self.assertRaisesRegex(ValueError, "explicit template mapping"):
            batch_create_from_today_videos.resolve_account_jobs(
                config,
                ["1234567890"],
                None,
            )


class ConfigStoreTests(unittest.TestCase):
    def test_atomic_write_replaces_json_and_keeps_backup(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text('{"version": 1}\n', encoding="utf-8")
            config_store.atomic_write_json(path, {"version": 2})
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), {"version": 2})
            self.assertEqual(
                json.loads(path.with_suffix(".json.bak").read_text(encoding="utf-8")),
                {"version": 1},
            )
            self.assertEqual(list(path.parent.glob(".config.json.*.tmp")), [])


class ConfigAndCredentialTests(unittest.TestCase):
    def test_repository_config_resolves_from_plugin_checkout(self):
        self.assertEqual(
            config_paths.project_config_path(),
            ROOT / "config" / "ads-plan-monitor" / "config.json",
        )

    def test_installed_skill_without_git_uses_home_config(self):
        with tempfile.TemporaryDirectory() as directory:
            start = Path(directory) / "skills" / "ads-plan-monitor" / "scripts"
            start.mkdir(parents=True)
            with mock.patch.object(config_paths, "repository_root", return_value=None):
                self.assertEqual(
                    config_paths.resolve_config_path(),
                    config_paths.home_config_path(),
                )

    def test_environment_config_precedence(self):
        with mock.patch.dict(os.environ, {config_paths.CONFIG_ENV: "~/env-config.json"}, clear=False):
            self.assertEqual(config_paths.resolve_config_path(), Path("~/env-config.json").expanduser())
            self.assertEqual(config_paths.resolve_config_path("explicit.json"), Path("explicit.json"))

    def test_plaintext_fallback_requires_opt_in(self):
        with mock.patch.object(credential_store.platform, "system", return_value="Linux"), \
                mock.patch.object(credential_store, "has_command", return_value=False), \
                mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(credential_store.backend_name(), "unavailable")
        with mock.patch.object(credential_store.platform, "system", return_value="Linux"), \
                mock.patch.object(credential_store, "has_command", return_value=False), \
                mock.patch.dict(os.environ, {credential_store.INSECURE_FALLBACK_ENV: "1"}, clear=True):
            self.assertEqual(credential_store.backend_name(), "file-fallback")

    def test_sensitive_fields_are_removed(self):
        config = {"api": {"access_token": "a", "refresh_token": "r", "secret": "s", "base_url": "u"}}
        cleaned = credential_store.strip_sensitive_config(config)
        self.assertEqual(cleaned["api"], {"base_url": "u"})

    def test_developer_id_is_not_merged_into_business_api_config(self):
        merged = credential_store.merge_credentials(
            {"api": {"base_url": "https://example.com"}},
            {"access_token": "token", "developer_id": "developer-1"},
        )
        self.assertEqual(merged["api"]["access_token"], "token")
        self.assertNotIn("developer_id", merged["api"])

    def test_macos_hex_encoded_credentials_are_decoded(self):
        data = {"access_token": "token", "refresh_token": "refresh"}
        encoded = json.dumps(data).encode("utf-8").hex()
        self.assertEqual(credential_store.decode_stored_credentials(encoded), data)

    def test_invalid_stored_credentials_raise_clear_error(self):
        with self.assertRaisesRegex(RuntimeError, "not valid JSON"):
            credential_store.decode_stored_credentials("not-json")


class OfficialMcpTests(unittest.TestCase):
    def bridge_server(self):
        return {
            "transport": {
                "type": "stdio",
                "command": sys.executable,
                "args": [str(configure_official_mcp.bridge_path())],
            }
        }

    def test_bridge_url_encodes_ids(self):
        url = oceanengine_mcp_bridge.build_url("app id", "developer/id")
        self.assertIn("app_id=app+id", url)
        self.assertIn("developer_id=developer%2Fid", url)

    def test_bridge_rejects_untrusted_message_endpoint(self):
        with self.assertRaisesRegex(RuntimeError, "untrusted"):
            oceanengine_mcp_bridge.validated_message_endpoint(
                "https://open.oceanengine.com/sse", "https://example.com/messages"
            )

    def test_status_is_redacted(self):
        with mock.patch.object(
            configure_official_mcp,
            "get_server",
            return_value=self.bridge_server(),
        ), mock.patch.object(configure_official_mcp.shutil, "which", return_value="codex"):
            result = configure_official_mcp.status(
                {"app_id": "app-1", "developer_id": "developer-1"}
            )
        self.assertTrue(result["ready"])
        self.assertNotIn("app-1", json.dumps(result))
        self.assertNotIn("developer-1", json.dumps(result))

    def test_configure_registers_stdio_bridge(self):
        credentials = {"app_id": "app-1"}
        calls = []

        def fake_run(arguments, check=False):
            calls.append(arguments)
            return SimpleNamespace(returncode=0, stdout="{}", stderr="")

        with mock.patch.object(credential_store, "read_credentials", return_value=credentials), \
                mock.patch.object(credential_store, "configure_developer_id"), \
                mock.patch.object(oceanengine_mcp_bridge, "probe", return_value=["tool-1"]), \
                mock.patch.object(configure_official_mcp, "get_server", side_effect=[None, self.bridge_server()]), \
                mock.patch.object(configure_official_mcp, "run_codex", side_effect=fake_run), \
                mock.patch.object(configure_official_mcp.shutil, "which", return_value="codex"):
            result = configure_official_mcp.configure("developer-1")

        self.assertTrue(result["ready"])
        self.assertEqual(result["verified_tool_count"], 1)
        self.assertEqual(calls[0][:4], ["mcp", "add", configure_official_mcp.SERVER_NAME, "--"])
        self.assertEqual(Path(calls[0][4]).resolve(), Path(sys.executable).resolve())
        self.assertEqual(Path(calls[0][5]).resolve(), configure_official_mcp.bridge_path())
        self.assertNotIn("app-1", json.dumps(calls))
        self.assertNotIn("developer-1", json.dumps(calls))

    def test_failed_registration_does_not_store_developer_id(self):
        with mock.patch.object(
            credential_store,
            "read_credentials",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "configure_developer_id",
        ) as save, mock.patch.object(
            configure_official_mcp,
            "get_server",
            return_value=None,
        ), mock.patch.object(
            oceanengine_mcp_bridge,
            "probe",
            return_value=["tool-1"],
        ), mock.patch.object(
            configure_official_mcp,
            "run_codex",
            return_value=SimpleNamespace(returncode=1, stdout="", stderr="failed"),
        ):
            with self.assertRaisesRegex(RuntimeError, "Unable to register"):
                configure_official_mcp.configure("developer-1")
        save.assert_not_called()

    def test_failed_probe_does_not_change_local_state(self):
        with mock.patch.object(
            credential_store,
            "read_credentials",
            return_value={"app_id": "app-1"},
        ), mock.patch.object(
            credential_store,
            "configure_developer_id",
        ) as save, mock.patch.object(
            oceanengine_mcp_bridge,
            "probe",
            side_effect=RuntimeError("Official MCP rejected the configured developer credentials"),
        ), mock.patch.object(
            configure_official_mcp,
            "run_codex",
        ) as run_codex:
            with self.assertRaisesRegex(RuntimeError, "rejected"):
                configure_official_mcp.configure("developer-1")
        save.assert_not_called()
        run_codex.assert_not_called()


class FileLockTests(unittest.TestCase):
    def test_stale_pid_lock_is_recovered(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token.lock"
            path.write_text("99999999", encoding="utf-8")
            with token_manager.FileLock(path, timeout=0.1):
                self.assertEqual(path.read_text(encoding="utf-8"), str(os.getpid()))
            self.assertFalse(path.exists())

    def test_live_pid_lock_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token.lock"
            path.write_text(str(os.getpid()), encoding="utf-8")
            lock = token_manager.FileLock(path, timeout=0)
            self.assertFalse(lock.remove_if_stale())
            self.assertTrue(path.exists())

    def test_recent_incomplete_lock_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token.lock"
            path.write_text("", encoding="utf-8")
            lock = token_manager.FileLock(path, timeout=0)
            self.assertFalse(lock.remove_if_stale())


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
        expansion = {
            "candidate_advertiser_count": 1,
            "verified_advertiser_count": 1,
            "role_counts": {"ADVERTISER": 2, "CUSTOMER_ADMIN": 1},
            "branch_error_count": 0,
            "unsupported_role_count": 0,
            "verification_error_count": 0,
        }
        with mock.patch.object(token_manager, "get_json", return_value=response), \
                mock.patch.object(token_manager, "expand_authorized_advertisers", return_value=([1], expansion)):
            updated, summary = token_manager.update_authorized_accounts(config)
        self.assertEqual(updated["api"]["authorized_advertiser_ids"], [1])
        self.assertEqual(len(updated["api"]["oauth_authorized_accounts"]), 3)
        self.assertNotIn("company_list", updated["api"]["oauth_authorized_accounts"][0])
        self.assertEqual(summary["oauth_authorized_account_count"], 3)
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


class ExitAndValidationTests(unittest.TestCase):
    def test_no_qualified_videos_is_failure(self):
        self.assertEqual(batch_create_from_today_videos.batch_exit_code([{"status": "no_qualified_videos"}]), 1)
        self.assertEqual(batch_create_from_today_videos.batch_exit_code([{"status": "completed"}]), 0)

    def test_validator_modes_are_independent(self):
        config = valid_config()
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(config, credentials)
        self.assertTrue(validate_config.mode_is_ready(result, "all"))

        incomplete = copy.deepcopy(config)
        incomplete["links"]["open_url"] = "https://example.com/open"
        result = validate_config.validate_config(incomplete, credentials)
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-submit"))
        self.assertFalse(validate_config.mode_is_ready(result, "all"))

    def test_v2_without_business_template_keeps_query_ready(self):
        config = plan_templates.migrate(valid_config())
        config["active_plan_template"] = None
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(config, credentials)
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-preview"))
        self.assertIn("default template cannot create plans", result["plan_template_error"])

    def test_v2_mismatched_template_keeps_query_ready(self):
        config = CreatePlanTests().v2_config()
        config["account"]["advertiser_id"] = 999
        credentials = {
            "app_id": "app",
            "secret": "secret",
            "access_token": "token",
        }
        result = validate_config.validate_config(config, credentials)
        self.assertTrue(validate_config.mode_is_ready(result, "query"))
        self.assertFalse(validate_config.mode_is_ready(result, "create-submit"))
        self.assertIn("bound to advertiser 1234567890", result["plan_template_error"])

    def test_first_run_uses_same_unique_product_rule(self):
        config = valid_config()
        credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
        with mock.patch.object(credential_store, "read_credentials", return_value=credentials):
            _, create_missing, _ = first_run.check_fields(config)
        self.assertNotIn("resolved_ids.product_platform_id", create_missing)

    def test_first_run_blocks_unknown_template(self):
        config = valid_config()
        config["active_plan_template"] = "missing-template"
        credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
        with mock.patch.object(credential_store, "read_credentials", return_value=credentials):
            _, create_missing, _ = first_run.check_fields(config)
        self.assertTrue(any(item.startswith("plan template:") for item in create_missing))

    def test_first_run_distinguishes_missing_and_incomplete_templates(self):
        self.assertEqual(
            first_run.next_action(["account.advertiser_id"], None, []),
            "edit_config",
        )
        self.assertEqual(
            first_run.next_action([], None, ["links.landing_page_url"]),
            "create_business_template",
        )
        self.assertEqual(
            first_run.next_action([], {"name": "draft"}, ["links.landing_page_url"]),
            "complete_active_template",
        )
        self.assertEqual(
            first_run.next_action([], {"name": "ready"}, []),
            "ready",
        )

    def test_validator_main_returns_selected_mode_status(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = valid_config()
            config["links"]["open_url"] = "https://example.com/open"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            credentials = {"app_id": "app", "secret": "secret", "access_token": "token"}
            with mock.patch.object(credential_store, "read_credentials", return_value=credentials), \
                    mock.patch.object(credential_store, "status", return_value={}), \
                    redirect_stdout(StringIO()):
                self.assertEqual(validate_config.main([str(config_path), "--mode", "query"]), 0)
                self.assertEqual(validate_config.main([str(config_path), "--mode", "create-submit"]), 1)


if __name__ == "__main__":
    unittest.main()
