# 项目结构

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

`ocean-watch` 是单 Skill 仓库：仓库根目录本身就是 Codex 可安装的 `ads-plan-monitor` Skill。

## 仓库结构

```text
.
├── SKILL.md
├── agents/
│   └── openai.yaml
├── assets/
│   ├── config.example.json
│   └── plan-input.example.json
├── references/
│   ├── current-template-notes.md
│   └── official-api-notes.md
├── scripts/
│   ├── credential_store.py
│   ├── oauth_local_authorize.py
│   ├── token_manager.py
│   ├── first_run.py
│   ├── validate_config.py
│   ├── create_plan.py
│   ├── batch_create_from_today_videos.py
│   ├── query_active_materials_report.py
│   ├── query_custom_report.py
│   ├── query_report_config.py
│   ├── query_videos.py
│   └── ...
├── docs/
│   ├── configuration.md
│   ├── commands.md
│   └── project-structure.md
├── README.md
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

## 目录职责

| 路径 | 面向对象 | 职责 |
| --- | --- | --- |
| `SKILL.md` | Codex | Skill 触发后的核心工作指令 |
| `agents/` | Codex UI | Skill 展示名称、简介和默认提示 |
| `assets/` | 用户和脚本 | 示例配置、输入模板；只能放占位符 |
| `references/` | Codex | 按需读取的 API 笔记和模板规则 |
| `scripts/` | 用户和 Codex | 授权、查询、创建、批量创建等确定性脚本 |
| `docs/` | 人类用户 | 安装、配置、命令和结构说明 |
| `README.md` | 人类用户 | 项目首页和快速开始 |
| `SECURITY.md` | 维护者和用户 | 密钥、运行产物和敏感信息处理 |
| `CONTRIBUTING.md` | 贡献者 | 开发原则和提交前检查 |

## 本地私有目录

这些目录可能在开发或使用时出现，但不属于开源包：

```text
config/
runs/
.venv/
```

| 路径 | 说明 |
| --- | --- |
| `config/` | 真实广告账户、商品模板、落地页、监测链接、素材 ID 等业务配置 |
| `runs/` | API 请求/响应、dry-run 结果、报表导出和调试文件 |
| `.venv/` | Python 虚拟环境 |

## 设计边界

- 仓库根目录即 Skill 根目录，安装时直接链接整个仓库。
- 人类文档放 `README.md` 和 `docs/`。
- Codex 运行时规则放 `SKILL.md` 和 `references/`。
- 真实业务配置只放本地 `config/`，不进入 Git。
- 示例配置必须脱敏，只保留结构和占位符。
- API 流程放 `scripts/`，减少手工拼接请求导致的错误。
- 写操作默认先 dry-run 或 payload 预览，用户明确要求后再提交。

## 提交前结构检查

```bash
git status --short --ignored
git ls-files config runs .venv
```

第二条命令应该没有输出。
