# 配置与授权

本文说明配置文件、业务模板、OAuth 和凭据存储。首次安装流程见[快速开始](getting-started.md)，命令参数见 [CLI 参考](cli.md)。

## 存储边界

Ocean Watch 将非敏感业务配置与敏感凭据分开：

| 数据 | 默认位置 | 是否可提交 |
| --- | --- | --- |
| 业务配置 | `$CODEX_HOME/ads-plan-monitor/config.json` | 否 |
| 开发业务配置 | `config/ads-plan-monitor/config.json` | 否 |
| App、Token、MCP 标识符 | 操作系统凭据仓库 | 否 |
| 授权账户索引、迁移状态 | `$CODEX_HOME/ads-plan-monitor/state/` | 否 |
| 显式启用的明文回退凭据 | `$CODEX_HOME/ads-plan-monitor/*.json` | 否 |
| 运行结果和 batch journal | 用户指定路径或本机状态目录 | 否 |

`CODEX_HOME` 未设置时默认为 `~/.codex`。配置、授权状态和凭据目录始终使用同一个根；不要把自定义 `CODEX_HOME` 放进 Git 工作树。

配置解析顺序：`--config`、`ADS_PLAN_MONITOR_CONFIG`、开发仓库配置、用户配置。安装在
`$CODEX_HOME/plugins/cache` 下的插件即使携带仓库元数据，也始终读取用户配置，避免把插件缓存误判为开发仓库。

Go Shadow 候选原地兼容相同配置和状态根，不创建第二份“Go 配置”。Python 与 Go 使用相同字段、凭据 service/account 命名、文件锁和原子替换协议；Go 首次读取不得隐式迁移或删除未知字段。生产路由仍全量 Python，因此普通用户不需要修改配置来启用 Go，也不存在受支持的用户级运行时开关。

## 渠道

- `marketing`：巨量营销，当前已经实现 OAuth、账户、素材、计划和报表。
- `qianchuan`：巨量千川，当前生产实现 OAuth、Token 刷新、授权广告主同步、商品/直播全域模板、达人和商品查询、作品链接批量新建/追加/删除、计划参数调整及计划/素材报表。计划报表的 Python handler 暂时保留官方 MCP 兼容传输，Go Shadow 已改用官方 SDK REST；MCP 不是目标业务架构，也不作为 SDK 失败时的静默回退。

渠道之间不共享 App、Secret、Token、账户或模板。旧配置迁移到巨量营销：

```bash
ocean-watch auth migrate --config PATH
```

迁移在配置进程锁内读取、转存凭据、清理敏感字段并原子写入，可安全重复执行。模板向导使用配置 revision 做乐观并发控制；交互期间配置被其他进程修改时会返回 `configuration_conflict`，必须重新加载后再操作。旧授权缺少完整账户映射时，状态会显示 `pending_account_sync`，按输出中的本地授权 ID 执行：

```bash
ocean-watch auth sync-accounts \
  --channel marketing \
  --authorization-id AUTHORIZATION_ID
```

普通业务调用不要传 `--authorization-id`；它们按目标 `advertiser_id` 自动解析授权。

## 配置 Schema

顶层关键字段：

| 字段 | 说明 |
| --- | --- |
| `config_schema_version` | 渠道与配置结构版本，当前为 `2` |
| `default_channel` | 默认渠道 |
| `channels.<channel>` | 非敏感 API、OAuth 地址和回调配置 |
| `account.channel` | 当前业务账户渠道 |
| `account.advertiser_id` | 当前广告主 |
| `integrations.qianchuan_work_metadata.endpoint` | 可选千川作品解析服务，仅允许保存在本机配置 |
| `managed_account_schema_version` | 用户负责账户簿版本，当前为 `1` |
| `managed_accounts.<channel>` | 该渠道下用户主动维护的负责账户列表 |
| `plan_template_schema_version` | 营销计划模板版本，当前为 `6` |
| `default_plan_template` | 创建模板的默认骨架，不可投放 |
| `plan_templates` | 广告主绑定的真实业务模板 |
| `qianchuan_product_template_schema_version` | 千川商品模板版本，当前为 `8` |
| `default_qianchuan_product_template` | 千川商品全域默认骨架，不可投放 |
| `qianchuan_product_templates` | 千川广告主和商品绑定的业务模板 |
| `qianchuan_live_template_schema_version` | 千川直播模板版本，当前为 `1` |
| `default_qianchuan_live_template` | 千川直播全域默认骨架，不可投放 |
| `qianchuan_live_templates` | 绑定千川广告主与直播账号的业务模板 |

以下字段不得写入配置：`app_id`、`secret`、`access_token`、`refresh_token`、`auth_code`、`developer_id`。

公开端点覆盖只接受巨量官方 HTTPS 主机。业务 API、OAuth 和官方 MCP 客户端拒绝 HTTP 重定向并限制响应体大小，避免把 Token 或请求体转发到非预期主机。抖音作品短链解析是独立流程，只允许在官方 `douyin.com`、`iesdouyin.com` 域名范围内跳转。

`managed_accounts` 只保存非敏感的账户名称、广告主 ID、启用状态，以及可选的 OAuth 授权主体 `auth_account_id`。唯一键是 `channel + advertiser_id`，所以相同数字 ID 可以在营销和千川分别存在。同一广告主同时出现在多个 OAuth 授权时，用 `--auth-account-id` 固定选择；账户变更会在进程锁内重新读取并原子写入，避免并发命令丢失更新。授权账户同步不得新增、删除或覆盖负责账户记录。

## 千川作品解析服务

该服务用于加速千川作品链接预检，是可选能力，默认未配置。真实地址属于本机配置，不写入开源仓库、Plugin 清单、Skill、示例或命令输出。安装态配置：

```bash
ocean-watch setup work-metadata \
  --endpoint https://YOUR_PRIVATE_HOST/PATH \
  --home-config
```

开发仓库可显式指定被 Git 忽略的配置文件：

```bash
ocean-watch setup work-metadata \
  --endpoint https://YOUR_PRIVATE_HOST/PATH \
  --config config/ads-plan-monitor/config.json
```

查看状态不会打印地址，清除配置分别使用：

```bash
ocean-watch setup work-metadata --home-config
ocean-watch setup work-metadata --clear --home-config
```

本机 JSON 结构如下，开源示例只保留空字符串：

```json
{
  "integrations": {
    "qianchuan_work_metadata": {
      "endpoint": "https://YOUR_PRIVATE_HOST/PATH"
    }
  }
}
```

端点必须是无用户名、密码和片段的 HTTPS URL。插件以 `GET` 请求调用，并在端点已有查询参数之后追加一个 URL 编码的 `url` 参数：

```text
GET <本机配置的 endpoint>?url=<公开抖音作品链接>
Accept: application/json
```

### 数据返回接口

解析服务应返回以下 JSON。字段可以包含更多业务数据，但插件只使用表格中标出的字段：

```json
{
  "code": 200,
  "message": "数据获取成功",
  "data": {
    "author": {
      "nickname": "达人昵称",
      "unique_id": "公开抖音号",
      "uid": "数值UID",
      "avatar": "https://PUBLIC_IMAGE_URL"
    },
    "product": {
      "product_info_id": "商品ID",
      "product_info_img": "https://PUBLIC_IMAGE_URL",
      "product_info_name": "商品名称"
    },
    "video": {
      "video_info_cover": "https://PUBLIC_IMAGE_URL",
      "video_info_id": "作品ID",
      "video_info_title": "作品标题",
      "video_info_url": "https://PUBLIC_VIDEO_URL",
      "play_url": "https://PUBLIC_AUDIO_URL"
    }
  }
}
```

| 响应字段 | 要求 | 插件用途 |
| --- | --- | --- |
| `code` | 必须为数字 `200` | 判断解析成功 |
| `data.video.video_info_id` | 必填，纯数字字符串 | 作品 `aweme_item_id` |
| `data.author.unique_id` | 与 `uid` 同时提供时启用加速 | 定向查询官方授权达人 |
| `data.author.uid` | 与 `unique_id` 同时提供时启用加速，纯数字字符串 | 官方查询使用的数值 `aweme_id` 提示 |
| `data.product.product_info_id` | 可选，纯数字字符串 | 与模板全部商品 ID 比对；非空且均不匹配时立即跳过 |
| `data.product.product_info_name` | 可选 | 诊断提示，不参与投放判断 |
| `data.author.nickname` | 名称形式含 `creator_name` 时新建计划必填 | 仅用于本次千川新计划名称中的达人名称，不写入模板或长期缓存 |
| 图片、标题、视频和音频 URL | 可选 | 不用于计划创建，也不持久化 |

响应体上限为 1 MiB，服务端重定向会被拒绝。商品 ID 命中或为空不能证明作品可投，仍需通过千川官方授权关系、作品归属和商品过滤接口；只有商品 ID 明确不匹配时才执行前置跳过。解析服务未配置、响应异常或单次使用 `--no-link-metadata-api` 时，插件回退到受限抖音短链跳转和完整官方查询。

## 计划模板

`ocean-watch templates list` 是全渠道的本地快速查询入口，一次读取配置后同时返回营销和千川模板，不触发 Token 刷新或官方 API 请求。默认输出精简字段；`--channel` 可筛选渠道，`--include-details` 用于完整诊断。

巨量营销默认模板的地域定向由行政区树的省级节点生成。官方 `audience.city` 是包含列表，因此默认配置写入港、澳、台、新疆、西藏之外的 29 个省级地域 ID，并在 `resolved_ids.city_names` 保存对应名称；不会添加官方接口不存在的“排除地域”字段。业务模板可以通过覆盖 `resolved_ids.city_ids` 和 `resolved_ids.city_names` 自定义地域。

营销和千川商品业务模板都使用用户填写的名称。模板名称只是便于识别的标签，不编码广告主、商品或素材来源；真实归属始终由 `bindings` 决定，不依赖名称解析。营销向导还分别收集 `project_name_template` 和 `promotion_name_template`，在实际创建项目和广告时渲染对应名称。

真实业务模板没有“当前”或“默认”状态。所有创建计划命令必须显式提供模板名称或模板 ID；`default_plan_template`、`default_qianchuan_product_template` 和 `default_qianchuan_live_template` 只用于复制创建新模板，永远不参与投放。

真实归属由 `bindings` 决定，不依赖名称解析：

```json
{
  "bindings": {
    "channel": "marketing",
    "advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
    "platform": "REPLACE_WITH_PLATFORM",
    "traffic_source": "CID",
    "product_id": "REPLACE_WITH_PRODUCT_ID",
    "product_name": "REPLACE_WITH_PRODUCT_NAME"
  },
  "material_strategy": {
    "source_type": "ACCOUNT_UPLOAD",
    "selection_mode": "MANUAL",
    "max_materials_per_unit": 5
  }
}
```

`source_type` 只允许：

- `ACCOUNT_UPLOAD`：账户上传素材，在线名称使用“混剪”。
- `CREATOR_AUTHORIZED`：达人合作授权素材，在线名称使用“原生”。

具体视频、封面、作品和 material ID 属于本次运行，不能写回营销 Schema v6 模板。

### 创建模板

```bash
ocean-watch templates create
```

共享入口首先选择 `marketing` 或 `qianchuan`，再进入对应渠道向导。授权状态只作为提示；未授权渠道仍可创建模板，但不能执行真实投放。营销向导在来源模板之前先选择 `ACCOUNT_UPLOAD`（混剪）或 `CREATOR_AUTHORIZED`（原生），来源列表只显示同模式模板，然后完成目标绑定、日预算、净成交 ROI、性别、年龄、产品卖点、素材规则、文案、链接、逐字段预览和最终确认。产品卖点按官方规则限制为每条 6–9 个位置、最多 10 条。广告主占位符不会成为默认值；已有授权索引时必须选择该渠道覆盖的广告主，未授权渠道的预览标记为 `UNVERIFIED`。

创建骨架使用官方商品库主图：`product_image_type=DPA`、`product_image_fields=["images_url"]`，标准向导不要求用户填写图片 ID。提交前会通过官方接口验证该商品字段；字段不可用时，插件只会从同广告主、同商品的已有官方单元复用主图和非空品牌 ID，并在内存中转为 `CUSTOM` payload。没有可复用单元时会在创建项目前阻断。高级配置仍可显式使用 `CUSTOM`，并在 `overrides.resolved_ids.product_image_ids` 配置账户图片素材 ID；这些 ID 不是视频或封面 ID。

转化资产同样属于广告主边界。模板已绑定 `event_asset_ids` 时直接使用；缺失时，提交前只从同广告主、同商品的官方项目解析。唯一候选可自动补齐，多个有效候选必须由用户明确选择，不能按列表顺序猜测。

复制策略：

| 目标变化 | 保留 | 清理 |
| --- | --- | --- |
| 同广告主、同商品 | 业务设置 | 动态素材 ID |
| 同广告主、新商品 | 通用设置 | 商品资产、链接、追踪、文案 |
| 跨广告主、同商品 | 文案可复用 | 账户资产、链接、追踪、达人白名单 |
| 跨广告主、新商品 | 通用设置 | 账户与商品相关全部字段 |

不完整候选只能保存为草稿，不能激活。默认骨架永远不能用于真实创建。

### 文案素材

```bash
ocean-watch templates set-copy \
  --template TEMPLATE \
  --title TITLE_1 \
  --title TITLE_2
```

每条标题必须为 5–30 个字符。相同商品可只复制另一模板的文案：

```bash
ocean-watch templates set-copy --template TARGET --from-template SOURCE
```

该操作不会复制广告主绑定、参数、链接或账户资产。

## 千川商品模板

千川商品模板与营销 Schema v6 完全独立：

```bash
ocean-watch qc-templates list
ocean-watch qc-templates create
```

模板绑定一个千川广告主、商品全称、用于计划命名的商品简称和 1–30 个商品 ID，模板名称由用户填写。全称保存在 `bindings.product_name`，简称保存在 `bindings.product_short_name`，模板列表和预览会同时返回两者。`plan_name_template` 定义创建新商品全域计划时的名称形式，支持 `product_name`、`product_short_name`、`creator_name`、`aweme_id`、`douyin_id`、`date`、`time`、`datetime`、`month_day`、`type`、`business` 占位符；`{product_name}` 始终表示商品全称，`{product_short_name}` 表示商品简称。`month_day` 按 `8.4` 形式输出且不补零，渲染结果会先清除 Emoji、Unicode 符号和控制字符、归一化空白，再限制为最多 100 加权字符；清洗后为空会在读取凭据前阻断。默认骨架使用 `{month_day}-{creator_name}-{product_short_name}-{type}-{business}`，其中类型和商务不固化在模板内，而是从每行素材输入的可选 Tab 分列读取；没有的字段会连同相邻分隔符一起省略。`--plan-type`、`--business` 仅作为未分列输入的整批回退值。只向已有计划追加素材时不会改已有计划名称。Schema v8 会用旧模板原有的商品全称回填简称，并将上一版默认五段形式切换到简称占位符，所以首次迁移后的实际计划名称不变；显式使用 `{product_name}` 等自定义形式保持不变。

默认投放参数：

| 字段 | 默认值 |
| --- | --- |
| `smart_bid_type` | `SMART_BID_CUSTOM` |
| `roi2_goal` | `1.7` |
| `budget` | `5000` |
| `qcpx_mode` | `QCPX_MODE_ON` |
| `video_schedule_type` | `SCHEDULE_FROM_NOW` |
| `deep_external_action` | `AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI` |

模板明确不保存 `aweme_id`、`product_channel_info`、渠道 ID、达人 ID、视频、图片或创意列表。`material_strategy.source_type=CREATOR_RUNTIME_QUERY` 表示素材必须在创建运行时根据用户提供的抖音号或作品链接查询；作品链接批量任务不会把解析结果写回模板。

## App 与 OAuth

直接启动营销授权：

```bash
ocean-watch auth authorize --channel marketing
```

千川使用独立应用和凭据槽：

```bash
ocean-watch auth authorize --channel qianchuan
```

当前渠道缺少应用配置时，插件会先打开本地安全表单，APP ID 和 Secret 在同一页面填写；提交并安全保存后自动继续官方 OAuth。需要更换应用但暂不授权时使用：

```bash
ocean-watch auth set-app --channel marketing
ocean-watch auth set-app --channel qianchuan
```

默认回调地址为 `http://127.0.0.1:8787/oauth/callback`，必须与开放平台设置完全一致。

- Plugin 安装不触发 OAuth；首次使用相应渠道时才运行 `auth authorize`。
- 回调地址不是授权入口，不应手动打开。在 Codex 中使用 `--print-url --no-open`，只把临时 `start_url` 交给用户，由用户在对应巨量账户的浏览器分组中打开。
- Codex 返回临时地址后必须保持授权进程并持续等待回调；授权完成后主动输出账户同步和广告主到 Token 的映射结果。
- 本地服务只在授权命令运行期间监听，授权完成或超时后关闭。
- 巨量营销和巨量千川共用这一条回调地址，不需要分别申请路径。
- OAuth `state` 使用 `AD.<随机值>` 表示巨量营销，使用 `QC.<随机值>` 表示巨量千川。
- 回调必须同时匹配完整随机 `state` 和当前授权渠道，否则拒绝交换 Token。
- `AD` 或 `QC` 单独出现不是有效 `state`，不能省略随机值。

渠道短码只用于本次 OAuth 会话路由，不替代广告主 ID，也不会写入业务模板。授权成功后：

1. 获取官方授权主体。
2. 按 `account_role` 展开广告主；千川额外展开店铺、客户中心或企业 BP 下的广告主。
3. 通过广告主信息接口验证 ID。
4. 原子保存一份独立授权记录和非敏感账户索引。

`oauth_authorized_account_count` 是授权主体数量，不等于可投放广告主数量。真实广告主数量看 `authorized_advertiser_count`。

同一渠道可以有多份 OAuth 授权。业务命令按广告主自动选 Token；只有多个授权同时覆盖同一广告主时才传 `--auth-account-id`。若账户同步失败，Token 会保留为 `pending_account_sync`，按状态输出的授权 ID 重试：

```bash
ocean-watch auth sync-accounts \
  --channel qianchuan \
  --authorization-id AUTHORIZATION_ID
```

若官方账户已绑定另一份本地授权，确认后增加 `--rebind-existing` 完成迁移。

## Token 生命周期

所有业务服务在 API 调用前检查 Token。剩余有效期不足 30 分钟时自动刷新，并保存轮换后的 Access Token 与 Refresh Token。并发进程使用本地锁避免重复刷新。

```bash
ocean-watch auth status --channel marketing
ocean-watch auth refresh --channel marketing
ocean-watch auth sync-accounts --channel marketing
```

状态：

- `ready`：可直接调用。
- `refresh`：下次调用前自动刷新。
- `reauthorize`：Refresh Token 缺失或过期，需要重新授权。

## 跨平台凭据仓库

- macOS：Keychain。
- Windows：DPAPI 保护的用户本地文件。
- Linux：Secret Service，通过 `secret-tool`。

缺少安全后端时默认拒绝保存。只有受限开发环境可以显式启用：

```bash
export ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1
```

该模式在用户目录保存明文凭据，不应用于真实广告账户。

## 配置校验

```bash
ocean-watch setup init --home-config
ocean-watch setup validate --mode all
```

校验模式：

- `query`：授权和账户查询就绪。
- `create-preview`：模板足以生成 payload。
- `create-submit`：凭据、账户和在线创建字段完整。
- `all`：全部模式。

## 可选官方 MCP

```bash
ocean-watch mcp configure
ocean-watch mcp status
ocean-watch mcp capabilities
ocean-watch mcp capabilities --tool TOOL_NAME
```

上述命令配置可选的官方 MCP，并从运行时 `tools/list` 查询当前工具。`capabilities` 返回工具名称和描述，指定 `--tool` 时返回该工具当前的完整输入 Schema；实际路由以运行时结果为准，不依赖静态工具清单。MCP 验证和注册成功后，Developer ID 写入本机操作系统凭据仓库供后续任务复用；包含 App ID 和 Developer ID 的动态 URL 只在桥进程内存中构造，不写入业务配置、Plugin 清单或 Codex MCP 配置。

已配置 MCP 时，插件优先用于当前明确支持且参数契约匹配的远程操作。配置、OAuth 本机回调、凭据持久化、模板、负责账户、本机缓存和执行日志仍由 CLI 管理。读操作可在 MCP 调用前失败时安全回退 OpenAPI；写操作仍需原有预览和明确确认，若 MCP 是否已执行不确定，必须先查询并核对状态，不能直接改走 OpenAPI 重试。

当前生产 Python `qc-reports plans` 暂时使用官方业务 MCP `https://open.oceanengine.com/qianchuan/mcp`。插件复用目标广告主绑定的千川 OAuth `Access-Token`，调用前自动刷新，并通过 `Tool-Range` 限制为全域计划和报表工具。Token 不写入配置文件、Plugin 清单或 Codex MCP 配置，也不需要用户额外维护 MCP API Key。该传输是迁移期兼容路径；目标 Go 业务路径使用官方 SDK REST，完成 Gate 后替换它，不能把 MCP 路径继续扩展为新的领域依赖。
