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
- `GET https://api.oceanengine.com/open_api/v3.0/report/custom/config/get/`
- `GET https://api.oceanengine.com/open_api/v3.0/report/custom/get/`
- `POST https://api.oceanengine.com/open_api/v3.0/project/status/update/`
- `POST https://api.oceanengine.com/open_api/v3.0/promotion/status/update/`
- `POST https://api.oceanengine.com/open_api/v3.0/promotion/budget/update/`
- `POST https://api.oceanengine.com/open_api/v3.0/promotion/bid/update/`
- `POST https://api.oceanengine.com/open_api/v3.0/project/roigoal/update/`

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
- CID link values are opaque strings: do not decode, re-encode, normalize, append macros, or reuse
  query parameters across fields. The project payload owns the display/click tracking arrays; the
  promotion payload owns `external_url_material_list` and `open_url`.
- Do not infer validity or field meaning from the domain, scheme, path, or query parameters. Preserve
  the user-provided string in its selected field through template save/load and payload construction.

## Promotion Create Confirmed Fields

- `advertiser_id`, `project_id`, and `name` are required.
- `operation` supports `ENABLE`, `DISABLE`.
- `promotion_materials` is required.
- Vertical video uses `CREATIVE_IMAGE_MODE_VIDEO_VERTICAL`.
- `title_material_list` accepts at most 10 title materials.
- `title_material_list[].title` length is 5-30 characters by official counting rules; two English characters count as one position. The 55-character rule in this schema belongs to search keyword `bidword_list[].default_word`, not the creative title.
- `source` is conditionally required for `landing_type=SHOP`.
- `promotion_materials.product_info.product_image_type` supports `DPA` and `CUSTOM`.
- `DPA` requires `product_image_fields` from product-library metadata; the upgraded product schema exposes the product image collection as `images_url`.
- `CUSTOM` requires one or more account image IDs in `image_ids`; those IDs are not video or cover IDs.
- `brand_info` can contain `yuntu_category_id`, `brand_name_id`, `ecom_brand_id`, or `cdp_brand_id` depending on account asset linkage.
- Promotion success returns `data.promotion_id`.

## Project Reports And Setting Updates

- Query the report-config endpoint for `UNI_PROJECT_DATA` before requesting project metrics. Select only dimensions and metrics returned for the current advertiser and permission set.
- Project report rows come from `/v3.0/report/custom/get/`. Traverse every declared page; a display limit must never reduce the rows used for totals.
- Project and promotion status updates accept `ENABLE` or `DISABLE` in batch `data` rows.
- Promotion budget and bid updates use `promotion_id`; project ROI updates use `project_id` and `roi_goal`.
- Update commands accept at most ten unique IDs per official batch, default to dry-run, resolve the target advertiser's Marketing authorization only, and treat row-level errors as command failure.
- Report and update responses may contain request IDs and business errors, but no credential value is safe to print.

## Still Requires Runtime Lookup

- Available optimization target combination for the account and product chain.
- Event asset IDs must be resolved within the target advertiser. A same-product project can be
  used only when it leaves one valid candidate; ambiguous candidates require user selection.
- Exact ROI field combination for automatic ecommerce APP order flow.
- City IDs from administrative region API.
- Video IDs, cover image IDs, and product image IDs from material APIs.
- Product status and product platform fields.
- Brand and category IDs.
- Duplicate project/promotion names.

For a standard DPA image template, query the upgraded-product metadata before creating the
project. If the configured field collection is empty, a same-advertiser and same-product official
promotion may supply reusable `image_ids` and non-empty `brand_info`; otherwise block before the
project transaction. Do not persist this runtime fallback as video material state.
