# 文档中心

这里存放 `ocean-watch` 的长期维护文档。README 只提供项目概览；安装后的实际操作以本目录为准。

## 按阅读顺序

1. [快速开始](getting-started.md)：从环境检查、初始化、OAuth 到第一次查询或预览。
2. [配置与授权](configuration.md)：配置位置、Schema、模板、凭据和 Token 生命周期。
3. [CLI 参考](cli.md)：完整命令树、常用参数、示例和行为说明。
4. [架构说明](architecture.md)：模块边界、依赖方向、事务和测试约束。
5. [发布指南](releasing.md)：正式产物、离线安装、校验和维护者发版流程。

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
| 发布产物与校验 | `docs/releasing.md` |

已完成的阶段性设计稿不保留在 `docs/` 中；需要追溯设计过程时使用 Git 历史。修改 CLI、配置 Schema 或安全边界时，应同步更新对应正式文档。
