# 配置与授权

`ocean-watch` 把业务配置和 OAuth 凭据分开保存：

- 业务配置：项目本地 `config/ads-plan-monitor/config.json`
- OAuth 凭据：本机系统凭据仓库

这样做是为了避免把 token、secret 或授权码提交到 GitHub。

## 创建配置

```bash
mkdir -p config/ads-plan-monitor
cp skills/ads-plan-monitor/assets/config.example.json config/ads-plan-monitor/config.json
```

`config/` 已被 `.gitignore` 排除。这个目录用于保存每个使用者自己的真实广告账户和模板信息。

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
python3 skills/ads-plan-monitor/scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --set-app
```

脚本会交互式要求输入 App ID 和 Secret。不要把它们写进配置文件或聊天记录。

## OAuth 授权

默认回调地址：

```text
http://127.0.0.1:8787/oauth/callback
```

巨量引擎开放平台应用里的回调地址必须与本地配置一致。

启动授权：

```bash
python3 skills/ads-plan-monitor/scripts/oauth_local_authorize.py \
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
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --status
```

如果 `advertiser_id_authorized` 为 `false`，说明当前配置的广告主没有包含在本次 OAuth 授权账户里，需要重新授权或切换广告主。

## 配置校验

```bash
python3 skills/ads-plan-monitor/scripts/first_run.py \
  --config config/ads-plan-monitor/config.json

python3 skills/ads-plan-monitor/scripts/validate_config.py \
  config/ads-plan-monitor/config.json
```

`first_run.py` 更适合第一次使用；`validate_config.py` 更适合提交前或排查字段缺失。
