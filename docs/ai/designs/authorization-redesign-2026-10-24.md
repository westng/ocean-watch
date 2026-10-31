# 授权方案重做设计

> 文档属性：AI 辅助设计，适用范围：Ocean Watch 的巨量营销、巨量千川和巨量星图本地授权；权威边界：本文件描述目标与迁移约束，不代表代码已经完成或真实账户已经验收。

> 状态：历史初稿，授权业务顺序与恢复语义已由 [2026-10-29 实施计划](../plans/authorization-flow-refactor-2026-10-29.md) 和[正式授权合同](../../reference/authorization-recovery.md)替代。下文“当前实现”、旧版本仍有效及本机确认页等表述是初稿假设，不能作为当前行为或平台 Token 有效性的依据。SQLite 授权状态迁移和跨重启会话恢复仍属于独立范围。

## 现状结论

当前实现把四件不同的事情放在同一条流程里：OAuth App 配置、官方 OAuth grant、广告主账户发现/绑定、访问 Token 刷新。CLI 和 MCP 又各自维护一套浏览器回调流程。结果是：

- MCP 重新授权时总是新建授权 ID，不能表达“更新哪个既有授权”；
- MCP 路径默认 `RebindExisting=true`，一次重新登录可能覆盖已有广告主映射；
- 缺少 App 配置时，浏览器确认页要求用户把 App Secret 填进网页；
- 授权索引仍由 JSON 维护，而目标状态模型已经要求 SQLite 事务、OS 凭据引用和可恢复的 Token 轮换；
- `star_map` 已有 CLI/provider 支持，但 MCP 恢复合同没有覆盖它；
- 授权会话只在当前 Runtime 内存中存在，宿主退出后只能重新开始，且不会保留“已完成 OAuth、等待账户同步”的可恢复意图。

这些问题属于边界和状态模型错误，继续在旧流程上追加错误码不会解决。

## 新的边界

授权分成三个独立对象：

1. **App 配置**：每个渠道一个 `app_id` 和 `secret`，只写入 OS Credential Store。只允许 CLI/受控本机配置入口写入，MCP 和浏览器确认页不能提交 Secret。
2. **Authorization grant**：一次官方 OAuth 用户授权，稳定的 `authorization_id` 代表这次 grant。重新授权必须带已有 ID；授权成功后原 ID 原子切换到新的凭据版本，失败时旧版本继续有效。
3. **Account scope snapshot**：官方完整账户发现结果，单独记录账户/广告主范围、来源、摘要和同步时间。新 grant 默认不能抢占其他 grant 的账户；需要变更绑定时必须走显式账户同步/重绑动作。

查询、刷新和写入只接受 `channel + authorization_id/advertiser_id` 解析出的绑定，不猜测跨渠道身份。`advertiser_id` 只能用于定位唯一既有 grant；多个匹配时直接返回歧义。

## 状态机

### 授权会话

```text
created
  → awaiting_user
  → provider_pending
  → exchanging
  → snapshot_pending
  → completed

任意未完成状态 → cancelled / expired / failed
snapshot_pending → account_sync_required
```

会话返回的 `authorization_url` 只指向本机短期确认页。MCP 请求只创建会话，不持有浏览器或官方网络请求。会话状态必须可查询；状态里不能出现授权码、Token、Secret、原始官方响应或可执行命令。

### Grant 与凭据

`authorizations` 记录稳定身份、渠道、凭据引用、当前 revision、状态和快照摘要；Secret/Access Token/Refresh Token 仍只在 OS Credential Store。刷新遵循“持久化轮换意图 → 官方刷新 → 写入候选凭据 → 原子激活 revision”的顺序，已有的 SQLite rotation 端口作为唯一实现，不再用 JSON revision 递增替代它。

## 迁移与兼容

- 旧 JSON 授权只能作为一次性只读输入；迁移前备份，无法唯一确定 grant 或账户范围时进入隔离状态，不能自动重绑。
- 新代码不删除旧凭据、不覆盖旧授权映射，直到新 snapshot 成功并通过冲突检查；失败只留下待同步事实。
- CLI 与 MCP 共享同一 Application/Port。浏览器流程只负责用户确认和 OAuth callback，不能再拥有一套业务授权逻辑。
- 本轮第一步先落地三个硬门禁：显式既有 grant 重新授权、禁止 MCP 默认重绑、禁止 MCP 网页填写 App Secret；随后接入 SQLite 授权快照和跨重启会话恢复。

## 验收边界

本地测试必须覆盖：新 grant、既有 grant 原子换证、账户冲突拒绝、显式重绑、重复 callback、过期/取消、缺少 App 配置、三渠道 state 隔离和 Token 不出输出。真实官方 OAuth、桌面 Host 版本、真实账户范围和线上发布仍需独立验收，不能由这些单元测试代替。
