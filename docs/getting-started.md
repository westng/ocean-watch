# 快速开始

本文从全新安装走到第一次只读查询或计划预览。命令参数详见 [CLI 参考](cli.md)，配置字段详见[配置与授权](configuration.md)。

## 1. 准备环境

本节描述当前已发布的生产路径。Go SDK 运行时仍处于隔离 Shadow 和发布 Gate 中，普通用户不需要安装 Go，也不要直接运行 `prototype/` 下的候选程序。

需要：

- Codex CLI `0.144.1+`
- Python `3.9+`
- 巨量引擎开发者账号及所需 API 权限
- 可用的本机安全凭据仓库：macOS Keychain、Windows DPAPI 或 Linux Secret Service

安装 Plugin：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建一个 Codex 任务。首次使用先运行：

```bash
ocean-watch setup doctor
```

该命令只检查环境，不安装 Python、不修改系统设置，也不发起 OAuth。

当前 Plugin 的生产策略仍将全部命令路由到 Python。未来 Go 路由启用后会由签名发布 manifest 和 native bootstrap 选择同版本运行时，不会要求用户手工切换；当前状态见[架构说明](architecture.md)。

## 2. 初始化配置

推荐把配置写入用户目录：

```bash
ocean-watch setup init --home-config
```

默认位置为 `$CODEX_HOME/ads-plan-monitor/config.json`；未设置 `CODEX_HOME` 时使用 `~/.codex/ads-plan-monitor/config.json`。仓库开发时也可使用被 Git 忽略的 `config/ads-plan-monitor/config.json`。

初始化只写入非敏感业务配置。App Secret、Access Token 和 Refresh Token 由操作系统凭据仓库保存。

## 3. 完成 OAuth

营销和千川必须分别授权：

```bash
ocean-watch auth authorize --channel marketing
ocean-watch auth authorize --channel qianchuan
```

首次授权某个渠道时，本地表单会先收集该渠道的 App ID 和 Secret，再跳转官方 OAuth。需要更换应用但不立即授权时使用：

```bash
ocean-watch auth set-app --channel marketing
```

开放平台回调地址统一登记为：

```text
http://127.0.0.1:8787/oauth/callback
```

这是官方回调地址，不是手动访问入口。本地服务只在授权命令运行期间监听。

在 Codex 中需要自行选择浏览器分组时：

```bash
ocean-watch auth authorize --channel qianchuan --print-url --no-open
```

在目标账户对应的浏览器中打开输出的临时 `start_url`，并保持命令运行到授权和账户同步结束。

检查状态：

```bash
ocean-watch auth status --channel marketing
ocean-watch auth status --channel qianchuan
```

若 Token 已保存但账户同步失败，按输出中的本地授权 ID 重试：

```bash
ocean-watch auth sync-accounts \
  --channel qianchuan \
  --authorization-id AUTHORIZATION_ID
```

## 4. 配置负责账户

负责账户是本机维护的常用账户簿，不等于 OAuth 返回的全部账户：

```bash
ocean-watch accounts add \
  --channel marketing \
  --advertiser-id ADVERTISER_ID \
  --name ACCOUNT_NAME

ocean-watch accounts add \
  --channel qianchuan \
  --advertiser-id ADVERTISER_ID \
  --name ACCOUNT_NAME
```

当多份 OAuth 授权同时覆盖同一广告主时，增加 `--auth-account-id AUTH_ACCOUNT_ID` 固定所属授权。分别检查账户簿和当天表现：

```bash
ocean-watch accounts list
ocean-watch accounts report
```

“我负责的账户”“我常用的账户”等名单问法只执行 `accounts list`，不会刷新 Token 或查询表现。只有明确问消耗、GMV、ROI、订单或日期表现时才执行 `accounts report`。跨渠道报表会隔离单账户失败；营销与千川 GMV 口径不同，因此跨渠道只合计消耗，GMV 和 ROI 按渠道展示。

## 5. 创建业务模板

统一入口会先选择渠道：

```bash
ocean-watch templates create
```

也可以直接进入指定渠道：

```bash
ocean-watch templates create --channel marketing
ocean-watch templates create --channel qianchuan
```

营销模板需选择账户上传素材或达人授权素材，并绑定广告主、商品和投放参数。千川模板绑定广告主及 1–30 个商品 ID。

列出或查看模板：

```bash
ocean-watch templates list
ocean-watch templates show --channel marketing --template TEMPLATE_NAME
ocean-watch templates show --channel qianchuan --template TEMPLATE_ID_OR_NAME
```

默认模板只是创建骨架。真实创建必须显式指定业务模板，不存在“当前模板”或“默认业务模板”。

## 6. 先查询，再预览

营销上传素材：

```bash
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch plans create --plan-template TEMPLATE_NAME --video-id VIDEO_ID
```

营销达人素材：

```bash
ocean-watch materials creator --aweme-id AWEME_ID
ocean-watch plans create-creator --plan-template TEMPLATE_NAME --item-id ITEM_ID
```

千川达人素材和作品链接：

```bash
ocean-watch qc-materials creator-videos \
  --plan-template TEMPLATE_ID \
  --douyin-id DOUYIN_SHOW_ID

ocean-watch plans batch-qianchuan-works \
  --plan-template TEMPLATE_ID \
  --work-url DOUYIN_WORK_URL
```

这些计划命令默认 dry-run。确认预览中的广告主、模板、商品、素材、预算和 ROI 后，才在同一命令末尾增加 `--submit`。批量营销达人任务先使用 `--preflight`，确认断点和阻断项后再改为 `--submit`。

## 7. 查询报表

```bash
ocean-watch reports materials --start-date YYYY-MM-DD --end-date YYYY-MM-DD

ocean-watch qc-reports plans \
  --advertiser-id ADVERTISER_ID \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

默认输出结构化 JSON 到终端；只有显式传入 `--out PATH` 的命令才写文件。

## 下一步

- 需要完整参数和批量工作流：阅读 [CLI 参考](cli.md)。
- 需要配置模板、作品解析服务或排查 Token：阅读[配置与授权](configuration.md)。
- 准备修改代码：阅读[架构说明](architecture.md)和[贡献指南](../CONTRIBUTING.md)。
