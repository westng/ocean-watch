# 配置与授权

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skills: ads-plan-monitor, qc-plan-monitor

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

## 渠道

- `marketing`：巨量营销，当前已经实现 OAuth、账户、素材、计划和报表。
- `qianchuan`：巨量千川，当前实现 OAuth、Token 刷新、授权广告主同步、商品全域模板、按商品过滤的达人视频查询、作品链接批量新建、追加或删除商品全域计划自提素材，以及官方 MCP 全域计划报表；直播模板尚未实现。

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
| `managed_account_schema_version` | 用户负责账户簿版本，当前为 `1` |
| `managed_accounts.<channel>` | 该渠道下用户主动维护的负责账户列表 |
| `plan_template_schema_version` | 计划模板版本，当前为 `3` |
| `active_plan_template` | 未显式指定时使用的业务模板 |
| `default_plan_template` | 创建模板的默认骨架，不可投放 |
| `plan_templates` | 广告主绑定的真实业务模板 |
| `qianchuan_product_template_schema_version` | 千川商品模板版本，当前为 `2` |
| `default_qianchuan_product_template` | 千川商品全域默认骨架，不可投放 |
| `active_qianchuan_product_template` | 当前千川商品模板 ID |
| `qianchuan_product_templates` | 千川广告主和商品绑定的业务模板 |

以下字段不得写入配置：`app_id`、`secret`、`access_token`、`refresh_token`、`auth_code`、`developer_id`。

公开端点覆盖只接受巨量官方 HTTPS 主机。业务 API、OAuth 和官方 MCP 客户端拒绝 HTTP 重定向并限制响应体大小，避免把 Token 或请求体转发到非预期主机。抖音作品短链解析是独立流程，只允许在官方 `douyin.com`、`iesdouyin.com` 域名范围内跳转。

`managed_accounts` 只保存非敏感的账户名称、广告主 ID、启用状态，以及可选的 OAuth 授权主体 `auth_account_id`。唯一键是 `channel + advertiser_id`，所以相同数字 ID 可以在营销和千川分别存在。同一广告主同时出现在多个 OAuth 授权时，用 `--auth-account-id` 固定选择；账户变更会在进程锁内重新读取并原子写入，避免并发命令丢失更新。授权账户同步不得新增、删除或覆盖负责账户记录。

## 计划模板

业务模板建议命名：

```text
平台-CID-商品名-商品ID-素材来源
```

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

具体视频、封面、作品和 material ID 属于本次运行，不能写回 Schema v3 模板。

### 创建模板

```bash
ocean-watch templates create
```

向导必须完成来源选择、目标绑定、素材来源、复制策略、逐字段预览和最终确认。复制策略：

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

千川商品模板与营销 Schema v3 完全独立：

```bash
ocean-watch qc-templates list
ocean-watch qc-templates create
```

模板绑定一个千川广告主、产品名称和 1–30 个商品 ID。用户可见名称为：

```text
广告主ID-商品全域-产品-商品ID1/商品ID2
```

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
- 回调地址不是授权入口，不应手动打开。授权命令启动后，只打开浏览器自动进入的页面；自动打开失败时使用 `--print-url --no-open` 并打开 `start_url`。
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

## 官方文档 MCP

```bash
ocean-watch mcp configure
ocean-watch mcp status
```

上述命令配置开发文档 MCP，用于官方文档、OpenAPI Schema 和 SDK 示例。包含 App ID 和 Developer ID 的动态 URL 只在桥进程内存中构造，不写入 Plugin 清单或 Codex 配置。

千川计划报表使用官方业务 MCP `https://open.oceanengine.com/qianchuan/mcp`。插件复用目标广告主绑定的千川 OAuth `Access-Token`，调用前自动刷新，并通过 `Tool-Range` 限制为全域计划和报表工具。Token 不写入配置文件、Plugin 清单或 Codex MCP 配置，也不需要用户额外维护 MCP API Key。
