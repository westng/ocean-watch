# AI 过程文档

本目录保存 Ocean Watch 的 AI 辅助设计、计划、调研、评审和交接材料。这些文件用于讨论与实施准备，不是当前产品行为的权威来源。

## 权威边界

- 当前运行行为以代码、配置、测试、`.codex-plugin/plugin.json`、两个 Skill 及正式文档为准；
- 过程文档中的状态、版本、行号、测试结果、环境事实和完成度在引用前必须重新核对；
- 已确认并长期有效的结论应落实到代码、测试、契约或正式文档，不能只保留在本目录；
- 未实施的设计不得写入 `README.md` 或 `docs/README.md` 作为现有能力。

## 分类

- [`designs/`](designs/)：目标架构、方案比较和设计决策；
- [`plans/`](plans/)：分步实施与整改计划；是否进入实施以各文档状态和当前任务授权为准；
- `research/`：调研、诊断和证据记录；
- `reviews/`：评审与审计快照；
- `handoffs/`：跨会话或跨人员交接。

## 当前设计

- [Ocean Watch Plugin 目标架构 V3](designs/ocean-watch-plugin-target-architecture-v3.md)：W0–W7 已完成；W8 候选已构建安装，等待新任务 Host 加载检查；W9–W10 尚未执行

## 当前计划

- [Ocean Watch Plugin V3 架构整改实施计划](plans/ocean-watch-plugin-architecture-remediation-plan.md)：实施中；下一步是在新任务完成 W8 Host 加载检查

## 当前审计

- [Ocean Watch Plugin 当前架构审计](research/ocean-watch-plugin-current-architecture-audit.md)：2026-08-18 现状基线

## 历史设计

- [千川计划创建快速工作流设计（历史 V2）](designs/qianchuan-plan-creation-workflow-v2.md)：已被 V3 取代，不再作为实施依据
