# 千川作品链接删除计划素材设计

## 目标

为 `qc-plan-monitor` 增加可重用的计划素材删除流程。用户提供千川广告主 ID、全域计划 ID 和一个或多个抖音作品链接，插件通过官方 API 精确删除对应的自提素材，不修改计划的其他设置。

## 接口与边界

- 查询计划详情：`GET /v1.0/qianchuan/uni_promotion/ad/detail/`。
- 查询全部视频素材：`GET /v1.0/qianchuan/uni_promotion/ad/material/get/`，`material_status=ALL`。
- 删除素材：`POST /v1.0/qianchuan/uni_promotion/ad/material/delete/`。
- 删除接口使用 `material_ids`，不能直接使用 `aweme_item_id`；单次最多 100 个。
- 仅删除 `material_select_type=CUSTOM` 的自提素材；智选素材必须跳过并返回原因。
- 官方说明多号和多商品场景下可能同时删除同一素材的关联投放；预检必须显示该风险。

## 命令流程

```text
ocean-watch plans remove-qianchuan-work \
  --advertiser-id ADVERTISER_ID \
  --ad-id AD_ID \
  --work-url DOUYIN_WORK_URL
```

1. 仅跟随受信任的抖音域名跳转，提取并去重 `aweme_item_id`。
2. 使用目标广告主绑定的千川授权，查询计划详情与全部视频素材。
3. 按 `material_info.video_material.aweme_item_id` 匹配作品，再取内层 `material_id`。
4. 未找到、已删除和智选素材作为可识别结果返回；同一作品匹配多个不同素材 ID 时阻止删除。
5. 默认 dry-run；只有显式 `--submit` 才按 100 个一批调用删除接口。
6. 提交后重新查询计划素材，每个目标都必须变为 `DELETED`；否则标记验证失败。

## 实现结构

- `QianchuanPlanGateway` 增加删除端点和 `delete_materials` 方法。
- `remove_qianchuan_work_materials` 负责链接解析、素材匹配、预检、分批删除与删后验证。
- CLI 增加 `plans remove-qianchuan-work`，仍使用千川独立授权和广告主级进程锁。
- Skill 和用户文档只描述可见命令、官方限制与安全边界，不写入真实业务 ID。

## 测试

- 作品链接精确匹配内层 `material_id`。
- dry-run 不调用删除端点。
- 已删除重试幂等，智选素材被拒绝。
- 重复行同一素材 ID 可合并；多个不同素材 ID 阻止。
- 删除端点参数和 100 个分批上限正确。
- 官方返回非零、分页截断和删后状态未变更时返回失败。
