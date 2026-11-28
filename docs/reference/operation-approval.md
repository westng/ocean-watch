# Operation 确认基础合同

本文维护统一操作确认的源码合同。Core、本地 Broker 和 SQLite 审批消费已接入新的千川操作入口；旧投放入口尚未全部迁移。预发布运行时阻断旧在线写入。隔离测试不替代真实交互和业务验收，不能据此声称 M1/M2 外部验收完成。

## 千川操作入口

| CLI | MCP | 行为 |
| --- | --- | --- |
| `operations prepare --preflight-id ID` | `prepare_qianchuan_operation` | 重新核验官方作品授权/归属/商品、模板、目标及素材差异，持久化实际载荷和不可变确认摘要；无投放写入 |
| `operations get --operation-id ID --version N --snapshot-digest DIGEST` | `get_operation` | 本地读取摘要、每项派发与读回事实 |
| `operations confirm` + 精确操作三元组 | `request_operation_confirmation` | 独立 Broker 确认；只返回安全审批引用 |
| `operations execute` + 三元组 + `--approval-id ID --submit` | `execute_operation` | 消费审批后逐项派发；MCP 不接收 submit 布尔值 |
| `operations reconcile` + 三元组 | `reconcile_operation` | 只读对账并持久化事实，不重发或自动续发 |

确认与执行要求启动时已选择 `execute` 模式；准备和对账要求 `plan`，本地查看允许 `read`。执行前再次核对授权身份、业务日、模板、完整目标和作品/素材状态，实际载荷必须与已批准载荷逐字节规范化一致。审批等待期间不持广告主写锁。

新建计划名包含固定操作后缀用于未知结果对账。首发一次新建组最多 100 条作品；已有计划追加仍以每请求 100 条分批。超限或无法完整展示时阻断，不在批准后重新拆分。确认界面按实际派发字段展示预算，追加素材本身没有预算变更。

SQLite Schema 7 保存审批、派发目标和独立读回事实，支持在执行前显式重新确认。官方返回成功仍须读回身份、素材并保存当日绑定后才算 applied。未知/在途/尚未读回的同组记录会阻断其他操作重复派发。重启后可查询旧操作；对账等待旧租约结束或正常收尾释放，只有官方读回证实已应用才记录成功。部分成功的未派发项需重新预检与确认，不复用已消费审批。

升级旧状态需先执行 `operations upgrade-state` 查看预览，再显式 `operations upgrade-state --submit`。迁移器先做受保护备份，活动状态使用者阻断升级；普通查询不自动迁移。升级后的数据库不能交给旧 Runtime 写入，回退前必须使用对应备份和兼容 Runtime，并核对已经发生的线上操作。

Windows 状态目录要求受保护 DACL，数据库与 WAL/SHM 等旁路文件逐项核验实际所有者和 ACL；可信主体限定为当前用户、SYSTEM 与 Administrators。管理员进程的默认所有者可以是 Administrators，不能因此拒绝权限仍受限的继承文件；其他主体、缺失或 null DACL 和重解析点仍被拒绝。

## 千川预算单位

2026-11-27 核对了仓库固定的[官方 Go SDK v1.1.92 生成模型](https://github.com/oceanengine/ad_open_sdk_go/tree/v1.1.92/models)和下表各接口的公开参数表。SDK 给出字段名和数值类型，单位以对应接口、方向、字段的明确说明为准。

| 接口 | 字段 | SDK 类型 | 公开说明与展示 |
| --- | --- | --- | --- |
| [新建计划](https://open.oceanengine.com/docs/1804360384937988) 请求 | `delivery_setting.budget` | `float64` | 元，最多两位小数。确认书显示「计划预算（元）」及载荷数值，例如 `1234.50` 即 1234.50 元。 |
| [更新预算](https://open.oceanengine.com/docs/1841395352172800) 请求 | `update_budget_infos[].budget` | `float64` | 元，最多两位小数。此独立更新接口属于旧入口。 |
| [计划列表](https://open.oceanengine.com/docs/1771195810853899) 响应 | `data.ad_list[].ad_info.budget` | `*float64` | 仅写「预算」，单位未核实。报表预算列展示接口数值，报表 `amount_unit=CNY` 适用于消耗、GMV 等金额指标。 |
| [计划详情](https://open.oceanengine.com/docs/1804362305657868) 响应 | `data.delivery_setting.budget` | `*float64` | 仅写「预算」，单位未核实。执行读回比对数值，实际账号的写入、读回与后台金额对应关系待人工验收。 |
| [账户日预算](https://open.oceanengine.com/docs/1777635660375047) 响应 | `data.budget` | `*float64` | 元。`budget_mode=INFINITE` 表示不限。 |
| [常规建议预算](https://open.oceanengine.com/docs/1770008006444036) 响应 | `data.suggest_budget_low`、`data.suggest_budget_high` | `*float64` | 千分之一分，100000 个接口单位等于 1 元。 |
| [全域建议预算](https://open.oceanengine.com/docs/1828257556490251) 响应 | `data.budget`、`budget_min`、`budget_max`、`budget_danger`、`suggested_budget_list[]`、`estimate_trans_cost_list[].budget`（均位于 `data`） | `*int64` 或 `[]int64` | 文档说明推荐值、档位和上下限，单位未核实。保留各字段原值。 |

新创建的操作按上表生成确认摘要。已持久化的旧摘要与原快照、审批引用绑定，仍展示准备时保存的文字。如需带单位的确认书，应重新预检并准备新操作。

## 调用与数据边界

受保护组合根 `bootstrap.NewOperations` 固定 Provider、证据来源及 Verifier。`operations.Service.Confirm` 只接收精确操作 ID、版本和快照摘要，从 Repository 读取已经持久化的快照并生成确认内容。它不接受模型提供的说明、Provider、挑战、签名或“已批准”标志，也不派发官方写入。

快照 `items[]` 为每项保存 `item_id`、`group_id`、`action`、`target_id`、`payload_digest`、`expected_digest` 和 `changes[]`。每条变更保存 `field`、`before` 和 `after`，需要时保存 `material_count`。具体用例负责从实际派发载荷与预期对象生成这些事实，不能从自然语言说明反推。动作目前限于 `create/append/update/delete`。

Core 生成的确认内容包含渠道、广告账户、操作版本、对象、逐项变化、素材数量、删除风险、预检时间及有效期；预算、ROI 等变化通过带单位的变更字段展示。缺少变更、重复身份、隐藏控制字符、未知动作或超出完整展示上限时拒绝确认；旧 Journal 不能只凭摘要转换为可审批操作。正文最多 12 KiB、100 项，每项最多 20 个变更，不截断后继续批准。

审批期限最多五分钟，且不超过快照期限。保存决定时再次核对当前版本、摘要、范围和有效期；领取执行时也复核当前快照。若仅批准响应丢失、审批引用遗失或过期，先用 `get` 查看状态。尚未领取执行且预检仍有效时，可用原三元组显式再次 `confirm`。重新确认立即作废旧引用，用户重新批准后才得到新引用。预检已过期须重新预检、准备并确认。若丢失的是执行响应，用原三元组查看状态，已派发或结果未知时用 `reconcile` 对账，切勿再次派发。新版本使旧版本的审批不能领取执行。

## 本地 Broker

Broker 按请求启动同一 Runtime 的独立内部进程，在读取业务配置、创建业务客户端之前进入专用入口。它不持有官方凭据端口，不提供业务写入接口，不常驻，也不提供模型可调用的批准工具。

macOS/Linux 的父进程创建独立的 `0700` 临时目录和 `0600` Unix domain socket。随机挑战仅通过继承的 stdin 管道交给子进程，不放入参数、环境或文件。套接字路径和同用户连接本身不能证明批准；握手及返回决定必须匹配私有挑战和完整确认文档摘要。子进程还必须正常退出。帧有大小、字段和期限限制，不监听 TCP。

确认界面使用固定系统程序：macOS 使用 AppleScriptObjC/AppKit 的只读滚动视图，默认按钮拒绝；Linux 使用可滚动的 Zenity 文本视图，必须勾选核对并点击批准。正文作为数据传递，不拼入脚本。关闭、拒绝、异常、取消和超时都不能生成审批证据；取消会结束本请求的进程组并等待回收。子进程持续检查所属 Runtime 的连接，连接丢失即取消界面；正常收尾移除临时 IPC，强制杀进程后的目录清理及真实宿主生命周期仍属外部验收范围。

Provider 只保留当前请求的一份短期证据，受保护的 `EvidenceSource` 交付一次，Verifier 消费一次，完成或失败后销毁。对外结果仅含操作引用、`approval_id` 和期限。SQLite 保存证据摘要和 nonce 摘要，不保存原始挑战或 nonce；执行时在事务内消费审批并领取租约。

此边界不防御同一 OS 用户下拥有任意 Shell、进程控制和文件权限的恶意程序。宿主自动工具许可、TTY 输入和模型文本都不是用户对具体投放操作的确认事件。

## 平台与验收范围

| 环境 | 当前源码行为 | 尚缺证据 |
| --- | --- | --- |
| macOS | Unix socket + AppKit 界面实现；AppleScript 可做只编译验证 | 实际人工点击、取消、界面展示和宿主生命周期验收 |
| Linux 图形会话 | Unix socket + 固定 `/usr/bin/zenity`；缺少程序或图形会话则不可用 | 真实图形界面和宿主验收 |
| Windows | 明确返回 Broker 不可用，不降级到 TCP 或终端确认 | 受保护 named pipe、原生界面及其隔离和人工验收 |

在 `runtime/ocean-watch-go/` 运行 `go test -tags=isolatedhost -count=1 -run '^TestIsolatedOperationHost$' -v ./internal/application/plans/qianchuan` 可复现隔离宿主验收。测试用内存 MCP 客户端连接实际工具处理程序，在临时目录使用 SQLite 和模拟广告主、作品、业务读写与审批 Provider。三种场景分别检查拒绝后派发次数为零，批准后仅派发一次，以及模拟写入已发生但响应丢失后，在新 MCP 会话重开数据库、读取 `unknown`、只读对账恢复 `applied`。再次执行原审批必须失败，总派发次数仍为一。

现有宿主没有因此被认证为可信原生审批 Provider。本地隔离测试验证协议、Verifier、SQLite 一次性消费、版本漂移及未知派发事实；它们不能证明真实用户界面或五平台准入通过。只有新的操作入口使用本 Broker；稳定版旧 `--submit` 路径保持既有合同，预发布则拒绝这些旧在线写入。
