# WorkBuddy 本机接入

WorkBuddy 通过本地 stdio MCP 和三个生成的 Skill 接入共享 Go Runtime，复用本机状态和系统凭据库。当前支持查询、报表、千川作品预检及授权管理；计划、素材和模板写入等 CLI 能力不通过 Shell 补执行。此入口为本机安装，尚未发布连接器 Marketplace 包。

## 安装与更新

在包含预构建 Runtime 的仓库根目录运行，需要可用的 Python 3：

```bash
python3 scripts/install-workbuddy.py
```

macOS Apple Silicon 使用原生 arm64 Python。安装器按解释器的 `platform.machine()` 选择二进制，Rosetta 下的 Intel Python 会选到 amd64 产物：

```bash
/usr/bin/arch -arm64 /usr/bin/python3 scripts/install-workbuddy.py
```

安装器校验当前平台二进制、插件身份和 Skill/F2 资源的清单哈希，把 Runtime 放入 `~/.workbuddy/binaries/ocean-watch/` 下按版本和清单指纹固定的目录，并备份旧 MCP 配置和 Skill 到 `~/.workbuddy/backups/ocean-watch/`。它保留其他 MCP 服务，不写入 Token，也不修改 WorkBuddy 原生信任设置；存在旧 `ocean-watch-preview` 入口时会先备份再移出活动配置。

`OCEAN_WATCH_HOME` → `CODEX_HOME` → `~/.codex` 决定共享状态根，也可显式指定 `--state-home`；`--workbuddy-home` 可覆盖宿主安装目录。默认与其他宿主共用真实状态，不另建预览状态副本。

安装或更新后，重新加载 MCP 并新建会话，必要时重启 WorkBuddy；宿主要求信任时由用户在原生界面确认。更新源码和 Runtime 后重跑同一安装命令，已安装的固定目录不会自动跟随源码变化。

## 授权与能力范围

三个 Skill 的 MCP 路由从仓库共享 Skill 合同生成，WorkBuddy 专属说明来自 [skill.md](skill.md)。未提供 MCP 工具的 CLI 能力仅作范围说明，不作为可执行路由。

星图使用 `xt-plan-monitor`，对应 Runtime 渠道键为 `star_map`。`get_star_map_authorization` 读取本地授权状态及账号映射，`list_managed_accounts` 显式传 `channel=star_map`；授权与同步复用下列四个管理工具。星图账号以 `subject_id` 和 `subject_kind=star_account` 标识，不借用其他渠道的模板、计划、素材或报表能力。

授权恢复使用 `start_authorization`、`start_account_sync`、`get_authorization_session` 和 `cancel_authorization_session`。授权/重新授权直接打开官方 OAuth URL，账户同步打开本机确认页；Core 负责凭据保存与账户同步，秘密不经过模型。官方应用的回调 URI 保持 `http://127.0.0.1:8787/oauth/callback`，尚未配置 App 时先完成安全配置。详见[配置与授权](../../docs/guides/configuration.md)和[授权恢复合同](../../docs/reference/authorization-recovery.md)。

## 已验证范围

2026-10-30 已在 **WorkBuddy 5.9.3、macOS arm64** 更新本机固定目录，确认原生界面展示并启用 `ads-plan-monitor`、`qc-plan-monitor`、`xt-plan-monitor` 三项 Skill。对实际安装二进制执行的隔离 MCP 协议探测通过，覆盖 26 个工具发现和无凭据的本地夹具查询。

更新并重启宿主后，`ocean-watch` 原生界面显示连接成功及「26/26 个工具已启用」，包含三项 F2 配置工具和 `get_star_map_authorization`。实际安装的二进制、F2 包装脚本及清单与本地已验证构建逐一一致；独立进程也确认系统 Cookie 可读，旧配置会话丢失不会再被判断为保存失败。后续更新若再次触发信任提示，在「专家·技能·连接器 → 连接器 → 自定义连接器」中对 `ocean-watch` 完成原生确认。

当前构建已通过用户指定真实作品的 F2 解析、商品/账户自动路由和千川只读预检，缓存命中复核也返回可创建且无失败；测试未写入计划。该结果来自本机 MCP 执行，不能替代 WorkBuddy 自然语言流程、完整 OAuth 恢复及全部 WB1 门禁，其他平台也需分别验证。离线测试、连接成功、Token 存在或本地账户 `enabled` 均不能单独证明官方业务成功。
