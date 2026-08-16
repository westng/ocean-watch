---
name: qc-plan-monitor
description: Dedicated 巨量千川 skill for local OAuth, responsible accounts, token refresh, product/live templates, creator/work/product discovery, all-domain plan creation and updates, material changes, account/plan/product/room/author reports across 全域与乘方, and run inspection. Use for 千川初始化、千川授权、查询或管理用户常用的、负责的、管理的、日常投放范围内的账户（包括口语、简称、错别字和上下文追问）、同步广告主、检查 Token 映射、创建/校验/删除商品或直播全域模板、查询达人/作品/商品/计划/素材、批量新建或追加商品全域计划、删除计划素材、调整计划状态/预算/ROI、按账户/商品/直播间/达人及日期查询全域或乘方消耗和 ROI, or validating official Qianchuan payloads.
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
3. Create advertiser-bound product or live all-domain templates.
4. Resolve a visible Douyin ID to its authorized numeric `aweme_id`.
5. Query creator videos through the Qianchuan API and filter them by template products.
6. Validate official product or live all-domain plan payloads.
7. Dry-run or explicitly submit one all-domain plan.
8. Resolve multiple Douyin work links, group product-matched works by creator, then create or append product all-domain plans.
9. Resolve Douyin work links to plan `material_id` values and remove verified custom materials.
10. Execute Qianchuan reads and guarded writes through the bundled Go CLI and official SDK/REST endpoints.
11. List/search products and inspect plan details, materials, local runs, and authorization mappings.
12. Dry-run or explicitly submit guarded plan status, budget, and ROI updates.
13. Route natural-language account, product, live-room, author, and custom 全域/乘方 report intent to the matching official report endpoint.

Qianchuan live templates and live all-domain creation are implemented; model strategy remains read-only by default. Do not route Qianchuan requests through `ads-plan-monitor`, Marketing templates, Marketing credentials, or the Marketing project/promotion transaction.

For a generic request to create a `投放模板` that does not name Marketing or Qianchuan, ask for the channel before entering either template-source wizard. Use the shared `templates create` entry without `--channel`; authorization state is displayed but must not silently select a channel or prevent an unauthorized channel from creating a draft template.

After Qianchuan is selected, ask whether the template is `商品全域` or `直播全域` before listing source templates. For automation, pass `--template-type product|live` explicitly.

Ignore placeholder advertiser IDs during template creation. Validate an entered Qianchuan advertiser against the local Qianchuan advertiser index when authorization exists; if Qianchuan is not yet authorized, allow the template binding but mark it `UNVERIFIED` in the preview and block real delivery until authorization validation succeeds.

## Command Entry

Use the launcher from this Skill root:

```bash
./run <domain> <action> [options]
# Windows: run.cmd <domain> <action> [options]
```

The launcher selects and executes the bundled Go binary for the current platform. Do not use it for the common MCP-backed reads listed below: template browsing/details, responsible-account membership, local Qianchuan authorization inspection, product search, plan list/detail/material membership, fixed account/plan reports, work-link batch preflight, or preflight snapshot inspection. Use the Plugin-provided MCP tools for those operations. Keep advanced and custom reads on the explicit CLI routes documented below.

| Request | Command |
| --- | --- |
| Check local environment | `setup doctor --channel qianchuan` |
| Start Qianchuan OAuth | `auth authorize --channel qianchuan` |
| Replace Qianchuan app | `auth set-app --channel qianchuan` |
| Token/account status or advertiser mapping | MCP `get_qianchuan_authorization` |
| Refresh token | `auth refresh --channel qianchuan` |
| Sync advertisers | `auth sync-accounts --channel qianchuan` |
| List Marketing and Qianchuan templates | MCP `list_templates` |
| Show one exact Marketing or Qianchuan template | MCP `get_template` |
| List responsible/common accounts | MCP `list_managed_accounts` |
| List/search products | MCP `search_qianchuan_products` |
| List plans | MCP `list_qianchuan_plans` |
| Show plan details or material membership | MCP `get_qianchuan_plan` |
| Query one account aggregate | MCP `report_qianchuan_account` |
| Query all-domain plan spend | MCP `report_qianchuan_plans` |
| Preflight product plans from work links | MCP `preflight_qianchuan_works` |
| Inspect one exact preflight snapshot | MCP `get_qianchuan_preflight` |
| Create product template | `qc-templates create` |
| Migrate product templates | `qc-templates migrate` |
| Create live template | `qc-templates create-live` |
| Validate/delete templates | `templates validate` / `templates delete` |
| Inspect public work link | `qc-materials inspect-work` |
| List authorized creators | `qc-materials authorized-creators` |
| Query product-matched creator videos | `qc-materials creator-videos` |
| Create all-domain plan | `plans create-qianchuan` |
| Submit a confirmed work-link preflight | `plans batch-qianchuan-works --submit --preflight-id ID` |
| Remove plan materials by work link | `plans remove-qianchuan-work` |
| Query legacy material report | `qc-reports materials` |
| Query all-domain post material dimensions | `qc-reports custom --data-topic SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO|SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE|SITE_PROMOTION_PRODUCT_POST_DATA_TITLE|SITE_PROMOTION_PRODUCT_POST_DATA_OTHER` |
| Inspect report topics, dimensions, and metrics | `qc-reports schema [--data-topic TOPIC] [--managed-accounts]` |
| Query a custom 全域/乘方 topic | `qc-reports custom --data-topic TOPIC --dimension DIM --metric METRIC [--advertiser-id ID ...|--managed-accounts]` |
| Query product-dimension performance | `qc-reports products` |
| Query live-room performance | `qc-reports rooms` |
| Query Douyin-author performance | `qc-reports authors` |
| Update status/budget/ROI | `qc-plans update-status/update-budget/update-roi` |
| List/show local batch runs | `runs list` / `runs show` |
| Manage responsible accounts | `accounts add/remove/enable/disable` |
| Query responsible-account spend | `accounts report` |

## Development Boundary

- Plugin development modifies tracked source, public examples, docs, and tests only.
- Business details given during development are requirements or fixtures, not permission to persist them.
- Do not read real local credentials, mutate business config, or call real APIs unless the user explicitly requests business execution.
- Never automate the Qianchuan web admin. Use official APIs only.

## First-Use Environment Check

The bundled Go CLI handles all Qianchuan authorization, account, template, material, plan, report, and local-state logic. Python is used only by public Douyin work metadata resolution through pinned F2 `0.0.1.7`. Require Python `3.10+` before `qc-materials inspect-work`, `plans batch-qianchuan-works`, or another F2-dependent work-link flow. If no supported interpreter exists, stop that flow and ask the user to install Python and reopen Codex.

Then run `setup doctor --channel qianchuan`. Resolve blocking Python `3.10+`, exact F2 `0.0.1.7` package version, operating-system, secure credential backend, or loopback callback-port checks before Qianchuan OAuth. Codex CLI availability is reported separately and may be a warning when the Skill is already running inside Codex.

## Authorization

Qianchuan uses its own app, credential slots, authorization records, and advertiser index. It shares only the local callback URI with Marketing:

```text
http://127.0.0.1:8787/oauth/callback
```

OAuth state is `QC.<nonce>`. Require an exact state and channel match before token exchange or storage. The first authorization opens one local page that collects App ID and Secret together, stores them in the operating-system credential backend, and redirects to official OAuth.

OAuth starts on first use, not during Plugin installation. Run `auth authorize` and keep the process alive until the browser returns. Register the loopback callback URI in the official console, but never ask the user to open that URI directly. If the browser does not open automatically, rerun with `--print-url --no-open` and open only `start_url`.

Business commands resolve the Qianchuan authorization bound to the target `advertiser_id`. Never fall back to a Marketing authorization. Use `--auth-account-id` only when multiple Qianchuan authorizations cover the same advertiser.

Use MCP `get_qianchuan_authorization` with an optional string `advertiser_id` to inspect local advertiser-to-authorization resolution. It reads only local authorization and credential-presence state, never refreshes a Token or calls an official API, and returns token-presence booleans only, never token values. Keep `auth mappings --channel qianchuan` only as an explicit development or diagnostic CLI entry; never use it as an automatic MCP fallback.

`managed_accounts` is a separate local user preference shared by both Skills. Interpret requests about the accounts the user commonly uses, is responsible for, manages, operates, maintains, or normally runs campaigns from as one semantic responsible-account intent, not by exact wording or keyword matching. Recognize colloquial abbreviations such as `常用的户` or `我管的户`, misspellings, omitted nouns, and contextual follow-ups; these examples are illustrative, not an exact or exhaustive keyword list. Never require canonical wording.

Split that semantic intent by requested output, using the full utterance and conversation context rather than exact keywords:

- A membership-only request such as `我负责的账户`, `我常用的账户`, or a contextual equivalent asks which accounts are in scope. Call MCP `list_managed_accounts` during the current turn. It reads enabled local registry records only and must not resolve credentials, refresh a Token, or call an official report API. Set `include_disabled=true` only when the user explicitly asks to include disabled records.
- A performance request mentioning spend, GMV, ROI, orders, performance, a date range, or equivalent metrics asks how those accounts are performing. Run `accounts report` during the current turn. Run without a channel filter unless the user explicitly names Marketing or Qianchuan.

Do not infer a performance request merely because the user asks for their accounts. Do not reuse an earlier conversational answer or cached result when either intent is repeated or paraphrased. Never replace this registry with every OAuth-authorized advertiser.

For membership results, treat `list_managed_accounts` top-level `presentation` as mandatory. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim. Preserve its four columns: channel, account name, advertiser ID, and enabled state. Do not add performance metrics, query status, date range, or failure details.

For performance results, treat `accounts report` top-level `presentation` as mandatory. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim as the complete result. Do not reconstruct it from `accounts` or `summary`, and do not omit, merge, rename, reorder, summarize, or replace its date range, account summary, account rows, per-channel summaries, failure details, or metric-basis section. Cross-channel spend is additive; use `channel_summaries` for GMV and ROI because each channel uses a different official conversion definition. One account failure must not hide successful accounts. Never persist real registry entries in tracked Plugin files or dump raw JSON unless requested.

Qianchuan account performance must call `GET /v1.0/qianchuan/report/uni_promotion/get/`, the official all-domain advertiser-dimension aggregate endpoint documented at `1865675229008199`. Request the account-level `stat_cost`, ROI2 order, GMV, and ROI fields directly. Do not call `qianchuan_report_uni_promotion_data_get_v1`, `/v1.0/qianchuan/uni_promotion/list/`, or any plan-detail endpoint for an account aggregate. Those plan-level interfaces belong only to plan reports and plan operations.

## Unified And Overall Report Intent

Interpret report requests semantically from the full utterance and conversation context. Do not require users to name a command, endpoint, exact metric field, or fixed phrase. Treat the examples below as illustrations rather than an exact or exhaustive keyword list.

First identify the requested subject and scope:

- Keep a request about the performance of the user's responsible/common account set on `accounts report`, with an optional Qianchuan channel filter. Do not replace this multi-account workflow with a single-advertiser `qc-reports` command.
- Use MCP `report_qianchuan_account` with `scope=overall` for one Qianchuan advertiser's fixed account aggregate when the user asks for 乘方, the combined/overall account view, or a view that must include 乘方.
- Use MCP `report_qianchuan_account` with `scope=uni` when the user explicitly limits one advertiser's fixed account aggregate to 全域 and does not ask for 乘方 or a combined view.
- Use `qc-reports products` when performance is grouped by, filtered to, or compared across products. Select `--report-mode uni` for 全域 and `--report-mode overall` for 乘方; preserve an explicit product ID as a report filter. Do not use `qc-products` for spend, GMV, ROI, orders, or other performance data.
- Use MCP `search_qianchuan_products` only for product assets, eligibility, names, inventory, or finding a product ID without performance metrics.
- Use `qc-reports rooms` for a live-room performance subject and `qc-reports authors` for a Douyin account/creator performance subject. If the user supplies only a creator name or visible Douyin ID, resolve one exact authorized numeric `aweme_id` before the author report; never choose a fuzzy or ambiguous creator.
- Use MCP `report_qianchuan_plans` for the fixed individual-plan report and plan-target comparison. Preserve its `presentation.rendered_markdown` verbatim. Use `qc-reports custom` only when the user requests a different topic, dimension, metric set, filter, ordering, or aggregation that the fixed MCP report does not expose.
- Use `qc-reports custom` for 全域投后素材维度 requests, including 素材维度数据, 视频素材数据, 图片素材数据, 标题素材数据, 其他创意, and similar phrases. Resolve the current official topic with `qc-reports schema` when uncertain; default explicit video-material requests to `SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO`, image-material requests to `SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE`, title-material requests to `SITE_PROMOTION_PRODUCT_POST_DATA_TITLE`, and other-creative requests to `SITE_PROMOTION_PRODUCT_POST_DATA_OTHER`. Carry the user's requested dimensions, metrics, dates, sorting, and limits into the custom report.
- Use `qc-reports materials` only when the user explicitly asks for the legacy `/v1.0/qianchuan/report/material/get/` material report or gives legacy material filters such as `material_type`, `material_mode`, or `video_source`. Do not route ordinary 素材维度, 全域素材, 投后素材, or video-material performance requests to `qc-reports materials`.
- Use `qc-reports schema` when the user asks for `数据主题列表`, report topics, dimensions, or metrics. Omit `--data-topic` to fetch the default common Qianchuan topic list; pass explicit topics when named. Add `--managed-accounts` only when the user asks for the locally responsible/common Qianchuan account set, and preserve explicit multiple `--advertiser-id` values for a user-provided multi-account scope.
- Use `qc-reports custom` for any resolved data-topic/dimension/metric combination, not only material reports. It supports one advertiser, repeated/comma-separated `--advertiser-id` values, or `--managed-accounts` for enabled locally responsible Qianchuan accounts. Multi-account custom queries must keep the same `data_topic`, dimensions, metrics, date range, filters, and ordering across accounts; aggregate rows by identical dimension values, sum only additive count/money metrics, and keep ratio, ROI, CPC, CPM, ECPM, rate, and cost-per metrics non-additive unless a command explicitly computes a weighted formula.

Carry explicit dates, date ranges, IDs, 全域/乘方 scope, marketing goal, time granularity, requested metrics, filters, and display limits into the command. Default omitted dates to the current local day. Ask only for a genuinely required unresolved advertiser or dimension identity; do not ask the user to restate natural language as a command.

Read `references/unified-report-routing.md` before executing any advanced `qc-reports schema`, `custom`, `products`, `rooms`, or `authors` request, and before executing any 全域投后素材维度 report. It defines endpoint contracts, required identifiers, topics, pagination, and output boundaries.

## All-Domain Plan Reports

Query the fixed plan-performance view with MCP `report_qianchuan_plans`. The equivalent diagnostic CLI remains:

```bash
ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

The Go runtime queries the official all-domain report config/data endpoints and product all-domain plan list through the Ocean Engine SDK/REST client. Use topic `SITE_PROMOTION_PRODUCT_AD` and dimension `ad_id` as the financial source, reading each metric from the official returned value without extra scaling. Use the plan list with `VIDEO_PROM_GOODS`, `UNI_PROJECT`, and `status=ALL` only to enrich rows with names, statuses, creators, products, budgets, and ROI targets. Never display or infer money from plan-list `stats_info`; those internal fixed-point values are not report currency. Never persist the refreshed access token in Plugin metadata, output, or report files.

Default to the current day and ten report rows. The report-data and plan-metadata calls must use the same requested date range, so both query only the current day when no dates are supplied; never apply a separate historical lookback. `--top 0` returns all report rows. Summaries must use all paged report data, including rows beyond the display limit, and aggregate raw decimal metrics before display rounding. Treat report money values as CNY exactly as returned; do not apply a guessed scale. Fail closed on missing required metrics, invalid pagination, duplicate plan IDs, or malformed numeric values. Request `need_compensate_info=true` from the plan list and include each plan's status, cost-guarantee state and reason, bid mode, ROI target bid, daily budget, spend, actual ROI, GMV, and orders. For `status=ALL`, retain financial rows missing plan-list metadata and expose `metadata_available=false` plus `metadata_missing_count`; a specific status requires complete metadata. Return total spend, plans with spend, orders, GMV, weighted ROI, one-hour settled amount, and weighted one-hour settled ROI. Do not write a file unless `--out` is explicit.

Treat the command's top-level `presentation` object as the default response contract. When `presentation.required=true`, reproduce `presentation.rendered_markdown` as the plan table instead of composing a new table. Do not omit, merge, rename, reorder, or replace its columns with a shorter ranking for brevity, and include `presentation.required_details` outside the table when they are not already visible. Only narrow the table when the user explicitly asks for fewer or different fields in the current request; a generic request for plan spend does not authorize simplification.

## Template Contracts

Qianchuan product templates are independent from Marketing templates.

Use MCP `list_templates` for every natural-language request to find, browse, compare, count, or select local templates. Pass `channel=qianchuan` for a Qianchuan-only request and `channel=all` only for an explicitly cross-channel request. Follow the opaque `next_cursor` until `null` when the full list is required, and preserve the returned string `template_id`, `template_kind`, `status`, and readiness fields. The tool reads only managed local state and does not resolve credentials, call an official API, or refresh authorization.

Use MCP `get_template` for exact template details with `channel=qianchuan` and the stored string `template_id` returned by `list_templates` or explicitly supplied and confirmed by the user. Never pass a display name or fuzzy selector. For an explicitly requested Marketing detail, pass `channel=marketing` and its canonical ID. Return only the tool's whitelisted bindings, delivery settings, material strategy, naming forms, validation issues, and readiness state.

If either MCP tool is unavailable, its dependency is not loaded, the local state changes during pagination, or the call returns an error, explain the stable failure and stop that template read. Never search the repository, run `templates list`/`templates show` or the legacy `qc-templates list` as a silent fallback, and never parse CLI JSON to imitate a tool result. `STATE_CHANGED` permits restarting `list_templates` once from the first page; it does not permit mixing state versions.

- `default_qianchuan_product_template` is a creation skeleton and can never create a real plan.
- New business templates use the `qc-templates create` wizard and choose the default skeleton or an existing Qianchuan product template as the source.
- Qianchuan business templates have no active/default pointer. Every material query or plan-creation workflow must provide an explicit template ID or confirmed display name.
- Every business template binds one Qianchuan advertiser, a full product name, a product short name for plan naming, and 1–30 product IDs.
- Product-template display names are user-defined labels. The wizard requires a non-empty name, while advertiser and product ownership remain exclusively in `bindings`.
- Every product template stores `bindings.product_name` as the full product name and `bindings.product_short_name` as the plan-name label. The create wizard must ask for both separately; template lists and previews expose both so the user can verify the binding.
- Every product template stores a `plan_name_template` used only when creating a new product all-domain plan. Supported placeholders are `product_name`, `product_short_name`, `creator_name`, `aweme_id`, `douyin_id`, `date`, `time`, `datetime`, `month_day`, `type`, and `business`; `{product_name}` always renders the full name, while `{product_short_name}` renders the configured short name. `month_day` renders without zero-padding, for example `8.4`, and the rendered name is limited to 100 weighted characters.
- The default product skeleton uses `{month_day}-{creator_name}-{product_short_name}-{type}-{business}`. When that template creates a new plan, pass the per-run values with `--plan-type` and `--business`; both are required only when referenced by the selected template. Existing-plan material append never requires them and never changes the existing plan name.
- Schema v4 templates first receive `{product_name}-{creator_name}-{datetime}` to preserve their previous behavior. Schema v5 to v6 upgrades the default skeleton to the five-part full-name format, and Schema v7 completes missing business-template patterns. Schema v8 adds `product_short_name`: existing business templates initially copy their full product name into the short-name field, the prior default five-part pattern switches to the short-name placeholder without changing the rendered value, and explicitly customized patterns remain unchanged.
- Before dry-run output or submission, remove Emoji, Unicode symbol, and control characters from rendered or raw product-plan names, normalize whitespace, then enforce the official 100-weighted-character limit. Stop before credentials if the cleaned name is empty.
- Product IDs are deduplicated in input order and enforce the official maximum of 30.
- Defaults are custom bidding, ROI `1.7`, budget `5000`, smart coupon on, long-term delivery, and net payment ROI optimization.
- Do not store `aweme_id`, product channel information, creator IDs, video IDs, image IDs, or creative lists.
- `material_strategy.source_type` is `CREATOR_RUNTIME_QUERY`; creator information and materials belong to the creation run.

Use `plans create-qianchuan --plan-template TEMPLATE_ID` to build a material-free base payload for low-level preflight. It reports `runtime_creator_materials` and blocks template-only submission. Use `plans batch-qianchuan-works` for the complete runtime work-query and material-injection workflow.

Qianchuan live templates are separate:

- `default_qianchuan_live_template` is a non-business skeleton.
- Business templates bind one advertiser, creator/live-account name, and numeric `aweme_id`.
- Names are `巨量千川-广告账户ID-直播账号名-aweme_id-直播全域`.
- The default uses conservative bidding, budget `5000`, long-term delivery, and smart material selection.
- Live templates never persist product IDs, work IDs, or manual materials.
- Create with `plans create-qianchuan --live-template TEMPLATE_ID`; live plans reject `--name`.

Use `templates validate` for both Qianchuan template kinds. `templates delete` defaults to dry-run and requires `--submit`; default skeletons are not business deletion targets.

## Creator Material Discovery

Use `qc-materials inspect-work --work-url URL` to inspect and deduplicate public work links without touching a plan. It resolves public metadata only through the bundled read-only F2 CLI and must keep F2 Cookie, stderr, and raw exceptions out of every output or error. Use `qc-materials authorized-creators --advertiser-id ID [--query VALUE]` for the official product-all-domain authorization list.

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

For an ordinary natural-language request to validate or prepare product all-domain plans from work links, call MCP `preflight_qianchuan_works` with the exact product template ID and all user work rows. Do not launch `plans batch-qianchuan-works` for this preflight and do not silently fall back to CLI if the MCP tool is unavailable. The MCP preflight never creates or modifies an official plan, but it does call official read endpoints, may refresh the advertiser-bound Token, may update the non-sensitive owner-hint cache, and stores a short-lived local snapshot. Preserve its top-level `presentation` contract exactly as required below.

Use MCP `get_qianchuan_preflight` when the user asks to inspect or reconfirm an exact `preflight_id`. It is local-only: it must not resolve credentials, refresh a Token, or call an official endpoint. It returns only expiry, template/product summary, eligible/skipped counts, and stable creator `create|append` decisions. It must never expose source URLs, template payloads, authorization selectors, raw journals, or fingerprints.

MCP shortens the intent-to-Application-Service path and avoids CLI argument/stdout translation; it does not remove the official authorization, ownership, product-match, current-plan, and material-diff reads required for a trustworthy preflight. Diagnose remaining latency from `performance`, not from transport choice alone.

Accept one or more Douyin share links or complete user rows with repeated `--work-url` arguments:

```bash
ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url $'DOUYIN_WORK_URL\tPLAN_TYPE\tBUSINESS_OWNER' \
  --work-url DOUYIN_WORK_URL
```

Treat each argument as one user row. Extract a Douyin URL from a plain link, Markdown link target, or complete share command. If Tab-separated columns exist, use the last two columns as that row's optional plan type and business owner; one extra column is the type. Omit absent type or business segments from the rendered plan name instead of prompting for them. `--plan-type` and `--business` remain optional whole-command fallbacks for unstructured rows. Do not persist these runtime values back into the template.

Follow only redirects that remain under the official `douyin.com` or `iesdouyin.com` domains, normalize the final `/video/{aweme_item_id}` URL, and deduplicate repeated works. For `v.douyin.com`, use only the first path segment as the share code so adjacent date markers or command noise without whitespace cannot become part of the URL. The redirect step is not an advertising API call; all business facts still come from official Qianchuan APIs.

Resolve each work only from a valid numeric creator UID and visible Douyin ID supplied by the bundled F2 CLI or the 30-day owner cache. Pass the visible Douyin ID to the official authorization endpoint as `search_key_words`, require the returned row's numeric `aweme_id` to equal the hinted UID, verify ownership in batches of 50 under that exact numeric creator, then query the same creator with the template products. Never pass the numeric UID as `search_key_words`, list every authorized creator, or scan unrelated creators during batch plan creation. Skip invalid links, missing creator identities, unavailable creators, creator/work mismatches, product mismatches, unsupported material types, and duplicate input without stopping the batch.

The first successful official ownership check stores only the non-sensitive `aweme_item_id`, visible Douyin ID, and numeric `aweme_id` relationship in the local state cache for 30 days. A later preflight uses that relationship only as a query hint: it must query the official authorization endpoint with the cached visible Douyin ID, require one returned row whose numeric `aweme_id` matches the cached UID, then re-query the numeric creator with the current work and template products. The returned visible ID may differ after an account rename without invalidating a matching numeric identity. Missing, expired, disabled, unavailable, or stale identity hints skip that work with an explicit reason; a numeric-only hint must not be placed in `search_key_words`, trigger an unfiltered authorization-list request, or scan other creators. An official targeted-query or identity-check error must be reported as an incomplete query, not as proof that the work is unauthorized or mismatched. Cache read or write failures are non-blocking and must be exposed in `performance.owner_hint_cache`; they never weaken validation or fail an otherwise valid batch. The legacy `broad_scan_work_count` diagnostic remains present for output compatibility but is always zero. Batch preflight uses one command-scoped bounded read pool for official plan pages, plan details, plan materials, targeted authorization, ownership, and product checks. `--concurrency` controls the maximum number of in-flight reads for that command, defaults to 8, and has an explicit maximum of 10; it is not an endpoint QPS target or a QPS policy table. Transient `40100` or HTTP `429` handling retries and cools down only the failed request within existing bounded retry rules.

Invoke the pinned F2 `0.0.1.7` integration once for all resolved work IDs through the current Python interpreter's read-only module CLI. Validate that exact version before querying. Never invoke F2's downloader command, create its database, download media, or read browser cookies automatically. The CLI may read `OCEAN_WATCH_F2_DOUYIN_COOKIE` from the local process environment; when it is absent, use F2's own `TokenManager.gen_ttwid()` visitor initialization. It must run in a temporary directory, emit one JSON document only, and map F2's raw response to the stable `code/message/data` contract with `data.author`, `data.product`, and `data.video`. Query unique works through one shared read-only F2 HTTP connection pool with bounded concurrency, an 8-second per-work deadline, one retry for only the failed first-pass works, and a 20-second overall metadata deadline; preserve completed rows when a slow work reaches either deadline. Return compact first-pass, retry, slowest-work, timeout, and total-duration diagnostics. Document and present this design only as the current F2 contract. Do not pass a Cookie on the command line, persist it, or print F2 stderr. The mapped product uses the first item in `aweme_detail.anchor_info.extra` and exposes `product_info_id`, `product_info_img`, and `product_info_name`; use a non-empty product ID only as an early mismatch hint. Treat the F2 identity as an untrusted public identity hint, and treat every other F2 field as an untrusted public hint: they cannot prove authorization, ownership, product match, or deliverability. Always perform the same official targeted authorization, ownership, and product checks before creation. F2 unavailable, visitor initialization failure, timeout, network failure, malformed output, or an incomplete identity must remain a compact per-work warning and continue to the 30-day cache; otherwise skip only that work. Batch creation never restores broad official creator discovery.

For a new plan, render the default name as `M.D-creator-product[-type][-business]`: `M.D` is the unpadded creation month and day, creator comes from the F2 nickname, and product comes from the selected template. Type and business are present only when the user row provides them. If a name pattern needs `creator_name` but F2 did not return a nickname, stop creation instead of falling back to an official-account label. All eligible rows grouped under one creator must agree on type, business, and the F2 nickname; otherwise stop that creator in preflight instead of choosing a value silently.

Return `performance` timings for link resolution, credential preparation, material resolution, plan reconciliation, and the whole command. Use these fields when diagnosing latency instead of describing the whole preflight as link parsing. Do not create user-facing result files unless `--out` is explicit; the internal owner-hint cache is the only permitted automatic local optimization state.

Before writing, list current product all-domain plans and confirm candidates through plan detail. Treat paused plans as existing and deleted plans as absent. Resolve an existing plan by creator identity plus the products matched by that creator's verified materials:

The plan list exposes the visible Douyin ID in `room_info.anchor_id`, while plan detail exposes the numeric `aweme_id`. Use both identities to select list candidates, then require the numeric detail `aweme_id` to match before treating a plan as existing. Never compare only one identifier type and never create a new plan merely because the list uses the visible ID.

The plan-list `start_time` and `end_time` describe the returned data period, not the plan creation period. For batch work-link reconciliation, set both to the current local date (`00:00:00` through `23:59:59`) because this lookup only decides whether the current batch should create a plan or append materials. Traverse every declared page for that day and do not stop after an arbitrary number of plans. Start this current-day scan once credentials and the advertiser lock are ready, without waiting for F2, then reuse the completed inventory after verified works have been grouped. One command performs exactly one logical plan scan; 1, 50, or more input links must not multiply it or trigger one scan per creator.

Serialize the complete official-query and reconciliation phase for the same advertiser in both dry-run and submit mode. Use the same `qianchuan-advertiser-{advertiser_id}.lock` for batch creation/append, material deletion, plan creation, and status/budget/ROI updates. Within one batch command, all official reads share the command-scoped `--concurrency` pool and do not use the legacy fixed 250 ms dispatch interval or cross-endpoint cooldown. The advertiser lock still serializes separate batch commands and every write remains serial. Other Qianchuan commands retain their existing request-control behavior unless their own contract says otherwise.

For `plans batch-qianchuan-works` and `plans remove-qianchuan-work`, do not impose a cumulative local request cap. Request counts remain diagnostic and must never fail a business task merely because they exceed a local threshold. Continue respecting advertiser serialization, endpoint page-size limits, complete pagination, bounded retry rules, explicit cancellation, and genuine official or business errors.

Retry transient `40100` rate limits, `51010` service timeouts, retryable transport failures, and explicit RPC timeouts with bounded jittered backoff while reading the product all-domain plan list and candidate plan details. A failed list page must retry that same page without restarting the completed portion of the current-day scan. Do not retry non-transient business errors or any write request through this read retry path.

- Filter every creator candidate by intersecting its detail-derived `product_ids` with the creator group's verified-material `matched_product_ids` before deciding the action.
- No product-matched plan: create from template delivery settings and the first 100 eligible homepage works.
- Homepage-work creation omits `creative_card` by default and must never send an empty card object. The official field table makes the whole card conditional on merchant account type, while the verified creator-homepage flow accepts omission. If a future account reports the whole card as missing, return that account-specific failure instead of inventing selling points.
- Existing plan: do not change any plan setting; list all plan videos and append only missing `aweme_item_id` values.
- More than one product-matched plan after filtering: fail that creator with `multiple_existing_plans` without choosing one. Never report this ambiguity from creator matches that bind only unrelated products.
- More than 100 works: create once, then append remaining chunks through the dedicated add-material endpoint.

Default to dry-run. Add `--submit` only after explicit online-write permission. Link/F2 resolution and credential preparation start concurrently; once credentials are ready, the current-plan scan starts without waiting for F2. Official read tasks share the command-scoped bounded pool, while writes remain serial. Both dry-run and submit take the advertiser-scoped process lock around official validation and reconciliation. Return one final summary. Do not emit per-link progress or create a file unless `--out` is explicit.

When a dry-run has at least one `would_create` or `would_append` creator group, it stores a minimal short-lived snapshot in the managed operation journal and returns `preflight_id` plus `expires_at`. The snapshot expires after 30 minutes or at the end of the current `Asia/Shanghai` business day, whichever comes first. It contains only the template digest and the minimum verified creator, work, product, and expected-action data needed for a safe write; it must not contain Tokens, Cookies, source URLs, raw F2 responses, or raw official responses. A dry-run with no eligible write action does not return a submit-capable snapshot.

After explicit online-write permission, submit the confirmed snapshot with `plans batch-qianchuan-works --submit --preflight-id ID`. There is no `submit_qianchuan_preflight` MCP tool yet. `--preflight-id` requires `--submit` and cannot be combined with `--plan-template`, `--work-url`, `--plan-type`, or `--business`. The submit path validates the journal, expiry, fingerprint, and current template digest, resolves the advertiser-bound credential again, holds the existing advertiser lock, rescans current plans once, and queries only the material membership needed for the current diff. It must not repeat link redirects, F2, targeted creator authorization, ownership checks, or product checks. If a creator's create/append action or append target changed, mark only that creator `preflight_changed`; continue other unchanged creator jobs serially. Newly present append materials are idempotently skipped. Preserve the existing Guard, `OnceDispatcher`, operation keys, and unknown-write readback reconciliation.

### Mandatory Batch Completion Response

After every `plans batch-qianchuan-works` result, treat the top-level `presentation` object as the mandatory user-facing completion contract. When `presentation.required=true`, perform these steps in order:

1. Output `presentation.rendered_markdown` verbatim as the main result table. Do not reconstruct it from `counts`, `results`, or a model-selected subset.
2. Preserve exactly these five headers and this order: `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`.
3. Do not omit, rename, merge, reorder, summarize, or replace these columns. In particular, never substitute a table whose columns are `处理方式`, `状态`, `数量`, `成功数`, `失败数`, `失败原因`, or similar operational summaries.
4. Keep `skipped`, `query_failures`, and `failed_results` outside the five-column table. Show them after the table when non-empty, following `presentation.required_details`; they never become replacement columns.
5. Even when there are no successful rows, output the five-column header from `presentation.rendered_markdown`, then explain the empty, skipped, or failed result outside the table. Never invent IDs or successful rows.

The five-column table is the default for both dry-run previews and submitted batch completion. A dry-run may show `—` for an unknown plan ID. For `素材ID`, the command prefers the official `material_id` and explicitly falls back to the creation `aweme_item_id` only when the official material ID is absent. Brevity, a large batch, partial failure, or conversational wording is not permission to change this format. Only a direct user request in the current turn that explicitly asks to suppress the table or names different columns may override it; never infer an override.

Use MCP `search_qianchuan_products` for the official selectable-product endpoint. Use MCP `list_qianchuan_plans` for plan metadata and MCP `get_qianchuan_plan` with `include_materials=true` for exact plan details and material membership. Never interpret plan-list `stats_info` as report currency.

## Work-Link Material Removal

Remove one or more custom plan materials by Douyin work link:

```bash
ocean-watch plans remove-qianchuan-work \
  --advertiser-id ADVERTISER_ID \
  --ad-id AD_ID \
  --work-url DOUYIN_WORK_URL
```

Resolve and deduplicate the work links, then list all plan video materials with `material_status=ALL`. Match each `aweme_item_id` to the nested `material_info.video_material.material_id`; never send an `aweme_item_id` to the delete endpoint. A work must resolve to exactly one unique material ID and every matching row must use `material_select_type=CUSTOM`. Skip smart-selected materials because the official delete endpoint supports custom materials only.

Default to dry-run. Hold the same advertiser-scoped lock used by create and append operations during dry-run detail/material reconciliation and submitted deletion. With explicit `--submit`, delete at most 100 material IDs per official request. Re-query the plan after submission and report success only when every submitted material is `DELETED`. Already-deleted materials are idempotent. Warn that the official API may remove the same material across related creators or products in multi-binding scenarios. Do not modify plan settings or create an output file unless `--out` is explicit.

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

For raw official payloads, normalize decimal-string IDs before dry-run output or submission, including top-level `aweme_id`, product creative `product_id`, and nested `aweme_item_id` values in video and blocked-video material lists. Invalid IDs must block before credential resolution or an official request.

Bid rules:

- `SMART_BID_CUSTOM` requires `roi2_goal`.
- `SMART_BID_CONSERVATIVE` rejects `roi2_goal`.
- `budget` is required and supports at most two decimal places.

Block submission before token resolution when validation fails. Success requires both official `code: 0` and `data.ad_id`.

## Plan And Material Operations

For the common read tools in the command table, require the corresponding MCP capability in the current session. If a required tool is unavailable, explain that the installed Plugin snapshot or current Host session does not expose it and stop that read. Do not run the equivalent CLI command, parse CLI JSON, search local state, or reconstruct a result as a silent fallback. This fail-closed rule does not prohibit the explicitly advanced CLI report routes below, responsible-account mutations, authorization refresh/sync, plan writes, or local run inspection.

Use `qc-reports schema` with no `--data-topic` for a default common Qianchuan data-topic list, or with one or more explicit topics to inspect official dimensions and metrics. It can run for one account, multiple explicit advertiser IDs, or `--managed-accounts`; preserve partial successes and report failed accounts separately.

Use `qc-reports custom` for all-domain and overall data-topic reports through `/v1.0/qianchuan/report/uni_promotion/data/get/`, including product, plan, material, title, other-creative, and other schema-resolved topics. Single-account output preserves the historical `rows` contract. Multi-account output aggregates by the requested dimension tuple and exposes per-account results and failures; do not replace it with `accounts report`, because `accounts report` is only for the fixed responsible-account spend summary. For all-domain post material dimension reports, do not use `qc-reports materials`. The common product all-domain material topics are `SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO`, `SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE`, `SITE_PROMOTION_PRODUCT_POST_DATA_TITLE`, and `SITE_PROMOTION_PRODUCT_POST_DATA_OTHER`; `OVERALL_ROI_PRODUCT_MATERIAL` is the combined overall product-material topic. Query `qc-reports schema` first when the exact topic, dimension, or metric is not explicit.

Use `qc-reports materials` only for the legacy `/v1.0/qianchuan/report/material/get/` report. Paginate every declared page; `--top` limits display only, while summaries use all fetched rows. Use official material filters and do not substitute plan-list stats. A custom field set that omits GMV or order metrics must return those summaries as `null`, not zero. Do not use this legacy command for ordinary 全域素材维度 or video-material post reports.

Plan setting commands are fixed: `qc-plans update-status`, `update-budget`, and `update-roi`. Run them without `--submit` first and show the exact IDs, endpoint, and payload. Submit only the confirmed scope while holding the advertiser lock. Treat any failed result row as command failure. Qianchuan `DELETE` requires both `--submit` and `--confirm-delete`.

Use `runs list/show` only for Plugin-managed journals under the local state root. Never accept arbitrary paths or infer online state from a journal.

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
- Qianchuan unified and overall reports: `https://open.oceanengine.com/labels/12/docs/1824289224504835`

Read `references/official-api-notes.md` for confirmed endpoint and account-expansion details. If local notes conflict with current official documentation or an official API response, use the current official source.

## Output And Safety

- Keep official IDs exact; serialize number fields only where the API requires numbers.
- Never print App Secret, Access Token, Refresh Token, auth code, or sensitive request headers.
- Keep only the single-plan payload/template `plans create-qianchuan` dry-run independent of credentials and the HTTP client. Batch work-link and material-removal dry-runs require advertiser-bound credentials and official read APIs for ownership, product, plan, and material reconciliation; they must never write.
- Never print or persist `OCEAN_WATCH_F2_DOUYIN_COOKIE`, F2 stderr, or raw F2 exceptions.
- Show advertiser, goal, product count, budget, bid type, ROI, material counts, blocking fields, and endpoint before submission.
- Present report summaries and rankings as Markdown tables in conversation; JSON remains the CLI boundary.
- Batch work-link output must follow `Mandatory Batch Completion Response`: reproduce the required five-column `presentation.rendered_markdown` only after the batch finishes, then summarize skipped and failed details outside that table.
- Preserve official responses for diagnosis, but never expose stored credentials.
