---
name: ads-plan-monitor
description: Dedicated 巨量营销 skill for first-run setup, local OAuth, current authorized-user advertiser freshness, responsible or commonly managed accounts, advertiser-bound templates, uploaded or creator materials, plan creation/update previews, performance reports, run history, and evidence-based strategy. Use for 巨量营销初始化、授权或刷新 Token、同步新增或移除的广告主、常用账户、模板、素材、计划、消耗/ROI/GMV 报表和投放策略 through official APIs, including colloquial, abbreviated, misspelled, or contextual follow-ups.
---

# Ads Plan Monitor

## Fast Routing Contract

Classify the user's outcome once, then use the first matching route. Do not search the repository, inspect plugin cache paths, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

Every `./run` below is documentation shorthand for this skill's launcher. Resolve the absolute directory of the loaded `SKILL.md` and invoke `<skill-directory>/run` (macOS/Linux) or `<skill-directory>\run.cmd` (Windows), quoting the path. This applies to Codex, Claude Code and DeepSeek Harness; the conversation's working directory is not the skill directory. Claude Code may also use `${CLAUDE_PLUGIN_ROOT}/skills/ads-plan-monitor/run`. Do not search unrelated directories. WorkBuddy uses MCP only.

<!-- capability-routes:start -->
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
| Make current OAuth user's advertiser coverage match official access | MCP `start_account_sync` |
<!-- capability-routes:end -->

| User outcome | Exact route |
| --- | --- |
| Authorize or recover expired Marketing access | MCP `start_authorization` with `channel=marketing` and the original advertiser if known |
| Retry saved authorization account discovery | MCP `start_account_sync` with `channel=marketing` and the exact authorization ID |
| Confirmed Ocean Watch goal not matched above | MCP `get_capabilities` once with `channel=marketing`, then use its unique command |
| Initialize, mutate local accounts/templates, create/update plans, custom reports, or inspect runs | the unique `./run <domain> <action>` route in `references/workflow-reference.md` |

When a row matches, use that exact logical MCP tool. If its declaration is deferred, use the host's supported tool search/discovery mechanism, targeted at `ocean-watch` and the exact tool name (including a filtered `ALL_TOOLS` lookup when that is the host API). Read the returned input Schema and invoke the discovered callable name; do not guess a namespace or arguments. Discovery is not a business query and does not require `get_capabilities`. If discovery or invocation reports the tool unavailable, report that error and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row matches.

## Authorization Recovery

On `REAUTHORIZATION_REQUIRED`, preserve the original request arguments and call MCP `start_authorization` with `channel=marketing` and the original advertiser if known. Open the returned `authorization_url`: `awaiting_configuration` opens the local App ID/Secret form and continues to official OAuth in the same session; `awaiting_callback` opens official OAuth directly; account synchronization opens the local confirmation page. Poll MCP `get_authorization_session` using its session ID and channel; only `completed` with `can_resume=true` permits rerunning the original read with the same arguments. Never ask for secrets, require the same links again, or suggest terminal commands for this MCP flow. Stop automatic retries on failure, cancellation, expiry or scope mismatch. Use MCP `cancel_authorization_session` when the user cancels.

For reauthorization, include the exact existing `authorization_id` supplied by the error or the local authorization inspector. Core verifies the official user UID and App before updating that grant, saves the credential before role-based discovery, and retains a saved grant for account synchronization if discovery fails.

On `ACCOUNT_SYNC_REQUIRED`, obtain the exact pending authorization ID from the authorization inspector and call MCP `start_account_sync`; a session with `sync_required=true` already supplies that ID. Preserve the advertiser, present its browser confirmation page and poll the session; do not generate another OAuth grant. Authorization recovery does not approve advertising writes.

## Intent Rules

- This Skill owns only `marketing`. Route Qianchuan requests to `$qc-plan-monitor` and Star Map requests to `$xt-plan-monitor`; never mix apps, credentials, templates, advertiser mappings, endpoints, or plan transactions.
- Interpret “我负责的/常用的/管理的户” and contextual paraphrases as responsible-account intent. Membership means `list_managed_accounts`; spend, ROI, GMV, orders, or dated performance requires a fresh report in the current turn.
- A request to make local authorization reflect official advertiser access means MCP `start_account_sync` with `channel=marketing` and an exact authorization ID from the local inspector, regardless of wording. Browser confirmation is required before synchronization.
- For template creation, use `./run templates create --input-json` as documented in [references/template-create.md](references/template-create.md). Start with known fields or `{}`; ask only for returned `missing_fields` and use returned `choices`. Never infer a missing channel or material source from active state; do not run the terminal wizard for an agent workflow.
- If one required identifier or business choice is genuinely missing, ask only for that item. Do not run discovery unrelated to the missing field.

## Execution Boundary

- In plugin-development conversations, business examples are requirements or fixtures. Do not read or modify real config, credentials, journals, or official APIs unless the user explicitly switches to real business execution.
- Use the Plugin-provided MCP tools for the common reads above. Use `./run` on Unix or `run.cmd` on Windows only for the explicitly routed remaining operation.
- All business Runtime, MCP, OAuth state, Token, cache, and config stay on the user's machine. Never ask the user to paste a Secret, Token, refresh token, auth code, or Cookie into chat; never print one.
- Current alpha supports Marketing plan previews only. Marketing creation, deletion and budget/bid/ROI/status submission have not migrated to trusted operations and are unavailable even with user permission. Do not add `--submit`, elevate policy or retry a legacy write. Local template/account management retains its own documented boundary.
- Strategies are evidence-based and read-only unless the user explicitly requests an exact write.

## On-Demand Detail

Read `references/workflow-reference.md` only after the route is known and only for setup/OAuth, account mutation, template mutation, plan create/update, custom report, run inspection, or strategy details. Read the narrower existing reference named there when required. Do not read the full reference for the eight direct business MCP routes or `get_capabilities`.

## Output

- Preserve `presentation.rendered_markdown` verbatim when `presentation.required=true`.
- Report official-source time range, channel, advertiser scope, partial failures, and whether any local or online write occurred.
- Do not create result files unless `--out` is explicit. Do not expose configuration paths, raw responses, credentials, or internal errors.
