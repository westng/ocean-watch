# Ocean Watch 达人授权素材模板设计

日期：2026-07-13

## 目标

Ocean Watch 当前只支持从广告主账户上传的视频素材创建巨量营销计划。巨量营销还允许广告主选择已获得投放授权的达人视频素材。本次改造要在同一个 `ads-plan-monitor` Skill 内增加达人授权素材能力，并让创建计划模板明确绑定素材来源，避免上传素材和达人素材被错误混用。

设计必须满足：

- 业务模板固定绑定一种素材来源。
- 模板继续明确绑定渠道、广告主、平台和商品。
- 达人素材必须通过官方接口实时查询，不把动态授权状态固化为模板事实。
- 创建前校验达人授权归属、状态和有效期。
- 上传素材现有流程保持兼容，已有模板无需重新创建。
- 第一版不允许同一个单元混合上传素材和达人素材。
- 普通原生单元只允许一个 `native_setting.aweme_id`，因此同一单元的达人素材必须来自同一个达人。
- 本期只实现巨量营销，不复用到尚未开发的巨量千川。

## 范围

### 本期包含

- 计划模板 schema 从 v2 升级到 v3。
- 为业务模板增加素材来源和选择规则。
- 将已有业务模板迁移为上传素材模板。
- 在模板创建向导中增加素材来源步骤。
- 增加统一素材候选模型和素材 Provider 接口。
- 接入巨量营销官方达人授权素材查询接口。
- 支持预览、选择、校验和批量创建达人素材计划。
- 为两种素材来源提供独立的 promotion payload 转换器。
- 在查询结果和运行摘要中显示素材来源。

### 本期不包含

- 巨量千川达人素材。
- 自动联系达人或申请素材授权。
- 管理达人授权关系。
- 下载、转存或重新上传达人视频。
- 在同一个单元内混合两种素材来源。
- 根据达人画像自动生成运营策略。
- 在模板中长期保存某一批达人素材 ID。

### 开发验收阶段

`0.7.0` 的第一阶段先在隔离 fixture 中跑通单项目、单单元链路：查询授权关系、运行时选材、提交前复查、生成达人原生 promotion payload，以及按顺序创建 project 和 promotion。达人素材批量分组、多账户并发、运行日志和中断恢复属于后续阶段；在这些能力完成前，`create_creator_plan.py` 不宣称支持达人批量创建。

## 核心决策

“素材来源”是业务模板的固定执行契约，“具体素材”是每次创建计划的运行时输入。

模板负责回答：

- 该计划使用上传素材还是达人授权素材。
- 如何筛选候选素材。
- 每个单元最多放多少条素材。

每次创建负责回答：

- 当前有哪些素材仍然可用。
- 用户最终选择了哪些素材。
- 提交瞬间授权和素材状态是否仍然有效。

这样可以避免模板保存已过期的达人授权，也不会把一次创建任务选中的素材污染后续任务。

## 模板模型

`plan_template_schema_version` 升级为 `3`。业务模板新增 `material_strategy`：

```json
{
  "bindings": {
    "channel": "marketing",
    "advertiser_id": "1234567890123456",
    "platform": "示例平台",
    "traffic_source": "CID",
    "product_id": "9876543210987654",
    "product_name": "示例商品"
  },
  "material_strategy": {
    "source_type": "CREATOR_AUTHORIZED",
    "selection_mode": "MANUAL",
    "max_materials_per_unit": 5,
    "creator_filters": {
      "creator_ids": [],
      "authorization_status": "VALID",
      "minimum_remaining_days": 1
    }
  }
}
```

### 素材来源

稳定枚举值：

| 枚举值 | 含义 |
| --- | --- |
| `ACCOUNT_UPLOAD` | 广告主账户上传的视频素材 |
| `CREATOR_AUTHORIZED` | 已授权当前广告主投放的达人视频素材 |

业务模板必须填写 `source_type`。默认模板骨架保持来源中立，由模板创建向导收集目标来源。

### 选择模式

第一版支持：

| 枚举值 | 含义 |
| --- | --- |
| `MANUAL` | 查询候选后由用户明确选择 |
| `LATEST` | 按官方可用时间或发布时间倒序选择 |

现有“今天上传素材”属于 `ACCOUNT_UPLOAD` 的运行时筛选条件，不作为通用选择模式。达人接口若没有可靠的“上传时间”语义，不允许把“今天达人素材”解释为今天授权或今天发布；必须让用户明确筛选口径。

### 达人筛选规则

`creator_filters` 只允许出现在 `CREATOR_AUTHORIZED` 模板中：

- `creator_ids`：可选达人白名单；空数组表示不限达人。
- `authorization_status`：第一版固定要求 `VALID`。
- `minimum_remaining_days`：创建时授权至少还需有效多少天，默认 `1`。

不得在模板中保存昵称作为权威筛选键。达人 ID、素材 ID、视频 ID 等官方 ID 必须按无损十进制字符串处理。

## 模板命名

新模板推荐使用：

```text
平台-CID-商品名-商品ID-素材来源
```

例如：

```text
示例平台-CID-示例商品-9876543210987654-上传素材
示例平台-CID-示例商品-9876543210987654-达人素材
```

已有模板名称不自动改名，避免破坏脚本参数、活动模板指针和用户习惯。迁移后列表展示应明确补充 `上传素材` 标签。新向导生成模板名称时包含来源后缀；用户可以修改名称，但同一配置内仍必须唯一。

## 模板向导

创建业务模板继续强制使用向导：

1. 选择默认模板骨架或某个真实业务模板作为来源。
2. 输入目标渠道和广告主 ID。
3. 输入平台、流量来源、商品名和商品 ID。
4. 明确选择上传素材或达人素材。
5. 配置选择模式、单元素材数量和来源专属筛选条件。
6. 配置投放参数、商品资产、文案、链接和监测地址。
7. 展示绑定关系、来源、继承字段、清理字段和完整性校验。
8. 用户确认后保存；激活仍需单独确认。

从现有模板复制时：

- 来源不变时可以继承不含具体素材 ID 的选择规则。
- 来源改变时额外清除原来源专属筛选条件。
- 跨广告主复制时继续执行现有账户资产清理策略。
- 达人 ID 白名单默认视为广告主相关配置；跨广告主复制时清空。
- 任何动态素材 ID 都不进入新模板。

清理顺序固定为：先复制基础投放与商品字段，再无条件删除 `overrides.materials.video_ids`、`overrides.materials.video_cover_ids` 以及来源 adapter 定义的内容、授权和封面动态 ID；来源改变时继续删除原来源专属选择规则；跨广告主时最后清空 `material_strategy.creator_filters.creator_ids`。即使广告主、商品和来源均相同，新模板也不得继承具体素材 ID。

## 统一素材领域模型

创建统一的 `MaterialCandidate`，屏蔽不同官方接口返回结构：

```json
{
  "channel": "marketing",
  "owner_advertiser_id": "1234567890123456",
  "source_type": "CREATOR_AUTHORIZED",
  "source_key": {
    "id_type": "MATERIAL_ID",
    "id_value": "1111111111111111"
  },
  "material_id": "1111111111111111",
  "video_id": "2222222222222222",
  "item_id": "3333333333333333",
  "title": "素材标题",
  "cover_url": "https://example.invalid/cover",
  "creator_id": "4444444444444444",
  "creator_name": "达人名称",
  "authorization_subject_id": "5555555555555555",
  "authorization_status": "VALID",
  "authorization_expires_at": "2026-08-01T00:00:00+08:00",
  "usable": true,
  "unusable_reason": null,
  "raw_status": "官方原始状态"
}
```

除 `source_key` 外，来源专属字段不存在时使用 `null`，不得编造 ID 或把不同类型 ID 互相替代。每个 Provider 必须根据官方 schema 生成非空 `source_key.id_type` 和 `source_key.id_value`；如果官方返回无法形成稳定来源键，该候选必须拒绝并报告。只有 Provider 和 payload adapter 可以接触来源专属原始字段。

用户本次选中的素材保存在临时 `MaterialSelection` 中，不写回业务模板：

```json
{
  "source_type": "CREATOR_AUTHORIZED",
  "advertiser_id": "1234567890123456",
  "selected_material_keys": [
    "marketing:1234567890123456:CREATOR_AUTHORIZED:MATERIAL_ID:1111111111111111"
  ],
  "selected_at": "2026-07-13T12:00:00+08:00"
}
```

候选唯一键必须使用 `channel + owner_advertiser_id + source_type + source_key.id_type + source_key.id_value`，防止多渠道、多账户、不同接口或不同 ID 类型返回相同数字时发生碰撞。`owner_advertiser_id` 必须来自本次查询上下文或官方可验证归属，不能从模板回填后假装已经验证；官方存在独立授权对象时同时保留 `authorization_subject_id`。查询和提交前复查均验证这两个归属字段。

`MaterialSelection` 只存在于进程内存或脱敏运行摘要中；运行摘要可以保存官方非敏感 ID 和状态，但不得被下一次任务直接当作已校验候选使用。

## 代码边界

新增统一接口：

```text
MaterialProvider
|-- AccountUploadMaterialProvider
`-- CreatorAuthorizedMaterialProvider
```

Provider 负责：

- 调用对应官方查询接口并处理分页。
- 将官方响应转换成 `MaterialCandidate`。
- 保留官方状态用于诊断。
- 根据模板规则生成候选列表。
- 对用户选中的素材执行提交前复查。

创建模块不再直接依赖 `/2/file/video/get/` 的响应结构。现有 `query_videos.py` 保留为兼容入口，但底层改为调用上传素材 Provider。新增统一查询入口供模板驱动的创建流程使用。

`material_strategy` 是业务模板顶层字段，不放入 `overrides`。`plan_templates.apply()` 将它复制到有效运行配置，并同时放入 `_selected_plan_template` 摘要。v3 Provider 流程只接受本次 `MaterialSelection`，不得读取 `overrides.materials.video_ids`、`overrides.materials.video_cover_ids` 或其他旧动态素材字段。含有这些旧字段的模板必须先完成下文的显式迁移确认，不能静默改变或继续执行旧行为。

payload 构建使用来源适配器：

```text
PromotionMaterialAdapter
|-- AccountUploadPromotionAdapter
`-- CreatorAuthorizedPromotionAdapter
```

达人素材必须按官方 promotion create schema 使用其专属字段。实现前通过官方文档 MCP 或官方接口文档确认查询端点、ID 类型、授权状态字段和创建 payload，不允许假设达人素材也只需要普通 `video_id`。

## 创建计划数据流

1. 解析业务模板并验证渠道、广告主和商品绑定。
2. 根据 `material_strategy.source_type` 选择唯一 Provider。
3. 使用模板筛选规则查询候选素材。
4. 返回对话内表格，展示素材 ID、达人、状态、有效期和不可用原因。
5. 根据选择模式取得本次运行素材集合。
6. 调用 Provider 重新查询并校验所选素材；每一组必须在创建项目之前完成最后一次复查。
7. 生成项目和单元 payload 预览。
8. 按 `max_materials_per_unit` 分组，且不得超过官方上限。
9. 用户确认真实创建后，按现有并发控制提交项目和单元。
10. 运行摘要记录素材来源、选择数量、跳过原因和创建结果，不记录敏感凭据。

素材授权可能在查询与提交之间变化。提交前复查失败时只跳过失效素材；若某组没有剩余素材，则不创建该组项目。官方仍可能在复查后、promotion 提交时拒绝素材，此时必须记录已创建的 `project_id`、promotion 失败响应和素材快照，结果标记为 `promotion_failed`。

### 批量运行日志和恢复

提交前冻结批次清单，为每个账户和分组生成稳定 `group_key`。稳定键至少包含渠道、广告主、模板名、素材来源、排序后的候选唯一键和组序号。恢复时使用原清单和原分组，禁止重新查询后自动重排、重新编号或把新素材加入旧批次。

运行日志使用现有原子 JSON 写入工具，并由单一 `RunJournalWriter` 协调器串行持久化。分组工作线程不得直接读改写同一个日志文件；它们提交包含 `group_key`、期望 revision 和新状态的转换事件，并等待写入协调器确认落盘后才能发起下一次有副作用的 API 请求。协调器在进程内锁内重新读取当前 revision、拒绝旧 revision 覆盖、原子写入并读回验证。每次成功转换增加单调 revision。

每次状态转换后立即落盘：

```text
planned -> revalidated -> project_pending -> project_created -> promotion_pending -> created
                                             `-> promotion_failed
```

- `project_created` 必须保存 `project_id` 后才能提交 promotion。
- `created` 组已有 `promotion_id`，恢复时直接跳过。
- `project_created` 或 `promotion_failed` 组恢复时复查原素材，并复用原 `project_id`，不得再次创建项目。
- `planned` 和明确的 `project_failed` 可以在用户确认后重试。
- API 请求已发出但结果未能可靠写入时标记 `outcome_uncertain`，必须先通过官方查询接口人工或程序化对账，禁止自动重试。

官方接口若没有幂等键，客户端不能承诺严格 exactly-once。运行日志的目标是避免已知成功状态被重复提交，并让不确定状态显式停住，而不是猜测请求是否成功。

## 校验和错误

提交前必须校验：

- 模板渠道为 `marketing`。
- 目标广告主与模板 `bindings.advertiser_id` 一致。
- 模板素材来源存在且受支持。
- Provider 返回的授权对象属于当前广告主。
- 达人授权状态有效且到期时间满足最短剩余天数。
- 素材状态允许投放。
- 用户选择的素材来自本次候选集合。
- 单元内所有素材来源相同。
- 同一达人素材单元内所有候选的 `creator_id` 相同，并与 `native_setting.aweme_id` 一致。
- 每单元素材数不超过模板值和官方上限中的较小值。
- payload 中使用的官方 ID 类型与来源适配器一致。

稳定错误码：

| 错误码 | 含义 |
| --- | --- |
| `material_source_missing` | 业务模板未绑定素材来源 |
| `material_source_mismatch` | 运行时素材与模板来源不一致 |
| `creator_material_not_authorized` | 达人素材不属于当前广告主授权范围 |
| `creator_authorization_expired` | 授权已过期或剩余时间不足 |
| `creator_material_unavailable` | 素材当前不可投放 |
| `mixed_material_sources` | 同一单元混入多种素材来源 |
| `material_selection_empty` | 筛选或复查后没有可用素材 |

接口权限不足、官方字段变化和分页不完整必须失败并报告，不得将部分列表当作完整候选集合自动创建。

## 查询和展示

默认在对话中以 Markdown 表格展示，不生成表格文件：

| 素材ID | 来源 | 达人 | 授权状态 | 授权到期 | 可投放 | 原因 |
| --- | --- | --- | --- | --- | --- | --- |

达人名称只用于展示，选择和提交使用官方 ID。用户没有明确要求时，不输出完整原始 API 响应。

现有素材消耗查询继续以账户当前投放单元为入口。能从单元响应可靠识别来源时补充来源列；官方响应无法可靠识别时显示 `UNKNOWN`，不得根据是否有达人名称猜测。

## 迁移

迁移必须幂等：

1. 版本高于 `3` 时拒绝；等于 `3` 时验证后原样返回；等于 `2` 时执行 v2 到 v3；低于 `2` 时先调用既有 v1 到 v2 迁移，再继续到 v3。
2. 默认模板骨架保持 `material_strategy` 为空。
3. 每个已有业务模板补充 `material_strategy.source_type: ACCOUNT_UPLOAD`，并保留 `display_name`、`bindings`、`copy_materials`、`created_from` 和所有不认识的非敏感元数据。
4. 对每个业务模板先按 v2 继承和覆盖规则计算有效 `defaults.max_videos_per_project`，再写入该模板的 `material_strategy.max_materials_per_unit`；若输入中已存在合法 v3 值，以 v3 值为准。旧字段只兼容读取一个版本，新写入不再产生。
5. 检查准确路径 `plan_templates.<name>.overrides.materials.video_ids` 和 `plan_templates.<name>.overrides.materials.video_cover_ids`，以及 adapter 注册的其他动态素材路径。字段非空时不自动激活迁移结果：生成预览，说明这些 ID 将从模板移除，并要求用户确认改用运行时选材；拒绝或非交互模式下返回 `legacy_material_selection_requires_confirmation`，原配置保持不变。
6. 用户确认后从 v3 业务模板删除全部动态素材 ID，只在迁移备份和非执行审计摘要中保留原值；v3 Provider 永远不读取这些备份。
7. 验证完整迁移结果后原子写入并保留备份。
8. 设置 `plan_template_schema_version: 3`。

任何迁移失败都不能留下半写入配置。`active_plan_template` 只有在对应模板完成迁移并通过完整性校验后才保留；需要素材迁移确认的活动模板在确认前继续由 v2 代码读取，不能被 v3 创建入口部分执行。

## 测试

测试不得读取真实配置、系统凭据或调用真实 Ocean Engine API。至少覆盖：

- v2 模板幂等迁移为上传素材模板。
- v1 到 v2 到 v3 链式迁移、v3 原样返回和高版本拒绝。
- 保留 `created_from` 与未知非敏感模板元数据。
- 非空旧动态素材 ID 必须确认，确认后从 v3 模板移除。
- 默认模板骨架保持不可直接执行业务创建。
- 新向导要求选择素材来源。
- 复制模板并切换来源时清理来源专属字段。
- 跨广告主复制时清空达人白名单。
- 两个 Provider 的分页、规范化和错误处理。
- 官方大整数 ID 无损处理。
- 达人授权有效、临期、过期和状态异常边界。
- 提交前复查发现部分素材失效。
- 禁止混合来源、广告主不匹配和来源不匹配。
- 两种 payload adapter 的固定快照测试。
- 每组五条和最后不足五条的批量分组。
- 原子批量运行日志、稳定组键和状态转换。
- promotion 失败时保留并复用 `project_id`，跳过已成功组。
- `outcome_uncertain` 必须对账且禁止自动重试。
- macOS、Windows 和 Linux 的完整 CI 矩阵。

## 发布和兼容

- 该功能随插件 `0.7.0` 发布。
- 现有上传素材命令和脚本参数在一个兼容版本内继续有效。
- `SKILL.md`、中英文 README、配置说明、命令说明和示例配置同步更新。
- 变更日志明确说明已有模板会自动解释为上传素材模板。
- 更新插件 cachebuster 并重新验证本地安装版本。
- 发布门槛为插件校验、Skill 校验、全部回归测试和 GitHub Actions 六组矩阵全绿。

## 实现前置条件

实现者必须先从巨量营销官方文档确认并记录：

- 达人授权素材列表接口路径。
- 请求分页、广告主和筛选字段。
- 素材、视频、内容、达人和授权对象各自的 ID 类型。
- 授权状态、授权范围和有效期字段。
- promotion create 中选择达人素材的字段结构。
- 是否需要额外的抖音号、星图、授权任务或内容 ID。
- 素材可投放状态的官方定义。

若官方文档与本设计中的字段名称不同，以官方 schema 为准，但 `MaterialProvider`、统一候选模型、模板来源绑定和提交前复查这四个架构约束保持不变。

## 验收标准

用户可以创建两个绑定同一广告主和商品、但素材来源不同的业务模板。选择达人素材模板创建计划时，插件只展示当前广告主已获授权且可投放的达人素材，清楚显示授权状态和到期时间；用户选择后，插件在提交前再次校验并使用官方达人素材字段创建计划。上传素材现有流程继续工作；完成迁移确认的已有模板按上传素材处理，且两种来源不会在同一单元中被混用。
