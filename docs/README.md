# 文档中心

当前仓库包含 Go Runtime、营销 / 千川 / 星图三个 Skill、MCP/CLI 入口和多个宿主适配资源；V3 已将主要广告业务统一到 Go 核心，但 PRD 的完整 M1–M5 和逐宿主外部验收仍未完成。Python 目前只用于固定版本 F2 的抖音公开作品元数据解析。

1. [快速开始](guides/getting-started.md)：安装、环境检查、初始化与 OAuth。
2. [配置与授权](guides/configuration.md)：配置优先级、凭据、账户与 F2 边界。
3. [CLI 参考](reference/cli.md)：稳定命令分组、输出和写入安全语义。
4. [V3 项目运行架构](architecture/overview.md)：完整分层、宿主与核心边界、调用链路、进程归属、数据持久化、升级迁移及当前落地差异。
5. [发布指南](release/releasing.md)：版本、五平台二进制、Tag 与 Release。
6. [DeepSeek Harness 接入](guides/deepseek-harness.md)：原生组合包、安装、兼容版本与验证。
7. [AI 过程文档](ai/README.md)：设计、实施计划、调研、评审和交接材料的分类与权威边界。
8. [宿主内授权恢复](reference/authorization-recovery.md)：官方 OAuth、本机账户同步确认、会话状态与原请求恢复合同。
9. [WorkBuddy 本机接入](../hosts/workbuddy/README.md)：安装、更新、共享状态和 MCP 能力范围；已验证 macOS arm64 安装与工具 / Skill 发现，完整业务验收待完成。
10. [千川混合商品作品建计划](reference/qianchuan-work-routing.md)：逐行商品与账户路由、待确认行、独立预检快照和按账户提交。
11. [当前项目详细流程图](architecture/workflows.md)：19 张图、247 个步骤的用途、方法、条件分支与源码；包括千川账户共享读取、达人独立预检、真实串并行关系及全部 CLI/MCP 能力。
12. [千川官方 MCP 查询合同](reference/qianchuan-official-mcp.md)：156 项受控 GET 映射、任务入口、实例策略、精度/分页及可选开发资料；真实宿主和授权支持单独验收。
13. [Operation 确认基础合同](reference/operation-approval.md)：M1 本地 Broker、不可变确认摘要、受保护证据和一次性审批；平台限制、未切换业务入口及实际界面验收边界。

仓库级文档包括[贡献指南](../CONTRIBUTING.md)、[安全说明](../SECURITY.md)和[更新日志](../CHANGELOG.md)。阶段性迁移稿不再作为当前事实源；历史设计可从 Git 历史追溯。AI 过程文档只记录计划和验收边界，不替代代码、测试或正式文档。

## 存放与命名

- 持续维护的产品需求、架构、配置、接口、用户手册和发布流程按职责放入 `docs/product/`、`docs/architecture/`、`docs/guides/`、`docs/reference/` 和 `docs/release/`；`docs/` 根目录只保留入口 `README.md`。模块专属说明放对应模块的 README，可审核 SQL 放 `docs/sql/`，文档站内容放 `docs-site/`。
- AI 辅助设计、实施计划、调研、评审与交接按职责放入 `docs/ai/` 对应分类，不因 AI 参与编写而迁移正式文档。
- 除目录入口 `README.md` 外，文件沿用小写英文、短横线分词的 `.md` 命名；阶段计划可用 `v3-` 等明确范围前缀，不重复堆叠项目名称。分类目录按需创建，不保留空目录或占位文件。
- 重命名、移动或删除文档时同步更新索引、相对链接和脚本依赖；确认废弃或已被替代的文档直接删除，不另建副本归档。

## 产品规划

- [开源产品重构 PRD](product/prd.md)：共享 Go 核心与 Skill、配置优先的薄宿主包、业务工具集与执行模式、SQLite 状态、可信审批/恢复、统一诊断及逐宿主验收。V3 已落地部分实现，WorkBuddy 已有本机安装入口；完整目标方案、全量状态迁移和逐宿主业务验收仍未完成。
