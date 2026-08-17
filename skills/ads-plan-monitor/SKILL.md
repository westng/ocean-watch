---
name: ads-plan-monitor
description: Dedicated 巨量营销 skill for first-run setup, local OAuth, current authorized-user advertiser freshness, responsible or commonly managed accounts, advertiser-bound templates, uploaded or creator materials, plan creation and updates, performance reports, run history, and evidence-based strategy. Use for 巨量营销初始化、授权或刷新 Token、同步新增或移除的广告主、常用账户、模板、素材、计划、消耗/ROI/GMV 报表和投放策略 through official APIs, including colloquial, abbreviated, misspelled, or contextual follow-ups.
---

# Ads Plan Monitor

## Fast Routing Contract

Classify the user's outcome once, then use the first matching route. Do not search the repository, inspect plugin cache paths, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

| User outcome | Exact route |
| --- | --- |
| List local Marketing or Qianchuan templates | MCP `list_templates` |
| Show one exact template | MCP `get_template` |
| List accounts I manage/use | MCP `list_managed_accounts` |
| Query spend/performance for my managed account set | `./run accounts report` |
| Inspect Marketing Token/app/advertiser mapping | MCP `get_marketing_authorization` |
| Search uploaded Marketing videos | MCP `search_marketing_videos` |
| Search Marketing creator materials | MCP `search_marketing_creator_materials` |
| Fixed Marketing material report | MCP `report_marketing_materials` |
| Fixed Marketing project report | MCP `report_marketing_plans` |
| Make current OAuth user's advertiser coverage match official access | `./run auth sync-accounts --channel marketing` |
| Confirmed Ocean Watch goal not matched above | MCP `get_capabilities` once with `channel=marketing`, then use its unique command |
| Initialize, authorize, mutate local accounts/templates, create/update plans, custom reports, or inspect runs | the unique `./run <domain> <action>` route in `references/workflow-reference.md` |

When a row matches, call that exact MCP tool immediately. MCP declarations may be deferred from the visible prompt: never enumerate `ALL_TOOLS`, inspect caches, or read memory to decide whether a named tool exists. Treat it as unavailable only when the direct call itself returns a Host-level unknown-tool or unavailable error; then report that error and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row already matches.

## Intent Rules

- This Skill owns only `marketing`. Route every Qianchuan request to `$qc-plan-monitor`; never mix apps, credentials, templates, advertiser mappings, endpoints, or plan transactions.
- Interpret “我负责的/常用的/管理的户” and contextual paraphrases as responsible-account intent. Membership means `list_managed_accounts`; spend, ROI, GMV, orders, or dated performance requires a fresh report in the current turn.
- A request to make the local authorization reflect newly granted, removed, stale, or missing official advertiser access means `auth sync-accounts --channel marketing`, regardless of exact wording, abbreviations, omissions, or misspellings.
- Generic “创建投放模板” without a channel must use `templates create` without `--channel` and ask Marketing or Qianchuan. After Marketing, ask uploaded/mixed materials or creator-native materials. Do not infer a channel from active state.
- If one required identifier or business choice is genuinely missing, ask only for that item. Do not run discovery unrelated to the missing field.

## Execution Boundary

- In plugin-development conversations, business examples are requirements or fixtures. Do not read or modify real config, credentials, journals, or official APIs unless the user explicitly switches to real business execution.
- Use the Plugin-provided MCP tools for the common reads above. Use `./run` on Unix or `run.cmd` on Windows only for the explicitly routed remaining operation.
- All business Runtime, MCP, OAuth state, Token, cache, and config stay on the user's machine. Never ask the user to paste a Secret, Token, refresh token, auth code, or Cookie into chat; never print one.
- Default every plan mutation to preview/dry-run. Add `--submit` only after explicit permission for the exact advertiser, objects, payload, and action. Deletion requires its existing extra confirmation gate.
- Strategies are evidence-based and read-only unless the user explicitly requests an exact write.

## On-Demand Detail

Read `references/workflow-reference.md` only after the route is known and only for setup/OAuth, account mutation, template mutation, plan create/update, custom report, run inspection, or strategy details. Read the narrower existing reference named there when required. Do not read the full reference for the eight direct business MCP routes or `get_capabilities`.

## Output

- Preserve `presentation.rendered_markdown` verbatim when `presentation.required=true`.
- Report official-source time range, channel, advertiser scope, partial failures, and whether any local or online write occurred.
- Do not create result files unless `--out` is explicit. Do not expose configuration paths, raw responses, credentials, or internal errors.
