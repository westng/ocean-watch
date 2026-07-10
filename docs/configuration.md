# 配置与授权

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

`ocean-watch` 把业务配置和 OAuth 凭据分开保存：

- 业务配置：项目本地 `config/ads-plan-monitor/config.json`
- OAuth 凭据：本机系统凭据仓库

这样做是为了避免把 token、secret 或授权码提交到 GitHub。

## 创建配置

```bash
mkdir -p config/ads-plan-monitor
cp assets/config.example.json config/ads-plan-monitor/config.json
```

`config/` 已被 `.gitignore` 排除。这个目录用于保存每个使用者自己的真实广告账户和模板信息。

所有命令按以下顺序查找配置：命令行 `--config`、环境变量 `ADS_PLAN_MONITOR_CONFIG`、项目配置、`~/.codex/ads-plan-monitor/config.json`。

## 配置内容

需要按业务填写的典型字段：

| 字段 | 用途 |
| --- | --- |
| `account.advertiser_id` | 巨量引擎广告主 ID |
| `active_plan_template` | 默认创建计划模板名 |
| `plan_templates` | 不同平台/商品的创建参数模板 |
| `defaults` | 预算、出价、ROI、投放设置、命名模板 |
| `resolved_ids` | 城市、商品、图片、转化资产、落地页等官方 ID |
| `tracking_urls` | 展示和点击/有效触点监测链接 |
| `links` | 落地页和直达链接 |
| `titles` | 单元标题素材 |

不要填写到项目配置里的字段：

- `app_id`
- `secret`
- `access_token`
- `refresh_token`
- `auth_code`

## 模板命名

建议使用：

```text
平台-CID-商品名-商品ID
```

示例：

```text
示例平台-CID-示例商品-REPLACE_WITH_PRODUCT_ID
```

同一仓库可以维护多个模板。Codex 默认读取 `active_plan_template`；用户在对话里指定模板时，脚本会使用对应 `plan_templates.<模板名>`。

## 保存 App 凭据

首次使用前，把巨量引擎开放平台 App ID 和 Secret 保存到本机凭据仓库：

```bash
python3 scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --set-app
```

脚本会交互式要求输入 App ID 和 Secret。不要把它们写进配置文件或聊天记录。

凭据后端：macOS 使用 Keychain，Windows 使用 DPAPI，Linux 使用 Secret Service。Linux 缺少 `secret-tool` 时应安装系统的 `libsecret` 工具；脚本不会自动改用明文文件。仅限受限开发环境，可显式设置：

```bash
export ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1
```

该选项会把凭据以明文保存到用户目录，不应用于生产广告账户。

## OAuth 授权

默认回调地址：

```text
http://127.0.0.1:8787/oauth/callback
```

巨量引擎开放平台应用里的回调地址必须与本地配置一致。

启动授权：

```bash
python3 scripts/oauth_local_authorize.py \
  --config config/ads-plan-monitor/config.json
```

流程：

1. 脚本启动本地 HTTP 回调服务。
2. 浏览器打开官方 OAuth 授权页。
3. 用户选择授权账户并确认。
4. 回调拿到 `auth_code`。
5. 脚本换取 token，并写入本机凭据仓库。
6. 终端只输出脱敏状态。

## 检查状态

```bash
python3 scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --status
```

如果 `advertiser_id_authorized` 为 `false`，说明当前配置的广告主没有包含在本次 OAuth 授权账户里，需要重新授权或切换广告主。

## Token 刷新

所有查询和创建脚本都会在调用官方 API 前检查 Access Token。剩余有效期不足 30 分钟时，脚本使用本机凭据仓库中的 Refresh Token 自动刷新，并保存官方返回的新 Access Token 和轮换后的 Refresh Token；并发任务通过本地锁避免重复刷新。

查看状态：

```bash
python3 scripts/token_manager.py --status
```

手动强制刷新：

```bash
python3 scripts/token_manager.py --refresh
```

状态中的 `next_action` 含义：`ready` 可直接调用，`refresh` 会在下次 API 调用前刷新，`reauthorize` 表示 Refresh Token 缺失或已过期，需要重新运行 `scripts/oauth_local_authorize.py`。

## 授权账户同步

OAuth 换 Token 响应不是完整的广告主账户详情。首次或重新授权成功后，脚本会先调用官方 `/oauth2/advertiser/get/` 获取授权主体，再按 `account_role` 展开真实广告主：

- `ADVERTISER`：直接使用该广告主 ID。
- `CUSTOMER_ADMIN` / `CUSTOMER_OPERATOR`：调用 `/2/customer_center/advertiser/list/`，参数包含 `account_source=AD`。
- `PLATFORM_ROLE_ENTERPRISE_BP_ADMIN` / `PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR`：调用 `/2/ebp/advertiser/list/`。
- 最后按 50 个一组调用 `/2/advertiser/info/` 验证广告主 ID。

状态区分：

- `oauth_authorized_account_count`：官方接口返回的全部授权主体，可能包含客户中心、企业 BP、星图等角色账户。
- `authorized_advertiser_count`：角色展开并通过广告主信息接口验证后的真实广告主账户。

不要把授权主体数量当作可投放广告主数量。手动重新同步可运行：

```bash
python3 scripts/token_manager.py --sync-accounts
```

普通 Access Token 刷新只轮换 Token，不重复展开账户关系，避免每次刷新拖慢查询和创建流程。

如果授权主体存在但真实广告主为 0，重新运行本地 OAuth，并在官方授权页选择目标广告主账户。

## 配置校验

```bash
python3 scripts/first_run.py \
  --config config/ads-plan-monitor/config.json

python3 scripts/validate_config.py \
  config/ads-plan-monitor/config.json \
  --mode all
```

`validate_config.py` 支持 `query`、`create-preview`、`create-submit` 和 `all` 四种模式，所选模式未就绪时返回非零退出码。`first_run.py` 更适合第一次使用；验证器更适合提交前或排查字段缺失。
