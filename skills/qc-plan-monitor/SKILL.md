---
name: qc-plan-monitor
description: Dedicated 巨量千川 skill for first-run setup, local OAuth and Token refresh, responsible or commonly managed accounts, product/live templates, creator/work/product discovery, product or live all-domain plans, material changes, safe Douyin-work preflight and batch create/append, plan status/budget/ROI updates, and account/plan/product/room/author reports across 全域与乘方. Use for 千川初始化、授权、同步广告主、常用户、模板、达人/作品/商品/计划/素材、批量建计划或追加素材、消耗/ROI/GMV 报表 and official payload validation, including colloquial, abbreviated, misspelled, or contextual follow-ups.
---

# QC Plan Monitor

## Fast Routing Contract

Classify the user's outcome once, then use the first matching route. Do not search the repository, inspect plugin cache paths, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

Every `./run` below is this skill's own launcher. On Codex invoke it as written. On Claude Code invoke `${CLAUDE_PLUGIN_ROOT}/skills/qc-plan-monitor/run` with the same arguments, because the working directory is the user's project rather than this skill directory. Do not search for the launcher anywhere else.

<!-- capability-routes:start -->
| User outcome | Exact route |
| --- | --- |
| List local Marketing or Qianchuan templates | MCP `list_templates` |
| Show one exact template | MCP `get_template` |
| List accounts I manage/use | MCP `list_managed_accounts` |
| Query spend/performance for my managed Qianchuan account set | `./run accounts report --channel qianchuan` |
| Inspect Qianchuan Token/app/advertiser mapping | MCP `get_qianchuan_authorization` |
| Search selectable Qianchuan products | MCP `search_qianchuan_products` |
| List Qianchuan product all-domain plans | MCP `list_qianchuan_plans` |
| Show one exact plan and optional materials | MCP `get_qianchuan_plan` |
| Fixed account overall/uni report | MCP `report_qianchuan_account` |
| All-domain account report | MCP `report_qianchuan_uni_account` |
| Fixed plan report | MCP `report_qianchuan_plans` |
| Preflight Douyin works without creating or changing a plan | MCP `preflight_qianchuan_works` |
| Inspect one exact preflight snapshot | MCP `get_qianchuan_preflight` |
<!-- capability-routes:end -->

| User outcome | Exact route |
| --- | --- |
| Confirmed Ocean Watch goal not matched above | MCP `get_capabilities` once with `channel=qianchuan`, then use its unique command |
| Initialize, authorize, mutate templates/accounts/plans/materials, custom reports, or inspect runs | the unique `./run <domain> <action>` route in `references/workflow-reference.md` |

When a row matches, call that exact MCP tool immediately. MCP declarations may be deferred from the visible prompt: never enumerate `ALL_TOOLS`, inspect caches, or read memory to decide whether a named tool exists. Treat it as unavailable only when the direct call itself returns a Host-level unknown-tool or unavailable error; then report that error and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row already matches.

## Intent Rules

- This Skill owns only `qianchuan`. Never reuse Marketing apps, credentials, templates, advertiser mappings, endpoints, or creation transactions.
- Interpret “我负责的/常用的/管理的户” and contextual paraphrases as responsible-account intent. Membership means `list_managed_accounts`; spend, ROI, GMV, orders, or dated performance requires a fresh report.
- Generic Qianchuan template creation must ask `商品全域` or `直播全域`; never silently default one kind. Generic cross-channel template creation must first ask Marketing or Qianchuan.
- “预检/校验这些作品创建计划” without explicit write permission is read-only `preflight_qianchuan_works`, even when rows include plan type or business owner. Never replace it with CLI batch creation or submission.
- For ordinary 全域/乘方 custom material, product, room, author, title, or creative reports, choose the exact official topic/dimensions with `qc-reports schema` before `qc-reports custom`; do not substitute legacy material reports or plan-list statistics.
- If one required identifier or business choice is genuinely missing, ask only for that item. Do not enumerate or scan unrelated creators/accounts.

## Execution Boundary

- In plugin-development conversations, business examples are fixtures. Do not read or modify real config, credentials, journals, or official APIs unless the user explicitly switches to real business execution.
- All business Runtime, MCP, OAuth state, Token, cache, config, F2 execution, and preflight snapshots stay on the user's machine. MCP is local stdio; never publish it to the public network.
- Never ask the user to paste a Secret, Token, refresh token, auth code, or Cookie into chat; never print one.
- All plan/material/settings mutations default to dry-run. Submit only after explicit permission for the exact advertiser, plan/material IDs, endpoint, and payload. Qianchuan delete additionally requires `--confirm-delete`.
- F2 public metadata is only an identity/product hint. Official Qianchuan targeted authorization, ownership, product, plan, and material checks remain authoritative.

## Mandatory Batch Presentation

For every work-link preflight or submitted batch result, when `presentation.required=true`, output `presentation.rendered_markdown` verbatim. Preserve exactly `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题` in that order. Show skipped, query failures, and failed results after the table; never replace it with an operational summary table. A write-capable `preflight_id` may be submitted only after exact user confirmation and before expiry.

## On-Demand Detail

Read `references/workflow-reference.md` only after the route is known and only for setup/OAuth, template lifecycle, creator discovery, batch submission, material removal, plan settings, custom reports, runs, or advanced output rules. Read `references/unified-report-routing.md` only for custom report routing. Do not read the full reference for the eleven direct business MCP routes or `get_capabilities`.

## Output

- Preserve required Presentation, time range, report scope (`overall` includes 乘方; `uni` is 全域), channel, advertiser, partial failures, and true source.
- Do not create files unless `--out` is explicit. Do not expose configuration paths, source links in stored snapshots, raw F2/official responses, credentials, or internal errors.
