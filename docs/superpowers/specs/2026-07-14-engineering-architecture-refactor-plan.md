# Ocean Watch 工程化重构实施计划

1. 建立 `pyproject.toml`、`src/ocean_watch` 包、统一 CLI 和 Plugin 启动器。
2. 抽取结构化错误、配置存储、输出、路径、锁和官方 API Client。
3. 将授权与 Token 模块迁入 `auth`，统一网络与配置依赖。
4. 将模板领域迁入 `templates`，保留 Schema、向导和校验行为。
5. 将上传与达人素材迁入 `materials`，统一查询和选择契约。
6. 将 payload 与创建流程迁入 `plans`，建立共享 `PlanExecutor`。
7. 将两类批量流程改为共享调度、journal 和结果模型。
8. 将查询与报表迁入 `reports`，移除重复分页和 HTTP 实现。
9. 重组测试为 unit、integration 和 CLI 三层。
10. 重写中英文 README、CLI、配置、架构和贡献文档。
11. 删除旧脚本，运行跨版本测试、静态检查、Plugin/Skill 校验和敏感信息扫描。
