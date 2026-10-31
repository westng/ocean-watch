---
name: {{SKILL}}
description: {{DESCRIPTION}}
---

本 Skill 只处理 `{{CHANNEL}}`，通过 `ocean-watch` 本地 MCP 使用共享业务数据和凭据库。渠道、应用、模板、账户和官方接口不得混用。普通业务请求直接调用匹配工具；不扫描仓库、缓存或其他宿主。

{{CHANNEL_GUIDANCE}}

| 用户目标 | MCP 路由 |
| --- | --- |
{{ROUTES}}
| 授权、重新授权或修复失效授权 | MCP `start_authorization`，显式 channel={{CHANNEL}}；重新授权时带同一 authorization_id，保留待恢复 advertiser_id |
| 同步一个精确授权的账户 | MCP `start_account_sync`，显式 channel={{CHANNEL}} 和 authorization_id |
| 查询授权恢复状态 | MCP `get_authorization_session`，使用返回的 session_id 和相同渠道 |
| 取消授权恢复 | MCP `cancel_authorization_session`，使用返回的 session_id 和相同渠道 |
| 其他已确认业务目标 | MCP `get_capabilities`，channel={{CHANNEL}}；只解释能力目录 |

遇到 `REAUTHORIZATION_REQUIRED` 或用户要求重新授权，调用 `start_authorization`。如果已有授权状态提供 `authorization_id`，必须沿用它；只有首次授权才省略该字段。向用户提供返回的 `authorization_url`，授权/重新授权时直接打开官方 OAuth 页面，账户同步时打开本机确认页；启动会话不代表已经授权。不要要求执行命令或把 Secret、Token、授权码、Cookie 粘贴到聊天。

保留用户原请求的模板、作品链接、账户和日期。查询 `get_authorization_session` 直到终态；等待用户操作时告知当前状态，避免紧密轮询。仅当 `status=completed` 且 `can_resume=true`，才用原参数重新调用原来的查询/预检，不要求用户重发已提供的链接。授权恢复不授予投放提交权限。

`ACCOUNT_SYNC_REQUIRED` 时从当前渠道授权检查工具取得待同步的精确 authorization_id，调用 `start_account_sync`。会话 `sync_required=true` 时使用会话返回的 authorization_id，保留 advertiser_id；不要重复 OAuth。账户冲突默认失败，只有用户明确要求改变归属时才传 `rebind_existing=true`。`failed`、`cancelled`、`expired` 或账户范围不符时如实报告，停止自动重试。`AUTHORIZATION_BUSY` 表示已有流程或回调端口占用，不重装、不杀其他进程。

账户清单和模板来自本地配置；`enabled` 不代表官方已授权。只有本次工具返回 `source=official_api` 才称为官方数据，保留日期、范围、分页、截断及部分失败。不得用合成账户或历史聊天结果替代真实查询。

所有业务调用使用此连接器的 MCP；能力目录中的 CLI 命令不等于已提供的工具，不切换到 Shell/CLI/其他连接器执行。缺少工具、Host 信任或连接不可用时报告真实阻塞。创建计划、提交预检、预算/ROI/状态及删除不在此接入支持范围内；不能借授权恢复绕过投放审批。

千川作品预检会查询官方并保存本地短期快照，不创建或修改投放计划。`presentation.required=true` 时原样展示 `presentation.rendered_markdown`；千川批量结果保留 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题` 顺序并说明跳过与失败。仅追问真正缺失的业务参数；回复不展示秘密、内部路径或原始响应。
