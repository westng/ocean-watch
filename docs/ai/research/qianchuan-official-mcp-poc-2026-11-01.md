# 千川官方 MCP P0.5 最小真实联调

历史属性：本文是 P0.5 最小联调时点的证据，不描述后续正式实现。当前合同见[正式查询合同](../../reference/qianchuan-official-mcp.md)，后续实施证据见[验收记录](../reviews/qianchuan-official-mcp-implementation-2026-11-01.md)。
文档属性：AI 工程验收快照。核对日期：2026-11-01；记录时间：11:18 UTC / 19:18 Asia/Shanghai。状态：单账户三项 GET 的最小工程联调通过；正式客户端、共享策略、首批任务和 Host 验收未完成。

权威边界：只证明本次明确授权的精确范围与所列字段。本文不证明 156 项全量可用、所有角色兼容、完整账户统计或实际 AI 客户端接入；后续引用须重新核对代码、环境和授权状态。稳定约束见[正式内部接入合同](../../reference/qianchuan-official-mcp.md)，阶段门禁见[实施计划](../plans/qianchuan-official-mcp-integration-2026-11-01.md)。

## 范围和环境

用户授权沿用上次视频预检账户做少量真实只读联调。通过最近匹配预检记录定位到唯一授权映射，只查询该广告主；旧预检快照已失效，仅用于定位，不作为当前作品验收证明。此记录不保存生产账户/授权 ID、名称、业务金额、Token、headers 或原始响应。

使用 macOS Keychain 中既有 TokenManager/TokenLease；本次租约 revision 2，未刷新 Token。Go 1.27.0 darwin/amd64，`modelcontextprotocol/go-sdk@v1.7.0`，`ad_open_sdk_go@v1.1.92`。官方端点为 `https://open.oceanengine.com/qianchuan/mcp`，业务写请求为零。

临时联调入口复用现有状态根、授权存储及 SDK 工厂；每次业务对照预算 6 次，账户读取使用共享槽位与冷却。入口及临时 SDK 比较方法在本轮结束前删除；没有安装/更新真实 Host profile、构建分发二进制、提交、推送或发布。

## 发现与实际 Schema

首次冷连接完成 initialize，tools/list 一页完成，未调用业务工具。Tool-Range 请求余额、配额、计划列表三项，三项均返回，但额外返回一个未选择工具。本地独立白名单没有因此扩张。公开 156 项登记、这次三项发现与当前 Host 工具数分别统计。

| query_id | 真实输入 Schema SHA-256 |
| --- | --- |
| `qianchuan_account_balance_get_v1` | `653d8528e11269eb27adac59167c288a5e30a538db01591d143fdcd224d14998` |
| `qianchuan_ad_quota_get_v1` | `f710ed35304a666fc90f0984fb8eb807dcd4e12c26ed44626c8d960f4734d138` |
| `qianchuan_uni_promotion_list_v1` | `6f7a64b85d1cd2a627cef343d5479b3d15c566f0f351df9c02cc518687d46026` |

这是当次完整原始输入 Schema 的 source digest，不是结构/语义执行指纹，也不批准后续 Schema 变化。原型要求调用者提供已审查的当前 digest；P1 的兼容分类与批准映射尚未实施。

余额输入为必填 `advertiser_id`，integer/int64；配额同型且 minimum 1。列表必填广告主、起止时间及营销目标；正确 API 文档还要求 `fields`，本次显式提供。列表时间格式、场景、排序与过滤依据[正确列表文档](https://open.oceanengine.com/labels/12/docs/1771195810853899)及锁定 SDK 核实。

列表可选 `page_size` 存在 integer 类型与字符串枚举冲突。原型没有修改服务端 Schema：省略可选参数，以官方文档默认 10 为保护条件，并检查实际返回。隔离测试证明显式整数 10 与字符串 "10" 均不能被原 Schema 校验静默放行。自选其他页大小未验收。

## 同条件业务对照

| 读取 | 样本与对照结果 | 最终通过运行的 MCP / SDK 耗时 |
| --- | --- | --- |
| 余额 | 返回码 0；14 个实际返回字段与 SDK 完整一致，账户身份相符；原始金额保持整数 token | 230 / 844 ms |
| 配额 | 返回码 0；通投/搜索两个顶层对象及其全部实际嵌套字段与 SDK 一致，月消耗的十进制 token 保留 | 345 / 425 ms |
| 商品全域计划第 1 页 | 10 条；有序计划 ID、商品 ID、stat_cost、page_info 与 SDK 一致 | 649 / 610 ms |
| 商品全域计划第 2 页 | 10 条；相同字段一致，跨页计划 ID 无重复 | 462 / 486 ms |

计划范围固定为 2026-10-31 的 `00:00:00`～`23:59:59`、`VIDEO_PROM_GOODS`、`UNI_PROJECT`、`ALL_DATA`、控成本、状态 ALL、不区分有无消耗，仅请求 `stat_cost`。不是完整账户计划集合；读取的是默认每页 10 条的前两页。

计划实际响应为 `structured_json`。20 次商品 ID 出现均超过 `2^53`，与 SDK 的精确整数逐项一致；实际计划 ID 本次未超过该阈值，超长计划 ID 由合成 JSON/SSE fixture 覆盖。本轮对照没有通过浮点转换恢复身份。

余额和计划 `stat_cost` 的官方单位均为“千分之一分”，即元的 `1/100000`。本次比较同单位原始值，未上线金额展示转换。配额 `month_cost` 的货币单位在已读取文档中未明确，不作单位推断。

两页均返回 `has_more=true`，最终 `complete=false`、`truncated_due_to_page_budget=true`，停止继续分页。这个未完成标记符合有界联调门禁，不说明全量列表已验收。

两页整份原始 data 的 DeepEqual 为 false；观察到 MCP 将未请求指标等键显式返回 null，而 SDK 的类型序列化省略相应键。未将未请求/未映射的其他字段纳入通过结论，也未声称所有差异已分类；P1 须用批准字段投影，并保留 missing/null 语义。

## 计量与失败

余额/配额最终通过运行：initialize 1、tools/list 1、tools/call 2、SDK 比较派发 2；业务预算 used 4 / limit 6。两页最终通过运行也为 initialize 1、tools/list 1、tools/call 2、SDK 派发 2，used 4 / limit 6。同一 MCP 会话复用到第二次业务读取，关闭成功；协议计数不是官方系统内部 API 总调用数。

余额/配额对照运行的打开/发现为 789 / 110 ms；两页运行为 856 / 125 ms。首次纯发现运行为 1252 / 185 ms。少量测量包含网络和本地保护开销，不推断 p50/p95、官方配额或保证提速。

联调中曾自然出现 `40100`，且相同条件的 SDK 列表读取成功；[官方返回码](https://open.oceanengine.com/labels/7/docs/1696710760866831)明确这是系统级请求频率超限。它不证明参数错误、账户权限不足或本账户 QPS 用尽。修复原型先释放读取槽位、后解析业务码导致的冷却反馈遗漏：现在先解析，安全的 40100 错误进入现有 SDK/MCP 共享冷却器。

最终两页运行没有出现限流，不需要重试；命令预算和限流失败反馈有隔离验证，不宣称修复后真实命中过冷却恢复路径。临时联调入口最多每页重试一次，该恢复分支在修复后的真实读取中未再次触发；预算/父截止时间始终生效，未人为制造过期、越权或限流。

临时 SDK 对照入口初次漏接命令预算，调用在本地派发前被拒绝；补齐预算后完成上述对照。配额 SDK 依据正确 API 使用 ad.oceanengine.com 对应现有 profile，不把所有千川接口统一到 api.oceanengine.com。

## 验收与后续门禁

P0.5 最小工程连接、三项业务 GET、真实默认分页、长 ID/金额原始精度、部分结果标记和自然错误记录已通过。S_R1 的余额/配额/列表已有最小 PoC 证据，但 15 项查询合同草案和 9 个目标任务不因此变为可执行。

尚未验证：全部字段和单位、所有账户/角色/场景、任意页大小、Token 刷新/旧会话退役、完整共享策略、正式客户端复验、新任务 MCP/Skill 路由、实际 Host 发现调用及其他 153 项。原型继续默认不接入生产任务，现有 SDK 预检和报表保留。

本轮代码回归通过：`go test -race ./internal/adapters/officialmcp ./internal/contracts`、`go test ./internal/bootstrap -run '^TestOfficialProbe' -count=1`、`go test ./internal/mcpserver`、`go test ./internal/adapters/filesystem -run 'Qianchuan.*(Read|Request)' -count=1`。分别覆盖原始协议/分页/预算/频控反馈、TokenLease 装配、既有工具合同及共享读取控制。156 行机器目录与原始 Markdown 表一致；55 条本地文档链接、代码块、Scope ID 脱敏及临时入口清理检查通过；Skill quick_validate、分发校验、`git diff --check` 及新增文件空白检查通过。所请求消耗缺失/null 拒绝与精确十进制读取加入后，`go test -race ./internal/adapters/officialmcp` 复验通过；这些保护未改变真实请求参数或已对照的数值。
