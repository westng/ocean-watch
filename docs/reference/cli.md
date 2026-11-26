# CLI 参考

## 不可变操作与预发布

`operations prepare/get/confirm/execute/reconcile` 提供千川准备、状态查询、独立 Broker 确认、精确执行和只读对账，参数与恢复规则见 [Operation 合同](operation-approval.md)。`operations upgrade-state` 默认只预览状态，`--submit` 才执行受保护备份与显式迁移。预发布版本阻断旧在线写入，新执行必须使用精确操作 ID、版本、快照摘要及审批引用。

## 入口

macOS/Linux：

```bash
skills/ads-plan-monitor/run <domain> <action> [options]
skills/qc-plan-monitor/run <domain> <action> [options]
skills/xt-plan-monitor/run <domain> <action> [options]
```

上述路径相对于仓库根。AI 在任意用户项目中调用时，必须从已加载的 `SKILL.md` 目录构造并引用启动器绝对路径，不能直接在会话目录执行 `./run`。

Windows 使用对应的 `run.cmd`。三个 Skill 启动器只负责选择当前平台的内置 Go 二进制，命令和 JSON 合同相同。星图 Skill 使用 `--channel star_map`，仅接入授权与账户能力，不能把营销或千川命令当成星图计划 / 报表接口。

完整参数以运行时帮助为准：

```bash
skills/ads-plan-monitor/run --help
skills/qc-plan-monitor/run qc-reports --help
skills/qc-plan-monitor/run plans batch-qianchuan-works --help
```

## 命令分组

| 分组 | 主要用途 |
| --- | --- |
| `setup` | 环境检查、初始化、配置就绪校验 |
| `auth` | App、OAuth、Token、广告主同步与脱敏映射 |
| `accounts` | 负责账户名单与跨渠道账户表现 |
| `templates` | 跨渠道模板列表、详情、创建、校验、删除 |
| `qc-templates` | 千川商品/直播模板专属操作 |
| `materials` | 营销账户上传与达人授权素材 |
| `qc-materials` | 千川达人、作品和公开作品链接检查 |
| `qc-products` | 千川可投商品列表与搜索 |
| `plans` | 营销创建/批量/参数更新与千川创建/作品批处理/素材删除 |
| `qc-plans` | 千川计划列表、详情、素材、参数更新和当日计划绑定审计/绑定 |
| `reports` | 营销素材、项目、自定义报表与字段发现 |
| `qc-reports` | 千川计划、素材、账户、商品、直播间、达人和自定义报表 |
| `discover` | 营销项目、广告、DPA、事件、目标与地域资产反查 |
| `runs` | Plugin 管理的本地执行记录 |

## 输出与退出码

非交互命令的 stdout 只输出一个 UTF-8 JSON 文档；人类直接使用的旧交互向导包含提示文本，AI 应使用下述结构化模板入口。成功使用退出码 `0`；参数或配置问题通常为 `2`；业务、官方 API 或环境失败为非零。凭据不会写入 JSON、stderr 或日志。

当结果包含：

```json
{"presentation": {"required": true, "rendered_markdown": "..."}}
```

Skill 必须原样展示 `rendered_markdown`，保留列顺序、日期范围、失败详情和指标口径。除非用户明确要求，不输出原始 JSON。

## 只读与写入

- 名单、模板查询、素材查询、报表和计划详情为只读。
- 模板创建/删除、计划创建/追加/删除和预算/ROI/状态调整默认 dry-run。
- 当前 alpha 的在线写入仅支持可信千川 Operation 执行；旧计划创建、删除和设置变更的 `--submit` 被拒绝。
- `qc-plans bind` 只写本地绑定、不调用官方写接口，但会改变后续在线计划选择，因此同样要求显式 `--submit` 和精确 `--group-id` / `--ad-id`；`qc-plans binding-audit` 为只读。
- 先展示广告主、对象 ID、端点、载荷摘要和阻断项，再接受提交确认。
- 写入结果不确定时先回读官方状态对账，不跨端点盲目重试。

## 常用示例

```bash
# 环境和授权
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run auth authorize --channel marketing --print-url --no-open
# Replace one existing authorization grant without creating a second identity
skills/ads-plan-monitor/run auth authorize --channel marketing --authorization-id AUTHORIZATION_ID --print-url --no-open
skills/qc-plan-monitor/run auth sync-accounts --channel qianchuan

# 负责账户
skills/ads-plan-monitor/run accounts list
skills/ads-plan-monitor/run accounts report --start-date YYYY-MM-DD --end-date YYYY-MM-DD

# 模板和素材（CLI 开发/诊断入口；Codex/Claude Code/豆包工作普通模板读取使用 MCP）
skills/ads-plan-monitor/run templates list --channel marketing
skills/qc-plan-monitor/run qc-materials inspect-work --work-url DOUYIN_WORK_URL

# 千川作品计划预检
skills/qc-plan-monitor/run plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL

# 报表
skills/ads-plan-monitor/run reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
skills/qc-plan-monitor/run qc-reports products --advertiser-id ADVERTISER_ID --report-mode uni
```

## 千川作品链接

`qc-materials inspect-work` 和 `plans batch-qianchuan-works` 接受普通链接、Markdown 链接或带口令片段的文本。包装层规范化短链并批量调用 F2；F2 只提供公开身份和商品提示。创建前仍以可见抖音号定向查询官方授权达人，校验数字 UID，再按同一达人和模板商品验证作品。

CLI 的批量入口在兼容期内继续接受旧的逐行 `--work-url` 参数并输出弃用提示；新 MCP 入口只接受结构化 `items[]`，不保留旧顶层批次类型或商务字段。CLI 和 MCP 最终都调用同一个批次 Application Service，输入顺序、分组身份、预检快照和安全边界保持一致。兼容参数的移除必须另行记录在版本变更中。

混合商品或账户使用重复 `--work-item` JSON 参数（MCP 使用 `items[]`），每行可指定 `product_id/product_name`、`advertiser_id`、`plan_template`，省略全局 `--plan-template`。入口根据启用模板的商品/账户绑定唯一匹配后，按广告主和模板拆批；歧义、缺少商品或公开元数据包含多个商品的行保留待确认。每个子批次独立核验和生成预检 ID，一个账户失败不阻断其他账户。没有跨账户统一执行 ID，每个成功预检须独立准备 Operation、保护确认并执行。字段、示例和流程图见[千川混合商品作品建计划流程](qianchuan-work-routing.md)。

F2 失败时，可在结构化行中补充准确 `creator_douyin_id` 和命名 `creator_name`，由官方定向查询取得唯一数字 UID 后继续归属、商品核验。昵称只用于命名，不能代替身份；未知解析错误不能直接归因为反爬或未授权。

批量成功结果固定展示 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`。跳过、官方查询不完整和失败原因放在表外，空结果也保留表头。

## 结构化模板创建

`templates create --input-json JSON` 或 `templates create --input-file -` 接收一个严格 JSON 对象，后者从标准输入读取。业务字段全部放在对象中，不与旧 `--channel`、`--template-type` 等向导参数混用。

```bash
skills/ads-plan-monitor/run templates create --input-json '{}'
```

缺少渠道、模板类型或业务参数时返回 `status=needs_input`、`missing_fields` 和适用的 `choices`，不写入配置、不读取交互输入。`channel` 为 `marketing` 或 `qianchuan`；千川必须显式选择 `template_kind=product|live`，营销必须选择 `marketing.material_source_type`。`source_template` 使用 `default` 骨架或准确来源名称/ID。分渠道参数及合成示例见 [营销模板](../../skills/ads-plan-monitor/references/template-create.md) 和 [千川模板](../../skills/qc-plan-monitor/references/template-create.md)。

完整请求返回 `status=preview`、`template`、`state_version`、`preview_digest` 和本地授权覆盖状态。审阅后重复相同 JSON，并提供 `--submit --expected-revision REV --preview-digest DIGEST` 才会保存；输入、配置或来源漂移会拒绝提交。最终 Compare-and-Swap 仍校验预览配置版本，不覆盖并发修改、不自动重试。`status=created` 且 `changed=true` 仅表示本地模板已保存，不授予在线计划写入权限。

JSON 拒绝未知字段、重复键、尾随值和超长输入。渠道隔离、广告主策略与本地授权范围均在 Application 中复核；没有授权的渠道可先创建模板，但必须报告 `authorization_status=unverified`。WorkBuddy 保持 MCP-only，尚未提供模板创建工具。
