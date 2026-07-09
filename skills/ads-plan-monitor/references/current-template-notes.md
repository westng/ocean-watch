# Current Template Notes

Use this reference only for the current active plan template and reusable template rules.
Do not infer behavior from experiments or historical run files.

## Active Template

- Template name: `天猫-CID-蛋白粉-7563545512968814601`
- Naming rule: `平台-CID-商品名-商品ID`
- Platform: `天猫`
- Traffic source: `CID`
- Product label: `蛋白粉`
- Product ID: `7563545512968814601`
- Product name: `多莓蛋白粉`
- Category label: `食品饮料/普通膳食营养食品/蛋白粉/氨基酸/胶原蛋白`
- Brand label: `MEIDABOSHILY/每日博士`

## Confirmed Defaults

- Daily budget: `300`
- CPA bid: `100`
- Deep bid type: `NET_ORDER_ROI`
- Deep optimization label: `净成交ROI`
- ROI goal: `1.5`
- Hide converted users: `NO_EXCLUDE`
- Age targeting: omit `audience.age` for unlimited age.
- Video image mode: `CREATIVE_IMAGE_MODE_VIDEO_VERTICAL`
- Max videos per project: `5` unless the user explicitly changes it.

## Current Links

Use the links from `plan_templates.<template>.tracking_urls` and `plan_templates.<template>.links`
in config. Preserve every query parameter exactly when editing links.

Current template links are the source of truth.

## Query Scope

For current material monitoring:

1. Query `/v3.0/promotion/list/`.
2. Extract `promotion_materials.video_material_list[].material_id`.
3. Query `/v3.0/report/custom/get/` with both extracted `material_id` values and promotion IDs.
4. Use matched material rows for chat summaries and top-spend tables.

Do not use broad `total_metrics` from a promotion-only report as the current material-list total.

## Cleanup Rule

Do not keep historical API result JSON, CSV output, dry-run files, or cached Python bytecode inside
the skill directory. They can mislead future runs and should live outside the skill package only when
the user explicitly requests saved artifacts.
