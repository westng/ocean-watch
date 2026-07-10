# 项目结构

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

`ocean-watch` 是单 Plugin、单 Skill 仓库。仓库根目录是 Codex Plugin 和 marketplace 根；`skills/ads-plan-monitor/` 是 Skill 根。创建计划、查询数据和逻辑策略是同一个 Skill 的内部执行分支。

## 仓库结构

```text
.
├── .agents/plugins/
│   └── marketplace.json
├── .codex-plugin/
│   └── plugin.json
├── skills/
│   └── ads-plan-monitor/
│       ├── SKILL.md
│       ├── agents/
│       │   └── openai.yaml
│       ├── assets/
│       │   └── config.example.json
│       ├── references/
│       │   ├── current-template-notes.md
│       │   └── official-api-notes.md
│       └── scripts/
│           ├── configure_official_mcp.py
│           ├── credential_store.py
│           ├── oauth_local_authorize.py
│           ├── token_manager.py
│           ├── first_run.py
│           ├── create_plan.py
│           ├── batch_create_from_today_videos.py
│           └── ...
├── docs/
├── tests/
├── .github/workflows/ci.yml
├── README.md
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

## 目录职责

| 路径 | 面向对象 | 职责 |
| --- | --- | --- |
| `.agents/plugins/marketplace.json` | Codex CLI | 本地或 GitHub marketplace 入口 |
| `.codex-plugin/plugin.json` | Codex | Plugin 元数据和 Skill 发现路径 |
| `skills/ads-plan-monitor/SKILL.md` | Codex | Skill 触发后的核心工作指令 |
| `skills/ads-plan-monitor/agents/` | Codex UI | Skill 展示名称、简介和默认提示 |
| `skills/ads-plan-monitor/assets/` | 用户和脚本 | 脱敏示例配置；只能放占位符 |
| `skills/ads-plan-monitor/references/` | Codex | 按需读取的 API 笔记和模板规则 |
| `skills/ads-plan-monitor/scripts/` | 用户和 Codex | 授权、查询、创建、批量创建和 MCP 配置 |
| `tests/` | 维护者 | 不调用真实业务 API 的回归测试 |
| `docs/` | 人类用户 | 安装、配置、命令、设计和结构说明 |

## 配置定位

脚本按以下顺序查找业务配置：

1. 命令行 `--config`。
2. 环境变量 `ADS_PLAN_MONITOR_CONFIG`。
3. Git 开发仓库根目录的 `config/ads-plan-monitor/config.json`。
4. 用户目录的 `~/.codex/ads-plan-monitor/config.json`。

安装后的 Plugin 通常不包含 `.git`，因此自动使用用户目录配置。OAuth 和 MCP 参数独立保存在系统凭据仓库。

## 本地私有目录

`config/`、`runs/` 和 `.venv/` 可能在开发时出现，但不属于开源包。提交前运行：

```bash
git status --short --ignored
git ls-files config runs .venv
```

第二条命令应该没有输出。

## 设计边界

- Plugin 清单不存个人 MCP URL 或凭据。
- Codex 运行时规则只放在 Skill 和 references 中。
- 真实业务配置、OAuth 凭据和运行结果不进入 Git。
- API 写操作默认先 dry-run 或预览，用户明确要求后再提交。
- 官方 MCP 只负责开发文档、Schema 和 SDK 示例，不替代业务 API。
