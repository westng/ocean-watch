# 架构说明

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## 设计原则

1. **一个 Plugin，一个 Skill，一个 CLI。** Codex 通过 Skill 路由，所有确定性操作通过统一 CLI 执行。
2. **领域边界优先。** 授权、模板、素材、计划、报表各自拥有模型与规则。
3. **基础设施单一实现。** 配置、凭据、HTTP、错误、锁和公共数据工具不能在领域模块复制。
4. **写操作显式。** 创建默认 dry-run，在线提交必须显式 `--submit`。
5. **业务状态外置。** 真实配置、凭据、任务清单、journal 和结果不进入开源仓库。

## 模块依赖

```mermaid
flowchart LR
    Skill["ads-plan-monitor Skill"] --> CLI["cli"]
    CLI --> Auth["auth"]
    CLI --> Templates["templates"]
    CLI --> Materials["materials"]
    CLI --> Plans["plans"]
    CLI --> Reports["reports"]
    CLI --> Discovery["discovery"]
    Auth --> Core["core"]
    Templates --> Core
    Materials --> API["api"]
    Plans --> API
    Reports --> API
    Discovery --> API
    API --> Core
```

领域模块不能导入 CLI。`core` 不能导入任何业务领域。普通业务网络请求只能由 `api.OceanEngineClient` 发出。

## API Client

`OceanEngineClient` 统一负责：

- 基础 URL 与 endpoint 拼接。
- GET 查询参数的官方 JSON 编码。
- `Access-Token` 与 JSON Header。
- 请求超时。
- HTTP 错误转换。
- 网络错误转换为结构化 `ApiError`。

OAuth Token endpoint 和官方 MCP 使用不同协议适配器，可以直接使用标准库网络，但必须遵守相同脱敏和错误边界。

## 计划创建

`PlanExecutor` 是所有素材来源和批量模式共享的创建事务：

```mermaid
sequenceDiagram
    participant Command as Plan command
    participant Executor as PlanExecutor
    participant API as OceanEngineClient
    Command->>Executor: project payload + promotion payload
    alt dry-run
        Executor-->>Command: preflight payloads
    else submit
        Executor->>API: project/create
        API-->>Executor: project_id
        Executor->>API: promotion/create(project_id)
        API-->>Executor: promotion_id or error
        Executor-->>Command: completed or resumable failure
    end
```

上传素材和达人素材只负责选择、校验并把来源特有字段注入 promotion payload。批量模块只负责并发调度、journal 和汇总，不重复实现项目/单元提交。

## 模板模型

模板 Schema v3 包含：

- `default_plan_template`：跨业务默认骨架，不可投放。
- `plan_templates`：真实业务模板。
- `bindings`：渠道、广告主、平台、流量来源、商品 ID 与商品名。
- `material_strategy`：`ACCOUNT_UPLOAD` 或 `CREATOR_AUTHORIZED`。
- `overrides`：业务投放参数、链接、追踪链接和官方资产 ID。
- `copy_materials`：标题文案及复制来源。

模板绑定是执行约束。目标渠道或广告主不匹配时，创建在 Token 刷新和 API 调用前停止。

## 授权与凭据

每个渠道拥有独立应用、授权记录和广告主索引。一次 OAuth 授权对应一个本地 authorization record；业务命令根据目标 `advertiser_id` 解析正确 Token。

所有渠道共用 `http://127.0.0.1:8787/oauth/callback`。OAuth `state` 采用 `<渠道短码>.<随机值>`：巨量营销为 `AD`，巨量千川为 `QC`。回调处理器校验完整 `state` 后再解析渠道，解析结果必须与发起授权的渠道一致，Token 交换和存储使用回调确认后的渠道。

敏感字段保存在操作系统凭据仓库。非敏感授权索引和迁移 journal 位于用户状态目录。配置文件只保存业务设置和公开 endpoint。

## 错误与输出

CLI 边界使用机器可读 JSON。公共异常包含稳定 `code`、用户可读 `message`、可选 `details` 和退出码。领域命令尚未迁移到公共异常的历史结果仍保持结构化 JSON，新增代码应优先使用 `OceanWatchError`。

所有输出必须在打印前脱敏。严禁输出 Secret、Access Token、Refresh Token、授权码或包含用户标识符的完整 MCP URL。

## 测试边界

- 纯模型、校验和 payload 使用单元测试。
- API 事务通过 Fake Client 或显式依赖注入测试。
- CLI 测试验证命令路由、参数透传、JSON 错误和退出码。
- 测试不访问网络、不读取本机凭据、不加载真实业务配置。

新增领域能力时，应先扩展已有服务契约；只有确实存在新职责时才增加模块。
