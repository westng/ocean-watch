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
10. Prefer verified official MCP tools for supported Qianchuan reads and guarded writes, with OpenAPI fallback where safe.
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
| Advertiser authorization mapping | `auth mappings --channel qianchuan` |
| Refresh token | `auth refresh --channel qianchuan` |
| Sync advertisers | `auth sync-accounts --channel qianchuan` |
| List product templates | `qc-templates list` |
| List Marketing and Qianchuan templates | `templates list` |
| Show one Marketing or Qianchuan template | `templates show --channel CHANNEL --template TEMPLATE` |
| Create product template | `qc-templates create` |
| Migrate product templates | `qc-templates migrate` |
| List/create live templates | `qc-templates list-live` / `qc-templates create-live` |
| Validate/delete templates | `templates validate` / `templates delete` |
| Inspect public work link | `qc-materials inspect-work` |
| List authorized creators | `qc-materials authorized-creators` |
| Query product-matched creator videos | `qc-materials creator-videos` |
| List/search products | `qc-products list` / `qc-products search` |
| List/show plan materials | `qc-plans list/show/materials` |
| Create all-domain plan | `plans create-qianchuan` |
| Create or append from work links | `plans batch-qianchuan-works` |
| Remove plan materials by work link | `plans remove-qianchuan-work` |
| Query all-domain plan spend | `qc-reports plans` |
| Query material performance | `qc-reports materials` |
| Query account aggregate including 乘方 | `qc-reports account` |
| Query only 全域 account aggregate | `qc-reports uni-account` |
| Inspect report topics, dimensions, and metrics | `qc-reports schema` |
| Query a custom 全域/乘方 topic | `qc-reports custom` |
| Query product-dimension performance | `qc-reports products` |
| Query live-room performance | `qc-reports rooms` |
| Query Douyin-author performance | `qc-reports authors` |
| Update status/budget/ROI | `qc-plans update-status/update-budget/update-roi` |
| List/show local batch runs | `runs list` / `runs show` |
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

Use `auth mappings --channel qianchuan [--advertiser-id ID]` to verify advertiser-to-authorization resolution. The output contains token-presence booleans only, never token values.

`managed_accounts` is a separate local user preference shared by both Skills. Interpret requests about the accounts the user commonly uses, is responsible for, manages, operates, maintains, or normally runs campaigns from as one semantic responsible-account intent, not by exact wording or keyword matching. Recognize colloquial abbreviations such as `常用的户` or `我管的户`, misspellings, omitted nouns, and contextual follow-ups; these examples are illustrative, not an exact or exhaustive keyword list. Never require canonical wording.

Split that semantic intent by requested output, using the full utterance and conversation context rather than exact keywords:

- A membership-only request such as `我负责的账户`, `我常用的账户`, or a contextual equivalent asks which accounts are in scope. Run `accounts list` during the current turn. It reads enabled local registry records only and must not resolve credentials, refresh a Token, or call an official report API. Use `--all` only when the user explicitly asks to include disabled records.
- A performance request mentioning spend, GMV, ROI, orders, performance, a date range, or equivalent metrics asks how those accounts are performing. Run `accounts report` during the current turn. Run without a channel filter unless the user explicitly names Marketing or Qianchuan.

Do not infer a performance request merely because the user asks for their accounts. Do not reuse an earlier conversational answer or cached result when either intent is repeated or paraphrased. Never replace this registry with every OAuth-authorized advertiser.

For membership results, treat `accounts list` top-level `presentation` as mandatory. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim. Preserve its four columns: channel, account name, advertiser ID, and enabled state. Do not add performance metrics, query status, date range, or failure details.

For performance results, treat `accounts report` top-level `presentation` as mandatory. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim as the complete result. Do not reconstruct it from `accounts` or `summary`, and do not omit, merge, rename, reorder, summarize, or replace its date range, account summary, account rows, per-channel summaries, failure details, or metric-basis section. Cross-channel spend is additive; use `channel_summaries` for GMV and ROI because each channel uses a different official conversion definition. One account failure must not hide successful accounts. Never persist real registry entries in tracked Plugin files or dump raw JSON unless requested.

Qianchuan account performance must call `GET /v1.0/qianchuan/report/uni_promotion/get/`, the official all-domain advertiser-dimension aggregate endpoint documented at `1865675229008199`. Request the account-level `stat_cost`, ROI2 order, GMV, and ROI fields directly. Do not call `qianchuan_report_uni_promotion_data_get_v1`, `/v1.0/qianchuan/uni_promotion/list/`, or any plan-detail endpoint for an account aggregate. Those plan-level interfaces belong only to plan reports and plan operations.

## Unified And Overall Report Intent

Interpret report requests semantically from the full utterance and conversation context. Do not require users to name a command, endpoint, exact metric field, or fixed phrase. Treat the examples below as illustrations rather than an exact or exhaustive keyword list.

First identify the requested subject and scope:

- Keep a request about the performance of the user's responsible/common account set on `accounts report`, with an optional Qianchuan channel filter. Do not replace this multi-account workflow with a single-advertiser `qc-reports` command.
- Use `qc-reports account` for one Qianchuan advertiser's account aggregate when the user asks for 乘方, the combined/overall account view, or a view that must include 乘方. Default `adlab_scene=OVERALL_PROJECT`; pass `data_period` only when its meaning is requested and the scene supports it.
- Use `qc-reports uni-account` when the user explicitly limits one advertiser's account aggregate to 全域 and does not ask for 乘方 or a combined view.
- Use `qc-reports products` when performance is grouped by, filtered to, or compared across products. Select `--report-mode uni` for 全域 and `--report-mode overall` for 乘方; preserve an explicit product ID as a report filter. Do not use `qc-products` for spend, GMV, ROI, orders, or other performance data.
- Use `qc-products list/search` only for product assets, eligibility, names, inventory, or finding a product ID without performance metrics.
- Use `qc-reports rooms` for a live-room performance subject and `qc-reports authors` for a Douyin account/creator performance subject. If the user supplies only a creator name or visible Douyin ID, resolve one exact authorized numeric `aweme_id` before the author report; never choose a fuzzy or ambiguous creator.
- Use `qc-reports plans` for individual plan rows and plan-target comparison, and `qc-reports materials` for material performance. Do not substitute the account, product, room, or author aggregate for those subjects.
- Use `qc-reports schema` when the user asks what report topics, dimensions, or metrics are available. Use `qc-reports custom` only when a nonstandard topic/dimension/metric combination is explicit or has been resolved from the schema.

Carry explicit dates, date ranges, IDs, 全域/乘方 scope, marketing goal, time granularity, requested metrics, filters, and display limits into the command. Default omitted dates to the current local day. Ask only for a genuinely required unresolved advertiser or dimension identity; do not ask the user to restate natural language as a command.

Read `references/unified-report-routing.md` before executing any `qc-reports account`, `uni-account`, `schema`, `custom`, `products`, `rooms`, or `authors` request. It defines endpoint contracts, required identifiers, topics, pagination, and output boundaries.

## MCP Preference And Capability Check

MCP is an optional acceleration and capability surface, not a setup prerequisite. If the user has configured it, prefer MCP for a Qianchuan remote operation only after confirming that the exact tool is present in the current runtime inventory and that its current input schema matches the operation. Read `references/mcp-capability-routing.md` before choosing an MCP tool; it contains the supported Plugin-operation intersection and the read/write fallback rules.

Use runtime `tools/list` as the authority, not a remembered or static tool list. When the current tool inventory or schema is not already visible, use `mcp capabilities` and `mcp capabilities --tool TOOL_NAME`. Never infer parameters from a tool name. Keep local configuration, OAuth browser flows, credential persistence, templates, responsible-account registry operations, caches, journals, and work-link resolution in the bundled CLI.

Reads may fall back to the existing OpenAPI command after a missing tool, schema mismatch, or pre-dispatch MCP failure. Writes must retain the existing dry-run, explicit confirmation, advertiser binding, locking, result validation, and post-write verification. If an MCP write may have been dispatched but its result is unknown, never retry through OpenAPI until current state has been queried and reconciled. Do not replace a multi-step batch or resumable journal workflow with isolated MCP calls.

## All-Domain Plan Reports

Query plan performance with the advertiser-bound Qianchuan authorization:

```bash
ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

The command uses the official Streamable HTTP MCP at `https://open.oceanengine.com/qianchuan/mcp`. It injects the refreshed local Qianchuan `Access-Token` only in memory and restricts the remote server with `Tool-Range`. This path already satisfies the MCP preference rule for its three report tools. Never persist the token in Plugin metadata, Codex MCP configuration, command output, or report files.

Use `qianchuan_report_uni_promotion_data_get_v1` with topic `SITE_PROMOTION_PRODUCT_AD` and dimension `ad_id` as the only financial source. Read each metric from its returned `Value` or `ValueStr`. Separately call `qianchuan_uni_promotion_list_v1` with `VIDEO_PROM_GOODS`, `UNI_PROJECT`, and `status=ALL` only to enrich report rows with plan names, statuses, creators, products, budgets, and ROI targets. Never display or infer money from plan-list `stats_info`; those internal fixed-point values are not report currency values. Use `qianchuan_report_uni_promotion_config_get_v1` to inspect available metric contracts, and do not substitute the standard Qianchuan plan report for all-domain plans.

Default to the current day and ten report rows. The report-data and plan-metadata calls must use the same requested date range, so both query only the current day when no dates are supplied; never apply a separate historical lookback. `--top 0` returns all report rows. Summaries must use all paged report data, including rows beyond the display limit, and aggregate raw decimal metrics before display rounding. Treat report money values as CNY exactly as returned; do not apply a guessed scale. Fail closed on missing required metrics, invalid pagination, duplicate plan IDs, or malformed numeric values. Request `need_compensate_info=true` from the plan list and include each plan's status, cost-guarantee state and reason, bid mode, ROI target bid, daily budget, spend, actual ROI, GMV, and orders. For `status=ALL`, retain financial rows missing plan-list metadata and expose `metadata_available=false` plus `metadata_missing_count`; a specific status requires complete metadata. Return total spend, plans with spend, orders, GMV, weighted ROI, one-hour settled amount, and weighted one-hour settled ROI. Do not write a file unless `--out` is explicit.

Treat the command's top-level `presentation` object as the default response contract. When `presentation.required=true`, reproduce `presentation.rendered_markdown` as the plan table instead of composing a new table. Do not omit, merge, rename, reorder, or replace its columns with a shorter ranking for brevity, and include `presentation.required_details` outside the table when they are not already visible. Only narrow the table when the user explicitly asks for fewer or different fields in the current request; a generic request for plan spend does not authorize simplification.

## Template Contracts

Qianchuan product templates are independent from Marketing templates.

- `default_qianchuan_product_template` is a creation skeleton and can never create a real plan.
- New business templates use the `qc-templates create` wizard and choose the default skeleton or an existing Qianchuan product template as the source.
- Qianchuan business templates have no active/default pointer. Every material query or plan-creation workflow must provide an explicit template ID or confirmed display name.
- Every business template binds one Qianchuan advertiser, product name, and 1–30 product IDs.
- Product-template display names are user-defined labels. The wizard requires a non-empty name, while advertiser and product ownership remain exclusively in `bindings`.
- Every product template stores a `plan_name_template` used only when creating a new product all-domain plan. Supported placeholders are `product_name`, `creator_name`, `aweme_id`, `douyin_id`, `date`, `time`, `datetime`, `month_day`, `type`, and `business`; `month_day` renders without zero-padding, for example `8.4`, and the rendered name is limited to 100 weighted characters.
- The default product skeleton uses `{month_day}-{creator_name}-{product_name}-{type}-{business}`. When that template creates a new plan, pass the per-run values with `--plan-type` and `--business`; both are required only when referenced by the selected template. Existing-plan material append never requires them and never changes the existing plan name.
- Schema v4 templates first receive `{product_name}-{creator_name}-{datetime}` to preserve their previous behavior. Schema v5 to v6 upgrades the default skeleton to the new five-part format. Schema v7 also upgrades business templates that are missing a pattern or still use that exact legacy default; explicitly customized patterns remain unchanged.
- Before dry-run output or submission, remove Emoji, Unicode symbol, and control characters from rendered or raw product-plan names, normalize whitespace, then enforce the official 100-weighted-character limit. Stop before credentials if the cleaned name is empty.
- Product IDs are deduplicated in input order and enforce the official maximum of 30.
- Defaults are custom bidding, ROI `1.7`, budget `5000`, smart coupon on, long-term delivery, and net payment ROI optimization.
- Do not store `aweme_id`, product channel information, creator IDs, video IDs, image IDs, or creative lists.
- `material_strategy.source_type` is `CREATOR_RUNTIME_QUERY`; creator information and materials belong to the creation run.

Use `templates show --channel qianchuan --template TEMPLATE_ID_OR_NAME` for a complete, read-only single-template query. It returns top-level `channel=qianchuan`, bindings, delivery settings, material strategy, and readiness from one local config read without credentials or official API calls. Use the same shared command with `--channel marketing` and an exact Marketing template name for Marketing details.

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

Use `qc-materials inspect-work --work-url URL` to inspect and deduplicate public work links without touching a plan. It may call only the locally configured metadata endpoint and must redact that endpoint from every output or error. Use `qc-materials authorized-creators --advertiser-id ID [--query VALUE]` for the official product-all-domain authorization list.

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

Accept one or more Douyin share links or complete user rows with repeated `--work-url` arguments:

```bash
ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url $'DOUYIN_WORK_URL\tPLAN_TYPE\tBUSINESS_OWNER' \
  --work-url DOUYIN_WORK_URL
```

Treat each argument as one user row. Extract a Douyin URL from a plain link, Markdown link target, or complete share command. If Tab-separated columns exist, use the last two columns as that row's optional plan type and business owner; one extra column is the type. Omit absent type or business segments from the rendered plan name instead of prompting for them. `--plan-type` and `--business` remain optional whole-command fallbacks for unstructured rows. Do not persist these runtime values back into the template.

Follow only redirects that remain under the official `douyin.com` or `iesdouyin.com` domains, normalize the final `/video/{aweme_item_id}` URL, and deduplicate repeated works. The redirect step is not an advertising API call; all business facts still come from official Qianchuan APIs.

List authorized product creators once. Resolve works in batches of 50 by querying every authorized numeric `aweme_id`, then query each resolved creator again with every template product. Skip invalid links, unauthorized works, disabled creators, product mismatches, unsupported material types, and duplicate input without stopping the batch.

The first successful official ownership check stores only the non-sensitive `aweme_item_id`, visible Douyin ID, and numeric `aweme_id` relationship in the local state cache for 30 days. A later preflight or confirmed submission uses that relationship only as a query hint: it must use the official authorization endpoint to resolve the visible Douyin ID exactly, then re-query the hinted creator with the current work and template products. Missing, expired, disabled, or stale hints fall back to the complete official creator scan. Cache read or write failures are non-blocking and must be exposed in `performance.owner_hint_cache`; they never weaken validation or fail an otherwise valid batch. The default bounded concurrency is 8, with an explicit maximum of 10, only for public-link resolution and local task coordination. All official Qianchuan API requests for one advertiser have exactly one in-flight request across endpoints, clients, commands, and Plugin processes, with at least 250 ms between dispatches. Only the small targeted authorization, ownership, and product checks may retry official rate-limit code `40100` with bounded backoff; never retry every request in a broad creator scan.

The optional public-link metadata endpoint must come from local config at `integrations.qianchuan_work_metadata.endpoint`; never hard-code, persist in tracked files, or print the endpoint. Configure it with `setup work-metadata --endpoint URL --home-config`. Its response provides `video_info_id`, `author.unique_id`, `author.uid`, optional `author.nickname`, and optional `product_info_id`. Send only the public Douyin link; never send advertiser IDs, credentials, template payloads, or local state. Use `author.nickname` only as the creator-name label for this creation run; never persist it in templates or the long-term owner cache. Treat the identity author fields as targeted official-query hints. A non-empty `product_info_id` outside the template's product ID set is an immediate product mismatch: skip it before authorization queries and never create or append it. An empty product hint continues to official validation. For a matching hint, query only that hinted template product for the work instead of querying all template products, but still require the official product-filtered creator-video result before creating or appending. Ignore remote cover, playback URL, avatar, title, and media URLs for plan creation and persistence. Missing config, resolver failure, or `--no-link-metadata-api` restores safe Douyin redirect plus broad official discovery.

For a new plan, render the default name as `M.D-creator-product[-type][-business]`: `M.D` is the unpadded creation month and day, creator comes from the configured metadata API nickname, and product comes from the selected template. Type and business are present only when the user row provides them. If a name pattern needs `creator_name` but the metadata API did not return `author.nickname`, stop creation instead of falling back to an official-account label. All eligible rows grouped under one creator must agree on type, business, and any third-party nickname; otherwise stop that creator in preflight instead of choosing a value silently.

Return `performance` timings for link resolution, credential preparation, material resolution, plan reconciliation, and the whole command. Use these fields when diagnosing latency instead of describing the whole preflight as link parsing. Do not create user-facing result files unless `--out` is explicit; the internal owner-hint cache is the only permitted automatic local optimization state.

Before writing, list current product all-domain plans and confirm candidates through plan detail. Treat paused plans as existing and deleted plans as absent. Resolve an existing plan by creator identity plus the products matched by that creator's verified materials:

The plan list exposes the visible Douyin ID in `room_info.anchor_id`, while plan detail exposes the numeric `aweme_id`. Use both identities to select list candidates, then require the numeric detail `aweme_id` to match before treating a plan as existing. Never compare only one identifier type and never create a new plan merely because the list uses the visible ID.

The plan-list `start_time` and `end_time` describe the returned data period, not the plan creation period. For batch work-link reconciliation, set both to the current local date (`00:00:00` through `23:59:59`) because this lookup only decides whether the current batch should create a plan or append materials. Traverse every declared page for that day and do not stop after an arbitrary number of plans. Scan this current-day plan list exactly once per command after all verified works have been grouped; 1, 50, or more input links must not multiply plan-list scans or trigger one scan per creator.

Serialize the complete official-query and reconciliation phase for the same advertiser in both dry-run and submit mode. Use the same `qianchuan-advertiser-{advertiser_id}.lock` for batch creation/append, material deletion, plan creation, and status/budget/ROI updates. The current and legacy Qianchuan API clients in one command must share one request throttle, one rate-limit cooldown, and one hard request budget. When the official response supplies `Retry-After`, honor it within the bounded cooldown; otherwise apply the bounded local cooldown. Persist interval and cooldown state under the local request-control directory so Python and Go Plugin processes share it and fail closed when that state is corrupt. This advertiser-scoped serialization prevents concurrent preflights, submissions, and immediate material-to-plan transitions from creating an avoidable request burst.

Give `plans batch-qianchuan-works` and `plans remove-qianchuan-work` a hard budget of 512 official HTTP attempts per command, including retries and all clients. Expose `{limit, used, remaining}` as `performance.request_budget` for batch completion. Stop before dispatch with `request_budget_exceeded` when exhausted; never reset the budget by creating a second client or restarting a page scan inside the same command.

Retry transient `40100` rate limits, `51010` service timeouts, retryable transport failures, and explicit RPC timeouts with bounded jittered backoff while reading the product all-domain plan list and candidate plan details. A failed list page must retry that same page without restarting the completed portion of the current-day scan. Do not retry non-transient business errors or any write request through this read retry path.

- Filter every creator candidate by intersecting its detail-derived `product_ids` with the creator group's verified-material `matched_product_ids` before deciding the action.
- No product-matched plan: create from template delivery settings and the first 100 eligible homepage works.
- Homepage-work creation omits `creative_card` by default and must never send an empty card object. The official field table makes the whole card conditional on merchant account type, while the verified creator-homepage flow accepts omission. If a future account reports the whole card as missing, return that account-specific failure instead of inventing selling points.
- Existing plan: do not change any plan setting; list all plan videos and append only missing `aweme_item_id` values.
- More than one product-matched plan after filtering: fail that creator with `multiple_existing_plans` without choosing one. Never report this ambiguity from creator matches that bind only unrelated products.
- More than 100 works: create once, then append remaining chunks through the dedicated add-material endpoint.

Default to dry-run. Add `--submit` only after explicit online-write permission. Link parsing and creator task preparation may execute concurrently, but official API traffic remains single-in-flight for the advertiser. Both dry-run and submit take the advertiser-scoped process lock around official validation and reconciliation. Return one final summary. Do not emit per-link progress or create a file unless `--out` is explicit.

### Mandatory Batch Completion Response

After every `plans batch-qianchuan-works` result, treat the top-level `presentation` object as the mandatory user-facing completion contract. When `presentation.required=true`, perform these steps in order:

1. Output `presentation.rendered_markdown` verbatim as the main result table. Do not reconstruct it from `counts`, `results`, or a model-selected subset.
2. Preserve exactly these five headers and this order: `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`.
3. Do not omit, rename, merge, reorder, summarize, or replace these columns. In particular, never substitute a table whose columns are `处理方式`, `状态`, `数量`, `成功数`, `失败数`, `失败原因`, or similar operational summaries.
4. Keep `skipped`, `query_failures`, and `failed_results` outside the five-column table. Show them after the table when non-empty, following `presentation.required_details`; they never become replacement columns.
5. Even when there are no successful rows, output the five-column header from `presentation.rendered_markdown`, then explain the empty, skipped, or failed result outside the table. Never invent IDs or successful rows.

The five-column table is the default for both dry-run previews and submitted batch completion. A dry-run may show `—` for an unknown plan ID. For `素材ID`, the command prefers the official `material_id` and explicitly falls back to the creation `aweme_item_id` only when the official material ID is absent. Brevity, a large batch, partial failure, or conversational wording is not permission to change this format. Only a direct user request in the current turn that explicitly asks to suppress the table or names different columns may override it; never infer an override.

Use `qc-products list/search` for the official selectable-product endpoint. Use `qc-plans list/show/materials` for plan metadata and material membership. Never interpret plan-list `stats_info` as report currency.

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

Use `qc-reports materials` for `/v1.0/qianchuan/report/material/get/`. Paginate every declared page; `--top` limits display only, while summaries use all fetched rows. Use official material filters and do not substitute plan-list stats. A custom field set that omits GMV or order metrics must return those summaries as `null`, not zero.

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
- Qianchuan MCP business tools: `https://open.oceanengine.com/labels/12/docs/1839622960207943`
- Qianchuan MCP tool list: `https://open.oceanengine.com/labels/12/docs/1847297003631945`
- Qianchuan MCP guide and examples: `https://open.oceanengine.com/labels/12/docs/1849835441833027`
- Qianchuan unified and overall reports: `https://open.oceanengine.com/labels/12/docs/1824289224504835`

Read `references/official-api-notes.md` for confirmed endpoint and account-expansion details and `references/mcp-capability-routing.md` before selecting an MCP business tool. If local notes conflict with official documentation, the current MCP schema, or official MCP results, use the current official source.

## Output And Safety

- Keep official IDs exact; serialize number fields only where the API requires numbers.
- Never print App Secret, Access Token, Refresh Token, auth code, or sensitive MCP URLs.
- Keep only the single-plan payload/template `plans create-qianchuan` dry-run independent of credentials and the HTTP client. Batch work-link and material-removal dry-runs require advertiser-bound credentials and official read APIs for ownership, product, plan, and material reconciliation; they must never write.
- Never expose the configured private work-metadata endpoint; report only configured/not configured.
- Show advertiser, goal, product count, budget, bid type, ROI, material counts, blocking fields, and endpoint before submission.
- Present report summaries and rankings as Markdown tables in conversation; JSON remains the CLI boundary.
- Batch work-link output must follow `Mandatory Batch Completion Response`: reproduce the required five-column `presentation.rendered_markdown` only after the batch finishes, then summarize skipped and failed details outside that table.
- Preserve official responses for diagnosis, but never expose stored credentials.
