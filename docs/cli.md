# CLI 参考

## 入口

macOS/Linux：

```bash
skills/ads-plan-monitor/run <domain> <action> [options]
skills/qc-plan-monitor/run <domain> <action> [options]
```

Windows 使用对应的 `run.cmd`。两个入口只负责选择当前平台的内置 Go 二进制，命令和 JSON 合同相同。

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
| `templates` | 跨渠道模板列表、详情、创建、迁移、校验、删除 |
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

stdout 只输出一个 UTF-8 JSON 文档。成功使用退出码 `0`；参数或配置问题通常为 `2`；业务、官方 API 或环境失败为非零。凭据不会写入 JSON、stderr 或日志。

当结果包含：

```json
{"presentation": {"required": true, "rendered_markdown": "..."}}
```

Skill 必须原样展示 `rendered_markdown`，保留列顺序、日期范围、失败详情和指标口径。除非用户明确要求，不输出原始 JSON。

## 只读与写入

- 名单、模板查询、素材查询、报表和计划详情为只读。
- 模板创建/删除、计划创建/追加/删除和预算/ROI/状态调整默认 dry-run。
- 在线写入必须显式 `--submit`；千川计划删除还要求 `--confirm-delete`。
- `qc-plans bind` 只写本地绑定、不调用官方写接口，但会改变后续在线计划选择，因此同样要求显式 `--submit` 和精确 `--group-id` / `--ad-id`；`qc-plans binding-audit` 为只读。
- 先展示广告主、对象 ID、端点、载荷摘要和阻断项，再接受提交确认。
- 写入结果不确定时先回读官方状态对账，不跨端点盲目重试。

## 常用示例

```bash
# 环境和授权
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run auth authorize --channel marketing --print-url --no-open
skills/qc-plan-monitor/run auth sync-accounts --channel qianchuan

# 负责账户
skills/ads-plan-monitor/run accounts list
skills/ads-plan-monitor/run accounts report --start-date YYYY-MM-DD --end-date YYYY-MM-DD

# 模板和素材（CLI 开发/诊断入口；Codex 普通模板读取使用 MCP）
skills/ads-plan-monitor/run templates list --channel marketing
skills/qc-plan-monitor/run qc-materials inspect-work --work-url DOUYIN_WORK_URL

# 千川作品计划预检
skills/qc-plan-monitor/run plans batch-qianchuan-works \
  --advertiser-id ADVERTISER_ID \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL

# 报表
skills/ads-plan-monitor/run reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
skills/qc-plan-monitor/run qc-reports products --advertiser-id ADVERTISER_ID --report-mode uni
```

## 千川作品链接

`qc-materials inspect-work` 和 `plans batch-qianchuan-works` 接受普通链接、Markdown 链接或带口令片段的文本。包装层规范化短链并批量调用 F2；F2 只提供公开身份和商品提示。创建前仍以可见抖音号定向查询官方授权达人，校验数字 UID，再按同一达人和模板商品验证作品。

CLI 的批量入口继续接受旧的逐行 `--work-url` 参数作为兼容层，并输出弃用提示；新 MCP 入口只接受结构化 `items[]`，不保留旧顶层批次类型或商务字段。CLI 和 MCP 最终都调用同一个批次 Application Service，输入顺序、分组身份、预检快照和安全边界保持一致。

批量成功结果固定展示 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`。跳过、官方查询不完整和失败原因放在表外，空结果也保留表头。
