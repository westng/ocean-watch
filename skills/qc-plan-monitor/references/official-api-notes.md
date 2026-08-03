# Qianchuan Official API Notes

> Organization: westng
> Project: ocean-watch
> Skill: qc-plan-monitor

Official docs:

- All-domain plan create: https://open.oceanengine.com/labels/12/docs/1804360384937988?origin=left_nav
- Agent advertiser discovery: https://open.oceanengine.com/labels/12/docs/1697467832592392?origin=left_nav
- Creator videos by product: https://open.oceanengine.com/labels/12/docs/1697466774382599?origin=left_nav
- Product all-domain plan list: https://open.oceanengine.com/labels/12/docs/1771195810853899?origin=left_nav
- Product all-domain plan detail: https://open.oceanengine.com/labels/12/docs/1804362305657868?origin=left_nav
- Product all-domain plan materials: https://open.oceanengine.com/labels/12/docs/1804363488115850?origin=left_nav
- Add product all-domain plan materials: https://open.oceanengine.com/labels/12/docs/1835232814536707?origin=left_nav
- Qianchuan MCP business tools: https://open.oceanengine.com/labels/12/docs/1839622960207943?origin=left_nav
- Qianchuan MCP tool list: https://open.oceanengine.com/labels/12/docs/1847297003631945?origin=left_nav
- Qianchuan MCP guide and examples: https://open.oceanengine.com/labels/12/docs/1849835441833027?origin=left_nav

## Authorization And Accounts

- Authorize page: `https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html`
- Authorize params include `material_auth=1` and `state=QC.<nonce>`.
- Token exchange and refresh use `https://ad.oceanengine.com/open_api`.
- Shop advertisers: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/shop/advertiser/list/`.
- Agent advertisers: `GET https://ad.oceanengine.com/open_api/2/agent/advertiser/select/`.
- `PLATFORM_ROLE_SHOP_ACCOUNT` expands with `shop_id` and `permission=["QC_AWEME"]`.
- Shop advertiser responses primarily expose `data.adv_id_list[].adv_id`; retain compatibility
  with `data.list` without dropping the official field.
- `PLATFORM_ROLE_QIANCHUAN_AGENT` uses the authorization subject as `advertiser_id`; prefer a
  non-empty `data.list` and fall back to `data.advertiser_ids` when the list is empty.
- Customer-center and EBP expansion use `account_source=QIANCHUAN`.
- Traverse every declared expansion page. Validate response page numbers and stable pagination
  totals when supplied, reject duplicate advertiser IDs, and require the final unique count to
  equal `total_number` before persisting the authorization snapshot.
- Candidate advertisers are verified through advertiser info in batches of 50 before persistence.
- Missing optional agent permission error `40002` is a partial discovery result; other expansion failures remain blocking.

## Official MCP Reports

- Remote endpoint: `https://open.oceanengine.com/qianchuan/mcp` using Streamable HTTP.
- Developer authorization uses the existing Qianchuan `Access-Token` header and `Content-Type: application/json`; no separate MCP API Key is required.
- Limit exposed tools with the `Tool-Range` header. The report client allows only `qianchuan_uni_promotion_list_v1`, `qianchuan_report_uni_promotion_config_get_v1`, and `qianchuan_report_uni_promotion_data_get_v1`.
- `qianchuan_report_uni_promotion_data_get_v1` with topic `SITE_PROMOTION_PRODUCT_AD` and dimension `ad_id` is the authoritative product all-domain financial report. Use its nested metric `Value` or `ValueStr` without additional scaling.
- `qianchuan_uni_promotion_list_v1` supplies plan names, statuses, creators, products, budgets, and ROI targets. Use the same requested date range as the financial report; when dates are omitted, both calls query only the current day. Its `stats_info` uses an internal fixed-point representation and must never be displayed or converted into report currency.
- The config tool exposes available all-domain dimensions and metrics. Do not use the standard `qianchuan_report_ad_get_v1` as a substitute for product all-domain reporting.
- Traverse every declared page and reject incomplete pagination, duplicate plan IDs, missing required metrics, fractional count values, and non-finite numbers. Aggregate the raw decimal values before rounding output.
- `status=ALL` preserves report rows when historical plan metadata is no longer returned and marks them as unavailable. A specific status filter requires resolved metadata and fails closed when it cannot be obtained.
- Access Tokens remain in the operating-system credential backend, are refreshed before MCP use, and must never be written into Codex MCP configuration or output.

## Qianchuan Account Aggregate

- Official document: https://open.oceanengine.com/labels/12/docs/1865675229008199
- Account-dimension aggregate: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/report/uni_promotion/get/`.
- Use it for responsible-account performance with `advertiser_id`, `start_date`, `end_date`, `marketing_goal=ALL`, `order_platform=QIANCHUAN`, and explicit account metric fields.
- The response directly supplies advertiser-level aggregate `stat_cost`, ROI2 orders, GMV, and ROI. It does not require plan pagination or plan metadata enrichment.
- Do not substitute `/v1.0/qianchuan/uni_promotion/list/`; that endpoint returns the account's plan list and belongs to plan-level queries only.

## All-Domain Plan Create

- Endpoint: `POST https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_aweme/ad/create/`.
- `marketing_goal` supports `VIDEO_PROM_GOODS` and `LIVE_PROM_GOODS`.
- `delivery_setting.smart_bid_type` supports `SMART_BID_CUSTOM` and `SMART_BID_CONSERVATIVE`.
- Custom bidding requires `roi2_goal`; conservative bidding rejects it.
- `budget` is required and supports at most two decimal places.
- Product delivery requires one to 30 `product_ids`; `name` is optional and supported only for product delivery.
- Live delivery requires `aweme_id` and does not support `name`.
- `SCHEDULE_START_END` requires valid `start_time` and `end_time` dates.
- Success requires `code: 0` and `data.ad_id`.

This is one Qianchuan transaction. It does not use Marketing project and promotion endpoints.

### Product And Live Template Boundaries

- Product templates bind one advertiser, a product label, and one to 30 product IDs. Creator and material IDs remain runtime data.
- Live templates bind one advertiser and one numeric live-account `aweme_id`. They do not bind products or work IDs.
- Material-free live creation uses `marketing_goal=LIVE_PROM_GOODS` and `creative_setting.smart_select_material=true`; `name` is unsupported for this goal.
- Product and live templates use separate schema keys and default skeletons. A default skeleton is never a business template or a direct plan-creation selector.

## Creator Materials

- Product all-domain authorized creators: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_aweme/authorized/get/`.
- Required creator-list fields are `advertiser_id`; `filtering.marketing_goal=VIDEO_PROM_GOODS`; `filtering.search_key_words`; `filtering.scene=CREATE`; `page`; and `page_size` up to 100.
- `search_key_words` searches the visible Douyin ID or creator name. The plugin requires an exact local `aweme_show_id` match and uses the returned numeric `aweme_id`.
- Product-filtered creator videos: `GET https://ad.oceanengine.com/open_api/v1.0/qianchuan/file/video/aweme/get/`.
- The video request requires `advertiser_id`, numeric `aweme_id`, and supports `filtering.product_id`, cursor pagination, and `count` from 1 to 50.
- Query template products separately and deduplicate works while preserving every matched product ID.
- Creator identity and material results are runtime data. They never belong in a Qianchuan product template.

## Products, Plans, Materials, And Updates

- Selectable products: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_promotion/product/get/`.
- Product all-domain plans: `GET /v1.0/qianchuan/uni_promotion/list/`; details: `GET /v1.0/qianchuan/uni_promotion/ad/detail/`; materials: `GET /v1.0/qianchuan/uni_promotion/ad/material/get/`.
- Material performance: `GET /v1.0/qianchuan/report/material/get/`. Paginate every declared page and aggregate raw report metrics before rounding. Plan-list `stats_info` is not report currency.
- Plan status: `POST /v1.0/qianchuan/uni_promotion/ad/status/update/`; budget: `POST /v1.0/qianchuan/uni_promotion/ad/budget/update/`; ROI target: `POST /v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/`.
- Update commands accept at most ten unique plan IDs, default to dry-run, and treat any failed result row as command failure. `DELETE` additionally requires an explicit delete confirmation.
- Every read and write resolves only the Qianchuan authorization mapped to the requested advertiser. Marketing credentials are never a fallback.

## Work Links And Plan Reconciliation

- Douyin share links use the official `v.douyin.com` → `www.iesdouyin.com` → `www.douyin.com/video/{aweme_item_id}` redirect chain. There is no Qianchuan short-link resolver; only follow allow-listed `douyin.com` and `iesdouyin.com` redirects and parse the numeric work ID.
- `search_key_words` is optional on `/v1.0/qianchuan/uni_aweme/authorized/get/`, so a batch may list all product all-domain creators once.
- `/v1.0/qianchuan/file/video/aweme/get/` accepts up to 50 `filtering.aweme_item_ids` and an optional `filtering.product_id`. Resolve creator ownership first, then verify each template product.
- `/v1.0/qianchuan/uni_promotion/list/` uses `marketing_goal=VIDEO_PROM_GOODS`, `filtering.status=ALL`, and `adlab_scene=UNI_PROJECT`. `ALL` includes paused plans and excludes deleted plans.
- Plan-list `start_time` and `end_time` are required data-period fields and do not filter plan creation time; creation dates have separate optional fields. Batch work-link reconciliation queries the current local day (`00:00:00` through `23:59:59`) because it only decides whether to create a plan or append materials, and traverses every declared page for that day.
- Plan detail returns exact `aweme_id`, `product_infos`, status, and operation status. Never choose among multiple exact creator matches.
- Plan-list `room_info.anchor_id` is the visible Douyin ID, not the numeric detail `aweme_id`. Candidate reconciliation must carry both identifiers and use plan detail for the final numeric identity check.
- Plan material list returns `material_info.video_material.aweme_item_id`; query `material_status=ALL` and deduplicate before any write.
- Add materials with `/v1.0/qianchuan/uni_promotion/ad/material/add/`. Existing plans receive no budget, ROI, status, schedule, name, or other plan fields.
- Delete custom plan materials with `POST /v1.0/qianchuan/uni_promotion/ad/material/delete/`. The request uses one to 100 nested plan `material_id` values, not `aweme_item_id`; smart-selected materials are unsupported. In multi-creator or multi-product scenarios, deleting one shared material may remove its related delivery associations as well.
- Homepage works use `aweme_item_id` and official `image_mode`. The create payload groups them by matched template product; the add payload groups them by products already present in the plan.
- The official create-plan table marks `creative_card` as conditional by merchant account type: no-account merchants omit it and account-bound merchants provide it. Creator-homepage creation omits the card by default because the verified flow accepts that payload. Never send an empty object: once the object is present, the API validates its inner selling-point fields. If omission is rejected for another account, preserve the official error and require an explicit account-specific card configuration instead of inventing selling points.
