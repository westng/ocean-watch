import copy
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from ocean_watch.plans import batch_create_creator_plans, create_creator_plan
from ocean_watch.templates import plan_templates


def creator_config():
    base = {
        "api": {"base_url": "https://api.example.test/open_api"},
        "account": {"advertiser_id": "1234567890123456", "channel": "marketing"},
        "defaults": {
            "operation": "ENABLE",
            "project_name_template": "{material_date}_原生素材roi_详情页",
            "promotion_name_template": "原生单元_{product_name}_{material_date}",
            "product_name": "test product",
            "product_id": "1001",
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
            "product_info": {
                "product_image_type": "CUSTOM",
                "selling_points": ["测试商品卖点"],
            },
        },
        "materials": {},
        "resolved_ids": {
            "city_ids": [1],
            "unique_product_id": "1001",
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
    config = plan_templates.migrate(base)
    name = "平台-CID-测试商品-1001-达人素材"
    config["plan_templates"] = {
        name: {
            "display_name": name,
            "bindings": {
                "channel": "marketing",
                "advertiser_id": "1234567890123456",
                "platform": "平台",
                "traffic_source": "CID",
                "product_id": "1001",
                "product_name": "test product",
            },
            "copy_materials": {"titles": ["这是达人素材测试文案"]},
            "material_strategy": {
                "source_type": "CREATOR_AUTHORIZED",
                "selection_mode": "MANUAL",
                "max_materials_per_unit": None,
                "creator_filters": {
                    "creator_ids": [],
                    "auth_types": ["VIDEO_ITEM"],
                    "authorization_status": "VALID",
                    "minimum_remaining_days": 1,
                },
            },
            "overrides": {
                "defaults": copy.deepcopy(base["defaults"]),
                "resolved_ids": copy.deepcopy(base["resolved_ids"]),
                "tracking_urls": copy.deepcopy(base["tracking_urls"]),
                "links": copy.deepcopy(base["links"]),
            },
        }
    }
    config["active_plan_template"] = name
    return config, name


def manifest(template_name, jobs=None):
    return {
        "schema_version": 1,
        "channel": "marketing",
        "advertiser_id": "1234567890123456",
        "plan_template": template_name,
        "material_date": "7.14",
        "budget": 5000,
        "jobs": jobs or [
            {
                "aweme_id": "creator-one",
                "item_ids": ["8101"],
                "product_match": {
                    "status": "MATCHED",
                    "evidence": "title contains test product",
                },
            },
            {
                "aweme_id": "creator-two",
                "item_ids": ["8201"],
                "product_match": {
                    "status": "USER_CONFIRMED",
                    "evidence": "confirmed by user",
                },
            },
        ],
    }


def cli_args(config_path, jobs_path, journal_path, submit=False):
    return SimpleNamespace(
        config=str(config_path),
        jobs_file=str(jobs_path),
        concurrency=2,
        journal=str(journal_path),
        submit=submit,
        include_payloads=False,
        out=None,
        channel="marketing",
        auth_account_id=None,
    )


def fake_preflight(args):
    aweme_id = args.project_name.rsplit("_", 1)[-1]
    return {
        "mode": "dry_run",
        "selected_creator": {"aweme_id": aweme_id, "aweme_name": aweme_id},
        "selected_materials": [{"item_id": value} for value in args.item_id],
        "project_payload": {"name": args.project_name},
        "promotion_payload": {"name": args.promotion_name},
        "missing_fields": [],
    }, 0


class CreatorBatchTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.config, self.template_name = creator_config()
        self.config_path = self.root / "config.json"
        self.config_path.write_text(json.dumps(self.config), encoding="utf-8")
        self.jobs_path = self.root / "jobs.json"
        self.journal_path = self.root / "journal.json"

    def tearDown(self):
        self.directory.cleanup()

    def write_manifest(self, value=None):
        self.jobs_path.write_text(
            json.dumps(value or manifest(self.template_name)),
            encoding="utf-8",
        )

    def test_manifest_requires_product_match_evidence(self):
        value = manifest(self.template_name, jobs=[{
            "aweme_id": "creator-one",
            "item_ids": ["8101"],
        }])
        self.write_manifest(value)
        with self.assertRaises(batch_create_creator_plans.CreatorBatchError) as raised:
            batch_create_creator_plans.load_jobs(
                self.config_path,
                self.jobs_path,
                "marketing",
            )
        self.assertEqual(raised.exception.code, "product_match_confirmation_required")

    def test_names_append_creator_id_and_inherit_budget(self):
        self.write_manifest()
        _, jobs = batch_create_creator_plans.load_jobs(
            self.config_path,
            self.jobs_path,
            "marketing",
        )
        self.assertTrue(jobs[0]["project_name"].endswith("_creator-one"))
        self.assertTrue(jobs[0]["promotion_name"].endswith("_creator-one"))
        self.assertEqual(jobs[0]["budget"], 5000)

    def test_fingerprint_ignores_product_match_evidence_wording(self):
        self.write_manifest()
        _, jobs = batch_create_creator_plans.load_jobs(
            self.config_path,
            self.jobs_path,
            "marketing",
        )
        changed = copy.deepcopy(jobs)
        changed[0]["product_match"]["evidence"] = "same decision, different wording"
        self.assertEqual(
            batch_create_creator_plans.batch_fingerprint(jobs),
            batch_create_creator_plans.batch_fingerprint(changed),
        )

    def test_dry_run_preflights_jobs_concurrently(self):
        self.write_manifest()
        args = cli_args(self.config_path, self.jobs_path, self.journal_path)
        with mock.patch.object(
            create_creator_plan,
            "execute",
            side_effect=lambda creator_args, **_: fake_preflight(creator_args),
        ) as execute:
            result, exit_code = batch_create_creator_plans.run_batch(args)
        self.assertEqual(exit_code, 0)
        self.assertEqual(result["counts"], {"ready": 2})
        self.assertEqual(execute.call_count, 2)
        self.assertFalse(self.journal_path.exists())

    def test_submit_resumes_partial_job_and_skips_completed_job(self):
        self.write_manifest()
        args = cli_args(
            self.config_path,
            self.jobs_path,
            self.journal_path,
            submit=True,
        )
        first_projects = []

        def first_execute(creator_args, progress_callback=None, **_):
            if not creator_args.submit:
                return fake_preflight(creator_args)
            aweme_id = creator_args.project_name.rsplit("_", 1)[-1]
            project_id = f"project-{aweme_id}"
            first_projects.append((aweme_id, creator_args.project_id))
            progress_callback({
                "status": "project_created",
                "project_id": project_id,
                "response": {"code": 0, "data": {"project_id": project_id}},
            })
            if aweme_id == "creator-one":
                progress_callback({
                    "status": "promotion_failed",
                    "project_id": project_id,
                    "response": {"code": 40000, "message": "retry me"},
                })
                return {
                    "project_id": project_id,
                    "failure_stage": "promotion_create",
                    "submit_failed": True,
                }, 1
            promotion_id = f"promotion-{aweme_id}"
            progress_callback({
                "status": "completed",
                "project_id": project_id,
                "promotion_id": promotion_id,
                "response": {"code": 0, "data": {"promotion_id": promotion_id}},
            })
            return {
                "project_response": {"code": 0, "data": {"project_id": project_id}},
                "promotion_response": {"code": 0, "data": {"promotion_id": promotion_id}},
            }, 0

        with mock.patch.object(create_creator_plan, "execute", side_effect=first_execute):
            first, first_code = batch_create_creator_plans.run_batch(args)
        self.assertEqual(first_code, 1)
        self.assertEqual(first["counts"], {"promotion_failed": 1, "created": 1})
        journal = json.loads(self.journal_path.read_text(encoding="utf-8"))
        first_key = next(key for key in journal["jobs"] if "creator-one" in key)
        self.assertEqual(journal["jobs"][first_key]["project_id"], "project-creator-one")

        second_submit_calls = []

        def second_execute(creator_args, progress_callback=None, **_):
            if not creator_args.submit:
                return fake_preflight(creator_args)
            second_submit_calls.append((creator_args.project_name, creator_args.project_id))
            self.assertEqual(creator_args.project_id, "project-creator-one")
            self.assertTrue(creator_args.promotion_only)
            progress_callback({
                "status": "completed",
                "project_id": creator_args.project_id,
                "promotion_id": "promotion-creator-one",
                "response": {"code": 0, "data": {"promotion_id": "promotion-creator-one"}},
            })
            return {
                "project_id": creator_args.project_id,
                "promotion_response": {
                    "code": 0,
                    "data": {"promotion_id": "promotion-creator-one"},
                },
            }, 0

        with mock.patch.object(create_creator_plan, "execute", side_effect=second_execute):
            second, second_code = batch_create_creator_plans.run_batch(args)
        self.assertEqual(second_code, 0)
        self.assertEqual(second["counts"], {"created": 1, "skipped_completed": 1})
        self.assertEqual(len(second_submit_calls), 1)
        self.assertEqual(
            len([row for row in second["results"] if row["aweme_id"] == "creator-two"]),
            1,
        )


if __name__ == "__main__":
    unittest.main()
