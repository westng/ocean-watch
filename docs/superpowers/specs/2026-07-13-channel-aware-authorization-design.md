# Ocean Watch 渠道化授权设计

日期：2026-07-13

## 目标

Ocean Watch 需要同时承载不同巨量业务渠道。当前已实现的 OAuth、账户同步、计划创建、素材查询和数据报表全部属于“巨量营销”；后续将独立开发“巨量千川”。本次改造只建设渠道化授权底座，并将现有能力无感归入巨量营销，不实现千川业务 API。

设计必须满足：

- 巨量营销和巨量千川使用完全独立的 APP ID、Secret、回调地址、Token 与账户集合。
- 同一渠道允许用户使用多个官方账号完成授权。
- 授权账户由官方返回的 `account_id` 区分。
- 业务调用通过渠道和账户 ID 自动解析正确授权，不允许跨渠道回退。
- 现有巨量营销配置和本机凭据可迁移，不要求用户重新授权。
- 敏感信息继续只保存在操作系统凭据仓库，不进入项目配置。

## 范围

### 本期包含

- 定义稳定渠道标识 `marketing` 和 `qianchuan`。
- 将现有 OAuth 应用配置、授权凭据、账户同步和业务模板归属到 `marketing`。
- 支持同一渠道保存多份授权记录。
- 根据官方授权账户 `account_id` 建立授权索引。
- 调整首次使用、授权、Token 状态和账户同步向导，使渠道信息明确可见。
- 为旧配置和旧凭据提供幂等迁移。
- 为千川保留明确的未配置、未实现状态。

### 本期不包含

- 巨量千川 OAuth 端点和账户接口。
- 巨量千川计划创建、素材查询、报表或策略能力。
- 在营销和千川之间共享 Token、账户或模板。
- 开发与自动化验收期间修改生产用户状态或触发真实 API 请求。发布后的迁移命令可以在用户主动运行时，按本文备份和事务规则更新其选定的本地配置与系统凭据条目。

## 渠道模型

代码维护一个渠道注册表，业务代码不得散落硬编码渠道参数。

```json
{
  "marketing": {
    "display_name": "巨量营销",
    "implemented": true
  },
  "qianchuan": {
    "display_name": "巨量千川",
    "implemented": false
  }
}
```

每个渠道负责定义自己的 OAuth 地址、业务 API 地址、账户同步实现和能力状态。当前巨量营销沿用项目已有端点；千川在后续开发前不提供虚构默认值。

未知渠道必须报错。未实现渠道必须返回结构化的 `channel_not_implemented` 错误，不得尝试巨量营销实现。

## 非敏感配置

配置 schema 升级后使用渠道命名空间。真实应用密钥和 Token 不出现在该文件中。

```json
{
  "config_schema_version": 2,
  "default_channel": "marketing",
  "channels": {
    "marketing": {
      "api": {
        "base_url": "https://api.oceanengine.com/open_api"
      },
      "oauth": {
        "redirect_uri": "http://127.0.0.1:8787/oauth/callback",
        "authorize_url": "https://ad.oceanengine.com/openapi/audit/oauth.html",
        "token_base_url": "https://ad.oceanengine.com/open_api"
      }
    },
    "qianchuan": {
      "status": "not_implemented"
    }
  }
}
```

兼容期内，旧顶层 `api` 和 `oauth` 只可被迁移器读取，不再作为新写入格式。现有 `account.advertiser_id` 继续表示当前业务目标账户，但新增 `account.channel: marketing` 明确其渠道。

## 本机凭据模型

系统凭据仓库按渠道隔离应用凭据和授权记录。所有官方 ID 必须在 HTTP JSON 解码边界无损读取为字符串，不能先经过浮点数。渠道适配器使用支持任意精度整数的解析方式，并在模型入口统一转换；不支持无损数字的运行时必须使用 lossless JSON parser。ID 的规范语法为 `0|[1-9][0-9]*`，拒绝负号、正号、空白、小数和前导零。测试必须覆盖大于 JavaScript 安全整数范围的数字字面量。

逻辑数据结构如下：

```json
{
  "version": 2,
  "channels": {
    "marketing": {
      "app": {
        "app_id": "<secret-store>",
        "secret": "<secret-store>"
      },
      "authorizations": {
        "<authorization_id>": {
          "access_token": "<secret-store>",
          "refresh_token": "<secret-store>",
          "access_token_expires_at": "<timestamp>",
          "refresh_token_expires_at": "<timestamp>",
          "generation": 1,
          "authorized_accounts": [
            {
              "account_id": "<string>",
              "account_name": "<string>",
              "account_role": "<string>",
              "advertiser_ids": ["<string>"]
            }
          ]
        }
      },
      "account_index": {
        "<account_id>": "<authorization_id>"
      },
      "advertiser_index": {
        "<advertiser_id>": [
          {
            "account_id": "<string>",
            "authorization_id": "<authorization_id>"
          }
        ]
      }
    }
  }
}
```

`authorization_id` 是本地生成的稳定随机标识，用于表示一次 OAuth 授权。一个授权 Token 可能覆盖多个官方账户，因此 Token 只保存一次，多个 `account_id` 指向同一个 `authorization_id`。每个授权账户同时保存其展开并验证后的 `advertiser_ids`。

`account_index` 是账户当前归属的权威来源。`advertiser_index` 只从“`account_index[account_id]` 仍指向该授权记录”的账户快照重建；旧授权记录中已转移的账户不得参与解析。已归属其他授权的账户不会被旧授权同步自动抢回，除非用户显式执行 rebind。rebind 必须在一个渠道提交中切换归属并重建反向索引。授权记录可以保留历史快照，但仅当前归属生效。

索引键在逻辑上是 `channel + account_id`。不同渠道出现相同 `account_id` 时互不影响。

### 物理存储适配器

Keychain、Windows Credential Manager 和 Linux Secret Service 的容量、枚举和替换语义不同，因此系统凭据仓库只保存有界的敏感数据；账户名称、账户关系和索引属于非敏感元数据，保存到 `~/.codex/ads-plan-monitor/state/` 下仅当前用户可读写的版本化状态文件。Windows 使用当前用户 ACL，POSIX 使用目录 `0700`、文件 `0600`。

凭据适配器提供统一的命名条目接口：

- `app/<channel>`：该渠道 APP ID 和 Secret。
- `authorization/<channel>/<authorization_id>/<revision>`：一份不可变的 Access Token、Refresh Token 和有效期。

状态文件提供：

- `channels/<channel>/manifest-<generation>.json`：授权记录 ID、精确 Token revision、generation、授权账户快照和两个索引。
- `channels/<channel>/current.json`：当前已提交 manifest generation 及校验摘要。
- `migration/journal.json`：配置迁移和凭据迁移的独立状态、稳定迁移 ID 与最终激活状态。

凭据适配器必须支持读、写、读回校验、删除和明确的“安全后端不可用”错误。单个凭据条目只含有界 Token bundle；写入前仍检查后端实际限制，超限时失败，不得截断。Linux 无 Secret Service、系统凭据仓库锁定或损坏时一律 fail closed；未经用户另行明确配置，不允许回退到明文文件或环境变量。适配器契约测试以各后端已知上限验证边界值和超限失败。

每次变更先获取统一的渠道提交锁；刷新还需先获取该授权记录的刷新锁，然后进入渠道提交。渠道锁必须是由打开句柄持有的操作系统级跨进程独占锁，在 Windows 使用文件句柄锁，在 POSIX 使用 `flock` 或等价机制；锁实现封装为跨平台适配器。任何 manifest 基线读取都必须发生在成功加锁之后。进程崩溃或句柄关闭由操作系统释放锁，不依赖 PID 猜测；锁文件中的 PID、generation 和随机 nonce 仅用于诊断，不决定所有权，释放前验证本进程持有句柄和 nonce。

操作从当前 manifest 读取基线，写入并读回校验新的不可变 Token revision（如有）和新 manifest，再用原子文件替换切换 `current.json`。因为旧 manifest 引用旧的不可变 Token revision，指针切换前失败时旧状态完整有效。切换后新 manifest 是唯一权威数据，不再读取旧 generation。提交完成后可清理无引用 Token revision 和 manifest，清理失败不影响正确性。

所有会改变渠道 manifest 的刷新、同步、重新授权和 rebind 都经过同一渠道锁，因此提交可串行化；generation 不匹配时必须重新读取基线或返回 `credential_generation_conflict`。进程间锁文件只保存 PID、generation 和诊断 nonce，不保存任何凭据，并使用仅当前用户可访问的权限；打开的 OS 锁句柄是所有权的唯一权威依据。

## 授权和账户同步

授权命令必须显式接收或通过向导选择渠道。当前默认渠道为 `marketing`，但输出仍需显示渠道名称，避免用户误认为授权适用于所有业务。

授权流程：

1. 校验渠道存在且已实现 OAuth。
2. 从该渠道的系统凭据条目读取 APP ID 和 Secret。
3. 使用该渠道回调地址启动本地 OAuth 回调服务。
4. 用授权码交换 Token，生成新的 `authorization_id`。
5. 使用新 Token 调用该渠道的授权账户接口，并逐个展开、验证其覆盖的广告主。渠道适配器只有在所有分页、所有账户展开和验证全部成功后才能返回 `complete_snapshot: true`。
6. 将每个 `account_id` 和 `advertiser_id` 规范化为字符串，构建授权账户映射。
7. 原子写入授权记录和 `account_index`。
8. 输出渠道、授权记录 ID、账户数量和账户列表，不输出 Token。

如果一个 `account_id` 已指向旧授权记录，新授权在提交前检测全部归属冲突。交互模式下，新 Token 和完整快照暂时只保留在内存中，由用户一次性确认所有冲突账户是否 rebind；非交互模式必须预先提供 `--rebind-existing`，否则丢弃内存中的 Token 和授权码结果，返回冲突，重试需要重新走 OAuth。拒绝确认时不写任何凭据、manifest 或索引。

确认 rebind 后，在一个渠道事务内写入新 Token revision、授权记录、全部账户归属变化和两个索引，不允许只迁移部分冲突账户。旧同步任务只能删除仍指向自身授权记录的索引项，不能删除已经切换到新授权的映射，也不能重新取得其归属。只有在旧记录不再被任何账户引用时，才允许清理旧记录。网络错误、分页不完整、部分账户展开失败、验证失败或凭据写入失败时，整个快照不可提交，当前 manifest 保持不变。

Token 刷新通过 `channel + authorization_id` 定位记录。Access Token、可能轮换的 Refresh Token 及有效期写入新的不可变 Token revision，并由新 manifest 一次引用；旧 generation 不得覆盖新 generation。刷新不重建账户索引，也不触发不必要的账户同步。

OAuth 回调只允许监听配置中明确的 loopback 主机，精确匹配回调路径。每次尝试生成加密安全的随机 `state`，并绑定渠道、回调服务实例和过期时间；`state` 与授权码均只能消费一次，超时、重放或并发错配必须拒绝。若官方端点支持 PKCE 则启用。授权码、Token 和完整回调查询串不得写入日志或诊断输出。

## 授权解析

所有业务 API 调用统一使用授权解析器，输入至少包含：

- `channel`
- 目标业务账户 ID
- 可选的授权账户 `account_id`

解析顺序：

1. 校验渠道已实现所需能力。
2. 如果显式提供授权账户 `account_id`，只在该渠道索引中查找，并验证它确实覆盖目标广告主。
3. 否则根据同步后的账户映射，找到包含目标广告主的授权账户。
4. 唯一匹配时自动选择。
5. 无匹配时停止并提示该渠道重新授权或同步账户。
6. 多个匹配时停止并展示候选 `account_id`，要求用户明确选择。
7. 读取对应授权记录并在必要时刷新 Token。

任何情况下都不得搜索其他渠道，也不得回退到旧的全局 Token。账户同步必须收到渠道适配器的完整快照才允许提交；从当前授权记录重新构建该记录的广告主关系，只删除仍属于该记录但本次已不存在的陈旧关系，然后按 `account_index` 当前归属重建 `advertiser_index`。部分结果只用于显示诊断，不得改变 manifest。

若旧凭据迁移后尚无 `account_id`，它以渠道内稳定的 `authorization_id` 标记为 `pending_account_sync`。首次业务调用不得猜测或使用全局 Token，而是返回 `legacy_authorization_pending_sync` 及针对该授权记录的同步命令；同步命令使用这份已迁移的营销授权，成功后建立索引。已有完整账户映射的用户不需要重新授权。

## 模板和业务绑定

默认模板骨架不参与真实业务使用，可以保持渠道无关，只保存跨渠道确实通用的字段。业务模板必须新增 `bindings.channel`。

现有模板迁移规则：

- 所有已有业务模板设置 `bindings.channel: marketing`。
- 现有 `bindings.advertiser_id`、平台、商品和文案保持不变。
- 创建和查询入口若未显式传渠道，则从业务模板或当前账户读取；迁移兼容期最后才使用 `default_channel`。
- 模板渠道与命令渠道不一致时，在 Token 解析和业务 API 调用前停止。

后续千川模板可以复用模板管理框架，但其字段 schema、验证和 payload 构建应由千川能力独立定义。

## 向导和命令

首次使用向导展示：

- 巨量营销：当前实现状态、应用凭据状态、授权记录数、授权账户数。
- 巨量千川：`待开发`，不展示可执行授权命令。

授权、Token、同步账户命令增加 `--channel`，当前默认值为 `marketing`。状态输出必须包含 `channel`、`channel_display_name`、`authorization_id` 和授权 `account_id`，并保持敏感字段脱敏。

所有现有业务命令在 schema v2 启用的同一版本内增加 `--channel` 和可选 `--auth-account-id`，并统一接入授权解析器。创建、批量创建、查询、报表、刷新和账户同步路径全部完成迁移是发布门槛；任何直接读取全局 Token 的旧路径必须删除或硬失败。正常情况下只需提供广告主 ID，解析器应自动匹配；只有歧义时才要求额外参数。

`account_id`、`advertiser_id` 和 `authorization_id` 不是密钥。用户主动执行的本地交互或 JSON 状态输出可以显示完整 ID，确保候选可选择、修复命令可执行；通用日志、异常遥测和公开诊断只显示掩码。Token、Secret、授权码和回调查询始终完全隐藏。

## 迁移

迁移分为配置和系统凭据两部分，并要求幂等。

### 配置迁移

配置迁移、凭据迁移、恢复、回滚和每次 migration journal 状态转换都必须先获取同一个全局迁移锁。该锁复用前述 OS 级跨进程独占锁适配器，不能用普通 PID 文件替代。每次 journal 更新均通过临时文件写入、fsync、原子替换和读回校验完成；配置迁移与凭据迁移不得并发修改 journal。

1. 检测缺少 `config_schema_version` 或版本小于 2。
2. 校验版本、字段类型和已有渠道节点；版本高于当前实现时拒绝继续。
3. 将旧 `api.base_url`、`oauth.*` 移入 `channels.marketing`。
4. 旧字段与新渠道字段同时存在且值不一致时停止并报告冲突，不静默覆盖；一致时合并。
5. 设置 `default_channel: marketing`。
6. 为 `account` 以及 `plan_templates` 中的每个业务模板补充 `marketing`，默认模板骨架不绑定渠道。
7. 保留不认识的非敏感字段，先验证完整迁移结果，再通过单文件原子替换写入并保留本地备份。
8. `config_schema_version: 2` 只在所有配置与模板变更成功后写入；重复执行得到同一结果。

### 凭据迁移

1. 在已持有全局迁移锁的情况下读取 journal。若迁移 journal 不存在，先生成稳定 `authorization_id` 和 Token revision，并在创建任何目标条目前原子写入、读回验证 `credentials: preparing` journal；重试复用这些 ID。
2. 读取现有单一 Ocean Engine 凭据条目，将旧 APP ID、Secret 准备为 `marketing.app`。
3. 将旧 Token 写入不可变营销 Token revision，将已同步账户包装为营销授权快照，并从可确认的 `account_id -> advertiser_ids` 关系构建索引。
4. 写入并读回校验新凭据条目与状态 manifest。原子切换营销 `current.json` 是凭据迁移的唯一提交点，再将 journal 更新为 `credentials: committed`。
5. 配置迁移独立更新 `config: committed`。只有 `config` 与 `credentials` 均 committed 后，协调器才写入 `activation: schema_v2_active`。
6. 激活后新存储成为唯一权威来源，禁止再次回退或合并旧 Token。旧凭据保留一个兼容版本周期作为只读回滚副本，确认稳定后通过显式清理命令删除。

迁移不得访问业务 API。旧凭据缺少可验证的账户映射时保留为 `pending_account_sync` 的营销授权记录，通过前述显式同步流程建立索引。在 `current.json` 提交点之前失败时，未提交的新条目可清理，current 指针和旧凭据保持不变。提交点之后发生崩溃时，任何被 current 引用的条目都不得清理或盲目回滚；重启后协调器验证 current 引用了 journal 中的稳定迁移 ID，然后幂等推进到 `credentials: committed`。若验证失败则停止业务执行并要求显式恢复，不自动回退旧 Token。

启动协调器支持“配置先提交”和“凭据先提交”两种混合状态。未激活时只允许迁移重试、状态诊断和回滚，不允许运行业务命令，也不允许业务路径读取旧全局 Token。每个状态转换均可重复执行；测试在 journal 写入、凭据 revision 写入、manifest 写入、current 切换、两类 committed 和最终 activation 前后注入崩溃。

## 错误模型

至少提供以下稳定错误码：

- `unknown_channel`
- `channel_not_implemented`
- `channel_app_credentials_missing`
- `authorization_not_found`
- `authorized_account_not_found`
- `authorization_ambiguous`
- `template_channel_mismatch`
- `legacy_credentials_migration_failed`
- `legacy_authorization_pending_sync`
- `secure_credential_backend_unavailable`
- `credential_generation_conflict`

错误信息必须说明渠道、相关账户 ID 和下一条安全修复命令，但不得包含 Token、Secret 或完整凭据内容。

## 测试和验收

自动化测试覆盖：

- 旧配置迁移为 `marketing` 且重复迁移结果一致。
- 旧应用凭据和 Token 迁移后仍可读取，不泄露敏感信息。
- 同一营销渠道保存多份授权时不会互相覆盖。
- 一个 Token 覆盖多个 `account_id` 时只保存一份 Token。
- `account_id -> advertiser_ids` 和反向索引可重建，显式账户也必须覆盖目标广告主。
- 账户从仍拥有其他账户的旧授权 rebind 到新授权后，只由新授权解析；旧同步不能抢回。
- 目标广告主自动解析唯一授权，歧义时停止并给出完整可选择的候选 ID。
- Token 刷新只更新对应授权记录，并发刷新不会丢失轮换后的 Refresh Token。
- 营销模板自动补充渠道，跨渠道使用被拒绝。
- 千川返回未实现错误，不调用营销端点。
- Windows、macOS 和 Linux 凭据适配器通过同一契约测试；安全后端不可用时 fail closed。
- 原有营销创建、批量创建、素材查询和报表回归测试全部通过。
- OAuth `state` 错配、过期、重放和并发回调均被拒绝。
- 覆盖 HTTP 边界超大数字 ID、畸形 ID、陈旧索引、部分分页、部分账户展开、部分写入、崩溃恢复、迁移重试和同步竞态。
- 覆盖刷新与同步、刷新与重新授权、rebind 与旧同步并发，并在 current 指针切换前后注入崩溃。
- 在 Windows、macOS 和 Linux 运行独立进程锁竞争、持锁进程崩溃和安全释放测试。
- 覆盖独立进程中配置迁移与凭据迁移竞争，以及每次 journal 更新前后的崩溃恢复。
- OAuth rebind 拒绝不留下状态；多个冲突账户只能全部 rebind 或全部不变。
- 通过可注入的凭据适配器、文件系统、时钟、随机源、回调服务和 HTTP 客户端做故障注入；端点 spy 证明千川路径不会产生任何营销 HTTP 请求。

验收完成后，现有用户不重新授权即可继续使用巨量营销；所有状态和向导输出都能明确看出当前渠道；千川不会意外读取或调用营销配置。
