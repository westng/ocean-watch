# Go SDK 验收计划

本文是 Go SDK 迁移的可执行验收规范。每个用例都给出 Given/When/Then、命令、fixture、证据和通过标准；缺少命令或证据本身即为失败。阶段和责任见[实施任务书](go-sdk-execution-plan.md)，接口范围见[迁移矩阵](go-sdk-migration-matrix.md)。

## 1. 验收原则

1. **先合同、后实现。** Python 基线在 P0 冻结；Go 结果与归一化基线比较，不凭人工印象判断。
2. **测试不访问真实业务。** 自动测试使用合成配置、内存凭据和可注入 Transport；生产 host allowlist 不为测试放宽。
3. **真实 canary 单独审批。** 只读 canary 和写 canary 分开，写入只允许专用测试广告主并需要双人批准。
4. **证据可追溯且脱敏。** 每条 AC 产生机器结果、日志摘要和 hash；严禁保存真实 Token、账户明细和投放链接。
5. **失败关闭。** 缺字段、分页矛盾、重复 ID、未知写入、签名失败和脱敏扫描失败均阻断发布。

## 2. 测试环境

### 2.1 固定工具链

| 项目 | 要求 |
| --- | --- |
| Go | `1.26.5`；CI 使用精确版本并记录 `go version` |
| Python 基线 | 当前支持的 `3.9` 和 `3.12` |
| 官方 SDK | `v1.1.92` / `3b0bab7648f8e94fba1fd8bcfb96c43539b95c28` |
| OS/架构 | macOS arm64/amd64、Linux amd64/arm64、Windows amd64 |
| 时区 | 业务日期用例固定 `Asia/Shanghai`；另测 UTC 主机不改变业务当天 |
| Locale/编码 | `UTF-8`；Windows 控制台验证中文 JSON/Markdown |

CI 的 Go patch 升级需要独立依赖 PR 并重跑 AC-101–AC-128。开发者本机版本不作为签字证据。

### 2.2 测试层级

| 层级 | 网络 | 数据 | 目标 |
| --- | --- | --- | --- |
| Unit | 无 | 纯值对象 | 校验、映射、汇总、Presentation、错误分类 |
| Adapter contract | 无 | 合成官方响应 fixture | 生成 Service、envelope、分页、重试、Token 与 DTO |
| CLI golden | 无 | 合成 `$CODEX_HOME` | 命令、参数、JSON、退出码、文件副作用 |
| Fault injection | 无 | 可编程 RoundTripper | 超时、断连、429/5xx、业务码、未知写入 |
| Cross-platform | 无 | 合成状态与凭据后端 | 文件锁、编码、launcher、缓存 |
| Read canary | 官方只读 | 专用授权 | 实际权限、字段、分页和口径 |
| Write canary | 官方写入 | 专用不可投放测试广告主 | 最小创建/更新/追加/删除和对账 |

Adapter 测试通过注入 `http.RoundTripper` 拦截官方 host 请求，不在生产配置增加测试 host。fixture 必须包含 SDK 实际反序列化形状，不能绕过 SDK 直接构造领域结果。

## 3. Fixture 与证据规范

### 3.1 目录

实施阶段必须建立：

```text
contracts/
  commands.yaml
  output/*.schema.json
  presentation/*.md
testdata/
  contracts/python/
  state/v*/
  oceanengine/<channel>/<endpoint-alias>/<scenario>/*.json
  workmetadata/
artifacts/go-sdk-acceptance/<git-sha>/
  environment.json
  junit/
  contracts/
  security/
  performance/
  canary/
  release/
  summary.json
  signoff.json
```

`testdata/` 可提交，但只能包含明显占位数据。`artifacts/` 不提交 Git，由 CI 作为受限 artifact 保存 90 天；canary artifact 保存脱敏摘要，不保存原始官方响应。

### 3.2 占位数据

统一使用：

| 类型 | 示例 |
| --- | --- |
| 广告主 | `1000000000000001`、`1000000000000002` |
| 计划/项目 | `2000000000000001` |
| 商品 | `3000000000000001` |
| 达人 | `4000000000000001` / `creator_fixture_a` |
| 素材/作品 | `5000000000000001` |
| Token | `TEST_ACCESS_TOKEN_DO_NOT_USE` |
| URL | `https://www.douyin.com/video/5000000000000001` |

secret scan 应将任何非 allowlist 的长随机串、Bearer/Access-Token Header、`refresh_token`、`secret`、`auth_code` 和 MCP 动态 URL 判为失败。

### 3.3 标准验收命令

以下入口由 P0/P1 创建，之后所有阶段复用：

```bash
scripts/acceptance/run.sh --suite contracts
scripts/acceptance/run.ps1 -Suite contracts

go -C prototype/ocean-watch-go test ./...
go -C prototype/ocean-watch-go test -race ./...
go -C prototype/ocean-watch-go vet ./...
staticcheck ./...
govulncheck ./...
gosec ./...
```

`contracts` 套件构建候选二进制、调用同一 Go runner 捕获 Python 基线并比较、生成 JSON/JUnit、执行证据脱敏扫描。默认证据目录绑定当前 commit；可用 `OCEAN_WATCH_EVIDENCE_DIR`（PowerShell 为 `-EvidenceDir`）覆盖。Windows 等价命令由 CI PowerShell job 执行；脚本差异不能改变测试范围。

需要真实模型的 Skill 评测使用同一验收入口，并把模型身份固定在证据而不是生产 Skill 的关键词规则中：

```bash
scripts/acceptance/run.sh --suite skill-eval --trials 3 \
  --driver-command "$OCEAN_WATCH_SKILL_EVAL_DRIVER" \
  --model "$OCEAN_WATCH_EVAL_MODEL" \
  --reasoning "$OCEAN_WATCH_EVAL_REASONING"
scripts/acceptance/run.ps1 -Suite skill-eval -Trials 3 -DriverCommand "$env:OCEAN_WATCH_SKILL_EVAL_DRIVER" -Model "$env:OCEAN_WATCH_EVAL_MODEL" -Reasoning "$env:OCEAN_WATCH_EVAL_REASONING"
```

该套件需要受控的 Codex 评测凭据，因此在发布候选环境运行；普通 PR 继续运行语义清单、Skill 元数据和 Presentation 的确定性测试。runner 必须安装当前候选 Plugin，隔离每个会话，禁用真实写入，捕获脱敏工具轨迹，并在 `environment.json` 记录 Codex 版本、模型快照、推理配置和 Plugin 版本。

## 4. 合同与 CLI 验收

### AC-101 命令树与参数兼容

- Owner/Gate：QO + SCO；G0/G1/G5。
- Given：`contracts/commands.yaml` 包含当前 `COMMANDS` 全部命令、参数、默认值和 help 归一化规则。
- When：分别对 Python 基线和 Go CLI 运行顶层、domain、action 的 `--help` 及代表性参数解析。
- Then：命令无缺失；参数名、是否必填、默认值、重复参数和互斥关系一致；新增内部选项不出现在用户帮助中。
- 命令：`go test ./internal/cli -run TestCommandManifestParity -count=1`。
- 证据：`contracts/ac-101-command-parity.json`、JUnit。
- 通过标准：差异数 `0`，退出码 `0`。

### AC-102 JSON、stdout/stderr 与退出码

- Owner/Gate：QO；G1。
- Given：成功、业务失败、配置失败、异常和 SIGINT fixture。
- When：通过 contract runner 启动 CLI，分别捕获 stdout、stderr 和进程码。
- Then：stdout 恰有一个 UTF-8 JSON；错误结构稳定；成功/业务/配置/中断分别为 `0/1/2/130`；SDK 日志不混入任一输出。
- 命令：`go test ./internal/cli -run 'TestJSONBoundary|TestExitCodes|TestInterrupt' -count=1`。
- 证据：`contracts/ac-102-process-boundary.json`。
- 通过标准：全部场景通过，stdout JSON parse failure 为 `0`。

### AC-103 负责/常用账户名单合同

- Owner/Gate：SCO + QO；G1/G5。
- Given：合成账户簿含营销、千川、启用和停用记录，Token 已过期；network spy 和 credential spy 已启用。
- When：执行 `accounts list`，并让 Skill 回放“我负责的账户”“我常用的广告账户”“我管的户”和带错别字问法。
- Then：都路由 `accounts list`；Token refresh、credential read 和网络调用均为 `0`；默认只含启用记录；Presentation 固定渠道、账户名称、广告主 ID、启用状态四列。
- 命令：`go test ./internal/application/accounts ./internal/presentation ./contracts -run TestManagedAccountMembershipContract -count=1`；`scripts/acceptance/run.sh --suite skill-eval --case-set responsible-account-membership --trials 3`。
- 证据：`contracts/ac-103-membership.json`、`contracts/ac-103-rendered.md`、逐 case/试次的模型工具轨迹。
- 通过标准：确定性调用计数全为 `0`，四列表头和顺序字节级一致；公开集与保密改写集每个阻断 case 均 `3/3` 正确路由，且没有报表、刷新或网络工具调用。

### AC-104 账户表现 endpoint 隔离

- Owner/Gate：MA + QA-API；G2。
- Given：两个启用账户，一个营销、一个千川；官方 spy 记录 Service 与请求参数。
- When：执行当天 `accounts report`。
- Then：营销只调用 `ReportCustomGetV30ApiService` 的账户聚合；千川只调用 `QianchuanReportUniPromotionGetV10ApiService`；`QianchuanUniPromotionListV10ApiService` 调用数为 `0`；一个账户超时不取消另一个结果。
- 命令：`go test ./internal/application/accounts -run TestAccountReportEndpointIsolation -count=1`。
- 证据：`contracts/ac-104-service-calls.json`、完整/部分失败黄金 JSON。
- 通过标准：每账户期望报表请求 `1` 次（分页 fixture 除外），计划列表 `0` 次，Presentation 完整。

### AC-105 强制 Presentation

- Owner/Gate：SCO + QO；G1/G3/G4/G5。
- Given：名单、账户报表、营销计划报表、千川计划报表、千川批量成功/空/部分失败黄金结果。
- When：生成 `presentation` 并执行 Skill response replay。
- Then：`required=true` 时 Skill 原样输出 `rendered_markdown`；不得删、并、改、调列；千川批量始终是 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`，失败详情在表外。
- 命令：`go test ./internal/presentation ./contracts -run TestMandatoryPresentationContracts -count=1`，运行现有 `python3 -m unittest tests.test_plugin_metadata -v`，以及 `scripts/acceptance/run.sh --suite skill-eval --case-set mandatory-presentation --trials 3`。
- 证据：`contracts/ac-105-presentation-diff.json`、Markdown golden。
- 通过标准：结构化列和 Markdown 差异均为 `0`；空结果仍保留表头；模型每个阻断 case 的最终强制 Markdown 均 `3/3` 字节级一致。

## 5. 状态、授权与安全验收

### AC-106 现有状态 Schema 与路径兼容

- Owner/Gate：RL + AP；G1/G5。
- Given：每个历史 Schema、未知字段、项目配置/用户配置优先级和只读文件 fixture。
- When：Go 读取、无修改写回、Python→Go→Python 往返，并在只读命令前后计算目录 hash。
- Then：无需重新初始化；未知字段保留；优先级不变；只读目录 hash 不变；迁移幂等。
- 命令：`go test ./internal/platform/config ./internal/platform/state -run TestLegacyStateCompatibility -count=1`。
- 证据：`contracts/ac-106-state-roundtrip.json`、before/after hash。
- 通过标准：语义和未知字段差异 `0`；第二次迁移文件 hash 不变。

### AC-107 锁、原子写、崩溃与凭据兼容

- Owner/Gate：AP + RL + SO；G1/G2。
- Given：Python 与 Go 进程竞争同一合成配置/刷新锁；写入在 fsync、rename 前后注入崩溃；三平台测试凭据。
- When：运行 100 轮并发读改写、崩溃恢复和跨运行时凭据读取。
- Then：无截断 JSON、无丢失已确认更新、无双刷新；Go 能读取 Python 命名的测试凭据；权限符合平台要求。
- 命令：`go test -race ./internal/platform/lock ./internal/adapters/credentials -run 'TestCrossRuntimeLock|TestAtomicCrashRecovery|TestCredentialCompatibility' -count=100`。
- 证据：`contracts/ac-107-concurrency.json`、各平台权限摘要。
- 通过标准：100 轮错误 `0`，race `0`，明文 Secret artifact `0`。

### AC-108 OAuth callback 与 state

- Owner/Gate：AP + SO；G2。
- Given：有效 AD/QC state、错误前缀、随机值不匹配、重复 callback、过期 callback 和端口占用 fixture。
- When：运行本地 OAuth 流程到 Token Adapter 边界。
- Then：只有完整匹配的 state 可交换 Token；渠道来自已验证 state；重复/过期 callback 不覆盖授权；同步失败保留 pending 授权。
- 命令：`go test ./internal/application/auth ./internal/adapters/browser -run TestOAuthCallbackState -count=1`。
- 证据：`contracts/ac-108-oauth-state.json`。
- 通过标准：攻击/错误场景 Token Service 调用数 `0`；合法 AD/QC 各 `1`。

### AC-109 脱敏、host、重定向和响应限制

- Owner/Gate：SO；G2/G5。
- Given：Token/Secret 位于 Header、query、JSON、嵌套 SDK error 和 redirect Location；超大/无效响应。
- When：触发正常日志、diagnostics、HTTP error、panic recovery 和 artifact 收集。
- Then：敏感值不出现；非官方 host 和跨 host redirect 被拒绝；超限响应失败关闭；SDK log middleware 未启用。
- 命令：`go test ./internal/platform/redaction ./internal/adapters/oceanengine -run 'TestRedactionCorpus|TestOfficialHostPolicy|TestResponseLimit|TestSDKLoggingDisabled' -count=1`。
- 证据：`security/ac-109-redaction.json`、secret scan 报告。
- 通过标准：敏感命中 `0`；违规 host 请求实际发送数 `0`。

### AC-110 Token 刷新、单飞与渠道隔离

- Owner/Gate：AP；G2。
- Given：有效、临期、过期 Token；50 个同授权并发调用；营销和千川使用相同广告主占位 ID 的不同授权。
- When：并发启动业务请求并注入刷新成功/失败/保存失败。
- Then：同授权只刷新一次；等待者复用；失败保留旧凭据；营销请求从不携带千川 Token，反之亦然；明确过期重放最多一次。
- 命令：`go test -race ./internal/application/auth -run TestTokenRefreshSingleflightAndIsolation -count=20`。
- 证据：`contracts/ac-110-refresh-counts.json`。
- 通过标准：每轮 refresh 数 `1`；跨渠道 Token 泄漏 `0`；业务重放不超过 `1`。

### AC-111 广告主完整同步与快照保护

- Owner/Gate：AP + QO；G2。
- Given：直客、Customer Center、EBP、代理和千川店铺多页 fixture；旧快照已有额外账户；其中一个角色中页失败。
- When：执行 `auth sync-accounts` 成功和失败场景。
- Then：成功时合并全部声明页、去重并原子替换；失败时命令非零且旧快照字节不变；不得返回“成功但少账户”。
- 命令：`go test ./internal/application/auth -run TestAdvertiserSnapshotTransaction -count=1`。
- 证据：`contracts/ac-111-snapshot.json`、old/new hash。
- 通过标准：成功账户数等于 fixture 唯一集合；失败 old/new hash 相同。

## 6. SDK Adapter、分页与报表验收

### AC-112 分页、当前页重试与去重

- Owner/Gate：QO + RL；G2/G3。
- Given：三页数据，页 2 依次返回 `40100`、`51010`、成功；另有 HTTP 429、停滞 cursor、矛盾总数和重复唯一键 fixture。
- When：运行 page 和 cursor 两类分页器。
- Then：调用序列严格为 `1,2,2,2,3`；页 1 不重扫；停滞/矛盾/重复 fail closed；`Retry-After` 有上限。
- 命令：`go test ./internal/platform/pagination ./internal/platform/retry -run TestPageLocalRetryStateMachine -count=1`。
- 证据：`performance/ac-112-call-sequence.json`。
- 通过标准：期望序列完全一致；非法分页结果不返回部分成功。

### AC-113 报表指标来源、精度与汇总

- Owner/Gate：MA + QA-API + QO；G2/G3。
- Given：多页小数指标、展示 top 小于总行数、缺失 GMV/订单、跨渠道不同口径和 plan-list `stats_info` 干扰值。
- When：执行账户、营销计划、千川计划和素材报表。
- Then：完整页先用 Decimal 汇总再显示取舍；`--top` 不改总计；缺指标为 null；跨渠道 GMV/ROI 不错误混加；千川金额不读取 `stats_info`。
- 命令：`go test ./internal/application/reports -run TestReportMetricContracts -count=1`。
- 证据：`contracts/ac-113-report-metrics.json`。
- 通过标准：期望值逐字段相等，不允许浮点容差掩盖分币误差。

### AC-114 生成 Service、Envelope 与 DTO 映射

- Owner/Gate：AO + 各 Adapter Owner；G2/G3/G4。
- Given：[迁移矩阵](go-sdk-migration-matrix.md)列出的每个 endpoint 的成功、空、HTTP error、HTTP 200 业务 error、缺少必需 data fixture，以及目标架构定义的 host profile。
- When：运行 Adapter 合同套件并扫描 imports/Service 调用。
- Then：每个 endpoint 使用准确生成 Service、HTTP 方法和 host profile；`CommonApi` 使用数 `0`；业务 `code != 0` 必须错误；SDK 模型不出 Adapter；ID 字符串无精度损失。
- 命令：`go test ./internal/adapters/oceanengine/... -run TestGeneratedServiceContracts -count=1` 和 `go test ./internal/architecture -run TestSDKImportBoundary -count=1`。
- 证据：`contracts/ac-114-endpoint-coverage.json`。
- 通过标准：矩阵 endpoint 覆盖率 `100%`，错误漏判 `0`，越界 import `0`。

### AC-115 千川计划报表 SDK REST 迁移

- Owner/Gate：QA-API + QO；G3。
- Given：同一脱敏 fixture 可由旧 MCP mapper 和新 SDK REST mapper处理，包含元数据缺失、`status=ALL` 和特定状态。
- When：运行 shadow comparison，并检查 Service spy。
- Then：财务字段来自 `QianchuanReportUniPromotionDataGetV10ApiService`，计划元数据来自 list；日期范围相同；默认输出和 Presentation 兼容；SDK 失败不静默回退 MCP。
- 命令：`go test ./internal/application/reports -run TestQianchuanPlanReportSDKParity -count=1`。
- 证据：`contracts/ac-115-sdk-mcp-shadow.json`、Service 调用记录。
- 通过标准：归一化业务字段差异 `0`；list 金额读取 `0`；静默回退 `0`。

## 7. 写入与批量验收

### AC-116 dry-run、提交与写入边界

- Owner/Gate：SO + QO；G4。
- Given：每个创建、更新、删除命令和有效/无效 payload；write Service spy。
- When：不带 `--submit`、带 `--submit`、以及千川删除缺少/具备 `--confirm-delete` 执行。
- Then：dry-run 写调用 `0` 且不需要 Token；无效 payload 在凭据读取前阻断；删除只有双确认才调用；输出显示 endpoint、广告主和变更范围。
- 命令：`go test ./internal/application/plans ./internal/cli -run TestWriteAuthorizationBoundary -count=1`。
- 证据：`contracts/ac-116-write-guard.json`。
- 通过标准：未经授权写调用 `0`；所有命令场景覆盖率 `100%`。

### AC-117 营销项目/单元事务与续建

- Owner/Gate：MA + QO；G4。
- Given：项目成功/单元成功，项目失败，项目成功/单元失败，已有 journal 续建和批量部分失败 fixture。
- When：执行单个与批量上传/达人计划。
- Then：项目 ID 只来自成功响应；单元使用正确项目；失败 journal 可续建且不重复项目；所有创建命令使用同一 Executor。
- 命令：`go test ./internal/application/plans/marketing -run TestProjectPromotionTransaction -count=1`。
- 证据：`contracts/ac-117-marketing-transaction.json`。
- 通过标准：重复项目/单元 `0`；每种失败状态有确定恢复动作和退出码。

### AC-118 营销未知写入结果对账

- Owner/Gate：MA + SO；G4。
- Given：项目/单元写在请求发送前失败、发送后响应前断连、官方已应用、未应用和出现多个候选 fixture。
- When：运行 Executor 和 reconciler。
- Then：发送前失败可安全返回；发送后不自动重放；唯一匹配为 `applied`，无匹配为 `not_applied`，多匹配为 `ambiguous` 并停止。
- 命令：`go test ./internal/application/plans/marketing -run TestUnknownWriteReconciliation -count=1`。
- 证据：`contracts/ac-118-reconciliation.json`。
- 通过标准：unknown 场景 create Service 调用最多 `1`；ambiguous 后续写调用 `0`。

### AC-119 千川当天判重与创建对账

- Owner/Gate：QA-API + QO；G4。
- Given：Asia/Shanghai 固定当天、三页计划、页 2 瞬时失败、可见抖音 ID 与数值 `aweme_id`、同达人不同商品、已删除/暂停/重复候选、未知创建结果 fixture。
- When：执行 `plans batch-qianchuan-works` 预检和提交对账。
- Then：list 仅请求当天 `00:00:00`–`23:59:59`；序列 `1,2,2,2,3`；详情只查候选；暂停视为已有、删除视为不存在；精确达人 + 商品才追加；未知创建先查询，不重发。
- 命令：`TZ=UTC go test ./internal/application/plans/qianchuan -run TestCurrentDayPlanReconciliation -count=1`。
- 证据：`performance/ac-119-plan-scan.json`、请求参数和调用图。
- 通过标准：历史 180 天请求 `0`；页 1 重扫 `0`；错误匹配/重复创建 `0`。

### AC-120 千川批量追加幂等与完成格式

- Owner/Gate：QA-API + SCO；G4。
- Given：55 个链接，含重复、无效、未授权、商品不匹配、已有素材、101 个新素材、追加响应未知和部分失败 fixture。
- When：dry-run、首次 submit、相同输入重跑并执行 Presentation replay。
- Then：宽授权列表最多一次；作品按官方上限分批；已有素材不追加；101 个素材按 `100+1`；未知结果先查素材；重跑新增调用为 `0`；最终始终固定五列，失败在表外。
- 命令：`go test ./internal/application/plans/qianchuan -run TestBatchWorkIdempotencyAndPresentation -count=1`。
- 证据：`performance/ac-120-batch-calls.json`、`contracts/ac-120-rendered.md`。
- 通过标准：重复追加 `0`；单次 add 大于 100 的请求 `0`；列差异 `0`。

### AC-121 千川删除与计划设置对账

- Owner/Gate：QA-API + SO；G4。
- Given：CUSTOM、SMART、重复、已删除、歧义 material；101 个删除；设置更新成功/部分失败/超时 fixture。
- When：执行删除和 Marketing/Qianchuan 设置更新。
- Then：只发送 nested `material_id`；SMART 跳过；删除分 `100+1`；成功须回读 `DELETED`；设置逐行回读，部分失败返回非零；同广告主写串行。
- 命令：`go test ./internal/application/plans/marketing ./internal/application/plans/qianchuan -run TestMutationReconciliation -count=1`。
- 证据：`contracts/ac-121-mutations.json`。
- 通过标准：发送 `aweme_item_id` 次数 `0`；未回读确认的成功数 `0`；并发写重叠 `0`。

## 8. 非功能与发布验收

### AC-122 请求数量、限流与性能预算

- Owner/Gate：RL + QO；G3/G4。
- Given：固定本地 fixture 和可编程 0/25/100 ms 延迟，覆盖两个账户、三页报表和 55 链接批量。
- When：各场景运行 30 次，记录 p50/p95、Service 调用数、重试和限流等待。
- Then：请求数符合业务下限，没有 N+1 或第 1 页重扫；本地命令 p95 不高于 150 ms；Go fixture 用例 p95 不高于 Python 基线 1.15 倍且无单次超过用例 deadline。
- 命令：`go test ./internal/performance -run TestRequestBudgets -count=30`，`go test -bench . -benchmem ./internal/performance`。
- 证据：`performance/ac-122-summary.json`、benchstat。
- 通过标准：`accounts list` 网络 `0`；两账户报表计划列表 `0`；页故障序列符合 AC-112；55 链接授权宽扫描不超过 `1`。

### AC-123 取消、deadline、资源与 data race

- Owner/Gate：RL + QO；G1/G3/G4。
- Given：挂起连接、慢响应、阻塞锁、并发分页和 SIGINT fixture。
- When：触发 context deadline 和中断，随后检查 goroutine、文件锁和临时文件。
- Then：请求及时取消，进程返回 `130` 或结构化 timeout；无 goroutine/锁/临时文件泄漏；共享 Client 无运行时配置写竞争。
- 命令：`go test -race ./...` 和 `go test ./internal/platform -run TestCancellationAndLeak -count=20`。
- 证据：`performance/ac-123-race-and-leak.json`。
- 通过标准：race `0`、泄漏 `0`、超出 deadline 5 秒以上场景 `0`。

### AC-124 多平台二进制与 Plugin launcher

- Owner/Gate：RO + QO；G5。
- Given：五个平台构建资产、当前支持的 Python 解释器、未安装 Go/Ocean Watch Python 包/SDK 的干净 runner、正确/错误 OS/arch、损坏缓存、错误摘要、错误签名、错误版本身份、离线有/无缓存。
- When：在干净 runner 安装 Plugin 并通过两个 Skill `run.py` 启动 `--version` 和 `accounts list`。
- Then：选择同 `product_version` 且 manifest 身份完全匹配的正确资产；先验签/校验后执行；缓存原子且用户私有；坏资产拒绝；离线有缓存可用、无缓存给结构化错误。
- 命令：`scripts/acceptance/run.sh --suite launcher` 与 `scripts/acceptance/run.ps1 -Suite launcher`。
- 证据：`release/ac-124-platform-matrix.json`。
- 通过标准：5/5 平台通过；错误资产执行次数 `0`；只依赖兼容期已冻结的 Python 3.9+ 解释器，不依赖系统 Go、pip 安装、Ocean Watch Python 包或 SDK。

### AC-125 供应链、静态分析与可复现构建

- Owner/Gate：SO + RO；G0/G5。
- Given：固定 Tag、干净 builder 和依赖清单。
- When：构建两次，生成 checksum、SBOM、provenance，执行 vet/staticcheck/govulncheck/gosec/license/secret scan。
- Then：SDK pin、提交和许可证匹配；产物可追溯；SDK 日志启用代码扫描为零；完整 gosec 报告没有扫描错误或 `nosec`，所有 Medium/Low 启发式命中与限期控制清单的规则、文件和代码指纹精确一致，且 High/Critical 不允许进入控制清单。
- 命令：`scripts/acceptance/run.sh --suite supply-chain`。
- 证据：`security/ac-125-sbom.*`、扫描报告、两次 checksum。
- 通过标准：Critical/High `0`；gosec 新增/变化/陈旧/过期/扫描错误/`nosec` 均为 `0`；签名验证 `100%`；可复现差异 `0` 或有 AO+SO 批准说明。`contracts/security/gosec-controls.json` 只记录受控 Medium/Low 启发式命中，不代替 SO 评审或任何 Gate 签字。

### AC-126 升级、缓存与回滚不损坏状态

- Owner/Gate：RO + AP + QO；G5。
- Given：Python-only 安装、旧 Go、当前 Go、损坏当前缓存和同一合成用户状态。
- When：依次执行 Python→Go、Go→旧 Go、Go route→Python、重新升级，并在每步运行名单、模板和授权状态查询。
- Then：无需重新授权；状态 Schema 和 hash 符合预期；回滚只改变运行时路由；损坏缓存不会污染好版本。
- 命令：`scripts/acceptance/run.sh --suite upgrade-rollback`。
- 证据：`release/ac-126-upgrade-rollback.json`、状态 hash 链。
- 通过标准：数据丢失/不可读 `0`；重新授权次数 `0`；回滚恢复时间不超过 15 分钟。

### AC-127 真实 canary 与停止演练

- Owner/Gate：MT + RO + SO + 领域 Owner；G2/G4/G5。
- Given：专用测试授权、无真实用户数据、批准的操作清单、消耗风险为零或硬性隔离、即时停止负责人在线。
- When：先执行只读 canary，再执行最小 dry-run；获得 MT+SO 双批准后执行一个最小写场景并立即回读和暂停/清理；最后演练 R1/R3 回滚。
- Then：实际字段/权限与 fixture 一致；无额外 endpoint；写对象唯一且可对账；停止和回滚在目标时间内完成。
- 命令：`scripts/acceptance/run.sh --suite canary --approval-file "$APPROVAL_FILE" --evidence-dir "$EVIDENCE_DIR"`。
- 证据：`canary/ac-127-summary.json`、审批 ID、对象 ID 单向 hash、请求计数、清理确认。
- 通过标准：未批准写调用 `0`；重复对象 `0`；非清单 endpoint `0`；R1 5 分钟内、R3 15 分钟内完成。

### AC-128 文档、Skill 与用户升级体验

- Owner/Gate：SCO + RO + QO；G0 基线/G5。
- Given：干净 Codex 安装、旧版本升级、至少包含账户名单/账户表现/模板/计划查询/写入确认/强制 Presentation 的单轮与多轮语义集、发布前保密改写集和当前文档链接。语义集包含口语、简称、错别字、省略、上下文追问、相邻意图负例，但不把文本关键词作为期望判定条件。
- When：安装候选 Plugin，在不安装 Go、Ocean Watch Python 包或 SDK 的情况下完成初始化、账户名单、账户表现、计划报表、dry-run 和批量结果展示；再用批准的 Codex 模型对每个阻断 case 独立运行三次。
- Then：用户无需固定句式或理解运行时；Skill 根据完整对话路由正确命令；名单不触发表现或网络；写入仍需明确确认；固定展示合同不变；文档命令可复制执行；当前态/目标态标记准确。
- 命令：`python3 -m unittest tests.test_plugin_metadata -v`、`scripts/acceptance/run.sh --suite skill-eval --trials 3` 和 `scripts/acceptance/run.sh --suite user-journey`。
- 证据：`contracts/ac-128-skill-eval.json`、脱敏逐 case 工具轨迹、`contracts/ac-128-user-journeys.json`、文档 link check；证据必须包含模型快照、Codex/Plugin 版本、推理配置、commit 和试次。
- 通过标准：公开集和保密改写集的阻断 case 均 `3/3` 正确，错误/多余工具调用 `0`，强制 Presentation 差异 `0`，断链 `0`，除冻结的 Python 解释器外手工补依赖步骤 `0`。模型或 Codex 版本变化必须重新运行，不得沿用旧证据。

## 9. Canary 控制

### 9.1 只读 canary

- 使用至少一个营销和一个千川专用授权。
- 覆盖 Token 临期刷新、完整广告主分页、当天账户报表、计划/素材多页读取。
- 记录 endpoint alias、页数、重试数、耗时和响应 Schema hash，不记录原始 Token 或业务行。
- 与 Python 在同一日期范围运行 shadow；不得让 shadow 双倍请求突破授权限额，必要时顺序运行并限制频率。

### 9.2 写 canary

写 canary 不属于自动 CI，必须满足：

1. 专用测试广告主与真实业务隔离，并验证无实际消耗风险。
2. PR 已通过全部模拟写入验收。
3. 审批文件包含版本、commit、命令、广告主 hash、最大对象数、最大预算参数、停止负责人和过期时间。
4. 先 dry-run 并人工核对 exact payload，再只执行批准范围。
5. 每次写后立即官方回读；无法确认时停止，不继续下一步。
6. 创建对象立即暂停或按官方能力清理，保留对象 ID 单向 hash 和清理结论。

禁止在 canary 为方便而降低 `--submit`、`--confirm-delete`、host、Token 隔离或对账要求。

## 10. 阶段 Gate 与签字

| Gate | 必过 AC | 必签角色 | 允许切换 |
| --- | --- | --- | --- |
| G0 | AC-101–AC-107、AC-114、AC-125、AC-128 的基线部分 | MT, AO, QO, SO, SCO | 无，只允许实现 |
| G1 | AC-101–AC-107、AC-123 | AO, RL, QO, SCO | 本地命令 Go default |
| G2 | AC-104、AC-108–AC-114 | AP, MA, QA-API, QO, SO | auth/accounts Go default |
| G3 | AC-105、AC-112–AC-115、AC-122–AC-123 | MA, QA-API, RL, QO, SCO | 只读业务 Go default |
| G4 | AC-105、AC-116–AC-123、AC-127 | MT, MA, QA-API, QO, SO, RO | 写命令分批 Go default |
| G5 | AC-101–AC-128 | MT, AO, QO, SO, RO, SCO | 全量发布 |

`signoff.json` 最小字段：

```json
{
  "gate": "G3",
  "git_sha": "FULL_COMMIT_SHA",
  "sdk_version": "v1.1.92",
  "evidence_sha256": "SHA256_OF_SUMMARY",
  "approvals": [
    {"role": "QO", "identity": "REVIEWER_ID", "approved_at": "RFC3339"}
  ],
  "exceptions": []
}
```

阻断级 AC 不允许豁免。非阻断差异必须包含 Owner、影响、到期日和回滚条件；到期仍未关闭则自动阻断下一 Gate。

G5 在上述角色字段之外还必须绑定完整 `candidate_identity`、
`candidate_identity_sha256` 和精确 canonical `summary.json` 的
`evidence_sha256`。摘要本身必须包含六个不同成功 workflow run 的
`source_runs` 与摘要值；六类固定为 formal、model、canary、Marketplace、rollback、
rollout，且全部属于同一仓库和 commit。签字必须晚于摘要生成，SO 与 RO 必须是不同
审批人。签字不能提交到源码或由 workflow 生成；它经受保护环境验证后，与候选、证据树、
摘要及 prepare/signoff producer metadata 一起形成 sealed artifact。Tag 发布只消费该
seal，并在只读与写入 Job 中各自重新验证。

## 11. 最终通过标准

发布候选只有同时满足以下条件才可宣布“可执行且已验收”：

- AC-101–AC-128 在适用平台全部通过，`summary.json` 中 failed/blocking/expired exception 均为 `0`。
- [迁移矩阵](go-sdk-migration-matrix.md)的命令与 endpoint 覆盖率 `100%`，无未经批准的 `CommonApi`。
- 合同差异、Presentation 差异、敏感值命中、data race、重复写入和错误账户写入均为 `0`。
- 账户同步失败保留旧快照；千川页 2 故障不重扫页 1；批量判重只查当天。
- 五个平台资产通过签名/checksum、安装、离线缓存、升级和回滚。
- 正式、模型、canary、Marketplace、回滚和 rollout 六类来源运行均成功且互不复用 run ID，全部绑定同一候选 commit。
- G5 签字完成，证据 hash、候选 identity、来源运行、seal 与发布 Tag/commit 绑定；公开 Release 不暴露审批身份。

仅完成代码、仅通过单元测试或仅在开发者机器运行成功，都不构成验收完成。
