# 配置与授权

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## 存储边界

Ocean Watch 将非敏感业务配置与敏感凭据分开：

| 数据 | 默认位置 | 是否可提交 |
| --- | --- | --- |
| 业务配置 | `~/.codex/ads-plan-monitor/config.json` | 否 |
| 开发业务配置 | `config/ads-plan-monitor/config.json` | 否 |
| App、Token、MCP 标识符 | 操作系统凭据仓库 | 否 |
| 授权账户索引、迁移状态 | `~/.codex/ads-plan-monitor/state/` | 否 |
| 运行结果和 batch journal | 用户指定路径或本机状态目录 | 否 |

配置解析顺序：`--config`、`ADS_PLAN_MONITOR_CONFIG`、开发仓库配置、用户配置。

## 渠道

- `marketing`：巨量营销，当前已经实现 OAuth、账户、素材、计划和报表。
- `qianchuan`：巨量千川，只保留隔离标识，尚未实现。

渠道之间不共享 App、Secret、Token、账户或模板。旧配置迁移到巨量营销：

```bash
ocean-watch auth migrate --config PATH
```

迁移使用原子写入和 journal，可安全重复执行。旧授权缺少完整账户映射时，状态会显示 `pending_account_sync`，按输出中的本地授权 ID 执行：

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
| `plan_template_schema_version` | 计划模板版本，当前为 `3` |
| `active_plan_template` | 未显式指定时使用的业务模板 |
| `default_plan_template` | 创建模板的默认骨架，不可投放 |
| `plan_templates` | 广告主绑定的真实业务模板 |

以下字段不得写入配置：`app_id`、`secret`、`access_token`、`refresh_token`、`auth_code`、`developer_id`。

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

## App 与 OAuth

保存营销应用：

```bash
ocean-watch auth set-app --channel marketing
```

启动授权：

```bash
ocean-watch auth authorize --channel marketing
```

默认回调地址为 `http://127.0.0.1:8787/oauth/callback`，必须与开放平台设置完全一致。

- 巨量营销和巨量千川共用这一条回调地址，不需要分别申请路径。
- OAuth `state` 使用 `AD.<随机值>` 表示巨量营销，使用 `QC.<随机值>` 表示巨量千川。
- 回调必须同时匹配完整随机 `state` 和当前授权渠道，否则拒绝交换 Token。
- `AD` 或 `QC` 单独出现不是有效 `state`，不能省略随机值。

渠道短码只用于本次 OAuth 会话路由，不替代广告主 ID，也不会写入业务模板。授权成功后：

1. 获取官方授权主体。
2. 按 `account_role` 展开客户中心或企业 BP 下的广告主。
3. 通过广告主信息接口验证 ID。
4. 原子保存一份独立授权记录和非敏感账户索引。

`oauth_authorized_account_count` 是授权主体数量，不等于可投放广告主数量。真实广告主数量看 `authorized_advertiser_count`。

同一渠道可以有多份 OAuth 授权。业务命令按广告主自动选 Token；只有多个授权同时覆盖同一广告主时才传 `--auth-account-id`。

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

MCP 用于官方文档、OpenAPI Schema 和 SDK 示例，不执行广告业务。包含 App ID 和 Developer ID 的动态 URL 只在桥进程内存中构造，不写入 Plugin 清单或 Codex 配置。
