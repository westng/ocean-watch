# Official API Notes

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

Official docs:

- Project create: https://open.oceanengine.com/labels/34/docs/1740868093375503?origin=left_nav
- Promotion create: https://open.oceanengine.com/labels/34/docs/1740946299496459?origin=left_nav

## Confirmed Endpoints

- `POST https://api.oceanengine.com/open_api/v3.0/project/create/`
- `POST https://api.oceanengine.com/open_api/v3.0/promotion/create/`
- `GET https://ad.oceanengine.com/open_api/oauth2/advertiser/get/`
- `GET https://api.oceanengine.com/open_api/2/customer_center/advertiser/list/`
- `GET https://api.oceanengine.com/open_api/2/ebp/advertiser/list/`
- `GET https://api.oceanengine.com/open_api/2/advertiser/info/`

## Authorized Advertiser Expansion

The OAuth advertiser endpoint returns authorization subjects, not only direct advertisers. Expand by `account_role`:

- `ADVERTISER`: use the subject advertiser ID directly.
- `CUSTOMER_ADMIN` and `CUSTOMER_OPERATOR`: query customer-center advertisers with `cc_account_id` and `account_source=AD`.
- `PLATFORM_ROLE_ENTERPRISE_BP_ADMIN` and `PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR`: query EBP advertisers with `enterprise_organization_id` and `account_source=AD`.
- Deduplicate candidates, then validate them through advertiser info in chunks of 50.

Required headers:

- `Access-Token`
- `Content-Type: application/json`

## Project Create Confirmed Fields

- `advertiser_id` is required.
- `operation` supports `ENABLE`, `DISABLE`.
- `delivery_mode` supports `MANUAL`, `PROCEDURAL`.
- `landing_type` includes `SHOP`.
- `marketing_goal` supports `VIDEO_AND_IMAGE`, `LIVE`.
- `ad_type` supports `ALL`, `SEARCH`.
- Automatic delivery with `landing_type=SHOP` should use `delivery_range.inventory_catalog=UNIVERSAL_SMART`.
- Regional targeting uses `audience.district=REGION`, `audience.region_version=2.3.2`, and `audience.city` numeric city codes.
- `audience.location_type` supports `CURRENT`, `HOME`, `TRAVEL`, `ALL`.
- `audience.gender` supports `GENDER_FEMALE`, `GENDER_MALE`, `NONE`.
- Age enums include `AGE_BETWEEN_24_30` and `AGE_BETWEEN_31_40`.
- `delivery_setting.schedule_type` supports `SCHEDULE_FROM_NOW`, `SCHEDULE_START_END`, `SCHEDULE_7_DAYS`.
- `delivery_setting.budget_mode` supports `BUDGET_MODE_INFINITE`, `BUDGET_MODE_DAY`, `BUDGET_MODE_TOTAL`.
- `delivery_setting.pricing` supports `PRICING_OCPM`; automatic delivery supports only `PRICING_OCPM`.
- `track_url_setting.track_url` and `track_url_setting.action_track_url` hold display and click/action tracking links.

## Promotion Create Confirmed Fields

- `advertiser_id`, `project_id`, and `name` are required.
- `operation` supports `ENABLE`, `DISABLE`.
- `promotion_materials` is required.
- Vertical video uses `CREATIVE_IMAGE_MODE_VIDEO_VERTICAL`.
- `title_material_list` accepts at most 10 title materials.
- `title_material_list[].title` length is 5-30 characters by official counting rules; two English characters count as one position. The 55-character rule in this schema belongs to search keyword `bidword_list[].default_word`, not the creative title.
- `source` is conditionally required for `landing_type=SHOP`.
- `brand_info` can contain `yuntu_category_id`, `brand_name_id`, `ecom_brand_id`, or `cdp_brand_id` depending on account asset linkage.
- Promotion success returns `data.promotion_id`.

## Still Requires Runtime Lookup

- Available optimization target combination for the account and product chain.
- Exact ROI field combination for automatic ecommerce APP order flow.
- City IDs from administrative region API.
- Video IDs, cover image IDs, and product image IDs from material APIs.
- Product status and product platform fields.
- Brand and category IDs.
- Duplicate project/promotion names.
