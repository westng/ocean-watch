# 文档中心

当前仓库包含 Go Runtime、两个 Skill、MCP/CLI 入口和多个宿主适配资源；V3 已将主要广告业务统一到 Go 核心，但 PRD 的完整 M1–M5 和逐宿主外部验收仍未完成。Python 目前只用于固定版本 F2 的抖音公开作品元数据解析。

1. [快速开始](guides/getting-started.md)：安装、环境检查、初始化与 OAuth。
2. [配置与授权](guides/configuration.md)：配置优先级、凭据、账户与 F2 边界。
3. [CLI 参考](reference/cli.md)：稳定命令分组、输出和写入安全语义。
4. [架构说明](architecture/overview.md)：Go 模块、官方 API、状态与请求治理。
5. [发布指南](release/releasing.md)：版本、五平台二进制、Tag 与 Release。
6. [DeepSeek Harness 接入](guides/deepseek-harness.md)：原生组合包、安装、兼容版本与验证。
7. [AI 过程文档](ai/README.md)：设计、实施计划、调研、评审和交接材料的分类与权威边界。

仓库级文档包括[贡献指南](../CONTRIBUTING.md)、[安全说明](../SECURITY.md)和[更新日志](../CHANGELOG.md)。阶段性迁移稿不再作为当前事实源；历史设计可从 Git 历史追溯。AI 过程文档只记录计划和验收边界，不替代代码、测试或正式文档。

## 存放与命名

- 持续维护的产品需求、架构、配置、接口、用户手册和发布流程按职责放入 `docs/product/`、`docs/architecture/`、`docs/guides/`、`docs/reference/` 和 `docs/release/`；`docs/` 根目录只保留入口 `README.md`。模块专属说明放对应模块的 README，可审核 SQL 放 `docs/sql/`，文档站内容放 `docs-site/`。
- AI 辅助设计、实施计划、调研、评审与交接按职责放入 `docs/ai/` 对应分类，不因 AI 参与编写而迁移正式文档。
- 除目录入口 `README.md` 外，文件沿用小写英文、短横线分词的 `.md` 命名；阶段计划可用 `v3-` 等明确范围前缀，不重复堆叠项目名称。分类目录按需创建，不保留空目录或占位文件。
- 重命名、移动或删除文档时同步更新索引、相对链接和脚本依赖；确认废弃或已被替代的文档直接删除，不另建副本归档。

## 产品规划

- [开源产品重构 PRD](product/prd.md)：共享 Go 核心与 Skill、配置优先的薄宿主包、业务工具集与执行模式、SQLite 状态、可信审批/恢复、统一诊断及逐宿主验收；WorkBuddy 纳入后续接入。V3 已落地部分实现，但完整目标方案、真实数据迁移和逐宿主验收仍未完成，也不因本文获得发布或其他操作授权。
