# 项目结构

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skills: ads-plan-monitor, qc-plan-monitor

```text
.
├── .agents/plugins/marketplace.json
├── .codex-plugin/plugin.json
├── .github/workflows/ci.yml
├── pyproject.toml
├── skills/ads-plan-monitor/
│   ├── SKILL.md
│   ├── agents/openai.yaml
│   ├── assets/
│   ├── references/
│   ├── run.py
│   └── src/ocean_watch/
│       ├── api/
│       ├── accounts/
│       ├── auth/
│       ├── cli/
│       ├── core/
│       ├── discovery/
│       ├── integrations/
│       ├── materials/
│       ├── onboarding/
│       ├── plans/
│       ├── reports/
│       ├── strategy/
│       └── templates/
├── skills/qc-plan-monitor/
│   ├── SKILL.md
│   ├── agents/openai.yaml
│   ├── assets/
│   ├── references/
│   └── run.py
├── tests/
├── docs/
├── README.md
├── README.en-US.md
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

## 路径职责

| 路径 | 职责 |
| --- | --- |
| `.codex-plugin/plugin.json` | Plugin 清单与 UI 元数据 |
| `.agents/plugins/marketplace.json` | GitHub/本地 marketplace 入口 |
| `skills/ads-plan-monitor/SKILL.md` | Codex 触发、路由和安全约束 |
| `skills/ads-plan-monitor/assets/` | 脱敏配置与任务示例 |
| `skills/ads-plan-monitor/references/` | 按需加载的官方 API 与模板说明 |
| `skills/ads-plan-monitor/run.py` | Plugin 内无需安装的统一 CLI 启动器 |
| `skills/ads-plan-monitor/src/ocean_watch/` | 可测试、可安装的应用包 |
| `skills/ads-plan-monitor/src/ocean_watch/accounts/` | 跨渠道负责账户模型和管理 CLI |
| `skills/qc-plan-monitor/SKILL.md` | 千川授权、商品模板、作品链接计划处理与官方 MCP 全域报表规则 |
| `skills/qc-plan-monitor/assets/` | 千川脱敏 payload 示例 |
| `skills/qc-plan-monitor/references/` | 千川官方 API 说明 |
| `skills/qc-plan-monitor/run.py` | 复用共享 CLI 的千川 Skill 启动器 |
| `tests/` | 不访问真实 API 的单元、集成和 CLI 测试 |
| `docs/` | 用户、架构和贡献者文档 |

## 不进入仓库的目录

- `config/`：真实业务配置。
- `runs/`：报表和运行结果。
- `.venv/`：本地虚拟环境。
- `__pycache__/`、`.pytest_cache/`、覆盖率文件和临时日志。
- Creator batch jobs 和执行 journal。

提交前运行：

```bash
git status --short --ignored
git ls-files config runs .venv
```

第二条命令应没有输出。
