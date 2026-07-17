import copy
import unittest

from ocean_watch.plans import marketing_runtime_assets

from tests.support import valid_config


class RuntimeAssetClient:
    def __init__(self, *, projects=None, promotions=None, event_assets=None, dpa_data=None):
        self.projects = projects or []
        self.promotions = promotions or {}
        self.event_assets = event_assets or []
        self.dpa_data = dpa_data if dpa_data is not None else {"asset_list": []}
        self.calls = []

    def get(self, path, params=None):
        self.calls.append(("GET", path, copy.deepcopy(params)))
        if path == marketing_runtime_assets.PROJECT_LIST_PATH:
            return {
                "code": 0,
                "request_id": "project-request",
                "data": {
                    "list": copy.deepcopy(self.projects),
                    "page_info": {"page": 1, "total_page": 1},
                },
            }
        if path == marketing_runtime_assets.PROMOTION_LIST_PATH:
            project_id = str((params.get("filtering") or {}).get("project_id"))
            return {
                "code": 0,
                "request_id": f"promotion-request-{project_id}",
                "data": {"list": copy.deepcopy(self.promotions.get(project_id, []))},
            }
        if path == marketing_runtime_assets.EVENT_ASSET_LIST_PATH:
            return {
                "code": 0,
                "request_id": "event-list-request",
                "data": {"asset_list": copy.deepcopy(self.event_assets)},
            }
        if path == marketing_runtime_assets.OPTIMIZED_GOAL_PATH:
            return {
                "code": 0,
                "request_id": f"goal-request-{params['asset_id']}",
                "data": {
                    "goals": [{"external_action": "AD_CONVERT_TYPE_APP_ORDER"}],
                },
            }
        raise AssertionError(f"unexpected GET {path}")

    def post(self, path, payload=None, params=None):
        self.calls.append(("POST", path, copy.deepcopy(payload)))
        if path == marketing_runtime_assets.DPA_ASSET_DETAIL_PATH:
            return {
                "code": 0,
                "request_id": "dpa-request",
                "data": copy.deepcopy(self.dpa_data),
            }
        raise AssertionError(f"unexpected POST {path}")


def runtime_config():
    config = valid_config()
    config["account"]["advertiser_id"] = 1234567890
    config["defaults"].update({
        "asset_type": "THIRDPARTY",
        "external_action": "AD_CONVERT_TYPE_APP_ORDER",
        "product_info": {
            "product_name_type": "CUSTOM",
            "product_image_type": "DPA",
            "product_image_fields": ["images_url"],
            "product_selling_point_type": "CUSTOM",
            "titles": ["test product"],
            "selling_points": ["测试商品推荐"],
        },
    })
    config["resolved_ids"] = {
        "city_ids": [1],
        "unique_product_id": "7651932094620303396",
    }
    return config


def matching_project(asset_ids=None):
    return {
        "project_id": 1001,
        "advertiser_id": 1234567890,
        "landing_type": "SHOP",
        "marketing_goal": "VIDEO_AND_IMAGE",
        "delivery_mode": "PROCEDURAL",
        "ad_type": "ALL",
        "asset_type": "THIRDPARTY",
        "related_product": {"unique_product_id": 7651932094620303396},
        "optimize_goal": {
            "external_action": "AD_CONVERT_TYPE_APP_ORDER",
            "asset_ids": list(asset_ids or [2001]),
        },
    }


class MarketingRuntimeAssetTests(unittest.TestCase):
    def test_resolves_event_and_product_creative_from_matching_delivery(self):
        client = RuntimeAssetClient(
            projects=[matching_project()],
            promotions={
                "1001": [{
                    "promotion_id": 3001,
                    "promotion_materials": {
                        "product_info": {"image_ids": ["image-1", "image-2"]},
                    },
                    "brand_info": {
                        "brand_name_id": 4001,
                        "cdp_brand_id": None,
                    },
                }],
            },
        )

        resolved, diagnostics = marketing_runtime_assets.resolve(
            runtime_config(),
            client=client,
        )

        self.assertEqual(resolved["resolved_ids"]["event_asset_ids"], [2001])
        self.assertEqual(
            resolved["resolved_ids"]["product_image_ids"],
            ["image-1", "image-2"],
        )
        self.assertEqual(resolved["resolved_ids"]["brand_info"], {"brand_name_id": 4001})
        product_info = resolved["defaults"]["product_info"]
        self.assertEqual(product_info["product_image_type"], "CUSTOM")
        self.assertNotIn("product_image_fields", product_info)
        self.assertEqual(diagnostics["event_asset"]["source"], "matching_project")
        self.assertEqual(diagnostics["product_creative"]["source"], "matching_promotion")

    def test_keeps_dpa_when_configured_fields_exist(self):
        config = runtime_config()
        config["resolved_ids"]["event_asset_ids"] = [2001]
        client = RuntimeAssetClient(
            dpa_data={
                "asset_list": [{"properties": {"images_url": ["https://image.test/1"]}}],
            },
        )

        resolved, diagnostics = marketing_runtime_assets.resolve(config, client=client)

        self.assertEqual(
            resolved["defaults"]["product_info"]["product_image_type"],
            "DPA",
        )
        self.assertNotIn("product_image_ids", resolved["resolved_ids"])
        self.assertEqual(diagnostics["event_asset"]["status"], "configured")
        self.assertEqual(diagnostics["product_creative"]["source"], "dpa_product_fields")
        requested_paths = [path for _, path, _ in client.calls]
        self.assertNotIn(marketing_runtime_assets.PROJECT_LIST_PATH, requested_paths)
        self.assertNotIn(marketing_runtime_assets.PROMOTION_LIST_PATH, requested_paths)

    def test_ambiguous_event_assets_require_selection_without_mutating_input(self):
        config = runtime_config()
        config["defaults"]["product_info"]["product_image_type"] = "CUSTOM"
        config["resolved_ids"]["product_image_ids"] = ["image-1"]
        original = copy.deepcopy(config)
        client = RuntimeAssetClient(
            event_assets=[
                {"asset_id": 2001, "asset_name": "asset one"},
                {"asset_id": 2002, "asset_name": "asset two"},
            ],
        )

        with self.assertRaises(
            marketing_runtime_assets.MarketingRuntimeAssetError
        ) as caught:
            marketing_runtime_assets.resolve(config, client=client)

        self.assertEqual(caught.exception.code, "event_asset_selection_required")
        self.assertEqual(config, original)
        self.assertEqual(
            [item["asset_id"] for item in caught.exception.details["candidate_event_assets"]],
            ["2001", "2002"],
        )


if __name__ == "__main__":
    unittest.main()
