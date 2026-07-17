---
name: qc-plan-monitor
description: Dedicated 巨量千川 skill for local OAuth, responsible-account lists across Qianchuan and Marketing, token refresh, product templates, creator video discovery, all-domain plan creation, work-link material changes, and official MCP reports. Use for 千川初始化、千川授权、管理我负责的账户、跨渠道查询负责账户消耗、同步千川广告主、检查千川 Token、创建商品全域模板、查询达人视频、批量新建或追加商品全域计划、删除计划素材、查询千川计划消耗和 ROI、创建商品或直播全域计划, or validating official Qianchuan payloads.
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
10. Query product all-domain plan spend, orders, GMV, and ROI through the official Qianchuan MCP.

Qianchuan strategy and live templates are not implemented yet. Do not route Qianchuan requests through `ads-plan-monitor`, Marketing templates, Marketing credentials, or the Marketing project/promotion transaction.

For a generic request to create a `投放模板` that does not name Marketing or Qianchuan, ask for the channel before entering either template-source wizard. Use the shared `templates create` entry without `--channel`; authorization state is displayed but must not silently select a channel or prevent an unauthorized channel from creating a draft template.

Ignore placeholder advertiser IDs during template creation. Validate an entered Qianchuan advertiser against the local Qianchuan advertiser index when authorization exists; if Qianchuan is not yet authorized, allow the template binding but mark it `UNVERIFIED` in the preview and block real delivery until authorization validation succeeds.

## Command Entry

Use the launcher from this Skill root:

```bash
python3 run.py <domain> <action> [options]
```

If the package is installed, `ocean-watch <domain> <action>` is equivalent.

| Request | Command |
| --- | --- |
| Check local environment | `setup doctor --channel qianchuan` |
| Configure private work metadata | `setup work-metadata --endpoint URL --home-config` |
| Start Qianchuan OAuth | `auth authorize --channel qianchuan` |
| Replace Qianchuan app | `auth set-app --channel qianchuan` |
| Token/account status | `auth status --channel qianchuan` |
| Refresh token | `auth refresh --channel qianchuan` |
| Sync advertisers | `auth sync-accounts --channel qianchuan` |
| List product templates | `qc-templates list` |
| List Marketing and Qianchuan templates | `templates list` |
| Show one Marketing or Qianchuan template | `templates show --channel CHANNEL --template TEMPLATE` |
| Create product template | `qc-templates create` |
| Migrate product templates | `qc-templates migrate` |
| Query product-matched creator videos | `qc-materials creator-videos` |
| Create all-domain plan | `plans create-qianchuan` |
| Create or append from work links | `plans batch-qianchuan-works` |
| Remove plan materials by work link | `plans remove-qianchuan-work` |
| Query all-domain plan spend | `qc-reports plans` |
| Manage responsible accounts | `accounts add/list/remove/enable/disable` |
| Query responsible-account spend | `accounts report` |

## Development Boundary

- Plugin development modifies tracked source, public examples, docs, and tests only.
- Business details given during development are requirements or fixtures, not permission to persist them.
- Do not read real local credentials, mutate business config, or call real APIs unless the user explicitly requests business execution.
- Never automate the Qianchuan web admin. Use official APIs only.

## First-Use Environment Check

On a new computer, detect Python before invoking `run.py`: macOS/Linux should try `python3 --version` then `python --version`; Windows should try `py -3 --version`, `python --version`, then `python3 --version`. Require Python `3.9+`. If no supported interpreter exists, stop and ask the user to install Python and reopen Codex.

Then run `setup doctor --channel qianchuan`. Resolve blocking Python, operating-system, secure credential backend, or loopback callback-port checks before Qianchuan OAuth. Codex CLI availability is reported separately and may be a warning when the Skill is already running inside Codex.

## Authorization

Qianchuan uses its own app, credential slots, authorization records, and advertiser index. It shares only the local callback URI with Marketing:

```text
http://127.0.0.1:8787/oauth/callback
```

OAuth state is `QC.<nonce>`. Require an exact state and channel match before token exchange or storage. The first authorization opens one local page that collects App ID and Secret together, stores them in the operating-system credential backend, and redirects to official OAuth.

OAuth starts on first use, not during Plugin installation. Run `auth authorize` and keep the process alive until the browser returns. Register the loopback callback URI in the official console, but never ask the user to open that URI directly. If the browser does not open automatically, rerun with `--print-url --no-open` and open only `start_url`.

Business commands resolve the Qianchuan authorization bound to the target `advertiser_id`. Never fall back to a Marketing authorization. Use `--auth-account-id` only when multiple Qianchuan authorizations cover the same advertiser.

`managed_accounts` is a separate local user preference shared by both Skills. A request for `我负责的账户` resolves enabled records from this registry, not every OAuth-authorized advertiser. Run `accounts report` without a channel filter for concurrent Marketing and Qianchuan results, or filter by the explicitly named channel. Cross-channel spend is additive; use `channel_summaries` for GMV and ROI because each channel uses a different official conversion definition. One account failure must not hide successful accounts. Never persist real registry entries in tracked Plugin files.

## All-Domain Plan Reports

Query plan performance with the advertiser-bound Qianchuan authorization:

```bash
ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

The command uses the official Streamable HTTP MCP at `https://open.oceanengine.com/qianchuan/mcp`. It injects the refreshed local Qianchuan `Access-Token` only in memory and restricts the remote server with `Tool-Range`. Never persist the token in Plugin metadata, Codex MCP configuration, command output, or report files.

Use `qianchuan_report_uni_promotion_data_get_v1` with topic `SITE_PROMOTION_PRODUCT_AD` and dimension `ad_id` as the only financial source. Read each metric from its returned `Value` or `ValueStr`. Separately call `qianchuan_uni_promotion_list_v1` with `VIDEO_PROM_GOODS`, `UNI_PROJECT`, and `status=ALL` only to enrich report rows with plan names, statuses, creators, products, budgets, and ROI targets. Never display or infer money from plan-list `stats_info`; those internal fixed-point values are not report currency values. Use `qianchuan_report_uni_promotion_config_get_v1` to inspect available metric contracts, and do not substitute the standard Qianchuan plan report for all-domain plans.

Default to the current day and ten report rows. `--top 0` returns all report rows. Summaries must use all paged report data, including rows beyond the display limit, and aggregate raw decimal metrics before display rounding. Treat report money values as CNY exactly as returned; do not apply a guessed scale. Fail closed on missing required metrics, invalid pagination, duplicate plan IDs, or malformed numeric values. Request `need_compensate_info=true` from the plan list and include each plan's status, cost-guarantee state and reason, bid mode, ROI target bid, daily budget, spend, actual ROI, GMV, and orders. For `status=ALL`, retain financial rows missing plan-list metadata and expose `metadata_available=false` plus `metadata_missing_count`; a specific status requires complete metadata. Return total spend, plans with spend, orders, GMV, weighted ROI, one-hour settled amount, and weighted one-hour settled ROI. Do not write a file unless `--out` is explicit.

## Product Template Contract

Qianchuan product templates are independent from Marketing templates.

- `default_qianchuan_product_template` is a creation skeleton and can never create a real plan.
- New business templates use the `qc-templates create` wizard and choose the default skeleton or an existing Qianchuan product template as the source.
- Qianchuan business templates have no active/default pointer. Every material query or plan-creation workflow must provide an explicit template ID or confirmed display name.
- Every business template binds one Qianchuan advertiser, product name, and 1–30 product IDs.
- Display names use the shared `渠道-广告账户ID-商品名-商品ID-模版类型` rule: `巨量千川-广告账户ID-商品名-商品ID1/商品ID2-商品全域`.
- Product IDs are deduplicated in input order and enforce the official maximum of 30.
- Defaults are custom bidding, ROI `1.7`, budget `5000`, smart coupon on, long-term delivery, and net payment ROI optimization.
- Do not store `aweme_id`, product channel information, creator IDs, video IDs, image IDs, or creative lists.
- `material_strategy.source_type` is `CREATOR_RUNTIME_QUERY`; creator information and materials belong to the creation run.

Use `templates show --channel qianchuan --template TEMPLATE_ID_OR_NAME` for a complete, read-only single-template query. It returns bindings, delivery settings, material strategy, and readiness from one local config read without credentials or official API calls. Use the same shared command with `--channel marketing` and an exact Marketing template name for Marketing details.

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

The first successful official ownership check stores only the non-sensitive `aweme_item_id`, visible Douyin ID, and numeric `aweme_id` relationship in the local state cache for 30 days. A later preflight or confirmed submission uses that relationship only as a query hint: it must use the official authorization endpoint to resolve the visible Douyin ID exactly, then re-query the hinted creator with the current work and template products. Missing, expired, disabled, or stale hints fall back to the complete official creator scan. Cache read or write failures are non-blocking and must be exposed in `performance.owner_hint_cache`; they never weaken validation or fail an otherwise valid batch. The default bounded concurrency is 8, with an explicit maximum of 10. Only the small targeted authorization, ownership, and product checks may retry official rate-limit code `40100` with bounded backoff; never retry every request in a broad creator scan.

The optional public-link metadata endpoint must come from local config at `integrations.qianchuan_work_metadata.endpoint`; never hard-code, persist in tracked files, or print the endpoint. Configure it with `setup work-metadata --endpoint URL --home-config`. Its response provides `video_info_id`, `author.unique_id`, `author.uid`, and optional `product_info_id`. Send only the public Douyin link; never send advertiser IDs, credentials, template payloads, or local state. Treat author fields as targeted official-query hints. A non-empty `product_info_id` outside the template's product ID set is an immediate product mismatch: skip it before authorization queries and never create or append it. An empty product hint continues to official validation, and a matching hint must still pass the official product-filtered creator-video query. Ignore remote cover, playback URL, avatar, and title for plan creation and persistence. Missing config, resolver failure, or `--no-link-metadata-api` restores safe Douyin redirect plus broad official discovery.

Return `performance` timings for link resolution, credential preparation, material resolution, plan reconciliation, and the whole command. Use these fields when diagnosing latency instead of describing the whole preflight as link parsing. Do not create user-facing result files unless `--out` is explicit; the internal owner-hint cache is the only permitted automatic local optimization state.

Before writing, list current product all-domain plans and confirm candidates through plan detail. Treat paused plans as existing and deleted plans as absent. The official product all-domain contract allows one plan per creator:

The plan list exposes the visible Douyin ID in `room_info.anchor_id`, while plan detail exposes the numeric `aweme_id`. Use both identities to select list candidates, then require the numeric detail `aweme_id` to match before treating a plan as existing. Never compare only one identifier type and never create a new plan merely because the list uses the visible ID.

The plan-list `start_time` and `end_time` describe the returned data period, not the plan creation period. Always use one legal period inside the latest 180 days and traverse every declared page. There is no fixed plan-count cap; do not split the query into older data-period windows and do not stop after an arbitrary number of plans.

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
- Qianchuan MCP business tools: `https://open.oceanengine.com/labels/12/docs/1839622960207943`
- Qianchuan MCP tool list: `https://open.oceanengine.com/labels/12/docs/1847297003631945`
- Qianchuan MCP guide and examples: `https://open.oceanengine.com/labels/12/docs/1849835441833027`

Read `references/official-api-notes.md` for confirmed endpoint and account-expansion details. If local notes conflict with official documentation or official MCP results, use the official source.

## Output And Safety

- Keep official IDs exact; serialize number fields only where the API requires numbers.
- Never print App Secret, Access Token, Refresh Token, auth code, or sensitive MCP URLs.
- Keep dry-run independent of credentials and the HTTP client.
- Show advertiser, goal, product count, budget, bid type, ROI, material counts, blocking fields, and endpoint before submission.
- Present report summaries and rankings as Markdown tables in conversation; JSON remains the CLI boundary.
- Batch work-link output must summarize created, appended, already-present, skipped, and failed groups only after the batch finishes.
- Preserve official responses for diagnosis, but never expose stored credentials.
