# Qianchuan MCP Capability Routing

> Organization: westng
> Project: ocean-watch
> Skill: qc-plan-monitor

## Source Of Truth

MCP is optional. Its absence must not block the Plugin's local setup, OAuth, templates, OpenAPI queries, or guarded write workflows.

The runtime `tools/list` response is the source of truth for MCP availability. The supplied July 2026 capability snapshot contains 220 tools: 151 `qianchuan_*_v1` tools, 3 OAuth tools, 33 general v2 tools, and 33 general v3 tools. Treat that snapshot as discovery guidance only because the server can add, remove, or change tools and input schemas.

Use these commands for the locally configured developer MCP:

```bash
ocean-watch mcp status
ocean-watch mcp capabilities
ocean-watch mcp capabilities --tool TOOL_NAME
```

The list command returns runtime names and descriptions. The single-tool form returns the complete runtime tool definition, including its input schema. Never infer required parameters from a tool name or this reference.

## Selection Rules

Prefer MCP for an official remote operation only when all of these conditions hold:

1. The MCP is configured and the exact tool appears in the current runtime inventory.
2. Its runtime description and input schema match the requested operation and identifiers.
3. The operation can use credentials without printing or requesting them in chat. Persist Developer ID only in the local system credential store after successful MCP verification; never persist App Secret, Access Token, Refresh Token, auth code, or a sensitive MCP URL outside the existing credential-store contract.
4. The MCP path preserves the Plugin's advertiser binding, pagination, validation, output, and write-safety contract.

Use the bundled CLI instead for local config, OAuth browser setup, credential rotation or persistence, templates, responsible-account registry changes, local caches, journals, and work-link resolution. These operations include local state or orchestration that a remote MCP tool does not provide.

For a read, a missing tool, schema mismatch, pre-dispatch MCP failure, or unsupported credential injection may fall back to the existing CLI/OpenAPI path. Report the fallback when it changes freshness or completeness.

For a write, always produce the existing CLI dry-run/preflight first and obtain the same explicit confirmation required by the Skill. Use MCP only for the exact validated operation. Fall back to OpenAPI only when failure occurred before dispatch. If dispatch may have occurred or the outcome is unknown, do not retry through another transport; query current state and reconcile first. Never replace a multi-step batch or resumable journal workflow with isolated direct MCP calls.

## Plugin Operation Intersection

The current capability snapshot contains the following exact tools for operations already owned by this Plugin. Runtime discovery must still confirm each tool before use.

| Plugin operation | Preferred MCP tool(s) | Boundary |
| --- | --- | --- |
| Authorized Qianchuan creators | `qianchuan_uni_aweme_authorized_get_v1` | Require exact visible/numeric creator identity checks. |
| Product-filtered creator videos | `qianchuan_file_video_aweme_get_v1` | Preserve cursor pagination, product filtering, and deduplication. |
| Merchant all-domain products | `qianchuan_uni_promotion_product_get_v1` | Use only for the advertiser types accepted by the tool schema. |
| Creator/institution all-domain products | `qianchuan_uni_promotion_product_aweme_get_v1` | Preserve advertiser and creator binding. |
| All-domain plan list | `qianchuan_uni_promotion_list_v1` | Never treat `stats_info` as report currency. |
| All-domain plan detail | `qianchuan_uni_promotion_ad_detail_v1` | Use for final numeric creator/product identity checks. |
| All-domain plan materials | `qianchuan_uni_promotion_ad_material_get_v1` | Preserve material status and ID semantics. |
| All-domain plan report contract | `qianchuan_report_uni_promotion_config_get_v1` | Inspect available metrics and dimensions first. |
| All-domain plan report data | `qianchuan_report_uni_promotion_data_get_v1` | Authoritative financial source for product all-domain plans. |
| Material performance | `qianchuan_report_material_get_v1` | Preserve full pagination and raw-value aggregation. |
| Create all-domain plan | `qianchuan_uni_aweme_ad_create_v1` | Only after template/runtime validation and explicit submit confirmation. |
| Add all-domain materials | `qianchuan_uni_promotion_ad_material_add_v1` | Keep batch reconciliation and advertiser lock semantics. |
| Delete all-domain materials | `qianchuan_uni_promotion_ad_material_delete_v1` | Require dry-run, explicit submit, and post-write verification. |
| Update all-domain status | `qianchuan_uni_promotion_ad_status_update_v1` | Preserve ten-ID limit; `DELETE` requires delete confirmation. |
| Update all-domain budget | `qianchuan_uni_promotion_ad_budget_update_v1` | Preserve ten-ID limit and partial-failure handling. |
| Update all-domain ROI | `qianchuan_uni_promotion_ad_roi2_goal_update_v1` | Validate the plan optimization goal before dispatch. |

## Additional Advertised Families

The snapshot also advertises Qianchuan account balance and finance, standard-promotion plans, audiences, Xiaodian promotion orders, campaign groups, images/videos/carousels, standard and custom reports, live-room dashboards, suggestions, logs, smart boost, all-domain control tasks, authorization, products, and review suggestions. General tools cover OAuth, agency accounts and finance, uploads, geographic/targeting dictionaries, transfers, subscriptions, security, and comments.

Those families are MCP capabilities, but they are not automatically Plugin workflows. Use one only when the user's request is in scope, its current schema is inspected, credentials can be handled safely, and the response contract can be validated. Otherwise retain the existing CLI/OpenAPI path or report that the Plugin does not yet wrap that operation.
