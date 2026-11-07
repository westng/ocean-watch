# 千川官方 MCP 查询合同

本文维护当前源码的查询、配置和结果边界；线上已发布版本可能尚未包含这些未发布改动。业务能力以当前 Runtime 返回的目录与合同为准，实际宿主验收见 [实施验收记录](../ai/reviews/qianchuan-official-mcp-implementation-2026-11-01.md)。

## 查询范围

官方公开目录在 2026-11-01 核对为 156 项，分为 11 组。原始目录快照、155 个实际输入 Schema 和公开 GET 文档分别保存；原始快照不因本地纠错而改写。正式执行合同由这些来源和锁定的官方 SDK 编译，保留来源摘要与合同指纹。远端没有提供输出 Schema 的项目，输出依据已保存的官方响应字段构建白名单，不能声称远端验证了本地输出 Schema。

当前 156 项均有受控查询映射：153 项为 `contract_verified`，3 项为 `contract_limited`。这表示本地合同覆盖，不表示当前授权拥有全部角色、每个查询均已真实调用或已读完全部数据。每项的 `enabled` 还受实例策略控制，`last_known_availability` 与本地合同状态分开报告。

| 组 | 项数 |
| --- | ---: |
| accounts | 17 |
| assets | 14 |
| aweme_orders | 17 |
| compliance | 9 |
| finance | 17 |
| live | 5 |
| operations | 8 |
| plans | 23 |
| products | 5 |
| reports | 20 |
| targeting | 21 |

`qianchuan_ad_pivativewords_get_v1` 的原名称保持不变；实际工具发现缺少该名称，执行仅映射到已注册的计划详情 GET，核对精确计划身份后只投影 `pivative_words`。目录返回 `execution_query_id` 和 `mapping_evidence`，不借用其他接口的主体或输出权限。

## 对话中的任务入口

| MCP 工具 | 用途与边界 |
| --- | --- |
| `get_qianchuan_official_capabilities` | 先按关键词或组检索，再按精确 `query_id` 获取参数、主体、条件、单位、分页和限制；只读本地合同。 |
| `query_qianchuan_official` | 执行一个已批准的 GET，接受精确查询、主体、业务参数和返回游标。 |
| `get_qianchuan_account_health` | 检查一个广告主或本地启用的负责账户的余额、日预算、配额；保留失败项。 |
| `diagnose_qianchuan_plan` | 核对精确计划及 `uni/overall/legacy` 场景，查询适用的审核、学习期、赔付事实；不适用项单独列出。 |
| `inspect_qianchuan_material_quality` | 检查精确素材的原创和低效标签；作品输入先进行本批 F2、官方达人、归属、目标商品和素材身份核验。 |
| `get_qianchuan_delivery_suggestions` | 按场景查询 `budget/roi/bid/audit` 建议；缺少参数时给出字段要求，不调整计划。 |
| `get_qianchuan_report_schema` | 通过既有 SDK 查询当前账户的报表主题、维度、指标。 |
| `report_qianchuan_custom` | 按已确认的主题、维度、指标和有界日期/分页查询，复用 SDK 报表服务。 |
| `report_qianchuan_managed_accounts` | 复用 SDK 负责账户报表，只遍历本地启用的千川负责账户。 |

默认 MCP 合同包含 40 个工具（含五个独立千川操作入口）；启用可选 `developer_docs` 后为 43 个。新增工具或 Skill 合同要求宿主重新加载，在 Codex 新聊天中发现。已有计划列表、报表及作品预检继续使用其既有 SDK 用例。CLI 的 `qc-official catalog` 和 `qc-official query --input-json -` 映射同一正式查询服务，不是模型可指定任意 URL 的网关。

稳定代理按固定工具合同隔离 Runtime 选择和拒绝记录；旧聊天的合同不匹配不能把新版 Runtime 全局隔离。版本槽位、安装锁和活跃租约仍共享，其他合同的当前版本在清理时保留。启动配置的公开修订标识用于宿主重新加载代理，不改变业务模式或工具集；仅发现工具不代表其实际调用已通过验收。

`auth_account_id` 是当前授权映射中的数字账户选择器。精确广告主只有一个已核实映射时可省略；映射有歧义时先选择准确的授权账户。授权记录引用与该选择器是不同身份，只有目录明确允许 `scope.kind=authorization` 的查询才能把授权引用作为 `scope.id`。

已命中任务入口时直接调用对应工具；长尾需求才检索目录。已工具化入口在宿主缺失时明确报告，Skill 不静默改走等价 CLI。详见 [Skill 查询路由](../../skills/qc-plan-monitor/references/official-queries.md)。

## 连接、策略与主体

官方业务端点固定为 `https://open.oceanengine.com/qianchuan/mcp`，使用 Streamable HTTP。MCP 协议的 POST 传送 GET 查询；本批业务工具没有官方写入路径。`Access-Token` 由既有 TokenManager/TokenLease 在 Adapter 内注入，订阅类查询需要的应用 Token 也只由内部授权服务取得。Token、Secret、请求 headers、URL、工具名、HTTP 方法、后端选择和执行模式均不属于模型输入。

`Tool-Range` 只表示远端发现范围。本地执行还要核对精确白名单、兼容 Schema、固定 GET、实例工具集与模式、授权及主体关系；Schema 漂移不能自动扩大权限。会话池按授权修订和工具发现配置隔离，Token 修订变化后退役旧会话；等待、取消、闲置回收和失败都释放本实例资源。

实例从用户管理的 `business_policy` 固定 `mode`、`toolsets` 和可选主体白名单，CLI/MCP 与 Token 准备共用 Core 检查。默认 `plan` 保留原有查询和预检，启用 `core/marketing/qianchuan/reports`；`developer_docs` 默认关闭。未知配置拒绝启动，普通请求不能改变策略。模式不会绕过既有提交门禁，也不表示完整 V3 Operation/Broker 已实现。

模型输入中的 ID 为规范十进制字符串。`scope.kind` 必须是该查询支持的 `advertiser/agent/shop/customer_center/organization/authorization/public_catalog`，除 `public_catalog` 使用空 ID 外均要求精确主体。父子关系必须由当前授权记录证明，子账户还受更窄白名单限制。仅持有广告主 ID、工具可见或远端 Schema 可见都不证明代理、店铺或组织权限；授权过期时返回受控恢复要求。

## 参数与分页

源 Schema 的矛盾与补充约束以独立执行合同保存。例如数字枚举、建议参数、预算范围、日期跨度和条件必填项结合公开文档及锁定 SDK 核对，不静默改写原始来源。精确目录条目返回 `input_schema`、`request_fields`、`restricted_parameters`、`conditional_rules`、`bound_user_fields` 与 `max_date_span_days`。出价建议的商家角色豁免尚无足够本地证据，当前合同仍要求精确 `aweme_id`。

每次通用查询只读取有界一批，`limit` 为 1–1000 并继续受官方接口最大值约束。用户不能在 `parameters` 中直接提供 page、offset 或上游 cursor。返回游标签名绑定查询、主体、授权选择、参数、limit、实例策略与合同版本；续页必须保持这些输入。目录游标与结果游标不可互换。

| 受限项目 | 当前处理 |
| --- | --- |
| `qianchuan_tools_smart_boost_ad_boost_version_get_v1` | 官方文档缺少列表结束规则；只返回首批，完整性为未知。 |
| `subscribe_accounts_list_v3` | Schema 有 `next_cursor`，但未给出可验证的结束规则；只返回首批。 |
| `qianchuan_uni_promotion_product_get_v1` | 普通 tab 使用已核实页模型；`NEW_PRODUCT/BREAKTHROUGH_PRODUCT` 的游标结束规则未核实，只返回首批。 |

`qianchuan_product_available_get_v1` 的 `NEW_PRODUCT_BOOST` 场景采用文档明确的 cursor/`has_more` 模型，首请求省略 cursor；不能套用普通页码模型。上述三个受限项目保持 `contract_limited` 和 `PAGINATION_FIRST_SLICE_ONLY`，不存在可用的全量续页时不伪造游标。

## 结果与失败

通用查询返回查询身份、来源、查询时间、渠道与主体、日期/统计口径、白名单 `columns/items`、失败和 `completeness/has_more/cursor`。ID 与数值保留十进制字符串，空值为 null；不经过浮点数、不把缺失值补零，也不输出原始业务 JSON 或媒体 URL。

余额和计划消耗的文档单位为“千分之一分”，DTO 标记 `CNY/100000` 并保留原值；展示人民币元时须按十进制定点换算。元、分和秒分别标记已核实单位；文档未明确的单位保持未知，例如配额中的 `month_cost`，不得猜测。

`partial`、`no_data`、`failed` 与完整性单独表达。续页片段、结果上限或缺失分页合同均不能用于全量汇总或“没有候选计划”的判断。任务外层调用成功也可能包含失败事实，须逐项检查，不能将未知余额、预算或审核状态解释为正常。

SDK/MCP 共用广告主级两槽位只读准入、派发间隔、请求预算及频控冷却。HTTP 200 后仍验证业务返回码，`40100` 反馈共享冷却；不因协议 POST 占用业务写槽位。未知 Schema 返回 `SCHEMA_REVIEW_REQUIRED`，越权、缺参、授权失效、取消与超时使用稳定错误，不暴露底层错误文本。

## 可选开发资料

`developer_docs` 独立提供 `search_oceanengine_api_docs`、`get_oceanengine_api_schema`、`get_oceanengine_sdk_example`，仅查询文档及 `go/java` 示例。读取创建、更新接口文档不执行该业务接口。元数据在系统凭据库独立保存；可引用精确既有授权的非秘密 App ID，引用的 App 变化后绑定失效，也支持不依赖业务 OAuth 的独立 App 配置。

该通道不读取业务 Token/Secret，元数据不进入查询参数或结果，文本有大小限制和截断标记。缺配置、禁用或失败不阻断正常业务查询。配置和使用见 [配置与授权](../guides/configuration.md)及 [Skill 开发资料路由](../../skills/qc-plan-monitor/references/developer-docs.md)。

## 实现与证据

正式代码：[执行合同](../../runtime/ocean-watch-go/internal/contracts/official_approved.go)、[Core 查询](../../runtime/ocean-watch-go/internal/application/official/service.go)、[任务编排](../../runtime/ocean-watch-go/internal/application/official/tasks.go)、[业务 MCP Adapter](../../runtime/ocean-watch-go/internal/adapters/officialmcp/client.go)、[开发资料](../../runtime/ocean-watch-go/internal/application/developerdocs/service.go)。隔离测试覆盖 156 项 Core 和 JSON/SSE 传输矩阵、策略、主体、参数、分页、投影、错误及取消。

真实联调只证明已授权单账户的少量样本；日预算返回 50000 的原因未核实，不据此认定参数或权限错误。完整实际客户端验收与其他角色/场景的线上可用性分开记录。[P0.5 记录](../ai/research/qianchuan-official-mcp-poc-2026-11-01.md)是历史阶段证据，不能替代正式实现和当前 Host 验收。
