<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">
  巨量引擎盯盘助手
</p>

<p align="center">
  面向 Codex 的开源 Plugin，通过官方 API 完成账户授权、素材查询、计划创建、报表汇总与投放分析
</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="skills/ads-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-ads--plan--monitor-4B5563" alt="Ads Plan Monitor Skill"></a>
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="skills/ads-plan-monitor/references/official-api-notes.md"><img src="https://img.shields.io/badge/Ocean%20Engine-Official%20API-1677FF" alt="Ocean Engine official API"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

中文默认 | [English](./README.en-US.md)

`ocean-watch` 是一个可安装到 Codex 的巨量引擎广告投放自动化 Plugin。仓库只包含一个 `ads-plan-monitor` Skill，并由该 Skill 在首次配置、授权、模板、素材、计划、报表和策略分支之间路由。

当前业务实现面向巨量营销（`marketing`），使用巨量引擎官方 API。巨量千川（`qianchuan`）已经预留独立渠道边界，但尚未实现，不会复用营销应用、Token 或账户。

## 核心能力

| 领域 | 能力 |
| --- | --- |
| 授权 | 本地 OAuth、多授权账户索引、自动 Token 刷新、广告主同步 |
| 模板 | 默认骨架、广告主/商品/平台/素材来源绑定、交互式创建与迁移 |
| 素材 | 账户上传视频、达人主页视频、达人合作授权视频与可用性校验 |
| 创建 | 上传素材和达人素材计划、dry-run、并发批量、失败续建 |
| 报表 | 当前单元素材关联、素材维度数据、消耗排行与自定义报表 |
| 策略 | 基于只读投放数据提供停投、观察、放量等运营建议 |
| 开发 | 官方文档 MCP、OpenAPI Schema 和 SDK 示例查询 |

## 安装 Plugin

需要 Codex CLI `0.144.1` 或更高版本，以及 Python `3.9+`：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后，新建一个 Codex 任务加载最新版本。然后可以直接说：

```text
用 ads-plan-monitor 初始化配置
查询当前广告账户今天素材消耗前十
按今天上传的视频素材，每 5 条一个单元创建计划，先预览
查询达人授权视频并使用达人素材模板创建计划
```

## 本地开发

仓库提供一个统一 CLI。无需安装即可运行：

```bash
python3 skills/ads-plan-monitor/run.py --help
python3 skills/ads-plan-monitor/run.py setup init --home-config
```

开发者也可以安装可编辑包，获得 `ocean-watch` 命令：

```bash
python3 -m venv .venv
source .venv/bin/activate              # Windows: .venv\Scripts\activate
python3 -m pip install -e .
ocean-watch --help
```

所有命令遵循 `ocean-watch <领域> <动作>`：

```bash
ocean-watch auth status --channel marketing
ocean-watch templates list
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch reports materials
ocean-watch plans create --plan-template TEMPLATE --video-id VIDEO_ID
```

创建命令默认只预览。只有显式传入 `--submit` 才会调用在线写接口。

## 安全边界

- App ID、Secret、Access Token、Refresh Token 和授权码不写入 Git 配置。
- macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。
- 业务配置默认位于 `~/.codex/ads-plan-monitor/config.json`；开发仓库可使用被忽略的 `config/ads-plan-monitor/config.json`。
- `config/`、`runs/`、缓存、日志和本地任务清单不属于开源包。
- 开发 Plugin 时不会读取真实业务配置或调用真实账户；只有用户明确要求真实执行时才进入业务模式。

详见 [安全说明](SECURITY.md) 和 [配置文档](docs/configuration.md)。

## 工程架构

项目采用标准 `src/` 包结构和单一 CLI：

```text
skills/ads-plan-monitor/
├── SKILL.md                    # Codex 路由与安全规则
├── assets/                     # 脱敏示例
├── references/                 # 按需加载的官方 API 说明
├── run.py                      # Plugin 内无需安装的统一入口
└── src/ocean_watch/
    ├── cli/                    # 命令路由和输出边界
    ├── core/                   # 配置、错误、锁和公共数据工具
    ├── api/                    # 唯一官方业务 API Client
    ├── auth/                   # OAuth、Token 和授权账户
    ├── templates/              # 模板模型、校验与向导
    ├── materials/              # 上传素材与达人素材
    ├── plans/                  # Payload、统一事务执行器和批量调度
    ├── reports/                # 报表查询、关联与聚合
    └── discovery/              # 账户资产反查
```

上传素材、达人素材、单条和批量创建共用 `PlanExecutor`。所有普通业务请求共用 `OceanEngineClient`，领域模块不自行实现 Header、URL 编码、超时或 HTTP 异常处理。

完整设计见 [架构说明](docs/architecture.md)。

## 文档

- [快速开始](docs/getting-started.md)
- [CLI 参考](docs/cli.md)
- [配置与授权](docs/configuration.md)
- [架构说明](docs/architecture.md)
- [项目结构](docs/project-structure.md)
- [贡献指南](CONTRIBUTING.md)
- [安全说明](SECURITY.md)
- [更新日志](CHANGELOG.md)

## 质量检查

```bash
PYTHONPATH=skills/ads-plan-monitor/src python3 -m compileall -q skills/ads-plan-monitor/src/ocean_watch
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
git diff --check
```

CI 在 Windows、macOS 和 Linux 的 Python `3.9`、`3.12` 上运行。

## License

MIT
