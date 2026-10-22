# 文档中心

当前仓库包含 Go Runtime、两个 Skill、MCP/CLI 入口和多个宿主适配资源；目标 PRD 规划为共享 Go 核心、共享 Skill 与配置优先的薄宿主包，尚未全部实施。Python 目前只用于固定版本 F2 的抖音公开作品元数据解析。

1. [快速开始](getting-started.md)：安装、环境检查、初始化与 OAuth。
2. [配置与授权](configuration.md)：配置优先级、凭据、账户与 F2 边界。
3. [CLI 参考](cli.md)：稳定命令分组、输出和写入安全语义。
4. [架构说明](architecture.md)：Go 模块、官方 API、状态与请求治理。
5. [发布指南](releasing.md)：版本、五平台二进制、Tag 与 Release。
6. [DeepSeek Harness 接入](deepseek-harness.md)：原生组合包、安装、兼容版本与验证。

仓库级文档包括[贡献指南](../CONTRIBUTING.md)、[安全说明](../SECURITY.md)和[更新日志](../CHANGELOG.md)。阶段性迁移稿不再作为当前事实源；历史设计可从 Git 历史追溯。

## 产品规划

- [开源产品重构 PRD](prd.md)：共享 Go 核心与 Skill、配置优先的薄宿主包、业务工具集与执行模式、SQLite 状态、可信审批/恢复、统一诊断及逐宿主验收；WorkBuddy 纳入后续接入。目标方案尚未实现，也不代表代码重构、真实数据迁移或发布已获授权。
