# 本机授权与恢复

这是持续维护的接口合同。MCP 与 CLI 共用应用层 `auth.Service`：兑换授权码、核验官方登录用户、保存授权，再按角色获取账户。会话协调器只管理生命周期；入口适配请求与浏览器交互，共用本机凭据库、授权索引和刷新锁。下面的管理工具不授予任何广告投放写入权限。

| MCP 工具 | 必填参数 | 可选参数 | 行为 |
| --- | --- | --- | --- |
| `start_authorization` | `channel` | `advertiser_id`, `authorization_id`, `rebind_existing` | 已配置 OAuth App 时直接返回官方 OAuth URL；指定 ID 时核验身份并更新该 grant，未指定时按官方用户与 App 匹配唯一既有 grant，或创建新 grant；保存后同步账户 |
| `start_account_sync` | `channel`, `authorization_id` | `advertiser_id`, `rebind_existing` | 返回本机确认页；确认后同步指定授权，必要的 Token 刷新沿用共享锁 |
| `get_authorization_session` | `channel`, `session_id` | 无 | 查询本实例会话的安全状态，不读取 Token 值、不发官方请求 |
| `cancel_authorization_session` | `channel`, `session_id` | 无 | 取消未完成会话；已经保存的授权不会被删除，已派发的请求不能承诺撤销 |

`channel` 必须显式为 `marketing`、`qianchuan` 或 `star_map`，不使用默认渠道。所有输入拒绝未声明字段；不接受 Secret、Token、授权码、回调地址、可执行命令或模型确认标记。`authorization_id` 用于明确指定既有 grant；仅指定 `advertiser_id` 不会自动选择或替换旧授权。`rebind_existing` 默认 false，只有用户明确要求改变账户归属时才传 true。

状态为 `awaiting_user`、`awaiting_callback`、`authorizing`、`syncing`、`completed`、`failed`、`cancelled`、`expired`。新授权/重新授权在启动时进入 `awaiting_callback` 并返回官方 OAuth URL；账户同步仍进入 `awaiting_user` 并返回本机确认页。MCP 启动请求立即返回，浏览器流程不占住 MCP 请求；该请求结束或浏览器断开不会取消已经接收的授权任务。Runtime 正常关闭会请求取消活动会话，并等待后台任务退出后释放本机服务。CLI 则等待完整业务结果和 HTTP 收尾后才返回。会话只保存在启动它的 Runtime 中，过期或重启后应重新发起。一个 Runtime 同时只接受一个活动会话，相同请求复用该会话；其他进程占用回调端口时返回忙碌，不接管监听。

MCP 本机回调服务仅监听 `127.0.0.1:8787`，沿用 CLI 的 `/oauth/callback`，会话有效期十分钟。CLI 默认等待五分钟，可用受控命令参数指定 loopback 回调地址和超时。新授权/重新授权直接进入官方 OAuth；账户同步才使用本机确认页。缺失的 App 配置必须先通过受控 CLI 写入 OS 凭据库，MCP/浏览器不收集 Secret。带 `authorization_id` 时授权范围明确显示这是替换既有 grant 的凭据。账户冲突默认失败，只有显式 `rebind_existing=true` 才允许改变归属；模型调用本身不批准覆盖映射。回调使用随机 state，校验渠道和一次性 state；回调地址、官方站点由 Runtime 固定，授权码只在进程内交给 Core。凭据不进入 MCP、日志或会话状态。

## 服务生命周期

1. 先绑定本机回调端口，再向调用方返回或打开官方授权 URL；不能先发起授权、再尝试启动监听。
2. 回调通过渠道和随机 state 校验后，只能认领一次。错误 state 不终止有效会话；已认领的重复回调返回冲突，不重复换码。收到授权码不是成功，也不是关闭信号。
3. 同一个应用层任务依次完成换码、官方用户身份核验、凭据及待同步授权保存、账户发现和快照激活。任务真正返回后才发布最终处理结果；处理中本机服务继续运行。MCP 回调立即跳转到不带授权码的状态页，页面处理中自动刷新。
4. 正常完成或失败时，MCP 服务在最终结果页成功写出后开始关闭；浏览器未取结果时，最多保留五秒结果页交付窗口。新会话、显式取消或 Runtime 关闭可以结束这段窗口。该窗口从业务任务结束后起算，不会截断换码或账户同步。
5. 取消、过期或服务异常先取消任务 context，再等待换码、用户核验、存储或同步中的 worker 收尾；worker 尚未退出时不发布取消/过期终态，也不主动关闭监听器。取消调用自身截止时间到达可以先返回，但后台收尾仍继续。已成功提交的结果优先保留为成功，不会被并发取消改成“未保存”。
6. 统一关闭路径在业务 worker 退出后执行 HTTP graceful shutdown，最多等待五秒 HTTP 收尾，必要时关闭残留连接，并等待 Serve 返回。关闭后端口释放，MCP 仍可查询本 Runtime 保留的安全终态。

本实现不是独立常驻服务，也不提供跨重启的授权会话恢复；进程被强制终止时无法保证继续处理。重新启动后应查询已保存的 grant 和账户状态，再发起所需的授权或同步；不能把尚未完成的内存会话当作成功。

macOS 凭据写入使用 `security -i`，完整的原位更新命令与十六进制载荷仅通过 stdin 传递，避免依赖终端密码提示；写入后必须读回核验，不先删除旧条目。系统凭据命令最多等待 30 秒，调用方较短的取消或截止时间优先。既有 grant 重新授权与自动刷新共用按渠道和授权 ID 隔离的跨进程凭据锁，持锁后再读取当前版本，直到新凭据及账户快照激活结束。

## 授权与角色获取流程

1. 在官方 OAuth 回调通过校验后兑换授权码；换码和刷新使用 `ad.oceanengine.com`。
2. 使用新 Token 调用 `api.oceanengine.com/open_api/2/user/info/` 获取官方用户 UID。UID 必须有效；响应提供 App ID 时必须与本机配置一致。Token 响应中的广告主列表不能替代角色展开。
3. 授权身份按 `channel + app_id + authorized_user_id` 区分。未指定授权 ID 时，只复用唯一匹配的已验证记录；指定 ID 时核验其已知 App/用户身份。缺少身份字段的旧 grant 不自动合并，可由用户显式指定旧 ID 重新授权并补齐身份；多个匹配记录返回 `AUTHORIZATION_AMBIGUOUS`。身份不匹配返回 `AUTHORIZATION_IDENTITY_MISMATCH`，不替换凭据。
4. 将新凭据写入安全凭据库并核验，然后选中新的 revision、保存 App/UID，将 grant 标为 `pending_account_sync=true`。新授权和重新授权均先保存这一状态；此前的账户快照留作恢复依据，待同步期间不能用于业务请求。
5. 调用 `api.oceanengine.com/open_api/oauth2/advertiser/get/` 获取授权根账户，以 `account_role` 分支；仅在缺少该字段时兼容 `account_type`。失效根账户不保留可用广告主映射。

| 根账户角色 | 广告主获取接口与参数 |
| --- | --- |
| `ADVERTISER` | 直接取有效的 `advertiser_id`，缺失时使用有效账户 ID |
| `PLATFORM_ROLE_AWEME` | 抖音号保留 `non_advertiser_role` 提示，不映射为广告主，继续展开其他根账户 |
| `CUSTOMER_ADMIN` / `CUSTOMER_OPERATOR` | `/2/customer_center/advertiser/list/`，传 `cc_account_id` 和 `account_source` |
| `PLATFORM_ROLE_ENTERPRISE_BP_ADMIN` / `PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR` | `/2/ebp/advertiser/list/`，传 `enterprise_organization_id` 和 `account_source` |
| `PLATFORM_ROLE_SHOP_ACCOUNT`（千川） | `/v1.0/qianchuan/shop/advertiser/list/`，传 `shop_id` 和 `permission=["QC_AWEME"]`；优先读取 `list`，为空时兼容 `adv_id_list`，不重复合并两种表示 |
| `PLATFORM_ROLE_QIANCHUAN_AGENT`（千川） | 沿用既有 `/2/agent/advertiser/select/` 和官方代理域名；应用缺少权限 `40002` 时记录发现问题 |

客户中心、EBP、店铺及广告主详情使用 `api.oceanengine.com`；客户中心与 EBP 的营销来源为 `AD`，千川为 `QIANCHUAN`。展开接口每页 100 条，读取全部分页后合并去重，以每批最多 50 个 ID 调用 `/2/advertiser/info/` 校验。分页或详情校验失败不替换账户快照；快照激活还必须确认当前凭据 revision 与发现时一致，旧请求不能覆盖新授权。

星图保留独立边界：仅星图账户进入发现，详情使用 `/2/star/info/`；暂不支持展开的星图角色记录发现问题。明确识别的权限缺失或不支持角色通过 `account_discovery_issues` 报告，不表示该角色全部账户已经发现。

OAuth、用户核验、授权账户展开和账户信息批量校验只依赖渠道与授权 ID，不要求预先选择业务广告主。所有请求仍经过授权级并发、命令预算、超时和响应校验；千川广告主级共享限流用于选定广告主后的业务接口。

## 完成与恢复

`completed` 表示指定 grant 的凭据和通过校验的账户快照已保存；已记录的发现问题仍需单独查看，不代表所有角色均已覆盖。指定 `advertiser_id` 时，必须确认新快照实际覆盖该广告主才返回 `can_resume=true`。

授权保存之前失败，本机原 revision 不变；官方平台是否仍接受旧 Token 不能由本机状态保证。保存之后账户发现失败或被取消，新授权和重新授权都保留新 revision、待同步状态及安全的 `authorization_id`，会话返回 `sync_required=true`。使用 `start_account_sync` 恢复，不再兑换授权码。补同步再次失败也保留待同步状态；原本可用授权的普通同步失败不会额外将其标为待同步。只有成功激活新快照后才解除业务限制。

取消和过期分别返回 `AUTHORIZATION_CANCELLED`、`AUTHORIZATION_EXPIRED`；本机服务异常为 `AUTHORIZATION_SERVICE_FAILED`，只有后台收尾完成后才发布这些终态。官方刷新错误 `40103`、`40107` 统一为不可重试的 `REAUTHORIZATION_REQUIRED`，错误中给出 `start_authorization` 与渠道。账户快照待同步单独返回 `ACCOUNT_SYNC_REQUIRED`，不能把它当作必须重新 OAuth。

Skill 保留原请求中的模板、链接和账户参数；授权/重新授权时让用户打开返回的官方 OAuth URL，账户同步时打开返回的本机确认页，查询会话直到终态，完成后重发同一个只读查询/预检。恢复不重复要求用户提供已有链接，不提交投放，也不使用 Shell/CLI 代替 WorkBuddy 的 MCP 业务路由。取消、超时、范围不符或失败都应停止自动恢复。

## 失败诊断

处理失败时，Core 只保留白名单失败环节和分类原因，不保留底层错误文本、官方原始消息、Secret、Token 或授权码。失败环节为：

| `failure_stage` | 含义 |
| --- | --- |
| `app_credentials` | 读取本机应用配置 |
| `token_exchange` | 兑换官方授权或核验授权响应 |
| `authorized_user` | 获取并核验官方登录用户 UID 与 App 身份 |
| `credential_storage` | 写入安全凭据库并核验读回结果 |
| `authorization_storage` | 保存或激活本机授权记录 |
| `account_sync` | 发现、验证和同步授权账户 |

`failure_reason` 区分官方业务拒绝、HTTP 错误、请求失败、授权响应缺少字段、用户信息无有效 UID、授权身份不匹配或歧义、凭据写入失败和写入后核验失败等情况。账户同步进一步区分 `ACCOUNT_DISCOVERY_FAILED`（账户发现）、`ACCOUNT_SNAPSHOT_INVALID`（数据校验）、`ACCOUNT_ACTIVATION_FAILED`（快照保存）和 `ACCOUNT_MAPPING_CONFLICT`（账户已属于其他授权）。归属冲突不能靠重复同步解决，必须由用户明确决定是否传入 `rebind_existing=true`；确认页会显示账户改绑的实际影响。

官方响应可附带正整数 `official_error_code`、100–599 的 `http_status`，以及最多 128 个字母、数字、下划线或连字符组成的 `official_request_id`。不合规字段被丢弃，不将错误消息或链接误作请求编号。

MCP 继续返回原有 `session` 结构，诊断通过标准工具结果 `_meta["ocean_watch.authorization_failure"]` 和额外的安全文本内容提供，不修改工具输入、输出 Schema 或授权状态的含义。CLI 将同一白名单诊断写入错误 `details`，本机状态页显示失败环节、分类原因及可公开的官方错误码和请求编号。诊断不完整时应明确原因尚未核实，不能根据通用 `AUTHORIZATION_FAILED` 猜测应用密钥、网络或本地保存中的哪一项失败。

旧实现没有保留的失败细节不能追溯恢复。需要新验证时，先处理已确认的问题，再按原渠道和授权范围启动受控授权；不能因出现失败提示而循环生成新 grant。诊断不改变取消、过期或已保存 grant 的账户同步恢复规则。
