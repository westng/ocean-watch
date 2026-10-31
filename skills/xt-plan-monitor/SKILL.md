---
name: xt-plan-monitor
description: Dedicated 巨量星图 skill for local OAuth, authorization recovery, authorized-account synchronization, managed Star Map accounts, and local app/token/account mappings through the shared Ocean Watch framework. Use for 星图初始化、授权、重新授权、同步账号、常用或负责账号及授权状态, including contextual follow-ups. Star Map plan, material, and performance-report APIs are not integrated yet.
---

# Star Map Plan Monitor

## Fast Routing Contract

This Skill owns `star_map`. `xt-plan-monitor` is the Skill name; never replace the Runtime channel key with that name. Classify the outcome once. Do not search the repository, inspect plugin caches, read memory, or run setup probes before a normal business request. Do not call an equivalent CLI route when this table names an MCP tool.

Every `./run` below is this skill's launcher. On Codex invoke it as written. On Claude Code use `${CLAUDE_PLUGIN_ROOT}/skills/xt-plan-monitor/run` with the same arguments. On DeepSeek Harness, resolve `<skill-directory>/run` or `<skill-directory>\run.cmd` from the absolute directory of the loaded `SKILL.md`, rather than the conversation's working directory. WorkBuddy uses the MCP routes only.

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

When a row matches, call that exact MCP tool immediately; never enumerate `ALL_TOOLS` or inspect caches to locate deferred declarations. If the direct call reports an unavailable tool, report it and stop without a CLI fallback. Do not call `get_capabilities` when a preceding row matches. Shared catalog entries for templates, reports, or runs do not establish Star Map support.

## Authorization Recovery

For new authorization, open the official OAuth URL returned by `start_authorization`. For reauthorization, include the exact existing `authorization_id`; Core verifies the official App and user before replacing that grant. Keep original account/request arguments. If a tool needs the optional account scope, its legacy `advertiser_id` field contains the exact Star Map account ID, not a Marketing or Qianchuan advertiser.

For saved grants with pending synchronization, call `start_account_sync` and open its local confirmation page instead of repeating OAuth. Both paths use `get_authorization_session` with `channel=star_map` until a terminal state. Only `completed` with `can_resume=true` permits retrying the original local query. Stop automatic retries on failure, cancellation, expiry, or a scope mismatch. On cancellation use `cancel_authorization_session`.

Account rebinding requires explicit user approval for the affected accounts before `rebind_existing=true`. Missing App configuration must be handled through the documented local configuration flow, without asking for secrets in chat or passing them through the model. Session completion establishes saved credentials and account scope, not support for unimplemented Star Map plan/report APIs.

## Channel and Output Boundaries

- Treat Star Map IDs as `subject_kind=star_account`. Separate the local managed-account registry from the official authorization snapshot; list membership alone does not prove current access or Token validity.
- Keep apps, grants, account mappings, and credentials isolated from Marketing and Qianchuan. Explicit requests for those channels belong to `$ads-plan-monitor` or `$qc-plan-monitor`.
- Star Map plan creation, templates, materials, spend, ROI, GMV, and performance reports are not integrated. Explain the missing capability instead of substituting another channel's API or inventing results.
- In plugin-development conversations, examples are fixtures; do not access real credentials, business state, or official APIs unless the user requests real business execution.
- All Runtime state and credentials remain local. Never expose Secret, Token, auth code, Cookie, raw official responses, or internal paths in chat or logs.
- Preserve `presentation.rendered_markdown` when `presentation.required=true`. Identify the channel, account scope, source, pending sync, and partial failures. Do not create result files without explicit `--out`.
