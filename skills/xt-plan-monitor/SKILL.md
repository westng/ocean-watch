---
name: xt-plan-monitor
description: Dedicated 巨量星图 skill for local OAuth, authorization recovery, authorized-account synchronization, managed Star Map accounts, and local app/token/account mappings through the shared Ocean Watch framework. Use for 星图初始化、授权、重新授权、同步账号、常用或负责账号及授权状态, including contextual follow-ups. Star Map plan, material, and performance-report APIs are not integrated yet.
---

# Star Map Plan Monitor

## Fast Routing Contract

This Skill owns `star_map`. `xt-plan-monitor` is the Skill name; never replace the Runtime channel key with that name. Classify the outcome once. Do not search the repository, inspect plugin caches, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

Every `./run` below is documentation shorthand for this skill's launcher. Resolve the absolute directory of the loaded `SKILL.md` and invoke `<skill-directory>/run` (macOS/Linux) or `<skill-directory>\run.cmd` (Windows), quoting the path. This applies to Codex, Claude Code and DeepSeek Harness; the conversation's working directory is not the skill directory. Claude Code may also use `${CLAUDE_PLUGIN_ROOT}/skills/xt-plan-monitor/run`. Do not search unrelated directories. WorkBuddy uses MCP only.

<!-- capability-routes:start -->
| User outcome | Exact route |
| --- | --- |
| List local managed Star Map accounts | MCP `list_managed_accounts` |
| Inspect Star Map app/token/account authorization | MCP `get_star_map_authorization` |
| Make current Star Map user's account coverage match official access | MCP `start_account_sync` |
<!-- capability-routes:end -->

Pass `channel=star_map` to account-list and authorization-management tools. `get_star_map_authorization` already fixes the channel; use its optional `subject_id` only for an exact Star Map account. Account sync requires an exact `authorization_id` from that inspector or a saved authorization session. Do not choose among multiple grants without sufficient account/user context.

| User outcome | Exact route |
| --- | --- |
| Authorize, reauthorize, or recover expired Star Map access | MCP `start_authorization` with `channel=star_map` |
| Query or cancel the current authorization session | MCP `get_authorization_session` or `cancel_authorization_session` with the same channel and session ID |
| Confirmed Star Map setup or local-account operation not matched above | MCP `get_capabilities` once with `channel=star_map`; read [references/workflow-reference.md](references/workflow-reference.md) for the supported CLI scope |

When a row matches, use that exact logical MCP tool. If its declaration is deferred, use the host's supported tool search/discovery mechanism, targeted at `ocean-watch` and the exact tool name (including a filtered `ALL_TOOLS` lookup when that is the host API). Read the returned input Schema and invoke the discovered callable name; do not guess a namespace or arguments. Discovery is not a business query and does not require `get_capabilities`. If discovery or invocation reports the tool unavailable, report that error and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row matches. Shared catalog entries for templates, reports, or runs do not establish Star Map support.

## Authorization Recovery

For new authorization, open `authorization_url` returned by `start_authorization`. With `awaiting_configuration`, the user fills App ID/Secret only in that local page, then continues to official OAuth using the same session ID; `awaiting_callback` opens official OAuth directly. For reauthorization, include the exact existing `authorization_id`; Core verifies the official App and user before replacing that grant. Keep original account/request arguments. If a tool needs the optional account scope, its legacy `advertiser_id` field contains the exact Star Map account ID, not a Marketing or Qianchuan advertiser.

For saved grants with pending synchronization, call `start_account_sync` and open its local confirmation page instead of repeating OAuth. Both paths use `get_authorization_session` with `channel=star_map` until a terminal state. Only `completed` with `can_resume=true` permits retrying the original local query. Stop automatic retries on failure, cancellation, expiry, or a scope mismatch. On cancellation use `cancel_authorization_session`.

Account rebinding requires explicit user approval for the affected accounts before `rebind_existing=true`. Missing App configuration must be handled through the documented local configuration flow, without asking for secrets in chat or passing them through the model. Session completion establishes saved credentials and account scope, not support for unimplemented Star Map plan/report APIs.

## Channel and Output Boundaries

- Treat Star Map IDs as `subject_kind=star_account`. Separate the local managed-account registry from the official authorization snapshot; list membership alone does not prove current access or Token validity.
- Keep apps, grants, account mappings, and credentials isolated from Marketing and Qianchuan. Explicit requests for those channels belong to `$ads-plan-monitor` or `$qc-plan-monitor`.
- Star Map plan creation, templates, materials, spend, ROI, GMV, and performance reports are not integrated. Explain the missing capability instead of substituting another channel's API or inventing results.
- In plugin-development conversations, examples are fixtures; do not access real credentials, business state, or official APIs unless the user requests real business execution.
- All Runtime state and credentials remain local. Never expose Secret, Token, auth code, Cookie, raw official responses, or internal paths in chat or logs.
- Preserve `presentation.rendered_markdown` when `presentation.required=true`. Identify the channel, account scope, source, pending sync, and partial failures. Do not create result files without explicit `--out`.
