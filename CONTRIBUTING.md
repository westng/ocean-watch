# Contributing

感谢参与 Ocean Watch。开始前请阅读[架构说明](docs/architecture.md)和[文档中心](docs/README.md)。

## 开发环境

- Go `1.26.5`
- Python `3.10+`，仅用于 F2 包装层、版本和分发工具
- F2 `0.0.1.7`

```bash
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install -e ".[dev]"
```

## 架构规则

- Go 是唯一业务运行时，不添加另一套业务实现或静默回退。
- CLI 只处理参数、路由、稳定 JSON envelope 和退出码。
- Application 依赖 Domain 与 Ports；生成 SDK 类型只进入 `internal/adapters/oceanengine`。
- 配置、凭据、Token、分页、重试、限流、锁、脱敏和写入对账保持单一实现。
- 创建、追加、删除和设置更新默认 dry-run，在线写入必须显式确认。
- Python Adapter 只允许固定 F2 的作品公开元数据用途，不扩展到广告业务。
- 新代码使用明确依赖注入和合成 fixture；测试不得访问真实网络、凭据或业务配置。

## Skill 与文档

- `SKILL.md` 保存意图、执行安全和强制展示合同；详细 API 字段放 `references/`。
- 修改行为时同步中英文 README、对应文档与 `CHANGELOG.md ## 未发布`。
- 示例只使用明显占位符，不提交真实账户、商品、链接、Cookie 或凭据。
- 修改 Plugin/Skill 后运行仓库分发校验；本地开发重装使用 Plugin Creator 的 cachebuster 流程，不手改 Marketplace 配置。

## 验证

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
skills/ads-plan-monitor/run --version
skills/qc-plan-monitor/run --help
git diff --check
```

CI 另在 Linux、macOS、Windows 验证 Go 与启动器，并在 Python 3.10、3.12 验证 F2 包装层。

## Pull Request 与发布

PR 描述应包含问题、设计选择、行为变化、验证结果和安全影响，避免混入无关重构。正式发布流程见[发布指南](docs/releasing.md)：发布准备先作为普通可审查提交进入 `main`，手动 Release 工作流只读验证固定提交并创建 Tag/Release，不修改或回推分支。

提交前确认 `config/`、`runs/`、`.venv/`、日志、任务清单、journal、Cookie 和 API 响应未被跟踪。
