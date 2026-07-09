# ocean-watch

巨量引擎盯盘助手，一个开源的 Codex Skill。它把巨量引擎广告计划创建、账户素材查询、素材维度报表和盯盘策略分析收在同一个 `ads-plan-monitor` 技能里，通过官方 Ocean Engine Marketing API 工作。

> 这是给 Codex 使用的 Skill 仓库，不是独立 SaaS 或网页后台。所有广告操作都通过本机脚本和官方 API 执行。

## 功能

- 首次使用向导：检查本地配置、OAuth 授权状态和创建计划所需字段。
- 本地 OAuth：通过 `127.0.0.1` 回调完成官方授权，token 存入本机凭据仓库。
- 创建计划：按可配置商品/平台模板创建项目和单元。
- 批量创建：按当天上传的视频素材分组创建计划，支持多账户并发。
- 查询数据：查询账户下单元、素材、视频素材和素材维度报表。
- 盯盘策略：基于账户和素材数据给出只读分析建议。

## 目录

```text
skills/ads-plan-monitor/
├── SKILL.md
├── agents/openai.yaml
├── assets/
│   ├── config.example.json
│   └── plan-input.example.json
├── references/
│   ├── current-template-notes.md
│   └── official-api-notes.md
└── scripts/
```

## 安装

把 `skills/ads-plan-monitor` 复制或软链接到你的 Codex skills 目录：

```bash
mkdir -p ~/.codex/skills
ln -s "$(pwd)/skills/ads-plan-monitor" ~/.codex/skills/ads-plan-monitor
```

也可以直接把整个仓库作为项目目录使用。Codex 在项目中看到 `skills/ads-plan-monitor/SKILL.md` 后，可以按本地 Skill 使用。

## 初始化

复制示例配置：

```bash
mkdir -p config/ads-plan-monitor
cp skills/ads-plan-monitor/assets/config.example.json config/ads-plan-monitor/config.json
```

编辑 `config/ads-plan-monitor/config.json`，只填写业务配置，例如广告主 ID、商品模板、链接模板、素材和城市等。不要把 `app_id`、`secret`、`access_token`、`refresh_token` 写进项目配置。

保存 App ID 和 Secret 到本机凭据仓库：

```bash
python3 skills/ads-plan-monitor/scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --set-app
```

启动本地 OAuth 授权：

```bash
python3 skills/ads-plan-monitor/scripts/oauth_local_authorize.py \
  --config config/ads-plan-monitor/config.json
```

默认回调地址是：

```text
http://127.0.0.1:8787/oauth/callback
```

这个地址需要与你在巨量引擎开放平台应用里配置的回调地址一致。

## 常用命令

检查配置：

```bash
python3 skills/ads-plan-monitor/scripts/first_run.py \
  --config config/ads-plan-monitor/config.json

python3 skills/ads-plan-monitor/scripts/validate_config.py \
  config/ads-plan-monitor/config.json
```

查看 token 状态，输出会自动脱敏：

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --status
```

查询今天上传的视频素材：

```bash
python3 skills/ads-plan-monitor/scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode library-get \
  --date today \
  --fetch-all
```

查询当前账户素材表现：

```bash
python3 skills/ads-plan-monitor/scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json
```

创建计划前预览 payload：

```bash
python3 skills/ads-plan-monitor/scripts/create_plan.py \
  --config config/ads-plan-monitor/config.json \
  --plan-template "平台-CID-商品名-商品ID" \
  --video-id REPLACE_WITH_VIDEO_ID
```

真实提交创建计划时再加 `--submit`：

```bash
python3 skills/ads-plan-monitor/scripts/create_plan.py \
  --config config/ads-plan-monitor/config.json \
  --plan-template "平台-CID-商品名-商品ID" \
  --video-id REPLACE_WITH_VIDEO_ID \
  --submit
```

按当天视频素材分组批量创建，默认先 dry-run：

```bash
python3 skills/ads-plan-monitor/scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5
```

确认无误后再提交：

```bash
python3 skills/ads-plan-monitor/scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5 \
  --submit
```

## 在 Codex 中使用

示例提问：

- `用 ads-plan-monitor 初始化配置`
- `查询当前广告账户今天素材消耗前十`
- `查询今天上传的视频素材`
- `按今天上传的视频素材，每 5 条一个单元创建计划，先 dry-run`
- `使用某个计划模板，拿这条视频素材创建一条计划`
- `根据素材维度数据给我盯盘建议`

Skill 会优先做只读查询和 payload 预览。真实创建计划需要用户明确要求提交。

## 安全

- 不要提交 `config/ads-plan-monitor/config.json`。
- 不要提交 `runs/`、`.venv/`、日志、临时 JSON 或 CSV。
- 不要在聊天、README、issue、commit message 中粘贴 token、secret 或 auth code。
- 本项目默认把 OAuth 凭据放在本机凭据仓库：
  - macOS: Keychain
  - Windows: DPAPI 保护的用户本地文件
  - Linux: Secret Service
  - 受限环境下才使用本地 fallback 文件

更多细节见 [SECURITY.md](SECURITY.md)。

## 开发检查

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile skills/ads-plan-monitor/scripts/*.py
python3 -B skills/ads-plan-monitor/scripts/validate_config.py config/ads-plan-monitor/config.json
```

## 许可证

本仓库尚未附带许可证文件。公开发布前建议补充 `LICENSE`。
