# Contributing

感谢参与 Ocean Watch。请把仓库当作面向陌生贡献者的公共工程维护：模块职责、命令契约、错误语义和测试应当在不依赖历史对话的情况下可以理解。

开始前请先阅读[架构说明](docs/architecture.md)和[文档中心](docs/README.md)。

## 开发环境

```bash
python3 -m venv .venv
source .venv/bin/activate              # Windows: .venv\Scripts\activate
python3 -m pip install -e ".[dev]"
```

项目最低支持 Python 3.9，CI 同时验证 3.9 和 3.12。

## 架构规则

- `cli` 只解析参数、调用领域入口、映射输出与退出码。
- `core` 不依赖业务领域；配置、错误、锁和通用数据工具在这里单一实现。
- 普通巨量引擎业务请求必须通过 `api.OceanEngineClient`。
- 项目/单元创建必须通过 `plans.PlanExecutor`，批量模块不得复制创建事务。
- 素材来源差异放在 `materials` 或 payload adapter，不复制完整计划流程。
- 模板归属与素材来源是执行约束，不能靠命令参数绕过。
- 新代码使用明确依赖注入测试，不 monkey-patch 已删除的历史函数。
- 不为一次调用创建抽象；只有真实共享职责才进入 `core` 或公共服务。

## Skill 与文档

- `SKILL.md` 只保留 Codex 路由、安全边界和执行约束。
- 详细 API 字段放 `references/`。
- 用户安装、配置和命令放 `README` 与 `docs/`。
- 修改 CLI、配置 Schema 或行为时，同步中英文 README 和对应文档。
- 已完成的阶段性设计稿通过 Git 历史追溯，不长期保留在正式文档目录。
- 示例只能使用明显占位符，不能出现真实账户、商品、品牌、链接或凭据。

## 测试

- 单元测试覆盖纯函数、模板规则、素材校验和 payload。
- 集成测试通过 Fake Client 验证 OAuth、计划事务、批量续建和报表关联。
- CLI 测试覆盖命令路由、参数透传、JSON 错误和退出码。
- 测试不得访问网络、系统凭据或真实业务配置。

提交前运行：

```bash
PYTHONPATH=skills/ads-plan-monitor/src python3 -m compileall -q skills/ads-plan-monitor/src/ocean_watch scripts
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src scripts tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch scripts
python3 scripts/release.py check
python3 -m build
python3 skills/ads-plan-monitor/run.py --version
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
python3 -m json.tool skills/ads-plan-monitor/assets/config.example.json >/dev/null
git diff --check
```

发布前还必须分别在 Python `3.9` 和 `3.12` 的干净环境中安装生成的 wheel，并用临时 `CODEX_HOME` 执行 `ocean-watch setup init --home-config`，确认安装态资源和命令不依赖源码目录。

## 发布

正式发布不从本机手工上传产物。维护者先同步 `pyproject.toml`、`ocean_watch.__version__` 和 Plugin 基础版本，将 `Unreleased` 内容整理到对应版本的 Changelog 标题，并确认 `main` 的 CI 成功。然后在 GitHub Actions 手动运行 `Release` 工作流并选择 `main`。工作流会从三处版本元数据自动生成 `vMAJOR.MINOR.PATCH` Tag，重新运行质量门、构建 CLI 和完整 Plugin 包、生成校验和来源证明，最后创建 Tag 和 GitHub Release。

不要在版本不一致、Changelog 未收口或 CI 失败时强行创建 Release。完整检查清单和命令见[发布指南](docs/releasing.md)。

## Pull Request

PR 描述应包含：问题、设计选择、行为变化、测试结果和安全影响。避免在同一个 PR 混入无关重构。若改动官方字段语义，请附官方文档链接并说明验证方式。

## 敏感信息

提交前确认以下路径没有被跟踪：`config/`、`runs/`、`.venv/`、任务清单、journal、日志和 API 响应。更多要求见 `SECURITY.md`。
