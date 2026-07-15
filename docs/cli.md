# CLI 参考

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skills: ads-plan-monitor, qc-plan-monitor

## 入口

安装 Python 包后使用 `ocean-watch`。Plugin 内无需安装时可从对应 Skill 启动：

```bash
python3 skills/ads-plan-monitor/run.py
python3 skills/qc-plan-monitor/run.py
```

下文统一写作 `ocean-watch`。所有业务命令支持 `--config PATH`；不指定时按环境变量、开发配置、用户配置顺序解析。

## 命令树

```text
ocean-watch
├── setup
│   ├── init
│   └── validate
├── auth
│   ├── set-app
│   ├── authorize
│   ├── status
│   ├── refresh
│   ├── sync-accounts
│   └── migrate
├── accounts
│   ├── list
│   ├── add
│   ├── remove
│   ├── enable
│   ├── disable
│   └── report
├── templates
│   ├── list
│   ├── create
│   ├── migrate
│   └── set-copy
├── qc-templates
│   ├── list
│   ├── create
│   └── migrate
├── materials
│   ├── videos
│   ├── creator
│   ├── images
│   └── products
├── qc-materials
│   └── creator-videos
├── plans
│   ├── create
│   ├── create-creator
│   ├── create-qianchuan
│   ├── batch-qianchuan-works
│   ├── remove-qianchuan-work
│   ├── batch-upload
│   └── batch-creator
├── reports
│   ├── materials
│   ├── schema
│   └── custom
├── qc-reports
│   └── plans
├── discover
│   ├── projects
│   ├── promotions
│   ├── dpa
│   ├── events
│   ├── deep-bids
│   ├── goals
│   └── cities
└── mcp
    ├── configure
    └── status
```

每个动作支持独立 `--help`：

```bash
ocean-watch plans batch-creator --help
```

## 负责账户

负责账户是用户主动维护的常用账户，不等于 OAuth 返回的全部授权账户：

```bash
ocean-watch accounts add \
  --channel qianchuan \
  --advertiser-id ADVERTISER_ID \
  --auth-account-id AUTH_ACCOUNT_ID \
  --name ACCOUNT_NAME
ocean-watch accounts list
ocean-watch accounts disable --channel qianchuan --advertiser-id ADVERTISER_ID
ocean-watch accounts enable --channel qianchuan --advertiser-id ADVERTISER_ID
ocean-watch accounts remove --channel qianchuan --advertiser-id ADVERTISER_ID
```

`--auth-account-id` 是可选的；当同一广告主出现在多个 OAuth 授权中时，用它固定账户所属授权。重复执行 `accounts add` 仅更新名称或显式传入的授权主体，不会重新启用已禁用账户。

查询所有启用账户当天消耗：

```bash
ocean-watch accounts report
ocean-watch accounts report --channel marketing
ocean-watch accounts report --channel qianchuan --start-date YYYY-MM-DD --end-date YYYY-MM-DD
```

默认跨渠道并发查询。营销使用无维度的 `BASIC_DATA` 账户聚合报表，千川使用商品全域计划报表汇总；单账户结果统一字段名，并附带 `metric_basis` 说明官方指标来源。总消耗可跨渠道相加；营销 `in_app_order_gmv` 与千川含券 ROI2 支付 GMV 口径不同，因此混合查询的顶层 `total_gmv`、`weighted_roi` 为 `null`，实际值按 `channel_summaries` 分渠道展示。单账户失败不会中止其他账户。只读请求会对业务临时错误 `40100`、`51010`，HTTP `429`/部分 `5xx`，以及明确标记可重试的超时或连接错误额外尝试两次，默认等待 1 秒、2 秒。只有显式传入 `--out` 才写文件。

## 初始化与授权

```bash
ocean-watch setup init --home-config
ocean-watch setup validate --mode all
ocean-watch auth authorize --channel marketing
ocean-watch auth status --channel marketing
ocean-watch auth refresh --channel marketing
ocean-watch auth sync-accounts --channel marketing
```

千川使用独立应用完成授权与账户发现：

```bash
ocean-watch auth authorize --channel qianchuan
ocean-watch auth status --channel qianchuan
ocean-watch auth sync-accounts --channel qianchuan --authorization-id AUTHORIZATION_ID
```

首次执行 `auth authorize` 时，如果当前渠道没有应用配置，浏览器会打开一张本地表单，同屏收集 App ID 和 Secret，保存后直接跳转官方 OAuth。需要主动更换应用时使用 `auth set-app --channel CHANNEL`，它会打开相同表单但不发起授权。

营销和千川流程共用默认回调地址，通过 OAuth `state` 的 `AD`/`QC` 前缀区分渠道；用户无需维护两条回调路径。若账户同步失败，授权保留为 pending 状态，可用最后一条命令重试；确认迁移账户归属时增加 `--rebind-existing`。

千川支持 `auth`、`qc-templates`、`qc-materials`、`qc-reports`、`plans create-qianchuan`、`plans batch-qianchuan-works` 和 `plans remove-qianchuan-work`。营销计划和报表命令不会回退使用千川授权。

一般业务调用会自动刷新 Token；只有排错或主动验证时才需要 `auth refresh`。

## 模板

```bash
ocean-watch templates list
ocean-watch templates create
ocean-watch templates migrate --confirm-remove-legacy-materials
ocean-watch templates set-copy --template TEMPLATE --title TITLE
ocean-watch templates set-copy --template TARGET --from-template SOURCE
```

新业务模板只能通过 `templates create` 交互式向导创建。`set-copy` 只修改标题文案，不复制链接、账户资产或投放参数。

千川商品全域模板使用独立命令：

```bash
ocean-watch qc-templates list
ocean-watch qc-templates create
ocean-watch qc-templates migrate
```

向导从不可投放的默认骨架或已有千川商品模板复制创建，绑定广告主、产品和 1–30 个商品 ID。多个商品 ID 使用 `/` 分隔，名称使用 `广告主ID-商品全域-产品-商品ID`。模板只保存投放参数和商品归属，不保存达人、视频、图片或渠道信息。

## 上传素材

```bash
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch materials videos --mode ad-get --video-id VIDEO_ID
ocean-watch materials videos --mode cover-suggest --video-id VIDEO_ID
ocean-watch materials images --mode library-get
ocean-watch materials products --product-id PRODUCT_ID
```

## 达人素材

查询合作授权视频：

```bash
ocean-watch materials creator --aweme-id AWEME_ID
```

查询公开主页视频：

```bash
ocean-watch materials creator --source homepage --aweme-id AWEME_ID
```

主页可见与合作授权是两种事实。创建原生计划时只接受当前广告主有效授权快照中的素材。

## 千川达人素材

```bash
ocean-watch qc-materials creator-videos \
  --plan-template TEMPLATE_ID \
  --douyin-id DOUYIN_SHOW_ID \
  --creator-name CREATOR_NAME
```

`--douyin-id` 填抖音 App 中用户可见的抖音号。命令只使用模板绑定的千川广告主授权，先通过商品全域可投抖音号接口严格解析数值 `aweme_id`，再按模板中的每个商品 ID 查询视频。官方商品过滤排除不匹配视频；结果按作品去重并记录 `matched_product_ids`。默认输出 JSON 到终端，只有显式传入 `--out PATH` 才写文件。

## 单条创建

上传素材 dry-run：

```bash
ocean-watch plans create \
  --plan-template TEMPLATE \
  --video-id VIDEO_ID
```

达人素材 dry-run：

```bash
ocean-watch plans create-creator \
  --plan-template CREATOR_TEMPLATE \
  --item-id ITEM_ID
```

显式增加 `--submit` 才执行在线创建。项目创建成功但单元失败时，结果包含 `project_id` 和 `failure_stage: promotion_create`，可使用 `--promotion-only --project-id PROJECT_ID` 续建。

千川全域计划 dry-run：

```bash
ocean-watch plans create-qianchuan \
  --plan-template TEMPLATE_ID \
  --name PLAN_NAME
```

也可用 `--payload-file`、`--payload-json JSON` 或 `--payload-file -` 读取官方 payload；三种来源只能选择一个。`--advertiser-id` 可补充缺失的广告主 ID，但不能与 payload 或模板绑定冲突。在线提交增加 `--submit`，插件只解析该广告主的千川授权；成功返回 `data.ad_id`。

千川创建直接调用 `/v1.0/qianchuan/uni_aweme/ad/create/`，不是营销的项目加单元两步事务。商品模板生成不含素材的基础 payload，并以 `runtime_creator_materials` 阻止模板单独在线提交；完整的运行时素材注入由下方作品链接批量命令处理。原始官方 payload 示例位于 `skills/qc-plan-monitor/assets/qianchuan-*-plan.example.json`。

## 批量创建

千川作品链接批量任务：

```bash
ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL_1 \
  --work-url DOUYIN_WORK_URL_2 \
  --concurrency 4
```

命令跟随抖音短链并提取作品 ID，通过官方接口批量确认授权达人和模板商品匹配，再按数值 `aweme_id` 分组。达人没有商品全域计划时按模板新建；已有计划（包括暂停）时只调用素材追加接口，预算、ROI、状态和名称保持不变。计划素材已存在、链接无效、达人未授权或商品不匹配时跳过，并在整批结束后统一反馈。默认 dry-run，真实写入增加 `--submit`；只有显式传入 `--out` 才写文件。

按作品链接删除计划自提素材：

```bash
ocean-watch plans remove-qianchuan-work \
  --advertiser-id ADVERTISER_ID \
  --ad-id AD_ID \
  --work-url DOUYIN_WORK_URL
```

命令先查询计划素材，把作品 `aweme_item_id` 精确映射为计划内层 `material_id`。仅 `material_select_type=CUSTOM` 可删除，智选素材跳过；默认只预检，增加 `--submit` 后每批最多删除 100 条并重新查询 `DELETED` 状态。官方接口在多号或多商品场景下可能同时删除关联投放，预检结果会显示该提示。

按当天上传视频分组：

```bash
ocean-watch plans batch-upload \
  --date today \
  --videos-per-unit 5 \
  --plan-template TEMPLATE
```

多账户任务使用 `--accounts` 和重复的 `--account-template ADVERTISER_ID=TEMPLATE`。账户与组均有独立并发上限。

达人批量任务使用运行时 JSON 清单：

```bash
ocean-watch plans batch-creator \
  --jobs-file /path/to/creator-jobs.json \
  --concurrency 4
```

格式见 `skills/ads-plan-monitor/assets/creator-batch-jobs.example.json`。每个任务必须记录商品匹配结论和证据。提交时写入本机非敏感 journal，重复执行会跳过已完成任务，并从已创建项目续建失败单元。

## 报表

```bash
ocean-watch reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
ocean-watch reports schema --data-topic MATERIAL_DATA
ocean-watch reports custom \
  --data-topic MATERIAL_DATA \
  --dimension material_id \
  --metric stat_cost
```

默认不写表格文件。Codex 应在对话中使用 Markdown 表格展示汇总和排行。

千川商品全域计划报表通过官方 MCP 查询：

```bash
ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD \
  --top 10
```

默认日期为当天，营销目标为商品全域，计划范围为 `UNI_PROJECT`，并按消耗降序返回前十。`--top 0` 返回全部报表行；汇总始终基于所有分页结果。命令使用目标广告主绑定的千川 OAuth Token，调用官方 Streamable HTTP MCP。金额和 ROI 只读取全域报表返回值，计划列表补充计划状态、成本保障、出价方式、ROI 目标出价、预算、名称和业务归属，不推算其内部固定精度金额。输出同时包含总消耗、有消耗计划数、整体成交金额、订单数、加权支付 ROI、1 小时净成交金额和加权净成交 ROI。

报表分页、计划 ID 和七个必需指标任一缺失、重复或非法时，命令拒绝返回不完整汇总。汇总先使用原始 `Decimal` 值跨全部页面计算，最后才按展示精度舍入。`--status ALL` 会保留历史财务行；计划列表无法补齐其元数据时，行内 `metadata_available=false`，汇总中的 `metadata_missing_count` 同步计数，名称、状态、成本保障、出价和预算保持为空。指定具体状态时必须解析到计划元数据，否则整次查询失败，避免错误筛选。

## 资产反查与 MCP

```bash
ocean-watch discover projects --name NAME
ocean-watch discover promotions --project-id PROJECT_ID
ocean-watch discover cities --city-csv cities.csv
ocean-watch mcp configure
ocean-watch mcp status
```

`mcp configure/status` 管理的是开发文档 MCP。千川业务报表使用独立的官方业务 MCP 传输层，由 `qc-reports` 在内存中注入自动刷新的千川 Token，不把 Token 写入 Codex MCP 配置。
