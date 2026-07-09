# 常用命令

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

下面命令默认从 `config/ads-plan-monitor/config.json` 读取业务配置，并从本机凭据仓库读取 OAuth token。

## 初始化和检查

```bash
python3 scripts/first_run.py \
  --config config/ads-plan-monitor/config.json

python3 scripts/validate_config.py \
  config/ads-plan-monitor/config.json

python3 scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --status
```

## 查询视频素材

查询今天上传的视频素材：

```bash
python3 scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode library-get \
  --date today \
  --fetch-all
```

按日期查询：

```bash
python3 scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode library-get \
  --date YYYY-MM-DD \
  --fetch-all
```

校验视频能否用于广告投放：

```bash
python3 scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode ad-get \
  --video-id REPLACE_WITH_VIDEO_ID
```

获取视频封面建议：

```bash
python3 scripts/query_videos.py \
  --config config/ads-plan-monitor/config.json \
  --mode cover-suggest \
  --video-id REPLACE_WITH_VIDEO_ID
```

## 查询素材表现

查询当前账户今天的单元素材表现：

```bash
python3 scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json
```

查询指定日期：

```bash
python3 scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json \
  --start-date YYYY-MM-DD \
  --end-date YYYY-MM-DD
```

只看某个项目或单元：

```bash
python3 scripts/query_active_materials_report.py \
  --config config/ads-plan-monitor/config.json \
  --project-id REPLACE_WITH_PROJECT_ID

python3 scripts/query_active_materials_report.py \
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
python3 scripts/query_report_config.py \
  --config config/ads-plan-monitor/config.json \
  --data-topic MATERIAL_DATA
```

## 自定义报表

```bash
python3 scripts/query_custom_report.py \
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

先预览 payload：

```bash
python3 scripts/create_plan.py \
  --config config/ads-plan-monitor/config.json \
  --plan-template "平台-CID-商品名-商品ID" \
  --video-id REPLACE_WITH_VIDEO_ID
```

确认后提交：

```bash
python3 scripts/create_plan.py \
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
python3 scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5
```

确认后提交：

```bash
python3 scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --date today \
  --videos-per-unit 5 \
  --submit
```

多账户并发：

```bash
python3 scripts/batch_create_from_today_videos.py \
  --config config/ads-plan-monitor/config.json \
  --accounts 1111111111111111,2222222222222222 \
  --date today \
  --videos-per-unit 5 \
  --account-concurrency 2 \
  --group-concurrency 2 \
  --submit
```

## 辅助反查

查询项目：

```bash
python3 scripts/query_projects.py \
  --config config/ads-plan-monitor/config.json \
  --name REPLACE_WITH_NAME
```

查询单元：

```bash
python3 scripts/query_promotions.py \
  --config config/ads-plan-monitor/config.json \
  --project-id REPLACE_WITH_PROJECT_ID
```

查询商品：

```bash
python3 scripts/query_products.py \
  --config config/ads-plan-monitor/config.json \
  --name REPLACE_WITH_PRODUCT_NAME
```

查询图片素材：

```bash
python3 scripts/query_images.py \
  --config config/ads-plan-monitor/config.json
```

解析城市 ID：

```bash
python3 scripts/resolve_city_ids.py \
  --config config/ads-plan-monitor/config.json \
  --city-csv path/to/cities.csv
```
