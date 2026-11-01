---
name: qc-plan-monitor
description: Dedicated 巨量千川 skill for setup, local OAuth and Token refresh, F2 Cookie configuration, responsible accounts, product/live templates, creator/work/product discovery, all-domain plans, safe Douyin-work preflight and batch create/append, account balance/budget/quota, plan diagnosis, material quality, official delivery suggestions, approved official GET queries, and 全域/乘方 reports. Optional API documentation, Schema and Go/Java examples support plugin development. Use for 千川、配置F2、授权、常用户、模板、达人/作品/商品/计划/素材、预检、批量建计划、追加素材、余额/配额、诊断、首发/低效、建议ROI、消耗/ROI/GMV、自定义报表 and contextual follow-ups.
---

# QC Plan Monitor

## Fast Routing Contract

Classify the user's outcome once, then use the first matching route. Do not search the repository, inspect plugin cache paths, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

Every `./run` below is this skill's own launcher. On Codex invoke it as written. On Claude Code invoke `${CLAUDE_PLUGIN_ROOT}/skills/qc-plan-monitor/run` with the same arguments, because the working directory is the user's project rather than this skill directory. Do not search for the launcher anywhere else.

On DeepSeek Harness, use the absolute directory of the loaded `SKILL.md` to resolve this launcher: `<skill-directory>/run` on macOS/Linux or `<skill-directory>\run.cmd` on Windows. Keep the same arguments. The conversation's working directory is not the skill directory; use the location returned by Harness instead of guessing an installation path.

<!-- capability-routes:start -->
| User outcome | Exact route |
| --- | --- |
| List local Marketing or Qianchuan templates | MCP `list_templates` |
| Show one exact template | MCP `get_template` |
| List accounts I manage/use | MCP `list_managed_accounts` |
| Query spend/performance for my managed Qianchuan account set | MCP `report_qianchuan_managed_accounts` |
| Inspect Qianchuan Token/app/advertiser mapping | MCP `get_qianchuan_authorization` |
| Search selectable Qianchuan products | MCP `search_qianchuan_products` |
| List Qianchuan product all-domain plans | MCP `list_qianchuan_plans` |
| Show one exact plan and optional materials | MCP `get_qianchuan_plan` |
| Fixed account overall/uni report | MCP `report_qianchuan_account` |
| All-domain account report | MCP `report_qianchuan_uni_account` |
| Fixed plan report | MCP `report_qianchuan_plans` |
| Preflight Douyin works without creating or changing a plan | MCP `preflight_qianchuan_works` |
| Configure or initialize F2 Douyin Cookie | MCP `start_f2_configuration` |
| Inspect one exact preflight snapshot | MCP `get_qianchuan_preflight` |
| Check account balance, daily budget and available plan quota | MCP `get_qianchuan_account_health` |
| Diagnose one exact plan with its current delivery scene | MCP `diagnose_qianchuan_plan` |
| Inspect original or inefficient labels of exact materials | MCP `inspect_qianchuan_material_quality` |
| Get official budget, ROI or bid suggestions without changing delivery | MCP `get_qianchuan_delivery_suggestions` |
| Discover exact unified report topics, dimensions and metrics | MCP `get_qianchuan_report_schema` |
| Run a custom material, product, author or room report | MCP `report_qianchuan_custom` |
<!-- capability-routes:end -->

| User outcome | Exact route |
| --- | --- |
| Authorize or recover expired Qianchuan access | MCP `start_authorization` with `channel=qianchuan` and the original advertiser if known |
| Retry saved authorization account discovery | MCP `start_account_sync` with `channel=qianchuan` and the exact authorization ID |
| Approved official query outside the task routes above | MCP `get_qianchuan_official_capabilities` with a narrow keyword, then exact `query_id`; MCP `query_qianchuan_official` with its approved contract |
| Explicit API-documentation, Schema or SDK-example request | The fixed optional MCP route in `references/developer-docs.md` |
| Confirmed Ocean Watch goal not matched above | MCP `get_capabilities` once with `channel=qianchuan`, then use its unique command |
| Initialize, mutate templates/accounts/plans/materials, explicitly advanced CLI reports, or inspect runs | the unique `./run <domain> <action>` route in `references/workflow-reference.md` |

When a row matches, call that exact MCP tool immediately. MCP declarations may be deferred from the visible prompt: never enumerate `ALL_TOOLS`, inspect caches, or read memory to decide whether a named tool exists. Treat it as unavailable only when the direct call itself returns a Host-level unknown-tool or unavailable error; then report that error and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row already matches.

## Authorization Recovery

On `REAUTHORIZATION_REQUIRED`, preserve the original template, links, advertiser and other arguments, then call MCP `start_authorization` with the explicit channel. For authorization or reauthorization, open its returned official OAuth URL; for account synchronization, open the returned local confirmation page. Never ask for secrets or suggest terminal commands to complete this MCP flow. Poll MCP `get_authorization_session` with the returned session ID and same channel; do not claim completion while awaiting the user. Only `completed` with `can_resume=true` permits rerunning the original read/preflight with its original arguments, without asking for the same links again. Stop automatic retries on failure, cancellation, expiry or scope mismatch. Use MCP `cancel_authorization_session` when the user cancels.

For reauthorization, include the exact existing `authorization_id` supplied by the error or the local authorization inspector. Core verifies the official user UID and App before updating that grant, saves the credential before role-based discovery, and retains a saved grant for account synchronization if discovery fails.

On `ACCOUNT_SYNC_REQUIRED`, obtain the exact pending authorization ID from the local authorization inspector and call MCP `start_account_sync`; if a session has `sync_required=true`, use its returned authorization ID. Keep the original advertiser. Present the browser confirmation page and poll the same way; do not repeat OAuth for a saved grant. These tools manage authorization only and never authorize a plan submission.

## Intent Rules

- “配置 F2 / 初始化 F2 / 设置 F2 / 更新或重新配置抖音 Cookie” calls `start_f2_configuration` with `{}`. Open or present its `configuration_url`; the user enters Cookie only in that local page. Never collect Cookie in chat, tool arguments, environment commands or files. Poll `get_f2_configuration_session` with its `session_id`; `completed` plus `credential_saved=true` and `can_resume=true` permits retrying the original links/arguments. If the session is not found after a Runtime restart or host switch, inspect `error.details.configuration_status`: `configured=true` permits retrying the original request without reopening configuration. Never infer a failed save from a missing session. These checks verify system-vault storage, not live Cookie validity (`online_verified=false`). Stop on `failed/cancelled/expired`; use `cancel_f2_configuration_session` on user cancellation. `F2_CONFIGURATION_REQUIRED` and per-row `f2_cookie_missing/f2_cookie_invalid/f2_credential_unavailable` use the same configuration flow. Do not treat a bare HTTP 403 as proof that Cookie is the only cause. F2 Cookie is independent of advertiser OAuth; configure it once for the current OS user, not once per advertiser.

- This Skill owns only `qianchuan`. Route Star Map requests to `$xt-plan-monitor`. Never reuse Marketing or Star Map apps, credentials, templates, account mappings, endpoints, or creation transactions.
- Interpret “我负责的/常用的/管理的户” and contextual paraphrases as responsible-account intent. Membership means `list_managed_accounts`; spend, ROI, GMV, orders, or dated performance requires a fresh report.
- “这个户余额/日预算/还能建几条” uses `get_qianchuan_account_health`; “我负责的户” selects `managed_accounts=true` only when that is the user's intended set. Balance, budget and quota failures remain separate. `OFFICIAL_API_ERROR` or an absent value does not prove the account is healthy.
- “这条计划为什么不跑/审核/学习期/成本保障” uses `diagnose_qianchuan_plan` with the exact plan ID. Keep the verified `uni`, `overall` or `legacy` scene and skipped checks; legacy-only endpoints cannot describe all-domain learning status.
- “首发/低效/素材质量” uses `inspect_qianchuan_material_quality` with exact material IDs, or structured `works[]` containing `url`, product and creator hints. A Douyin work ID, creator UID, video ID and material ID are distinct. Labels do not prove ownership or create-plan eligibility.
- “官方建议预算/ROI/出价” uses `get_qianchuan_delivery_suggestions` with its explicit scene and kind. Suggestions preserve the official object and observation time and do not authorize a modification. Use `references/official-queries.md` only for missing task parameters or a long-tail official query.
- Generic Qianchuan template creation must ask `商品全域` or `直播全域`; never silently default one kind. Generic cross-channel template creation must first ask Marketing or Qianchuan.
- “预检/校验这些作品创建计划” without explicit write permission is read-only `preflight_qianchuan_works`, even when rows include plan type or business owner. Never replace it with CLI batch creation or submission.
- `ok=true` and a `preflight_id` alone do not mean a group passed. Check each group's `status`, `counts`, `failed_results`, `skipped`, and `query_failures`; only `would_create`, `would_append`, or `already_present` describe a completed group preflight. Report failed or pending groups explicitly. Resolve this batch's F2 metadata before using warm owner hints; neither cached nor supplied identity hints skip the current public lookup or official authorization, ownership, and product checks. Plan naming can use the nickname returned by the current official creator check when no row nickname was supplied.
- For mixed-product or mixed-advertiser work links, use the same MCP preflight with all structured `items[]` and omit a global `plan_template` unless the user actually selected one template for the whole batch. Preserve each row's explicit `product_id` or exact configured `product_name`, `plan_template`, and `advertiser_id` when supplied. Core resolves a unique active product template and splits advertiser/template batches; never pick the first account or send all links under one template. Rows without explicit routing use public product hints; missing, multiple-product, unmatched, or ambiguous rows stay in `pending` for only the missing choice.
- If F2 cannot provide an identity, accept a user-supplied exact `creator_douyin_id` and a naming-only `creator_name` on that row; `creator_uid` is optional. Core must recover a unique numeric UID through an exact official visible-ID match, then still verify authorization, work ownership, and product. Nicknames never identify or authorize a creator. Do not explain an unknown F2 failure as a proven login or anti-bot block.
- For ordinary 全域/乘方 custom material, product, room, author, title, or creative reports, use `get_qianchuan_report_schema` when the exact topic/dimensions/metrics are unknown, then `report_qianchuan_custom` for that advertiser. Responsible-account fixed spend/ROI uses `report_qianchuan_managed_accounts`. Preserve dates, the selected topic and partial failures.
- For another approved official query, discover only its narrow catalog entry. `enabled=true` means local policy and contract allow a call; current OAuth availability remains unverified until the scoped query succeeds. Reuse a contract already established in this conversation. Do not invent an upstream tool, URL, credential, subject relationship or optional parameter.
- If one required identifier or business choice is genuinely missing, ask only for that item. Do not enumerate or scan unrelated creators/accounts.

## Execution Boundary

- In plugin-development conversations, business examples are fixtures. Do not read or modify real config, credentials, journals, or official APIs unless the user explicitly switches to real business execution.
- All business Runtime, MCP, OAuth state, Token, cache, config, F2 execution, and preflight snapshots stay on the user's machine. MCP is local stdio; never publish it to the public network.
- Never ask the user to paste a Secret, Token, refresh token, auth code, or Cookie into chat; never print one.
- All plan/material/settings mutations default to dry-run. Submit only after explicit permission for the exact advertiser, plan/material IDs, endpoint, and payload. Qianchuan delete additionally requires `--confirm-delete`.
- F2 public metadata is only an identity/product hint. Official Qianchuan targeted authorization, ownership, product, plan, and material checks remain authoritative.

## Mandatory Batch Presentation

For every work-link preflight or submitted batch result, when `presentation.required=true`, output `presentation.rendered_markdown` verbatim. Preserve exactly `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题` in that order. Show skipped, query failures, and failed results after the table; never replace it with an operational summary table. A write-capable `preflight_id` may be submitted only after exact user confirmation and before expiry.

When `routed=true`, label each `batches[]` entry with its advertiser and template, then apply that same contract to `batches[].preflight.presentation`. Show `pending` and child failures after the tables. There is no cross-account submit ID: preserve every successful child's own `preflight_id` and expiry. On a child authorization failure, use `batches[].failure.error` for recovery and retry only that child's original rows; do not rerun successful children or submit them again. CLI diagnostic results use `batches[].result` for the same child data.

## On-Demand Detail

Read `references/workflow-reference.md` only after the route is known and only for setup/OAuth, template lifecycle, creator discovery, batch submission, material removal, plan settings, explicitly advanced CLI reports, runs, or advanced output rules. Read `references/unified-report-routing.md` for custom reporting, `references/official-queries.md` for official task parameters and catalog queries, or `references/developer-docs.md` for an explicit optional documentation request. Direct task MCP calls with known arguments do not require the full manual.

## Output

- Preserve required Presentation, time range, report scope (`overall` includes 乘方; `uni` is 全域), channel, advertiser, partial failures, and true source.
- Preserve official query `columns[].unit`, decimal-string cells, nulls, `observed_at` and `completeness`. Raw units such as `1/100000 CNY` must not be presented as yuan without an exact decimal conversion. A partial slice cannot prove absence, a complete inventory or an account-wide total. Continue only with the returned bound cursor and the unchanged parameters; `contract_limited` queries may expose only the first upstream batch.
- Do not create files unless `--out` is explicit. Do not expose configuration paths, source links in stored snapshots, raw F2/official responses, credentials, or internal errors.
