# Ocean Watch 开源重构 PRD 与目标技术方案

| 属性 | 内容 |
| --- | --- |
| 文档类型 | 产品需求、目标架构和实施基线 |
| 文档版本 | 0.5 |
| 状态 | 目标方案已明确；V3 已落地部分运行时改造，完整 M1–M5 与逐宿主外部验收仍未完成 |
| 创建日期 | 2026-10-21 |
| 更新日期 | 2026-10-30 |
| 目标 | 将 Ocean Watch 重构为好用、可恢复、可验证、易贡献的开源本地投放工具 |
| 本轮范围 | 产品与技术方案及阶段状态；不授权真实数据迁移、插件安装、提交、推送或发布 |

> 本文明确产品需求和第 3～12 节的目标技术方案；当前 V3 已落地部分运行时改造，但完整目标方案和外部验收仍以代码、测试与逐项证据为准。架构主线为 **共享 Go 业务核心 + 共享 Skill + 配置优先的薄宿主适配包**，即 Host-neutral Core + 多个 Host-native Adapter。复杂度集中在投放确认、数据一致性、对账和恢复，而不是通用插件框架。DeepSeek Harness 是插件生态参考案例和目标入口之一，不是 Ocean Watch 的核心架构依赖；不得把它的框架、Profile 或内部接口推广到其他宿主。实现中改变目标方案时，先更新本文或对应 ADR，不在代码中静默折中。

## 1. 产品结论

Ocean Watch 的目标产品是：

> **本地优先、面向 AI 助手、具有可信执行和可恢复能力的投放操作内核。**

产品面向 Codex、Claude Code、Harness、豆包工作和独立 CLI 等入口，并将 **WorkBuddy 列为后续必须接入的宿主目标**。各入口按自身正式支持的插件、Bundle、连接器、Skill 或命令行机制交付，并共享同一个 Go 业务内核；这里的 Harness 包含当前仓库接入的 DeepSeek Harness，不代表把其插件框架作为公共基座。

首批 M0～M5 仍围绕原有五个入口完成重构；WorkBuddy 的扩展边界在本期预留，具体接入纳入后续 WB1，不把“后面接入”解释为本期已经实现或必须同时发布。

宿主负责自然语言理解和界面承载；Host-native Adapter 负责原生安装注册、生命周期、能力声明、配置、审批桥接和结果映射；Host-neutral Core 负责确定性的授权解析、业务校验、预检、确认验证、执行、对账、恢复和数据保护。各宿主是平等适配目标，具体支持级别须逐个验收。

渠道入口统一为 `ads-plan-monitor`（营销）、`qc-plan-monitor`（千川）和 `xt-plan-monitor`（星图），都通过同一能力合同和共享 Go Core 执行。Skill 名称与内部渠道键分离，星图继续使用 `star_map`，账号类型为 `star_account`。星图已接入的授权与账户能力必须由独立 Skill 承接；计划、素材和报表扩展仍按实际 API 接入范围验收。

### 1.1 必须解决的问题

1. 用户不应理解插件缓存、Runtime 槽位、Python 环境或内部配置路径，才能完成安装和首次查询。
2. 高风险操作不能由一段聊天文本、一个 `--submit` 参数或模型填写的布尔值直接触发。
3. 预检和提交之间必须有不可变的操作版本；目标、预算、素材集合或计划身份变化时必须重新确认。
4. 官方请求超时、进程退出、本地写入失败和部分批次成功必须保留事实，不能把未知结果当作失败后盲目重试。
5. 多宿主必须共享一套业务内核，不能让 MCP、CLI、Skill 各自拥有一套授权和写入逻辑。
6. 开源贡献者必须能够在没有真实凭据、真实广告账户和真实 Host profile 的情况下开发和测试。

### 1.2 非目标

- 不引入 Ocean Watch 自营云服务、远程公网 MCP、多租户或跨设备同步。
- 不引入微服务、Kubernetes、Redis、消息队列或常驻后台服务作为普通用户前置条件。
- 不建设通用 Agent 编排框架、Host SDK、工作流 DSL、消息总线或任意命令执行工具；不为多工具接入增加完整桌面应用或自研更新平台。
- 不把参考项目的服务、安装器或框架变成 Ocean Watch 业务调用前置依赖；不因为示例采用 TypeScript 就把共享核心或安装工具全部改成 Node。
- 不把 Cordis、Harness Profile、某一宿主的 SDK 或会话对象变成公共业务内核依赖；不为了“原生”重复实现宿主已有的 MCP/Skill 提供方。
- 不提供无人确认的自动投放、自动调预算、自动停启计划或自动策略执行。
- 不承诺官方 API 未提供的撤销、强一致或端到端 exactly-once。
- 不把星图计划/报表、其他广告平台或全面 F2 Go 重写作为本次重构前置条件。

## 2. 用户流程与成功标准

### 2.1 首次使用

```text
在现有宿主安装 Ocean Watch 对应插件/Skill，或安装独立 CLI
  → 通过宿主初始化入口或 ocean-watch setup doctor 检查
  → 配置 OAuth App
  → 完成渠道授权
  → 同步授权账户
  → 查询账户/模板/报表
```

成功标准：

- 用户不需要编辑 JSON、寻找缓存目录或安装 Go。
- 环境检查按能力报告缺失项和修复动作；统一 `setup doctor` 的无凭据、无业务请求诊断合同见第 8.6 节，不能以检测到资源文件冒充宿主已加载。
- 无 F2 时，账户、模板、报表等不依赖作品公开元数据的功能仍可用。
- Secret、Access Token、Refresh Token 和 Cookie 不进入聊天、普通配置、日志或结果文件。

### 2.2 查询

```text
用户目标
  → 只追问缺少的必要字段
  → 调用任务型查询能力
  → 展示渠道、账户、时间范围、口径、部分失败和来源
```

查询不得因为模型不确定就扫描仓库、插件缓存、历史对话或全部工具目录。

### 2.3 写入

```text
创建/调整/删除目标
  → Prepare：确定输入和业务身份
  → Preflight：读取官方状态并生成操作快照
  → Present：展示差异、风险和摘要
  → Approve：独立可信交互确认摘要
  → Execute：只接受操作 ID + 精确版本 + 已验证审批回执引用
  → Reconcile：读取官方状态确认结果
  → Recover：处理部分成功、未知和重启恢复
```

确认前不取得广告主写锁，不发送在线写请求，不保存明文凭据到操作记录。

### 2.4 首个纵向闭环

本次重构先贯通：

```text
千川作品批量预检
  → 操作快照
  → 用户确认
  → 批量创建/追加
  → 部分成功与未知结果对账
  → 重启后查询和恢复
```

该闭环通过前，不迁移所有业务写入，也不删除仍承担兼容或恢复职责的旧实现；现有兼容路径不代表目标架构已经完成。

## 3. 目标架构总览

```text
首批：Codex Plugin / Claude Plugin / Harness Bundle / 豆包 Skill / CLI
后续：WorkBuddy Connector + Skill
                                 │
                   薄 Host Ecosystem Adapter Layer
              声明式清单 / 安装注册 / 生命周期 / 审批桥接
                共享 Skill：业务意图、调用顺序与展示规则
                                 │
                    Ocean Watch Capability API
              稳定请求/响应、能力元数据、错误和操作身份
                                 │
               跨进程：本地 MCP stdio / 稳定 CLI JSON
               进程内：CLI / Go 入口直接调用 Application
                                 │
                    Host-neutral Go Core
                  Application → Domain / Ports
                                 │
               SQLite / OS 凭据库 / 官方 API / F2
```

“原生适配”表示遵守宿主生态，不表示必须绕开 MCP 或编写宿主进程内业务代码。宿主官方插件机制若通过 Skill + MCP 集成，按此分发就是原生适配。MCP 是可复用的协议入口，CLI 是独立使用及部分宿主支持的调用入口；两者都不是业务内核，也不是所有宿主必须采用的同一种插件格式。

### 3.1 架构选择

| 选择 | 目标方案 | 不采用的方案 |
| --- | --- | --- |
| 部署形态 | 本地优先、进程内模块化单体 | 微服务、远程业务服务 |
| 核心语言 | Go 作为唯一广告业务运行时 | 第二套 Python/Node 业务实现 |
| 宿主生态 | 声明式宿主描述 + 清单模板 + 少量专属代码，按正式扩展机制分发 | 一个宿主框架通吃，或提前建设通用 Host SDK |
| Skill | 共享 Markdown 业务意图与展示合同，附受控宿主调用映射 | 多份业务规则副本，或由 Skill 自行决定权限 |
| 调用边界 | 官方 MCP Go SDK 的本地 stdio + 稳定 CLI JSON，Go CLI 内部调用 Application | MCP 启动 CLI 并解析 stdout；自建第三套网络/RPC 业务入口 |
| 能力合同 | Go DTO + JSON Schema + 能力元数据；按任务设计工具 | 每条 CLI 机械映射工具，或暴露万能命令 |
| 业务组织 | 按能力的垂直切片 + 共享基础设施；工具集与执行模式正交 | 按入口复制业务逻辑，把工具可见性当执行权限 |
| 事务状态 | `database/sql` + `modernc.org/sqlite` + 编号 SQL 迁移，不引入 ORM | 多个 JSON 文件组成隐式事务，或顺带建设通用数据库框架 |
| Secret | macOS Keychain、Windows DPAPI、Linux Secret Service | Token 写入 SQLite、普通配置或日志 |
| 公开作品元数据 | Python F2 作为可替换外部 Adapter | F2 承载授权、计划或报表逻辑 |
| 写入确认 | 统一 ApprovalProvider；已验证宿主可信审批优先，按需本地 Broker 兜底 | 自动相信宿主品牌、`--yes`、`--submit`、模型布尔值或任意回调 |
| 升级 | 进程固定不可变 Runtime，显式更新 | 业务调用期间后台热切换 Runtime |
| 发布 | Go 多平台构建 + GitHub Actions，同源生成宿主包、校验和及来源证明 | 在业务请求中下载/热换 Runtime，或自研更新平台 |

### 3.2 Host Adapter 与 Core 的责任边界

| 层 | 必须负责 | 禁止负责 |
| --- | --- | --- |
| Host-native Adapter | 宿主清单、安装/启停/卸载、Skill/Tool 挂载、受支持配置入口、可信审批桥接、取消和结果映射 | 官方授权选择、业务分组、计划匹配、预算规则、业务状态写入与 Token 存取 |
| Capability API / Transport | 稳定 DTO、能力/副作用元数据、严格解码、错误码、兼容和协议映射 | 任意 shell、数据库通道、把宿主声明当作审批证明、复制业务流程 |
| Application / Domain | 完整用例、操作状态、确认验证、派发控制、对账和恢复 | 引用宿主 SDK、Cordis、Profile、宿主会话对象或界面组件 |
| 基础设施 Adapter | SQLite、安全凭据、固定官方端点、公开元数据与受保护文件 | 自行决定哪个用户目标已确认、绕过 Application 在线写入 |

业务内核不得 `import` 宿主包；宿主会话 ID 只能作为不透明追踪元数据，不能授予广告主权限或执行权。一个宿主缺少可选功能不阻塞其他宿主；两者也不能因版本不同各自复制和轮换同一授权。

### 3.3 各入口的目标交付形态

下表是目标与证据边界，不宣称新版已兼容所有桌面版本。官方机制核对见第 16.1 节；实施前仍须以实际安装版本验证。

| 入口 | 分发与挂载目标 | 当前可引用依据 | 不作无证据承诺 |
| --- | --- | --- | --- |
| Codex | 对应版本支持的 Plugin 清单 + Skill + 本地 MCP；复用宿主插件管理与目录发现 | 官方同时说明便携清单和 Codex 兼容格式 [S1]；仓库已有兼容清单 | 不因通用文档存在 hooks/UI 就宣称目标桌面版本提供可验证审批回执 |
| Claude Code | Plugin 清单、Skill、本地 MCP；配置和 hooks 仅在版本支持且确有需要时使用 | 官方插件清单与 MCP 注册规则 [S2]；仓库已有适配 | 不强制新增工具提供方，不依赖未验证的审批/会话 API |
| Harness | 当前接入的 DeepSeek Harness Bundle；复用其已支持的 MCP/Skill provider 与卸载生命周期 | 官方 Bundle/Profile 边界 [S3]；仓库 `harness/index.mjs` 已采用提供方组合 | 其内部框架依赖只留在 Harness 适配包，不进入 Core；不安装独立旧 CLI 宿主 |
| 豆包工作 | 以目标桌面版本实际支持的 Skill 安装与调用方式交付；入口解析固定 Runtime | 当前仓库 Skill 安装器；本轮未核实其官方通用插件/审批 SDK | 不杜撰豆包 Plugin/Settings/Approval API，不把 Skill 安装称为全部原生能力已验收 |
| 独立 CLI | 原生命令行、稳定 JSON、交互/非交互模式和审批接入 | 直接调用 Go Application；不要求用户安装桌面宿主 | 不把 CLI 参数或 TTY 存在当作可信确认本身 |
| WorkBuddy（后续 WB1） | Connector + Skill，业务调用选择本地 MCP stdio；如采用外层插件打包，仍复用这一业务通道 | 官方连接器开发规范 [S6] 与插件用户说明 [S7]；文档核实不等于桌面验收 | 不在同一连接器混用 MCP/CLI 业务路由，不推定连接授权就是投放审批，不要求新增云服务 |
| 其他宿主 | 增加适配包和能力声明，复用 Capability API | 按接入时的正式扩展接口与版本验收 | 不要求使用 Harness 格式，也不以“兼容 MCP”代替完整产品验收 |

### 3.4 能力声明与生命周期合同

每个适配包提供可机读描述：`adapter_id`、适配包版本、宿主版本范围、平台、安装方式、Skill/Tool 注册方式、Runtime/Capability 合同范围，以及 `approval_mode`、配置/取消/结果展示/卸载能力及证据引用。`approval_mode` 仅可为 `verified_host`、`local_broker` 或 `unavailable`；声明不是权限凭证，不能由普通请求覆盖。

- 使用宿主已有提供方可避免重复注册；同一作用域中的每个逻辑能力只注册一次，项目级与插件级并存须明确显示来源和处理冲突。
- 启用、停用、重载、卸载由宿主拥有的生命周期触发；只释放本适配器持有的监听、工具、连接、子进程和租约，不结束其他宿主进程。
- 卸载不删除共享账户、凭据和操作历史。用户显式清除数据是独立动作，必须检查其他宿主和未结操作。
- 宿主取消只停止未派发工作；已有派发可能生效，Core 继续保留事实并在恢复时对账。会话结束不是官方撤销。
- 缺少 Settings、事件流或可信审批时，声明准确支持级别并走已定义替代路径，不模拟不存在的接口。

### 3.5 进程与资源所有权

不设计共享 MCP 守护进程、共享刷新 worker 或共享 Broker 服务。多个宿主共享的是受保护数据、系统凭据引用及跨进程锁，不是一个可被某个宿主随意停止的常驻进程。

| 资源 | 所有者和释放方式 | 多宿主行为 |
| --- | --- | --- |
| MCP 连接、Go MCP 子进程、宿主工具注册和监听 | 由对应 `adapter_id + instance_id` 实例独占；宿主提供方按其生命周期管理，退出只关闭本实例资源 | A 与 B 各自启动连接/进程，不共享句柄；卸载 A 不得结束 B |
| CLI 子进程及临时调用文件 | 对应调用实例；正常退出清理，异常退出只回收可证实属于自己的资源 | 不通过全局进程名查杀，不删除其他实例临时目录 |
| Broker、临时 IPC 端点和审批交互 | 单次确认请求的 Provider/Broker 实例；独占端点，结束或超时回收 | 不创建全局共享确认窗口；操作/版本/请求 nonce 区分来源；旧请求不能复用到另一实例 |
| Runtime、Skill 和 Adapter 发布文件 | 安装层管理的不可变产物；活动进程持使用租约，只由安装层做受限清理 | 共享同一字节产物允许，但不共享运行进程；卸载单个 Host 不删除其他包或未结操作仍需的资源 |
| SQLite 连接与刷新逻辑 | 每个 Runtime 进程独占自己的连接和按需刷新逻辑；退出关闭连接；事务、授权刷新锁和广告主 OS 锁跨进程协调 | 数据和锁协议共享，不创建第二份授权；没有全局刷新 worker 的 owner/refcount 问题 |
| 共享数据库、凭据和业务历史 | 用户所有；仅通过受控迁移/清理动作处理 | 最后一个 Host 卸载也不自动删除数据；只清理各自临时进程/端点与活动租约 |

正常停用先停止接收新任务、取消未派发工作，再有界关闭本实例；已经派发的请求按第 6 节保存状态。异常退出由 OS 释放进程句柄和锁，遗留临时资源仅在能核对实例/端点归属且确无活动持有者时回收；不凭 PID 数字或目录年龄直接判定可删。宿主重载生成新实例，不能继承旧进程的执行权。

### 3.6 WorkBuddy 后续接入方案

WorkBuddy 使用同一 Capability API、Go Core、共享状态根和系统凭据库，不另建 WorkBuddy 版广告业务、Token 仓库或远程服务。当前提供 `hosts/workbuddy/` 薄 Skill 输入与 `scripts/install-workbuddy.py` 本机安装入口，用于真实共享状态及浏览器授权恢复；WB1 的正式连接器分发包与全部宿主验收仍待完成。

**确定的接入方向：**

- 选择官方开发规范中的 **MCP + Skill** 连接器方案，运行一个本地 stdio MCP Server；不要求部署 HTTPS 服务，也不把 Go Runtime 暴露到公网。[S6]
- 规划 `hosts/workbuddy/` 作为构建输入，生成 `connector-meta.json`、`mcp.json`、`icon.svg` 和 `skills/<name>/SKILL.md` 等宿主资源。图标是正式分发资源，不是本次文档任务的新产物。
- 官方要求一个连接器只配置一个 MCP Server，业务方案在 MCP + Skill 与 CLI + Skill 中二选一。本适配包选择 MCP，不在 Skill 中为未覆盖功能静默改走 CLI；确需的业务能力先完成统一 Capability/MCP 映射。官方允许的独立认证辅助机制与混用业务路由不是同一件事，但本期不启用额外认证脚本。[S6]
- Go 二进制的封装、启动路径展开、`cwd`、字段最低客户端版本、沙箱对安全凭据与网络的访问，在目标 WorkBuddy 桌面版本验证；按实际使用字段生成 `minWorkbuddyVersion`，不复制 Node 示例或虚构 Go `runtime` 字段。未通过安装验证前不承诺一键可用。[S6]
- Skill 共享业务意图和展示规则，只覆盖 WorkBuddy 的发现/调用差异；不依赖安装 Codex、Claude 或 Harness 才能启动 Runtime。WorkBuddy 拥有并回收本实例 MCP 子进程，关闭连接器不删除其他入口共享数据。
- WorkBuddy 的连接认证与巨量引擎业务 OAuth、投放操作确认是不同层次。业务凭据仍由 Core 管理，不重复委托 WorkBuddy 云端保管，不把真实 Token 写入连接器配置；若后续需要连接认证，单独设计且不能冒用业务 refresh token。
- 原生投放审批回执、取消传播和进程释放目前未做桌面验证；只在来源/摘要验证通过后启用 `verified_host`。否则使用本地 Broker；两条通道均不可用时明确只支持查询/预检。插件启用、连接成功或授予目录权限不能代替具体投放确认。

WorkBuddy 官方用户文档还描述了包含 Skill、MCP 等组件的插件打包形态。[S7] WB1 按目标客户端支持程度决定外层包装与安装路径；插件包装不是第二套调用协议，不能因此重复注册同名工具或绕开上述连接器约束。首次接入以开发者连接器规范为配置依据；用户指南中的概括性命名与之不同，不据此推导可混用 MCP/CLI。

### 3.7 配置优先的宿主接入与共享 Skill

宿主差异通过第 3.4 节的声明式描述和各包内清单模板集中维护：支持版本/平台、安装作用域与资源布局、固定 Runtime 启动参数、Skill/Tool 注册方式、审批模式、取消及卸载行为。描述是构建/注册输入和验收依据，不是可执行工作流 DSL，也不能自行授予权限。

- 能由清单和模板表达的，不增加运行时适配代码；真正需要宿主生命周期或可信事件桥接的，只实现对应薄适配器。各宿主使用自身支持的配置格式，不强行统一外部清单。
- 安装优先交给宿主自己的插件管理机制；确需手工注册时使用第 10.3 节受控安装辅助，不因检测到目录就自动改写全局配置或个人规则。
- `skills/` 是业务意图、调用顺序、必要输入和展示规则的共享来源。宿主包只携带受控工具/路径映射；构建时校验映射存在、结果等价，避免长期维护多份业务说明。
- Skill 是给 AI 的使用说明，不是业务执行器、可信确认来源或权限凭证；授权选择、计划分组、预算校验、Token 管理和派发状态均由 Go Core 决定。
- JavaScript/宿主 SDK 只出现在确需它们的适配包。共享构建/安装辅助优先复用已有 Go 与小型脚本，不为接入引入第二套业务语言或通用插件框架。

### 3.8 开源方案借鉴与取舍

以下是架构方法的参考，不是要安装为 Ocean Watch 的运行依赖，也不表示这些项目已经解决我们的投放安全和目标宿主兼容性：

| 参考项目 | 吸收的做法 | Ocean Watch 的落点与不照搬部分 |
| --- | --- | --- |
| Superpowers [S9] | 共享 Skill、薄工具映射、宿主自己的加载/分发机制 | 第 3.7 节；不照搬全会话提示注入方式，不让 Skill 承担业务安全 |
| Context7 [S10] | 集中描述客户端配置路径、MCP 注册和 Skill 目录 | 声明式宿主描述与清单模板；不照搬其 Node 安装器、云后端或凭据参数传递方式 |
| GitHub MCP Server [S11] | 单一服务提供业务工具，按工具集和只读模式控制暴露范围 | 第 8.5 节；工具暴露限制不代替账户权限、逐操作确认和核心校验 |
| Playwright MCP [S12] | 根据场景选择 MCP 或 CLI + Skill，共享执行能力 | 第 8.4 节；不把 MCP 当安全边界，不以浏览器自动化替代官方投放 API，不在 WorkBuddy 连接器混用业务通道 |

技术栈保持 Go 模块化单体、官方 MCP Go SDK、Go DTO/JSON Schema、SQLite/系统凭据及官方 Go SDK/REST Adapter；Python/F2 只做可替换公开元数据解析。复用仓库已声明依赖，不因参考项目使用其他库而替换整套工具链。SQLite 驱动已在 `go.mod` 固定为 `modernc.org/sqlite v1.60.1`，配套 `modernc.org/libc v1.77.1`；千川预检 Journal 已接入 SQLite 操作版本/快照表，营销旧 Journal、凭据和其他迁移路径仍按阶段逐步切换。运行条件及跨平台准入按第 5.2 节在 M1 验证，尚未完成整体门禁。

## 4. 目标代码结构

目标仓库采用“宿主适配包 + 共享资源/合同 + Go Core”的单仓布局，不是把所有宿主写成 Go 插件。下列路径是目标，不表示文件已创建；安装清单不得直接指向未生成的目录。

```text
hosts/
├── codex/                       # 目标版本清单、安装/发现与接入测试
├── claude/                      # Plugin 清单、MCP/Skill、可用 hooks 配置
├── harness/                     # Harness 专属 Bundle 入口和生命周期
├── doubao/                      # 实际可用的 Skill 安装/调用适配
├── workbuddy/                   # 后续 WB1：Connector + Skill，目标路径尚未创建
└── cli/                         # 分发、帮助与独立入口接入测试
contracts/capability/             # Go DTO/JSON Schema、工具集、副作用、状态和错误合同
contracts/host/                   # 声明式适配描述与兼容证据格式
skills/                          # 共享意图/展示规则；宿主仅提供受控调用映射
runtime/ocean-watch-go/           # 所有入口共用的业务内核
```

`hosts/harness` 可保留必要 JavaScript/宿主依赖；其他宿主只需要清单和资源时不强行增加 SDK。构建时组装各宿主需要的根目录/资源布局，不能让目标宿主通过 `../` 引用包外资源。公共部分先使用声明式合同和小型工具，不提前发布一套通用 Host SDK。

Go Runtime 的职责结构为：

```text
runtime/ocean-watch-go/
├── cmd/
│   ├── ocean-watch/             # CLI 主入口
│   ├── mcp-server/              # 当前版本 MCP stdio 入口
│   ├── approval-broker/         # 按需启动的本地审批兜底
│   └── build-runtime/           # 发布构建器
├── internal/
│   ├── domain/
│   │   ├── operations/          # Operation、Snapshot、Approval、Dispatch 状态机
│   │   ├── authorization/       # 授权身份和账户映射规则
│   │   ├── marketing/           # 巨量营销领域规则
│   │   ├── qianchuan/           # 千川分组、绑定、计划和素材规则
│   │   └── reports/             # 指标口径和值对象
│   ├── application/
│   │   ├── operations/          # prepare、preflight、验证审批、execute、reconcile
│   │   ├── auth/                # OAuth、Token、账户快照
│   │   ├── templates/
│   │   ├── materials/
│   │   ├── plans/
│   │   └── reports/
│   ├── ports/
│   │   ├── state/               # Repository、Transaction、Migration
│   │   ├── credentials/         # CredentialStore
│   │   ├── official/            # 官方 API 的稳定 DTO 接口
│   │   ├── metadata/            # F2/公开作品元数据接口
│   │   └── approval/             # ApprovalProvider / ApprovalVerifier，宿主无关
│   ├── adapters/
│   │   ├── sqlite/              # SQLite Repository、迁移和备份
│   │   ├── credentials/         # Keychain、DPAPI、Secret Service
│   │   ├── oceanengine/          # 官方 SDK/REST 类型和转换
│   │   ├── f2/                   # F2 子进程适配器
│   │   ├── approval/             # 宿主证据验证映射与本地 Broker 客户端
│   │   └── filesystem/           # 配置、导出、锁和临时文件
│   ├── transport/
│   │   ├── cli/                 # 参数解析、JSON、退出码
│   │   ├── mcp/                 # 本地协议、Schema、Presenter、工具注册
│   │   └── approval/             # 受保护审批入口；不是模型工具
│   ├── bootstrap/                # 唯一生产组合根
│   └── platform/                 # 重试、分页、限流、时钟、日志脱敏
└── migrations/                   # 有序 SQLite 迁移脚本
```

依赖方向：

```text
Host Adapter → Capability contract → Transport → Application → Domain / Ports
基础设施 Adapter → Ports / 稳定 DTO
Bootstrap → Application + Transport + 基础设施 Adapter（唯一生产组装处）
Domain / Application / Ports 不依赖任何宿主 SDK
Official SDK 只出现在 adapters/oceanengine
```

这是模块化单体加薄进程边界，不是新增微服务。Go CLI 在自身进程中调用 Application；JavaScript/其他语言宿主通过其 MCP client 或受控 CLI 能力调用 Go，不能直接 import Go 包。MCP server 自身不启动 CLI，不解析 CLI stdout。后续若某个原生工具必须转发调用，应复用 MCP 合同，不再建第二套自由命令 RPC。

`transport/cli` 和 `transport/mcp` 不得直接调用官方 SDK、SQLite、CredentialStore 或 F2。业务数据访问只经过 Application/Ports；启动资源组装由 Bootstrap 完成。

## 5. 本地数据与持久化方案

### 5.1 数据所有权

| 数据 | 存储 | 规则 |
| --- | --- | --- |
| OAuth App Secret、Access Token、Refresh Token | OS Credential Store | 只存引用、版本和元数据；不进 SQLite |
| 用户配置 | `config.json` 或等价小型配置文件 | 仅存非秘密配置和用户可编辑设置 |
| 账户/授权快照 | SQLite | 带版本、来源、更新时间和完整性校验 |
| 模板、绑定、预检、操作、对账 | SQLite | 事务性数据，受外键和状态约束保护 |
| F2 公开元数据缓存 | SQLite 或可重建缓存表 | 不证明授权、归属或商品匹配 |
| 原始导出/诊断 | 用户显式指定路径 | 默认不生成，生成前显示敏感性说明 |

默认状态根：

```text
OCEAN_WATCH_HOME
  → CODEX_HOME
  → ~/.codex

$STATE_ROOT/ads-plan-monitor/
├── config.json
├── state/ocean-watch.db
├── state/backups/
└── logs/
```

`STATE_ROOT` 指上面按优先级解析出的有效根，不代表必须设置 `OCEAN_WATCH_HOME`。默认兼容旧数据位置不等于依赖 Codex 宿主；所有适配器显式使用相同有效根，未来改变默认目录必须走迁移。

不得默认支持网络共享目录；状态根必须属于当前 OS 用户，目录和文件权限由运行时验证。

### 5.2 SQLite 运行规则

目标驱动确定为标准库 `database/sql` + 无 CGO 的 `modernc.org/sqlite`，使用显式 SQL、窄 Repository 和编号 SQL 迁移，不引入 ORM 或通用 Repository 生成器。上游文档列出本项目目标的五个 OS/架构，并要求 `modernc.org/libc` 与所选驱动自身 `go.mod` 的版本一致；这只是选型依据，不是我们的运行验证。[S8]

M1 锁定驱动及配套依赖的精确版本与校验和，验证实际引擎、内存/二进制成本、锁行为和备份恢复；不把未测试的“最新版本”写成已支持。若该驱动无法满足硬门禁，先更新选型并重新评审，不在实现中静默换库或退回 JSON 多写者。

- 开启 WAL、foreign keys 和有界 busy timeout；对每个物理连接应用并验证连接级设置，显式限制连接池及写入并发，不能只在任意一个池连接上执行 PRAGMA 后假设全部生效。
- 五平台均以目标二进制实际运行数据库打开、并发写/checkpoint、崩溃恢复和备份恢复用例；交叉构建或上游支持列表不能替代运行测试。
- 多进程 WAL 选择已包含官方 WAL-reset 修复的实际 SQLite 引擎：`3.51.3` 或更新版本，或有官方证据的修复回移版本；核对各平台运行时引擎版本，而不只核对 Go 驱动模块版本。M1 的并发写入/checkpoint 测试覆盖此准入条件。[S5]
- 所有 schema 变化使用编号迁移，不允许运行时静默重建数据库。
- 网络请求不在 SQLite 事务内执行。
- 用户确认等待期间不持有事务、业务写锁或官方 API 锁。
- 每次迁移前创建受保护备份，并记录迁移版本、来源 Runtime 和结果。WAL 模式采用 SQLite 一致性备份接口或停写后经验证的完整快照，不能只复制主 `.db` 文件并忽略在途 WAL。[S4][S5]
- 旧 Runtime 只能读取它声明支持的 schema；只有明确验证了只读兼容才允许只读，否则阻断，不能让无法理解新 schema 的进程自行尝试读取。
- 删除数据采用保留期和引用检查，未知待对账操作不能被清理。

### 5.3 目标表模型

表名是实现基线的一部分，但字段可随实现细化：

```text
schema_migrations
  version, applied_at, checksum

subjects
  id, channel, subject_kind, external_id, display_name, state_version

authorizations
  id, channel, credential_ref, token_revision, snapshot_digest,
  state, created_at, updated_at

authorized_subjects
  authorization_id, subject_id, source, first_seen_at, last_seen_at

managed_accounts
  subject_id, label, enabled, sort_order, updated_at

templates
  id, channel, subject_id, kind, payload_json, payload_digest,
  state, revision, created_at, updated_at

plan_bindings
  business_date, group_id, subject_id, ad_id, binding_digest,
  state, created_at, updated_at

operations
  id, kind, channel, subject_id, status, current_version, created_at, updated_at

operation_versions
  operation_id, version, operation_schema_version, runtime_version,
  input_digest, snapshot_digest, expires_at, created_at

execution_leases
  operation_id, operation_version, owner_id, fencing_epoch,
  acquired_at, expires_at, renewed_at

operation_items
  operation_id, operation_version, item_id, group_id, action, target_id,
  expected_digest, status, dispatch_key, failure_code, updated_at

preflight_snapshots
  operation_id, operation_version, schema_version, normalized_input_json,
  decision_json, dependency_digest, created_at, expires_at

approvals
  id, operation_id, operation_version, snapshot_digest, provider_id,
  evidence_hash, source_reference, issued_at, expires_at, consumed_at, status

dispatch_attempts
  operation_item_id, attempt_no, dispatch_key, state,
  official_request_id, started_at, finished_at, response_digest

reconciliations
  operation_item_id, state, candidate_count, official_object_id,
  evidence_json, reconciled_at

credential_rotations
  authorization_id, old_revision, new_revision, old_ref, candidate_ref,
  state, started_at, activated_at, failure_code

legacy_operation_mappings
  migration_id, source_record_id, source_checksum, source_runtime_build,
  source_schema, target_operation_id, target_version,
  decoder_version, reconciliation_contract, recovery_state

legacy_item_mappings
  migration_id, source_record_id, source_item_id, target_item_id,
  original_dispatch_key, official_request_id, official_object_id,
  dispatch_state, evidence_digest, scope, resolution

events
  id, aggregate_type, aggregate_id, event_type, payload_json, created_at
```

`subjects.external_id` 保留官方字符串 ID，并以 `(channel, subject_kind, external_id)` 唯一，不能把星图账号误作广告主。`operation_versions` 与快照内容不可原地改写；子项、租约、审批和派发引用精确操作版本，不能只引用一个可变 operation ID。

Token、Secret、Cookie、原始作品分享链接、原始审批凭证和完整官方响应不得进入上述持久化表。链接在预检时解析为规范化作品 ID，快照保存恢复必需的 ID/业务字段和内容摘要；若原输入没有保留，恢复不得假装可以重放它。审批表只保存摘要和不含秘密的引用。必要诊断字段经过脱敏和长度限制。

`events` 是事务内审计，不是第二套 Event Sourcing 事实源，不引入消息总线。数据库 schema、版本表、外键、唯一键和状态转换由迁移脚本与测试共同约束。

### 5.4 Token 轮换的跨存储恢复

授权刷新顺序固定为：在 SQLite 记录待轮换意图 → 调用官方刷新 → 新版本凭据写入 OS 安全后端 → 事务激活新引用和 revision → 标记轮换完成。凭据库与 SQLite 不能被视为同一事务。

- 多宿主按渠道和授权身份共享刷新锁；等待者重新读取已激活 revision，不能再用自己缓存的旧 refresh token。
- 官方已成功、新凭据已落盘但引用激活失败时，重启检查 `candidate_ref` 并安全完成激活；不能直接继续使用旧引用。
- 官方可能成功但响应丢失，或新凭据无法保存且无法取回时，保留不确定状态并要求重新授权，不保证自动恢复。
- 系统凭据读取只限授权服务和受控迁移进程；Host Adapter、审批提供方和普通诊断不可读取。
- 上述各边界均注入失败与重启测试，不能只测试“失败后旧 revision 未变”。

## 6. 统一操作模型

### 6.1 操作状态

```text
draft → preparing → preflight_ready → awaiting_approval → approved → executing
           └→ blocked                      └→ cancelled / expired
executing → succeeded / failed / partially_succeeded / unknown
unknown / partially_succeeded（含未知子项）→ reconciling
reconciling → succeeded / failed / partially_succeeded / unknown
```

这是分支状态机，不是所有操作必经的线性步骤。外层状态由子项汇总，有未知子项时必须显式标记待对账，不能作为可重试的明确失败。

规则：

- `preflight_ready` 绑定规范化输入、目标范围、模板摘要、前置状态摘要和有效期。
- `approved` 必须绑定当前 `operation_id + version + snapshot_digest`。
- `executing` 只能由一个持久化执行租约持有者领取；租约和派发前状态更新使用数据库事务及递增 fencing epoch，旧持有者不得在失去执行权后再派发。
- `unknown` 表示不能证明官方请求未生效，不能自动当作失败。
- `partially_succeeded` 不得被摘要成全部失败或全部成功。
- 目标、预算、素材集合、计划身份、授权范围或业务日发生实质变化时，旧审批失效，必须产生新版本。

### 6.2 派发语义

每个写入项都拥有稳定 `dispatch_key`；哈希输入采用有版本的规范化结构编码，不做可歧义字符串拼接：

```text
hash(operation_id, operation_version, item_id, target_id, action, payload_digest)
```

派发前：

1. 在 SQLite 事务中登记意图和 `dispatch_key`。
2. 检查该键是否已有明确成功或未知结果。
3. 明确成功的项不再派发。
4. 未知项先对账，不允许盲目重放。

官方请求超时后的结果只能是：

- `not_sent`：证明请求未发出；
- `acknowledged`：官方明确接受或读回确认；
- `unknown`：无法确认。

Ocean Watch 不把本地重试次数当作远端幂等证明。`dispatch_key` 只约束同一操作版本，不能阻止用户另建一个操作重复创建；跨操作重复风险仍通过广告主写锁、持久计划绑定、官方现状复核和业务身份处理。

确认消费、操作从 `approved` 领取执行和租约建立必须原子提交。执行期间只在短事务中登记每项派发；租约到期后接管者先处理在途/未知项，不能只拿到新租约就再次请求官方。取消与插件卸载不清除派发事实。

SQLite fencing epoch 只能拒绝本地过期写者，不会约束第三方官方 API。真正派发使用广告主级、由操作系统持有的跨进程写锁，锁覆盖派发前校验和该次有界网络请求/派发记录，不能凭租约到期强行抢占仍活着的持锁进程。进程崩溃释放 OS 锁后，接管者对遗留的已登记未完成派发按未知处理，先对账。等待锁期间可取消并报清晰诊断，不以并发性能为由解除这道边界。

首个可靠闭环采用有界、逐项派发；不以前置建设高并发写调度、队列或常驻 worker 为目标。返回 `operation_id` 仅表示状态可持久查询，不表示进程退出后仍有后台执行者。未派发项保持待处理，重启后重新校验版本、审批、执行权与依赖再显式恢复；在途未决项先对账，不自动续发。

## 7. 可信确认方案

### 7.1 统一审批端口：原生优先，Broker 兜底

目标不是所有宿主再弹一个相同窗口，而是使用统一的 `ApprovalProvider` / `ApprovalVerifier` 端口：

```text
Application 保存 operation_id + version + snapshot_digest
  → ApprovalProvider.Request：只能请求交互，不能签发同意
      ├── 经验证的宿主原生用户审批 → Host Approval Adapter
      └── 无可信宿主通道 → 按需本地 Approval Broker
  → 受保护验证入口校验审批来源及摘要，保存审批回执
  → 返回无秘密的 approval_id（只是引用，不是权限本身）
  → Execute 校验回执并原子消费、领取执行
```

Native Provider 和 Broker 使用同一审批内容、有效期和一次性消费规则。优先使用原生能力的前提是能证明收到的是用户在可信界面上对该具体操作的决定，而不只是插件回调、工具调用许可或模型生成的“同意”。通过不了验证时不启用该原生模式。

### 7.2 宿主无关的 ApprovalEvidence

审批证据至少绑定：`provider_id`、`adapter_id`、已验证宿主版本/来源、`operation_id`、`operation_version`、`snapshot_digest`、动作/对象范围、随机 nonce、签发和过期时间、决定及来源事件引用。

- 上述字段只是合同，不构成自证；`source=codex`、`trusted=true` 或任意 JSON 签名串不能获得权限。
- Provider 身份与可验证来源由安装/启动时的受保护配置建立，普通请求不能选择信任根、验签器或任意 Provider。
- Host Approval Adapter 负责把目标版本的真实审批事件转换成证据；Verifier 负责验证来源、内容、nonce、期限与操作版本。Core 不依赖宿主 SDK 或其内部对象。
- 必须确认哪些用户配置会自动放行、是否允许非交互决定，以及实际审批粒度。仅允许启动 MCP/CLI 进程的宿主权限，不等于用户已确认其内部的广告主、预算和素材差异。
- 模型只可拿到审批状态与 `approval_id`。原始 token、签名材料或回调认证秘密通过受保护 IPC/内存通道流转，不进入 MCP 参数、命令行、日志、数据库普通字段或项目文件。
- 受保护审批入口不是普通模型工具；不能调用 `approve_operation` 任意给自己批准。拿到一个 `approval_id` 也仍须校验对应的真实回执。
- 取消、拒绝、超时、自动许可和不存在确认记录都不能被转换为同意。操作版本或摘要不匹配时重新预检并确认。

### 7.3 本地 Broker 的具体边界

Broker 是宿主能力不足时按需启动的独立确认进程，不是常驻业务服务，也不是新桌面工作台。

- 默认通过受当前用户权限保护的 Unix domain socket / Windows named pipe 通信，不默认开放 TCP 监听。若界面需要 loopback HTTP，必须有独立安全设计与来源/会话绑定，不把 localhost 等同于可信来源。
- 显示由 Core 生成并绑定摘要的 canonical summary：渠道、广告主、目标对象、动作、关键差异、预算/ROI、素材数量、删除风险、预检时间和有效期。
- UI 的用户确认事件与模型输入隔离，采用短 TTL、随机挑战和一次性消费；IPC 同用户认证只用于进程边界，不单独证明点击由人完成。
- Broker 和宿主审批适配器均不能读取官方 Secret/Token，也不能派发官方业务请求。
- 用户关闭、取消和超时立即返回未批准，不保留可自动提交的默许状态。

### 7.4 降级与信任边界

- 宿主原生审批可信：使用原生 Provider，不要求用户重复到 Broker 再确认。
- 原生通道不可验证：使用可用的本地 Broker；Broker 也不可用则只支持查询、预检和恢复性只读对账，明确给出替代入口。
- 独立 CLI 的人工与非交互调用共享同一 Verifier。无界面环境只能消费事先生成、仍有效且精确匹配的审批记录，不能通过 `--yes`、`--force`、TTY 或环境变量绕过。
- 本方案不宣称能防御同一 OS 用户下拥有任意 shell、进程和文件权限的恶意程序；宿主若赋予模型这些权限，必须缩小安全承诺，不能称为绝对不可绕过。
- 当前没有任一新 Host Approval Adapter 在本轮获得实际认证；实现前须逐版本完成来源验证和失败用例，未通过不得标注可信写入支持。

## 8. 入口与接口方案

### 8.1 Application API

所有入口使用同一 Capability API 语义；Go 进程内调用应用接口，其他语言通过第 4 节定义的进程边界调用。以下是接口职责示意，不是已经存在的可编译 SDK：

```go
type OperationService interface {
    Prepare(ctx context.Context, request PrepareRequest) (Operation, error)
    Preflight(ctx context.Context, operationID string) (PreflightResult, error)
    Get(ctx context.Context, operationID string) (OperationView, error)
    RequestConfirmation(ctx context.Context, operationID string, version int) (ApprovalStatus, error)
    Execute(ctx context.Context, operationID string, version int, approval ApprovalReference) (ExecutionResult, error)
    Reconcile(ctx context.Context, operationID string) (ReconciliationResult, error)
    Cancel(ctx context.Context, operationID string) (CancelResult, error)
}
```

`Execute` 只消费精确操作版本和已验证审批回执引用，不接受重新构造的业务 payload、作品 URL、模板 JSON 或自由命令。`RequestConfirmation` 只能触发用户交互，不能使操作进入 `approved`；审批回执由第 7 节的受保护通道产生。查询及取消请求的主体范围也由 Core 校验，不信任 Host Adapter 自报广告主权限。

Capability 合同维护稳定 DTO、风险/副作用、兼容版本、状态与错误码；Native Tool 和 MCP 对同一用例映射这些合同，不复制业务校验。共享错误和事实字段，展示可按宿主能力调整；必需业务列、日期和失败信息不得被丢弃。

使用 Go DTO + JSON Schema + 静态能力元数据：至少描述 `capability_id`、所属工具集、渠道、输入/输出 schema、官方及本地副作用、最低执行模式、审批要求和合同版本。Go 核心处理程序与 MCP/CLI 映射使用同一合同，CI 用 fixture 检查 DTO、Schema、注册清单和错误语义漂移；不引入可上传代码、自由路由或动态业务插件。

### 8.2 MCP

保留高频只读任务型工具，按账户查询、报表、预检、操作执行等任务设计，不机械地把每条 CLI 命令变成 MCP 工具；写入工具不暴露万能命令。工具集和执行模式按第 8.5 节选择，目标接口分为：

```text
list_managed_accounts
get_*_authorization
list_templates / get_template
search_* / list_* / get_*
report_*
preflight_qianchuan_works
get_operation
get_qianchuan_preflight
reconcile_operation
request_operation_confirmation    # 只触发可信交互；不能产生批准
prepare_marketing_template_change # 固定用例：营销模板变更
preflight_marketing_template_change
prepare_marketing_plan_change     # 固定用例：营销计划变更
preflight_marketing_plan_change
prepare_qianchuan_plan_change     # 固定用例：千川计划设置
preflight_qianchuan_plan_change
prepare_qianchuan_material_change # 固定用例：千川素材变更
preflight_qianchuan_material_change
prepare_qianchuan_delete           # 固定用例：千川删除
preflight_qianchuan_delete
execute_operation                 # 精确版本 + 已验证回执引用
```

MCP 的任务型变更入口均先生成不可变操作版本，再复用 `get_operation`、`request_operation_confirmation`、`execute_operation` 和 `reconcile_operation`；不为每个业务动作复制一套确认/执行流程。目标高风险能力至少包括：营销模板变更、营销计划变更、千川计划设置、千川素材变更和千川删除；每项都必须有对应的 prepare/preflight 输入、风险摘要和 fixture，不能只依赖万能 `execute_operation`。

`preflight_qianchuan_works` 返回：

```json
{
  "operation_id": "op_...",
  "operation_version": 1,
  "snapshot_digest": "sha256:...",
  "status": "awaiting_approval",
  "expires_at": "...",
  "decisions": [],
  "blocked": [],
  "presentation": {}
}
```

MCP 不提供模型可直接调用的 `approve_operation` 来伪造用户确认。`request_operation_confirmation` 可以发起但不能代替用户确认；它是模型可调用的交互触发器，不是第 7 节所述受保护验证入口，也不能写入批准状态。`execute_operation` 只在 Core 已验证对应 Provider 回执时放行，不接受 `approved=true` 或客户端自造的审批内容。宿主工具权限和业务确认分别保留，不能以其中一方存在来跳过另一方。

没有 MCP 的入口仍可使用同一 Go CLI/Capability 语义；没有可信确认的入口不注册可用在线写入能力。按宿主正式工具模型映射，不要求为使用 MCP 的宿主另造一个原生 Tool Registry。

模式与确认的关系固定如下：

- `read` 不提供业务确认触发器，也不能创建可执行预检。
- `plan` 可以调用 `request_operation_confirmation` 触发可信界面展示；即使 Provider 返回批准，`plan` 也不能消费回执或在线执行，回执只能在仍有效且重新校验后的 `execute` 进程中消费。
- `execute` 才允许请求并消费与当前操作版本精确匹配的回执；模式由显式启动/配置动作切换，并在新 Runtime 进程初始化时固定，不能由模型参数临时提升。
- 第 7 节的 `ApprovalVerifier`、受保护 IPC/内存入口和回执消费不是模型工具；宿主或 Broker 只能通过该入口提交证据。

### 8.3 CLI

目标 CLI 命令分组：

```text
setup doctor [--json]
setup migrate
capabilities --json
auth authorize / auth sync-accounts / auth status
accounts list / accounts report
templates list / templates get / templates validate
operations get OPERATION_ID
operations reconcile OPERATION_ID
operations cancel OPERATION_ID
operations request-confirmation OPERATION_ID --version VERSION
operations execute OPERATION_ID --version VERSION --approval-id RECEIPT_ID
marketing templates prepare-change ...
marketing templates preflight OPERATION_ID --version VERSION
marketing plans prepare-change ...
marketing plans preflight OPERATION_ID --version VERSION
qianchuan plans prepare-change ...
qianchuan plans preflight OPERATION_ID --version VERSION
qianchuan materials prepare-change ...
qianchuan materials preflight OPERATION_ID --version VERSION
qianchuan delete prepare ...
qianchuan delete preflight OPERATION_ID --version VERSION
```

`RECEIPT_ID` 是数据库内审批回执的非秘密引用，不能自行授予权限；审批秘密不经 shell 参数或普通文件传递。交互命令仍须完成真实 Provider/Broker 交互，不能因为 CLI 命令已运行就标记批准。`marketing ...` 和 `qianchuan ...` 是稳定的任务型入口；未定义的 `plans ...` 万能命令不属于合同，新增高风险用例必须增加明确的 prepare/preflight 能力或先标记为未支持。

机器模式的业务结果 stdout 只输出一个稳定 JSON 文档；交互提示与日志不污染 JSON，日志只写 stderr；不输出 Secret/Token/Cookie、原始官方响应或内部堆栈。受控 CLI 适配调用该合同，不复制业务逻辑。

### 8.4 Capability API 与具体传输的等价合同

Capability API 是逻辑请求/响应合同，不是第三种网络服务。允许且仅采用以下三条调用路径：

1. Go CLI/其他 Go 入口在自身进程内调用 Application。
2. 支持 MCP 的 Host Adapter 用本地 MCP stdio 调用 Go MCP Server。
3. 不提供可用 MCP 入口的宿主用受控 CLI 子进程调用稳定 JSON；这是明确的第二种跨进程映射，不是从任意终端文本猜测业务结果。

| 维度 | MCP stdio | CLI JSON / 进程内语义 |
| --- | --- | --- |
| 合同准入 | 协议初始化完成后检查 Capability 版本及适配器要求；协议版本与业务合同版本分别管理 | CLI 提供无副作用 `capabilities --json`，返回同一业务合同版本；Adapter 初次连接固定 Runtime 时校验，不每次扫描仓库 |
| 身份 | 协议 request ID 与业务 `request_id` 分开；写入必须携带精确 `operation_id + operation_version + approval_id` | 参数只选择明确用例；结构化输入通过受限 stdin/已定义参数，不经 shell 拼接；输出保留相同业务身份，确认引用本身不是凭据 |
| 响应 | 工具结果返回结构化业务结果和稳定错误，必要 presentation 单独声明 | 机器模式一次调用一个 UTF-8 JSON envelope：`contract_version, request_id, ok, data/error`；大型结果分页，不截断成成功 |
| 错误 | 参数/协议错误与业务错误区分；业务错误使用稳定 `code, stage, retryability, next_action`，不泄漏内部错误 | 已知用例成功返回 `0`，参数/合同错误返回 `2`，业务/环境失败返回 `1`；具体未知/部分成功仍以 envelope 中状态为准，不能由退出码推断官方是否写入 |
| 非终态结果 | 调用返回 accepted/awaiting approval/executing 状态时保留操作 ID，后续通过 `get_operation` 查询 | CLI 同样允许返回操作非终态；退出码 `0` 只证明请求被正确处理，不代表批次已全部成功；Adapter 必须读状态 |
| 取消 | 可映射宿主取消到该次调用上下文；需要持久取消时调用精确操作的 Cancel 用例 | 进程信号只取消调用，不是官方撤销；`operations cancel` 为持久请求，已派发项仍保存并对账 |
| 超时/断开 | Host/MCP 截止时间不等于官方明确失败；Core 使用用例预算，有未决派发则保留未知状态 | Adapter 有界等待和终止本实例；没有完整 JSON 时视为传输不确定，禁止凭空合成失败或重放写入；通过操作身份查询恢复 |
| 进度/流式 | 目标版只要求完整响应和操作状态查询；可选通知不能成为正确性前提 | CLI stdout 不混入进度；轮询精确操作而不是保持无限 CLI 进程；不新建另一套业务流式协议 |

两种跨进程映射对同 fixture 的输入规范化、操作状态、错误和取消结果必须一致，差异仅限协议表示和展示。长期实现不得依赖某个 Host 的临时会话对象作为业务状态。请求/审批等待不得无限占用 MCP 调用或 CLI 进程，可返回可查询的持久状态。

### 8.5 业务工具集与执行模式

工具集决定向模型提供哪些业务能力，执行模式决定该运行实例可处理的副作用上限；两者正交，同一 MCP Server 内筛选，不为每个工具集启动一套服务或授权库。

| 工具集 | 目标能力 | 边界 |
| --- | --- | --- |
| `core` | 无秘密的能力/授权状态、账户及精确操作查询；受模式限制的公共操作入口 | 查询和执行均检查所属渠道/账户及操作内容，不是跨工具集的万能入口 |
| `marketing` | 营销模板、素材、计划的查询/预检/变更 | 按启用模式和每个用例风险分级 |
| `qianchuan` | 作品预检、计划组、绑定与千川变更 | 复用稳定业务身份、官方校验和恢复合同 |
| `reports` | 受已授权渠道/账户范围约束的报表 | 不因启用报表而获得对应渠道写入权 |
| `developer_docs` | 独立官方开发资料查询与 Go/Java 示例，默认关闭 | 使用系统保护的 App/开发者元数据，不读取业务 Token，不执行示例或业务接口 |

| 模式 | 允许行为 | 不允许行为 |
| --- | --- | --- |
| `read`（查询） | 官方查询、操作状态查询及明确只读的官方对账；可记录必要本地缓存/审计 | 投放变更、创建可执行预检或签发业务审批 |
| `plan`（预检） | 包含查询，计算方案并保存本地操作/快照、展示差异 | 派发官方投放写入、消费审批执行 |
| `execute`（执行） | 包含查询/预检，可请求可信交互并消费有效回执执行精确操作 | 绕过账户范围、摘要/有效期、防重放、租约、OS 锁或未知项对账 |

- 当前源码缺省为 `plan`，启用 `core/marketing/qianchuan/reports`，保留已有查询与预检；用户可用 `business_policy.toolsets` 明确缩小范围，`developer_docs` 默认关闭。一次注册同一作用域的逻辑服务，不因两个 Skill 重复注册。`plan` 仍会写本地状态，不得宣传成文件系统/数据库只读。
- 当前模式管理入口是用户配置 `business_policy.mode=read|plan|execute`；`allowed_subjects` 可同时限制主体与授权引用。切换后启动新的固定 Runtime，不能在一次 MCP/CLI 请求中隐式升权。`plan` 默认不等于 `execute`，用户必须显式选择后者且仍受用例门禁。完整 V3 可信审批仍按第 7 节作为独立目标，不由本批查询实施宣告完成。
- 模式与工具集由用户控制的安装/启动配置确定，连接初始化时固定并返回实际值；模型参数、Skill 或普通调用不能提高模式、选择信任根或扩展工具集。未知配置值、合同不匹配默认报错，不静默全开。
- `execute` 只有在第 7 节可信通道验证通过后才可用于在线写入；缺失时禁用写入并明确报告实际支持为查询/预检，不能仅提示警告后仍执行。独立 CLI 调用也受相同 Core 校验，不靠工具是否注册控制权限。
- Go Core 每次调用均复核实际模式、能力启用情况、渠道/账户、操作中的全部子项及审批；`core` 内的通用 Execute 也不能执行未启用业务工具集的操作。拿到其他宿主的 `operation_id` 或审批引用不会自动获得跨范围执行权。
- 查询/预检模式限制的是官方业务副作用，不冒称完全无本地写入。授权状态查询不发起新 OAuth；查询必要的 Token 刷新仍由受控授权服务按第 5.4 节轮换。配置、首次授权、迁移与清理走用户显式 setup/auth 管理动作，不因选择 `execute` 自动获得这些管理权限。

管理类操作不伪装成业务工具集能力，且不由 `read/plan/execute` 自动授予：

| 管理动作 | 允许入口与模式 | 副作用和确认边界 |
| --- | --- | --- |
| `setup doctor`、`capabilities --json` | CLI；宿主可映射为无副作用诊断 | 默认不发官方/OAuth 请求、不读秘密、不写用户状态；只报告资源、合同、模式和宿主发现证据 |
| `setup migrate` | CLI/受控安装辅助；不作为模型 MCP 工具 | 需要显式用户动作、备份、迁移锁和第 11 节未结项规则；不能由 `execute` 模式绕过 |
| `auth authorize` | CLI 或 `start_authorization` 对应的本机浏览器交互入口 | MCP 只能创建短期会话；用户在浏览器确认并完成官方 OAuth；授权秘密不经过模型参数，不能由 Skill 自行批准 |
| `auth sync-accounts` | CLI 或 `start_account_sync` 对应的本机浏览器确认入口；账户快照由 Core 写入 | 可能触发凭据刷新和本地状态写入；共享刷新锁和轮换恢复规则仍适用，不等于投放审批 |
| `auth status`、模板列出/读取/校验 | CLI；必要时可映射为受限只读工具 | 不授予业务写入；模板本地编辑必须单独展示本地副作用，不冒称官方投放变更 |
| 模板/账户本地创建、修改、删除 | CLI 或明确的本地管理界面 | 只改变本地配置/模板；如未来纳入 MCP，必须增加独立能力合同和风险确认，不能借 `execute_operation` 泛化 |

MCP/Skill 不提供 `setup migrate` 或泛化本地管理写入工具。授权管理只开放上述两种受限交互会话及 `get_authorization_session`、`cancel_authorization_session`；参数不能包含秘密、回调地址、任意命令或模型确认标记，不能借业务执行模式隐式批准管理写入。完整状态、错误和恢复规则见[宿主内授权恢复](../reference/authorization-recovery.md)。
- MCP/CLI 对同一 fixture 的能力过滤、越权拒绝和副作用边界一致。验收包含未知模式、隐藏工具直接调用、跨工具集执行、其他账户操作、无审批及重放，不能只检查工具列表变短。

### 8.6 统一诊断与首次使用体验

统一 `ocean-watch setup doctor [--json]` 检查以下事实，并以无秘密的稳定结果返回 `check_id`、状态、证据范围和修复动作：

1. 当前 Runtime、适配包、Capability/操作格式与数据库 schema 是否兼容；不存在数据库时报告未初始化，不自动建库或迁移。
2. 固定 Runtime 与 Skill/MCP 资源是否封装完整、启动路径是否依赖 cwd；宿主可提供真实发现信息时核对，否则只报告文件/配置存在与宿主加载未验证，不冒称已发现。
3. 状态目录归属/权限、只读可检的 schema 和锁环境是否满足要求；只查看必要元数据，不扫描全部历史或读取真实 Journal payload。
4. 系统凭据后端是否可用、配置是否完整；检查可用性与非秘密引用，不读取 Token/Secret 值，也不触发刷新。未做网络验证的授权有效性明确标记未验证。
5. 当前工具集、有效模式、可验证审批方式与受限原因；无可用确认时清楚显示不能在线执行及替代入口。
6. F2 等可选依赖是否可用，以及缺失会影响哪些能力，不阻断无关查询。

默认 doctor 不发送官方业务/OAuth 请求、不写用户状态、不安装依赖、不改宿主配置、不迁移数据、不创建诊断包；网络连通、正式宿主加载和合成业务验证使用单独明确的检查动作。普通业务请求不重复运行 doctor 或扫描所有工具。自动化测试只使用合成目录、凭据替身和假官方端点。

## 9. 千川批量预检技术实现

### 9.1 领域身份

保留当前代码已实现的稳定计划组身份，不因这次重构改变现有绑定键。规范化结构不含业务日：

```text
identity = {
  advertiser_id, template_id, creator_id,
  product_ids: sorted_unique(product_ids), plan_type, business
}
group_id = "qcg_" + hex(sha256(canonical_json(identity)))
```

规范化、字段编码和空值语义必须沿用当前 `internal/domain/qianchuan/batch.go` 及其夹具；不能用字符串拼接、加入业务日或变更前缀后仍声称绑定兼容。渠道在所属千川表/用例中隔离；其他渠道采用自己的身份合同。

计划绑定唯一键：

```text
(business_date, group_id)
```

规则：

- `items[]` 每行携带自己的 `work_url`、`plan_type` 和 `business`。
- 同一达人可以有多个计划组。
- 当前作品预检采用 `unique_existing`：无绑定且完整查询找到唯一兼容目标时，固定该计划 ID 进入追加预览；确认提交并重新核对成功后保存绑定。已有绑定优先核对原目标，多个候选或跨组占用不自动选择。
- 绑定身份、模板摘要、素材集合或业务日漂移时返回阻断，不猜测。
- F2 只提供身份提示；官方授权、作品归属、商品匹配和计划确认必须由官方 API 完成。

### 9.2 预检执行图

```text
normalize single/batch items + load templates
                   │
       shared short-link / F2 metadata pass
                   │
       route by template and advertiser
                   │
    parallel account scopes + immutable leases
          ├── shared complete plan inventory
          └── per-creator authorization / work / product checks
                         │
          each creator joins its required facts
                         │
        candidate UID / full product set + material diff
                         │
       create / append / already present / blocked
                         │
       separate child snapshots + final batch summary
```

约束：

- 读取任务并发默认 8、最大 10；路由子批次共享命令级容量。同账户固定最多两个读取在途，短状态锁控制派发间隔和共享冷却；旧版排他请求及写请求与新读取互斥。这些是本地限制，不是 QPS 或官方配额承诺。
- F2 先于账户查询；账户范围明确后共享一次不可变租约。缓存只补身份字段，不跳过本批 F2，也不代替官方事实；不同授权范围不共享官方事实。
- 商品全域列表在锁定 SDK 中没有 UID 筛选，按账户和授权范围完整扫描一次，再按每段最多 200 个并行核对全部必要详情。清单仍限制在 100 页、10000 条计划内；数字 UID、完整商品集合或分页覆盖未核实，以及任一段查询失败时，不能决定新建。
- 明确目标商品的作品只查询该商品；未明确目标仍核验模板候选集合。必要查询失败时不能采用部分商品集合。
- 当前快照 v3 显式保存选择策略；旧 v2 保留 `bound_only` 语义，v1 只读，旧执行器拒绝 v3。工具仍返回整批汇总，统一 V3 Operation/可信审批和宿主执行接入范围见第 6–8 节。
- 预检不持有广告主写锁；提交时才获取写锁并重新核对计划和素材差集。
- 千川预检按 `Asia/Shanghai` 业务日，最多有效 30 分钟且不跨业务日；不是到期删除审计数据。过期后创建新 operation version，旧内容保持只读，不能续期复用旧审批。
- 结果按组保存成功、阻断、跳过、失败和未知，不因单组失败丢失其他组事实。
- 每个官方失败记录阶段、脱敏 endpoint、耗时和 request_id。

## 10. 升级、分发与运行时方案

### 10.1 Runtime 版本固定

目标方案取消业务调用期间的 Runtime 热切换：

- 一个进程启动后固定一个不可变 Runtime 版本。
- 一个 Operation 记录 `runtime_version` 和 `operation_schema_version`。
- 新版本只影响新进程和新操作。
- 操作内容、审批摘要和派发身份不可变，执行进程固定 Runtime。跨进程恢复必须明确验证 operation schema、持久化读写版本和执行语义；不兼容时阻断，不能默认新版连只读对账也一定理解旧状态。
- 兼容的只读/对账可以由受验证的版本执行；恢复在线派发需要重新验证执行条件、执行权和审批有效性，不能仅因旧版本仍在磁盘就继续。
- 更新、回滚和清理由安装层执行，不由每次业务调用触发。
- 清理与保留由安装层管理；只保留活动进程和仍需恢复的未结操作所需版本，终态审计记录不永久 pin 二进制。无法安全恢复时给出用户处理路径，不能无限积累版本。

在该目标方案正式替代现有稳定代理前，现有代理不得被直接删除；先完成兼容矩阵和迁移验收。

### 10.2 发布产物

- 使用标准 Go 多平台构建流程与 GitHub Actions 生成 macOS arm64/amd64、Linux arm64/amd64、Windows amd64 产物；依赖、工具链与构建输入固定，CI 执行合同、故障和各目标实际运行测试，不把编译通过等同于宿主兼容。
- 每个版本同时生成版本、源码提交、平台、SHA-256 和构建来源信息；宿主包由声明式输入/模板和必要专属代码组装，校验 Skill/工具映射及包内路径，不动态拉取未固定 Runtime。校验和只证明字节完整性，来源证明不等于安全认证或已证明跨环境逐字节可复现。
- Host 包负责各自支持的 Plugin/Bundle/Skill/Tool/审批/配置和生命周期接入，并绑定已验证 Runtime；可复用同一 Skill/MCP 资源，但不复制业务实现，也不把一份 `.mcp.json` 当成通吃所有宿主的插件。
- 同一产品版本的各包从同一固定源码构建，描述 `adapter_version`、宿主范围、`capability_contract_version`、Runtime 摘要和可读写数据/操作格式范围。共享代码、Skill 和可校验字节产物，不要求所有宿主共享安装目录或常驻进程；沙箱要求时各包内置同源产物。适配包独立兼容，不要求强行共享一套 SDK。
- 新分发目录必须在目标宿主实际安装成功、资源封装和版本发现验证通过后才切换；不得先删根清单或全部内置二进制再补安装方式。
- 安装/更新失败保留原可用版本。
- Manifest、操作格式、SQLite schema、MCP/CLI 合同分别版本化，不能只用一个产品版本号表达全部兼容性。
- 未来 Manifest 格式升级必须先发布能读取旧格式的桥接版本；不得直接让旧 Runtime 遇到未知字段而失效。

### 10.3 宿主管理优先与受控安装辅助

优先使用宿主自身的 Plugin/Bundle/Connector/Skill 管理流程；配置描述用于生成其正式格式，不默认向个人规则、启动文件或全局配置注入内容。只有目标入口需要手工注册时，才提供小型受控辅助，用户显式选择宿主和作用域。

- 安装前展示将修改的路径、Ocean Watch 注册键、固定 Runtime 与所选工具集/模式；不把自动检测到目录解释为同意修改。
- 修改前校验格式和当前 revision，提供受保护的变更前副本并保留其他项目的键；如宿主配置可能含第三方秘密，副本按秘密级别限制权限，不输出到聊天、日志或普通诊断包。
- 重复安装应可识别本项目已有注册，不生成多份服务；遇到用户改动、同名冲突、未知格式或不支持字段时停止并报告，不覆盖整个配置文件或扩大权限。
- 卸载只撤销可核对属于本包且未被用户改写的注册和独占资源；不删除共享业务数据、凭据或其他宿主仍需的产物。并发及冲突回退按第 3.5、11.3 节处理。
- 构建安装辅助不等于获准操作真实 profile；开发验证使用隔离合成配置，实际安装、更新、迁移和发布仍需对应授权。

## 11. 迁移方案

### 11.1 数据迁移步骤

下列 D0～D6 是数据切换步骤，不与第 12 节的 M0～M5 项目里程碑混用：

```text
D0 只读盘点与冻结一致输入
  → D1 新 SQLite + Repository
  → D2 导入账户/授权引用/模板/绑定及非秘密配置
  → D3 旧预检、执行记录和未结项分类导入
  → D4 逐宿主切换查询入口
  → D5 逐宿主切换已通过验收的千川纵向闭环
  → D6 分批切换其余写入，按能力退役旧入口
```

D1～D3 可以使用合成环境完成；真实数据迁移仍需单独授权。未决官方派发不能一概导为只读历史：必须保留未知/待对账身份并阻止重复新建，只有已结记录和失效预检可作为纯历史导入。

### 11.2 迁移规则

1. `setup migrate --check` 只读检查，不修改用户数据。
2. `setup migrate --backup PATH` 创建受保护备份并记录 checksum。
3. 对冻结的一致输入记录迁移 ID、原路径、格式、checksum、目标版本和对象映射；相同输入重复运行不重复导入。内容已变化时必须报增量/冲突，不把旧 checksum 当作整库已经迁移的证据。
4. 未知字段、损坏 JSON、冲突绑定或凭据引用缺失默认阻断。
5. 系统凭据不导出到备份；只迁移 credential reference 和 revision。
6. 旧状态在新版本验证完成前保留，不能用回滚代码掩盖远端已经发生的写入。
7. 迁移真实数据前先停用相关旧入口并确认旧写者退出，取得状态根级迁移锁；冻结后再创建备份与导入。旧二进制不会自动遵守新写者锁，因此必须通过经验证的启动层桥接或停机切换隔离；无法保证旧写者不运行时阻断迁移，不将“增加新锁”当成充分保护。
8. 旧预检快照不自动变成新版本可提交操作；需要重新验证或明确标记只读历史。

### 11.3 逐宿主切换与回退

- 首批 Codex、Claude、Harness、豆包和 CLI 分别登记旧入口、目标适配包、状态根、Runtime 绑定及配置迁移；必须先在隔离目录验证，不操作真实 profile 来冒充测试。后续 WorkBuddy 是新增适配器，默认复用已迁移状态，不复制一套账户和授权；仅在发现真实旧接入数据时另行定义其迁移。
- 每个适配器先验证安装/发现/查询，再验证预检、可信审批或 Broker、模拟执行与恢复；不以一个宿主成功替代其他宿主。
- 同一作用域替换注册时清理本次旧注册，不能同时出现两份独立轮换 Token 的服务；多宿主有意共存则共享同一授权状态、锁和操作库。
- 安装回退只恢复包和入口，数据降级需另行证明兼容。SQLite/操作格式不兼容时不能启动旧写者；提供明确只读历史或备份恢复方案，不通过回退抹掉已发生的官方动作。
- 每项旧业务能力在替代入口有等价证据前保留，缺少可信审批导致写入模式变化须明确告知替代入口，不能默默删功能。

### 11.4 未结操作的逐项迁移合同

旧 Journal 不能通过重新生成新 `dispatch_key` 来冒充从未派发的操作。迁移器为每个源操作和子项保存可核对映射，至少包括：

| 映射部分 | 必须保存或明确未知的信息 |
| --- | --- |
| 来源 | 源根/相对记录 ID、文件 checksum、原 Journal/快照 schema、来源 Runtime 完整 build/摘要；源记录缺失版本时记为 unknown，不推断填成当前版本 |
| 目标 | 确定性目标 `operation_id + version`、目标 schema、迁移 ID 与解码器版本、每个子项的新旧 ID 映射；同一源映射重复运行不得新建操作 |
| 派发证据 | 原 operation/dispatch key、官方 request/object ID、已发送/明确成功/未知状态、证据摘要；原字段缺失时显式标明，不能生成虚假的官方证据 |
| 恢复范围 | 可核对的渠道/广告主/组/目标范围、允许使用的只读对账合同与兼容 Runtime、当前恢复状态和解除条件 |

恢复决策固定为：

- 已结操作和过期预检作为只读历史，不继承旧用户确认生成新审批。
- 能完整映射的未结操作以只读待对账状态导入，**迁移后不自动续发旧在线请求**；先用新 Core 中已验证的旧格式解码/现有对账用例恢复官方事实。仅增加数据解码兼容，不恢复第二套广告业务执行器。
- 元数据不足、来源 Runtime 不明、格式无法安全解码或官方事实仍歧义时，进入 `quarantined_unknown`：保留证据，禁止在线恢复。能确定广告主时阻断该范围中可能重复的写入；能精确确定组/目标时才缩小阻断，不把不确定范围当作无冲突。
- 连渠道/账户范围都无法确定的未结记录阻断状态根的写入切换，允许人工核对和只读使用；不能清空记录后继续发布。
- 不要求启动旧 Runtime 去解析新数据库。若没有任何已验证恢复 Runtime/对账合同，保留隔离和阻断，提供人工处理路径；不可因清理了旧二进制而丢弃未结证据。
- 对账后若还有明确未派发工作，走新的预检/操作版本与新确认；新操作记录其源操作关系，并依据迁移阻断和官方现状排除重复，不以新键绕过旧未知项。
- 解除隔离必须保存官方读回或人工核对依据、时间、范围及允许动作。用户单纯点“重试”不能把未知转换成未派发。

验收必须覆盖：迁移后重启、缺少源版本、未知 request ID、同源重复导入、跨操作重复请求、不兼容 Runtime、隔离范围和解除隔离；没有完成这些用例不得切换在线写入。

## 12. 实施阶段

### M0：行为基线

交付：

- 现有 CLI/MCP/Skill/模板/报表/写入能力清单，映射到静态能力目录、业务工具集与最低执行模式，逐项记录保留或迁移方式；
- 当前状态文件与凭据引用清单；
- 合成数据 fixture 和故障矩阵；
- Codex、Claude、Harness、豆包和 CLI 分别形成宿主版本 × OS/架构 × 安装/查询/预检/审批模式/写入/升级/卸载支持矩阵；WorkBuddy 单列为“后续计划、未实现/未验收”，本期预留合同边界，不要求完成其安装接入。
- 按第 3.7 节建立声明式宿主描述、清单模板、共享 Skill/受控调用映射及各 Adapter 最小加载骨架；启动安装/卸载冲突和生命周期验证，不等业务全部迁完才开始宿主接入；无官方接口证据的能力明确缺失。

退出条件：现有营销、千川及星图已接入授权/账户能力均有保留、修正或显式迁移映射；没有未说明的口径和安全门禁。任何新宿主专属依赖均不进入业务核心。

### M1：基础设施

交付：

- `database/sql` + `modernc.org/sqlite` 精确版本与配套依赖锁定、SQL schema/迁移器及窄 Repository；落实逐连接配置、连接池上限、一致备份与恢复；
- Operation/Preflight/Approval/Dispatch domain；
- OS Credential Store 引用模型；
- 统一 ApprovalProvider/Verifier、首个宿主原生审批可行性结论和按需 Broker 最小闭环；有可验证原生通道才实现桥接，否则明确采用 Broker，不把某个尚不存在的 Host API 设为必达前提；
- 旧 JSON 状态只读导入器，以及合成配置下 doctor 的无凭据/无业务请求/无用户状态写入合同。

退出条件：多进程、崩溃、迁移中断、摘要篡改、来源伪造、防重放、Token 轮换中断和错误凭据测试通过；所选 SQLite 驱动/实际引擎及打包在五平台完成构建与运行验证，记录依赖版本、锁/checkpoint、备份恢复及资源成本，不改变单内核方案。

### M2：千川纵向闭环

交付：

- 结构化 `items[]`；
- 按 `(business_date, group_id)` 建立当日绑定；`business_date` 不参与 `group_id` 计算；
- 预检操作持久化；
- 通过统一 Provider 使用已验证宿主审批或 Broker 兜底；
- 创建/追加的有界逐项派发、对账和未知恢复；执行模式、工具集和账户限制在 Core 内强制校验，不因隐藏写工具就跳过用例检查。

退出条件：至少一个宿主适配器和独立 CLI 的千川纵向闭环通过成功、部分成功、未知、过期、漂移、重复领取、进程重启用例；这只允许阶段预览，不表示其他宿主已兼容。

### M3：查询和只读迁移

交付：

- 账户、授权、模板、商品、计划和报表 Application Service；
- 统一结果事实/错误合同与 MCP、CLI、各宿主展示映射，保留必要列和口径，不强制相同 UI；
- 统一错误码、分页和 doctor 诊断；验证工具集过滤、默认 `plan`、CLI/MCP 越权拒绝及共享 Skill 映射，不机械复制每条 CLI 为工具；
- 营销与千川只读能力迁移。

退出条件：新旧实现对照测试通过，口径和失败语义无未说明差异。

### M4：写入迁移

交付：

- 营销模板/计划写入；
- 千川计划设置、素材和删除；
- 所有在线业务写入统一经过 Operation + Approval + Reconcile；本地模板/账户编辑按明确风险等级预览和确认，不伪装成官方写入，也不强制调用官方对账。

退出条件：对比 M0 能力清单，所有高风险用例在目标宿主和 CLI 中共享门禁；未知结果不盲重试，星图等未扩展业务不被静默删除或虚报支持。

### M5：宿主与开源交付

交付：

- Codex、Claude、Harness、豆包和 CLI 各自 Host-native Adapter 与 Capability 合同；
- 多平台安装与显式更新，宿主管理优先；手工辅助的预览、保留他项、重复安装、冲突中止及受保护备份测试；
- GitHub Actions 同源固定构建、各宿主组包、来源证明、校验和及迁移文档，不新增常驻更新平台；
- 合成开发环境、贡献指南、支持矩阵和用户测试报告。

退出条件：首批五个目标入口逐一完成第 13.5 节验收矩阵；每个声明正式支持的版本/平台组合都有证据，不能以“至少一组通过”代表整体完成。功能受限宿主明确支持级别和可信写入替代路径；若目标组合确实不可实现，先由用户确认范围调整，不静默排除。WorkBuddy 后续 WB1 不阻塞本期收口，但不能在发布说明中标为已支持。

M5 是收口阶段，不表示 Host Adapter 到此才开发；加载、原生审批可行性、安装和生命周期在 M0/M1 已启动并贯穿 M2～M4。

### WB1：WorkBuddy 后续接入（M0～M5 之后）

交付：WorkBuddy Connector + Skill 分发包、单个本地 MCP Server 配置、最低客户端版本、固定 Runtime 绑定、受支持平台/审批模式和使用文档；业务全部复用已完成的 Core，不再重构投放内核。

退出条件：按第 3.6 节接入并通过 HA-01～HA-08，至少与一个现有宿主做共享数据、刷新竞争、重复执行、独立卸载测试；验证长耗时预检和审批等待以操作状态返回、不无限占用 MCP 请求。原生审批未经验证时如实使用 Broker 或声明只读/预检支持。未通过不可标为正式兼容；本节不授权安装真实 WorkBuddy、修改其配置或发布连接器。

## 13. 验收标准

### 13.1 正确性与安全

- 不能通过模型参数、伪造 approval token、过期 token、错误 digest 或重放 token 触发写入。
- 同一 `dispatch_key` 不重复派发已确认成功项。
- 官方超时进入未知或对账中，不自动变成失败。
- 多进程只能有一个执行租约持有者。
- Token 不出现在 SQLite、日志、MCP/CLI 输出和诊断包。
- F2 结果不能单独证明授权、作品归属或商品匹配。
- 对隐藏工具的直接请求、越过有效模式、跨业务工具集和账户执行、未知配置值均默认拒绝；`core` 通用操作入口不能成为绕过点。查询/预检允许的本地副作用与 Token 刷新分别声明，不冒称数据库完全只读。

### 13.2 数据与恢复

- SQLite 迁移可中断、重复执行并验证 checksum。
- 迁移失败保留原状态和备份；旧记录按第 11.4 节逐操作映射，缺来源版本或官方证据不补造；隔离未知范围能阻断重复新建。
- 重启后能查询未完成操作、未派发项、明确成功项和未知项；导入的旧未结请求不自动续发，安全对账后仍需新的预检和确认。
- 不兼容 schema、操作格式和 Runtime 默认阻断写入。
- 已确认操作不会因新版本启动而改变目标或 payload。
- 所选驱动/配套依赖、实际 SQLite 引擎、逐连接设置和资源成本有五平台证据；不存在数据库时 doctor 不创建数据库。进程退出后未派发工作保持待处理，在途未知项保留并先对账，不虚报后台继续执行。

### 13.3 用户体验

- 干净环境从安装到无凭据 doctor 通过不超过 10 分钟，实际测量条件必须记录。
- 用户不需要编辑状态文件或查找插件缓存；doctor 能区分资源存在、宿主发现与未验证，说明工具集、模式、审批和可选依赖限制，不读取秘密或自动修改配置。
- 每次写入前能看到广告主、对象、动作、关键差异、风险、有效期和摘要。
- 失败结果能回答：失败阶段、哪些已成功、哪些未知、下一步是什么。
- 至少 5 名未参与实现的用户完成首次查询和一次合成批量预检，其中至少 4 人不需维护者手工修改配置。

### 13.4 开源质量

- 核心业务测试默认不访问真实网络、凭据、业务配置或 Host profile。
- Domain/Application/Adapter/Transport 与各 Host Adapter 有明确测试和依赖边界；Core 无宿主包、SDK、Profile 或会话对象引用。
- 发布产物可由源码和固定构建流程重建并核对摘要。
- 支持矩阵中的每个“正式支持”组合都有实际验收证据。
- 文档、代码、Schema、Skill 路由和更新日志不出现已实现状态漂移；DTO/Schema/注册清单及跨入口 fixture 的自动校验防止合同分叉。
- 共享 Skill 与宿主调用映射分离；参考项目不是业务运行前置依赖，不新增通用 Host SDK、工作流 DSL 或第二套业务实现。

### 13.5 多宿主逐项验收矩阵

Codex、Claude、Harness、豆包和 CLI 各自列出目标版本、OS/架构、适配包版本、Runtime/Capability 版本、审批模式和验证证据，逐行执行以下门禁。`不提供该扩展点` 只能用于真实不存在的可选宿主接口，不能代替核心业务验收；环境不具备时标记未验证，不虚报通过。

| 编号 | 检查项 | 通过条件 |
| --- | --- | --- |
| HA-01 | 干净安装与发现 | 通过实际宿主支持方式安装；Skill/Tool/CLI 可达，路径不依赖 cwd；共享 Skill 映射正确，无重复注册及包外资源；手工辅助预览变更、保留他项，重复安装和冲突中止可验证 |
| HA-02 | 启停、重载与卸载 | 按第 3.5 节实例归属关闭独占连接/进程/端点；异常退出后不留可误用资源；重新挂载可用；最后一个消费者卸载也保留用户数据 |
| HA-03 | 业务与展示合同 | 按第 8.4～8.6 节比较进程内/MCP/CLI 同 fixture 的身份、版本、超时、取消、错误、工具集/模式和业务事实；doctor 无秘密/无用户状态变更，中文、长 ID、分页、非终态与部分失败不丢列或误报成功 |
| HA-04 | 原生审批或 Broker | 确定并验证实际来源；自动放行、未知回调、伪造 provider、错误摘要、超时、拒绝和重放不能产生权限；回执秘密不经过模型 |
| HA-05 | 取消和恢复 | 插件/会话关闭可停止未派发项；已派发未知项保留并能对账；不把宿主卸载称为官方回滚 |
| HA-06 | 多宿主共存 | A/B 各自运行并查询/刷新授权/竞争同一操作 → 卸载 A → B 继续/重载 → 卸载最后实例；再测并发卸载和异常退出；无误杀、重复派发、孤儿进程或共享数据删除 |
| HA-07 | 更新与回退 | 包与 Runtime 版本可追溯；旧操作内容不变；不兼容数据/操作格式阻断；回退安装不启动不兼容旧写者 |
| HA-08 | 能力缺失 | 不存在的 Settings/事件/审批明确缺失；可用 fallback 在该版本实际验证；无可信审批时只读/预检，不静默使用模型确认 |

- Harness 验收继续使用已安装桌面版及内置 Runtime 做隔离测试，不安装独立 npm/CLI 宿主，不操作真实 `desktop` profile 或兼容豁免；真实安装操作须另有明确授权。
- 对宿主当前并不支持的 OS/架构，标记该组合不适用并说明依据，不承诺五个平台各跑全部宿主。
- 进入本期整体目标版时，首批五个入口均有明确结论，全部声明正式支持组合的核心能力通过。只有一个组合通过可以发受控预览，不能宣布多宿主重构完成。
- WorkBuddy 在 WB1 独立执行同一 HA-01～HA-08 矩阵，额外检查单连接器/单 Server、MCP/CLI 业务通道不混用、字段最低版本、启动路径、MCP 断开和共享授权；“后续必接”不等于“当前已兼容”。

## 14. 当前项目到目标方案的迁移映射

| 当前部分 | 目标归属 | 处理方式 |
| --- | --- | --- |
| `internal/application` | Application Use Cases | 保留业务规则，改为依赖 Operation/Repository ports |
| `internal/domain` | Domain Core | 抽出 Operation、Approval、Dispatch 和稳定业务身份 |
| `internal/adapters/oceanengine` | Official API Adapter | 保留 SDK 隔离，转换为稳定 DTO |
| `internal/adapters/filesystem` | SQLite/文件 Adapter | 配置文件保留；事务状态迁移 SQLite |
| `internal/adapters/credentials` | Credential Adapter | 保留 OS 安全凭据实现 |
| `internal/adapters/python` | F2 Adapter | 保持只读公开元数据边界 |
| `internal/mcpserver` | `transport/mcp` | 只做合同、解码、Presenter 和 Application 调用 |
| `internal/cli` | `transport/cli` | 只做参数、JSON envelope、退出码和交互入口 |
| `internal/runtimeupdate` | 安装/发布层 | 逐步退出业务热切换职责，保留迁移期兼容能力 |
| `.codex-plugin` / `.claude-plugin` 清单及 marketplace | `hosts/codex` / `hosts/claude` 的构建输入 | 生成目标版本可识别的包，先验收安装路径，不提前删除根部兼容清单 |
| `harness/index.mjs`、`config.mjs`、patch 与根 npm 包 | `hosts/harness` 专属适配 | 保留可用 MCP/Skill 提供方组合；生命周期/审批只依实际接口扩展，Core 不依赖 Cordis |
| `scripts/install-doubao.sh` | `hosts/doubao` 的安装适配 | 先验证实际 Skills 接入；不杜撰通用豆包插件 SDK |
| 后续新增 WorkBuddy 接入 | `hosts/workbuddy` 构建输入与连接器产物 | 复用 Skill/Capability/Core，采用本地 MCP，不增加第二套业务或凭据存储；WB1 前仅为规划路径 |
| `skills/*/SKILL.md` | 共享意图合同 + 受控 Host 路由覆盖 | 不承载业务规则，不自行签发审批；适配器映射真实用户交互 |
| `OperationJournal` | SQLite operations/events | 迁移为持久化操作、子项、派发和对账模型 |
| `qianchuan plan-bindings.json` | `plan_bindings` 表 | 显式迁移，保留 group_id 与业务日语义 |

## 15. 必须停止并重新评审的情况

出现以下任一情况，不得继续“先实现再说”：

- 某宿主的原生审批与 Broker 均无法验证，但产品仍要求在该入口在线写入。
- 需要凭空假设宿主 Settings/Approval/Session API，或把 Harness 框架当成其他宿主依赖才能接入。
- 官方结果在必要对账后仍有歧义，而实现准备把未知项自动重放或标记为确定成功。官方缺少幂等/查询能力本身不被掩盖，允许保留未知并人工处理。
- SQLite 与旧进程并存会产生无法阻止的双写。
- 迁移无法保留授权引用、模板、绑定或操作事实。
- 新架构需要第二套广告业务实现才能兼容旧入口。
- 为了保留热升级而允许已确认操作被不同 payload 或不同 schema 接管。
- 为追求性能引入无法测量、无法恢复或无法解释的并发写入。

## 16. 文档与状态规则

- 本文描述目标方案，不代表当前代码已经实现。
- 已实现内容必须同步到代码、测试、正式使用文档和 `CHANGELOG.md`。
- 架构改变时先更新本文和对应实施计划，再修改代码。
- 所有“正式支持”“已兼容”“迁移完成”“安全确认”都必须有验收证据。
- 本文不授权真实账户操作、真实数据迁移、插件安装、提交、推送、Tag、Release 或 Marketplace 发布。
- 当前现状参考：[docs/architecture/overview.md](../architecture/overview.md)、[docs/guides/configuration.md](../guides/configuration.md)、[docs/reference/cli.md](../reference/cli.md)、[CONTRIBUTING.md](../../CONTRIBUTING.md)、[SECURITY.md](../../SECURITY.md)。
- 当前清单/入口参考：[.codex-plugin/plugin.json](../../.codex-plugin/plugin.json)、[.claude-plugin/plugin.json](../../.claude-plugin/plugin.json)、[harness/index.mjs](../../harness/index.mjs)、[scripts/install-doubao.sh](../../scripts/install-doubao.sh)。

### 16.1 外部机制核实与适用边界

资料核对日期：2026-10-21。以下记录仅支持各自描述的扩展/存储机制或开源设计方法，不证明用户本机版本已兼容，也不是 Ocean Watch 目标审批方案的安全认证。v0.5 本轮重新读取 S5、S6 及新增 S8～S12；S1～S4、S7 保留此前资料入口，实施前仍须核对目标版本。未核实豆包通用插件/审批 SDK，不写入推测 API；上游 main/master 和包文档会变化，不将其当作固定发布合同。

- **[S1]** [OpenAI 插件打包](https://developers.openai.com/plugins/build/plugins)：描述便携插件与 Codex 兼容清单、Skill/MCP 资源；采用哪一格式由目标 Host 版本验收决定。文档可用不等于旧版桌面已经支持全部新字段。
- **[S2]** [Claude Code 插件清单参考](https://code.claude.com/docs/en/plugins-reference)：说明 Plugin 清单、MCP 注册及资源路径等；不据此推定存在可供 Core 验证的原生用户审批回执。
- **[S3]** [DeepSeek Harness 打包与安装](https://deepseek-harness.github.io/deepseek-harness/develop/basic/publish)：说明 Bundle 与 Profile 的区别；只是 Harness 适配器的依据，不作为跨宿主架构基座，教程 CLI 不是安装独立宿主的授权。
- **[S4]** [SQLite Online Backup API](https://www.sqlite.org/backup.html)：在线一致性备份机制；具体驱动支持和跨平台成本在 M1 验证。
- **[S5]** [SQLite WAL](https://www.sqlite.org/wal.html)：WAL 的同机与附属文件要求；不把复制单个 `.db` 当作完整在线备份。
- **[S6]** [WorkBuddy 连接器开发规范](https://open.workbuddy.cn/docs/connector)：2026-10-21 读取，说明 MCP + Skill / CLI + Skill 二选一、一个连接器一个 MCP Server、本地 stdio、连接器文件和字段最低版本；未据此认证桌面沙箱、Go 打包或原生投放审批能力。
- **[S7]** [WorkBuddy 插件用户说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Plug-In)：2026-10-21 读取，说明插件可组合 Skill/MCP 等组件及启停更新；不是可直接照抄的开发者清单或可信审批 SDK。

- **[S8]** [modernc.org/sqlite 包文档](https://pkg.go.dev/modernc.org/sqlite)：无 CGO 的 `database/sql` 驱动、五个目标 OS/架构、配套 `libc` 版本要求及连接级配置；据此选择驱动，依赖已按第 3、5.2 节固定；具体平台的性能、崩溃及备份恢复仍须在 M1 验证，不以依赖锁定或交叉构建代表通过。
- **[S9]** [Superpowers 跨宿主移植说明](https://raw.githubusercontent.com/obra/superpowers/main/docs/porting-to-a-new-harness.md)：共享 Skill、薄工具映射及宿主自己的安装机制；只借鉴职责分工，不照抄其提示注入行为或当作宿主认证。
- **[S10]** [Context7 客户端配置描述](https://raw.githubusercontent.com/upstash/context7/master/packages/cli/src/setup/agents.ts)：`AgentConfig` 集中表达 MCP 配置、Skill 目录和检测路径；只借鉴声明式适配，不照抄 Node/凭据传递和配置改写策略。
- **[S11]** [GitHub MCP Server](https://raw.githubusercontent.com/github/github-mcp-server/main/README.md)：多客户端配置、工具集选择、单工具和只读模式；这些不等于逐操作人工审批或账户授权边界。
- **[S12]** [Playwright MCP](https://raw.githubusercontent.com/microsoft/playwright-mcp/main/README.md)：MCP 与 CLI + Skill 的场景取舍，并声明自身不是安全边界；不作为 Ocean Watch 执行投放或可信确认的依赖。

### 16.2 剩余阶段门禁

实际宿主可信审批链、Broker 受保护交互、所选 SQLite 驱动及配套版本/实际引擎/多平台资源成本、模式与工具集强制校验、受控安装及宿主发现，都必须在对应 M0～M5 阶段有验证证据；基础安全与 SQLite 准入在 M1 关闭，WorkBuddy 仍在后续 WB1 单独验收。这些不是已完成事项。
