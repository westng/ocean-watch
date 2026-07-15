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

## Authorization And Accounts

- Authorize page: `https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html`
- Authorize params include `material_auth=1` and `state=QC.<nonce>`.
- Token exchange and refresh use `https://ad.oceanengine.com/open_api`.
- Shop advertisers: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/shop/advertiser/list/`.
- Agent advertisers: `GET https://ad.oceanengine.com/open_api/2/agent/advertiser/select/`.
- `PLATFORM_ROLE_SHOP_ACCOUNT` expands with `shop_id` and `permission=["QC_AWEME"]`.
- `PLATFORM_ROLE_QIANCHUAN_AGENT` uses the authorization subject as `advertiser_id`; `data.list` contains Qianchuan advertiser IDs.
- Customer-center and EBP expansion use `account_source=QIANCHUAN`.
- Candidate advertisers are verified through advertiser info in batches of 50 before persistence.
- Missing optional agent permission error `40002` is a partial discovery result; other expansion failures remain blocking.

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

## Creator Materials

- Product all-domain authorized creators: `GET https://api.oceanengine.com/open_api/v1.0/qianchuan/uni_aweme/authorized/get/`.
- Required creator-list fields are `advertiser_id`; `filtering.marketing_goal=VIDEO_PROM_GOODS`; `filtering.search_key_words`; `filtering.scene=CREATE`; `page`; and `page_size` up to 100.
- `search_key_words` searches the visible Douyin ID or creator name. The plugin requires an exact local `aweme_show_id` match and uses the returned numeric `aweme_id`.
- Product-filtered creator videos: `GET https://ad.oceanengine.com/open_api/v1.0/qianchuan/file/video/aweme/get/`.
- The video request requires `advertiser_id`, numeric `aweme_id`, and supports `filtering.product_id`, cursor pagination, and `count` from 1 to 50.
- Query template products separately and deduplicate works while preserving every matched product ID.
- Creator identity and material results are runtime data. They never belong in a Qianchuan product template.

## Work Links And Plan Reconciliation

- Douyin share links use the official `v.douyin.com` → `www.iesdouyin.com` → `www.douyin.com/video/{aweme_item_id}` redirect chain. There is no Qianchuan short-link resolver; only follow allow-listed `douyin.com` and `iesdouyin.com` redirects and parse the numeric work ID.
- `search_key_words` is optional on `/v1.0/qianchuan/uni_aweme/authorized/get/`, so a batch may list all product all-domain creators once.
- `/v1.0/qianchuan/file/video/aweme/get/` accepts up to 50 `filtering.aweme_item_ids` and an optional `filtering.product_id`. Resolve creator ownership first, then verify each template product.
- `/v1.0/qianchuan/uni_promotion/list/` uses `marketing_goal=VIDEO_PROM_GOODS`, `filtering.status=ALL`, and `adlab_scene=UNI_PROJECT`. `ALL` includes paused plans and excludes deleted plans.
- Plan detail returns exact `aweme_id`, `product_infos`, status, and operation status. Never choose among multiple exact creator matches.
- Plan material list returns `material_info.video_material.aweme_item_id`; query `material_status=ALL` and deduplicate before any write.
- Add materials with `/v1.0/qianchuan/uni_promotion/ad/material/add/`. Existing plans receive no budget, ROI, status, schedule, name, or other plan fields.
- Delete custom plan materials with `POST /v1.0/qianchuan/uni_promotion/ad/material/delete/`. The request uses one to 100 nested plan `material_id` values, not `aweme_item_id`; smart-selected materials are unsupported. In multi-creator or multi-product scenarios, deleting one shared material may remove its related delivery associations as well.
- Homepage works use `aweme_item_id` and official `image_mode`. The create payload groups them by matched template product; the add payload groups them by products already present in the plan.
- The official create-plan table marks `creative_card` as conditional by merchant account type: no-account merchants omit it and account-bound merchants provide it. Creator-homepage creation omits the card by default because the verified flow accepts that payload. Never send an empty object: once the object is present, the API validates its inner selling-point fields. If omission is rejected for another account, preserve the official error and require an explicit account-specific card configuration instead of inventing selling points.
