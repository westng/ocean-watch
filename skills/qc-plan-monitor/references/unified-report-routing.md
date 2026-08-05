# Qianchuan Unified And Overall Report Routing

Use this reference after the Skill has identified a Qianchuan 全域 or 乘方 reporting intent. Natural-language understanding chooses the subject; these deterministic contracts choose the command and official endpoint.

## Semantic Selection

| Intended subject | Command | Official endpoint | Required scope |
| --- | --- | --- | --- |
| One advertiser including 乘方 or combined account data | `qc-reports account` | `GET /v1.0/qianchuan/report/all_promotion/get/` | advertiser, dates, fields, `adlab_scene` |
| One advertiser limited to 全域 account data | `qc-reports uni-account` | `GET /v1.0/qianchuan/report/uni_promotion/get/` | advertiser, dates, fields |
| Available data-topic list, dimensions, and metrics | `qc-reports schema [--data-topic TOPIC] [--managed-accounts]` | `GET /v1.0/qianchuan/report/uni_promotion/config/get/` | one or more advertisers, optional topics |
| Custom topic/dimension/metric report | `qc-reports custom` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | one or more advertisers, topic, dimensions, metrics, dates |
| All-domain post video-material performance | `qc-reports custom --data-topic SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, dates, material dimensions, metrics |
| All-domain post image-material performance | `qc-reports custom --data-topic SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, dates, material dimensions, metrics |
| All-domain post title-material performance | `qc-reports custom --data-topic SITE_PROMOTION_PRODUCT_POST_DATA_TITLE` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, dates, title dimension, metrics |
| All-domain post other-creative performance | `qc-reports custom --data-topic SITE_PROMOTION_PRODUCT_POST_DATA_OTHER` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, dates, creative dimension, metrics |
| Combined overall product-material performance | `qc-reports custom --data-topic OVERALL_ROI_PRODUCT_MATERIAL` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, dates, material type filter, dimensions, metrics |
| Product performance | `qc-reports products` | `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | advertiser, report mode, dates |
| Live-room performance | `qc-reports rooms` | `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/` | advertiser, exact `room_id`, dates |
| Douyin-author performance | `qc-reports authors` | `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/` | advertiser, exact numeric `aweme_id`, dates |

Do not route responsible-account-set performance here; use `accounts report`. Do not route product asset discovery here; use `qc-products list/search`. Do not route plan rows here; use `qc-reports plans`. Do not route ordinary 全域素材维度, 投后素材, or video-material performance to the legacy `qc-reports materials` command.

## Account Contracts

For `qc-reports account`, pass `adlab_scene=OVERALL_PROJECT` by default. The official endpoint requires `advertiser_id`, `start_time`, `end_time`, `adlab_scene`, and `fields`. It accepts `data_period=ALL_DATA|OVER_ALL_DATA|UNI_DATA` only with `OVERALL_PROJECT`; reject `data_period` for `UNI_PROJECT` rather than sending an invalid request.

For `qc-reports uni-account`, pass `advertiser_id`, `start_date`, `end_date`, and explicit fields. Do not add `adlab_scene` or `data_period` because they belong to the combined/乘方 endpoint contract.

Both commands accept `marketing_goal` and `order_platform`. Preserve an explicit user scope; otherwise use `ALL` and `QIANCHUAN`.

## Custom And Product Contracts

Use `SITE_PROMOTION_PRODUCT_PRODUCT` for `qc-reports products --report-mode uni`. Use `OVERALL_ROI_PRODUCT_PRODUCT` for `--report-mode overall`. A product ID narrows report data through `--filter product_id=ID`; it is not a request to query product assets.

For `qc-reports schema`, treat `数据主题列表`, `有哪些数据主题`, `可用维度`, `可用指标`, and similar requests as schema intent. With no explicit topic, let the command query the default common Qianchuan topic list; when the user names one or more topics, pass them exactly. It supports repeated or comma-separated `--advertiser-id` values and `--managed-accounts` for enabled locally responsible Qianchuan accounts. Preserve partial successes and failed accounts separately.

For `qc-reports custom`, obtain a valid topic, dimensions, and metrics from the user or `qc-reports schema`. Preserve filters as `{field, operator: 7, values}` and retain ID values as strings. Pass `data_period` only when the chosen topic supports it. It supports single-account queries, explicit multi-account queries through repeated/comma-separated `--advertiser-id`, and responsible-Qianchuan-account queries through `--managed-accounts`.

Multi-account custom aggregation requires the same `data_topic`, dimensions, metrics, filters, dates, sort, and page options for every account. Aggregate rows by the exact requested dimension tuple. Sum only additive count, amount, GMV, order, click, show, and spend metrics. Do not add or average ratio, ROI, CTR, CVR, CPC, CPM, ECPM, rate, cost-per, average, or other unit metrics unless a command explicitly implements a weighted formula. One account failure must not discard successful account rows; report failed accounts separately and mark the overall result not ok.

For 全域投后素材维度 requests, use `qc-reports custom` with the official topic rather than `qc-reports materials`. Treat phrases such as `素材维度数据`, `视频素材数据`, `全域素材`, `投后素材`, `素材创建时间`, and `按素材统计` as this report family unless the user explicitly names the legacy material endpoint or legacy filters. Query `qc-reports schema` when unsure.

Default topic selection:

| User intent | `data_topic` | Default dimensions |
| --- | --- | --- |
| 视频素材 / video material | `SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO` | `material_id`, `roi2_material_video_name`, `roi2_material_upload_time`, `material_create_time_v2` |
| 图片素材 / image material | `SITE_PROMOTION_PRODUCT_POST_DATA_IMAGE` | `material_id`, image material name/info fields from schema, `material_create_time_v2` when available |
| 标题素材 / title material | `SITE_PROMOTION_PRODUCT_POST_DATA_TITLE` | `roi2_title_material_v3`, `stat_time_day` only when daily breakdown is requested |
| 其他创意 / other creative | `SITE_PROMOTION_PRODUCT_POST_DATA_OTHER` | `roi2_other_creative_name`, `stat_time_day` only when daily breakdown is requested |
| 综合素材类型 / overall material | `OVERALL_ROI_PRODUCT_MATERIAL` | `material_id`, `roi2_material_type_v3`, relevant material name/info fields, `material_create_time_v2` |

For `SITE_PROMOTION_PRODUCT_POST_DATA_VIDEO`, the common metric set is `stat_cost_for_roi2`, `product_show_count_for_roi2`, `product_click_count_for_roi2`, `product_cvr_rate_for_roi2`, `product_convert_rate_for_roi2`, `total_pay_order_count_for_roi2`, `total_pay_order_gmv_include_coupon_for_roi2`, `total_prepay_and_pay_order_roi2`, `total_cost_per_pay_order_for_roi2`, `total_pay_order_gmv_for_roi2`, `product_ecpm_for_roi2`, and `product_cpc_for_roi2`. If the user asks to group by material creation time, group on `material_create_time_v2`; rows whose official value is `-` or empty must remain separate unless the user explicitly allows fallback to `roi2_material_upload_time`.

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
