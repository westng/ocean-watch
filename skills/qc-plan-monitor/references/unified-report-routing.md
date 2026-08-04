# Qianchuan Unified And Overall Report Routing

Use this reference after the Skill has identified a Qianchuan 全域 or 乘方 reporting intent. Natural-language understanding chooses the subject; these deterministic contracts choose the command and official endpoint.

## Semantic Selection

| Intended subject | Command | Official endpoint | Required scope |
| --- | --- | --- | --- |
| One advertiser including 乘方 or combined account data | `qc-reports account` | `GET /v1.0/qianchuan/report/all_promotion/get/` | advertiser, dates, fields, `adlab_scene` |
| One advertiser limited to 全域 account data | `qc-reports uni-account` | `GET /v1.0/qianchuan/report/uni_promotion/get/` | advertiser, dates, fields |
| Available report dimensions and metrics | `qc-reports schema` | `GET /v1.0/qianchuan/report/uni_promotion/config/get/` | advertiser, one or more topics |
| Custom topic/dimension/metric report | `qc-reports custom` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, topic, dimensions, metrics, dates |
| Product performance | `qc-reports products` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, report mode, dates |
| Live-room performance | `qc-reports rooms` | `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/` | advertiser, exact `room_id`, dates |
| Douyin-author performance | `qc-reports authors` | `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/` | advertiser, exact numeric `aweme_id`, dates |

Do not route responsible-account-set performance here; use `accounts report`. Do not route product asset discovery here; use `qc-products list/search`. Do not route plan or material rows here; use `qc-reports plans/materials`.

## Account Contracts

For `qc-reports account`, pass `adlab_scene=OVERALL_PROJECT` by default. The official endpoint requires `advertiser_id`, `start_time`, `end_time`, `adlab_scene`, and `fields`. It accepts `data_period=ALL_DATA|OVER_ALL_DATA|UNI_DATA` only with `OVERALL_PROJECT`; reject `data_period` for `UNI_PROJECT` rather than sending an invalid request.

For `qc-reports uni-account`, pass `advertiser_id`, `start_date`, `end_date`, and explicit fields. Do not add `adlab_scene` or `data_period` because they belong to the combined/乘方 endpoint contract.

Both commands accept `marketing_goal` and `order_platform`. Preserve an explicit user scope; otherwise use `ALL` and `QIANCHUAN`.

## Custom And Product Contracts

Use `SITE_PROMOTION_PRODUCT_PRODUCT` for `qc-reports products --report-mode uni`. Use `OVERALL_ROI_PRODUCT_PRODUCT` for `--report-mode overall`. A product ID narrows report data through `--filter product_id=ID`; it is not a request to query product assets.

For `qc-reports custom`, obtain a valid topic, dimensions, and metrics from the user or `qc-reports schema`. Preserve filters as `{field, operator: 7, values}` and retain ID values as strings. Pass `data_period` only when the chosen topic supports it.

Traverse every declared page. Retry only the failed page for official `40100`, `51010`, HTTP `429`, and supported temporary transport failures. Require stable `total_page` and `total_number`, and reject a final row count that contradicts official pagination. `--top` limits display only; it must not stop pagination or change totals.

## Room And Author Contracts

`qc-reports rooms` requires an exact numeric `room_id`. `qc-reports authors` requires an exact numeric `aweme_id`; a visible Douyin ID or nickname is not the API identifier. Resolve an exact authorized creator first when necessary and stop on no match or multiple matches.

Use `TIME_GRANULARITY_DAILY` by default and switch to `TIME_GRANULARITY_HOURLY` only when the requested view is hourly or otherwise requires that granularity. Preserve an explicit marketing goal on author reports.

## Output And Safety

- Default dates to the current local day when omitted.
- Keep advertiser, product, room, author, plan, and material IDs as exact decimal strings in output.
- Do not write a report file unless `--out` is explicit.
- Present the requested dimensions and metrics without silently changing their business meaning.
- Never expose Access Tokens or stored authorization data.
