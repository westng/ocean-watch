# 常用命令

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

下面命令默认从 `config/ads-plan-monitor/config.json` 读取业务配置，并从本机凭据仓库读取 OAuth Token。当前业务命令默认渠道为 `marketing`（巨量营销）；`qianchuan` 尚未实现，不会复用营销凭据。

## 初始化和检查

```bash
python3 skills/ads-plan-monitor/scripts/first_run.py \
  --config config/ads-plan-monitor/config.json

python3 skills/ads-plan-monitor/scripts/validate_config.py \
  config/ads-plan-monitor/config.json \
  --mode all

python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --status
```

旧版本升级：

```bash
python3 skills/ads-plan-monitor/scripts/migrate_channels.py \
  --config config/ads-plan-monitor/config.json
```

只检查某个用途时，将 `--mode all` 改成 `query`、`create-preview` 或 `create-submit`。

配置官方开发文档 MCP：

```bash
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py --status
```

强制刷新 Access Token：

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --refresh
```

正常查询和创建不需要手动执行该命令；各 API 脚本会在 Token 临近过期时自动刷新。

只重新同步官方授权账户详情：

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --sync-accounts
```

旧版本迁移后若状态显示 `pending_account_sync: true`，使用状态中的本地授权 ID 完成首次同步：

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --authorization-id <AUTHORIZATION_ID> \
  --sync-accounts
```

一般无需指定授权账户，脚本会根据目标广告主自动选择 Token。只有多个授权同时覆盖该广告主时，增加：

```bash
--auth-account-id REPLACE_WITH_OFFICIAL_ACCOUNT_ID
```

## 查询视频素材

查询今天上传的视频素材：

```bash
python3 skills/ads-plan-monitor/scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode library-get \
  --date today \
  --fetch-all
```

按日期查询：

```bash
python3 skills/ads-plan-monitor/scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode library-get \
  --date YYYY-MM-DD \
  --fetch-all
```

校验视频能否用于广告投放：

```bash
python3 skills/ads-plan-monitor/scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode ad-get \
  --video-id REPLACE_WITH_VIDEO_ID
```

获取视频封面建议：

```bash
python3 skills/ads-plan-monitor/scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode cover-suggest \
  --video-id REPLACE_WITH_VIDEO_ID
```

## 查询素材表现

查询当前账户今天的单元素材表现：

```bash
python3 skills/ads-plan-monitor/scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json
```

查询指定日期：

```bash
python3 skills/ads-plan-monitor/scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

只看某个项目或单元：

```bash
python3 skills/ads-plan-monitor/scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json \
  --project-id REPLACE_WITH_PROJECT_ID

python3 skills/ads-plan-monitor/scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json \
  --promotion-id REPLACE_WITH_PROMOTION_ID
```

默认不会写文件。只有需要保存时再传：

```bash
--out runs/report.json
--csv-out runs/report.csv
```

## 发现报表字段

```bash
python3 skills/ads-plan-monitor/scripts/query_report_config.py \
  --config config/ads-plan-monitor/config.json \
  --data-topic MATERIAL_DATA
```

## 自定义报表

```bash
python3 skills/ads-plan-monitor/scripts/query_custom_report.py \
  --config config/ads-plan-monitor/config.json \
  --data-topic MATERIAL_DATA \
  --dimension material_id \
  --dimension cdp_promotion_id \
  --metric stat_cost \
  --metric click_cnt \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

## 创建计划

先查看业务模板及其归属广告主：

```bash
python3 skills/ads-plan-monitor/scripts/manage_plan_templates.py \
  --config config/ads-plan-monitor/config.json \
  list
```

输出中的 `advertiser_id` 是模板唯一归属的广告主 ID。目标广告主与该值不一致时，单条和批量创建都会在调用官方 API 前停止。

新建业务模板必须先运行向导：

```bash
python3 skills/ads-plan-monitor/scripts/manage_plan_templates.py \
  --config config/ads-plan-monitor/config.json \
  create-wizard
```

向导会要求选择默认骨架或已有业务模板，并在最终确认前展示复制策略、广告主绑定、逐字段差异和完整性校验。默认模板不能用于真实计划创建；不完整模板只能保存为草稿，不能激活。文案标题必须为 5–30 个字符。

配置已有模板的文案素材：

```bash
python3 skills/ads-plan-monitor/scripts/manage_plan_templates.py \
  --config config/ads-plan-monitor/config.json \
  set-copy \
  --template "平台-CID-商品名-商品ID" \
  --title "第一条文案" \
  --title "第二条文案"
```

相同商品从另一个创建计划模板复制文案：

```bash
python3 skills/ads-plan-monitor/scripts/manage_plan_templates.py \
  --config config/ads-plan-monitor/config.json \
  set-copy \
  --template "目标创建计划模板" \
  --from-template "来源创建计划模板"
```

先预览 payload：

```bash
python3 skills/ads-plan-monitor/scripts/create_plan.py \
  --config config/ads-plan-monitor/config.json \
  --plan-template "平台-CID-商品名-商品ID" \
  --video-id REPLACE_WITH_VIDEO_ID
```

确认后提交：

```bash
python3 skills/ads-plan-monitor/scripts/create_plan.py \
  --config config/ads-plan-monitor/config.json \
  --plan-template "平台-CID-商品名-商品ID" \
  --video-id REPLACE_WITH_VIDEO_ID \
  --submit
```

常用覆盖参数：

```bash
--budget 5000
--bid 1.5
--roi-goal 1.5
--product-name "REPLACE_WITH_PRODUCT_NAME"
--product-id REPLACE_WITH_PRODUCT_ID
```

## 批量创建

按当天视频素材分组，每 5 条素材一个单元，先 dry-run：

```bash
python3 skills/ads-plan-monitor/scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5
```

确认后提交：

```bash
python3 skills/ads-plan-monitor/scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5 \
  --submit
```

多账户并发：

```bash
python3 skills/ads-plan-monitor/scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --accounts 1111111111111111,2222222222222222 \
  --account-template 1111111111111111="模板A" \
  --account-template 2222222222222222="模板B" \
  --date today \
  --videos-per-unit 5 \
  --account-concurrency 2 \
  --group-concurrency 2 \
  --submit
```

多账户任务必须为每个广告主解析一个归属模板。某广告主只有一个绑定模板时可自动选择；存在零个或多个候选时，必须显式传入 `--account-template ADVERTISER_ID=TEMPLATE_NAME`。`--plan-template` 仅适用于单账户任务。

## 辅助反查

查询项目：

```bash
python3 skills/ads-plan-monitor/scripts/query_projects.py \
  --config config/ads-plan-monitor/config.json \
  --name REPLACE_WITH_NAME
```

查询单元：

```bash
python3 skills/ads-plan-monitor/scripts/query_promotions.py \
  --config config/ads-plan-monitor/config.json \
  --project-id REPLACE_WITH_PROJECT_ID
```

查询商品：

```bash
python3 skills/ads-plan-monitor/scripts/query_products.py \
  --config config/ads-plan-monitor/config.json \
  --name REPLACE_WITH_PRODUCT_NAME
```

查询图片素材：

```bash
python3 skills/ads-plan-monitor/scripts/query_images.py \
  --config config/ads-plan-monitor/config.json
```

解析城市 ID：

```bash
python3 skills/ads-plan-monitor/scripts/resolve_city_ids.py \
  --config config/ads-plan-monitor/config.json \
  --city-csv path/to/cities.csv
```
