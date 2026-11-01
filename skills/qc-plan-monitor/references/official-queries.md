# Approved Qianchuan Official Queries

Use this reference after selecting an official task or a query that the task routes do not cover. The main Skill's direct task routes take precedence.

| User task | MCP input contract |
| --- | --- |
| Balance, daily budget, quota | `get_qianchuan_account_health`: `advertiser_id` or `managed_accounts=true`; optional `checks=["balance","budget","quota"]`, `auth_account_id`, `limit` 1–20 and bound `cursor` |
| Plan diagnosis | `diagnose_qianchuan_plan`: exact `advertiser_id`, `plan_id`; optional `scene=uni|overall|legacy`, `auth_account_id` |
| Material labels | `inspect_qianchuan_material_quality`: `advertiser_id` and either `material_ids` or `works`. Each work uses `url`, optional `product_id`, `creator_douyin_id`, `creator_uid`, naming-only `creator_name` |
| Official suggestion | `get_qianchuan_delivery_suggestions`: `advertiser_id`, `kind=budget|roi|bid|audit`, `scene=legacy|uni|overall`, optional exact `plan_id`, `auth_account_id`, approved `parameters` |

`auth_account_id` selects an authorized numeric account from the current local mapping. The authorization/grant reference is a separate identity. For an exact advertiser with one verified mapping, omit this optional selector; when the mapping is ambiguous, resolve the authorized account before calling. A grant reference may be used as `scope.id` only for a catalog entry that explicitly allows `scope.kind=authorization`.

For a suggestion, use the chosen scene's contract. Legacy budget and bid need an exact creator; this integration requires `aweme_id` for bid because the catalog cannot verify the merchant-role exemption. Legacy ROI accepts its documented goal/action fields. All-domain budget supports the official creator/product suggestion contract; ROI and bid are currently legacy only. An exact plan may supply verified creator/product/goal parameters; contradictory user values are rejected. Missing fields are returned in `required_fields`, and only the unresolved business choice should be requested from the user.

Works always attempt the current F2 lookup, then the exact official creator, ownership and target-product checks. An explicit visible Douyin ID may recover missing public metadata. Product selection stays explicit or uniquely supported by the public hint; 100 works and at most 30 distinct target products form one bounded identity request. No preflight snapshot or plan write is created by material inspection.

## Catalog and Scoped Read

1. Search `get_qianchuan_official_capabilities` with a narrow `keyword` or `group`, optionally a bound catalog cursor. The 156 entries describe local contracts; they do not prove that the current user has every official role.
2. Request the selected exact `query_id` to obtain `input_schema`, `request_fields`, `scope_kinds`, conditional rules, output fields, units, paging variants and mapping evidence.
3. Call `query_qianchuan_official` with `query_id`, `scope={kind,id}`, optional `auth_account_id`, `parameters`, `limit` and a returned `cursor`. IDs in Host inputs are canonical decimal strings. `public_catalog` uses an empty scope ID; all other subjects use their exact known ID. Never substitute an advertiser for an agent, shop, customer center, organization or grant.
4. Repeat only with the returned cursor and unchanged query, scope, selector, parameters and limit. User-supplied page/cursor/offset parameters are rejected. A catalog cursor and a result cursor are different.

`contract_verified` means the frozen local contract is verified. `contract_limited` means a documented first-batch limitation remains: boost versions and subscribed-account lists have no verified termination rule; the new-product/breakthrough-product tabs of the all-domain product query also remain first-batch only. The available-product new-product-boost scene uses its documented `has_more` cursor model. Limits or unknown pagination always remain visible as incomplete; they cannot establish “no matching plan” or a complete report.

Only documented GET capabilities are dispatched. MCP transport POST carries these reads; application-token retrieval for subscription queries is private authentication control. The generic query cannot change the method, endpoint, backend, toolset, mode or credentials. SDK-backed existing reports and work preflight keep their current source and business rules.

On an upstream or Schema error, preserve the controlled code, stage, API code and missing-field paths. Unknown fields, units or subject relationships are not guessed. `SCHEMA_REVIEW_REQUIRED` blocks the affected query and does not expand the approved whitelist. A successful outer tool response can contain `status=partial|failed`; inspect each fact, failure and skipped check.
