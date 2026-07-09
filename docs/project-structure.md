# 项目结构

`ocean-watch` 是一个 Codex Skill 仓库。根目录面向开发者和开源用户，`skills/ads-plan-monitor` 面向 Codex 运行时。

## 根目录

```text
.
├── README.md
├── CONTRIBUTING.md
├── SECURITY.md
├── LICENSE
├── docs/
└── skills/
```

| 路径 | 说明 |
| --- | --- |
| `README.md` | 项目入口，说明定位、快速开始和文档导航 |
| `CONTRIBUTING.md` | 贡献规范、开发原则、提交前检查 |
| `SECURITY.md` | 密钥、运行产物和敏感信息处理 |
| `LICENSE` | 开源许可证 |
| `docs/` | 给人看的安装、配置、命令和结构文档 |
| `skills/` | Codex Skill 包 |

## Skill 包

```text
skills/ads-plan-monitor/
├── SKILL.md
├── agents/
│   └── openai.yaml
├── assets/
│   ├── config.example.json
│   └── plan-input.example.json
├── references/
│   ├── current-template-notes.md
│   └── official-api-notes.md
└── scripts/
    ├── credential_store.py
    ├── oauth_local_authorize.py
    ├── token_manager.py
    ├── first_run.py
    ├── validate_config.py
    ├── create_plan.py
    ├── batch_create_from_today_videos.py
    ├── query_active_materials_report.py
    ├── query_custom_report.py
    ├── query_report_config.py
    ├── query_videos.py
    └── ...
```

| 路径 | 说明 |
| --- | --- |
| `SKILL.md` | Codex 触发 Skill 后读取的主指令。保持精炼，不放用户手册。 |
| `agents/openai.yaml` | Skill 在 Codex UI 中的展示信息。 |
| `assets/` | 示例配置和输入模板。必须脱敏，只能放占位符。 |
| `references/` | Codex 按需读取的业务规则和 API 笔记。 |
| `scripts/` | 授权、查询、创建、批量创建等确定性脚本。 |

## 本地私有目录

这些目录用于本机运行，不应进入 Git：

```text
config/
runs/
.venv/
```

| 路径 | 说明 |
| --- | --- |
| `config/` | 真实广告账户、商品模板、落地页、监测链接、素材 ID 等业务配置 |
| `runs/` | API 请求/响应、dry-run 结果、报表导出、调试文件 |
| `.venv/` | Python 虚拟环境 |

## 设计边界

- 人类用户文档放在根目录和 `docs/`。
- Codex 运行时规则放在 `SKILL.md` 和 `references/`。
- 示例文件用占位符，真实配置只在本地 `config/`。
- API 流程尽量放脚本，避免让 Codex 每次重新拼接复杂请求。
- 写操作默认先 dry-run 或 payload 预览，用户明确要求后再提交。

## 提交前结构检查

```bash
git status --short --ignored
git ls-files config runs .venv
```

第二条命令应该没有输出。
