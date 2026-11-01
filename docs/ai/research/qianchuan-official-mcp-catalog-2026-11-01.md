# 千川官方 MCP 查询目录与接入证据（2026-11-01）

文档属性：AI 调研与公开目录快照。核对日期：2026-11-01。状态：公开条目已核对，余额/配额/两页商品全域计划的单账户最小 PoC 通过；全目录合同和可用性未验收。

权威边界：下表来自公开文档中的工具名、接口路径和关联文档链接，不代表本地插件已经实现这些工具，也不代表某个授权能调用全部条目。分组是本方案的设计分类；未逐项确认的 HTTP 方法、参数、分页和适用场景不得凭名称推断。

关联：[完整设计](../designs/qianchuan-official-mcp-skill-integration-2026-11-01.md)；[实施计划](../plans/qianchuan-official-mcp-integration-2026-11-01.md)。

## 原始来源

- [S1 千川 MCP 工具列表](https://open.oceanengine.com/labels/12/docs/1847297003631945)：公开页面实际列出 156 行工具，各工具名和路径唯一。
- [S2 千川业务工具](https://open.oceanengine.com/labels/12/docs/1839622960207943)：Streamable HTTP、开发者 Access-Token 请求头、JSON 数组字符串格式的 Tool-Range（例如 `[\"advertiser_info_v2\",\"advertiser_public_info_v2\"]`），以及官方应用授权“已暂停新增”；实际筛选行为与容量仍待联调。
- [S3 巨量引擎开发工具](https://open.oceanengine.com/labels/12/docs/1839622944349191)：SSE 及文档、Schema、SDK 示例三个开发工具。
- [S4 使用指南与实践案例](https://open.oceanengine.com/labels/12/docs/1849835441833027)：查询、分析与报表案例；其中旧授权示例不覆盖 S2 的当前新增授权限制。
- [本地能力合同](../../../runtime/ocean-watch-go/internal/contracts/capabilities.go)：本次源码核对为 91 个能力、26 个 MCP 工具、77 个 CLI 命令，重叠入口不能相加成能力总数。

## 已确认与未确认

已确认：公开目录 156 项、S2/S3 的连接方式、当前核心 SDK 使用的 12 个接口也出现在目录中；本轮明确授权范围内，三项业务 MCP 的发现/实际 Schema 与同条件 SDK 最小对照通过，详见[脱敏 PoC](qianchuan-official-mcp-poc-2026-11-01.md)。

未确认：完整远端下发集、其他工具的合同与授权、全部场景/单位、任意分页大小及频控容量。三项 Tool-Range 实际额外返回一个工具，不能作为权限白名单；预算、40100 反馈与默认两页的稳定约束维护在[正式内部合同](../../reference/qianchuan-official-mcp.md)。

所有目录条目的初始登记状态为 `published_only`。每项启用前完成 Schema、业务 GET、能力/工具集/最低模式、主体/场景、分页、输出和隔离验证；真实可用性单独记录，不能用目录登记代替联调。

关联方案修订为 1.1：P0.5 最小连接已在本轮授权范围通过，后续首批独立完成 P4-A/P5-A，其他目录按 P3 批次扩展；D1 开发资料独立且可选。全目录 156 项目标及下表原始登记保持不变；最小 PoC 不把候选条目自动标为已封装或可执行。

本地实施补充（2026-11-01）：[机器可读候选目录](../../../runtime/ocean-watch-go/internal/contracts/catalog/qianchuan-official-2026-11-01.json)保留下表 156 项原名称、路径和文档；[首批草案](../../../runtime/ocean-watch-go/internal/contracts/catalog/qianchuan-first-release-2026-11-01.json)登记 9 个目标工具、15 个查询依赖，全部未启用。锁定 `ad_open_sdk_go@v1.1.92` 的 `api_qianchuan_account_balance_get_v10.go`、`api_qianchuan_account_budget_get_v10.go`、`api_qianchuan_ad_quota_get_v10.go` 与 `api_qianchuan_uni_promotion_list_v10.go` 已核对使用 `http.MethodGet`；此事实不证明官方 MCP 的实际参数形状或角色/单位。

锁定 `modelcontextprotocol/go-sdk@v1.7.0` 的 `CallToolResult.StructuredContent` 通用解码使用浮点数。内部原型在 HTTP JSON/SSE 响应进入 SDK 前保留原始 JSON，再以 `json.Number` 读取业务数字；不包裹 SDK Connection，以保留内部会话更新机制。两页真实 structured_json 的长商品 ID 与 SDK 精确一致，余额/配额和两页关键事实对照通过；正式投影、会话生命周期和实际 Host 仍未验收。

## 文档差异与保留接口

- `qianchuan_uni_promotion_list_v1` 的路径是全域计划列表，但目录描述成“广告变更消息通知”，关联链接也指向该通知。本表保留原链接作为来源证据；本轮已通过官方搜索核对[正确计划列表文档](https://open.oceanengine.com/labels/12/docs/1771195810853899)、锁定 SDK 与真实输入 Schema，不实现 SPI。可选 `page_size` 的整数/字符串枚举冲突保留，原型仅省略该字段并验证默认 10 条。
- 本地 SDK 已调用 `/open_api/v1.0/qianchuan/report/all_promotion/get/`，本目录没有列出对应 `qianchuan_report_all_promotion_get_v1`。这是“本份 MCP 清单未列出”，不是认定官方 API 不存在。现有整体账户报表继续走 SDK，不能用纯全域报表代替包含乘方的口径。
- “核心 SDK 对照”仅标记本次核对的作品预检和报表核心接口。其他条目的“—”表示没有在本次做完整源码逐项映射，不表示仓库一定没有相关账户/授权实现。
- `learing`、`pivativewords` 和重复 `qianchuan/qianchuan` 路径按官方页面原样保留；不能自行纠正为另一个远端工具名或路径。

## 设计分组

| 分组 ID | 业务范围 | 公开工具数 |
| --- | --- | --- |
| `accounts` | 账户与授权关系 | 17 |
| `finance` | 资金与预算查询 | 17 |
| `plans` | 计划、审核与投放建议 | 23 |
| `assets` | 素材与质量标签 | 14 |
| `products` | 商品与可投关联 | 5 |
| `reports` | 报表与指标配置 | 20 |
| `live` | 直播间与实时表现 | 5 |
| `aweme_orders` | 随心推订单与建议 | 17 |
| `targeting` | 达人定向与受众词库 | 21 |
| `compliance` | 评论、屏蔽与违规积分 | 9 |
| `operations` | 操作日志、灰度与调控查询 | 8 |
| 合计 | 完整公开目录 | 156 |

## 全量登记表

每行的具体参数、HTTP 业务方法、适用全域/乘方/随心推场景、授权角色及输出字段均须在实施阶段验证。下面列出的 query_id 是拟采用的目录 ID，不是当前本地可调用的 MCP 工具。

| 序号 | 设计分组 | 官方工具 / 拟 query_id | 接口路径 | 核心 SDK 对照 | 官方关联文档 |
| --- | --- | --- | --- | --- | --- |
| 1 | `finance` | `advertiser_fund_transaction_get_v2` | `/open_api/2/advertiser/fund/transaction/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1780627317170250) |
| 2 | `accounts` | `advertiser_info_v2` | `/open_api/2/advertiser/info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697468139129870) |
| 3 | `accounts` | `advertiser_public_info_v2` | `/open_api/2/advertiser/public_info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697468127552590) |
| 4 | `finance` | `agent_adv_recharge_recharge_record_v2` | `/open_api/2/agent/adv/recharge/recharge_record/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1836524891344154) |
| 5 | `accounts` | `agent_advertiser_select_v2` | `/open_api/2/agent/advertiser/select/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467832592392) |
| 6 | `accounts` | `agent_advertiser_info_query_v2` | `/open_api/2/agent/advertiser_info/query/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1809915753481348) |
| 7 | `finance` | `agent_charge_verify_v2` | `/open_api/2/agent/charge/verify/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832711149796937) |
| 8 | `accounts` | `agent_info_v2` | `/open_api/2/agent/info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467936570371) |
| 9 | `accounts` | `customer_center_advertiser_list_v2` | `/open_api/2/customer_center/advertiser/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1796368918556803) |
| 10 | `accounts` | `ebp_advertiser_list_v2` | `/open_api/2/ebp/advertiser/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1829551614262412) |
| 11 | `assets` | `file_video_agent_get_v2` | `/open_api/2/file/video/agent/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1827446107102592) |
| 12 | `targeting` | `tools_admin_info_v2` | `/open_api/2/tools/admin/info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1816770040511488) |
| 13 | `targeting` | `tools_aweme_author_info_get_v2` | `/open_api/2/tools/aweme_author_info/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754604074361856) |
| 14 | `targeting` | `tools_aweme_category_top_author_get_v2` | `/open_api/2/tools/aweme_category_top_author/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466881912835) |
| 15 | `targeting` | `tools_aweme_info_search_v2` | `/open_api/2/tools/aweme_info_search/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754603678777351) |
| 16 | `targeting` | `tools_aweme_multi_level_category_get_v2` | `/open_api/2/tools/aweme_multi_level_category/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466893806595) |
| 17 | `targeting` | `tools_aweme_similar_author_search_v2` | `/open_api/2/tools/aweme_similar_author_search/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754603569552391) |
| 18 | `targeting` | `tools_country_info_v2` | `/open_api/2/tools/country/info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1816770002957402) |
| 19 | `targeting` | `tools_creative_word_select_v2` | `/open_api/2/tools/creative_word/select/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466866336772) |
| 20 | `targeting` | `tools_industry_get_v2` | `/open_api/2/tools/industry/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697640099656704) |
| 21 | `targeting` | `tools_interest_action_action_category_v2` | `/open_api/2/tools/interest_action/action/category/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466903390219) |
| 22 | `targeting` | `tools_interest_action_action_keyword_v2` | `/open_api/2/tools/interest_action/action/keyword/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466916182083) |
| 23 | `targeting` | `tools_interest_action_id2word_v2` | `/open_api/2/tools/interest_action/id2word/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754604787816462) |
| 24 | `targeting` | `tools_interest_action_interest_category_v2` | `/open_api/2/tools/interest_action/interest/category/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466937486339) |
| 25 | `targeting` | `tools_interest_action_interest_keyword_v2` | `/open_api/2/tools/interest_action/interest/keyword/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466948442116) |
| 26 | `targeting` | `tools_interest_action_keyword_suggest_v2` | `/open_api/2/tools/interest_action/keyword/suggest/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754604887592974) |
| 27 | `accounts` | `user_info_v2` | `/open_api/2/user/info/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1806175583735835) |
| 28 | `accounts` | `oauth2_advertiser_get` | `/open_api/oauth2/advertiser/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467748096067) |
| 29 | `finance` | `qianchuan_account_balance_get_v1` | `/open_api/v1.0/qianchuan/account/balance/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783322092364800) |
| 30 | `finance` | `qianchuan_account_budget_get_v1` | `/open_api/v1.0/qianchuan/account/budget/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1777635660375047) |
| 31 | `plans` | `qianchuan_ad_compensate_status_get_v1` | `/open_api/v1.0/qianchuan/ad/compensate_status/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1778157489597440) |
| 32 | `plans` | `qianchuan_ad_detail_get_v1` | `/open_api/v1.0/qianchuan/ad/detail/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467544882191) |
| 33 | `plans` | `qianchuan_ad_get_v1` | `/open_api/v1.0/qianchuan/ad/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467558690816) |
| 34 | `plans` | `qianchuan_ad_keywords_get_v1` | `/open_api/v1.0/qianchuan/ad/keywords/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1727447830172686) |
| 35 | `plans` | `qianchuan_ad_learing_status_get_v1` | `/open_api/v1.0/qianchuan/ad/learing_status/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1778158222500876) |
| 36 | `plans` | `qianchuan_ad_material_get_v1` | `/open_api/v1.0/qianchuan/ad/material/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1803705421766656) |
| 37 | `plans` | `qianchuan_ad_material_suggestion_v1` | `/open_api/v1.0/qianchuan/ad/material/suggestion/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1816413016616091) |
| 38 | `plans` | `qianchuan_ad_pivativewords_get_v1` | `/open_api/v1.0/qianchuan/ad/pivativewords/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1727448194054157) |
| 39 | `plans` | `qianchuan_ad_quota_get_v1` | `/open_api/v1.0/qianchuan/ad/quota/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1731182713398283) |
| 40 | `plans` | `qianchuan_ad_recommend_keywords_get_v1` | `/open_api/v1.0/qianchuan/ad/recommend_keywords/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1730711298227214) |
| 41 | `plans` | `qianchuan_ad_reject_reason_v1` | `/open_api/v1.0/qianchuan/ad/reject_reason/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467603007503) |
| 42 | `accounts` | `qianchuan_advertiser_type_get_v1` | `/open_api/v1.0/qianchuan/advertiser/type/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754620816918532) |
| 43 | `targeting` | `qianchuan_audience_group_get_v1` | `/open_api/v1.0/qianchuan/audience_group/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1744039355184140) |
| 44 | `targeting` | `qianchuan_audience_list_get_v1` | `/open_api/v1.0/qianchuan/audience_list/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1741766314771463) |
| 45 | `accounts` | `qianchuan_aweme_authorized_get_v1` | `/open_api/v1.0/qianchuan/aweme/authorized/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467726080011) |
| 46 | `aweme_orders` | `qianchuan_aweme_estimate_profit_v1` | `/open_api/v1.0/qianchuan/aweme/estimate_profit/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1757415052106760) |
| 47 | `targeting` | `qianchuan_aweme_interest_action_interest_keyword_v1` | `/open_api/v1.0/qianchuan/aweme/interest_action/interest/keyword/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754616248472640) |
| 48 | `aweme_orders` | `qianchuan_aweme_order_detail_get_v1` | `/open_api/v1.0/qianchuan/aweme/order/detail/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754615684361224) |
| 49 | `aweme_orders` | `qianchuan_aweme_order_get_v1` | `/open_api/v1.0/qianchuan/aweme/order/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754615684532301) |
| 50 | `aweme_orders` | `qianchuan_aweme_order_quota_get_v1` | `/open_api/v1.0/qianchuan/aweme/order/quota/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1758439025795076) |
| 51 | `aweme_orders` | `qianchuan_aweme_order_suggest_delivery_time_get_v1` | `/open_api/v1.0/qianchuan/aweme/order/suggest/delivery_time/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1779621018526732) |
| 52 | `products` | `qianchuan_aweme_product_available_get_v1` | `/open_api/v1.0/qianchuan/aweme/product/available/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754623806862336) |
| 53 | `aweme_orders` | `qianchuan_aweme_report_order_get_v1` | `/open_api/v1.0/qianchuan/aweme/report/order/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754616243989519) |
| 54 | `aweme_orders` | `qianchuan_aweme_suggest_roi_goal_v1` | `/open_api/v1.0/qianchuan/aweme/suggest/roi/goal/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1757415442277379) |
| 55 | `aweme_orders` | `qianchuan_aweme_suggest_bid_v1` | `/open_api/v1.0/qianchuan/aweme/suggest_bid/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1757415284062219) |
| 56 | `aweme_orders` | `qianchuan_aweme_uni_promotion_ad_material_get_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/ad/material/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1844129152007108) |
| 57 | `aweme_orders` | `qianchuan_aweme_uni_promotion_estimate_effect_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/estimate/effect/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834804152700935) |
| 58 | `aweme_orders` | `qianchuan_aweme_uni_promotion_order_detail_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/order/detail/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834798638887948) |
| 59 | `aweme_orders` | `qianchuan_aweme_uni_promotion_order_get_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/order/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834797699331148) |
| 60 | `aweme_orders` | `qianchuan_aweme_uni_promotion_order_report_get_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/order/report/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1844129628513816) |
| 61 | `aweme_orders` | `qianchuan_aweme_uni_promotion_order_suggest_delivery_time_get_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/order/suggest/delivery_time/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1836628495717452) |
| 62 | `aweme_orders` | `qianchuan_aweme_uni_promotion_report_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/report/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834802422645760) |
| 63 | `aweme_orders` | `qianchuan_aweme_uni_promotion_suggest_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/suggest/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834803969912960) |
| 64 | `aweme_orders` | `qianchuan_aweme_uni_promotion_suggest_roi_v1` | `/open_api/v1.0/qianchuan/aweme/uni_promotion/suggest/roi/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834804305062915) |
| 65 | `assets` | `qianchuan_aweme_video_get_v1` | `/open_api/v1.0/qianchuan/aweme/video/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754616254611535) |
| 66 | `accounts` | `qianchuan_aweme_auth_list_get_v1` | `/open_api/v1.0/qianchuan/aweme_auth_list/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1804287262648346) |
| 67 | `accounts` | `qianchuan_brand_authorized_get_v1` | `/open_api/v1.0/qianchuan/brand/authorized/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1775646836699149) |
| 68 | `plans` | `qianchuan_campaign_list_get_v1` | `/open_api/v1.0/qianchuan/campaign_list/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1702055567088643) |
| 69 | `assets` | `qianchuan_carousel_aweme_get_v1` | `/open_api/v1.0/qianchuan/carousel/aweme/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1778159412906125) |
| 70 | `assets` | `qianchuan_carousel_get_v1` | `/open_api/v1.0/qianchuan/carousel/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1778159308092424) |
| 71 | `targeting` | `qianchuan_dmp_audiences_get_v1` | `/open_api/v1.0/qianchuan/dmp/audiences/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1709032151638039) |
| 72 | `plans` | `qianchuan_estimate_effect_v1` | `/open_api/v1.0/qianchuan/estimate/effect/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1770010949341327) |
| 73 | `assets` | `qianchuan_file_video_aweme_get_v1` | `/open_api/v1.0/qianchuan/file/video/aweme/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1697466774382599) |
| 74 | `assets` | `qianchuan_file_video_efficiency_get_v1` | `/open_api/v1.0/qianchuan/file/video/efficiency/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754607188087808) |
| 75 | `assets` | `qianchuan_file_video_original_get_v1` | `/open_api/v1.0/qianchuan/file/video/original/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754607275560015) |
| 76 | `finance` | `qianchuan_finance_detail_get_v1` | `/open_api/v1.0/qianchuan/finance/detail/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1725977659075599) |
| 77 | `finance` | `qianchuan_finance_wallet_get_v1` | `/open_api/v1.0/qianchuan/finance/wallet/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1725977604633615) |
| 78 | `assets` | `qianchuan_image_get_v1` | `/open_api/v1.0/qianchuan/image/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1739304248623182) |
| 79 | `targeting` | `qianchuan_keyword_package_get_v1` | `/open_api/v1.0/qianchuan/keyword_package/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1777718838286403) |
| 80 | `plans` | `qianchuan_lq_ad_get_v1` | `/open_api/v1.0/qianchuan/lq_ad/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1725457239476224) |
| 81 | `assets` | `qianchuan_material_ad_get_v1` | `/open_api/v1.0/qianchuan/material/ad/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1816411517288523) |
| 82 | `assets` | `qianchuan_material_get_v1` | `/open_api/v1.0/qianchuan/material/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1810701753348235) |
| 83 | `targeting` | `qianchuan_orientation_package_get_v1` | `/open_api/v1.0/qianchuan/orientation_package/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1741765278816256) |
| 84 | `products` | `qianchuan_product_available_get_v1` | `/open_api/v1.0/qianchuan/product/available/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467688861696) |
| 85 | `reports` | `qianchuan_qianchuan_report_today_live_room_config_get_v1` | `/open_api/v1.0/qianchuan/qianchuan/report/today_live/room/config/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1807424096554059) |
| 86 | `reports` | `qianchuan_report_ad_get_v1` | `/open_api/v1.0/qianchuan/report/ad/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466415173644) |
| 87 | `reports` | `qianchuan_report_ad_material_get_v1` | `/open_api/v1.0/qianchuan/report/ad/material/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1803706947586073) |
| 88 | `reports` | `qianchuan_report_advertiser_get_v1` | `/open_api/v1.0/qianchuan/report/advertiser/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466393573376) |
| 89 | `reports` | `qianchuan_report_creative_get_v1` | `/open_api/v1.0/qianchuan/report/creative/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697466431262732) |
| 90 | `reports` | `qianchuan_report_custom_config_get_v1` | `/open_api/v1.0/qianchuan/report/custom/config/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1824289917859843) |
| 91 | `reports` | `qianchuan_report_custom_get_v1` | `/open_api/v1.0/qianchuan/report/custom/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1824289996057844) |
| 92 | `reports` | `qianchuan_report_live_get_v1` | `/open_api/v1.0/qianchuan/report/live/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763406675800128) |
| 93 | `reports` | `qianchuan_report_long_transfer_order_config_get_v1` | `/open_api/v1.0/qianchuan/report/long_transfer/order/config/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1807348855527500) |
| 94 | `reports` | `qianchuan_report_long_transfer_order_data_get_v1` | `/open_api/v1.0/qianchuan/report/long_transfer/order/data/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1807348907403268) |
| 95 | `reports` | `qianchuan_report_long_transfer_order_get_v1` | `/open_api/v1.0/qianchuan/report/long_transfer/order/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1754619225006091) |
| 96 | `reports` | `qianchuan_report_material_get_v1` | `/open_api/v1.0/qianchuan/report/material/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1745207002572807) |
| 97 | `reports` | `qianchuan_report_search_word_get_v1` | `/open_api/v1.0/qianchuan/report/search_word/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1762139117875212) |
| 98 | `reports` | `qianchuan_report_today_live_room_data_get_v1` | `/open_api/v1.0/qianchuan/report/today_live/room/data/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1807433722476556) |
| 99 | `reports` | `qianchuan_report_uni_promotion_config_get_v1` | `/open_api/v1.0/qianchuan/report/uni_promotion/config/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1823296280645708) |
| 100 | `reports` | `qianchuan_report_uni_promotion_data_get_v1` | `/open_api/v1.0/qianchuan/report/uni_promotion/data/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1823297941140569) |
| 101 | `reports` | `qianchuan_report_uni_promotion_dimension_data_author_get_v1` | `/open_api/v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1786236031165513) |
| 102 | `reports` | `qianchuan_report_uni_promotion_dimension_data_room_get_v1` | `/open_api/v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1786235355858947) |
| 103 | `reports` | `qianchuan_report_uni_promotion_get_v1` | `/open_api/v1.0/qianchuan/report/uni_promotion/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1770675169146947) |
| 104 | `reports` | `qianchuan_report_video_user_lose_get_v1` | `/open_api/v1.0/qianchuan/report/video_user_lose/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1745212588656647) |
| 105 | `accounts` | `qianchuan_shop_advertiser_list_v1` | `/open_api/v1.0/qianchuan/shop/advertiser/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1697467801357320) |
| 106 | `accounts` | `qianchuan_shop_authorized_get_v1` | `/open_api/v1.0/qianchuan/shop/authorized/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1775801786494976) |
| 107 | `plans` | `qianchuan_suggest_budget_v1` | `/open_api/v1.0/qianchuan/suggest/budget/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1770008006444036) |
| 108 | `plans` | `qianchuan_suggest_roi_goal_v1` | `/open_api/v1.0/qianchuan/suggest/roi/goal/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1730170307692548) |
| 109 | `plans` | `qianchuan_suggest_bid_v1` | `/open_api/v1.0/qianchuan/suggest_bid/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1761144594567183) |
| 110 | `live` | `qianchuan_today_live_room_detail_get_v1` | `/open_api/v1.0/qianchuan/today_live/room/detail/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763409499649031) |
| 111 | `live` | `qianchuan_today_live_room_flow_performance_get_v1` | `/open_api/v1.0/qianchuan/today_live/room/flow_performance/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763409732890699) |
| 112 | `live` | `qianchuan_today_live_room_get_v1` | `/open_api/v1.0/qianchuan/today_live/room/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763408973310988) |
| 113 | `live` | `qianchuan_today_live_room_product_list_get_v1` | `/open_api/v1.0/qianchuan/today_live/room/product_list/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763410225446924) |
| 114 | `live` | `qianchuan_today_live_room_user_get_v1` | `/open_api/v1.0/qianchuan/today_live/room/user/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763409882431488) |
| 115 | `operations` | `qianchuan_tools_allow_coupon_v1` | `/open_api/v1.0/qianchuan/tools/allow_coupon/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1771275948928071) |
| 116 | `operations` | `qianchuan_tools_estimate_audience_v1` | `/open_api/v1.0/qianchuan/tools/estimate_audience/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1757702925729792) |
| 117 | `operations` | `qianchuan_tools_gray_v1` | `/open_api/v1.0/qianchuan/tools/gray/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1763675121890315) |
| 118 | `operations` | `qianchuan_tools_log_search_v1` | `/open_api/v1.0/qianchuan/tools/log_search/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832813828161028) |
| 119 | `operations` | `qianchuan_tools_smart_boost_ad_boost_report_get_v1` | `/open_api/v1.0/qianchuan/tools/smart_boost/ad_boost/report/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1759313983537163) |
| 120 | `operations` | `qianchuan_tools_smart_boost_ad_boost_status_get_v1` | `/open_api/v1.0/qianchuan/tools/smart_boost/ad_boost/status/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1759313437747204) |
| 121 | `operations` | `qianchuan_tools_smart_boost_ad_boost_version_get_v1` | `/open_api/v1.0/qianchuan/tools/smart_boost/ad_boost/version/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1759313851340800) |
| 122 | `accounts` | `qianchuan_uni_aweme_authorized_get_v1` | `/open_api/v1.0/qianchuan/uni_aweme/authorized/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1771196800070656) |
| 123 | `plans` | `qianchuan_uni_aweme_suggest_budget_v1` | `/open_api/v1.0/qianchuan/uni_aweme/suggest/budget/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1828257556490251) |
| 124 | `plans` | `qianchuan_uni_promotion_ad_control_task_list_v1` | `/open_api/v1.0/qianchuan/uni_promotion/ad/control_task/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1824940765838411) |
| 125 | `plans` | `qianchuan_uni_promotion_ad_detail_v1` | `/open_api/v1.0/qianchuan/uni_promotion/ad/detail/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1804362305657868) |
| 126 | `plans` | `qianchuan_uni_promotion_ad_material_get_v1` | `/open_api/v1.0/qianchuan/uni_promotion/ad/material/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1804363488115850) |
| 127 | `products` | `qianchuan_uni_promotion_ad_product_get_v1` | `/open_api/v1.0/qianchuan/uni_promotion/ad/product/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832185836349447) |
| 128 | `plans` | `qianchuan_uni_promotion_ad_suggestion_v1` | `/open_api/v1.0/qianchuan/uni_promotion/ad/suggestion/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832628101966183) |
| 129 | `accounts` | `qianchuan_uni_promotion_authorizable_shop_list_v1` | `/open_api/v1.0/qianchuan/uni_promotion/authorizable_shop/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1844495918167047) |
| 130 | `assets` | `qianchuan_uni_promotion_block_material_get_v1` | `/open_api/v1.0/qianchuan/uni_promotion/block_material/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1825215820766292) |
| 131 | `plans` | `qianchuan_uni_promotion_list_v1` | `/open_api/v1.0/qianchuan/uni_promotion/list/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1848859765931272) |
| 132 | `products` | `qianchuan_uni_promotion_product_aweme_get_v1` | `/open_api/v1.0/qianchuan/uni_promotion/product/aweme/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1825216033296576) |
| 133 | `products` | `qianchuan_uni_promotion_product_get_v1` | `/open_api/v1.0/qianchuan/uni_promotion/product/get/` | 已核对 | [文档](https://open.oceanengine.com/labels/12/docs/1825216221095947) |
| 134 | `assets` | `qianchuan_video_by_aweme_get_v1` | `/open_api/v1.0/qianchuan/video/by_aweme/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1814938748938240) |
| 135 | `assets` | `qianchuan_video_get_v1` | `/open_api/v1.0/qianchuan/video/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1739309912219663) |
| 136 | `finance` | `account_fund_get_v3` | `/open_api/v3.0/account/fund/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1812964516306124) |
| 137 | `finance` | `cg_transfer_can_transfer_balance_get_v3` | `/open_api/v3.0/cg_transfer/can_transfer_balance/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1817406123096500) |
| 138 | `finance` | `cg_transfer_can_transfer_target_list_v3` | `/open_api/v3.0/cg_transfer/can_transfer_target/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1817407512558874) |
| 139 | `finance` | `cg_transfer_query_can_transfer_balance_v3` | `/open_api/v3.0/cg_transfer/query_can_transfer_balance/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1798395239487699) |
| 140 | `finance` | `cg_transfer_query_transfer_balance_v3` | `/open_api/v3.0/cg_transfer/query_transfer_balance/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1798395209349136) |
| 141 | `finance` | `cg_transfer_query_transfer_detail_v3` | `/open_api/v3.0/cg_transfer/query_transfer_detail/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1798395284969472) |
| 142 | `finance` | `cg_transfer_transfer_balance_get_v3` | `/open_api/v3.0/cg_transfer/transfer_balance/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1817406311960675) |
| 143 | `finance` | `cg_transfer_transfer_detail_get_v3` | `/open_api/v3.0/cg_transfer/transfer_detail/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1817406243103171) |
| 144 | `finance` | `charge_result_v3` | `/open_api/v3.0/charge/result/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832807350934532) |
| 145 | `assets` | `open_material_audit_pro_get_v3` | `/open_api/v3.0/open_material_audit/pro/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1832830677796042) |
| 146 | `finance` | `remittance_code_list_v3` | `/open_api/v3.0/remittance_code/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1834443764106314) |
| 147 | `compliance` | `security_score_disposal_info_get_v3` | `/open_api/v3.0/security/score_disposal_info/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1809255307204884) |
| 148 | `compliance` | `security_score_total_get_v3` | `/open_api/v3.0/security/score_total/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1809254968749066) |
| 149 | `compliance` | `security_score_violation_event_get_v3` | `/open_api/v3.0/security/score_violation_event/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1809254532005028) |
| 150 | `operations` | `subscribe_accounts_list_v3` | `/open_api/v3.0/subscribe/accounts/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1818567554191363) |
| 151 | `compliance` | `tools_aweme_banned_list_v3` | `/open_api/v3.0/tools/aweme_banned/list/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783173626394698) |
| 152 | `compliance` | `tools_comment_get_v3` | `/open_api/v3.0/tools/comment/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783172795243529) |
| 153 | `compliance` | `tools_comment_mid2item_id_v3` | `/open_api/v3.0/tools/comment/mid2item_id/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783173172223044) |
| 154 | `compliance` | `tools_comment_terms_banned_get_v3` | `/open_api/v3.0/tools/comment/terms_banned/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783173348083715) |
| 155 | `compliance` | `tools_comment_metrics_get_v3` | `/open_api/v3.0/tools/comment_metrics/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783172953221129) |
| 156 | `compliance` | `tools_comment_reply_get_v3` | `/open_api/v3.0/tools/comment_reply/get/` | — | [文档](https://open.oceanengine.com/labels/12/docs/1783172901160971) |

## 更新规则

目录更新以公开工具列表和经过授权的远端工具 Schema 为证据。新增、移除或变更条目进入代码评审后更新编译目录与 Skill 路由，不能让运行时自动扩大可执行工具集。已经发布的 query_id 保留兼容别名或明确迁移说明。

以后引用本快照，先核对当前公开页面、实际工具版本与仓库代码。原始业务返回、Token、Cookie 和生产账户数据不得附入本文件。
