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
- Unified and overall reports: https://open.oceanengine.com/labels/12/docs/1824289224504835?origin=left_nav

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

## Qianchuan Unified And Overall Reports

- Official documents: https://open.oceanengine.com/labels/12/docs/1824289224504835 and https://open.oceanengine.com/labels/12/docs/1865675229008199.
- Combined/乘方 account aggregate: `GET /v1.0/qianchuan/report/all_promotion/get/`. The official Go SDK requires `adlab_scene`; `data_period` is valid only for `OVERALL_PROJECT`.
- 全域 account aggregate: `GET /v1.0/qianchuan/report/uni_promotion/get/`. Use it for responsible-account performance with `advertiser_id`, `start_date`, `end_date`, `marketing_goal=ALL`, `order_platform=QIANCHUAN`, and explicit account metric fields.
- Schema: `GET /v1.0/qianchuan/report/uni_promotion/config/get/`; custom/product data: `GET /v1.0/qianchuan/report/uni_promotion/data/get/`.
- Live-room dimension: `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/`; author dimension: `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/`.
- 全域 product topic is `SITE_PROMOTION_PRODUCT_PRODUCT`; 乘方 product topic is `OVERALL_ROI_PRODUCT_PRODUCT`.
- Send all requested Schema topics in one `data_topics` request rather than multiplying calls by topic. Preserve optional supported `data_period`.
- Custom/product filters use official operator `7`, string-preserving values, complete pagination, and stable `total_page`/`total_number` validation.
- Room and author reports preserve optional `order_platform` and `smart_bid_type`; use hourly granularity only when the requested view requires it.
- The account responses directly supply advertiser-level aggregate metrics. Product, room, and author responses require strict pagination; display limits never stop traversal.
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
- Interactive creator lookup uses `search_key_words` with a visible Douyin ID or creator name, then requires an exact local `aweme_show_id` match and uses the returned numeric `aweme_id`. Batch work owner hints use the F2 visible Douyin ID as `search_key_words` and require the official row's numeric `aweme_id` to match the F2 numeric creator UID; the numeric UID must never replace the visible search value, while a changed visible ID in the official response does not invalidate a matching numeric identity or justify a broad creator-video scan.
- Product-filtered creator videos: `GET https://ad.oceanengine.com/open_api/v1.0/qianchuan/file/video/aweme/get/`.
- The video request requires `advertiser_id`, numeric `aweme_id`, and supports `filtering.product_id`, cursor pagination, and `count` from 1 to 50.
- Query template products separately and deduplicate works while preserving every matched product ID. When a link supplies a template-matching product hint, verify that work only against the hinted product; the hint narrows the official query but never replaces it.
- Creator identity and material results are runtime data. They never belong in a Qianchuan product template.

## Products, Plans, Materials, And Updates

- Selectable products: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_promotion/product/get/`.
- Product all-domain plans: `GET /v1.0/qianchuan/uni_promotion/list/`; details: `GET /v1.0/qianchuan/uni_promotion/ad/detail/`; materials: `GET /v1.0/qianchuan/uni_promotion/ad/material/get/`.
- User-supplied limits for architecture review are plan list QPS 200, plan detail QPS 50, and plan materials QPS 50. These values have not been independently verified in this repository and are not runtime configuration. The batch default of at most 8 in-flight reads (hard maximum 10) is a command-level connection/work bound, not an attempt to consume those QPS limits.
- Material performance: `GET /v1.0/qianchuan/report/material/get/`. Paginate every declared page and aggregate raw report metrics before rounding. Plan-list `stats_info` is not report currency.
- Plan status: `POST /v1.0/qianchuan/uni_promotion/ad/status/update/`; budget: `POST /v1.0/qianchuan/uni_promotion/ad/budget/update/`; ROI target: `POST /v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/`.
- Update commands accept at most ten unique plan IDs, default to dry-run, and treat any failed result row as command failure. `DELETE` additionally requires an explicit delete confirmation.
- Every read and write resolves only the Qianchuan authorization mapped to the requested advertiser. Marketing credentials are never a fallback.

## Work Links And Plan Reconciliation

- Douyin share links use the official `v.douyin.com` → `www.iesdouyin.com` → `www.douyin.com/video/{aweme_item_id}` redirect chain. There is no Qianchuan short-link resolver; only follow allow-listed `douyin.com` and `iesdouyin.com` redirects and parse the numeric work ID.
- `search_key_words` is optional on `/v1.0/qianchuan/uni_aweme/authorized/get/`, but batch plan creation must supply the visible Douyin ID from metadata or the 30-day owner cache. It must then require the returned row's numeric `aweme_id` to match the hinted creator UID; the numeric UID is not a valid substitute for `search_key_words`. It never uses the optional-field form to list every authorized creator. A missing, unavailable, or stale identity skips only that work; an official targeted-query or identity-check error is reported as incomplete rather than misclassified as unauthorized.
- `/v1.0/qianchuan/file/video/aweme/get/` accepts up to 50 `filtering.aweme_item_ids` and an optional `filtering.product_id`. Resolve creator ownership first, then verify each template product.
- `/v1.0/qianchuan/uni_promotion/list/` uses `marketing_goal=VIDEO_PROM_GOODS`, `filtering.status=ALL`, and `adlab_scene=UNI_PROJECT`. `ALL` includes paused plans and excludes deleted plans.
- Plan-list `start_time` and `end_time` are required data-period fields and do not filter plan creation time; creation dates have separate optional fields. Batch work-link reconciliation queries the current local day (`00:00:00` through `23:59:59`) because it only decides whether to create a plan or append materials, and traverses every declared page for that day.
- One batch command scans the current-day plan list exactly once, independent of input-link or creator count. The credentials branch starts the plan scan without waiting for link/F2 resolution, so both branches can overlap. After the first page declares stable totals, remaining pages may use the same bounded pool as targeted creator authorization, ownership/product checks, candidate details, and existing-plan materials. A transient page failure retries only that page with jittered backoff; a bounded `40100` or HTTP `429` retry applies to the failed request instead of imposing a command-wide fixed dispatch interval.
- Batch dry-run and submit serialize the complete official-query and reconciliation phase per advertiser with `qianchuan-advertiser-{advertiser_id}.lock`. Inside one batch command, `--concurrency` defaults to 8 and caps all official reads at 10 in flight across endpoints. The batch read factory intentionally does not attach the legacy shared 250 ms interval or cross-process cooldown. Other Qianchuan commands retain their existing request-control contracts.
- A successful dry-run with at least one create/append action stores a minimal operation-journal snapshot and returns `preflight_id` and `expires_at`. It expires after 30 minutes or at the end of the current `Asia/Shanghai` business day, whichever comes first. `plans batch-qianchuan-works --submit --preflight-id ID` validates the snapshot and current template, resolves current advertiser credentials, rescans plans once, and queries only necessary material differences; it does not repeat link/F2, creator authorization, ownership, or product checks. A changed create/append action or append target blocks only that creator with `preflight_changed`; unchanged creators continue serially.
- `--preflight-id` requires `--submit` and is mutually exclusive with `--plan-template`, `--work-url`, `--plan-type`, and `--business`. The snapshot never contains credentials, Cookies, input URLs, raw F2 output, or raw official responses. Batch create/append, deletion reconciliation, plan creation, and plan-setting mutations continue sharing the advertiser lock; writes remain serial and keep existing operation keys, one-shot dispatch, and unknown-write readback reconciliation.
- Plan detail returns exact `aweme_id`, `product_infos`, status, and operation status. Never choose among multiple exact creator matches.
- Plan-list `room_info.anchor_id` is the visible Douyin ID, not the numeric detail `aweme_id`. Candidate reconciliation must carry both identifiers and use plan detail for the final numeric identity check.
- Plan material list returns `material_info.video_material.aweme_item_id`; query `material_status=ALL` and deduplicate before any write.
- Add materials with `/v1.0/qianchuan/uni_promotion/ad/material/add/`. Existing plans receive no budget, ROI, status, schedule, name, or other plan fields.
- Delete custom plan materials with `POST /v1.0/qianchuan/uni_promotion/ad/material/delete/`. The request uses one to 100 nested plan `material_id` values, not `aweme_item_id`; smart-selected materials are unsupported. In multi-creator or multi-product scenarios, deleting one shared material may remove its related delivery associations as well.
- Homepage works use `aweme_item_id` and official `image_mode`. The create payload groups them by matched template product; the add payload groups them by products already present in the plan.
- The official create-plan table marks `creative_card` as conditional by merchant account type: no-account merchants omit it and account-bound merchants provide it. Creator-homepage creation omits the card by default because the verified flow accepts that payload. Never send an empty object: once the object is present, the API validates its inner selling-point fields. If omission is rejected for another account, preserve the official error and require an explicit account-specific card configuration instead of inventing selling points.
