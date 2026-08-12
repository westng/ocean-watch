# 文档中心

Ocean Watch 只有一条业务执行路径：两个 Skill 通过各自启动器进入同一套内置 Go CLI，再调用巨量引擎官方 API。Python 只用于固定版本 F2 的抖音公开作品元数据解析。

1. [快速开始](getting-started.md)：安装、环境检查、初始化与 OAuth。
2. [配置与授权](configuration.md)：配置优先级、凭据、账户与 F2 边界。
3. [CLI 参考](cli.md)：稳定命令分组、输出和写入安全语义。
4. [架构说明](architecture.md)：Go 模块、官方 API、状态与请求治理。
5. [发布指南](releasing.md)：版本、五平台二进制、Tag 与 Release。

仓库级文档包括[贡献指南](../CONTRIBUTING.md)、[安全说明](../SECURITY.md)和[更新日志](../CHANGELOG.md)。阶段性迁移稿不再作为当前事实源；历史设计可从 Git 历史追溯。
