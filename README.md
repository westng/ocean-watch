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

### 版本与发布

本项目以 Git 仓库版本 Tag 作为 Codex Marketplace 安装源，并为同一提交创建使用中文 Changelog 的 GitHub Release。Release 工作流只验证固定提交、测试和五平台内置二进制，不修改仓库文件、不自动回推版本提交。普通代码变更继续记录在 `CHANGELOG.md` 的“未发布”段落，不会因为推送自动升级版本或创建 Release。

需要固定版本时，在注册 Marketplace 时指定 Tag：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

维护者流程见[发布指南](docs/releasing.md)。

## 普通用户使用 Q&A

下面的问法只是示意，不是固定口令。你可以使用口语、简称、错别字或上下文追问，Codex 会根据目标理解意图、选择对应能力，并在缺少必要信息时继续询问。

### 第一次配置营销或千川

**问：** 我第一次使用，能帮我检查环境并完成巨量营销授权吗？

**Ocean Watch：** 会检查 Codex、内置 Go CLI、安全凭据仓库和 OAuth 端口，再引导你配置对应渠道的 App ID 与 Secret。授权完成后会反馈同步到的广告主；巨量营销和巨量千川需要分别授权。只有千川作品链接解析额外依赖 Python `3.10+` 与固定版本 F2。

### 刷新当前授权用户的广告主

**问：** 我这个授权账号最近新增了几个广告主，帮我把本地权限更新到官方最新状态。

**Ocean Watch：** 会理解你要刷新当前 Marketing OAuth 用户的官方广告主覆盖，执行授权广告主同步，而不是要求你说出固定命令。同步只有在官方分页完整且验证成功后才会原子替换本地快照；若官方分页异常或返回不完整，会保留旧快照并明确报错。

### 查看负责账户和账户消耗

**问：** 把千川账户 `1234567890` 设为我负责的账户，名称叫“品牌旗舰店”。

**Ocean Watch：** 会把它加入本机账户簿。以后问“我负责的账户有哪些”“我常用的广告账户”等相近说法，只返回启用账户名单，不查询报表、不刷新 Token，也不把本地账户簿误当成官方授权广告主列表。

**继续问：** 那再看一下这些账户今天的消耗，按渠道汇总，并标出失败账户。

**Ocean Watch：** 会理解“这些账户”指刚才的负责账户，并发查询账户级报表、隔离单账户失败。营销和千川的成交金额、ROI 口径不同，会分渠道展示，不强行混算。

### 用上传视频创建营销计划

**问：** 查询“夏季防晒”账户今天上传的视频，每 5 条组成一个单元，用防晒霜混剪模板创建计划，先让我确认。

**Ocean Watch：** 会先展示广告主、模板、视频分组、预算、ROI 和计划数量，不会直接提交。你确认“按刚才的预览提交”后才会在线创建；若项目成功而单元失败，结果会保留项目 ID，供后续续建。

### 使用达人授权素材投放

**问：** 找出抖音号 `DOUYIN_SHOW_ID` 仍授权给这个营销账户的视频，用原生素材模板先做创建预检。

**Ocean Watch：** 会区分公开视频与当前广告主真正拥有投放授权的素材，只使用有效授权快照。批量预检会分别列出可创建、已完成、可续建和被阻断的项目，再等待确认。

### 按抖音作品链接批量处理千川计划

**问：** 为千川账户 `1234567890` 建一个防晒霜商品全域模板，商品 ID 是 `987654321`，预算 5000，目标 ROI 1.7。

**Ocean Watch：** 会分别询问商品全称和用于计划命名的商品简称，再校验模板与广告主、商品和渠道的绑定。默认计划名称为 `日期-达人名称-商品简称-类型-商务`；类型和商务不会固化在模板里，而是在每次可能新建计划时填写。

**继续问：** 用刚才的模板处理这些抖音作品。达人没有对应计划就新建，已有计划只追加缺少的素材，先预检。

**Ocean Watch：** 会先用 F2 批量解析作品公开元数据，再以得到的数字 UID 或有效的 30 天身份缓存，在目标广告主下定向调用千川官方接口确认达人授权、作品归属和商品匹配。缺失、失效或不匹配的单条作品会快速跳过，不会扫描全部授权达人。若需要新建计划，会先收集本次类型和商务，例如生成 `8.4-达人名称-防晒霜-随手po-刘研`；已有计划只追加缺少素材，不要求再次填写，也不改计划名称。确认执行后，成功明细固定返回 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`，跳过、官方查询不完整和失败原因单独说明。

### 删除千川计划中的指定作品

**问：** 我想从千川计划 `AD_ID` 中移除这个抖音作品，先告诉我会影响哪些素材，不要直接执行。

**Ocean Watch：** 会把作品精确映射到计划素材，只允许删除自提素材，并展示多账号或多商品场景下的联动影响。再次确认后才提交删除，提交后还会复查官方状态。

### 创建千川直播全域计划

**问：** 给千川账户 `1234567890` 的直播账号创建直播全域模板，先用默认设置预览一条直播计划。

**Ocean Watch：** 会使用独立的直播模板，不混入商品或作品素材字段，并先展示预算、出价方式、直播时段和智能选材设置，等你确认后再提交。

### 查看报表并获得投放建议

**问：** 看一下这个营销账户最近 7 天的素材表现，列出消耗前 10 和高消耗低转化素材，再给我优化建议。

**Ocean Watch：** 会在对话中展示只读报表，并给出停投、观察或放量建议，不会自行修改计划。

**继续问：** 千川账户 `1234567890` 今天的商品全域计划呢？

**Ocean Watch：** 会查询千川计划报表，汇总消耗、成交金额、订单和加权 ROI，并保持千川指标口径独立。

### 按业务对象查询千川全域与乘方数据

**问：** 看一下千川账户 `1234567890` 昨天包含乘方的整体消耗和成交。

**Ocean Watch：** 会识别这是单广告主的全域与乘方账户聚合，调用官方乘方账户接口；如果明确说“只看全域”，则改用全域账户接口。问“我负责的账户表现”时仍查询本机负责账户集合，不会误当成单广告主报表。

**继续问：** 商品 `987654321` 这周的消耗、GMV 和 ROI 呢？再按小时看看直播间 `ROOM_ID` 昨晚的效果。

**Ocean Watch：** 会先按商品维度查询效果数据，再按直播间和小时维度查询；如果只问“找这个商品或商品 ID”，则走商品资产查询。达人、计划、素材和可用报表字段也会按对象与上下文选择各自接口，不要求用户说出命令或固定口令。

## 使用时的确认规则

- 查询账户、素材、模板、计划详情和报表属于只读操作，可直接执行。
- 创建、追加、删除和计划参数调整默认先预览；没有明确确认，不执行在线写入。
- 模板必须绑定正确渠道、广告主和商品，不能跨渠道、跨广告主复用 Token 或素材。
- “我负责的账户”只读取本地账户簿；刷新当前授权用户可访问的广告主属于独立的官方授权同步。
- 消耗、GMV、ROI、订单或日期表现才调用官方报表；跨渠道只合计可比较的消耗。
- 一次任务可以有部分跳过或失败；Codex 会保留成功结果，并单独说明跳过、失败和官方查询不完整项。
- 强制 Presentation 结果按 CLI 返回的 `rendered_markdown` 展示，不能擅自删列、改口径或隐藏空表头。

## 自动化与排错

普通用户应直接用自然语言表达投放目标，不需要了解内部命令分组。需要脚本化、精确传参或排错时再使用稳定 CLI；完整 action 和参数见[CLI 参考](docs/cli.md)。macOS/Linux 使用 `skills/*/run`，Windows 使用 `skills\\*\\run.cmd`；两个入口调用相同的 Go CLI 和 JSON/Presentation 合同。

环境、授权状态、广告主映射和本地执行记录可分别使用 `setup doctor`、`auth status`、`auth mappings` 和 `runs` 检查。千川作品链接问题可先用 `qc-materials inspect-work` 查看 F2 映射结果；但最终授权、归属、商品匹配和可投放性始终以目标广告主下的千川官方接口为准。

## 安全与隐私

- App Secret、Access Token、Refresh Token 和授权码不写入 Git 配置或普通项目配置。
- macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。
- 用户配置、授权快照、缓存和执行记录位于 `$CODEX_HOME/ads-plan-monitor/`。
- 官方业务请求只允许巨量官方 HTTPS 主机。
- F2 只读取抖音公开作品元数据，不下载素材、不创建数据库、不自动读取浏览器 Cookie；商品提示不能替代千川官方授权、归属和商品校验。
- 报表、缓存、日志、任务清单和执行 journal 不属于开源仓库内容。

详见[安全说明](SECURITY.md)和[配置与授权](docs/configuration.md)。

## 开发者

无需安装 Go 或 Python 业务包即可运行内置 CLI：

```bash
skills/ads-plan-monitor/run --help
skills/qc-plan-monitor/run --help
```

Windows 使用对应的 `run.cmd`。源码开发和测试需要 Go `1.26.5`；只有 F2 相关流程需要 Python `3.10+` 与 F2 `0.0.1.7`。

## 文档

从[文档中心](docs/README.md)开始，或直接查看：

- [快速开始](docs/getting-started.md)
- [CLI 参考](docs/cli.md)
- [配置与授权](docs/configuration.md)
- [架构说明](docs/architecture.md)
- [发布指南](docs/releasing.md)
- [贡献指南](CONTRIBUTING.md)
- [安全说明](SECURITY.md)
- [更新日志](CHANGELOG.md)

## 质量检查

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
git diff --check
```

CI 在 Linux、macOS 和 Windows 执行 Go 单元测试、静态检查并验证对应平台内置 CLI，在 Linux 执行 F2 映射、版本一致性和分发合同检查。五平台确定性构建验证由 Release 工作流执行；该工作流不会修改文件或向 `main` 回推版本提交。

本地构建当前平台或五个平台内置 CLI：

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
```

## License

MIT
