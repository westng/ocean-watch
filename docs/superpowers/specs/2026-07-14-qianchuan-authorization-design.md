# 巨量千川授权与账户发现设计

## 目标

Ocean Watch 在保留巨量营销全部现有能力的同时，增加巨量千川的首期接入：独立应用配置、本地 OAuth、Token 刷新、授权主体发现、主体角色展开以及真实千川广告主 ID 持久化。本阶段不实现千川计划、素材、报表或策略能力，也不调用真实接口进行开发验证。

## 渠道边界

- 巨量营销渠道标识为 `marketing`，OAuth state 使用 `AD.<nonce>`。
- 巨量千川渠道标识为 `qianchuan`，OAuth state 使用 `QC.<nonce>`。
- 两个渠道共用 `http://127.0.0.1:8787/oauth/callback`，回调通过 state 前缀识别渠道并校验当前授权会话。
- App ID、Secret、Access Token 和 Refresh Token 按渠道写入操作系统凭据存储；千川不得回退读取营销渠道的凭据或端点。
- 千川首期能力集合仅为 `oauth` 和 `accounts`。`create`、`query`、`report` 继续由渠道能力检查明确拒绝。

## 架构

新增渠道适配器层，避免共享授权代码散布渠道条件判断：

- `MarketingChannelAdapter` 保存巨量营销已有授权 URL、账户来源和角色展开规则。
- `QianchuanChannelAdapter` 保存巨量千川授权 URL、`material_auth=1`、`QC_AWEME` 权限及千川账户来源。
- 适配器注册表根据运行时渠道返回唯一适配器；未知渠道立即失败。
- OAuth 回调服务器、Token 交换与刷新、授权状态存储、账户快照和 CLI 继续由共享层负责。

适配器提供以下稳定接口：

1. 构建官方授权 URL 参数。
2. 返回 Token、授权主体、广告主展开及广告主校验端点。
3. 根据授权主体角色返回分页请求定义，包括路径、基础参数、列表字段和 ID 字段。

## 官方接口

千川适配器使用以下官方接口：

| 用途 | 方法与地址 |
| --- | --- |
| 授权页 | `GET https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html` |
| 换取 Token | `POST https://ad.oceanengine.com/open_api/oauth2/access_token/` |
| 刷新 Token | `POST https://ad.oceanengine.com/open_api/oauth2/refresh_token/` |
| 获取授权主体 | `GET /oauth2/advertiser/get/` |
| 店铺广告主 | `GET /v1.0/qianchuan/shop/advertiser/list/` |
| 代理商广告主 | `GET https://ad.oceanengine.com/open_api/2/agent/advertiser/select/` |
| 客户中心广告主 | `GET /2/customer_center/advertiser/list/` |
| EBP 广告主 | `GET /2/ebp/advertiser/list/` |
| 校验广告主 | `GET /2/advertiser/info/` |

授权 URL 除 `app_id`、`state` 和 `redirect_uri` 外，固定携带 `material_auth=1`。

## 角色展开

- `ADVERTISER`：直接使用主体的 `advertiser_id`、`account_id` 或 `account_string_id`。
- `PLATFORM_ROLE_SHOP_ACCOUNT`：按 `shop_id` 分页请求店铺广告主，权限为 `["QC_AWEME"]`。
- `PLATFORM_ROLE_QIANCHUAN_AGENT`：以授权主体 ID 作为 `advertiser_id`，通过 `ad.oceanengine.com` 分页请求代理商管理的广告主；`data.list` 是千川广告主 ID 数组。
- 代理商账户列表需要应用单独开通接口权限。仅该分支返回官方 `40002` 时允许保存其他已验证账户，并在授权状态中持久化“账户发现不完整”；其他错误仍阻断同步。
- `CUSTOMER_ADMIN`、`CUSTOMER_OPERATOR`：按 `cc_account_id` 分页请求客户中心广告主，`account_source` 为 `QIANCHUAN`。
- `PLATFORM_ROLE_ENTERPRISE_BP_ADMIN`、`PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR`：按 `enterprise_organization_id` 分页请求 EBP 广告主，`account_source` 为 `QIANCHUAN`。
- 所有候选广告主按 50 个一批调用广告主信息接口校验，只持久化官方返回确认存在的 ID。

营销适配器保持原有直接广告主、客户中心和 EBP 展开规则，`account_source` 继续为 `AD`，不增加千川店铺分支。

## 授权事务

换取 Token 后按以下顺序提交：

1. 校验 Token 响应并计算过期时间。
2. 立即创建 `pending_account_sync=true` 的渠道授权，将 Token 写入安全凭据存储。
3. 请求授权主体并展开、校验广告主 ID。
4. 成功后原子替换授权快照，重建账户和广告主索引，并清除 pending 状态。
5. 失败时保留 Token 和 pending 授权，返回同步失败信息；用户可用 `auth sync-accounts --authorization-id` 重试，无需重新授权。

这套事务同时应用于营销和千川，以消除账户同步失败导致 Token 丢失的问题。

## 错误与安全

- state 不匹配、state 渠道不匹配或回调缺少 auth code 时终止授权。
- 任一角色展开分页失败时不提交不完整账户快照。
- 任一广告主校验批次失败时不提交不完整账户快照。
- 日志和 CLI 输出继续脱敏 Token、Secret 和授权码。
- 项目配置仅保存非敏感端点与回调地址；应用密钥和 Token 不进入仓库。

## 测试

所有测试使用 mock 响应，覆盖：

- 两个渠道的能力边界、授权域名、参数和 state。
- 两套应用凭据及 Token 的存储隔离。
- 千川四类角色展开、分页和 50 个一批校验。
- 先保存 pending Token、同步失败保留授权、重试后激活快照。
- 营销授权和账户同步回归。
- CLI 的千川配置、授权状态、刷新和账户同步入口。

验收标准为 Ruff、完整单元测试、插件清单校验全部通过，开发过程不触发任何真实千川 API 请求。
