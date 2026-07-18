<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">面向 Codex 的巨量引擎投放与盯盘 Plugin</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="https://github.com/westng/ocean-watch/actions/workflows/ci.yml"><img src="https://github.com/westng/ocean-watch/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/westng/ocean-watch/releases"><img src="https://img.shields.io/github/v/release/westng/ocean-watch?display_name=tag&sort=semver" alt="GitHub Release"></a>
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

中文 | [English](README.en-US.md)

`ocean-watch` 通过巨量引擎官方 API 完成 OAuth 授权、账户管理、素材查询、计划创建、报表汇总和投放分析。Plugin 包含两个职责独立的 Skill，共用同一套 CLI，但不共享应用、Token、账户、模板或创建事务：

| Skill | 渠道 | 已支持 |
| --- | --- | --- |
| `ads-plan-monitor` | 巨量营销 | 授权、负责账户、上传/达人素材、模板、计划、报表、策略 |
| `qc-plan-monitor` | 巨量千川 | 授权、负责账户、商品/直播全域模板、达人与商品查询、计划处理、计划/素材报表 |

千川直播模板和创建流程已经开放；策略建议仍以只读分析为主。所有创建、删除和参数调整命令默认只预览，只有显式传入 `--submit` 才会调用在线写接口。

如果你只是日常使用，不需要记命令、参数或 Skill 名称。安装后直接在 Codex 中描述目标即可；Codex 会选择正确渠道、补问缺少的信息，并在写入计划前展示预览。

## 安装

需要 Codex CLI `0.144.1+` 和 Python `3.9+`：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建一个 Codex 任务。第一次可以直接说“帮我初始化巨量引擎盯盘”，不必手动执行后续业务命令。

首次使用时 Codex 会先检查本机环境；需要手动检查时运行：

```bash
ocean-watch setup doctor
```

完整流程见[快速开始](docs/getting-started.md)。安装阶段不会启动 OAuth；首次执行 `auth authorize` 时才会临时启动本地回调服务。

### 版本与发布

本项目以 Git 仓库作为 Codex Marketplace 来源，不上传 Plugin ZIP、wheel 或源码分发包等自定义 Release 资产。维护者在 GitHub Actions 中手动运行 `Release` 工作流后，工作流会校验版本、Changelog、Plugin 元数据和测试，并创建可固定安装的 Tag 与只含版本说明的 [GitHub Release](https://github.com/westng/ocean-watch/releases)。GitHub 自动显示的 `Source code (zip/tar.gz)` 是对应 Tag 的平台源码快照，不是项目构建资产。

每个 Release 页面的版本说明直接来自 `CHANGELOG.md` 中对应版本段落，不使用自动生成的提交列表。需要固定版本时，在注册 Marketplace 时指定 Tag：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

维护者流程见[发布指南](docs/releasing.md)。

## 普通用户使用案例

下面的内容都可以直接发给 Codex。示例中的账户名、模板名、商品和作品链接换成自己的即可。

### 案例一：第一次配置营销或千川

你可以说：

```text
帮我初始化巨量引擎盯盘，并授权巨量营销。
```

或：

```text
帮我检查千川使用环境，然后引导我完成授权。
```

Codex 会检查 Python、Codex 版本、安全凭据仓库和 OAuth 端口，然后引导你填写对应渠道的 App ID 与 Secret。授权完成后会反馈同步到的广告主；营销和千川需要分别授权一次。

### 案例二：查看负责账户今天的整体情况

先告诉 Codex 哪些账户由你负责：

```text
把千川账户 1234567890 加入我负责的账户，名称设为「品牌旗舰店」。
```

以后只需要说：

```text
帮我看一下我负责的所有账户今天消耗情况，按渠道汇总，并标出查询失败的账户。
```

Codex 会并发查询已启用账户，汇总消耗并隔离单账户失败。营销和千川的成交金额、ROI 口径不同，因此会分渠道展示，不会强行混算。

### 案例三：用今天上传的视频创建营销计划

```text
查询「夏季防晒」账户今天上传的视频，按每 5 条一个单元，用「巨量营销-1234567890-防晒霜-987654321-混剪素材」模板创建计划，先预览，不要提交。
```

Codex 会先查询素材，再展示广告主、模板、视频分组、预算、ROI 和计划数量。确认预览无误后可以继续说：

```text
确认，按刚才的预览提交。
```

没有明确确认时不会在线创建。若项目已创建但单元失败，结果会保留项目 ID，方便后续续建，避免重复创建项目。

### 案例四：使用达人授权素材投放

```text
查询抖音号 DOUYIN_SHOW_ID 当前授权给这个营销账户的视频，筛出仍在授权期内的素材，用原生素材模板先做创建预检。
```

Codex 会区分公开主页视频和当前广告主真正拥有投放授权的视频，只使用有效授权快照中的素材。批量任务会先列出可创建、已完成、可续建和被阻断的项目，再等待确认。

### 案例五：按抖音作品链接处理千川计划

第一次先创建商品全域模板：

```text
为千川账户 1234567890 创建一个「防晒霜」商品全域模板，商品 ID 是 987654321，预算 5000，目标 ROI 1.7。
```

以后把作品链接直接交给 Codex：

```text
用「防晒霜」千川模板检查下面 3 个抖音作品链接。达人没有计划就新建，已有计划只追加缺少的素材，先预检：
https://v.douyin.com/EXAMPLE1/
https://v.douyin.com/EXAMPLE2/
https://v.douyin.com/EXAMPLE3/
```

Codex 会通过官方接口确认达人授权、作品归属和商品匹配。无效链接、商品不匹配、未授权作品和已存在素材会跳过；默认只返回预检结果，确认后才执行新建或追加。

### 案例六：删除千川计划中的指定作品

```text
从千川计划 AD_ID 中移除这个抖音作品，先告诉我会删除哪些素材以及可能的联动影响，不要直接执行：
https://v.douyin.com/EXAMPLE/
```

Codex 会先把作品精确映射到计划素材，只允许删除自提素材，并展示千川多号或多商品场景下可能存在的联动影响。只有再次确认后才提交删除，并在提交后复查删除状态。

### 案例七：创建千川直播全域计划

```text
为千川账户 1234567890 的直播账号创建直播全域模板，先沿用默认投放设置；然后用该模板预览创建直播计划。
```

直播模板独立绑定广告主、直播账号名称和数值 `aweme_id`，不会混入商品全域模板的商品或作品素材字段。Codex 会先展示预算、出价方式、直播时段和智能选材设置，再等待提交确认。

### 案例八：查看报表并获得投放建议

```text
查询这个营销账户最近 7 天的素材消耗，列出消耗前 10、低转化高消耗素材，并给出停投、观察或放量建议。
```

```text
查询千川账户 1234567890 今天所有商品全域计划，汇总消耗、成交金额、订单和加权 ROI，再列出消耗前 10。
```

报表默认直接在对话中展示，不会自动写入本地文件。建议基于只读数据生成，不会自行修改计划。

## 使用时的确认规则

- 查询账户、素材、模板和报表属于只读操作，可直接执行。
- 创建、追加、删除和计划参数调整默认先预览；没有明确确认，不执行在线写入。
- 模板必须绑定正确渠道、广告主和商品，不能跨账户复用 Token 或素材。
- 一次任务可以有部分跳过或失败；Codex 会保留成功结果并单独说明失败原因。

## 高级：直接使用 CLI

普通用户无需使用 CLI。需要自动化脚本、精确传参或排错时，可以使用统一命令：

```text
ocean-watch
├── setup          # 环境检查与配置初始化
├── auth           # 营销/千川 OAuth 与 Token
├── accounts       # 负责账户及跨渠道消耗
├── templates      # 统一模板查询与营销模板
├── qc-templates   # 千川商品与直播模板
├── materials      # 营销素材
├── qc-materials   # 千川达人素材与作品检查
├── qc-products    # 千川商品查询
├── plans          # 单条与批量计划
├── qc-plans       # 千川计划查询与参数调整
├── runs           # 本机执行记录
├── reports        # 营销报表
├── qc-reports     # 千川全域计划与素材报表
├── discover       # 官方资产反查
└── mcp            # 开发文档 MCP
```

每个层级均支持 `--help`。完整参数与示例见 [CLI 参考](docs/cli.md)。

## 安全与隐私

- App Secret、Access Token、Refresh Token 和授权码不写入 Git 配置。
- macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。
- 用户配置和状态位于 `$CODEX_HOME/ads-plan-monitor/`。
- 官方业务请求只允许巨量官方 HTTPS 主机；可选千川作品解析服务只接收公开抖音链接。
- 报表、缓存、日志、任务清单和执行 journal 不属于开源仓库内容。

详见[安全说明](SECURITY.md)和[配置与授权](docs/configuration.md)。

## 开发者

无需安装即可运行统一入口：

```bash
python3 skills/ads-plan-monitor/run.py --help
python3 skills/qc-plan-monitor/run.py --help
```

也可以安装可编辑包：

```bash
python3 -m venv .venv
source .venv/bin/activate              # Windows: .venv\Scripts\activate
python3 -m pip install -e ".[dev]"
ocean-watch --help
```

核心代码位于 `skills/ads-plan-monitor/src/ocean_watch/`，两个 Skill 的入口分别位于 `skills/ads-plan-monitor/` 和 `skills/qc-plan-monitor/`。模块职责和依赖规则见[架构说明](docs/architecture.md)。

## 文档

从[文档中心](docs/README.md)开始，或直接查看：

- [快速开始](docs/getting-started.md)
- [CLI 参考](docs/cli.md)
- [配置与授权](docs/configuration.md)
- [架构说明](docs/architecture.md)
- [发布指南](docs/releasing.md)
- [贡献指南](CONTRIBUTING.md)
- [更新日志](CHANGELOG.md)

## 质量检查

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch
python3 scripts/release.py check
git diff --check
```

CI 在 Windows、macOS 和 Linux 的 Python `3.9`、`3.12` 上运行，并验证源码安装后的 CLI、首次初始化、Plugin 清单和两个 Skill 元数据。

## License

MIT
