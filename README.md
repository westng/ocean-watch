<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">
  巨量引擎盯盘助手
</p>

<p align="center">
  面向 Codex 的开源 Skill，通过官方 Ocean Engine Marketing API 完成计划创建、素材查询、报表汇总与盯盘策略分析
</p>

<p align="center">
  <a href="SKILL.md"><img src="https://img.shields.io/badge/Codex-Skill-111827" alt="Codex Skill"></a>
  <a href="scripts/"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="references/official-api-notes.md"><img src="https://img.shields.io/badge/Ocean%20Engine-API-1677FF" alt="Ocean Engine API"></a>
  <a href="docs/configuration.md"><img src="https://img.shields.io/badge/OAuth-local-10B981" alt="Local OAuth"></a>
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/Credentials-local%20store-6B7280" alt="Local credential store"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

中文默认

`ocean-watch` 是一个可安装到 Codex 的巨量引擎广告投放自动化 Skill。它通过官方 Ocean Engine Marketing API，在本机完成 OAuth 授权、广告计划创建、素材查询、素材维度报表汇总和盯盘策略分析，适合投手、运营和增长团队在本地安全调用官方 API。

`ocean-watch` 不是网页后台，也不是第三方投放平台；它不接管广告账户，不存储云端业务数据，真实配置和 OAuth 凭据都保留在使用者自己的电脑上。

## What It Does

| 场景 | 能力 |
| --- | --- |
| 首次使用 | 创建本地配置、检查 OAuth 状态、提示缺失字段 |
| 授权 | 使用本机 `127.0.0.1` 回调完成 OAuth，凭据写入系统凭据仓库 |
| 创建计划 | 按商品/平台模板生成项目和单元 payload，确认后提交官方 API |
| 批量创建 | 获取当天上传视频素材，按 N 条素材一个单元分组创建，支持多账户并发 |
| 查询数据 | 查询账户单元、当前使用素材、视频素材库和素材维度报表 |
| 盯盘策略 | 基于素材和单元数据输出消耗、ROI、转化等维度的分析建议 |

## Quick Start

克隆仓库后，把 Skill 链接到 Codex 的 skills 目录：

```bash
mkdir -p ~/.codex/skills
ln -s "$(pwd)" ~/.codex/skills/ads-plan-monitor
```

运行环境需要 Python 3.9 或更高版本，只使用 Python 标准库。

创建本地配置：

```bash
mkdir -p config/ads-plan-monitor
cp assets/config.example.json config/ads-plan-monitor/config.json
```

编辑 `config/ads-plan-monitor/config.json`，填入广告账户、商品模板、落地页、监测链接、城市、素材等业务字段。不要把 OAuth secret 或 token 写进这个文件。

保存 App ID 和 Secret 到本机凭据仓库：

```bash
python3 scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --set-app
```

完成官方 OAuth 授权：

```bash
python3 scripts/oauth_local_authorize.py \
  --config config/ads-plan-monitor/config.json
```

检查配置和授权状态：

```bash
python3 scripts/first_run.py \
  --config config/ads-plan-monitor/config.json
```

## Usage With Codex

在 Codex 里可以直接这样说：

```text
用 ads-plan-monitor 初始化配置
查询当前广告账户今天素材消耗前十
查询今天上传的视频素材
按今天上传的视频素材，每 5 条一个单元创建计划，先 dry-run
使用某个计划模板，拿这条视频素材创建一条计划
根据素材维度数据给我盯盘建议
```

真实创建计划属于写操作。Skill 的默认策略是先做只读查询或 payload 预览，只有用户明确要求提交时才调用创建接口。

## Project Layout

```text
.
├── SKILL.md                  # Codex 读取的技能指令
├── agents/                   # Codex UI 元数据
├── assets/                   # 脱敏示例配置
├── references/               # 运行时按需读取的规则和 API 笔记
├── scripts/                  # API 调用、授权、查询和创建脚本
├── docs/                     # 给人看的安装、配置、命令和结构文档
├── README.md                 # 项目首页
├── CONTRIBUTING.md           # 贡献规范
├── SECURITY.md               # 安全说明
└── LICENSE                   # 开源许可证
```

不会进入仓库的本地目录：

```text
config/                               # 真实业务配置，本地私有
runs/                                 # API 运行产物和调试输出
.venv/                                # Python 虚拟环境
```

## Documentation

- [配置与授权](docs/configuration.md)
- [常用命令](docs/commands.md)
- [项目结构](docs/project-structure.md)
- [安全说明](SECURITY.md)
- [贡献指南](CONTRIBUTING.md)

## Security Model

项目配置只保存非密钥业务参数。OAuth App Secret、Access Token 和 Refresh Token 通过 `credential_store.py` 写入本机凭据仓库：

- macOS: Keychain
- Windows: DPAPI-protected user-local file
- Linux: Secret Service (`secret-tool`)

没有安全后端时，脚本默认拒绝写入凭据，不会静默降级为明文文件。仅在受限开发环境中可显式设置 `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1`，此时凭据以明文写入 `~/.codex/ads-plan-monitor/credentials.json`，不建议用于生产账户。

`config/`、`runs/`、`.venv/`、日志、临时文件和本地 fallback 凭据都被 `.gitignore` 排除。

## Development Checks

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile scripts/*.py
python3 -m json.tool assets/config.example.json >/tmp/ocean-watch-config-check.json
python3 -m unittest discover -s tests -v
git diff --check
```

## License

MIT
