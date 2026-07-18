# CLI 参考

本文记录统一 CLI 的命令结构和主要行为。首次使用请先阅读[快速开始](getting-started.md)；配置位置、模板 Schema 和凭据边界见[配置与授权](configuration.md)。具体参数始终以对应命令的 `--help` 为准。

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
│   ├── doctor
│   ├── init
│   ├── validate
│   └── work-metadata
├── auth
│   ├── set-app
│   ├── authorize
│   ├── status
│   ├── refresh
│   ├── sync-accounts
│   ├── migrate
│   └── mappings
├── accounts
│   ├── list
│   ├── add
│   ├── remove
│   ├── enable
│   ├── disable
│   └── report
├── templates
│   ├── list
│   ├── show
│   ├── create
│   ├── migrate
│   ├── set-copy
│   ├── validate
│   └── delete
├── qc-templates
│   ├── list
│   ├── create
│   ├── migrate
│   ├── list-live
│   ├── create-live
│   └── migrate-live
├── materials
│   ├── videos
│   ├── creator
│   ├── images
│   └── products
├── qc-materials
│   ├── creator-videos
│   ├── inspect-work
│   └── authorized-creators
├── qc-products
│   ├── list
│   └── search
├── qc-plans
│   ├── list
│   ├── show
│   ├── materials
│   ├── update-status
│   ├── update-budget
│   └── update-roi
├── runs
│   ├── list
│   └── show
├── plans
│   ├── create
│   ├── create-creator
│   ├── create-qianchuan
│   ├── batch-qianchuan-works
│   ├── remove-qianchuan-work
│   ├── batch-upload
│   ├── batch-creator
│   ├── update-project-status
│   ├── update-promotion-status
│   ├── update-budget
│   ├── update-bid
│   └── update-roi
├── reports
│   ├── materials
│   ├── schema
│   ├── custom
│   └── plans
├── qc-reports
│   ├── plans
│   └── materials
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
ocean-watch setup doctor
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
ocean-watch auth mappings --channel qianchuan --advertiser-id ADVERTISER_ID
```

首次执行 `auth authorize` 时，如果当前渠道没有应用配置，浏览器会打开一张本地表单，同屏收集 App ID 和 Secret，保存后直接跳转官方 OAuth。需要主动更换应用时使用 `auth set-app --channel CHANNEL`，它会打开相同表单但不发起授权。

授权是首次使用流程，不在 Plugin 安装阶段执行。`http://127.0.0.1:8787/oauth/callback` 只用于开放平台登记和官方回调，不是手动访问入口；本地服务仅在授权命令运行期间存在。在 Codex 中执行 `auth authorize --channel CHANNEL --print-url --no-open`，将输出中的临时 `start_url` 交给用户在目标账户对应的浏览器分组中打开，并保持命令运行。

营销和千川流程共用默认回调地址，通过 OAuth `state` 的 `AD`/`QC` 前缀区分渠道；用户无需维护两条回调路径。若账户同步失败，授权保留为 pending 状态，可用最后一条命令重试；确认迁移账户归属时增加 `--rebind-existing`。

`auth mappings` 只显示广告主、授权记录、授权主体以及 Access/Refresh Token 是否存在，不返回任何凭据值。千川支持 `auth`、`qc-templates`、`qc-materials`、`qc-products`、`qc-plans`、`qc-reports` 和千川计划创建命令。营销计划和报表命令不会回退使用千川授权。

一般业务调用会自动刷新 Token；只有排错或主动验证时才需要 `auth refresh`。

## 模板

```bash
ocean-watch templates list
ocean-watch templates list --channel marketing
ocean-watch templates list --channel qianchuan
ocean-watch templates list --include-details
ocean-watch templates show --channel marketing --template TEMPLATE_NAME
ocean-watch templates show --channel qianchuan --template TEMPLATE_ID_OR_NAME
ocean-watch templates create
ocean-watch templates create --channel marketing
ocean-watch templates create --channel marketing --material-source-type ACCOUNT_UPLOAD
ocean-watch templates create --channel marketing --material-source-type CREATOR_AUTHORIZED
ocean-watch templates create --channel qianchuan
ocean-watch templates create --channel qianchuan --template-type product
ocean-watch templates create --channel qianchuan --template-type live
ocean-watch templates migrate --confirm-remove-legacy-materials
ocean-watch templates set-copy --template TEMPLATE --title TITLE
ocean-watch templates set-copy --template TARGET --from-template SOURCE
ocean-watch templates validate
ocean-watch templates validate --channel qianchuan --template TEMPLATE
ocean-watch templates delete --channel marketing --template TEMPLATE
ocean-watch templates delete --channel marketing --template TEMPLATE --submit
```

`templates list` 只读取一次本地配置，默认同时返回巨量营销和巨量千川的精简业务模板、渠道计数和默认骨架。每条模板记录（包括默认骨架）都包含顶层 `channel=marketing|qianchuan`，即使脱离外层分组也能识别归属。该命令不请求官方接口；只有排查完整配置时才使用 `--include-details`。

`templates show` 是单模板快速查询入口，只读本地配置且不请求官方接口。营销模板使用完整模板名称，千川模板可使用 `template_id` 或完整模板名称；结果返回完整绑定、投放设置、素材策略和 `ready_for_plan_creation` 状态。

`templates validate` 只读检查模板 Schema、规范名称、渠道/广告主/商品绑定、素材策略和运行时素材泄漏。`templates delete` 默认只预演；显式 `--submit` 才修改本地配置。营销模板仍被 `created_from` 或 `copied_from_template` 引用时会阻断，确认清理引用后再删除；`--force` 只跳过该引用保护，不替代 `--submit`。

未传 `--channel` 时，`templates create` 必须先显示营销/千川及各自授权状态。选择营销后继续选择 `混剪素材（ACCOUNT_UPLOAD）` 或 `原生素材（CREATOR_AUTHORIZED）`；选择千川后继续选择商品全域或直播全域，再显示同类型来源模板。占位广告主 ID 不会作为默认值；已授权渠道会校验精确广告主 ID，只有唯一广告主才自动预填。未授权渠道仍可创建 `UNVERIFIED` 模板，但真实投放前必须完成该渠道授权。`set-copy` 只修改营销标题文案，不复制链接、账户资产或投放参数。

真实业务模板不存在当前或默认指针。所有计划创建命令必须显式传 `--plan-template`；多营销账户批量创建使用 `--account-template ADVERTISER_ID=TEMPLATE_NAME` 为每个账户明确映射。

营销向导会收集日预算、净成交 ROI、性别、年龄和官方要求的 6–9 位置产品卖点，并按 `巨量营销-广告账户ID-商品名-商品ID-模版类型` 自动生成名称；模版类型为“混剪素材”或“原生素材”。创建骨架的商品图来源是 `DPA` 商品库字段 `images_url`，因此不会询问图片 ID。提交前会验证 DPA 字段，并在不可用时从同广告主、同商品的官方单元自动解析主图；无可靠来源时在项目创建前阻断。缺失的转化资产也只从同账户、同商品项目解析，多个候选不会自动选择。

千川商品全域模板使用独立命令：

```bash
ocean-watch qc-templates list
ocean-watch qc-templates create
ocean-watch qc-templates migrate
ocean-watch qc-templates list-live
ocean-watch qc-templates create-live
ocean-watch qc-templates migrate-live
```

向导从不可投放的默认骨架或已有千川商品模板复制创建，绑定广告主、产品和 1–30 个商品 ID。多个商品 ID 使用 `/` 分隔，名称使用 `巨量千川-广告账户ID-商品名-商品ID-商品全域`。模板只保存投放参数和商品归属，不保存达人、视频、图片或渠道信息。

直播模板从独立的 `default_qianchuan_live_template` 或已有直播模板复制，绑定广告主、直播账号名称和数值 `aweme_id`。默认设置为保守出价、预算 5000、长期投放和智能选材。直播模板不保存商品、作品或手工素材，使用 `plans create-qianchuan --live-template TEMPLATE_ID` 创建；该模式不接受计划名称。

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

只检查公开作品链接和本机解析配置，不调用计划写接口：

```bash
ocean-watch qc-materials inspect-work --work-url DOUYIN_WORK_URL
```

列出商品全域授权达人：

```bash
ocean-watch qc-materials authorized-creators \
  --advertiser-id ADVERTISER_ID \
  --query DOUYIN_SHOW_ID
```

```bash
ocean-watch qc-materials creator-videos \
  --plan-template TEMPLATE_ID \
  --douyin-id DOUYIN_SHOW_ID \
  --creator-name CREATOR_NAME
```

`--douyin-id` 填抖音 App 中用户可见的抖音号。命令只使用模板绑定的千川广告主授权，先通过商品全域可投抖音号接口严格解析数值 `aweme_id`，再按模板中的每个商品 ID 查询视频。官方商品过滤排除不匹配视频；结果按作品去重并记录 `matched_product_ids`。默认输出 JSON 到终端，只有显式传入 `--out PATH` 才写文件。

## 千川商品与计划查询

```bash
ocean-watch qc-products list --advertiser-id ADVERTISER_ID
ocean-watch qc-products search --advertiser-id ADVERTISER_ID --name PRODUCT_NAME
ocean-watch qc-plans list --advertiser-id ADVERTISER_ID
ocean-watch qc-plans show --advertiser-id ADVERTISER_ID --ad-id AD_ID
ocean-watch qc-plans materials --advertiser-id ADVERTISER_ID --ad-id AD_ID
```

商品列表调用 `/v1.0/qianchuan/uni_promotion/product/get/`，支持商品 ID/名称、标签页、达人和未投放筛选。计划查询调用商品全域计划列表、详情和计划素材接口；计划列表中的 `stats_info` 不作为金额报表，消耗必须来自 `qc-reports`。

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

ocean-watch plans create-qianchuan \
  --live-template LIVE_TEMPLATE_ID
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
  --concurrency 8
```

可选解析服务通过 `setup work-metadata --endpoint URL --home-config` 写入本机配置，仓库不提供真实地址。配置后，千川作品链路只发送公开抖音链接并读取作品 ID、抖音号、数值 UID 和商品 ID；不传广告主、Token 或模板数据。返回非空商品 ID 且不在模板商品集合时直接跳过，不得新建或追加；匹配或空值仍由官方接口复核。未配置或使用 `--no-link-metadata-api` 时走安全兜底。命令默认使用 8 并发（上限 10）执行必要的官方素材校验。首次官方校验成功后，会在 `$CODEX_HOME/ads-plan-monitor/state/cache/` 保存 30 天的作品达人查询提示。缓存过期或失效时自动回退到全量官方扫描。`performance` 会分别展示链接解析、凭据准备、素材校验和计划对账耗时，并通过 `link_metadata.configured/enabled` 标识本次是否启用本机服务。

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
  --preflight \
  --concurrency 4
```

格式见 `skills/ads-plan-monitor/assets/creator-batch-jobs.example.json`。每个任务必须记录商品匹配结论和证据。`--preflight` 不调用写接口，会读取同一批次已有 journal，明确返回已完成跳过、本次可创建、项目已创建后续建单元、完整重试和阻断任务；确认后把 `--preflight` 改为 `--submit`。提交时写入本机非敏感 journal，重复执行会跳过已完成任务，并从已创建项目续建失败单元。

巨量营销项目上限已知为每广告主 200 个，但官方项目列表不能可靠表示配额占用。预检会把容量标记为 `CREATE_TIME_ONLY`，最终由 `/v3.0/project/create/` 判定，不会根据列表数量承诺剩余容量。

达人授权快照缺少 `video_cover_id` 时，预检只会从同广告主、同 `item_id`、同 `material_id` 且状态正常的官方历史单元解析唯一封面，并标记授权期仍需创建接口最终确认。如果单元创建已经返回“不在授权期间”，断点会保留已创建项目并阻断重复提交；重新授权且当前快照恢复自身封面后，只续建单元，不会再创建项目。

## 报表

```bash
ocean-watch reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
ocean-watch reports plans --advertiser-id ADVERTISER_ID --top 10
ocean-watch reports schema --data-topic MATERIAL_DATA
ocean-watch reports custom \
  --data-topic MATERIAL_DATA \
  --dimension material_id \
  --metric stat_cost
```

默认不写表格文件。Codex 应在对话中使用 Markdown 表格展示汇总和排行。

`reports plans` 先请求官方 `/v3.0/report/custom/config/get/`，从当前广告主实际开放的 `UNI_PROJECT_DATA` 契约选择项目 ID、项目名称和指标，再完整分页查询。自定义 `--metric` 若不在当前权限契约中会直接报错，不会降级或猜字段。未查询或契约未开放的 GMV/订单汇总返回 `null`，不伪装成业务值 `0`。

千川商品全域计划报表通过官方 MCP 查询：

```bash
ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD \
  --top 10

ocean-watch qc-reports materials \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD \
  --top 10
```

默认日期为当天，营销目标为商品全域，计划范围为 `UNI_PROJECT`，并按消耗降序返回前十。`--top 0` 返回全部报表行；汇总始终基于所有分页结果。命令使用目标广告主绑定的千川 OAuth Token，调用官方 Streamable HTTP MCP。金额和 ROI 只读取全域报表返回值，计划列表补充计划状态、成本保障、出价方式、ROI 目标出价、预算、名称和业务归属，不推算其内部固定精度金额。输出同时包含总消耗、有消耗计划数、整体成交金额、订单数、加权支付 ROI、1 小时净成交金额和加权净成交 ROI。

结果中的 `presentation` 是默认对话展示契约，并在 `rendered_markdown` 中提供 CLI 已生成的标准表格。`presentation.required=true` 且 `allow_column_omission=false` 时，必须原样展示其中的排名、计划、达人、商品、状态、成本保障、出价方式、目标 ROI、日预算、消耗、订单、GMV、实际 ROI、1 小时结算金额和 1 小时结算 ROI，并补充预算方式和成本保障结果/原因。除非用户在当前请求中明确指定更少或不同的字段，否则不能为了简洁缩减、合并、重排或重命名这些列。

报表分页、计划 ID 和七个必需指标任一缺失、重复或非法时，命令拒绝返回不完整汇总。汇总先使用原始 `Decimal` 值跨全部页面计算，最后才按展示精度舍入。`--status ALL` 会保留历史财务行；计划列表无法补齐其元数据时，行内 `metadata_available=false`，汇总中的 `metadata_missing_count` 同步计数，名称、状态、成本保障、出价和预算保持为空。指定具体状态时必须解析到计划元数据，否则整次查询失败，避免错误筛选。

`qc-reports materials` 调用官方 `/v1.0/qianchuan/report/material/get/`，支持素材 ID/类型/模式/来源筛选。展示上限只影响返回行，汇总始终基于全部已分页数据。自定义字段未包含成交金额或订单时，对应汇总为 `null`，不按零处理。

## 计划参数调整

```bash
ocean-watch plans update-project-status --advertiser-id ID --project-id ID --status DISABLE
ocean-watch plans update-promotion-status --advertiser-id ID --promotion-id ID --status ENABLE
ocean-watch plans update-budget --advertiser-id ID --promotion-id ID --value 5000
ocean-watch plans update-bid --advertiser-id ID --promotion-id ID --value 1.5
ocean-watch plans update-roi --advertiser-id ID --project-id ID --value 1.7

ocean-watch qc-plans update-status --advertiser-id ID --ad-id ID --status DISABLE
ocean-watch qc-plans update-budget --advertiser-id ID --ad-id ID --value 5000
ocean-watch qc-plans update-roi --advertiser-id ID --ad-id ID --value 1.7
```

以上命令默认只输出官方 endpoint 与 payload。在线修改必须增加 `--submit`；千川 `DELETE` 还必须同时传 `--confirm-delete`。每次最多处理 10 个去重后的 ID，提交时持有渠道和广告主级本机锁，并将官方批量结果中的部分失败作为整体失败返回。

## 执行记录

```bash
ocean-watch runs list
ocean-watch runs show --run-id creator-batch-RUN_ID
```

该命令只读 `$CODEX_HOME/ads-plan-monitor/state/runs/` 下由 Plugin 管理的 JSON journal；`run_id` 不接受路径字符。它不扫描用户指定的任意目录，也不显示凭据。

## 资产反查与 MCP

```bash
ocean-watch discover projects --advertiser-id ADVERTISER_ID --name NAME
ocean-watch discover promotions --advertiser-id ADVERTISER_ID --project-id PROJECT_ID
ocean-watch discover cities --city-csv cities.csv
ocean-watch mcp configure
ocean-watch mcp status
```

`mcp configure/status` 管理的是开发文档 MCP。千川业务报表使用独立的官方业务 MCP 传输层，由 `qc-reports` 在内存中注入自动刷新的千川 Token，不把 Token 写入 Codex MCP 配置。
