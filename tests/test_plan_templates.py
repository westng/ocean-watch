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

from ocean_watch.auth import (
    channels,
    token_manager,
)
from ocean_watch.plans import batch_create_from_today_videos, create_plan
from ocean_watch.plans import executor as plan_executor
from ocean_watch.templates import manage_plan_templates, plan_templates, template_workflow

from tests.support import (
    PromptAnswers,
    business_template_config,
    only_plan_template_name,
    valid_config,
)
from tests.support import payload_args as args


class CreatePlanTests(unittest.TestCase):

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

    def test_dpa_product_image_uses_product_fields_without_image_ids(self):
        config = valid_config()
        config["defaults"]["product_info"] = {
            "product_name_type": "CUSTOM",
            "product_image_type": "DPA",
            "product_image_fields": ["images_url"],
            "product_selling_point_type": "CUSTOM",
        }
        config["resolved_ids"].pop("product_image_ids")
        project, promotion = create_plan.build_payloads(config, args())
        product_info = promotion["promotion_materials"]["product_info"]
        self.assertEqual(product_info["product_image_fields"], ["images_url"])
        self.assertNotIn("image_ids", product_info)
        missing = create_plan.missing_fields(config, project, promotion, False)
        self.assertNotIn("resolved_ids.product_image_ids", missing)

    def test_decimal_unique_product_id_is_serialized_as_integer(self):
        config = valid_config()
        config["resolved_ids"]["unique_product_id"] = "9007199254740993"
        project, promotion = create_plan.build_payloads(config, args())
        self.assertEqual(
            project["related_product"]["unique_product_id"],
            9007199254740993,
        )
        self.assertEqual(
            promotion["promotion_related_product"][0]["unique_product_id"],
            9007199254740993,
        )

    def test_custom_product_selling_points_are_used(self):
        config = valid_config()
        config["defaults"]["product_info"]["selling_points"] = ["测试商品推荐"]
        _, promotion = create_plan.build_payloads(config, args())
        self.assertEqual(
            promotion["promotion_materials"]["product_info"]["selling_points"],
            ["测试商品推荐"],
        )

    def test_product_selling_point_length_is_validated(self):
        config = valid_config()
        config["defaults"]["product_info"].update({
            "product_selling_point_type": "CUSTOM",
            "selling_points": ["太短"],
        })
        project, promotion = create_plan.build_payloads(config, args())
        self.assertIn(
            "defaults.product_info.selling_points",
            create_plan.missing_fields(config, project, promotion, False),
        )

    def test_roi_goal_override(self):
        project, _ = create_plan.build_payloads(valid_config(), args(roi_goal=2.25))
        self.assertEqual(project["delivery_setting"]["roi_goal"], 2.25)

    def test_v2_template_binds_advertiser(self):
        config = business_template_config()
        effective = plan_templates.apply(
            config,
            only_plan_template_name(config),
            advertiser_id=1234567890,
        )
        self.assertEqual(effective["account"]["advertiser_id"], "1234567890")
        self.assertEqual(
            effective["_selected_plan_template"]["bindings"]["advertiser_id"],
            "1234567890",
        )

    def test_v2_template_rejects_other_advertiser(self):
        config = business_template_config()
        with self.assertRaisesRegex(ValueError, "bound to advertiser 1234567890"):
            plan_templates.apply(
                config,
                only_plan_template_name(config),
                advertiser_id=999,
            )

    def test_submit_rejects_other_advertiser_before_token_refresh(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(business_template_config()), encoding="utf-8")
            argv = [
                "ocean-watch plans create",
                "--config", str(config_path),
                "--advertiser-id", "999",
                "--plan-template", only_plan_template_name(business_template_config()),
                "--submit",
            ]
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.object(token_manager, "ensure_access_token") as ensure_token, \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main(), 2)
            ensure_token.assert_not_called()

    def test_business_template_must_be_selected_explicitly(self):
        config = business_template_config()
        with self.assertRaisesRegex(ValueError, "explicit plan template"):
            plan_templates.apply(config)

    def test_v2_template_requires_all_bindings(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        del config["plan_templates"][name]["bindings"]["platform"]
        with self.assertRaisesRegex(ValueError, "template bindings missing: platform"):
            plan_templates.apply(config, name)

    def test_template_migration_requires_confirmation_for_fixed_materials(self):
        config = valid_config()
        name = "平台-CID-商品-unique-product-1"
        config["plan_templates"] = {
            name: {
                "display_name": name,
                "custom_metadata": {"owner": "test-suite"},
                "platform": "平台",
                "traffic_source": "CID",
                "product_label": "test product",
                "product_id": "unique-product-1",
                **plan_templates.section_bundle(config),
            }
        }
        runtime_args = args(video_id=["video-1"])
        before = create_plan.build_payloads(
            create_plan.apply_plan_template(config, name),
            runtime_args,
        )
        with self.assertRaises(plan_templates.LegacyMaterialSelectionError):
            plan_templates.migrate(config)
        migrated = plan_templates.migrate(
            config,
            confirm_remove_legacy_materials=True,
        )
        migrated_name = (
            "巨量营销-1234567890-test product-unique-product-1-混剪素材"
        )
        after = create_plan.build_payloads(
            create_plan.apply_plan_template(migrated, migrated_name),
            runtime_args,
        )
        self.assertEqual(before, after)
        self.assertEqual(
            migrated["plan_templates"][migrated_name]["material_strategy"]["source_type"],
            "ACCOUNT_UPLOAD",
        )
        self.assertEqual(
            migrated["plan_templates"][migrated_name]["custom_metadata"],
            {"owner": "test-suite"},
        )
        self.assertNotIn(
            "video_ids",
            migrated["plan_templates"][migrated_name]
            .get("overrides", {})
            .get("materials", {}),
        )
        self.assertNotIn("active_plan_template", migrated)
        self.assertEqual(migrated["default_plan_template"]["materials"], {})
        self.assertEqual(migrated["default_plan_template"]["links"], {})
        self.assertEqual(migrated["default_plan_template"]["tracking_urls"], {})
        self.assertEqual(migrated["default_plan_template"]["titles"], [])
        self.assertNotIn(
            "unique_product_id",
            migrated["default_plan_template"]["resolved_ids"],
        )

    def test_current_template_rejects_fixed_runtime_material_ids(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        config["plan_templates"][name]["overrides"] = {
            "materials": {"video_ids": ["video-1"]},
        }
        with self.assertRaisesRegex(ValueError, "cannot store runtime material IDs"):
            plan_templates.apply(config, name)

    def test_current_template_rejects_empty_runtime_material_fields(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        config["plan_templates"][name]["overrides"] = {
            "materials": {"video_ids": []},
        }
        with self.assertRaisesRegex(ValueError, "cannot store runtime material IDs"):
            plan_templates.apply(config, name)

    def test_current_schema_rejects_legacy_active_template_field(self):
        config = business_template_config()
        config["active_plan_template"] = "missing-template"
        with self.assertRaisesRegex(ValueError, "does not support active_plan_template"):
            plan_templates.migrate(config)

    def test_v3_names_and_references_migrate_to_shared_rule(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
        source = config["plan_templates"].pop(source_name)
        old_source_name = "旧来源模板"
        source["display_name"] = old_source_name
        target = copy.deepcopy(source)
        target["bindings"].update({
            "product_id": "product-2",
            "product_name": "目标商品",
        })
        target["display_name"] = "旧目标模板"
        target["created_from"] = {"template": old_source_name}
        target["copy_materials"] = {
            "titles": ["这是有效测试文案"],
            "copied_from_template": old_source_name,
        }
        config["plan_template_schema_version"] = 3
        config["plan_templates"] = {
            old_source_name: source,
            "旧目标模板": target,
        }
        config["active_plan_template"] = "旧目标模板"

        migrated = plan_templates.migrate(config)

        new_source_name = (
            "巨量营销-1234567890-test product-unique-product-1-混剪素材"
        )
        new_target_name = "巨量营销-1234567890-目标商品-product-2-混剪素材"
        self.assertEqual(migrated["plan_template_schema_version"], 5)
        self.assertEqual(set(migrated["plan_templates"]), {new_source_name, new_target_name})
        self.assertNotIn("active_plan_template", migrated)
        migrated_target = migrated["plan_templates"][new_target_name]
        self.assertEqual(migrated_target["created_from"]["template"], new_source_name)
        self.assertEqual(
            migrated_target["copy_materials"]["copied_from_template"],
            new_source_name,
        )

    def test_v3_name_migration_rejects_collisions_without_mutation(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
        source = config["plan_templates"][source_name]
        config["plan_template_schema_version"] = 3
        config["plan_templates"] = {
            "旧模板一": copy.deepcopy(source),
            "旧模板二": copy.deepcopy(source),
        }
        original = copy.deepcopy(config)

        with self.assertRaisesRegex(ValueError, "naming collision"):
            plan_templates.migrate(config)

        self.assertEqual(config, original)

    def test_template_migration_cli_returns_structured_confirmation_error(self):
        config = valid_config()
        name = "平台-CID-商品-unique-product-1"
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
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text(json.dumps(config), encoding="utf-8")
            output = StringIO()
            with redirect_stdout(output):
                code = manage_plan_templates.main([
                    "--config", str(path), "migrate",
                ])
            unchanged = json.loads(path.read_text(encoding="utf-8"))
        result = json.loads(output.getvalue())
        self.assertEqual(code, 2)
        self.assertFalse(result["changed"])
        self.assertEqual(
            result["error_code"],
            "legacy_material_selection_requires_confirmation",
        )
        self.assertEqual(result["affected_templates"], [name])
        self.assertEqual(unchanged, config)

    def test_create_template_records_advertiser_binding(self):
        config = plan_templates.migrate(valid_config())
        arguments = SimpleNamespace(
            advertiser_id="456",
            platform="天猫",
            traffic_source="CID",
            product_id="product-2",
            product_name="新商品",
            product_image_ids=["image-2", "image-3", "image-2"],
            product_info={
                "product_name_type": "CUSTOM",
                "product_image_type": "CUSTOM",
                "product_selling_point_type": "CUSTOM",
                "titles": ["新商品"],
                "selling_points": ["新商品值得推荐"],
            },
            name=None,
            source_name="新来源",
            landing_page_url="https://landing.test/new",
            open_url="testapp://new",
            track_url="https://tracking.test/new-impression",
            action_track_url="https://tracking.test/new-click",
            title=["第一条测试文案", "第二条测试文案", "第一条测试文案"],
            from_template=None,
            force=False,
        )
        updated, name = manage_plan_templates.create_template(config, arguments)
        template = updated["plan_templates"][name]
        self.assertEqual(name, "巨量营销-456-新商品-product-2-混剪素材")
        self.assertEqual(template["bindings"]["advertiser_id"], "456")
        self.assertEqual(template["bindings"]["product_id"], "product-2")
        self.assertEqual(
            template["overrides"]["resolved_ids"]["unique_product_id"],
            "product-2",
        )
        self.assertEqual(
            template["overrides"]["resolved_ids"]["product_image_ids"],
            ["image-2", "image-3"],
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
        config = business_template_config()
        row = manage_plan_templates.list_templates(config)[0]
        self.assertEqual(row["channel"], "marketing")
        self.assertEqual(row["advertiser_id"], "1234567890")
        self.assertEqual(row["platform"], "平台")
        self.assertEqual(row["product_id"], "unique-product-1")
        self.assertEqual(row["product_image_ids"], [])
        self.assertEqual(row["product_image"]["type"], "CUSTOM")
        self.assertTrue(row["product_image"]["manual_image_ids_required"])
        self.assertEqual(row["delivery_settings"]["daily_budget"], 300)

    def test_cross_advertiser_template_clone_is_rejected(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
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
            from_template=source_name,
            force=False,
        )
        with self.assertRaisesRegex(ValueError, "cross-advertiser template cloning"):
            manage_plan_templates.create_template(config, arguments)

    def test_set_copy_materials_updates_business_template(self):
        config = business_template_config()
        name = only_plan_template_name(config)
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
        config = business_template_config()
        source_name = only_plan_template_name(config)
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
        summary = manage_plan_templates.default_template_summary(business_template_config())
        self.assertEqual(summary["name"], "default_plan_template")
        self.assertFalse(summary["business_usable"])
        self.assertFalse(summary["selectable_for_plan_creation"])
        self.assertEqual(summary["delivery_settings"]["daily_budget"], 300)
        self.assertEqual(summary["product_image"]["type"], "CUSTOM")
        self.assertTrue(summary["product_image"]["manual_image_ids_required"])
        self.assertEqual(summary["regions"]["city_count"], 1)

    def test_template_source_options_show_business_bindings(self):
        config = business_template_config()
        output = []
        selected = manage_plan_templates.select_template_source(
            config,
            input_fn=lambda _: "0",
            output_fn=output.append,
        )
        rendered = "\n".join(output)
        self.assertIsNone(selected)
        self.assertIn("渠道 marketing", rendered)
        self.assertIn("广告主 1234567890", rendered)
        self.assertIn("平台 平台", rendered)
        self.assertIn("商品 test product", rendered)
        self.assertIn("商品 ID unique-product-1", rendered)
        self.assertIn("素材来源 上传素材", rendered)

    def test_template_source_options_are_filtered_by_material_mode(self):
        config = business_template_config()
        upload_name = only_plan_template_name(config)
        native_name = "平台-CID-原生商品-product-2-达人素材"
        native = copy.deepcopy(config["plan_templates"][upload_name])
        native["display_name"] = native_name
        native["bindings"]["product_id"] = "product-2"
        native["bindings"]["product_name"] = "原生商品"
        native["material_strategy"]["source_type"] = "CREATOR_AUTHORIZED"
        config["plan_templates"][native_name] = native
        output = []

        selected = manage_plan_templates.select_template_source(
            config,
            input_fn=lambda _: "1",
            output_fn=output.append,
            material_source_type="CREATOR_AUTHORIZED",
        )

        rendered = "\n".join(output)
        self.assertEqual(selected, native_name)
        self.assertIn("创建来源（原生素材）", rendered)
        self.assertIn(native_name, rendered)
        self.assertNotIn(upload_name, rendered)

    def test_create_wizard_from_default_requires_confirmation(self):
        config = business_template_config()
        original = copy.deepcopy(config)
        answers = PromptAnswers({
            "请选择来源编号": "0",
            "广告主 ID": "456",
            "平台": "京东",
            "流量来源": "",
            "商品名称": "新商品推荐文案",
            "商品 ID": "product-2",
            "产品卖点": "",
            "日预算": "5000",
            "净成交 ROI 出价": "1.7",
            "性别": "2",
            "年龄": "24-49",
            "素材来源": "1",
            "素材选择方式": "",
            "每单元素材数量": "",
            "输入文案标题": ["第一条新文案", ""],
            "计划来源": "新来源",
            "落地页链接": "https://landing.test/new",
            "直达链接": "testapp://new",
            "展示监测链接": "https://tracking.test/new-impression",
            "点击/有效触点监测链接": "https://tracking.test/new-click",
            "确认创建此业务模板": "n",
        })
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        self.assertEqual(updated, original)
        self.assertFalse(result["confirmed"])
        self.assertFalse(result["changed"])
        self.assertTrue(result["validation"]["ready_for_plan_creation"])
        self.assertEqual(
            result["template"],
            "巨量营销-456-新商品推荐文案-product-2-混剪素材",
        )
        self.assertEqual(result["product_image"]["type"], "DPA")
        self.assertFalse(result["product_image"]["manual_image_ids_required"])
        self.assertEqual(result["product_selling_points"], ["新商品推荐文案推荐"])
        self.assertEqual(result["delivery_settings"]["daily_budget"], 5000)
        self.assertEqual(result["delivery_settings"]["roi_goal"], 1.7)
        self.assertEqual(result["delivery_settings"]["gender"], "GENDER_FEMALE")
        self.assertEqual(
            result["delivery_settings"]["ages"],
            [
                "AGE_BETWEEN_24_30",
                "AGE_BETWEEN_31_40",
                "AGE_BETWEEN_41_49",
            ],
        )

    def test_create_wizard_clones_business_template_and_clears_account_assets(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
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
        answers = PromptAnswers({
            "请选择来源编号": "1",
            "广告主 ID": "456",
            "平台": "京东",
            "流量来源": "",
            "商品名称": "同款商品",
            "商品 ID": "product-2",
            "产品卖点": "",
            "日预算": "",
            "净成交 ROI 出价": "",
            "性别": "",
            "年龄": "",
            "素材来源": "",
            "素材选择方式": "",
            "每单元素材数量": "",
            "输入文案标题": ["这是新商品测试文案", ""],
            "计划来源": "新来源",
            "落地页链接": "https://landing.test/new",
            "直达链接": "testapp://new",
            "展示监测链接": "https://tracking.test/new-impression",
            "点击/有效触点监测链接": "https://tracking.test/new-click",
            "确认创建此业务模板": "y",
        })
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        name = "巨量营销-456-同款商品-product-2-混剪素材"
        template = updated["plan_templates"][name]
        self.assertTrue(result["confirmed"])
        self.assertEqual(template["copy_materials"]["titles"], ["这是新商品测试文案"])
        self.assertEqual(template["overrides"]["materials"], {})
        self.assertNotIn("event_asset_ids", template["overrides"]["resolved_ids"])
        self.assertNotIn("product_image_ids", template["overrides"]["resolved_ids"])
        product_info = template["overrides"]["defaults"]["product_info"]
        self.assertEqual(product_info["product_image_type"], "DPA")
        self.assertEqual(product_info["product_image_fields"], ["images_url"])
        self.assertEqual(product_info["titles"], ["同款商品"])
        self.assertEqual(product_info["selling_points"], ["同款商品推荐"])
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
        self.assertEqual(set(config["plan_templates"]), {source_name})

    def test_same_advertiser_new_product_clears_product_assets_and_copy(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
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
        self.assertEqual(name, "巨量营销-1234567890-新商品-product-2-混剪素材")
        self.assertEqual(template["copy_materials"]["titles"], [])
        self.assertNotIn("product_image_ids", template["overrides"]["resolved_ids"])
        self.assertNotIn("product_platform_id", template["overrides"]["resolved_ids"])
        self.assertNotIn("landing_page_url", template["overrides"].get("links", {}))
        self.assertEqual(template["created_from"]["policy"], "same_advertiser_new_product")

    def test_same_advertiser_same_product_inherits_product_images(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
        config["plan_templates"][source_name]["overrides"] = {
            "resolved_ids": {
                "unique_product_id": "unique-product-1",
                "product_image_ids": ["source-image-1", "source-image-2"],
            }
        }
        _, template = template_workflow.build_template(
            config,
            {
                "advertiser_id": "1234567890",
                "platform": "平台",
                "traffic_source": "CID",
                "product_id": "unique-product-1",
                "product_name": "test product",
                "name": "同商品新模板",
                "titles": None,
            },
            source_name,
        )
        self.assertEqual(
            template["overrides"]["resolved_ids"]["product_image_ids"],
            ["source-image-1", "source-image-2"],
        )

    def test_product_image_ids_accept_chinese_commas_and_deduplicate(self):
        self.assertEqual(
            template_workflow.normalize_product_image_ids("image-1， image-2,image-1"),
            ["image-1", "image-2"],
        )

    def test_product_selling_points_follow_official_length_and_limit(self):
        self.assertEqual(
            template_workflow.normalize_product_selling_points(
                "商品卖点推荐，商品卖点推荐"
            ),
            ["商品卖点推荐"],
        )
        with self.assertRaisesRegex(ValueError, "6-9 positions"):
            template_workflow.normalize_product_selling_points("太短")
        with self.assertRaisesRegex(ValueError, "at most 10"):
            template_workflow.normalize_product_selling_points(
                [f"商品卖点{index}推荐" for index in range(11)]
            )

    def test_generated_template_name_contains_all_business_bindings(self):
        self.assertEqual(
            template_workflow.template_name(
                "123",
                "蛋白粉",
                "product-1",
                "ACCOUNT_UPLOAD",
            ),
            "巨量营销-123-蛋白粉-product-1-混剪素材",
        )

    def test_age_presets_use_one_official_enum_family(self):
        self.assertEqual(
            manage_plan_templates.normalize_ages("24-49"),
            [
                "AGE_BETWEEN_24_30",
                "AGE_BETWEEN_31_40",
                "AGE_BETWEEN_41_49",
            ],
        )
        with self.assertRaisesRegex(ValueError, "mixed official age groups"):
            manage_plan_templates.normalize_ages(
                "AGE_BETWEEN_18_23,AGE_BETWEEN_31_35"
            )

    def test_cross_advertiser_same_product_keeps_copy_but_clears_links(self):
        config = business_template_config()
        source_name = only_plan_template_name(config)
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
        config = business_template_config()
        name = only_plan_template_name(config)
        template = copy.deepcopy(config["plan_templates"][name])
        template["copy_materials"] = {"titles": ["太短"]}
        result = template_workflow.validate_candidate(config, name, template)
        self.assertFalse(result["ready_for_plan_creation"])
        self.assertTrue(
            any(field.startswith("copy_materials.titles:") for field in result["template_missing_fields"])
        )

    def test_candidate_validation_accepts_supported_name_placeholders(self):
        config = business_template_config()
        name = only_plan_template_name(config)
        template = copy.deepcopy(config["plan_templates"][name])
        template["copy_materials"] = {"titles": ["这是一条有效测试文案"]}
        result = template_workflow.validate_candidate(config, name, template)
        self.assertNotIn(
            "defaults.project_name_template",
            result["template_missing_fields"],
        )
        self.assertNotIn(
            "defaults.promotion_name_template",
            result["template_missing_fields"],
        )

    def test_candidate_validation_resolves_channel_api_base_url(self):
        config = channels.migrate_config(business_template_config())
        name = only_plan_template_name(config)
        template = copy.deepcopy(config["plan_templates"][name])
        template["copy_materials"] = {"titles": ["这是一条有效测试文案"]}
        result = template_workflow.validate_candidate(config, name, template)
        self.assertNotIn("api.base_url", result["template_missing_fields"])

    def test_incomplete_wizard_template_cannot_be_saved(self):
        config = business_template_config()
        answers = PromptAnswers({
            "请选择来源编号": "0",
            "广告主 ID": "456",
            "平台": "京东",
            "流量来源": "",
            "商品名称": "新商品",
            "商品 ID": "product-2",
            "产品卖点": "新商品值得推荐",
            "日预算": "",
            "净成交 ROI 出价": "",
            "性别": "",
            "年龄": "",
            "素材来源": "1",
            "素材选择方式": "",
            "每单元素材数量": "",
            "输入文案标题": "",
            "计划来源": "",
            "落地页链接": "",
            "直达链接": "",
            "展示监测链接": "",
            "点击/有效触点监测链接": "",
            "确认创建此业务模板": "y",
        })
        updated, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        self.assertFalse(result["confirmed"])
        self.assertTrue(result["blocked"])
        self.assertFalse(result["validation"]["ready_for_plan_creation"])
        self.assertEqual(updated, config)

    def test_direct_create_cannot_save_incomplete_template(self):
        config = business_template_config()
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
            force=False,
        )
        with self.assertRaisesRegex(ValueError, "incomplete plan template cannot be saved"):
            manage_plan_templates.create_template(config, arguments)
        self.assertEqual(config, original)

    def test_wizard_preview_contains_field_level_changes(self):
        config = business_template_config()
        answers = PromptAnswers({
            "请选择来源编号": "0",
            "广告主 ID": "456",
            "平台": "京东",
            "流量来源": "",
            "商品名称": "新商品",
            "商品 ID": "product-2",
            "产品卖点": "新商品值得推荐",
            "日预算": "5000",
            "净成交 ROI 出价": "1.7",
            "性别": "2",
            "年龄": "24-49",
            "素材来源": "1",
            "素材选择方式": "",
            "每单元素材数量": "",
            "输入文案标题": ["这是一条有效测试文案", ""],
            "计划来源": "新来源",
            "落地页链接": "https://landing.test/new",
            "直达链接": "testapp://new",
            "展示监测链接": "https://tracking.test/new-impression",
            "点击/有效触点监测链接": "https://tracking.test/new-click",
            "确认创建此业务模板": "n",
        })
        _, result = manage_plan_templates.run_create_wizard(
            config,
            input_fn=answers,
            output_fn=lambda _: None,
        )
        changed_fields = {change["field"] for change in result["changes"]}
        self.assertIn("bindings.advertiser_id", changed_fields)
        self.assertIn("overrides.links.landing_page_url", changed_fields)
        self.assertIn("overrides.defaults.product_info.product_image_type", changed_fields)
        self.assertIn("overrides.defaults.daily_budget", changed_fields)
        self.assertEqual(result["product_image"]["type"], "DPA")

    def test_failed_project_submission_returns_nonzero(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = valid_config()
            config["defaults"]["product_id"] = "unique-product-1"
            config["plan_templates"] = {
                "legacy": {**plan_templates.section_bundle(config)},
            }
            config_path.write_text(json.dumps(config), encoding="utf-8")
            client = mock.Mock()
            client.post.return_value = {"code": 500}
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=lambda path, config, **kwargs: config), \
                    mock.patch.object(plan_executor.OceanEngineClient, "__new__", return_value=client), \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main([
                    "--config", str(config_path),
                    "--plan-template", "legacy",
                    "--submit",
                ]), 1)

    def test_failed_promotion_submission_returns_nonzero(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config = valid_config()
            config["defaults"]["product_id"] = "unique-product-1"
            config["plan_templates"] = {
                "legacy": {**plan_templates.section_bundle(config)},
            }
            config_path.write_text(json.dumps(config), encoding="utf-8")
            responses = [
                {"data": {"project_id": 42}},
                {"code": 500},
            ]
            client = mock.Mock()
            client.post.side_effect = responses
            with mock.patch.object(token_manager, "ensure_access_token", side_effect=lambda path, config, **kwargs: config), \
                    mock.patch.object(plan_executor.OceanEngineClient, "__new__", return_value=client), \
                    redirect_stdout(StringIO()):
                self.assertEqual(create_plan.main([
                    "--config", str(config_path),
                    "--plan-template", "legacy",
                    "--submit",
                ]), 1)

class TemplateBatchMappingTests(unittest.TestCase):
    def config_with_two_accounts(self):
        config = business_template_config()
        second = "巨量营销-456-商品-product-2-混剪素材"
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
            "material_strategy": {
                "source_type": "ACCOUNT_UPLOAD",
                "selection_mode": "MANUAL",
                "max_materials_per_unit": 5,
            },
            "overrides": {"resolved_ids": {"unique_product_id": "product-2"}},
        }
        return config, second

    def test_multi_account_jobs_use_bound_templates(self):
        config, second = self.config_with_two_accounts()
        first = next(name for name in config["plan_templates"] if name != second)
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
        first = next(iter(config["plan_templates"]))
        with self.assertRaisesRegex(ValueError, "one account"):
            batch_create_from_today_videos.resolve_account_jobs(
                config,
                ["1234567890,456"],
                None,
                fallback_template=first,
            )

    def test_account_always_requires_explicit_mapping(self):
        config, _ = self.config_with_two_accounts()
        with self.assertRaisesRegex(ValueError, "explicit template mapping"):
            batch_create_from_today_videos.resolve_account_jobs(
                config,
                ["1234567890"],
                None,
            )
