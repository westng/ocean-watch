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

中文 | [English](README.en-US.md)

Ocean Watch 通过巨量引擎官方 API 完成 OAuth、负责账户、模板、素材、计划、报表和投放分析。两个 Skill 共用一套内置 Go CLI，但渠道凭据、授权、账户、模板和创建事务严格隔离：

| Skill | 渠道 | 能力 |
| --- | --- | --- |
| `ads-plan-monitor` | 巨量营销 | 授权、负责账户、上传/达人素材、模板、计划、报表与策略 |
| `qc-plan-monitor` | 巨量千川 | 授权、负责账户、商品/直播模板、达人作品、全域计划与全域/乘方报表 |

所有在线创建、追加、删除和参数调整默认 dry-run，只有用户明确确认后才增加 `--submit`。

## 当前实现

Go 主运行时切换已经完成，不再处于双运行时或 Shadow 迁移阶段。当前仓库和 Plugin 只保留一套广告业务实现：OAuth、授权广告主同步、负责账户、模板、素材、计划、报表、本地状态和写入对账全部由内置 Go CLI 执行。旧 Python 业务包、Go Prototype、Shadow 路由、运行时选择、业务回退、MCP 兼容入口和迁移 Gate/Bootstrap 资产均不再分发。

Python 不是第二套业务运行时，只用于启动固定版本 F2 `0.0.1.7`，解析千川作品链接中的抖音公开元数据。F2 返回的信息只作为定向查询提示，最终达人授权、作品归属、商品匹配和可投放性仍由目标广告主下的千川官方 API 确认。

## 运行方式

```text
Codex → Skill → run/run.cmd → 内置 Go CLI → 巨量引擎官方 API
                                      └→ Python 3.10+ → F2 0.0.1.7
                                         仅解析抖音公开作品元数据
```

- 广告业务只有 Go 一套运行时，没有业务回退或运行时选择。
- Plugin 已内置 macOS Intel/Apple Silicon、Linux x86_64/ARM64 和 Windows x86_64 二进制；用户无需安装 Go。
- Python 只在千川作品链接流程中运行固定版本 F2，不承载授权、账户、模板、计划或报表逻辑。
- F2 返回的是公开身份和商品提示；达人授权、作品归属、商品匹配和可投放性仍由千川官方接口复核。

详见[架构说明](docs/architecture.md)。

## 安装

需要 Codex CLI `0.144.1+`。如需千川作品链接解析，还需 Python `3.10+` 与 F2 `0.0.1.7`。

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建 Codex 任务，直接描述目标即可，例如：

- “帮我初始化巨量营销并完成授权。”
- “让当前授权用户下的广告主列表和官方最新权限保持一致。”
- “查询我负责的营销和千川账户今天消耗。”
- “用这些抖音作品为千川商品模板创建计划，先预检。”

首次使用可运行：

```bash
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run setup init --home-config
```

Windows 将 `run` 替换为 `run.cmd`。完整流程见[快速开始](docs/getting-started.md)。

## 使用边界

- “我负责的账户有哪些”只读取本地账户簿，不刷新 Token 或查询报表。
- 消耗、GMV、ROI、订单或日期表现才调用官方报表；跨渠道只合计可比较的消耗。
- 模板必须绑定正确渠道、广告主和商品，不能跨渠道复用 Token 或素材。
- 批量任务保留成功结果，并单独说明跳过、失败和官方查询不完整项。
- 强制 Presentation 结果按 CLI 返回的 `rendered_markdown` 展示，不能擅自删列或改口径。

## 安全

App Secret、Access Token、Refresh Token 和授权码不写入项目配置。macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。用户配置、授权快照、缓存和执行记录位于 `$CODEX_HOME/ads-plan-monitor/`，不属于仓库内容。

详见[配置与授权](docs/configuration.md)和[安全说明](SECURITY.md)。

## 开发

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
git diff --check
```

构建当前平台或五个平台内置 CLI：

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
```

更多内容见[文档中心](docs/README.md)、[贡献指南](CONTRIBUTING.md)和[更新日志](CHANGELOG.md)。
