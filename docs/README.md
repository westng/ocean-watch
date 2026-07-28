# 文档中心

这里存放 `ocean-watch` 的长期维护文档。README 只提供项目概览；安装后的实际操作以本目录为准。

## 按阅读顺序

1. [快速开始](getting-started.md)：从环境检查、初始化、OAuth 到第一次查询或预览。
2. [配置与授权](configuration.md)：配置位置、Schema、模板、凭据和 Token 生命周期。
3. [CLI 参考](cli.md)：完整命令树、常用参数、示例和行为说明。
4. [架构说明](architecture.md)：模块边界、依赖方向、事务和测试约束。
5. [发布指南](releasing.md)：Git Marketplace 分发、版本契约和维护者发版流程。

## Go SDK 迁移实施包

以下文档描述仍在 Gate 中的目标态，用于直接执行和验收；在 G5 完成前，当前运行行为仍以上述正式文档为准。[发布指南](releasing.md)已经纳入签名 Release 候选资产流程，但 Marketplace 仍从仓库 Tag 安装、生产运行时策略仍关闭，候选 ZIP 不能当作最终 Marketplace 安装路径：

1. [Go SDK 目标架构](go-sdk-target-architecture.md)：依赖边界、SDK 防腐层、韧性、安全、状态和发布决策。
2. [Go SDK 迁移矩阵](go-sdk-migration-matrix.md)：逐命令、逐 endpoint 的 SDK Service、阶段和合同映射。
3. [Go SDK 实施任务书](go-sdk-execution-plan.md)：P0–P5 工作包、角色、工作量、Gate 与回滚条件。
4. [Go SDK 验收计划](go-sdk-acceptance-plan.md)：AC-101–AC-128 的 Given/When/Then、命令、fixture、证据和签字标准。

P0 supporting decisions:

- [Go SDK threat model](go-sdk-threat-model.md): assets, trust boundaries, high-risk threats, controls, and Gate ownership.
- [Go SDK runtime release RFC](go-sdk-release-rfc.md): version identity, signed runtime manifest, launcher state machine, and rollback protocol.
- [ADR-0001 platform bootstrap](adr/0001-platform-bootstrap.md): why signature verification belongs in a native bootstrap during compatibility.

仓库级说明：

- [贡献指南](../CONTRIBUTING.md)
- [安全说明](../SECURITY.md)
- [更新日志](../CHANGELOG.md)

## 文档边界

| 内容 | 位置 |
| --- | --- |
| 项目定位、安装和最短示例 | 根目录 README |
| 用户操作与命令行为 | `docs/` |
| Codex 路由和执行约束 | `skills/*/SKILL.md` |
| 经确认的官方接口细节 | `skills/*/references/` |
| 版本变化 | `CHANGELOG.md` |
| 版本、Tag 与发布流程 | `docs/releasing.md` |

已完成的阶段性设计稿不保留在 `docs/` 中；需要追溯设计过程时使用 Git 历史。尚在执行的企业级迁移方案可以作为有状态实施包保留，但必须明确当前态、目标态、Gate 和完成后的归档方式。修改 CLI、配置 Schema 或安全边界时，应同步更新对应正式文档。
