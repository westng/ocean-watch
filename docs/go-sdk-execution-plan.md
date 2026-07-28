# Go SDK 实施任务书

本文把[目标架构](go-sdk-target-architecture.md)和[迁移矩阵](go-sdk-migration-matrix.md)拆成可领取、可排期、可验收和可回滚的工程任务。任何团队成员都应能仅凭本文和关联文档实施，不依赖历史对话。

## 1. 项目章程

### 1.1 交付目标

将 Ocean Watch 的确定性运行时迁移为基于官方 Go SDK `v1.1.92` 的本地模块化单体，并保持现有用户与 Skill 合同。迁移采用按命令绞杀方式；Python 在兼容期作为受控回退，不做一次性替换。

### 1.2 团队角色

| 角色 | 代号 | 责任 |
| --- | --- | --- |
| Maintainer | MT | 最终范围、预算、发布与破坏性决策 |
| Architecture Owner | AO | 依赖边界、ADR、阶段 Gate 和技术例外 |
| Runtime Lead | RL | Go CLI、Application、状态、并发和 contract runner |
| Marketing Adapter Owner | MA | 营销 SDK Adapter、报表和计划事务 |
| Qianchuan Adapter Owner | QA-API | 千川 SDK Adapter、报表、批量计划和素材事务 |
| Auth/Platform Owner | AP | OAuth、凭据、Token、账户发现和平台兼容 |
| Quality Owner | QO | fixture、合同测试、故障注入、验收证据 |
| Security Owner | SO | 威胁建模、脱敏、供应链、凭据和签名评审 |
| Release Owner | RO | 多平台构建、Plugin launcher、canary 与回滚 |
| Skill Contract Owner | SCO | 自然语言路由和 `presentation` 合同不回归 |

同一人可以兼任多个角色，但任务的实现者不能单独批准自己的阻断级验收。涉及凭据、线上写入和发布的 Gate 至少需要第二位审批人。

### 1.3 估算口径

- `1 人日` 为一个有完整开发环境的工程师一天有效工作量，含单元测试和代码评审修改，不含等待外部审批。
- 估算按熟悉当前 Python 代码和 Go 的工程师计算；首次接触仓库增加 20%–30%。
- 建议 3–4 人并行，规划周期约 10–14 个工程周、131–153 人日，之后保留两个正式版本观察期。
- 写链路真实 canary、应用权限审批或官方接口不稳定造成的日历等待不计入人日。

## 2. 工作规则

### 2.1 Definition of Ready

任务进入开发前必须具备：

1. 明确 Task ID、Owner、依赖、对应命令/endpoint 和验收 ID。
2. 官方 SDK Service 和官方文档已核对；若无生成 Service，先批准 `CommonApi` ADR。
3. 测试 fixture 使用占位数据，不包含真实账户、Token、作品链接或动态 MCP URL。
4. 涉及当前行为时已有 Python 黄金基线；涉及写入时已有结果未知对账设计。
5. 涉及状态写入时已说明锁粒度、原子替换、崩溃点和 Python 兼容方式。

### 2.2 Definition of Done

每个任务完成必须同时满足：

- 实现、单元测试、Adapter 合同测试和 CLI 黄金测试合入。
- `go test -race ./...`、静态检查和适用的验收用例通过。
- 没有 SDK 类型越过 Adapter；没有新增动态 endpoint 或敏感日志。
- 对应迁移矩阵状态和文档已更新。
- PR 记录行为变化、请求数量变化、安全影响、证据路径和回滚方法。
- 切换默认运行时前已完成 `Shadow -> Go canary -> Go default` 三个状态。

### 2.3 PR 与分支策略

- 每个 PR 聚焦一个 Task 或同一 Gate 下紧密相关的小任务，不混入无关重构。
- Adapter PR 在合同测试完整前不得同时切默认路由。
- 运行时路由、状态 Schema、SDK 版本和发布资产必须由独立 PR 修改。
- 禁止把真实响应或验收 evidence 提交到 Git；CI evidence 使用受控 artifact。
- 所有迁移分支都可从当时的 `main` 构建完整 Python 回退路径。

## 3. 里程碑总览

| 阶段 | 目标 | 估算 | 入口条件 | 出口 Gate |
| --- | --- | ---: | --- | --- |
| P0 | 冻结合同、风险与可行性 | 15–17 人日 | 本文批准 | G0 Baseline Ready |
| P1 | 建立 Go 核心与迁本地命令 | 23–25 人日 | G0 | G1 Local Parity |
| P2 | 迁 OAuth、账户同步和账户报表 | 19–21 人日 | G1 | G2 Identity & Accounts |
| P3 | 迁全部只读业务接口和报表 | 22–27 人日 | G2 | G3 Read Paths |
| P4 | 迁计划创建、更新、批量与删除 | 36–43 人日 | G3 | G4 Write Paths |
| P5 | 多平台装配、canary、发布和回滚 | 16–20 人日 | G4 | G5 General Availability |

P2 的 Adapter 工作可在 P1 后半段开发，但不得在 G1 前切流。P4 可提前做纯领域设计和 fixture，不得在 G3 前进行真实写 canary。

## 4. P0：合同与可行性基线

### `P0-01` 冻结 SDK 与依赖证据

| 属性 | 内容 |
| --- | --- |
| Owner | AO；SO 审批 |
| 估算 | 2 人日 |
| 依赖 | 无 |
| 交付物 | SDK pin/commit/license 记录、核心包编译日志、依赖与漏洞初始报告 |
| 执行 | 在隔离目录获取 Tag `v1.1.92`，验证提交 `3b0bab...`，运行 `go test . ./api ./config ./middleware ./models` |
| DoD | 版本可复现；上游 examples 失败不被误报为本项目失败；风险登记与目标架构一致 |
| 验收 | AC-114、AC-125 |

### `P0-02` 建立命令和输出黄金基线

| 属性 | 内容 |
| --- | --- |
| Owner | QO + SCO |
| 估算 | 6–7 人日 |
| 依赖 | 无 |
| 交付物 | `contracts/commands.yaml`、参数/默认值清单、退出码清单、JSON Schema、Markdown 黄金文件、Python baseline capture 工具、语义对话集、model-in-the-loop runner 与基线证据 |
| 执行 | 对[CLI 参考](cli.md)全部命令运行 `--help` 与 fixture；归一化时间、临时路径和随机 state 后保存黄金结果；在干净 Plugin 安装中按固定模型配置回放单轮/多轮语义用例和保密改写集 |
| DoD | 每个命令有 Owner、fixture、期望退出码；强制 Presentation 包含四列名单、账户报表完整区块、千川五列完成表；对话评测记录模型/Plugin/commit/工具轨迹且不依赖固定句式 |
| 验收 | AC-101–AC-105、AC-128 的基线部分 |

### `P0-03` 状态、锁和凭据兼容调查

| 属性 | 内容 |
| --- | --- |
| Owner | AP + RL；SO 审批 |
| 估算 | 3 人日 |
| 依赖 | P0-02 |
| 交付物 | 状态文件目录、Schema/未知字段清单、Keychain/DPAPI/Secret Service 命名清单、Python 锁行为探针 |
| 执行 | 仅用合成 `$CODEX_HOME` 生成所有状态变体；记录读、写、崩溃恢复和并发结果 |
| DoD | 能证明 Go 可原地读取；写锁不兼容项有明确修复任务；不接触开发者真实凭据 |
| 验收 | AC-106、AC-107 |

### `P0-04` 威胁模型与失败模式评审

| 属性 | 内容 |
| --- | --- |
| Owner | SO；AO、AP、RO 参与 |
| 估算 | 2 人日 |
| 依赖 | P0-01、P0-03 |
| 交付物 | 数据流图、STRIDE 清单、SDK 日志禁用检查、未知写入结果和供应链风险登记 |
| DoD | 每个高风险项有控制、测试、Owner 和关闭条件；Token/Secret 不进入日志的策略可测试 |
| 验收 | AC-109、AC-118、AC-125 |

### `P0-05` 发布与回滚 RFC

| 属性 | 内容 |
| --- | --- |
| Owner | RO + AO；MT 批准 |
| 估算 | 2–3 人日 |
| 依赖 | P0-01、P0-04 |
| 交付物 | 平台矩阵、版本身份规则、签名和 checksum 方案、运行时路由 manifest、缓存目录、离线/失败 UX、回滚 runbook、五平台 bootstrap 可行性原型 |
| DoD | 明确修改当前 Tag-only 发布模式；标准库 `run.py` 能验证所选签名，或 ADR 给出已验证替代 bootstrap；证明 Plugin 不执行未验证二进制；Python 回退不改用户状态 |
| 验收 | AC-124–AC-126 |

### G0：Baseline Ready

必须满足：

- `P0-01`–`P0-05` 全部完成。
- AC-101–AC-107 与 AC-128 Skill 语义部分的 Python/Plugin 基线证据已生成。
- SDK、状态兼容或发布方案不存在未分配 Owner 的高风险项。
- MT、AO、QO、SO、SCO 签字。

若 G0 未通过，停止 Go 实现；继续维护 Python，不创建半成品运行时入口。

## 5. P1：Go 核心与本地命令

### `P1-01` Go 模块化单体骨架与依赖门禁

| 属性 | 内容 |
| --- | --- |
| Owner | RL + AO |
| 估算 | 3 人日 |
| 依赖 | G0 |
| 交付物 | `go.mod`、目标目录、context 根、构建信息、import-boundary 测试、ADR 文件 |
| DoD | `domain`/`application` 无 SDK import；SDK 只存在 `internal/adapters/oceanengine`；`go test ./...` 通过 |
| 验收 | AC-114、AC-125 |

### `P1-02` CLI 兼容外壳

| 属性 | 内容 |
| --- | --- |
| Owner | RL + QO |
| 估算 | 4 人日 |
| 依赖 | P1-01、P0-02 |
| 交付物 | 完整命令树、参数绑定、`--version`、错误/退出码映射、stdout/stderr 约束 |
| DoD | 全部命令可解析；未迁命令由路由 manifest 交给 Python；`--help` 归一化后合同一致 |
| 验收 | AC-101、AC-102、AC-123 |

### `P1-03` JSON 与 Presentation 模块

| 属性 | 内容 |
| --- | --- |
| Owner | RL + SCO |
| 估算 | 3 人日 |
| 依赖 | P1-02 |
| 交付物 | 稳定 JSON envelope、错误模型、Markdown renderer、四列/五列/报表黄金测试 |
| DoD | stdout 只有一个 JSON；`presentation.required` 所有字段和 Markdown 字节级兼容 |
| 验收 | AC-102、AC-103、AC-105 |

### `P1-04` 配置、状态、锁和原子写

| 属性 | 内容 |
| --- | --- |
| Owner | RL + AP |
| 估算 | 5 人日 |
| 依赖 | P1-01、P0-03 |
| 交付物 | Config/State/Clock/Lock Ports，现有路径解析，未知字段保留，跨运行时并发测试 |
| DoD | Go/Python 并发不丢更新；故障注入不留下截断文件；读命令不产生写入 |
| 验收 | AC-106、AC-107 |

### `P1-05` 迁移本地领域命令

| 属性 | 内容 |
| --- | --- |
| Owner | RL；各领域 Owner 评审 |
| 估算 | 5–7 人日 |
| 依赖 | P1-02–P1-04 |
| 交付物 | `setup` 非 OAuth 命令、`accounts list/add/remove/enable/disable`、全部模板命令、`runs` |
| DoD | 每个命令 Shadow 结果等价；名单查询证明零网络和零 Token 读取；写配置合同不变 |
| 验收 | AC-103、AC-105–AC-107 |

### `P1-06` Contract runner 与差异报告

| 属性 | 内容 |
| --- | --- |
| Owner | QO + RL |
| 估算 | 3 人日 |
| 依赖 | P1-02、P0-02 |
| 交付物 | `cmd/contract-runner`、标准化规则、JSON/Markdown/退出码差异、JUnit 与 evidence 输出 |
| DoD | 单命令和全清单均可运行；任何非 allowlist 差异返回非零；报告通过 redaction scan |
| 验收 | AC-101、AC-102、AC-105 |

### G1：Local Parity

- P1 全部任务完成，所有本地命令标记 `Go default`。
- AC-101–AC-107、AC-123 在 macOS、Linux、Windows 通过。
- 账户名单自然语言 Skill 回放仍路由 `accounts list`，没有网络记录。
- Python/Go 对同一合成状态的读写往返和崩溃恢复通过。
- AO、RL、QO、SCO 签字。

G1 回滚仅切回 Python 本地命令；不得降级或重写用户状态。

## 6. P2：身份、账户同步与账户报表

### `P2-01` SDK Client Factory 与 Envelope Guard

| 属性 | 内容 |
| --- | --- |
| Owner | AP + AO；SO 审批 |
| 估算 | 4 人日 |
| 依赖 | G1、P0-01 |
| 交付物 | 不可变 Client profile、自定义 Transport、host allowlist、超时、禁止日志、业务 code guard、redactor |
| DoD | HTTP 200 + `code != 0` 必须失败；SDK dump 永久关闭；请求取消和响应大小限制生效 |
| 验收 | AC-109、AC-114、AC-123 |

### `P2-02` OS 凭据与 OAuth callback

| 属性 | 内容 |
| --- | --- |
| Owner | AP；SO 审批 |
| 估算 | 4–5 人日 |
| 依赖 | P2-01、P0-03 |
| 交付物 | 三平台 CredentialStore、现有命名兼容、本地 callback、state 校验、浏览器适配 |
| DoD | Go 可读取 Python 凭据；错误输出无敏感值；错误 state 在 Token 交换前阻断 |
| 验收 | AC-107–AC-109 |

### `P2-03` Token 生命周期和单飞刷新

| 属性 | 内容 |
| --- | --- |
| Owner | AP |
| 估算 | 3 人日 |
| 依赖 | P2-01、P2-02 |
| 交付物 | `ExchangeCode`、`RefreshToken` Adapter，TTL margin，授权锁，渠道隔离，一次性鉴权重放 |
| DoD | 50 个并发等待者只产生一次刷新；营销和千川永不串 Token；保存失败不覆盖旧凭据 |
| 验收 | AC-110 |

### `P2-04` 完整广告主发现与快照提交

| 属性 | 内容 |
| --- | --- |
| Owner | AP + QO |
| 估算 | 4–5 人日 |
| 依赖 | P2-01、P2-03 |
| 交付物 | 直客、Customer Center、EBP、代理、千川店铺 Adapter；统一分页器；候选快照事务 |
| DoD | 拉取全部声明页和角色；当前页重试；中间页失败保留旧快照；重复广告主映射确定 |
| 验收 | AC-111、AC-112 |

### `P2-05` 跨渠道账户聚合报表

| 属性 | 内容 |
| --- | --- |
| Owner | MA + QA-API |
| 估算 | 4 人日 |
| 依赖 | P2-01、P2-03 |
| 交付物 | Marketing `ReportCustomGet` Adapter、Qianchuan `ReportUniPromotionGet` Adapter、渠道口径汇总 |
| DoD | 两个账户只产生各自账户报表请求；千川零计划列表调用；单账户失败不取消其他账户；Presentation 等价 |
| 验收 | AC-104、AC-105、AC-113 |

### G2：Identity & Accounts

- `auth *`、`accounts report` 通过 Shadow 和 Go canary，状态改为 `Go default`。
- AC-104、AC-108–AC-114 全部通过。
- 真实只读 canary 至少覆盖一个营销授权和一个千川授权的刷新、全量分页和当天账户报表。
- canary 证据只保留脱敏计数和哈希，不保留真实 Token 或账户数据。
- AP、MA、QA-API、QO、SO 签字。

若出现账户缺失、部分快照覆盖、跨渠道 Token 或请求计划列表，立即切回 Python 并停止 P3 切流。

## 7. P3：只读业务接口与报表

### `P3-01` 营销素材与发现 Adapter

| 属性 | 内容 |
| --- | --- |
| Owner | MA |
| 估算 | 5–7 人日 |
| 依赖 | G2 |
| 交付物 | 视频、图片、达人授权、DPA、项目、单元、转化资产、出价、目标和官方行政区查询 Adapter |
| DoD | [迁移矩阵](go-sdk-migration-matrix.md)3.2/3.3 的读 endpoint 全覆盖；`discover cities` 通过官方行政区 Adapter；分页、批次和枚举映射有 fixture |
| 验收 | AC-112、AC-114 |

### `P3-02` 千川达人、商品、计划与素材读取

| 属性 | 内容 |
| --- | --- |
| Owner | QA-API |
| 估算 | 5–7 人日 |
| 依赖 | G2 |
| 交付物 | 授权达人、达人作品、商品、计划列表、详情、素材和素材报表 Adapter |
| DoD | cursor/page 全遍历；重复 ID、停滞 cursor 和矛盾总数 fail closed；详情临时 RPC 可重试 |
| 验收 | AC-112、AC-114 |

### `P3-03` 营销报表迁移

| 属性 | 内容 |
| --- | --- |
| Owner | MA + QO |
| 估算 | 4 人日 |
| 依赖 | P3-01 |
| 交付物 | `reports schema/custom/materials/plans`，Decimal 聚合，元数据关联，Presentation |
| DoD | 汇总使用全部页；展示 top 不改变总计；缺失指标不伪造零；金额精度与基线一致 |
| 验收 | AC-105、AC-112–AC-114 |

### `P3-04` 千川计划报表从 MCP 迁到 SDK REST

| 属性 | 内容 |
| --- | --- |
| Owner | QA-API + QO |
| 估算 | 4–5 人日 |
| 依赖 | P3-02 |
| 交付物 | `config/get`、`data/get`、plan-list 关联用例；MCP shadow comparator；现有 Presentation |
| DoD | 财务值只来自 `data/get`；list `stats_info` 不进入金额；相同日期范围；MCP 不做静默回退 |
| 验收 | AC-105、AC-113–AC-115 |

### `P3-05` 统一分页、重试、限流与性能验证

| 属性 | 内容 |
| --- | --- |
| Owner | RL + QO |
| 估算 | 4 人日 |
| 依赖 | P3-01–P3-04 |
| 交付物 | 分页状态机、读重试分类、jitter、`Retry-After`、授权/endpoint 限流器、请求计数测试 |
| DoD | 页 2 故障只重复页 2；批量链路没有 N×全量扫描；race 和 cancellation 测试通过 |
| 验收 | AC-112、AC-122、AC-123 |

### G3：Read Paths

- 所有只读命令完成 Shadow；关键报表完成真实只读 canary 后标记 `Go default`。
- AC-105、AC-112–AC-115、AC-122、AC-123 全部通过。
- 固定故障 fixture 证明无第 1 页重扫、无重复达人全扫描、无计划列表金额混用。
- 性能和请求数量不超过[验收计划](go-sdk-acceptance-plan.md)预算。
- MA、QA-API、RL、QO、SCO 签字。

## 8. P4：写入与批量事务

### `P4-01` 写操作安全框架

| 属性 | 内容 |
| --- | --- |
| Owner | RL + SO |
| 估算 | 4 人日 |
| 依赖 | G3 |
| 交付物 | dry-run guard、`--submit` capability、广告主锁、operation journal、unknown-write 状态机、reconciler 接口 |
| DoD | 无 `--submit` 时 SDK 写 Service 调用数为零；超时后不能进入通用 retry middleware |
| 验收 | AC-116、AC-118 |

### `P4-02` 营销项目/单元事务

| 属性 | 内容 |
| --- | --- |
| Owner | MA + RL |
| 估算 | 6–8 人日 |
| 依赖 | P4-01、P3-01 |
| 交付物 | 共享 `MarketingPlanExecutor`、项目/单元 Adapter、稳定业务键、对账和续建 |
| DoD | 所有营销创建命令共享事务；项目成功/单元失败可安全续建；未知结果不制造重复对象 |
| 验收 | AC-117、AC-118 |

### `P4-03` 营销批量与设置更新

| 属性 | 内容 |
| --- | --- |
| Owner | MA |
| 估算 | 5–6 人日 |
| 依赖 | P4-02 |
| 交付物 | 上传/达人批量调度、journal 恢复、项目/单元状态、预算、出价、ROI 更新 |
| DoD | 单广告主写串行；批量部分失败可重跑；每行结果决定退出码；不重复 Executor |
| 验收 | AC-116–AC-118、AC-121 |

### `P4-04` 千川全域创建与当日判重

| 属性 | 内容 |
| --- | --- |
| Owner | QA-API + RL |
| 估算 | 7–9 人日 |
| 依赖 | P4-01、P3-02、P3-05 |
| 交付物 | 千川创建 Adapter、当天计划扫描、候选详情确认、达人 + 商品 reconciler、创建对账 |
| DoD | `start_time/end_time` 固定当天；完成全部页；页失败不重启；未知创建结果查当天列表 + 详情，不重发 |
| 验收 | AC-116、AC-119 |

### `P4-05` 千川作品批量创建与追加

| 属性 | 内容 |
| --- | --- |
| Owner | QA-API + SCO |
| 估算 | 6–8 人日 |
| 依赖 | P4-04 |
| 交付物 | 55+ 链接批处理、owner hint、授权与商品复核、100 素材分块、追加对账、五列表格 |
| DoD | 授权列表只做一次宽扫描；同达人串行；已有素材不追加；完成响应固定五列且失败在表外 |
| 验收 | AC-105、AC-119、AC-120、AC-122 |

### `P4-06` 千川素材删除与计划设置

| 属性 | 内容 |
| --- | --- |
| Owner | QA-API |
| 估算 | 4 人日 |
| 依赖 | P4-01、P3-02 |
| 交付物 | CUSTOM material ID 删除、每批 100、状态/预算/ROI 更新、回读验证 |
| DoD | 删除要求 `--submit --confirm-delete`；不发送 `aweme_item_id`；成功须回读 `DELETED` |
| 验收 | AC-116、AC-121 |

### `P4-07` 写入故障注入和手工 canary runbook

| 属性 | 内容 |
| --- | --- |
| Owner | QO + SO + RO |
| 估算 | 4 人日 |
| 依赖 | P4-02–P4-06 |
| 交付物 | 每个写 endpoint 的断连/超时/部分成功 fixture，专用测试广告主 runbook，停止与清理步骤 |
| DoD | 自动测试证明不盲重放；手工 canary 需 MT + SO 双批准；证据不含真实业务数据 |
| 验收 | AC-116–AC-121、AC-127 |

### G4：Write Paths

- 全部写命令 dry-run 合同通过，写 fixture 的未知结果对账通过。
- 在专用、不可实际消耗的测试广告主完成最小 canary；每个对象创建后验证并暂停/清理。
- AC-116–AC-123、AC-127 全部通过，零重复计划、零跨账户写入、零未经确认写入。
- 每个写命令先进入 `Go canary`；至少一个受控观察窗口无阻断问题后才 `Go default`。
- MT、MA、QA-API、QO、SO、RO 签字。

出现重复对象、错误广告主、写请求自动重试或无法判定的状态污染时立即全量切回 Python 写链路；Go 保留只读能力用于诊断，不继续 canary。

## 9. P5：装配、发布与运营移交

### `P5-01` 可复现多平台构建

| 属性 | 内容 |
| --- | --- |
| Owner | RO + SO |
| 估算 | 4 人日 |
| 依赖 | G4 |
| 交付物 | 五个平台产物、`-trimpath` 构建、版本元数据、SHA-256、SBOM、签名、provenance |
| DoD | 两次干净构建摘要一致或差异有可审计解释；漏洞和许可证 Gate 通过 |
| 验收 | AC-124、AC-125 |

### `P5-02` Plugin launcher 与缓存

| 属性 | 内容 |
| --- | --- |
| Owner | RO + RL |
| 估算 | 4–5 人日 |
| 依赖 | P5-01、P0-05 |
| 交付物 | 平台选择、同产品版本下载、签名/checksum 校验、原子缓存、离线行为、两个标准库 `run.py` 兼容入口 |
| DoD | 不查 `latest`；错误平台/摘要/签名/版本身份拒绝执行；有当前支持的 Python 解释器但无 Go、Ocean Watch Python 包或 SDK 的干净机器可运行 Plugin |
| 验收 | AC-124–AC-126 |

### `P5-03` CI/CD 与发布文档改造

| 属性 | 内容 |
| --- | --- |
| Owner | RO + QO |
| 估算 | 3–4 人日 |
| 依赖 | P5-01、P5-02 |
| 交付物 | Go/现有 Python 质量矩阵、正式候选 workflow、六来源 G5 汇总、受限签字、sealed artifact、只消费 seal 的发布 workflow、验收 artifact、更新 `releasing.md`/`SECURITY.md`/README |
| DoD | Tag、product/plugin version、manifest、commit、候选、六个来源 run、最终摘要和签字身份一致；Tag Job 不重建候选或读取签名 Secret；仅受保护 publish Job 有写权限；发布前所有 Gate 自动阻断 |
| 验收 | AC-125、AC-128 |

### `P5-04` 升级、回滚与灾难恢复演练

| 属性 | 内容 |
| --- | --- |
| Owner | RO + AP + QO |
| 估算 | 3 人日 |
| 依赖 | P5-02、P5-03 |
| 交付物 | Python→Go、Go→旧 Go、Go→Python 路由演练；损坏缓存、离线、凭据不可用和中断恢复记录 |
| DoD | 回滚不修改用户配置/授权/模板；旧快照可读；损坏资产不会执行 |
| 验收 | AC-106、AC-124–AC-126 |

### `P5-05` 分批发布与支持移交

| 属性 | 内容 |
| --- | --- |
| Owner | MT + RO + SCO |
| 估算 | 2–4 人日，加两个版本观察期 |
| 依赖 | P5-04 |
| 交付物 | 内部/受邀/全量三个批次、监控看板、用户升级说明、支持和回滚责任表 |
| DoD | 每批满足观察窗口和退出标准；无格式回归、请求放大、凭据问题或重复写入 |
| 验收 | AC-127、AC-128 |

### G5：General Availability

- AC-101–AC-128 全部通过，阻断项为零。
- 五个平台的安装、首次运行、升级、离线缓存和回滚证据齐全。
- 迁移矩阵所有范围内命令为 `Go default`，所有例外有期限和 Owner。
- 当前态文档已更新，目标态文档状态改为 Implemented。
- G5 最终摘要绑定同一正式候选和六个不同的成功来源 run；签字晚于摘要，seal 可独立复验，Tag 发布只消费该 seal。
- 将长期有效的架构、CLI、配置和发布规则合并回正式文档；G5 签字和两个正式版本观察期结束后，用独立文档归档 PR 删除本实施任务书、迁移矩阵和验收计划。可追溯性保留在发布 Tag、Git 历史及受限验收 artifact，不在 `docs/` 留两套现行规范。
- MT、AO、QO、SO、RO、SCO 最终签字。

## 10. 阻断、暂停与回滚

### 10.1 立即阻断条件

出现任一项立即停止当前阶段切流：

- `presentation` 列缺失、重排、重命名或 `rendered_markdown` 未原样交给 Skill。
- “负责/常用账户”触发 Token 刷新、官方 API 或账户表现查询。
- 千川账户表现调用计划列表，或计划报表从 `stats_info` 读取金额。
- 分页失败从第 1 页重新扫描，账户同步以部分结果覆盖旧快照。
- 营销与千川 Token 串用，或任何敏感值进入 stdout、stderr、artifact、journal、URL。
- 未经 `--submit` 发生写入；写结果未知时被自动重放。
- 千川批量判重扩大为历史 180 天或重复创建同达人同商品计划。
- launcher 执行未签名、摘要不符或跨版本资产。
- 数据竞争、锁失效、配置截断或不可逆 Schema 修改。

### 10.2 回滚层级

| 层级 | 触发 | 动作 | 恢复条件 |
| --- | --- | --- | --- |
| R1 单命令 | 单命令合同或性能失败 | manifest 将该命令切回 Python | 修复后重新 Shadow + canary |
| R2 单领域 | 共用 Adapter/状态问题 | 该领域全部命令回 Python | 领域 Gate 全量重验 |
| R3 全 Go | 凭据、锁、供应链或重复写入 | 发布紧急 manifest/版本，全量 Python；撤销坏资产 | G2/G4/G5 重新签字 |
| R4 凭据事件 | 发现 Token/Secret 泄露 | 停止发布、撤销/轮换凭据、清理 artifact 和历史 | SO 完成事件复盘与验证 |

回滚只改变运行时选择，不自动降级状态或删除官方对象。已经可能生效的写入先通过官方查询对账；禁止用“重新执行一次”作为恢复方式。

## 11. 交付跟踪模板

每个 Task 在 Issue/PR 使用以下最小模板：

```text
Task ID:
Owner / Reviewer:
Commands / endpoints:
Dependencies:
Contract fixtures:
Behavior and request-count delta:
Security impact:
Acceptance IDs and evidence:
Migration state: Not started | Shadow | Go canary | Go default | Rolled back
Rollback command/manifest change:
Open risks and expiry:
```

阶段签字使用[验收计划](go-sdk-acceptance-plan.md)中的 Gate 记录，不以“CI 绿了”替代业务、安装和回滚验收。
