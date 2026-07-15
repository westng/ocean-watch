---
name: ads-plan-monitor
description: Dedicated 巨量营销 plan monitoring skill for first-run setup, local Marketing OAuth, responsible-account lists across Marketing and Qianchuan, advertiser-bound templates, material discovery, plan creation, performance reports, and strategy. Use for 巨量营销初始化、营销授权或刷新 token、管理我负责的账户、跨渠道查询负责账户消耗、创建或迁移营销投放模板、查询上传/达人素材、创建营销计划、查询素材消耗排行、汇总报表, and 投放策略分析 through official APIs.
---

# Ads Plan Monitor

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## Scope

This is one Skill with internal branches, not separate create/query/strategy skills:

1. `首次使用`: local config, secure credentials, OAuth, advertiser sync, readiness.
2. `模板`: advertiser/product/platform/material-source-bound plan templates.
3. `素材`: account-uploaded videos or creator homepage/authorized videos.
4. `创建`: dry-run or submit project/promotion pairs, single or concurrent batch.
5. `查询`: current promotion materials and material-level reports.
6. `策略`: evidence-based recommendations; read-only unless the user explicitly requests a write.
7. `负责账户`: local cross-channel account registry and concurrent spend summaries.

This Skill owns only `marketing` (巨量营销): OAuth, account discovery, templates, materials, plans, reports, and strategy. Route every Qianchuan request to `$qc-plan-monitor`; never reuse Marketing credentials, endpoints, templates, or creation transactions for Qianchuan.

## Command Entry

Use the unified launcher from this Skill root:

```bash
python3 run.py <domain> <action> [options]
```

If the package is installed, `ocean-watch <domain> <action>` is equivalent. Read `../../docs/cli.md` only when full command details are needed.

Core routes:

| Request | Command |
| --- | --- |
| First run | `setup init` |
| Validate config | `setup validate --mode query|create-preview|create-submit|all` |
| Marketing OAuth | `auth authorize --channel marketing` |
| Replace Marketing app | `auth set-app --channel marketing` |
| Token/account status | `auth status --channel marketing` |
| Create/list templates | `templates create` / `templates list` |
| Uploaded videos | `materials videos` |
| Creator videos | `materials creator` |
| Single upload plan | `plans create` |
| Single creator plan | `plans create-creator` |
| Upload batch | `plans batch-upload` |
| Creator batch | `plans batch-creator` |
| Current material report | `reports materials` |
| Report field discovery | `reports schema` |
| List responsible accounts | `accounts list` |
| Add responsible account | `accounts add` |
| Query responsible-account spend | `accounts report` |

## Development Boundary

Classify the request before touching local state:

- Plugin development requests modify only tracked source, public examples, docs, and tests.
- Business details given during development are requirements or fixtures, not permission to persist them.
- Do not read or mutate real `config/`, OS credentials, local journals, or real APIs during development validation.
- Enter business execution only when the user explicitly asks to query real data, write local business config, authorize locally, or submit real plans.
- When intent is ambiguous in a development conversation, remain in development mode.

Never use browser-admin automation. Use official APIs and the bundled CLI.

## Config And Secrets

Config resolution order:

1. Explicit `--config`.
2. `ADS_PLAN_MONITOR_CONFIG`.
3. Git checkout `config/ads-plan-monitor/config.json`.
4. `$CODEX_HOME/ads-plan-monitor/config.json` (`CODEX_HOME` defaults to `~/.codex`).

Project config is non-secret. Never ask the user to paste App Secret, Access Token, Refresh Token, auth code, or MCP identifiers into chat. Never print them.

Credentials use macOS Keychain, Windows DPAPI, or Linux Secret Service. Plaintext fallback is disabled unless the user explicitly sets `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1` for development.

Business commands resolve a Marketing authorization by target `advertiser_id` and optional official `auth_account_id`, refresh only that authorization, and never fall back across channels.

## Responsible Accounts

`managed_accounts` is a local, non-secret user preference. It is not the OAuth authorized-account snapshot and must never be overwritten by `auth sync-accounts`. Records are unique by `channel + advertiser_id` and contain name, advertiser ID, enabled state, and an optional `auth_account_id` that disambiguates overlapping OAuth authorizations.

When the user says `我负责的账户`, `常用账户`, or asks to view spend without listing IDs, use:

```bash
ocean-watch accounts report
```

Use `--channel marketing` or `--channel qianchuan` only when the request names one channel. With no channel filter, query every enabled responsible account across both channels. The command uses bounded concurrency, preserves configured account order, retries documented transient read failures, and returns successful accounts even when another account fails. Sum spend across channels, but present GMV and ROI from `channel_summaries` because Marketing and Qianchuan use different official conversion definitions. Never replace this registry with all OAuth-authorized advertisers.

Manage records with `accounts add/list/remove/enable/disable`. Real account names and IDs belong only in the user's ignored project or home config, never in tracked Skill files, examples, tests, or templates.

Marketing OAuth state is `AD.<nonce>`. Require an exact state and channel match before exchanging or storing tokens.

If the selected channel has no app credentials, `auth authorize` opens one local setup page that collects App ID and Secret together, stores them in the OS credential backend, then redirects to official OAuth. Do not split these fields into separate Codex prompts. `auth set-app` opens the same form only for an explicit app replacement.

## Official References

Use official docs as the source of truth:

- Project create: `https://open.oceanengine.com/labels/34/docs/1740868093375503`
- Promotion create: `https://open.oceanengine.com/labels/34/docs/1740946299496459`
- Account video list: `https://open.oceanengine.com/labels/34/docs/1696710601820172`
- Creator homepage videos: `https://open.oceanengine.com/labels/7/docs/1729982871844879`
- Creator authorization: `https://open.oceanengine.com/labels/7/docs/1729983667746823`
- Report config: `https://open.oceanengine.com/labels/34/docs/1755261744248832`
- Custom report: `https://open.oceanengine.com/labels/34/docs/1741387668314126`

Read `references/official-api-notes.md` for endpoint details and `references/creator-material-api-notes.md` for creator semantics. Read `references/current-template-notes.md` for reusable template and reporting rules. If references conflict with official docs or official MCP results, prefer the official source.

Official MCP is documentation-only. Use `mcp configure`/`mcp status`; continue using the official business API through the CLI for accounts, plans, and reports.

## Template Contract

Schema v3 has one `default_plan_template` and advertiser-bound business templates.

- The default template is a creation base only and must never submit a plan.
- New business templates must use the interactive `templates create` wizard.
- Every business template binds `channel`, `advertiser_id`, `platform`, `traffic_source`, `product_id`, and `product_name`.
- Every template binds `material_strategy.source_type` to `ACCOUNT_UPLOAD` or `CREATOR_AUTHORIZED`.
- Target channel and advertiser must match the template before token resolution or API calls.
- Dynamic video, cover, item, and material IDs belong to the current run, not the template.
- Titles live in `copy_materials.titles`; each title must contain 5–30 characters.
- New product or advertiser cloning clears account/product-owned assets according to the wizard preview.

Suggested template name:

```text
平台-CID-商品名-商品ID-素材来源
```

Online project and promotion names must expose `混剪` for `ACCOUNT_UPLOAD` and `原生` for `CREATOR_AUTHORIZED`.

## Marketing Create Workflow

Creation is always a two-step official transaction:

1. `/v3.0/project/create/`
2. `/v3.0/promotion/create/` with returned `project_id`

Default to dry-run. Submit only after explicit online-write permission, using `--submit`. Before submission:

- Apply the named or active business template.
- Apply explicit user overrides.
- Query and revalidate current material availability.
- Block unresolved values such as `REPLACE_WITH`, `TODO`, `待填`, or unsupported placeholders.
- Show advertiser, template, project/promotion names, budget/bid/ROI when present, material count, product ID, operation, missing fields, and endpoints.
- Never invent city, video, cover, image, product, brand, category, event, or landing-page IDs.

If project creation succeeds and promotion creation fails, preserve `project_id` and `failure_stage: promotion_create`. Resume with promotion-only instead of creating another project.

### Uploaded materials

- Query `/2/file/video/get/`; use returned promotion-ready `video_id`, not `material_id`.
- Validate through `/2/file/video/ad/get/` and fetch cover suggestions before submission.
- For today's grouped uploads, use `plans batch-upload`; do not manually loop single commands.
- Default maximum is 5 videos per unit unless official rules and the selected template explicitly support another value.
- Multi-account batches resolve one advertiser-bound template per account and run with bounded concurrency.

### Creator materials

- `materials creator` defaults to cooperation authorization through `/2/tools/aweme_auth_list/`.
- `--source homepage` queries public homepage videos and requires exactly one `aweme_id`.
- Homepage visibility does not imply cooperation authorization.
- Re-query the authorization snapshot immediately before creation.
- Reject inactive, expired, expiring-too-soon, incomplete, cross-advertiser, mixed-creator, or missing materials.
- One normal native promotion contains one `aweme_id`; all selected items in a unit must belong to that creator.
- Exclude clear product mismatches. Ambiguous materials require explicit user confirmation.
- Creator batch jobs must record `product_match.status` as `MATCHED` or `USER_CONFIRMED` plus concise evidence.
- Use `plans batch-creator` for multiple creators. Its local journal skips completed jobs and resumes promotion failures.

## Query And Reporting

For current material monitoring, use `reports materials`:

1. Query promotion/unit list and retain status fields.
2. Extract current `promotion_materials.video_material_list` IDs.
3. Query `MATERIAL_DATA` for those material and promotion IDs.
4. Join unit, material, and metric rows.

Do not use a broad promotion-only total as the current material-list total. Status values are displayed and remembered but not filtered unless the user asks for active-only data.

Default conversation output is Markdown tables, not spreadsheet files:

- Context: account, date range, unit/material/report counts.
- Summary: spend, impressions, clicks, CTR, CPC, CPM, conversions, conversion cost/rate, orders, GMV, ROI, play metrics when present.
- Top spend: promotion, IDs, status, spend, conversions, orders, GMV, ROI.

Write JSON or CSV only when explicitly requested.

## Strategy

Strategy is read-only by default:

1. Query current evidence for the requested date/account scope.
2. Separate facts from model judgment.
3. Identify high-spend/no-conversion, weak ROI, rising cost, and promising materials.
4. Recommend concrete stop, observe, test, or scale actions with reasons.
5. Do not change delivery state, budgets, bids, templates, or plans unless the user explicitly requests the write after seeing the evidence.

## Output And Safety

- Keep IDs as exact strings unless an official JSON field requires a number.
- Preserve every tracking-link query parameter exactly.
- Do not print credentials or sensitive MCP URLs.
- Report partial batch failures per account/job; do not hide successful rows.
- Prefer concise structured summaries; show full payloads only when requested.
