# Template Notes

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

Use this reference for the active plan template and reusable template rules.
Do not infer behavior from experiments, historical run files, or user-specific local config.

## Template Naming

- Naming rule: `平台-CID-商品名-商品ID-素材来源`
- Example placeholder: `示例平台-CID-示例商品-REPLACE_WITH_PRODUCT_ID-上传素材`
- The real active template name should live in the user's local `config/ads-plan-monitor/config.json`.

## Required Template Sections

A practical create-plan template normally needs:

- `defaults`: budget, bid/ROI, naming templates, delivery settings, audience settings, product defaults.
- `material_strategy`: account-upload or creator-authorized source and runtime selection rules.
- `resolved_ids`: city IDs, product IDs, image IDs, landing page asset IDs, event asset IDs, and brand/category IDs when available.
- `links`: landing page and app/open URL.
- `tracking_urls`: display and click/action tracking URLs.
- `copy_materials.titles`: reusable promotion title material.

Specific video, cover, item, and material IDs belong to the current run. Never persist them in a
schema v3 business template.

## Confirmed Defaults For This Skill

- Deep bid type for ROI flow: `NET_ORDER_ROI`
- Deep optimization label: `净成交ROI`
- Hide converted users unlimited: `NO_EXCLUDE`
- Age targeting unlimited: omit `audience.age`.
- Video image mode: `CREATIVE_IMAGE_MODE_VIDEO_VERTICAL`
- Max videos per project: `5` unless the user explicitly changes it.

## Links

Use the links from `plan_templates.<template>.tracking_urls` and
`plan_templates.<template>.links` in local config. Preserve every query parameter exactly when
editing links.

The public `assets/config.example.json` contains placeholder links only. Do not treat them as a
real campaign template.

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
