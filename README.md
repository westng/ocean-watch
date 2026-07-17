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
  <a href="skills/qc-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-qc--plan--monitor-2563EB" alt="QC Plan Monitor Skill"></a>
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="skills/ads-plan-monitor/references/official-api-notes.md"><img src="https://img.shields.io/badge/Ocean%20Engine-Official%20API-1677FF" alt="Ocean Engine official API"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

中文默认 | [English](./README.en-US.md)

`ocean-watch` 是一个可安装到 Codex 的巨量引擎广告投放自动化 Plugin，包含两个职责独立的 Skill：`ads-plan-monitor` 负责巨量营销，`qc-plan-monitor` 负责巨量千川。两者共享工程化 CLI 运行时，但不会混用授权、账户、模板或创建事务。

巨量营销（`marketing`）已支持授权、素材、计划、报表和策略。巨量千川（`qianchuan`）已支持独立应用授权、Token 刷新、真实广告主发现、商品全域模板、按商品过滤的达人视频查询、根据抖音作品链接新建、追加或删除商品全域计划自提素材，以及通过官方 MCP 查询全域计划消耗；千川策略和直播模板仍未开放。两个渠道不会复用应用、Token、账户、模板或创建事务。

## 核心能力

| 领域 | 能力 |
| --- | --- |
| 巨量营销 | `ads-plan-monitor`：授权、模板、上传/达人素材、计划、报表和策略 |
| 巨量千川 | `qc-plan-monitor`：授权、商品全域模板、达人视频、作品链接计划处理与全域计划报表 |
| 授权 | 营销/千川独立本地 OAuth、多授权账户索引、自动 Token 刷新 |
| 负责账户 | 本地跨渠道常用账户簿、启停管理、并发消耗汇总与失败隔离 |
| 模板 | 默认骨架、广告主/商品/平台/素材来源绑定、交互式创建与迁移 |
| 素材 | 账户上传视频、营销达人授权视频、千川按商品过滤达人视频与可用性校验 |
| 计划 | 营销上传/达人素材计划；千川作品链接新建、追加或删除全域素材；dry-run、显式在线提交 |
| 报表 | 营销素材维度与自定义报表；千川全域计划消耗、成交金额、订单与 ROI |
| 策略 | 基于只读投放数据提供停投、观察、放量等运营建议 |
| 开发 | 官方文档 MCP、OpenAPI Schema 和 SDK 示例查询 |

所有业务模板统一按 `渠道-广告账户ID-商品名-商品ID-模版类型` 自动命名。营销业务模板由向导绑定广告主、商品、平台和素材模式；创建骨架使用官方 DPA 商品库主图，不要求用户填写图片 ID，向导会明确预览预算、净成交 ROI、性别和年龄后再保存。提交前会从官方接口校验转化资产和 DPA 字段；必要时仅复用同广告主、同商品投放的商品主图，候选不唯一或无可靠来源时在创建项目前阻断。

默认模板只是创建骨架，真实业务模板没有“当前”或“默认”状态；创建计划时必须明确指定模板。

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
用 qc-plan-monitor 授权千川并预览商品全域计划
用 qc-plan-monitor 查询抖音号下与模板商品匹配的视频
用 qc-plan-monitor 按抖音作品链接预检删除指定千川计划素材
用 qc-plan-monitor 查询指定千川账户今天的全域计划消耗
帮我看下我负责的账户今天消耗情况
```

安装阶段不会打开或监听 OAuth 回调地址。首次授权时由 `auth authorize` 临时启动本地服务并打开正确入口；`http://127.0.0.1:8787/oauth/callback` 只用于开放平台登记和接收官方回调，不是需要手动访问的网页。

首次使用先检查环境：

```bash
ocean-watch setup doctor
```

检测覆盖 Python `3.9+`、Windows/macOS/Linux、Codex CLI、安全凭据仓库和 OAuth 回调端口。若尚未安装 Python，Codex 会先通过系统命令探测并停止后续流程，提示用户安装 Python；插件不会静默安装运行环境。

### 开发者条件

- 使用 SDK 需要首先注册成为巨量引擎开发者，请参考[开发者快速入门文档](https://open.oceanengine.com/labels/7/docs/1696710498372623)
- 使用 SDK 需要先拥有 API 的访问权限，所有 SDK 的使用与应用拥有的权限组相关联

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
ocean-watch auth authorize --channel qianchuan
ocean-watch accounts add --channel qianchuan --advertiser-id ADVERTISER_ID --name ACCOUNT_NAME
ocean-watch accounts report
ocean-watch templates create
ocean-watch qc-templates create
ocean-watch qc-materials creator-videos --plan-template TEMPLATE_ID --douyin-id DOUYIN_SHOW_ID
ocean-watch plans batch-qianchuan-works --plan-template TEMPLATE_ID --work-url DOUYIN_WORK_URL
ocean-watch plans remove-qianchuan-work --advertiser-id ADVERTISER_ID --ad-id AD_ID --work-url DOUYIN_WORK_URL
ocean-watch templates list
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch reports materials
ocean-watch qc-reports plans --advertiser-id ADVERTISER_ID
ocean-watch plans create --plan-template TEMPLATE --video-id VIDEO_ID
ocean-watch plans create-qianchuan --payload-file QIANCHUAN_PAYLOAD.json
ocean-watch setup work-metadata --endpoint https://YOUR_PRIVATE_HOST/PATH --home-config
```

`templates list` 会一次从本地配置列出巨量营销和巨量千川模板，不调用官方接口。可用 `--channel` 筛选渠道，或用 `--include-details` 查看完整配置。

创建命令默认只预览。批量达人创建提供独立 `--preflight`，会结合断点日志列出已完成、待创建、续建和阻断项；项目容量由官方创建接口最终确认。只有显式传入 `--submit` 才会调用在线写接口。

千川作品链接解析服务是可选的本机配置，开源仓库不包含真实地址。配置后只向该服务发送公开抖音链接，并读取作品 ID、公开抖音号、数值 UID 和商品 ID；不发送广告主 ID、Token、模板或本地状态。接口返回非空商品 ID 且未命中模板绑定的任一商品时，该作品会在读取投放凭据前直接跳过，不能新建计划或追加素材；商品命中或为空仍必须经过千川官方接口复核。未配置或使用 `--no-link-metadata-api` 时，自动回退到受限短链跳转和完整官方查询。请求与响应字段见[配置文档](docs/configuration.md#千川作品解析服务)。

## 安全边界

- App ID、Secret、Access Token、Refresh Token 和授权码不写入 Git 配置。
- macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。
- 用户配置、授权状态和回退凭据统一位于 `$CODEX_HOME/ads-plan-monitor/`；未设置 `CODEX_HOME` 时默认为 `~/.codex`。开发仓库可使用被忽略的 `config/ads-plan-monitor/config.json`。
- 官方业务 API、OAuth 和 MCP 只允许巨量官方 HTTPS 主机，传输层拒绝重定向并限制响应大小。
- 可选千川作品解析地址只保存在本机配置，不进入仓库；启用后只发送公开抖音链接，不发送凭据、广告账户或模板数据。
- `config/`、`runs/`、缓存、日志和本地任务清单不属于开源包。
- 开发 Plugin 时不会读取真实业务配置或调用真实账户；只有用户明确要求真实执行时才进入业务模式。

详见 [安全说明](SECURITY.md) 和 [配置文档](docs/configuration.md)。

## 工程架构

项目采用两个业务 Skill 和一个共享 `src/` CLI 运行时：

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
    ├── accounts/               # 用户负责账户簿与管理命令
    ├── templates/              # 模板模型、校验与向导
    ├── materials/              # 上传素材与达人素材
    ├── plans/                  # Payload、统一事务执行器和批量调度
    ├── reports/                # 报表查询、关联与聚合
    └── discovery/              # 账户资产反查
skills/qc-plan-monitor/
├── SKILL.md                    # 千川触发、授权与创建规则
├── assets/                     # 千川脱敏 payload 示例
├── references/                 # 千川官方 API 说明
└── run.py                      # 复用共享 CLI 运行时
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
python3 -m pip install -e ".[dev]"
PYTHONPATH=skills/ads-plan-monitor/src python3 -m compileall -q skills/ads-plan-monitor/src/ocean_watch
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch
python3 -m build
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
git diff --check
```

CI 在 Windows、macOS 和 Linux 的 Python `3.9`、`3.12` 上运行。发布门禁同时构建 sdist 与 wheel，并分别用 Python `3.9`、`3.12` 从 wheel 隔离安装，在临时 `CODEX_HOME` 中执行首次初始化，验证打包资源和安装态 CLI。

## License

MIT
