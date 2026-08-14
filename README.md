<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">面向 Codex 的巨量引擎投放与盯盘 Plugin</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="https://github.com/westng/ocean-watch/actions/workflows/ci.yml"><img src="https://github.com/westng/ocean-watch/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/westng/ocean-watch/tags"><img src="https://img.shields.io/github/v/tag/westng/ocean-watch?sort=semver" alt="Git Tag"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

<p align="center">
  <a href="README.md">README</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="LICENSE">MIT license</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

中文 | [English](README.en-US.md)

Ocean Watch 让投放人员直接在 Codex 中用自然语言管理巨量营销与巨量千川。它通过巨量引擎官方 API 完成授权、账户、素材、模板、计划、报表和投放分析，并在任何在线写入前展示预检结果、等待明确确认。

## 能做什么

| Skill | 渠道 | 主要能力 |
| --- | --- | --- |
| `ads-plan-monitor` | 巨量营销 | OAuth、授权广告主同步、负责账户、上传与达人授权素材、模板、计划、报表和策略分析 |
| `qc-plan-monitor` | 巨量千川 | OAuth、授权广告主同步、负责账户、商品与直播模板、达人作品、全域计划、全域与乘方报表 |

两个 Skill 共用一套 Go 业务实现，但渠道凭据、授权用户、广告主、模板、素材和写入事务严格隔离。

## 为什么选择 Ocean Watch

- **自然语言驱动**：按业务目标理解口语、简称和上下文追问，不要求记忆固定命令。
- **官方数据为准**：广告业务调用巨量引擎官方 API；公开作品信息不能替代官方授权、归属和商品校验。
- **默认安全**：创建、追加、删除和计划参数调整默认只预检，确认后才提交在线写入。
- **凭据隔离**：Secret 和 Token 使用操作系统安全存储，不写入普通项目配置、输出或日志。
- **结果可追踪**：批量任务保留成功结果，分别说明跳过、失败和官方查询不完整项；写入不确定时先回读对账。

## 快速开始

需要 Codex CLI `0.144.1+`。普通营销和千川流程直接使用 Plugin 内置运行时；只有千川抖音作品链接解析额外需要 Python `3.10+` 与 F2 `0.0.1.7`。

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建 Codex 任务，直接描述目标即可。完整的环境检查、渠道配置与 OAuth 流程见[快速开始](docs/getting-started.md)。

## 可以直接这样问

- **初始化授权**：“帮我检查环境并完成巨量营销授权。”
- **同步权限**：“这个授权账号新增了广告主，把本地权限更新到官方最新状态。”
- **查看经营数据**：“查询我负责的营销和千川账户今天消耗，按渠道汇总并标出失败账户。”
- **创建营销计划**：“查询今天上传的视频，每 5 条组成一个单元，用现有模板创建计划，先让我确认。”
- **处理千川作品**：“用这些抖音作品处理商品全域计划；没有对应计划就新建，已有计划只追加缺少素材，先预检。”
- **分析投放效果**：“看最近 7 天的素材表现，找出高消耗低转化素材并给出优化建议。”

这些问法只是示意，不是固定口令。Ocean Watch 会根据上下文选择渠道和能力，并在缺少广告主、模板、商品、日期或写入确认时继续询问。

## 操作与安全边界

- 查询账户、素材、模板、计划详情和报表属于只读操作。
- 创建、追加、删除和预算、ROI、状态调整必须先预览；没有明确确认，不执行在线写入。
- “我负责的账户”读取本机账户簿；刷新当前授权用户可访问的广告主属于独立的官方授权同步。
- 营销与千川的 GMV、ROI 等指标保持各自官方口径，跨渠道只汇总可比较的消耗。
- F2 只解析抖音公开作品元数据，不下载素材、不创建数据库、不自动读取浏览器 Cookie；最终可投放性始终由目标广告主下的千川官方接口确认。
- 用户配置、授权快照、缓存、报表和执行记录位于本机 `$CODEX_HOME/ads-plan-monitor/`，不属于开源仓库内容。

更多信息见[配置与授权](docs/configuration.md)和[安全说明](SECURITY.md)。

## 运行方式与支持范围

```text
Codex → Skill → 本地 stdio MCP ─┐
             └→ run/run.cmd ────┴→ Go Application Service → 巨量引擎官方 API
                                                       └→ Python 3.10+ → F2 0.0.1.7
                                                          仅解析抖音公开作品元数据
```

- 广告业务只有一套 Go Application/Domain 实现，MCP 与 CLI 是其上的两个入口，不存在第二套业务运行时或静默业务回退。
- 本地模板列表和精确详情使用 MCP 的 `list_templates`、`get_template`；其他尚未工具化的能力通过内置 Go CLI 执行。
- Plugin 已内置 macOS Intel/Apple Silicon、Linux x86_64/ARM64 和 Windows x86_64 CLI，普通用户无需安装 Go。
- 当前 MCP 固定启动清单只完成 macOS Apple Silicon 开发验收；五平台 CLI 分发不等于五平台 MCP 已完成安装验收。
- Python 只参与千川作品链接的公开元数据解析，不承载授权、账户、模板、计划或报表逻辑。

技术边界与当前实现见[架构说明](docs/architecture.md)。

## 文档

| 读者 | 入口 |
| --- | --- |
| 普通用户 | [Documentation](docs/README.md) · [快速开始](docs/getting-started.md) · [配置与授权](docs/configuration.md) |
| 脚本与排错 | [CLI 参考](docs/cli.md) |
| 开发者 | [架构说明](docs/architecture.md) · [Contributing](CONTRIBUTING.md) |
| 项目与安全 | [Security](SECURITY.md) · [Changelog](CHANGELOG.md) · [发布指南](docs/releasing.md) · [MIT license](LICENSE) |

## 鸣谢

- [F2](https://github.com/Johnserf-Seed/f2)：为千川作品链接流程提供抖音公开作品元数据解析能力。
- [巨量引擎开放平台 Go SDK](https://github.com/oceanengine/ad_open_sdk_go)：为巨量营销与巨量千川官方 API 集成提供 Go SDK 支持。

## License

[MIT](LICENSE)
