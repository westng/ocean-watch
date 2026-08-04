# Go SDK 迁移矩阵

本文把现有 CLI 用例和官方 endpoint 映射到 Go 模块、官方 SDK Service、韧性策略和验收项。它既记录已落地的 Shadow 实现，也记录仍未接入的命令和刻意保留的 Python 兼容面；运行时边界见[架构说明](architecture.md)，阶段完成度、责任和阻断项见[机器契约](../contracts/README.md)。

## 1. 使用规则

- `P0`–`P5` 是实施阶段，不是产品版本号。
- “本地”表示不调用巨量官方 API；它仍可能访问受控本机配置、状态或凭据后端。
- “页级重试”表示只重试当前 page/cursor，不重启已完成分页。
- “不重试写”表示 SDK 调用后发生超时或断连时先对账，禁止直接重放。
- Adapter 名称是目标代码边界；SDK Service 不得越过 Adapter 暴露给 Application、CLI 或 Skill。
- 当前矩阵内 endpoint 在 `v1.1.92` 均有生成 Service，因此 `CommonApi` 使用数为零。

命令路由状态只能取：`Not started`、`Shadow`、`Go canary`、`Go default`、`Python retained`、`Rolled back`。每次改变 handler、候选 manifest 或生产 manifest 的 PR 都必须同步状态列。

- `Not started`：命令尚无完整 Go CLI handler；底层组件可能已经实现，必须在状态说明中单独记录。
- `Shadow`：完整 Go handler 已通过隔离合同或测试专用 manifest 验证，但生产仍走 Python。
- `Go canary` / `Go default`：必须有对应 Gate、签名路由 manifest 和运行证据。
- `Python retained`：批准保留的兼容或诊断面，不是遗漏的 Go 业务路径。
- `Rolled back`：曾启用 Go 后，发布路由已切回 Python。

截至 2026-08-04，P1–P4 的大部分命令已达到 `Shadow`，但生产策略仍禁用，`ProductionRouteManifest` 将全部命令固定为 Python。Go 候选的默认开发 manifest 只启用已接入的本地命令；网络和写命令的 Shadow 由测试专用 manifest 显式开启。本机自动化通过不等于 Gate 已签字，也不代表生产 launcher 已切流。

2026-08-04 的当前盘点为：`COMMANDS` 共 `82` 个 CLI action，全部由第 2 节覆盖；原有 `51` 条官方路径加入乘方账户、直播间维度和达人维度 3 条新路径后，共 `54` 条唯一官方 OpenAPI path。全域账户、Schema 和自定义数据路径原本已在基线中，本次扩展其命令用途。固定 SDK 中对应生成 Service、HTTP 方法和 host profile均通过 Adapter 测试核验，并由 `contracts/commands.yaml` 与 `contracts/sdk-baseline.yaml` 固化。命令或 endpoint 发生增删时必须重新生成机器清单，并以重新评审后的清单作为分母。

### 当前实现说明

- `auth set-app/authorize/status/refresh/sync-accounts/mappings` 仍是 `Not started` 路由：P2 已实现并自动测试 SDK Client、OAuth callback、OS 凭据、Token 单飞刷新和完整广告主快照组件，但这些命令尚未接入 Go CLI handler。
- `qc-materials inspect-work` 仍是 `Not started`：Go runner 明确返回 `go_handler_missing`，生产继续使用 Python 作品解析路径。
- `mcp configure/status/capabilities` 标记为 `Python retained`：它们保留为可选诊断命令；业务报表不得在 SDK REST 失败时静默回退 MCP。
- 其他 `Shadow` 网络和写命令已有完整 Go handler，但在生产 manifest 中仍固定为 Python。

## 2. 命令迁移矩阵

| 用例 | CLI 命令 | 目标用例/Adapter | 阶段 | 核心合同 | 验收 | 当前状态 |
| --- | --- | --- | --- | --- | --- | --- |
| 环境检查 | `setup doctor` | `onboarding.Doctor` | P1 | JSON、平台和依赖检查兼容 | AC-101, AC-102 | Shadow |
| 初始化 | `setup init` | `onboarding.Initialize` + `filesystem.ConfigStore` | P1 | create-if-missing、原子写、Schema 不变 | AC-106, AC-107 | Shadow |
| 配置校验 | `setup validate` | `onboarding.Validate` | P1 | 不访问网络、错误码兼容 | AC-102, AC-106 | Shadow |
| 作品解析配置 | `setup work-metadata` | `workmetadata.Configure` | P1 | endpoint 不回显、显式 clear | AC-109 | Shadow |
| 应用配置 | `auth set-app` | `auth.ConfigureApp` + `CredentialStore` | P2 | Secret 只进安全后端 | AC-107, AC-109 | Not started |
| 本地 OAuth | `auth authorize` | `auth.Authorize` + OAuth SDK Adapter | P2 | state 校验、pending sync、失败可续 | AC-108, AC-110 | Not started |
| Token 状态 | `auth status` | `auth.Status` | P2 | 只返回脱敏状态 | AC-109 | Not started |
| Token 刷新 | `auth refresh` | `auth.Refresh` | P2 | 渠道隔离、单飞、原子保存 | AC-110 | Not started |
| 广告主同步 | `auth sync-accounts` | `auth.SyncAdvertisers` | P2 | 完整分页后替换；失败保留旧快照 | AC-111, AC-112 | Not started |
| 授权迁移 | `auth migrate` | `auth.MigrateState` | P1 | 幂等、可回滚、未知字段保留 | AC-106 | Shadow |
| 授权映射 | `auth mappings` | `auth.QueryMappings` | P2 | 不返回凭据值 | AC-109 | Not started |
| 负责账户名单 | `accounts list` | `accounts.ListManaged` | P1 | 只读本地；固定四列；零网络/零刷新 | AC-103 | Shadow |
| 负责账户维护 | `accounts add`、`accounts remove`、`accounts enable`、`accounts disable` | `accounts.Manage` | P1 | `channel + advertiser_id` 唯一；锁内读改写 | AC-106, AC-107 | Shadow |
| 账户表现 | `accounts report` | `accounts.Report` + Marketing/Qianchuan Report Adapter | P2 | 千川不查计划列表；部分失败不取消其他账户 | AC-104, AC-113 | Shadow |
| 跨渠道模板列表/详情 | `templates list`、`templates show` | `templates.Query` | P1 | 单次本地读取、渠道字段与 Presentation 兼容 | AC-105, AC-106 | Shadow |
| 模板创建路由 | `templates create` | `templates.CreateWizard` | P1 | 自然语言由 Skill 路由；向导规则不变 | AC-101, AC-106 | Shadow |
| 营销模板维护 | `templates migrate`、`templates set-copy`、`templates validate`、`templates delete` | `templates.MarketingLifecycle` | P1 | Schema、引用保护、dry-run/`--submit` | AC-106, AC-116 | Shadow |
| 千川商品模板 | `qc-templates list`、`qc-templates create`、`qc-templates migrate` | `templates.QianchuanProductLifecycle` | P1 | 稳定 template ID、1–30 商品、无运行时素材 | AC-106 | Shadow |
| 千川直播模板 | `qc-templates list-live`、`qc-templates create-live`、`qc-templates migrate-live` | `templates.QianchuanLiveLifecycle` | P1 | 直播绑定、无商品/作品字段 | AC-106 | Shadow |
| 上传视频查询 | `materials videos` | `materials.MarketingVideos` + Marketing Material Adapter | P3 | 分页/批量和 `--out` 兼容 | AC-112, AC-114 | Shadow |
| 达人视频查询 | `materials creator` | `materials.MarketingCreatorVideos` | P3 | 授权与主页事实分离 | AC-112, AC-114 | Shadow |
| 图片查询 | `materials images` | `materials.MarketingImages` | P3 | endpoint 模式和分页兼容 | AC-112, AC-114 | Shadow |
| 商品查询 | `materials products` | `materials.MarketingProducts` | P3 | DPA 字段与筛选兼容 | AC-112, AC-114 | Shadow |
| 千川作品检查 | `qc-materials inspect-work` | `materials.InspectPublicWork` + Work Metadata Adapter | P1 | 私有 endpoint 不回显；官方事实不由外部结果替代 | AC-109, AC-114 | Not started |
| 千川授权达人 | `qc-materials authorized-creators` | `materials.ListQianchuanCreators` | P3 | 精确账号匹配、完整分页 | AC-112, AC-114 | Shadow |
| 千川达人视频 | `qc-materials creator-videos` | `materials.QueryQianchuanCreatorVideos` | P3 | 每商品官方过滤、cursor 分页、去重 | AC-112, AC-114 | Shadow |
| 千川商品 | `qc-products list`、`qc-products search` | `materials.QueryQianchuanProducts` | P3 | 完整分页、展示限制不影响汇总 | AC-112, AC-114 | Shadow |
| 千川计划列表/详情/素材 | `qc-plans list`、`qc-plans show`、`qc-plans materials` | `plans.QueryQianchuan` | P3 | list/detail/materials 各自独立，分页严格 | AC-112, AC-114 | Shadow |
| 千川计划设置 | `qc-plans update-status`、`qc-plans update-budget`、`qc-plans update-roi` | `plans.UpdateQianchuanSettings` | P4 | dry-run、广告主锁、逐行失败、状态 `DELETE` 双确认 | AC-116, AC-121 | Shadow |
| 运行历史 | `runs list`、`runs show` | `runs.Query` | P1 | 仅允许状态根内 journal，拒绝任意路径 | AC-106, AC-109 | Shadow |
| 营销计划创建 | `plans create` | `plans.CreateMarketingUploaded` + `MarketingPlanExecutor` | P4 | 项目/单元事务、dry-run、未知结果对账 | AC-116, AC-118 | Shadow |
| 营销达人计划 | `plans create-creator` | `plans.CreateMarketingCreator` + `MarketingPlanExecutor` | P4 | 当前授权素材、共享事务 | AC-116, AC-118 | Shadow |
| 千川全域创建 | `plans create-qianchuan` | `plans.CreateQianchuan` | P4 | 单 endpoint；`code=0` + `ad_id` 才成功 | AC-116, AC-119 | Shadow |
| 千川作品批量创建/追加 | `plans batch-qianchuan-works` | `plans.BatchQianchuanWorks` | P4 | 当天判重、页级退避、五列表格、幂等追加 | AC-105, AC-119, AC-120 | Shadow |
| 千川作品素材删除 | `plans remove-qianchuan-work` | `plans.RemoveQianchuanMaterials` | P4 | material ID、CUSTOM、分批 100、回读 DELETED | AC-121 | Shadow |
| 营销上传批量 | `plans batch-upload` | `plans.BatchMarketingUploaded` | P4 | 账户/组并发、共享 Executor、journal 续建 | AC-117, AC-118 | Shadow |
| 营销达人批量 | `plans batch-creator` | `plans.BatchMarketingCreator` | P4 | 授权过期阻断、项目续建、部分失败汇总 | AC-117, AC-118 | Shadow |
| 营销设置更新 | `plans update-project-status`、`plans update-promotion-status`、`plans update-budget`、`plans update-bid`、`plans update-roi` | `plans.UpdateMarketingSettings` | P4 | dry-run、广告主锁、逐行结果 | AC-116, AC-121 | Shadow |
| 营销素材报表 | `reports materials` | `reports.MarketingMaterials` | P3 | 报表财务源与元数据关联分离 | AC-113, AC-114 | Shadow |
| 营销报表字段 | `reports schema` | `reports.MarketingSchema` | P3 | 官方字段原样规范化 | AC-114 | Shadow |
| 营销自定义报表 | `reports custom` | `reports.MarketingCustom` | P3 | 参数、分页、错误和 `--out` 兼容 | AC-112, AC-114 | Shadow |
| 营销计划报表 | `reports plans` | `reports.MarketingPlans` | P3 | 完整汇总、Presentation 固定列 | AC-105, AC-113, AC-114 | Shadow |
| 千川计划报表 | `qc-reports plans` | `reports.QianchuanPlans` + SDK Report Adapter | P3 | `data/get` 财务源；list 只补元数据；不读 `stats_info` 金额 | AC-105, AC-113, AC-115 | Shadow |
| 千川素材报表 | `qc-reports materials` | `reports.QianchuanMaterials` | P3 | 完整分页、缺失指标为 null | AC-112, AC-114 | Shadow |
| 千川乘方账户报表 | `qc-reports account` | `reports.QianchuanAllPromotion` | P3 | 必传 `adlab_scene`；`data_period` 仅限 `OVERALL_PROJECT` | AC-113, AC-114 | Shadow |
| 千川全域账户报表 | `qc-reports uni-account` | `reports.QianchuanUniPromotion` | P3 | 单广告主全域聚合；不替代负责账户集合报表 | AC-104, AC-113 | Shadow |
| 千川报表字段 | `qc-reports schema` | `reports.QianchuanSchema` | P3 | 多主题单请求；保留官方字段契约 | AC-113, AC-114 | Shadow |
| 千川自定义报表 | `qc-reports custom` | `reports.QianchuanCustom` | P3 | 主题/维度/指标/筛选原样映射；完整分页 | AC-112, AC-114 | Shadow |
| 千川商品表现 | `qc-reports products` | `reports.QianchuanProducts` | P3 | 全域/乘方主题分离；商品资产走 `qc-products` | AC-112, AC-113 | Shadow |
| 千川直播间表现 | `qc-reports rooms` | `reports.QianchuanRoom` | P3 | 精确 room ID；日/小时维度；完整分页 | AC-112, AC-114 | Shadow |
| 千川达人表现 | `qc-reports authors` | `reports.QianchuanAuthor` | P3 | 精确数值 aweme ID；不与达人素材查询混用 | AC-112, AC-114 | Shadow |
| 营销项目/单元发现 | `discover projects`、`discover promotions` | `discovery.MarketingPlans` | P3 | 绑定广告主、分页完整 | AC-112, AC-114 | Shadow |
| DPA 发现 | `discover dpa` | `discovery.DPA` | P3 | 模式化 endpoint、字段验证 | AC-114 | Shadow |
| 转化资产 | `discover events` | `discovery.EventAssets` | P3 | 广告主隔离、分页 | AC-112, AC-114 | Shadow |
| 深度出价/优化目标 | `discover deep-bids`、`discover goals` | `discovery.Optimization` | P3 | 官方枚举与筛选兼容 | AC-114 | Shadow |
| 城市解析 | `discover cities` | `discovery.ResolveCities` + Marketing Admin Adapter | P3 | 本地 CSV + 官方行政区读取；显式 `--write-config` | AC-106, AC-110, AC-114 | Shadow |
| MCP 兼容诊断 | `mcp configure`、`mcp status`、`mcp capabilities` | Python compatibility diagnostics | P3 | 保留 CLI；业务不得静默回退 MCP | AC-101, AC-109, AC-115 | Python retained |

## 3. 官方 endpoint 到 SDK Service

### 3.1 OAuth 与广告主发现

| Endpoint | SDK Service | 目标 Adapter 方法 | 分页/重试 | 使用命令 | 验收 |
| --- | --- | --- | --- | --- | --- |
| `POST /oauth2/access_token/` | `Oauth2AccessTokenApiService` | `oauth.ExchangeCode` | 不盲重试；失败保留 pending 前状态 | `auth authorize` | AC-108, AC-110 |
| `POST /oauth2/refresh_token/` | `Oauth2RefreshTokenApiService` | `oauth.RefreshToken` | 授权级单飞；明确未执行才重试 | `auth refresh`、业务自动刷新 | AC-110 |
| `GET /oauth2/advertiser/get/` | `Oauth2AdvertiserGetApiService` | `auth.ListDirectAdvertisers` | 页级读重试 | `auth authorize/sync-accounts` | AC-111, AC-112 |
| `GET /2/customer_center/advertiser/list/` | `CustomerCenterAdvertiserListV2ApiService` | `auth.ListCustomerCenterAdvertisers` | 页级读重试 | `auth sync-accounts` | AC-111, AC-112 |
| `GET /2/ebp/advertiser/list/` | `EbpAdvertiserListV2ApiService` | `auth.ListEbpAdvertisers` | 页级读重试 | `auth sync-accounts` | AC-111, AC-112 |
| `GET /2/advertiser/info/` | `AdvertiserInfoV2ApiService` | `auth.GetAdvertiserInfo` | 分批；读重试 | `auth sync-accounts` | AC-111, AC-114 |
| `GET /2/agent/advertiser/select/` | `AgentAdvertiserSelectV2ApiService` | `auth.ListAgentAdvertisers` | 页级读重试 | 千川/营销代理账户同步 | AC-111, AC-112 |
| `GET /v1.0/qianchuan/shop/advertiser/list/` | `QianchuanShopAdvertiserListV10ApiService` | `auth.ListQianchuanShopAdvertisers` | 页级读重试 | `auth sync-accounts --channel qianchuan` | AC-111, AC-112 |

账户同步的所有角色先写入内存候选快照；只有每个声明页成功且广告主详情校验完成后，才原子激活新快照。任何一行 SDK 映射失败都不能用部分列表覆盖旧数据。

### 3.2 营销报表、计划与设置

| Endpoint | SDK Service | 目标 Adapter 方法 | 分类 | 使用命令 | 验收 |
| --- | --- | --- | --- | --- | --- |
| `GET /v3.0/report/custom/config/get/` | `ReportCustomConfigGetV30ApiService` | `marketing.GetReportSchema` | 读；页级重试 | `reports schema/plans` | AC-113, AC-114 |
| `GET /v3.0/report/custom/get/` | `ReportCustomGetV30ApiService` | `marketing.QueryCustomReport` | 读；页级重试 | `accounts report`、`reports custom/materials/plans` | AC-104, AC-112, AC-113 |
| `GET /v3.0/project/list/` | `ProjectListV30ApiService` | `marketing.ListProjects` | 读；页级重试 | `discover projects`、运行时资产解析 | AC-112, AC-114 |
| `POST /v3.0/project/create/` | `ProjectCreateV30ApiService` | `marketing.CreateProject` | 写；未知结果对账 | `plans create/create-creator/batch-*` | AC-117, AC-118 |
| `POST /v3.0/project/status/update/` | `ProjectStatusUpdateV30ApiService` | `marketing.UpdateProjectStatus` | 写；不自动重试 | `plans update-project-status` | AC-116, AC-121 |
| `POST /v3.0/project/roigoal/update/` | `ProjectRoigoalUpdateV30ApiService` | `marketing.UpdateProjectROI` | 写；不自动重试 | `plans update-roi` | AC-116, AC-121 |
| `GET /v3.0/promotion/list/` | `PromotionListV30ApiService` | `marketing.ListPromotions` | 读；页级重试 | `discover promotions`、报表元数据、对账 | AC-112, AC-114 |
| `POST /v3.0/promotion/create/` | `PromotionCreateV30ApiService` | `marketing.CreatePromotion` | 写；未知结果按项目对账 | `plans create/create-creator/batch-*` | AC-117, AC-118 |
| `POST /v3.0/promotion/status/update/` | `PromotionStatusUpdateV30ApiService` | `marketing.UpdatePromotionStatus` | 写；不自动重试 | `plans update-promotion-status` | AC-116, AC-121 |
| `POST /v3.0/promotion/budget/update/` | `PromotionBudgetUpdateV30ApiService` | `marketing.UpdatePromotionBudget` | 写；不自动重试 | `plans update-budget` | AC-116, AC-121 |
| `POST /v3.0/promotion/bid/update/` | `PromotionBidUpdateV30ApiService` | `marketing.UpdatePromotionBid` | 写；不自动重试 | `plans update-bid` | AC-116, AC-121 |

### 3.3 营销素材、商品与发现

| Endpoint | SDK Service | 目标 Adapter 方法 | 分页/重试 | 使用命令 | 验收 |
| --- | --- | --- | --- | --- | --- |
| `GET /2/file/video/get/` | `FileVideoGetV2ApiService` | `marketing.ListLibraryVideos` | 页级读重试 | `materials videos`、`plans batch-upload` | AC-112, AC-114 |
| `GET /2/file/video/ad/get/` | `FileVideoAdGetV2ApiService` | `marketing.GetAdVideos` | 分批读；当前批重试 | `materials videos`、`plans batch-upload` | AC-114 |
| `GET /2/file/video/aweme/get/` | `FileVideoAwemeGetV2ApiService` | `marketing.ListCreatorHomepageVideos` | cursor/page 读重试 | `materials creator` | AC-112, AC-114 |
| `GET /2/tools/aweme_auth_list/` | `ToolsAwemeAuthListV2ApiService` | `marketing.ListCreatorAuthorizations` | 页级读重试 | `materials creator`、达人计划 | AC-112, AC-114 |
| `GET /2/file/image/get/` | `FileImageGetV2ApiService` | `marketing.ListLibraryImages` | 页级读重试 | `materials images` | AC-112, AC-114 |
| `GET /2/file/image/ad/get/` | `FileImageAdGetV2ApiService` | `marketing.GetAdImages` | 分批读；当前批重试 | `materials images` | AC-114 |
| `GET /2/tools/video_cover/suggest/` | `ToolsVideoCoverSuggestV2ApiService` | `marketing.SuggestVideoCovers` | 有界轮询；不扩大批次 | `materials videos`、`plans batch-upload` | AC-114 |
| `GET /2/dpa/clue_product/list/` | `DpaClueProductListV2ApiService` | `marketing.ListDPAProducts` | 页级读重试 | `materials products` | AC-112, AC-114 |
| `POST /2/dpa/asset_v2/detail/read/` | `DpaAssetV2DetailReadV2ApiService` | `marketing.GetDPAAsset` | 只读语义；明确瞬时失败才重试 | `discover dpa`、计划预检 | AC-114 |
| `GET /v3.0/dpa/ebp/product/detail/get/` | `DpaEbpProductDetailGetV30ApiService` | `marketing.GetEbpProduct` | 读重试 | `discover dpa` | AC-114 |
| `GET /2/dpa/dict/get/` | `DpaDictGetV2ApiService` | `marketing.GetDPADictionary` | 读重试 | `discover dpa` | AC-114 |
| `GET /2/dpa/meta/get/` | `DpaMetaGetV2ApiService` | `marketing.GetDPAMetadata` | 读重试 | `discover dpa` | AC-114 |
| `GET /2/tools/admin/info/` | `ToolsAdminInfoV2ApiService` | `marketing.GetAdminInfo` | 读重试 | `discover cities` | AC-110, AC-114 |
| `GET /2/tools/event/all_assets/list/` | `ToolsEventAllAssetsListV2ApiService` | `marketing.ListEventAssets` | 页级读重试 | `discover events`、计划预检 | AC-112, AC-114 |
| `GET /v3.0/event_manager/deep_bid_type/get/` | `EventManagerDeepBidTypeGetV30ApiService` | `marketing.ListDeepBidTypes` | 读重试 | `discover deep-bids` | AC-114 |
| `GET /v3.0/event_manager/optimized_goal/get_v2/` | `EventManagerOptimizedGoalGetV2V30ApiService` | `marketing.ListOptimizedGoals` | 读重试 | `discover goals`、计划预检 | AC-114 |

### 3.4 千川账户、计划与报表读取

| Endpoint | SDK Service | 目标 Adapter 方法 | 分页/重试 | 使用命令 | 验收 |
| --- | --- | --- | --- | --- | --- |
| `GET /v1.0/qianchuan/report/all_promotion/get/` | `QianchuanReportAllPromotionGetV10ApiService` | `qianchuan.FetchAllPromotion` | 只读重试 | `qc-reports account` | AC-113, AC-114 |
| `GET /v1.0/qianchuan/report/uni_promotion/get/` | `QianchuanReportUniPromotionGetV10ApiService` | `qianchuan.FetchUniPromotion` | 只读重试 | `accounts report`、`qc-reports uni-account` | AC-104, AC-113 |
| `GET /v1.0/qianchuan/report/uni_promotion/config/get/` | `QianchuanReportUniPromotionConfigGetV10ApiService` | `qianchuan.FetchSchemas` | 多主题单次请求；只读重试 | `qc-reports plans/schema` | AC-113, AC-115 |
| `GET /v1.0/qianchuan/report/uni_promotion/data/get/` | `QianchuanReportUniPromotionDataGetV10ApiService` | `qianchuan.FetchDataPage` | 失败页原位读重试 | `qc-reports plans/custom/products` | AC-112, AC-113, AC-115 |
| `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/room/get/` | `QianchuanReportUniPromotionDimensionDataRoomGetV10ApiService` | `qianchuan.FetchRoomDimensionPage` | 失败页原位读重试 | `qc-reports rooms` | AC-112, AC-114 |
| `GET /v1.0/qianchuan/report/uni_promotion/dimension_data/author/get/` | `QianchuanReportUniPromotionDimensionDataAuthorGetV10ApiService` | `qianchuan.FetchAuthorDimensionPage` | 失败页原位读重试 | `qc-reports authors` | AC-112, AC-114 |
| `GET /v1.0/qianchuan/report/material/get/` | `QianchuanReportMaterialGetV10ApiService` | `qianchuan.QueryMaterialReport` | 页级读重试 | `qc-reports materials` | AC-112, AC-114 |
| `GET /v1.0/qianchuan/uni_promotion/product/get/` | `QianchuanUniPromotionProductGetV10ApiService` | `qianchuan.ListProducts` | 页级读重试 | `qc-products list/search` | AC-112, AC-114 |
| `GET /v1.0/qianchuan/uni_promotion/list/` | `QianchuanUniPromotionListV10ApiService` | `qianchuan.ListPlans` | 页级重试；批量判重固定当天 | `qc-plans list`、`qc-reports plans`、批量判重 | AC-112, AC-115, AC-119 |
| `GET /v1.0/qianchuan/uni_promotion/ad/detail/` | `QianchuanUniPromotionAdDetailV10ApiService` | `qianchuan.GetPlanDetail` | 候选级读重试，包括临时 RPC timeout | `qc-plans show`、批量判重 | AC-114, AC-119 |
| `GET /v1.0/qianchuan/uni_promotion/ad/material/get/` | `QianchuanUniPromotionAdMaterialGetV10ApiService` | `qianchuan.ListPlanMaterials` | 页级读重试 | `qc-plans materials`、追加/删除对账 | AC-112, AC-120, AC-121 |

财务来源约束：负责账户集合表现和明确仅全域的单广告主表现使用 `uni_promotion/get`；包含乘方的单广告主账户表现使用 `all_promotion/get`；计划、商品和自定义主题表现使用 `data/get`；直播间和达人表现使用对应维度接口。计划列表只补元数据，禁止读取 `stats_info` 推算金额。

### 3.5 千川达人、作品与写入

| Endpoint | SDK Service | 目标 Adapter 方法 | 分类 | 使用命令 | 验收 |
| --- | --- | --- | --- | --- | --- |
| `GET /v1.0/qianchuan/uni_aweme/authorized/get/` | `QianchuanUniAwemeAuthorizedGetV10ApiService` | `qianchuan.ListAuthorizedCreators` | 页级读重试；宽扫描不滥用重试 | `qc-materials *`、批量创建 | AC-112, AC-119 |
| `GET /v1.0/qianchuan/file/video/aweme/get/` | `QianchuanFileVideoAwemeGetV10ApiService` | `qianchuan.ListCreatorVideos` | cursor/批次读重试 | `qc-materials creator-videos`、批量创建 | AC-112, AC-119 |
| `POST /v1.0/qianchuan/uni_aweme/ad/create/` | `QianchuanUniAwemeAdCreateV10ApiService` | `qianchuan.CreatePlan` | 写；未知结果按当天计划 + 详情对账 | `plans create-qianchuan/batch-qianchuan-works` | AC-116, AC-119 |
| `POST /v1.0/qianchuan/uni_promotion/ad/material/add/` | `QianchuanUniPromotionAdMaterialAddV10ApiService` | `qianchuan.AddMaterials` | 写；回查缺失素材，禁止整批盲重试 | `plans batch-qianchuan-works` | AC-120 |
| `POST /v1.0/qianchuan/uni_promotion/ad/material/delete/` | `QianchuanUniPromotionAdMaterialDeleteV10ApiService` | `qianchuan.DeleteMaterials` | 写；每批最多 100；回查 `DELETED` | `plans remove-qianchuan-work` | AC-121 |
| `POST /v1.0/qianchuan/uni_promotion/ad/status/update/` | `QianchuanUniPromotionAdStatusUpdateV10ApiService` | `qianchuan.UpdateStatus` | 写；不自动重试，回读 | `qc-plans update-status` | AC-116, AC-121 |
| `POST /v1.0/qianchuan/uni_promotion/ad/budget/update/` | `QianchuanUniPromotionAdBudgetUpdateV10ApiService` | `qianchuan.UpdateBudget` | 写；不自动重试，回读 | `qc-plans update-budget` | AC-116, AC-121 |
| `POST /v1.0/qianchuan/uni_promotion/ad/roi2_goal/update/` | `QianchuanUniPromotionAdRoi2GoalUpdateV10ApiService` | `qianchuan.UpdateROI` | 写；不自动重试，回读 | `qc-plans update-roi` | AC-116, AC-121 |

## 4. 非 SDK 边界

| 边界 | 目标 Adapter | 保留原因 | 禁止事项 | 验收 |
| --- | --- | --- | --- | --- |
| 本地 OAuth callback/browser | `browser.LocalOAuth` | SDK 不负责回调服务和浏览器 | 不接受非 loopback callback；不跳过 state | AC-108 |
| OS 凭据后端 | `credentials.CredentialStore` | 平台安全能力 | 不写项目配置；不回显 Secret/Token | AC-107, AC-109 |
| 文件配置/锁/journal | `filesystem.*` | 本地状态 | 不改变现有 Schema；不接受任意 journal 路径 | AC-106, AC-107 |
| 作品链接解析 | `workmetadata.Resolver` | 可选外部提示源 | 不发送广告主/凭据；不替代官方复核 | AC-109, AC-119 |
| 抖音官方短链跳转 | `workmetadata.DouyinRedirectResolver` | 链接标准化 | 只跟随官方域名；不把跳转结果当授权事实 | AC-109, AC-119 |
| 官方 MCP 诊断 | `mcp.Diagnostics` | 保留现有命令兼容 | 不作为 SDK REST 失败的静默业务回退 | AC-115 |

## 5. 合同测试键

每个 Adapter fixture 使用稳定键命名：

```text
<channel>/<endpoint-alias>/<scenario>/<page-or-attempt>.json
```

最低场景集合：

- `success-single`、`success-multi-page`、`empty`。
- `http-429`、`http-503`、`business-40100`、`business-51010`。
- `http-200-business-error`、`malformed-json`、`missing-required-data`。
- `timeout-before-response`、`timeout-unknown-write`。
- `pagination-stalled`、`pagination-contradictory`、`duplicate-identity`。
- `token-expired-refresh-success`、`token-expired-refresh-failed`。

fixture 必须使用明显占位 ID 和测试 Token，且由 secret scan 验证。具体目录、命令和证据格式见[验收契约](../contracts/README.md)及 `contracts/acceptance/`。

## 6. 矩阵完成条件

迁移 Owner 在每个命令切换前完成以下检查：

1. 对应 endpoint 行存在准确生成 Service，不使用通用 path 调用替代。
2. Adapter request/response mapping 有成功、空值、业务失败和分页 fixture。
3. 命令黄金输出、退出码和 `presentation` 与 Python 基线一致。
4. 读重试和写对账符合本行策略。
5. 命令状态先从 `Not started` 到 `Shadow`，再到 `Go canary`；完成阶段 Gate 后才标记 `Go default`。
6. 回滚时标记 `Rolled back`，记录原因、影响版本和修复 Owner，禁止直接改回 `Go default`。
