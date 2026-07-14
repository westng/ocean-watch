# Ocean Watch 工程化架构重构设计

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## 背景

当前实现已经覆盖 OAuth、授权账户同步、模板管理、上传素材、达人素材、计划创建、批量执行和报表查询，但主要以独立 Python 脚本增长。多个入口重复实现 HTTP 请求、配置读取、JSON 输出、分页、错误处理和计划创建事务，测试也按历史回归集中堆叠。功能能够工作，但模块边界和公共契约不足以支撑长期开放协作。

本次重构不保留旧脚本路径和命令行参数兼容性，直接建立面向开源维护的目标架构。

## 目标

- 使用标准 `src/` Python 包和 `pyproject.toml` 管理项目。
- 对人和 Codex 只提供一个 `ocean-watch` CLI。
- 按授权、模板、素材、计划、报表和策略划分领域。
- 统一配置、凭据、HTTP、错误、分页、并发、输出和运行记录。
- 单条与批量计划共享同一计划事务执行器。
- 保持真实配置、凭据和运行数据在仓库之外。
- 为用户、贡献者和 Skill 分别提供适量文档。

## 非目标

- 本次不实现巨量千川业务接口。
- 不改变已确认的模板 Schema 和业务校验规则。
- 不调用真实广告账户接口进行重构验收。
- 不把业务配置或示例业务数据迁入源码。

## 目录设计

```text
skills/ads-plan-monitor/
├── SKILL.md
├── agents/
├── assets/
├── references/
├── run.py
└── src/ocean_watch/
    ├── cli/
    ├── core/
    ├── api/
    ├── auth/
    ├── templates/
    ├── materials/
    ├── plans/
    ├── reports/
    ├── strategy/
    └── integrations/
```

仓库根目录的 `pyproject.toml` 提供可编辑安装和 `ocean-watch` 命令。Plugin 环境无需安装项目，可以通过 Skill 根目录的 `run.py` 启动同一 CLI。

## 分层职责

### CLI

CLI 只负责参数解析、调用应用服务、把结构化结果输出为 JSON，以及将领域异常映射为稳定退出码。CLI 不直接读写凭据、不拼接 API URL、不构造投放 payload。

### Core

Core 提供跨领域基础契约：配置路径与原子存储、结构化错误、JSON 输出、时间和 ID 规范化、进程锁及运行状态目录。领域模块不得复制这些实现。

### API

`OceanEngineClient` 是业务 API 的唯一网络出口，统一处理 URL、查询参数编码、JSON 请求、Access Token Header、超时、HTTP 错误和官方业务错误。OAuth 与官方 MCP 可以使用各自协议适配器，但复用相同的错误与脱敏规则。

### Domain Services

- `auth` 管理渠道、OAuth、Token 刷新和授权账户索引。
- `templates` 管理模板模型、迁移、校验、复制策略和创建向导。
- `materials` 负责上传素材和达人素材查询、规范化、筛选及可用性判断。
- `plans` 负责 payload 构建、提交事务、批量并发和失败续建。
- `reports` 负责报表字段发现、查询、关联和聚合。
- `strategy` 只消费标准化报表结果，不直接调用写接口。

## 计划创建事务

所有素材来源共享一个 `PlanExecutor`：

1. 校验模板归属、素材来源和运行参数。
2. 构建 project 与 promotion payload。
3. dry-run 时返回同一结构的预检结果。
4. submit 时先创建 project，再使用返回的 `project_id` 创建 promotion。
5. project 成功而 promotion 失败时返回可续建状态。
6. 批量执行器只负责调度任务、并发上限、journal 和结果汇总，不重复实现创建事务。

素材来源差异由 material adapter 注入 promotion payload，不能复制完整创建流程。

## 错误与输出

内部使用 `OceanWatchError(code, message, details, exit_code)`。所有 CLI 成功和失败均输出结构化 JSON：

```json
{
  "ok": false,
  "error": {
    "code": "template_binding_mismatch",
    "message": "selected template belongs to another advertiser",
    "details": {}
  }
}
```

凭据、Token、授权码和包含个人标识符的 MCP URL 必须在进入输出层前脱敏。

## 测试策略

- `tests/unit/`：纯函数、模型、校验和 payload。
- `tests/integration/`：使用假 Client 验证 OAuth、计划事务、批量续建和报表关联。
- `tests/cli/`：统一 CLI 的参数、JSON 契约和退出码。
- 测试不访问真实官方 API，不读取本机业务配置或凭据仓库。
- CI 在 Python 3.9 和 3.12、Windows/macOS/Linux 上执行编译、测试、JSON 校验和静态检查。

## 文档结构

- `README.md` / `README.en-US.md`：项目定位、能力、快速开始、架构概览和文档导航。
- `docs/getting-started.md`：从安装到首次授权的完整路径。
- `docs/configuration.md`：配置 Schema、凭据边界、渠道和模板。
- `docs/cli.md`：统一命令树、参数和示例。
- `docs/architecture.md`：模块依赖、数据流、错误模型和扩展方式。
- `CONTRIBUTING.md`：开发环境、测试、代码规范和 PR 要求。
- `SKILL.md`：只保留 Codex 执行所需的路由和安全约束，详细字段移入 references。

## 迁移策略

这是一次原子架构迁移：先建立公共基础设施和统一 CLI，再按领域移动实现并替换内部依赖，随后迁移测试和文档，最后删除旧 `scripts/`。任何阶段都不提交真实配置。完成条件是旧入口全部移除、统一 CLI 能覆盖现有能力、测试与校验全部通过。
