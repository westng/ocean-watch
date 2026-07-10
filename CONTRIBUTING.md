# Contributing

> Organization: westng
> Project: ocean-watch
> Skill: ads-plan-monitor

欢迎改进 `ocean-watch`。这个仓库维护的是 Codex Skill，请尽量让变更保持小而清楚。

## 开发原则

- `SKILL.md` 只放 Codex 执行任务所需的核心指令。
- 详细规则放在 `references/`。
- 可重复、容易出错的 API 流程放在 `scripts/`。
- 示例文件必须使用占位符，不要放真实账户、商品、品牌、投放链接或 token。
- 默认只读查询；写操作必须由用户明确要求。

## 提交前检查

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile scripts/*.py
python3 -m json.tool assets/config.example.json >/tmp/ocean-watch-config-check.json
python3 -m unittest discover -s tests -v
git diff --check
git status --short --ignored
```

提交前确认这些路径没有进入 staged files：

- `config/`
- `runs/`
- `.venv/`
- `__pycache__/`
- 本地日志、临时 JSON、CSV 输出

## 安全

不要在 PR、issue、commit message 或示例文件中粘贴真实 OAuth 凭据、广告账户数据、业务链路或接口响应。更多细节见 `SECURITY.md`。
