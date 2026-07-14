from types import SimpleNamespace

from ocean_watch.templates import plan_templates


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


def payload_args(**overrides):
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


def business_template_config():
    migrated = plan_templates.migrate(valid_config())
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
            "material_strategy": {
                "source_type": "ACCOUNT_UPLOAD",
                "selection_mode": "MANUAL",
                "max_materials_per_unit": 5,
            },
            "overrides": {},
        }
    }
    migrated["active_plan_template"] = name
    return migrated
