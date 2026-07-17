# 快速开始

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skills: ads-plan-monitor, qc-plan-monitor

## 1. 安装 Plugin

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建 Codex 任务。巨量营销使用 `ads-plan-monitor`，巨量千川使用 `qc-plan-monitor`。初始化不会调用业务写接口，安装阶段也不会触发 OAuth。

## 2. 初始化配置

先检查运行环境：

```bash
ocean-watch setup doctor
```

该命令只读取本机运行信息，不读取 Token、不调用官方 API，也不启动 OAuth。阻断项包含 Python 版本、操作系统、安全凭据后端和回调端口；Codex CLI 缺失或版本过低会作为独立警告返回。没有 Python 时，先安装 Python `3.9+` 并重新打开 Codex。

从源码运行：

```bash
python3 skills/ads-plan-monitor/run.py setup init --home-config
```

可编辑安装后运行：

```bash
ocean-watch setup init --home-config
```

默认用户配置位于：

```text
$CODEX_HOME/ads-plan-monitor/config.json
```

未设置 `CODEX_HOME` 时，上述根目录默认为 `~/.codex`。

开发仓库可以使用 `config/ads-plan-monitor/config.json`。该目录已被 Git 忽略。

## 3. 完成 OAuth

巨量营销：

```bash
ocean-watch auth authorize --channel marketing
```

巨量千川：

```bash
ocean-watch auth authorize --channel qianchuan
ocean-watch auth status --channel qianchuan
```

在 Codex 对话中，千川授权、账户和计划请求应由 `qc-plan-monitor` 处理；不要交给 `ads-plan-monitor`。

首次授权或当前渠道尚未配置应用时，插件会打开一个本地安全页面，在同一个表单中填写 App ID 和 Secret；提交后直接跳转官方授权。凭据只写入操作系统凭据仓库，不要把密钥写入配置或聊天。

主动更换某个渠道的应用时，可单独打开同一张配置表单：

```bash
ocean-watch auth set-app --channel qianchuan
```

本地回调地址必须与开放平台设置完全一致。默认值：

```text
http://127.0.0.1:8787/oauth/callback
```

这个地址只用于开放平台登记和官方授权完成后的回跳，不是授权入口，不要在安装后直接打开。运行 `auth authorize` 时插件才会临时监听该端口，命令必须保持运行直到回调完成。在 Codex 中固定使用 `--print-url --no-open`，把输出中的临时 `start_url` 交给用户，由用户在目标巨量账户所在的浏览器分组中打开；Codex 不替用户选择或启动默认浏览器。返回地址后 Codex 必须继续等待回调，授权完成时主动反馈授权主体数、已验证广告主数、同步状态与映射结果，不需要用户再次追问。

营销和千川应用可以配置同一个回调地址。插件通过官方原样返回的 OAuth `state` 区分渠道：`AD.<随机值>` 表示巨量营销，`QC.<随机值>` 表示巨量千川，并校验完整随机值防止串号。

授权完成后，插件会同步官方授权主体，按角色展开真实广告主，并保存非敏感账户索引。同步失败时 Token 不会丢失，可按状态中的 `authorization_id` 执行 `auth sync-accounts` 重试。

当前千川已开放授权、刷新 Token、账户发现、商品全域模板、按商品过滤的达人视频查询、根据作品链接新建、追加或删除商品全域计划自提素材，以及官方 MCP 全域计划报表；策略和直播模板仍未开放。

## 配置负责账户

授权账户可能很多，负责账户只保存用户日常管理的子集：

```bash
ocean-watch accounts add \
  --channel qianchuan \
  --advertiser-id ADVERTISER_ID \
  --name ACCOUNT_NAME
ocean-watch accounts list
```

之后在 Codex 中说“帮我看下我负责的账户消耗情况”，会执行 `accounts report`，并发查询所有启用的营销和千川账户。账户名称、ID 和启用状态属于本机非敏感业务配置，不进入开源仓库。

## 4. 检查就绪状态

```bash
ocean-watch auth status --channel marketing
ocean-watch setup validate --mode query
```

查询就绪后即可查询素材和报表。创建计划还需要一个完整、已激活且绑定目标广告主的业务模板。

千川商品全域计划消耗可直接查询：

```bash
ocean-watch qc-reports plans --advertiser-id ADVERTISER_ID
```

命令默认查询当天并显示消耗前十，总计仍覆盖全部分页计划。千川 OAuth Token 会在调用官方业务 MCP 前自动刷新，不需要单独配置 MCP API Key。

## 5. 创建业务模板

```bash
ocean-watch templates create
```

向导会要求：

1. 先选择巨量营销或巨量千川，并显示对应授权状态。
2. 巨量营销继续选择混剪素材或原生素材；巨量千川进入商品全域模板类型。
3. 选择该渠道、该模式的默认模板骨架或已有业务模板作为来源。
4. 明确广告主、平台、商品、预算、净成交 ROI、性别、年龄、产品卖点和渠道专属参数。
5. 按归属变化清理不能跨账户或跨商品复用的资产。
6. 预览逐字段差异和完整性校验。
7. 用户确认后才写入；激活需要单独确认。

渠道尚未授权时仍可先创建模板；授权状态只用于提示，不会替用户默认渠道或阻止模板草稿创建。

默认模板只用于创建新模板，不能直接投放。所有业务模板按 `渠道-广告账户ID-商品名-商品ID-模版类型` 自动命名。营销模版类型为“混剪素材”或“原生素材”。默认骨架使用商品库主图（`DPA`），标准向导不要求填写图片 ID。高级 `CUSTOM` 商品图配置仍必须遵守官方接口的账户图片 ID 要求。

真实业务模板没有当前或默认状态。创建营销计划时显式传入模板名称，创建千川计划时显式传入模板 ID；未选择模板时命令会返回结构化错误和可用模板列表。

千川商品全域模板使用独立向导：

```bash
ocean-watch qc-templates create
```

它绑定千川广告主、产品和最多 30 个商品 ID，名称使用 `巨量千川-广告账户ID-商品名-商品ID-商品全域`。默认参数为控成本、ROI `1.7`、预算 `5000`、智能优惠券开启、长期投放和净成交 ROI。模板不保存达人或素材 ID。

## 6. 先查询，再创建

查询当天上传视频：

```bash
ocean-watch materials videos --mode library-get --date today --fetch-all
```

预览一条上传素材计划：

```bash
ocean-watch plans create \
  --plan-template TEMPLATE \
  --video-id VIDEO_ID
```

确认后增加 `--submit`。达人素材使用 `materials creator` 和 `plans create-creator`；批量达人任务先用 `plans batch-creator --preflight` 展示断点续跑范围、素材阻断项和项目容量风险，确认后再改为 `--submit`。项目容量只能由官方创建接口最终确认。

先查询与模板商品匹配的千川达人视频：

```bash
ocean-watch qc-materials creator-videos \
  --plan-template TEMPLATE_ID \
  --douyin-id DOUYIN_SHOW_ID \
  --creator-name CREATOR_NAME
```

插件先从商品全域可投抖音号列表严格解析数值 `aweme_id`，再按模板中的每个商品 ID 调用千川视频接口。官方商品过滤不匹配的视频会被排除，重复作品会合并并保留 `matched_product_ids`。默认只在对话中输出，不写文件。

千川商品全域模板可生成基础 dry-run payload：

```bash
ocean-watch plans create-qianchuan \
  --plan-template TEMPLATE_ID \
  --name PLAN_NAME
```

该低层命令默认 dry-run。模板本身不含素材，因此模板单独提交会以 `runtime_creator_materials` 阻止在线写入。完整的作品链接流程使用：

```bash
ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL_1 \
  --work-url DOUYIN_WORK_URL_2
```

如需加速预检，先把私有解析地址写入本机配置：

```bash
ocean-watch setup work-metadata \
  --endpoint https://YOUR_PRIVATE_HOST/PATH \
  --home-config
```

插件只向本机配置的服务发送公开抖音链接，提前取得作品、达人和商品提示。非空商品 ID 未命中模板绑定的任一商品时，会在读取投放凭据和官方素材查询前直接跳过，不能新建计划或追加素材；商品匹配或为空仍必须通过千川官方接口复核。未配置或使用 `--no-link-metadata-api` 时，回退到受限短链跳转和完整官方查询。

官方复核完成后，达人没有计划时按模板新建；已有计划（包括暂停）时只追加未存在的作品素材。默认 dry-run，确认真实执行后增加 `--submit`。无效链接、未授权作品、商品不匹配和已存在素材会跳过，整批结束后统一返回结果。原始商品和直播 payload 示例仍位于 `skills/qc-plan-monitor/assets/`。

删除某个计划中的自提作品素材：

```bash
ocean-watch plans remove-qianchuan-work \
  --advertiser-id ADVERTISER_ID \
  --ad-id AD_ID \
  --work-url DOUYIN_WORK_URL
```

命令默认预检，会展示作品 ID、计划 `material_id`、素材选择类型和官方多号/多商品联动删除风险。只有增加 `--submit` 才删除 `CUSTOM` 素材，并且结果必须复查为 `DELETED`。

## 7. 查看投放数据

```bash
ocean-watch reports materials
```

默认结果输出到终端，由 Codex 在对话中整理为 Markdown 表格。只有显式传入 `--out` 或 `--csv-out` 才写文件。
