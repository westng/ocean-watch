# 发布指南

Ocean Watch 直接以 Git 仓库作为 Codex Marketplace 来源。项目只发布版本 Tag，不创建 GitHub Release 页面，也不构建或上传 Plugin ZIP、Python wheel、源码分发包、校验和或其他 Release 资产。

## 发布输出

| 输出 | 是否产生 | 用途 |
| --- | --- | --- |
| `vMAJOR.MINOR.PATCH` Tag | 是 | 固定 Marketplace 可复现安装版本 |
| GitHub Release 页面 | 否 | 版本变化统一记录在 `CHANGELOG.md` |
| Release assets | 否 | 不构建、不上传，也不参与安装 |

## 分发模型

默认安装始终跟随 Marketplace 当前检出的版本：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

需要可复现安装时，在注册 Marketplace 时固定版本 Tag：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

`.agents/plugins/marketplace.json` 将 Codex 指向仓库根目录，`.codex-plugin/plugin.json` 再通过 `./skills/` 发现两个 Skill。安装直接读取固定 Tag 对应的仓库内容，不依赖 GitHub Release。

## 发布契约

正式版本必须同时满足：

- `pyproject.toml`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本一致。
- 版本使用 `MAJOR.MINOR.PATCH`，Tag 使用对应的 `vMAJOR.MINOR.PATCH`。
- `CHANGELOG.md` 存在 `## X.Y.Z - YYYY-MM-DD` 段落，且 `未发布` 段落已经清空。
- 发布从当前 `origin/main` 提交手动触发。
- 编译、Ruff、Bandit、全部测试、Plugin 清单与两个 Skill 元数据校验通过。

版本变化只记录在 Changelog 对应版本段落。工作流不生成 Release Notes、不执行打包命令，也不创建 GitHub Release 或上传 artifact。

## 本地预检

```bash
python3 -m pip install -e ".[dev]"
python3 scripts/version_tag.py check
python3 scripts/version_tag.py tag
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src scripts tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch scripts
python3 skills/ads-plan-monitor/run.py --version
python3 skills/qc-plan-monitor/run.py --version
git diff --check
```

## 维护者发版

1. 将 `CHANGELOG.md` 的 `未发布` 内容整理到 `## X.Y.Z - YYYY-MM-DD`，并保留空的 `## 未发布` 段落。
2. 同步更新 `pyproject.toml`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本。Plugin 的 `+codex.*` cachebuster 可以保留。
3. 运行本地预检，提交版本改动，并等待 `main` 的 CI 成功。
4. 在 GitHub Actions 打开 `Publish Tag`，选择 `main`，点击 **Run workflow**。

```bash
python3 scripts/version_tag.py check --tag vX.Y.Z
gh workflow run tag.yml --repo westng/ocean-watch --ref main
```

工作流会从项目版本生成 Tag，重新执行质量门，然后把 Tag 推送到远端。已存在的同名 Tag 必须指向当前 `main` 提交；指向一致时重跑不会重复修改。Tag 推送任务拥有最小化的 `contents: write` 权限，验证任务保持仓库只读。
