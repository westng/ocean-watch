# 快速开始

## 1. 安装

需要 Codex CLI `0.144.1+`、Claude Code 或豆包工作。普通营销和千川命令直接使用 Plugin 内置 Go CLI；千川抖音作品链接流程另需 Python `3.10+` 和 F2 `0.0.1.7`。

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

豆包工作 (Doubao Work)：

```bash
# 1. 通过 Codex 或 Claude Code 安装插件
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch

# 2. 配置豆包工作 Skills
cd ~/.codex/plugins/ocean-watch  # 或你的插件安装目录
./scripts/install-doubao.sh

# 3. 完全退出并重启豆包工作
# 4. 在对话中使用：@巨量千川计划助手 或 @巨量引擎盯盘助手
```

三个 Host 共用同一份本地状态，授权只需完成一次。

首次安装后新建任务。普通用户直接说”帮我初始化巨量引擎盯盘”即可，不必记忆命令。兼容升级只替换本机业务 Runtime，已加载的稳定 MCP 代理会在同一任务内自动切换，不要求退出或重启客户端；若升级增加/删除工具、修改 Schema 或 Skill 触发合同,则属于 Host 合同升级，需要新任务加载新的工具清单。

## 2. 环境与配置

从任一 Skill 目录启动同一套 Go CLI：

```bash
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run setup init --home-config
```

Windows 使用 `skills\ads-plan-monitor\run.cmd`。`setup doctor` 分别检查 Python `3.10+`、当前解释器中的固定 F2 `0.0.1.7`、平台、Codex CLI、安全凭据后端和 OAuth 回调端口；它不会安装依赖、修改系统设置或发起 OAuth。

默认配置位于 `$CODEX_HOME/ads-plan-monitor/config.json`。状态根按 `OCEAN_WATCH_HOME`、`CODEX_HOME`、`~/.codex` 依次解析，两个 Host 共用同一个根。配置不包含 Secret 或 Token。

## 3. 分渠道授权

营销和千川必须分别授权：

```bash
skills/ads-plan-monitor/run auth authorize --channel marketing --print-url --no-open
skills/qc-plan-monitor/run auth authorize --channel qianchuan --print-url --no-open
```

首次授权会用本地表单收集对应渠道的 App ID 和 Secret，保存到操作系统凭据后端，然后跳转官方 OAuth。开放平台登记的统一回调地址是：

```text
http://127.0.0.1:8787/oauth/callback
```

只在目标账号所在的浏览器环境中打开命令返回的临时 `start_url`，并保持命令运行到回调、广告主同步和映射检查结束。回调地址本身不是手动打开入口。

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

在 Codex 中直接用自然语言查找或查看模板，Skill 会调用 Plugin 的 `list_templates` 与 `get_template`，不要求用户运行 CLI。营销模板区分账户上传与达人授权素材；千川模板区分商品全域与直播全域。默认模板只是创建骨架，实际投放必须选择绑定渠道、广告主和商品的业务模板。

千川作品链接先由 F2 批量补充公开达人/商品提示，再使用官方千川接口定向确认达人授权、作品归属和商品匹配。F2 不可用或身份缺失时只跳过无法验证的作品，不扫描全部授权达人。

所有计划命令默认预览。确认广告主、模板、商品、素材、预算和 ROI 后，才在同一命令增加 `--submit`；千川删除还要求 `--confirm-delete`。

## 6. 报表

```bash
skills/ads-plan-monitor/run reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD
skills/qc-plan-monitor/run qc-reports plans --advertiser-id ADVERTISER_ID --start-date YYYY-MM-DD --end-date YYYY-MM-DD
```

默认输出一个结构化 JSON 文档。只有显式 `--out PATH` 才写文件；当结果包含强制 Presentation 时，Codex 会按其中的 Markdown、列和指标口径展示。

下一步可阅读[配置与授权](configuration.md)和[CLI 参考](cli.md)。
