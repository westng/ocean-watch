# 千川账户报表修复与增强

## 问题背景

当用户在非乘方期间查询千川账户报表时，如果指标集包含乘方专属指标（`stat_cost_for_overall_roi2` 和 `total_prepay_and_pay_settle_overall_roi2_1h`），会触发 API 错误 40000："乘方计划仅支持在乘方期间查询 stat_cost_for_overall_roi2 指标"。

## 解决方案

实施了两个互补的修复：

### 1. 修复 `report_qianchuan_account` 的 overall 指标集

**文件**: `runtime/ocean-watch-go/internal/application/reports/qianchuan_unified.go`

**修改**: 从 `DefaultQianchuanAllPromotionFields` 中移除乘方专属指标：
- 移除: `stat_cost_for_overall_roi2`
- 移除: `total_prepay_and_pay_settle_overall_roi2_1h`

**效果**: 现在 `report_qianchuan_account` 工具在 `scope=overall` 模式下不会再触发 API 错误 40000，适用于任意日期范围。

### 2. 新增 `report_qianchuan_uni_account` MCP 工具

**新增文件修改**:
- `internal/mcpserver/server.go`: 注册新工具
- `internal/mcpserver/query_schema.go`: 添加输入输出 JSON Schema
- `internal/mcpserver/qianchuan_queries.go`: 实现处理函数和类型定义
- `internal/contracts/capabilities.go`: 注册能力路由
- `skills/qc-plan-monitor/SKILL.md`: 添加快速路由条目

**工具特性**:
- 工具名: `report_qianchuan_uni_account`
- 用途: 查询纯全域账户汇总数据（消耗、订单、GMV、ROI）
- 优势: 不包含任何乘方指标，适用于任意日期范围，语义更清晰
- 底层调用: 直接使用 `/v1.0/qianchuan/report/uni_promotion/get/` 端点

## 测试验证

所有测试通过：
- ✓ MCP 服务器契约测试（工具数量从 17 增加到 18）
- ✓ 千川查询工具测试
- ✓ 应用层报表服务测试

## 语义区分

- `report_qianchuan_account` (scope=overall): 混合模式，支持全域+乘方，但不包含乘方专属指标
- `report_qianchuan_account` (scope=uni): 仅全域，使用精简指标集
- `report_qianchuan_uni_account`: 专用全域工具，语义明确，推荐用于纯全域场景
