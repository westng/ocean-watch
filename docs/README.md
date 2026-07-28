# 文档中心

这里存放 `ocean-watch` 的长期维护文档。README 只提供项目概览；安装后的实际操作以本目录为准。

## 按阅读顺序

1. [快速开始](getting-started.md)：从环境检查、初始化、OAuth 到第一次查询或预览。
2. [配置与授权](configuration.md)：配置位置、Schema、模板、凭据和 Token 生命周期。
3. [CLI 参考](cli.md)：当前兼容命令合同、常用参数、示例和行为说明；命令分组不是架构模块。
4. [架构说明](architecture.md)：当前 Python 生产路径、Go Shadow 候选、模块边界、路由和发布 Gate。
5. [发布指南](releasing.md)：Git Marketplace 分发、版本契约和维护者发版流程。

## 架构与迁移状态

迁移信息只维护在以下长期事实源中，避免架构、任务、验收和发布状态在多份阶段文档间漂移：

- [架构说明](architecture.md)：当前 Python 生产路径、Go Shadow 候选、模块边界、安全与发布 Gate。
- [Go SDK 迁移矩阵](go-sdk-migration-matrix.md)：逐命令、逐 endpoint 的实现和路由状态。
- [阶段状态与验收契约](../contracts/README.md)：P0–P5 完成度、阻断项、AC 定义和证据格式。
- [发布指南](releasing.md)：Marketplace、签名候选、版本与回滚流程。
- [ADR-0001 platform bootstrap](adr/0001-platform-bootstrap.md)：兼容期使用原生启动器校验签名的架构决策。

仓库级说明：

- [贡献指南](../CONTRIBUTING.md)
- [安全说明](../SECURITY.md)
- [更新日志](../CHANGELOG.md)

## 文档边界

| 内容 | 位置 |
| --- | --- |
| 项目定位、安装和最短示例 | 根目录 README |
| 用户操作、当前运行时与命令行为 | `docs/` |
| Codex 路由和执行约束 | `skills/*/SKILL.md` |
| 经确认的官方接口细节 | `skills/*/references/` |
| 阶段自动化事实与阻断项 | `contracts/p0-status.yaml`–`contracts/p5-status.yaml` |
| 版本变化 | `CHANGELOG.md` |
| 版本、Tag 与发布流程 | `docs/releasing.md` |

阶段性设计稿和任务书不保留在 `docs/` 中；需要追溯设计过程时使用 Git 历史。阶段进度与验收结果只更新机器契约，架构、安全、发布或 CLI 行为变化则同步更新对应长期文档。
