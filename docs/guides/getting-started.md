# 快速开始

## 1. 安装

选择支持 Plugin 的 Codex / Claude Code、DeepSeek Harness 桌面版、豆包工作或 WorkBuddy。普通营销和千川流程使用内置 Go Runtime，无需安装 Go；千川抖音作品链接流程另需 Python `3.10+` 和 F2 `0.0.1.7`。WorkBuddy 本机安装器另需可用的 Python 3。

Codex：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Claude Code：

```bash
claude plugin marketplace add westng/ocean-watch
claude plugin install ocean-watch@ocean-watch
```

DeepSeek Harness 桌面版 `0.2.0-rc.2`：在应用的「插件 → 添加插件」中输入本仓库的绝对路径，安装到当前 `desktop` profile。无需安装独立 npm/CLI 宿主。

安装后同时启用三个 Skill 与本地 MCP 工具。详见 [DeepSeek Harness 接入](deepseek-harness.md)。

WorkBuddy：从包含预构建 Runtime 的仓库根目录运行：

```bash
python3 scripts/install-workbuddy.py
```

macOS Apple Silicon 使用原生 arm64 Python，避免 Rosetta 解释器误选 Intel 二进制：

```bash
/usr/bin/arch -arm64 /usr/bin/python3 scripts/install-workbuddy.py
```

安装器校验清单并备份旧配置和 Skill，保留其他 MCP 服务与原生信任设置。安装后重新加载 MCP / 新建会话，必要时重启 WorkBuddy。2026-10-30 已在 WorkBuddy `5.9.3`、macOS arm64 验证本机安装、三项 Skill 原生展示及重启后连接成功，26/26 个工具已启用，包含星图授权查询及 F2 配置工具；安装产物的隔离 MCP 协议探测也通过。完整业务验收仍待完成。详见 [WorkBuddy 本机接入](../../hosts/workbuddy/README.md)。

豆包工作（macOS Apple Silicon）：先在豆包工作中导入 `skills/` 下的三个 Skill，再从保留于本机的仓库根目录运行：

```bash
bash scripts/install-doubao.sh
```

脚本只更新已有用户 Skill 的 MCP 路径，使用仓库内的 arm64 Runtime，不负责安装 Skill。完成后完全退出并重启豆包工作；当前 V3 的实际发现和业务验收待完成。

所有宿主应使用同一份本地状态，同一渠道的有效授权可复用，营销与千川仍需分别授权。

首次安装后新建任务。普通用户直接说“帮我完成千川授权”即可，不必记忆命令。升级行为按宿主合同处理：Codex 的兼容 Runtime 升级可由稳定代理切换；Claude Code 更新插件后需要新任务；WorkBuddy 通过重跑安装器更新 Ocean Watch，再重新加载 MCP；DeepSeek Harness 和豆包工作在更新包或 Skill 后应按各自宿主流程重新加载并验收。若升级增加/删除工具、修改 Schema 或 Skill 触发合同，必须新建任务加载新的工具清单。

下文 CLI 示例适用于支持本机命令的入口。WorkBuddy 当前通过 MCP 执行查询、预检和授权管理，未提供模板、计划等写入入口，也不通过 Shell 补执行 CLI。

## 2. 环境与配置

从仓库根目录通过任一 Skill 的启动器使用同一套 Go CLI：

```bash
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run setup init --home-config
```

Windows 使用 `skills\ads-plan-monitor\run.cmd`。`setup doctor` 分别检查 Python `3.10+`、当前解释器中的固定 F2 `0.0.1.7`、平台、CLI 环境、安全凭据后端和 OAuth 回调 URI 配置；它不监听或验证回调端口，不会安装依赖、修改系统设置或发起 OAuth。

托管 Runtime 还会在 doctor 结果中报告当前执行版本、已选 Runtime slot、已安装候选的 manifest 指纹和 MCP 工具数量；如果这些内容不一致，会明确阻断并提示重新安装 Plugin，授权状态无需重复配置。

默认配置位于 `<共享状态根>/ads-plan-monitor/config.json`。状态根按 `OCEAN_WATCH_HOME`、`CODEX_HOME`、`~/.codex` 依次解析，所有宿主应使用同一个根。配置不包含 Secret 或 Token。

## 3. 分渠道授权

营销和千川必须分别授权。

星图通过独立的 `xt-plan-monitor` Skill 接入，授权管理显式使用 `channel=star_map`。本地状态查询用 `get_star_map_authorization`，负责账号查询用 `list_managed_accounts` 的 `star_map` 过滤。星图账号使用 `subject_kind=star_account`；星图计划、素材和报表尚未接入，不复用营销或千川能力。

在宿主对话中，Skill 使用 `start_authorization` 返回的官方 OAuth URL，并通过 `get_authorization_session` 等待结果。已保存授权但账户待同步时使用 `start_account_sync` 的本机确认页。不要在聊天中粘贴 Secret、Token 或授权码。MCP 尚未配置对应渠道 App 时，应先按[配置与授权](configuration.md)完成安全配置；恢复工具不通过模型或浏览器表单收集 App Secret。

CLI 授权示例：

```bash
skills/ads-plan-monitor/run auth authorize --channel marketing --print-url --no-open
skills/qc-plan-monitor/run auth authorize --channel qianchuan --print-url --no-open
```

CLI 尚未配置应用时，会用本地表单收集对应渠道的 App ID 和 Secret，保存到操作系统凭据后端，然后跳转官方 OAuth；已配置应用时直接进入授权。开放平台登记的统一回调地址是：

```text
http://127.0.0.1:8787/oauth/callback
```

只在目标账号所在的浏览器环境中打开命令返回的临时 `start_url`，并保持命令运行到回调、广告主同步和映射检查结束。回调地址本身不是手动打开入口。

回调后 Runtime 依次兑换 Token、读取官方登录用户 UID、保存待同步授权，再按角色获取广告账户。客户中心、EBP、千川店铺分别展开，广告主去重并批量校验后激活快照。MCP 共用这条流程，通过会话查询等待结果。

更新已有授权时明确传入 `--authorization-id AUTHORIZATION_ID`。未指定 ID 时，同一渠道、App 和已验证 UID 只复用唯一既有记录；身份未知的旧记录不自动合并。若授权已保存但账户同步失败，使用返回的授权 ID 补同步：

```bash
skills/ads-plan-monitor/run auth sync-accounts --channel marketing --authorization-id AUTHORIZATION_ID
```

补同步无需重新 OAuth；待同步状态解除前，业务查询不会使用该授权的旧账户范围。详见[授权与恢复合同](../reference/authorization-recovery.md)。

状态和重新同步：

```bash
skills/ads-plan-monitor/run auth status --channel marketing
skills/ads-plan-monitor/run auth sync-accounts --channel marketing
skills/qc-plan-monitor/run auth mappings --channel qianchuan
```

当用户表达“当前账号下新增了广告主”“本地广告主不全”“让权限和官方保持一致”等结果意图时，Skill 应理解为刷新当前授权用户的广告主覆盖，不要求固定词汇。

## 4. 负责账户

负责账户是本机常用账户簿，不等于 OAuth 返回的全部广告主：

```bash
skills/ads-plan-monitor/run accounts add --channel marketing --advertiser-id ADVERTISER_ID --name ACCOUNT_NAME
skills/qc-plan-monitor/run accounts add --channel qianchuan --advertiser-id ADVERTISER_ID --name ACCOUNT_NAME
skills/ads-plan-monitor/run accounts list
skills/ads-plan-monitor/run accounts report
```

“我负责哪些账户”只读取本地名单；只有询问消耗、GMV、ROI、订单或日期表现才调用官方报表。

## 5. 模板与计划预检

```bash
skills/ads-plan-monitor/run templates create
skills/qc-plan-monitor/run qc-products search --advertiser-id ADVERTISER_ID --keyword PRODUCT
skills/qc-plan-monitor/run plans batch-qianchuan-works --plan-template TEMPLATE_ID --work-url DOUYIN_WORK_URL
```

在已加载对应 MCP 工具的宿主中，直接用自然语言查找或查看模板，Skill 会调用 `list_templates` 与 `get_template`。千川作品预检使用 `preflight_qianchuan_works`，快照查看使用 `get_qianchuan_preflight`。营销模板区分账户上传与达人授权素材；千川模板区分商品全域与直播全域。默认模板只是创建骨架，实际投放必须选择绑定渠道、广告主和商品的业务模板。

千川作品链接先由 F2 批量补充公开达人/商品提示，再使用官方千川接口定向确认达人授权、作品归属和商品匹配。F2 不可用时可用有效的身份缓存继续定向官方复核；仍无法验证的作品单独跳过，不扫描全部授权达人。

所有计划命令默认预览。确认广告主、模板、商品、素材、预算和 ROI 后，才在同一命令增加 `--submit`；千川删除还要求 `--confirm-delete`。

## 6. 报表

```bash
skills/ads-plan-monitor/run reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
skills/qc-plan-monitor/run qc-reports plans --advertiser-id ADVERTISER_ID --start-date YYYY-MM-DD --end-date YYYY-MM-DD
```

CLI 默认输出一个结构化 JSON 文档。只有显式 `--out PATH` 才写文件；当 CLI / MCP 结果包含强制 Presentation 时，Skill 应按其中的 Markdown、列和指标口径展示。

下一步可阅读[配置与授权](configuration.md)和[CLI 参考](../reference/cli.md)。
