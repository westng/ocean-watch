#!/bin/bash
# 测试新增的 report_qianchuan_uni_account MCP 工具

echo "=== 测试 1: 检查工具总数 ==="
./runtime/ocean-watch-go/ocean-watch mcp capabilities 2>&1 | grep -c '"name"'

echo ""
echo "=== 测试 2: 搜索新工具 ==="
./runtime/ocean-watch-go/ocean-watch mcp capabilities 2>&1 | grep "report_qianchuan_uni_account" || echo "新工具未找到"

echo ""
echo "=== 测试 3: 验证 overall 指标集已修复 ==="
grep -A 10 "DefaultQianchuanAllPromotionFields" runtime/ocean-watch-go/internal/application/reports/qianchuan_unified.go | grep "stat_cost_for_overall_roi2" && echo "错误：仍包含乘方专属指标" || echo "正确：已移除乘方专属指标"

echo ""
echo "=== 测试 4: 验证能力注册 ==="
grep "qianchuan.uni_account_report" runtime/ocean-watch-go/internal/contracts/capabilities.go && echo "找到能力注册" || echo "能力注册未找到"
