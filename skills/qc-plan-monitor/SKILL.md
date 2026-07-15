---
name: qc-plan-monitor
description: Dedicated 巨量千川 skill for local OAuth, token refresh, advertiser-bound product templates, creator video discovery, product all-domain plan creation, and material append or removal from Douyin work links through official Qianchuan APIs. Use for 千川初始化、千川授权、同步千川广告主、检查千川 Token、创建商品全域模板、按抖音号查询匹配商品的达人视频、按多个抖音作品链接批量新建或追加商品全域计划、按作品链接删除计划自提素材、创建商品或直播全域计划, or validating official Qianchuan payloads.
---

# QC Plan Monitor

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: qc-plan-monitor

## Scope

This Skill owns the Qianchuan (`qianchuan`) branch:

1. Configure the Qianchuan app and run local OAuth.
2. Refresh Qianchuan tokens and discover authorized advertisers.
3. Create advertiser- and product-bound product all-domain templates.
4. Resolve a visible Douyin ID to its authorized numeric `aweme_id`.
5. Query creator videos through the Qianchuan API and filter them by template products.
6. Validate official product or live all-domain plan payloads.
7. Dry-run or explicitly submit one all-domain plan.
8. Resolve multiple Douyin work links, group product-matched works by creator, then create or append product all-domain plans.
9. Resolve Douyin work links to plan `material_id` values and remove verified custom materials.

Qianchuan reports, strategy, and live templates are not implemented yet. Do not route Qianchuan requests through `ads-plan-monitor`, Marketing templates, Marketing credentials, or the Marketing project/promotion transaction.

## Command Entry

Use the launcher from this Skill root:

```bash
python3 run.py <domain> <action> [options]
```

If the package is installed, `ocean-watch <domain> <action>` is equivalent.

| Request | Command |
| --- | --- |
| Start Qianchuan OAuth | `auth authorize --channel qianchuan` |
| Replace Qianchuan app | `auth set-app --channel qianchuan` |
| Token/account status | `auth status --channel qianchuan` |
| Refresh token | `auth refresh --channel qianchuan` |
| Sync advertisers | `auth sync-accounts --channel qianchuan` |
| List product templates | `qc-templates list` |
| Create product template | `qc-templates create` |
| Migrate product templates | `qc-templates migrate` |
| Query product-matched creator videos | `qc-materials creator-videos` |
| Create all-domain plan | `plans create-qianchuan` |
| Create or append from work links | `plans batch-qianchuan-works` |
| Remove plan materials by work link | `plans remove-qianchuan-work` |

## Development Boundary

- Plugin development modifies tracked source, public examples, docs, and tests only.
- Business details given during development are requirements or fixtures, not permission to persist them.
- Do not read real local credentials, mutate business config, or call real APIs unless the user explicitly requests business execution.
- Never automate the Qianchuan web admin. Use official APIs only.

## Authorization

Qianchuan uses its own app, credential slots, authorization records, and advertiser index. It shares only the local callback URI with Marketing:

```text
http://127.0.0.1:8787/oauth/callback
```

OAuth state is `QC.<nonce>`. Require an exact state and channel match before token exchange or storage. The first authorization opens one local page that collects App ID and Secret together, stores them in the operating-system credential backend, and redirects to official OAuth.

Business commands resolve the Qianchuan authorization bound to the target `advertiser_id`. Never fall back to a Marketing authorization. Use `--auth-account-id` only when multiple Qianchuan authorizations cover the same advertiser.

## Product Template Contract

Qianchuan product templates are independent from Marketing templates.

- `default_qianchuan_product_template` is a creation skeleton and can never create a real plan.
- New business templates use the `qc-templates create` wizard and choose the default skeleton or an existing Qianchuan product template as the source.
- Every business template binds one Qianchuan advertiser, product name, and 1–30 product IDs.
- Display names use `广告主ID-商品全域-产品-商品ID1/商品ID2`.
- Product IDs are deduplicated in input order and enforce the official maximum of 30.
- Defaults are custom bidding, ROI `1.7`, budget `5000`, smart coupon on, long-term delivery, and net payment ROI optimization.
- Do not store `aweme_id`, product channel information, creator IDs, video IDs, image IDs, or creative lists.
- `material_strategy.source_type` is `CREATOR_RUNTIME_QUERY`; creator information and materials belong to the creation run.

Use `plans create-qianchuan --plan-template TEMPLATE_ID` to build a material-free base payload for low-level preflight. It reports `runtime_creator_materials` and blocks template-only submission. Use `plans batch-qianchuan-works` for the complete runtime work-query and material-injection workflow.

## Creator Material Discovery

Query runtime creator videos with the business template and the Douyin ID visible in the Douyin app:

```bash
ocean-watch qc-materials creator-videos \
  --plan-template TEMPLATE_ID \
  --douyin-id DOUYIN_SHOW_ID \
  --creator-name CREATOR_NAME
```

The command first calls `/v1.0/qianchuan/uni_aweme/authorized/get/` with `VIDEO_PROM_GOODS`, `CREATE`, and the supplied search term. It accepts only an exact `aweme_show_id` match, then passes the returned numeric `aweme_id` to `/v1.0/qianchuan/file/video/aweme/get/`.

Query every product in the template separately. Keep only videos returned by the official `filtering.product_id` filter, paginate by cursor, deduplicate repeated works, and record `matched_product_ids`. A fuzzy-only account result, no authorized account, or multiple exact accounts must stop before video querying. Do not persist the creator or material result in the product template. Do not write a file unless the user explicitly requests `--out`.

## Work-Link Batch Creation

Accept one or more Douyin share links with repeated `--work-url` arguments:

```bash
ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL \
  --work-url DOUYIN_WORK_URL
```

Follow only redirects that remain under the official `douyin.com` or `iesdouyin.com` domains, normalize the final `/video/{aweme_item_id}` URL, and deduplicate repeated works. The redirect step is not an advertising API call; all business facts still come from official Qianchuan APIs.

List authorized product creators once. Resolve works in batches of 50 by querying every authorized numeric `aweme_id`, then query each resolved creator again with every template product. Skip invalid links, unauthorized works, disabled creators, product mismatches, unsupported material types, and duplicate input without stopping the batch.

Before writing, list current product all-domain plans and confirm candidates through plan detail. Treat paused plans as existing and deleted plans as absent. The official product all-domain contract allows one plan per creator:

- No plan: create from template delivery settings and the first 100 eligible homepage works.
- Homepage-work creation omits `creative_card` by default and must never send an empty card object. The official field table makes the whole card conditional on merchant account type, while the verified creator-homepage flow accepts omission. If a future account reports the whole card as missing, return that account-specific failure instead of inventing selling points.
- Existing plan: do not change any plan setting; list all plan videos and append only missing `aweme_item_id` values.
- More than one exact plan: fail that creator without choosing one.
- More than 100 works: create once, then append remaining chunks through the dedicated add-material endpoint.

Default to dry-run. Add `--submit` only after explicit online-write permission. Different creators may execute concurrently; one creator is always serialized, and a submit run takes an advertiser-scoped process lock. Return one final summary. Do not emit per-link progress or create a file unless `--out` is explicit.

## Work-Link Material Removal

Remove one or more custom plan materials by Douyin work link:

```bash
ocean-watch plans remove-qianchuan-work \
  --advertiser-id ADVERTISER_ID \
  --ad-id AD_ID \
  --work-url DOUYIN_WORK_URL
```

Resolve and deduplicate the work links, then list all plan video materials with `material_status=ALL`. Match each `aweme_item_id` to the nested `material_info.video_material.material_id`; never send an `aweme_item_id` to the delete endpoint. A work must resolve to exactly one unique material ID and every matching row must use `material_select_type=CUSTOM`. Skip smart-selected materials because the official delete endpoint supports custom materials only.

Default to dry-run. With explicit `--submit`, delete at most 100 material IDs per official request while holding the same advertiser-scoped lock used by create and append operations. Re-query the plan after submission and report success only when every submitted material is `DELETED`. Already-deleted materials are idempotent. Warn that the official API may remove the same material across related creators or products in multi-binding scenarios. Do not modify plan settings or create an output file unless `--out` is explicit.

## All-Domain Plan Creation

The official transaction is:

```text
POST /v1.0/qianchuan/uni_aweme/ad/create/
```

It is one request, not the Marketing project plus promotion transaction. The command accepts exactly one official payload JSON or product-template source:

```bash
ocean-watch plans create-qianchuan \
  --plan-template TEMPLATE_ID \
  --name PLAN_NAME
```

Use `--payload-file`, `--payload-json`, or `--payload-file -` for raw official payloads when needed. Default to dry-run. Add `--submit` only after explicit online-write permission and a clean preflight.

Supported goals:

- `VIDEO_PROM_GOODS`: one to 30 products; optional unique `name`; `aweme_id` depends on merchant account type.
- `LIVE_PROM_GOODS`: requires `aweme_id`; does not support `name`.

Bid rules:

- `SMART_BID_CUSTOM` requires `roi2_goal`.
- `SMART_BID_CONSERVATIVE` rejects `roi2_goal`.
- `budget` is required and supports at most two decimal places.

Block submission before token resolution when validation fails. Success requires both official `code: 0` and `data.ad_id`.

## Official References

- All-domain plan create: `https://open.oceanengine.com/labels/12/docs/1804360384937988`
- Agent advertiser discovery: `https://open.oceanengine.com/labels/12/docs/1697467832592392`
- Product all-domain authorized creators: `GET /v1.0/qianchuan/uni_aweme/authorized/get/`
- Creator videos by product: `https://open.oceanengine.com/labels/12/docs/1697466774382599`
- Product all-domain plan list: `https://open.oceanengine.com/labels/12/docs/1771195810853899`
- Product all-domain plan detail: `https://open.oceanengine.com/labels/12/docs/1804362305657868`
- Product all-domain plan materials: `https://open.oceanengine.com/labels/12/docs/1804363488115850`
- Add product all-domain plan materials: `https://open.oceanengine.com/labels/12/docs/1835232814536707`
- Delete product all-domain plan materials: `https://open.oceanengine.com/labels/12/docs/1804363891396633`

Read `references/official-api-notes.md` for confirmed endpoint and account-expansion details. If local notes conflict with official documentation or official MCP results, use the official source.

## Output And Safety

- Keep official IDs exact; serialize number fields only where the API requires numbers.
- Never print App Secret, Access Token, Refresh Token, auth code, or sensitive MCP URLs.
- Keep dry-run independent of credentials and the HTTP client.
- Show advertiser, goal, product count, budget, bid type, ROI, material counts, blocking fields, and endpoint before submission.
- Batch work-link output must summarize created, appended, already-present, skipped, and failed groups only after the batch finishes.
- Preserve official responses for diagnosis, but never expose stored credentials.
