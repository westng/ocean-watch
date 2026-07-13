---
name: ads-plan-monitor
description: Unified Ocean Engine / 巨量计划盯盘 skill with one skill and internal branches for first-run setup, local OAuth token authorization, creating single or batch ad plans from configurable templates, querying account unit/material performance, and strategy/monitoring analysis. Use when the user asks to 初始化配置, 第一次使用, 配置技能, 本地授权, 获取token, 刷新token, create 巨量计划, 新建计划, 批量创建计划, 按今天素材创建, 多账户并发创建, 查询素材数据, 素材维度数据, 消耗前十, 汇总数据, 盯盘数据, 逻辑策略, build project/promotion payloads, validate official API fields, configure plan templates following 平台-CID-商品名-商品ID, diagnose missing fields, or analyze ad performance through official Ocean Engine APIs.
---

# Ads Plan Monitor

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## Purpose

Use this as one complete `ads-plan-monitor` skill, not as separate create/query/strategy skills. Route each request to one of these internal branches:

1. `首次使用向导`: Initialize or check local config for a new user.
2. `创建计划`: Create 巨量引擎升级版广告计划 through official Ocean Engine Marketing API endpoints.
3. `查询数据`: Query account unit lists, current unit materials, and material performance reports.
4. `逻辑策略`: Analyze monitoring data and produce strategy decisions or recommendations. Use read-only data first; do not modify ads unless the user explicitly asks for an API write action.

Treat "创建计划" as a two-step API workflow:

1. Create a project with `/open_api/v3.0/project/create/`.
2. Create a promotion/unit with `/open_api/v3.0/promotion/create/` using the returned `project_id`.

Default to generating and validating payloads first. Submit to the API only after the user explicitly asks to create online plans and the local config is complete.

When the user asks about structure, explain that `ocean-watch` is one Codex Plugin containing one `ads-plan-monitor` Skill with internal branches. The repository root is the Plugin root; this directory is the Skill root.

## Required References

- Read `references/current-template-notes.md` when working on the current active plan template, fixed titles, tracking links, default product, naming rules, material selection rules, or report scope.
- Use the official docs as the source of truth for API field semantics:
  - Project create: `https://open.oceanengine.com/labels/34/docs/1740868093375503?origin=left_nav`
  - Promotion create: `https://open.oceanengine.com/labels/34/docs/1740946299496459?origin=left_nav`
  - Video material list: `https://open.oceanengine.com/labels/34/docs/1696710601820172?origin=left_nav`
  - Custom report config: `https://open.oceanengine.com/labels/34/docs/1755261744248832?origin=left_nav`
  - Custom report data: `https://open.oceanengine.com/labels/34/docs/1741387668314126?origin=left_nav`
- If the local reference and official docs conflict, prefer the official docs and call out the conflict.

## Official Documentation MCP

The Plugin can register Ocean Engine's official developer-documentation MCP as `oceanengine-developer-docs`. It is a documentation MCP, not a business-operation MCP. Continue to use the bundled Python scripts and official Marketing API for account data and plan creation.

When official MCP tools are available:

- Use `open_api_doc_gen` to find official endpoint paths and field semantics.
- Use `open_api_schema_gen` before implementing or changing request/response payload fields.
- Use `open_sdk_example_code_tool` only when an official SDK example is useful.
- Prefer MCP results over bundled API notes when they conflict, and update local notes only when the user asks to change the repository.

If MCP tools are missing, run `scripts/configure_official_mcp.py --status`. For first-time setup, run `scripts/configure_official_mcp.py`; it reads `app_id` from the OS credential store, prompts securely for `developer_id`, verifies the official tool list, and registers the bundled SSE-to-stdio bridge in the user's Codex config. The bridge builds the sensitive official URL only in memory. Never print or paste that URL because it contains both identifiers. MCP readiness is optional for business API use; fall back to `references/official-api-notes.md` when unavailable.

## Local Config

Look for config in this order:

1. User-provided explicit path.
2. Environment variable `ADS_PLAN_MONITOR_CONFIG`.
3. Project-local `config/ads-plan-monitor/config.json`.
4. `~/.codex/ads-plan-monitor/config.json`.

Project config stores only non-secret business settings. Never store `app_id`, `secret`, `access_token`, `refresh_token`, or auth codes in project config. Never ask the user to paste tokens into chat. Never print `access_token`, `refresh_token`, `secret`, or auth codes. If a config file is missing, tell the user to create it from `assets/config.example.json`.

Sensitive OAuth credentials live in the user's local OS credential store through `scripts/credential_store.py`:

- macOS: Keychain.
- Windows: DPAPI-protected local credential file under the user's home directory.
- Linux: Secret Service through `secret-tool`; install `libsecret` tooling when unavailable.
- File fallback: disabled by default. Only set `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1` for explicit development use, and warn that it stores plaintext credentials under the user's home directory.

Use `scripts/validate_config.py <config-path> --mode query|create-preview|create-submit|all` to check readiness before API work. The default mode is `all`.
Use `scripts/first_run.py` when a user is new to this skill, asks to initialize/configure it, or a config file is missing. The guide may create a local config from `assets/config.example.json`, but it must not call Ocean Engine APIs or print secrets.
Use `scripts/credential_store.py --set-app` to save `app_id` and `secret` into the local OS credential store before first authorization.
Use `scripts/oauth_local_authorize.py` when the user asks to 获取 token, 本地授权, or first-time OAuth setup. It starts a temporary local callback server at the configured redirect URI, opens the official authorization URL, receives `auth_code`, exchanges it for token fields, writes them to the local credential store, and prints only redacted status.
Use `scripts/token_manager.py --status` to inspect redacted token readiness, and `scripts/token_manager.py --refresh` to force refresh through the stored `refresh_token`.
Use `scripts/token_manager.py --sync-accounts` to sync OAuth subjects and expand real advertisers by `account_role`: direct `ADVERTISER`, customer-center roles through `/2/customer_center/advertiser/list/` with `account_source=AD`, and enterprise BP roles through `/2/ebp/advertiser/list/`. Verify expanded IDs through `/2/advertiser/info/` before saving `authorized_advertiser_ids`.
All API scripts should call `token_manager.ensure_access_token()` before read/write API requests. This refreshes expired tokens and uses a local lock file so concurrent batch creation does not refresh the same config in parallel.
If an API returns `40002` / "No permission to operate account", check `scripts/token_manager.py --status`. Do not treat `oauth_authorized_account_count` as the advertiser count; customer-center and platform-role subjects are not direct ad accounts. If `advertiser_id_authorized` is false, ask the user to re-authorize with that advertiser selected or switch config to an actual authorized advertiser ID.
Use `scripts/query_videos.py` to query account video materials through `/2/file/video/get/`, resolve video `material_id` values to promotion-ready `video_id` values, verify `/2/file/video/ad/get/` accepts them, and fetch `video_cover_id` candidates through `/2/tools/video_cover/suggest/` before submitting a promotion.
Use `scripts/batch_create_from_today_videos.py` when the user asks to batch-create plans from today's uploaded video materials, split videos into groups such as 5 per unit, or create across multiple advertiser accounts with concurrency. This script queries videos, validates promotion-ready IDs, fetches covers with retries, skips unqualified videos by default, groups materials, then creates project/promotion pairs.
Use `scripts/query_report_config.py` to fetch available custom-report dimensions and metrics before choosing report fields.
Use `scripts/query_custom_report.py` to query `/v3.0/report/custom/get/` directly when the user provides dimensions, metrics, and filters.
Use `scripts/query_active_materials_report.py` as the business entry for monitoring data: it reads the account promotion/unit list, records unit and material status fields, extracts video material IDs from units, queries custom-report material performance, and joins unit + material + metric rows.

## Plan Templates

Creation parameters use schema v2 with one shared `default_plan_template` and advertiser-bound business templates. The default template contains reusable settings only and must never be used to create or submit a plan directly. Template names follow:

`平台-CID-商品名-商品ID`

Every business template must contain `bindings.advertiser_id`, `bindings.platform`, `bindings.traffic_source`, `bindings.product_id`, and `bindings.product_name`. The advertiser binding is ownership: reject single or batch creation whenever the target advertiser differs from `bindings.advertiser_id`. Never let `--advertiser-id` or `--accounts` bypass it.

Keep only genuinely reusable delivery and geographic settings in `default_plan_template`. Materials, product and conversion asset IDs, product images, landing-page assets, links, tracking URLs, and titles belong to the advertiser-bound business template. Reject cross-advertiser cloning with `--from-template`; a new advertiser must provide its own account-specific assets and links.

Store promotion copy explicitly under `plan_templates.<name>.copy_materials.titles`. Use repeated `--title` options while creating a template, or `scripts/manage_plan_templates.py set-copy --template <name> --title <文案>` for an existing template. Treat missing copy materials as incomplete create configuration. These titles map to official `promotion_materials.title_material_list`; preserve their exact text and spacing.

Read `active_plan_template` from config when the user does not name a template. When the user names a template, pass `--plan-template <模板名>` to `scripts/create_plan.py`. Use `scripts/manage_plan_templates.py list` to show templates with their advertiser IDs. Use its `create` command to guide the user through advertiser ID, platform, traffic source, product ID, and product name; use `migrate` for legacy schema v1 configs. Template differences live under `plan_templates.<模板名>.overrides` and may override `defaults`, `materials`, `resolved_ids`, `links`, `tracking_urls`, and `titles`.

## Workflow

### 1. Classify the request

- If the user says "第一次使用", "初始化", "配置技能", "帮我配置", "缺配置", or a config is missing, run `scripts/first_run.py` and report the setup checklist. Do not call Ocean Engine APIs during the guide.
- If the user asks to "生成参数", "看 payload", "检查字段", or is missing credentials, generate payloads and validation notes only.
- If the user asks to "查询数据", "素材数据", "视频素材", "素材维度", "盯盘数据", or "查看素材表现", use read-only query scripts and do not create or update ads.
- If the user asks for "逻辑策略", "盯盘建议", "怎么处理", "是否关停", "是否放量", or similar, use the strategy branch: query current data first when needed, summarize the evidence, and output recommendations without calling write APIs by default.
- If the user asks to "创建计划", "调用接口", "真实创建", or similar, still produce a preflight summary first unless they explicitly asked for direct execution in the same turn.
- If any required field is unknown, stop before API submission and output a missing-field checklist.

### 2. Load defaults

Load config, apply `active_plan_template` or the user-selected plan template, then apply user overrides from the prompt. For the active template flow, expected defaults include:

- `advertiser_id`
- `product_id`
- `product_name`
- `daily_budget`
- `roi_goal`
- `source`
- `titles`
- `tracking_urls`
- `landing_page_url` or saved landing page asset ID
- city names or city IDs
- material selection rule

The default public example template is only a placeholder. Use the real product template from local config. For ROI flows, deep optimization `净成交ROI` is represented by `deep_bid_type: NET_ORDER_ROI`.

When `deep_bid_type` is not `DEEP_BID_DEFAULT`, include `roi_goal` in the project payload. When `deep_bid_type` is `DEEP_BID_DEFAULT`, do not include `roi_goal`.

Do not invent IDs for city, video, cover, image, brand, category, landing page, product platform, or product assets.
Treat category and brand names as template metadata until official category or brand IDs are resolved. Send `brand_info` only when non-empty official ID fields exist under `resolved_ids.brand_info`.

### 2A. First-run guide

For a new teammate, use this onboarding flow:

1. Run `scripts/first_run.py` with no arguments for project-local setup, or `scripts/first_run.py --home-config` when the skill is installed outside a shared project.
2. If the guide creates a config, tell the user the config path and ask them to fill only non-secret fields such as `account.advertiser_id` and `oauth.redirect_uri` in the file.
3. Minimum fields for read-only query data:
   - local `app_id` and `secret` saved through `scripts/credential_store.py --set-app`
   - local `access_token` or `refresh_token`, normally written by `scripts/oauth_local_authorize.py`
   - `account.advertiser_id`
4. If app credentials are missing, run `scripts/credential_store.py --config <config-path> --set-app`.
5. If token fields are missing, run `scripts/oauth_local_authorize.py --config <config-path>` and complete browser authorization. The approved local redirect URI is `http://127.0.0.1:8787/oauth/callback`; it must exactly match the app setting.
6. If the official developer-documentation MCP is not ready, run `scripts/configure_official_mcp.py`. This is recommended for development and troubleshooting but does not block query or create readiness.
7. Extra fields for creating plans:
   - one active business template explicitly bound to the target `advertiser_id`
   - `materials.video_ids`
   - `materials.video_cover_ids` if required
   - `resolved_ids.city_ids`
   - `resolved_ids.product_platform_id`
   - `resolved_ids.product_image_ids`
   - `tracking_urls.track_url`
   - `tracking_urls.action_track_url`
   - `links.landing_page_url`
   - `links.open_url`
   - titles and product defaults
8. After the user updates config and authorizes, run `scripts/validate_config.py <config-path> --mode all` and `scripts/token_manager.py --config <config-path> --status`.
9. If `ok_for_query_data` is true, the teammate can ask for "今天汇总数据" or "消耗前十". If create fields are still missing, allow query branch but block online plan creation.

### 3. Build project payload

Use these official defaults when they match the active short-video/image ecommerce flow:

- `operation`: `ENABLE` or `DISABLE`
- `delivery_mode`: `PROCEDURAL`
- `landing_type`: `SHOP`
- `marketing_goal`: `VIDEO_AND_IMAGE`
- `ad_type`: `ALL`
- `related_product.product_setting`: `SINGLE`
- `delivery_range.inventory_catalog`: `UNIVERSAL_SMART` for automatic SHOP delivery
- `audience.district`: `REGION`
- `audience.region_version`: `2.3.2`
- `audience.location_type`: `CURRENT`
- `audience.gender`: `GENDER_FEMALE`
- `audience.age`: omit the field for 年龄不限; only send configured age enums when the template explicitly sets them
- `audience.hide_if_converted`: `NO_EXCLUDE` for 过滤已转化用户不限; do not omit this field for the active template
- `delivery_setting.schedule_type`: `SCHEDULE_FROM_NOW`
- `delivery_setting.budget_mode`: `BUDGET_MODE_DAY`
- `delivery_setting.pricing`: `PRICING_OCPM` for automatic delivery
- `track_url_setting.track_url`: display tracking link list
- `track_url_setting.action_track_url`: click/action tracking link list

High-risk fields must be confirmed from official available-target APIs or a known successful response before submission:

- `external_action`, especially ecommerce `AD_CONVERT_TYPE_APP_ORDER`
- `deep_external_action`
- `deep_bid_type`
- whether `roi_goal` belongs at project level, promotion level, or both for this exact account and chain
- product payload shape, such as `product_platform_id`, `product_id`, `unique_product_id`, and `products[]`

### 4. Build promotion payload

After project creation, use returned `project_id`. For payload preview, use a placeholder `{{project_id}}`.

Required base fields:

- `advertiser_id`
- `project_id`
- `name`
- `operation`
- `source`
- `promotion_materials`

For vertical videos, use `image_mode: CREATIVE_IMAGE_MODE_VIDEO_VERTICAL`. Include no more than 5 videos per project for the current business rule unless the user changes that rule; if more qualified videos exist, split into additional project/promotion pairs.

For the active template flow, include the configured titles. Do not alter hashtag spacing in configured titles.

When the user asks to create plans from "today's videos", "当天视频素材", "新上传素材", or similar, first run `scripts/query_videos.py --mode library-get --date today --fetch-all` to get the account video material list. Use `selected_videos[].video_id` as `promotion_materials.video_material_list[].video_id`. Preserve `material_id`, `filename`, and `create_time` in the preflight summary so the user can see which uploaded asset maps to each planned unit.

For one-plan-per-video workflows, build one project/promotion pair per selected video by passing a single `--video-id` into `scripts/create_plan.py` for each row. Do not batch multiple videos into one plan unless the user explicitly asks for multi-video units. Ask for confirmation before submitting multiple online creations in one turn.

For batch workflows such as "今天上传的素材每 5 条一个单元", "按账户批量创建", or "多账户并发创建", use `scripts/batch_create_from_today_videos.py` instead of looping `create_plan.py` manually. The batch script keeps each group internally sequential (`project/create` then `promotion/create`) but runs accounts and material groups concurrently. It defaults to dry-run; pass `--submit` only when the user explicitly asks to create online plans in the same turn.

Common batch commands:

- Dry-run current account, today, active template: `scripts/batch_create_from_today_videos.py --date today --plan-template <模板名>`
- Create current account, 5 videos per unit, budget 5000, ROI goal 1.5: `scripts/batch_create_from_today_videos.py --date today --plan-template <模板名> --videos-per-unit 5 --budget 5000 --roi-goal 1.5 --submit`
- Multi-account creation: add `--accounts 186...,187... --account-concurrency 2 --group-concurrency 2`
- Limit or test: add `--max-videos 5` and omit `--submit`

Batch behavior:

- Query `/2/file/video/get/` for each account and date, then deduplicate by promotion `video_id`.
- Validate videos through `/2/file/video/ad/get/` by default.
- Fetch `video_cover_id` through `/2/tools/video_cover/suggest/` with retries. If covers are still unavailable or `RUNNING`, skip those videos by default and record them under `skipped_videos`; use `--no-skip-missing-cover` only when the run should block instead.
- Use the selected plan template and existing `create_plan.py` payload builder so template fields, tracking links, audience settings, and product payloads stay consistent.
- Split videos by `--videos-per-unit`, defaulting to `defaults.max_videos_per_project` and capped at 5 for the current promotion material rule.
- Auto-suffix names as `_01`, `_02`, etc. unless the template's name format already includes `{group_index}`, `{index}`, or `{suffix}`.
- Record partial failures. If project creation succeeds but promotion creation fails, preserve `project_id`, compact API response, and group video list for retry.
- Do not print tokens. Save JSON output with `--out` only when a file artifact is useful.

High-risk promotion fields that require IDs or confirmation before submission:

- `video_material_list[].video_id`
- `video_material_list[].video_cover_id` if required by the exact chain
- `brand_info.yuntu_category_id`
- `brand_info.brand_name_id`, `ecom_brand_id`, or `cdp_brand_id`
- landing page field choice: `external_url_material_list` vs `web_url_material_list`
- direct link field choice: `open_url` vs `open_urls[]`
- product info image IDs

### 4A. Query Account Video Materials

Use `/2/file/video/get/` when the user asks for videos under the advertising account, newly uploaded videos, today's videos, or material candidates for plan creation.

Common commands:

- Today: `scripts/query_videos.py --mode library-get --date today --fetch-all`
- Yesterday: `scripts/query_videos.py --mode library-get --date yesterday --fetch-all`
- Specific date: `scripts/query_videos.py --mode library-get --date YYYY-MM-DD --fetch-all`
- Filename contains text: add `--filename <text>`
- Specific material IDs: `scripts/query_videos.py --mode library-get --material-id <id>`
- Validate promotion-ready video IDs: `scripts/query_videos.py --mode ad-get --video-id <video_id>`
- Get a suggested cover: `scripts/query_videos.py --mode cover-suggest --video-id <video_id>`

Official library filters `video_ids`, `material_ids`, and `signatures` are mutually exclusive; pass only one of them in a single query. Date range filters can be combined with filename filtering in local post-processing.

Use `selected_videos` from the script output for downstream creation. Do not use `material_id` as `video_id`; the promotion payload needs the video `id` value from the video material response.

### 5. Preflight before API submission

Block submission if any of these are missing:

- `access_token`
- `advertiser_id`
- unique project and promotion names or permission to auto-suffix duplicates
- confirmed project payload fields
- confirmed promotion payload fields
- city IDs, if using regional targeting
- selected video IDs and any required cover IDs
- product ID and product投放状态 confirmation
- brand/category IDs when `brand_info` is required

Block submission if payload still contains placeholders such as `待反查`, `待填`, `TODO`, `{{...}}`, or empty strings in required fields.

Before submitting, show a concise summary:

- advertiser ID
- project name and promotion name
- daily budget, CPA bid, and ROI goal only when present in the payload
- city count
- video count
- product ID
- operation state
- endpoints to call

Then ask for explicit confirmation unless the user already gave explicit same-turn permission to create online plans with the prepared payload.

### 6. API call rules

Use only official Ocean Engine API endpoints under `https://api.oceanengine.com/open_api/`.

Headers:

```http
Access-Token: <from token_manager / local credential store>
Content-Type: application/json
```

Do not log secrets. Redact token-like fields in errors and summaries.

Expected success fields:

- project create returns `data.project_id`
- promotion create returns `data.promotion_id`

If project creation succeeds and promotion creation fails, preserve the project ID and error response so the user can decide whether to retry, disable, or delete the project.

### 7. Query material data

For material-level monitoring, call `scripts/query_active_materials_report.py`, not the raw report endpoint directly. The workflow is:

1. Query `/v3.0/promotion/list/` for the account or requested project/promotion.
2. Record `status`, `status_first`, `status_second`, `opt_status`, `material_status`, and `material_opt_status`; do not hide units because of status unless the user asks for filtering.
3. Extract `promotion_materials.video_material_list[]` material IDs and related video metadata.
4. Query `/v3.0/report/custom/config/get/` if dimensions or metrics need discovery.
5. Query `/v3.0/report/custom/get/` with `data_topic=MATERIAL_DATA`, material/unit dimensions, selected metrics, and filters based on the extracted unit/material IDs.
6. Join promotion, material, and metric fields into rows.

Defaults:

- report config endpoint: `/v3.0/report/custom/config/get/`
- report data endpoint: `/v3.0/report/custom/get/`
- account: `account.advertiser_id`
- date range: today unless the user gives dates
- data topic: `MATERIAL_DATA`
- default dimensions: `material_id`, `cdp_promotion_id`, `cdp_promotion_name`
- order: `stat_cost DESC`
- pagination: fetch all returned promotion/report pages unless the user asks for a specific page

Common filters:

- `--promotion-id <id>` for one or more ad units
- `--project-id <id>` for one project
- `--start-date YYYY-MM-DD --end-date YYYY-MM-DD`
- `--dimension <field>` and `--metric <field>` for custom report fields
- Exact material filtering is the default: report data is restricted by both extracted `material_id` values and unit IDs from `promotion/list`, so chat summaries match the currently listed video materials.
- `--include-extra-report-materials` only when the user explicitly wants the broader report-visible material rows under the same units.
- `--active-only` only when the user explicitly asks to see投放中/active-like units; normal monitoring should keep status fields and include every returned unit.

Do not write CSV or JSON files unless the user asks for a file or passes `--out` / `--csv-out`.

When the user asks for "汇总数据", "消耗是多少", "给我数据", or similar chat output, answer with Markdown tables in the conversation, not spreadsheet files. Default table output:

1. Show the account, date range, unit count, material count, report row count, and non-secret request IDs in a compact context table.
2. Show a summary metrics table with spend, impressions, clicks, CTR, CPC, CPM, conversions, conversion cost, conversion rate, orders, GMV, ROI, total plays, 3s plays, and play completion rate when those fields are present.
3. Show a top-spend unit/material table when row-level data is available, including promotion name, promotion ID, material ID, status fields, spend, impressions, clicks, conversions, conversion cost, orders, GMV, and ROI.
4. Mention status handling: statuses are recorded/displayed, not filtered by default. Only apply `--active-only` when the user explicitly asks for投放中/active-like rows.

Default metrics are the monitoring basics supported by `MATERIAL_DATA`: spend, impressions, clicks, CTR, CPC, CPM, conversions, conversion cost/rate, play metrics, and in-app order/ROI fields. Use `query_report_config.py --data-topic MATERIAL_DATA` to confirm fields before adding new metrics. Do not put dimension fields in `metrics`; custom reports return them under `rows[].dimensions`.

## Output Format

For payload-only work, output:

1. `project_payload`
2. `promotion_payload`
3. `missing_fields`
4. `preflight_notes`

For successful API creation, output:

1. project ID
2. promotion ID
3. created names
4. any non-secret request IDs or warnings

Keep payloads compact unless the user asks for full JSON.
