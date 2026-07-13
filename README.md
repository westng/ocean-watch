<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">
  巨量引擎盯盘助手
</p>

<p align="center">
  面向 Codex 的开源 Plugin，通过官方 API 完成计划创建、素材报表、账户授权与盯盘分析
</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="skills/ads-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-ads--plan--monitor-4B5563" alt="Ads Plan Monitor Skill"></a>
  <a href="skills/ads-plan-monitor/scripts/"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="skills/ads-plan-monitor/references/official-api-notes.md"><img src="https://img.shields.io/badge/Ocean%20Engine-API%20%2B%20MCP-1677FF" alt="Ocean Engine API and MCP"></a>
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/Credentials-local%20store-6B7280" alt="Local credential store"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

中文默认 | [English](./README.en-US.md)

`ocean-watch` 是一个可安装到 Codex 的巨量引擎广告投放自动化 Plugin。插件内只有一个 `ads-plan-monitor` Skill，并在 Skill 内部处理首次向导、计划创建、数据查询和逻辑策略四类分支。

业务操作目前通过官方 Ocean Engine Marketing API 完成，并统一归属 `marketing`（巨量营销）渠道。Plugin 已具备渠道隔离底座；`qianchuan`（巨量千川）保留为待开发渠道，不会读取或复用营销应用、Token 和账户。开发文档、OpenAPI Schema 和 SDK 示例可通过巨量引擎官方开发文档 MCP 查询。真实业务配置与 OAuth 凭据保留在使用者自己的电脑上。

## 能力

| 场景 | 能力 |
| --- | --- |
| 首次使用 | 创建本地配置，检查 OAuth、Token、广告主账户和官方 MCP 状态 |
| 授权账户 | 按渠道保存独立应用和多份 OAuth 授权，根据官方 `account_id` 与目标广告主自动选择 Token |
| Token 管理 | API 调用前自动检查有效期，临近过期时刷新并保存轮换凭据 |
| 创建计划 | 按平台和商品模板生成项目与单元，确认后提交官方 API |
| 批量创建 | 获取当天上传视频，按 N 条一个单元分组，支持多账户并发 |
| 查询数据 | 查询单元、当前使用素材、视频素材库和素材维度报表 |
| 盯盘策略 | 基于消耗、ROI、转化等数据给出分析和处理建议 |
| 官方文档 | 通过官方 MCP 查询接口文档、Schema 和 SDK 示例 |

计划模板采用“默认创建骨架 + 业务模板”结构。默认模板只保存可跨业务复用的投放参数，不参与真实投放。新模板向导要求选择默认骨架或已有业务模板，并根据广告主和商品是否变化清理账户资产、商品资产、链接及文案；不完整模板可以保存为草稿，但不能激活。每个业务模板只绑定一个广告主、平台、流量来源和商品，多账户批量创建会为每个广告主单独解析模板。

## 安装

需要 Codex CLI 0.144.1 或更高版本，以及 Python 3.9 或更高版本：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后，新建一个 Codex 任务即可加载最新 Plugin。没有真实私有目录的干净开发副本也可以直接作为本地 marketplace：

```bash
codex plugin marketplace add "$(pwd)"
codex plugin add ocean-watch@ocean-watch
```

不要从包含本地 `config/`、`runs/` 或 `.venv/` 的工作副本执行本地安装；Codex 可能复制这些未跟踪目录。日常使用优先通过 GitHub marketplace 安装。

## 首次使用

在 Codex 中直接说：

```text
用 ads-plan-monitor 初始化配置
```

向导会创建 `~/.codex/ads-plan-monitor/config.json`，并给出 OAuth、Token、业务模板和官方 MCP 的后续状态。已有活动模板不完整时会提示补全，而不是重新创建模板。开发仓库中则优先使用被 Git 忽略的 `config/ads-plan-monitor/config.json`。

OAuth App ID、Secret、Access Token、Refresh Token 和 MCP `developer_id` 均写入本机系统凭据仓库，不写进项目配置，也不应粘贴到聊天中。

从旧版本升级时，先执行一次渠道迁移。现有 APP ID、Secret、Token、授权账户和模板全部迁入 `marketing`，不需要重新授权：

```bash
python3 skills/ads-plan-monitor/scripts/migrate_channels.py \
  --config config/ads-plan-monitor/config.json
```

迁移可安全重复执行；中断后再次运行会沿用同一迁移记录继续完成。若旧 Token 没有完整的官方账户映射，状态会显示 `pending_account_sync: true`，按输出中的 `authorization_id` 执行一次同步：

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --authorization-id <AUTHORIZATION_ID> \
  --sync-accounts
```

### OAuth

从仓库开发时，可以运行：

```bash
python3 skills/ads-plan-monitor/scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --set-app

python3 skills/ads-plan-monitor/scripts/oauth_local_authorize.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing
```

默认回调地址为 `http://127.0.0.1:8787/oauth/callback`，必须与开放平台应用配置完全一致。

同一营销应用可以完成多次授权。Plugin 会保留每份授权，并根据目标 `advertiser_id` 自动匹配包含它的官方授权账户；只有匹配有歧义时才需要传 `--auth-account-id`。所有业务命令默认使用 `--channel marketing`。

`qianchuan` 目前只预留独立渠道结构，尚未实现授权和业务 API；它不会读取或复用任何营销应用、Token、账户或模板。

### 官方 MCP

官方 MCP 地址要求每个用户自己的 `app_id` 与 `developer_id`。Plugin 不会在公开清单中硬编码个人参数，而是在首次使用时注册到当前用户的 Codex 配置：

```bash
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py
```

脚本从系统凭据仓库读取 `app_id`，安全提示输入 `developer_id`，先与官方服务完成握手并检查工具列表，再把本地 SSE→stdio 桥注册为 `oceanengine-developer-docs`。检查状态：

```bash
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py --status
```

Codex 配置只保存本地桥接脚本路径；包含 `app_id` 和 `developer_id` 的官方 URL 仅在桥进程内存中生成。状态输出不会显示 MCP URL 或标识符。MCP 未配置时，业务 API 功能仍可使用，Skill 会回退到仓库内的脱敏参考资料。

## 使用示例

```text
查询当前广告账户今天素材消耗前十
查询今天上传的视频素材
按今天上传的视频素材，每 5 条一个单元创建计划，先 dry-run
使用指定计划模板，拿这条视频素材创建一条计划
根据素材维度数据给我盯盘建议
查询 promotion/create 的官方字段和 OpenAPI Schema
```

真实创建计划属于写操作。Plugin 默认先做只读查询或 payload 预览，只有用户明确要求提交时才调用创建接口。

在开发 Plugin 或完善 Skill 的对话中，账户、商品和链接信息默认只作为功能测试案例。Codex 只修改仓库代码、公开示例、文档和测试，并使用临时配置验收；除非用户另行明确要求操作本地业务状态，否则不会修改真实 `config/`、查询真实账户或调用业务 API。

## 项目结构

```text
.
├── .agents/plugins/marketplace.json       # GitHub / 本地 marketplace 入口
├── .codex-plugin/plugin.json              # Codex Plugin 清单
├── skills/ads-plan-monitor/
│   ├── SKILL.md                           # 单一 Skill 的核心指令
│   ├── agents/                            # Skill UI 元数据
│   ├── assets/                            # 脱敏示例配置
│   ├── references/                        # API 与模板规则
│   └── scripts/                           # 授权、查询、创建和 MCP 向导
├── tests/                                 # 回归测试
├── docs/                                  # 安装、配置、命令和设计文档
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

不会进入仓库的本地目录：`config/`、`runs/`、`.venv/`、缓存、日志和临时输出。

## 文档

- [配置、OAuth 与 MCP](docs/configuration.md)
- [常用命令](docs/commands.md)
- [项目结构](docs/project-structure.md)
- [安全说明](SECURITY.md)
- [贡献指南](CONTRIBUTING.md)

## 开发检查

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile skills/ads-plan-monitor/scripts/*.py
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
python3 -m json.tool skills/ads-plan-monitor/assets/config.example.json >/dev/null
python3 -m unittest discover -s tests -v
git diff --check
```

## License

MIT
