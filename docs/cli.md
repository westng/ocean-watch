# CLI 参考

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## 入口

安装 Python 包后使用 `ocean-watch`。Plugin 内无需安装时使用：

```bash
python3 skills/ads-plan-monitor/run.py
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
├── templates
│   ├── list
│   ├── create
│   ├── migrate
│   └── set-copy
├── materials
│   ├── videos
│   ├── creator
│   ├── images
│   └── products
├── plans
│   ├── create
│   ├── create-creator
│   ├── batch-upload
│   └── batch-creator
├── reports
│   ├── materials
│   ├── schema
│   └── custom
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

## 初始化与授权

```bash
ocean-watch setup init --home-config
ocean-watch setup validate --mode all
ocean-watch auth set-app --channel marketing
ocean-watch auth authorize --channel marketing
ocean-watch auth status --channel marketing
ocean-watch auth refresh --channel marketing
ocean-watch auth sync-accounts --channel marketing
```

`auth authorize` 的营销和千川流程共用默认回调地址，通过 OAuth `state` 的 `AD`/`QC` 前缀区分渠道；用户无需维护两条回调路径。

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

## 批量创建

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

## 资产反查与 MCP

```bash
ocean-watch discover projects --name NAME
ocean-watch discover promotions --project-id PROJECT_ID
ocean-watch discover cities --city-csv cities.csv
ocean-watch mcp configure
ocean-watch mcp status
```

官方 MCP 只用于开发文档、Schema 和 SDK 示例，不执行广告业务操作。
