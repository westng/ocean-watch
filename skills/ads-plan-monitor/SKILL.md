---
name: ads-plan-monitor
description: Dedicated 巨量营销 plan monitoring skill for first-run setup, local Marketing OAuth, authorized-advertiser discovery and freshness, responsible-account lists across Marketing and Qianchuan, advertiser-bound templates, material discovery, plan creation and updates, performance reports, run history, and strategy. Use whenever the user's intended outcome is to make the local Marketing authorization reflect the official account access currently held by that authorized user, including newly granted, removed, stale, or missing advertiser coverage, regardless of wording; also use for 巨量营销初始化、营销授权或刷新 token、查询或管理用户日常投放范围内的账户、跨渠道查询消耗、营销模板、素材、计划、报表和投放策略 through official APIs.
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

For a generic request to create a `投放模板` that does not name Marketing or Qianchuan, do not assume Marketing from the active account, existing authorization, or default channel. Run the shared `templates create` entry without `--channel` and ask the user to choose `巨量营销` or `巨量千川` before showing source templates. An unconfigured or unauthorized channel remains selectable for template creation; clearly state that authorization is required before real delivery. After Marketing is selected, ask `混剪素材（账户上传）` or `原生素材（达人授权）` before showing source templates, and show only business templates bound to that material mode.

After Qianchuan is selected, ask `商品全域` or `直播全域` before showing source templates. Never silently default one Qianchuan template kind.

Template advertiser selection must ignore placeholder IDs. When the selected channel has multiple authorized advertisers, require an exact advertiser ID and reject IDs outside that channel's advertiser index. Auto-fill only when exactly one authorized advertiser exists or a reusable source-template advertiser is still authorized. An unauthorized channel may save an unverified template binding, but the preview must show `UNVERIFIED` and real delivery must revalidate after OAuth.

## Command Entry

Use the unified launcher from this Skill root:

```bash
python3 run.py <domain> <action> [options]
```

If the package is installed, `ocean-watch <domain> <action>` is equivalent. Read `../../docs/cli.md` only when full command details are needed.

Core routes:

| Request | Command |
| --- | --- |
| Check local environment | `setup doctor` |
| First run | `setup init` |
| Validate config | `setup validate --mode query|create-preview|create-submit|all` |
| Marketing OAuth | `auth authorize --channel marketing` |
| Replace Marketing app | `auth set-app --channel marketing` |
| Token/account status | `auth status --channel marketing` |
| Refresh current authorized user's advertisers | `auth sync-accounts --channel marketing` |
| Advertiser authorization mapping | `auth mappings --channel marketing` |
| List all channel templates | `templates list` |
| Show one Marketing or Qianchuan template | `templates show --channel CHANNEL --template TEMPLATE` |
| Create Marketing template | `templates create --channel marketing` |
| Validate/delete template | `templates validate` / `templates delete` |
| Uploaded videos | `materials videos` |
| Creator videos | `materials creator` |
| Single upload plan | `plans create` |
| Single creator plan | `plans create-creator` |
| Upload batch | `plans batch-upload` |
| Creator batch | `plans batch-creator` |
| Current material report | `reports materials` |
| Marketing project report | `reports plans` |
| Report field discovery | `reports schema` |
| Update project/unit settings | `plans update-*` |
| List/show local batch runs | `runs list` / `runs show` |
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

## First-Use Environment Check

Before the first Python command on a new computer, detect a supported interpreter with ordinary system commands. On macOS/Linux, try `python3 --version` and then `python --version`. On Windows, try `py -3 --version`, `python --version`, and `python3 --version`. Require Python `3.10+`; if none is available, stop and tell the user to install Python and reopen Codex. Do not claim that the Plugin can install Python automatically.

After finding Python, run `setup doctor` before setup or authorization. It checks the Python version, Windows/macOS/Linux support, Codex CLI availability, secure credential backend, and whether the configured loopback callback port can be bound. Resolve every `blocking_check` before OAuth or business commands; warnings may be reported without blocking ordinary Plugin use. `setup init` includes the same environment report for first-run guidance.

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

Interpret responsible-account requests as a semantic responsible-account intent, not by exact wording or keyword matching. This intent covers any natural-language request about the advertising accounts in the user's customary working scope: accounts they commonly use, are responsible for, manage, operate, maintain, or normally run campaigns from. It also covers colloquial abbreviations such as `常用的户` or `我管的户`, misspellings, omitted nouns, and contextual follow-ups. These examples illustrate the intent; they are not an exact or exhaustive keyword list. Never ask the user to restate the request in a canonical format.

Split this semantic intent by what the user asks for:

- Membership-only requests such as `我负责的账户`, `我常用的账户`, `我管的户`, or a contextual equivalent ask which accounts are in the user's working scope. Run `accounts list` during the current turn. Read only the enabled local registry records by default; it must not resolve credentials, refresh a Token, or call any official report API.
- Performance requests that mention spend, GMV, ROI, orders, performance, a date range, or equivalent metrics ask how those accounts are performing. Run `accounts report` during the current turn. If the user names no advertiser ID, query all enabled responsible accounts.

Do not infer a performance request merely because the user asks for their accounts. Do not require exact keywords; determine whether the requested output is membership or metrics from the full utterance and conversation context.

For a membership request, run:

```bash
ocean-watch accounts list
```

Treat `accounts list` top-level `presentation` as the mandatory membership response contract. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim. It contains only channel, account name, advertiser ID, and enabled state. Do not add spend, GMV, ROI, orders, query status, date range, or failure columns. Use `--channel` only when the request names one channel, and use `--all` only when the user explicitly asks to include disabled accounts.

For a performance request, run:

```bash
ocean-watch accounts report
```

Do not answer either intent from an earlier message or cached result when the user repeats or paraphrases the request.

Use `--channel marketing` or `--channel qianchuan` only when the request names one channel. With no channel filter, query every enabled responsible account across both channels. The command uses bounded concurrency, preserves configured account order, retries documented transient read failures, and returns successful accounts even when another account fails. Sum spend across channels, but present GMV and ROI from `channel_summaries` because Marketing and Qianchuan use different official conversion definitions. Never replace this registry with all OAuth-authorized advertisers.

Treat `accounts report` top-level `presentation` as the mandatory performance response contract. When `presentation.required=true`, output `presentation.rendered_markdown` verbatim as the complete result. Do not reconstruct it from `accounts` or `summary`, and do not omit, merge, rename, reorder, summarize, or replace its date range, account summary, account rows, per-channel summaries, failure details, or metric-basis section. Only narrow the output when the user explicitly requests specific performance fields in the current message. Do not dump raw JSON unless requested.

Manage records with `accounts add/list/remove/enable/disable`. Real account names and IDs belong only in the user's ignored project or home config, never in tracked Skill files, examples, tests, or templates.

Marketing OAuth state is `AD.<nonce>`. Require an exact state and channel match before exchanging or storing tokens.

Interpret authorized-advertiser freshness as a semantic intent, not by exact wording or keyword matching. Determine it from the user's intended outcome, the full utterance, and conversation context: the user wants this authorized user's local Marketing advertiser coverage or mappings to match what the official platform currently grants. This includes additions, removals, stale snapshots, missing coverage, and contextual follow-ups even when the user does not name OAuth, synchronization, advertiser IDs, or a command. Handle colloquial language, paraphrases, omissions, and misspellings naturally. Any examples are illustrative only, never an exact or exhaustive trigger list, and never ask the user to restate the request using canonical words.

For this semantic intent, run the following during the current turn:

```bash
ocean-watch auth sync-accounts --channel marketing
```

This command reuses a still-valid access token or refreshes it when required, then queries the official authorization subjects, expands every supported role, verifies the complete advertiser set, and atomically replaces only that authorization's OAuth snapshot. It does not change `managed_accounts`. If more than one Marketing authorization exists, or a pending authorization must be recovered, first use redacted status/mapping output to select the exact `authorization_id`, then pass `--authorization-id ID`; never guess or merge different users' authorizations. Use `--rebind-existing` only when the user explicitly asks to move conflicting advertiser mappings to that authorization.

After a successful refresh, report `last_authorized_account_sync_at`, `authorized_advertiser_count`, the returned string-valued `authorized_advertiser_ids`, pending/partial discovery status, and the result of `auth mappings --channel marketing` for the refreshed authorization. Do not substitute `accounts list`: responsible accounts and the OAuth advertiser snapshot are separate data sources. If official pagination or verification is incomplete, report the failure and preserve the previous snapshot rather than claiming that no advertisers exist.

If the selected channel has no app credentials, `auth authorize` opens one local setup page that collects App ID and Secret together, stores them in the OS credential backend, then redirects to official OAuth. Do not split these fields into separate Codex prompts. `auth set-app` opens the same form only for an explicit app replacement.

OAuth is first-use setup, not Plugin-install authentication. Run `auth authorize` and keep that command alive while the browser flow completes. The loopback `redirect_uri` is only an exact value to register in the official console and an endpoint for the official callback; never present it as a URL the user should open.

When Codex starts OAuth, always use `--print-url --no-open` and return only `start_url` as the temporary entry page. Let the user open it in the browser profile that already holds the intended Ocean Engine account. Do not invoke the operating system's default browser or use the in-app browser unless the user explicitly asks for it.

After presenting `start_url`, keep the authorization command running and continue polling the same process; do not end the task while it is waiting for the callback. As soon as OAuth completes, proactively run `auth status` and report the channel, authorization result, authorized-subject count, verified advertiser count, pending/partial sync counts, and advertiser-to-Token mapping result. Never wait for the user to ask whether authorization succeeded, and never include credentials in the feedback.

Use `auth mappings --channel marketing [--advertiser-id ID]` for the mapping check. It may report authorization IDs, account IDs, advertiser IDs, and token-presence booleans only. Never expose credential values.

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

MCP is optional. Use `mcp configure`/`mcp status`, and use `mcp capabilities [--tool TOOL_NAME]` to inspect the current runtime tool inventory and schema. Prefer a configured MCP for official documentation and for an exact advertised remote operation only when its schema matches and credentials remain protected. Marketing project/promotion workflows, local state, OAuth persistence, and any operation without an exact runtime tool continue through the bundled CLI and official API. Route Qianchuan capability selection through `$qc-plan-monitor` and its `references/mcp-capability-routing.md`; never infer tool parameters from a static list.

## Template Contract

Schema v6 has one `default_plan_template` and advertiser-bound business templates.

`templates list` is the fast shared read path for Marketing and Qianchuan. It reads the local config once, calls no official API, and returns compact business-template rows plus default-skeleton counts. Every template record, including default skeletons, detailed rows, and single-template responses, must include top-level `channel=marketing|qianchuan`. Use `--channel marketing` or `--channel qianchuan` to filter, and `--include-details` only when full template diagnostics are needed.

Use `templates validate` before uncertain template operations. `templates delete` is a local write but still defaults to dry-run and requires `--submit`; do not use `--force` until referenced-template diagnostics have been shown and explicitly accepted. Default skeletons are never deletion targets.

Use `templates show --channel marketing|qianchuan --template TEMPLATE` for one-template detail queries. Marketing requires the exact template name; Qianchuan accepts an exact template ID or display name. Return the complete bindings, delivery settings, material strategy, and readiness state from one local config read without credentials or official API calls.

- The default template is a creation base only and must never submit a plan.
- New business templates must use the interactive `templates create` wizard.
- Every business template binds `channel`, `advertiser_id`, `platform`, `traffic_source`, `product_id`, and `product_name`.
- Every template binds `material_strategy.source_type` to `ACCOUNT_UPLOAD` or `CREATOR_AUTHORIZED`.
- The Marketing creation base uses `product_info.product_image_type=DPA` with the upgraded-product image field `images_url`; the standard wizard does not ask users for image IDs.
- `CUSTOM` product images remain an advanced configuration. When a manually edited or cloned template selects `CUSTOM`, one or more `overrides.resolved_ids.product_image_ids` are still required by the official API.
- The wizard explicitly collects daily budget, net-order ROI goal, gender, age targeting, and 6–9-position custom product selling points, then shows their official payload values before confirmation.
- The bundled Marketing default targets the 29 top-level regions outside Hong Kong, Macao, Taiwan, Xinjiang, and Tibet. The official payload receives this allowlist through `resolved_ids.city_ids`; do not invent an exclusion field.
- Target channel and advertiser must match the template before token resolution or API calls.
- Dynamic video, cover, item, and material IDs belong to the current run, not the template.
- Before an online write, resolve a missing event asset only from official same-advertiser,
  same-product projects. If more than one valid asset remains, block and require an explicit
  selection instead of guessing.
- Standard template creation does not ask for product image IDs. At runtime, verify the selected
  DPA product fields; when those fields are unavailable, reuse product images and non-empty brand
  IDs only from an official same-advertiser, same-product promotion. If neither source is available,
  block before project creation.
- Titles live in `copy_materials.titles`; each title must contain 5–30 characters.
- New product or advertiser cloning clears account/product-owned assets according to the wizard preview.
- Treat the four CID link inputs as opaque, independent values. Preserve every character exactly as
  supplied, including macro spelling, query order, percent encoding, case, and repeated parameters.
  Never decode/re-encode, normalize, append runtime macros, copy parameters between fields, or
  derive one link from another. Map them only as follows:
  `landing_page_url -> promotion_materials.external_url_material_list`,
  `open_url -> promotion_materials.open_url`,
  `track_url -> project.track_url_setting.track_url`, and
  `action_track_url -> project.track_url_setting.action_track_url`.
- Do not validate, infer, or restrict a CID link by domain, scheme, path, parameter name, parameter
  count, or field contents. The user-provided value is authoritative. Saving, loading, previewing,
  and submission must preserve the same string in the same field without additions or omissions.

Marketing business-template names are user-defined labels. The wizard must ask for a non-empty template name and keep business ownership exclusively in `bindings`; never derive or validate ownership by parsing the label.

The wizard separately asks for `project_name_template` and `promotion_name_template`. These forms are stored under `overrides.defaults`, shown in the preview, and rendered when a project and promotion are created. Existing templates keep their keys and display names during the v5-to-v6 migration.

## Marketing Create Workflow

Creation is always a two-step official transaction:

1. `/v3.0/project/create/`
2. `/v3.0/promotion/create/` with returned `project_id`

Default to dry-run. Submit only after explicit online-write permission, using `--submit`. Before submission:

- Apply the business template explicitly named by the user or confirmed in the current conversation. Never fall back to an active/default business template.
- Apply explicit user overrides.
- Resolve and report account/product runtime assets before creating the project. Runtime fallback
  data enriches the in-memory payload only and is never written into credentials or dynamic
  material fields.
- Query and revalidate current material availability.
- Block unresolved values such as `REPLACE_WITH`, `TODO`, `待填`, or unsupported placeholders.
- Show the four CID links field by field and compare each complete string with the confirmed
  template value before submission. Do not expose or request expanded runtime macro values.
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
- If the current authorization snapshot alone omits `video_cover_id`, recover it only from a unique
  official promotion material under the same advertiser with the same `item_id`, `material_id`, and
  `MATERIAL_STATUS_OK`. Keep this value runtime-only; block unresolved or ambiguous covers, and
  report that the authorization period remains a create-time-only check. If official creation has
  already rejected the work as outside its authorization period, block retries until a refreshed
  authorization snapshot supplies its own cover; preserve the created project for promotion-only resume.
- One normal native promotion contains one `aweme_id`; all selected items in a unit must belong to that creator.
- Exclude clear product mismatches. Ambiguous materials require explicit user confirmation.
- Creator batch jobs must record `product_match.status` as `MATCHED` or `USER_CONFIRMED` plus concise evidence.
- Use `plans batch-creator` for multiple creators. Its local journal skips completed jobs and resumes promotion failures.
- Before every creator batch write, run `plans batch-creator --preflight` with the same manifest and
  journal. Show the user the batch ID, already-completed count, ready count, blocked rows, planned
  operation per ready row, and the project-capacity warning; then submit only within the user's
  explicit confirmed scope. The official project list does not reliably expose quota occupancy,
  so never claim that capacity is available from a read-only count. `/v3.0/project/create/` is the
  final capacity check against the known per-advertiser project limit.

## Query And Reporting

For current material monitoring, use `reports materials`:

1. Query promotion/unit list and retain status fields.
2. Extract current `promotion_materials.video_material_list` IDs.
3. Query `MATERIAL_DATA` for those material and promotion IDs.
4. Join unit, material, and metric rows.

Do not use a broad promotion-only total as the current material-list total. Status values are displayed and remembered but not filtered unless the user asks for active-only data.

For project-level performance, use `reports plans`. It must first negotiate `UNI_PROJECT_DATA` through the official report-config endpoint and query only dimensions and metrics available to that advertiser. A user-requested unavailable metric is blocking; never guess an alternate field. Report unqueried or unavailable GMV/order summaries as `null`, never zero.

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

When a write is explicitly requested, use `plans update-project-status`, `update-promotion-status`, `update-budget`, `update-bid`, or `update-roi`. First run without `--submit`, show IDs, endpoint and payload, then submit only the confirmed scope. Never construct a naked ad-hoc payload outside these commands.

## Output And Safety

- Keep IDs as exact strings unless an official JSON field requires a number.
- Preserve every tracking-link query parameter exactly.
- Do not print credentials or sensitive MCP URLs.
- Report partial batch failures per account/job; do not hide successful rows.
- Prefer concise structured summaries; show full payloads only when requested.
- Plan-setting and template-deletion writes are dry-run by default and require `--submit`.
