# 发布指南

Ocean Watch 直接以 Git 仓库作为 Codex Marketplace 来源。GitHub Release 用于版本发现、变更记录和固定 Tag，不上传 Plugin ZIP、Python wheel、源码分发包或其他自定义 Release 资产。GitHub 自动显示的 `Source code (zip/tar.gz)` 是对应 Tag 的平台源码快照，无法也无需由项目工作流管理。

## 分发模型

默认安装始终跟随 Marketplace 当前检出的版本：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

需要可复现安装时，在注册 Marketplace 时固定 Release Tag：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

`.agents/plugins/marketplace.json` 将 Codex 指向仓库根目录，`.codex-plugin/plugin.json` 再通过 `./skills/` 发现两个 Skill。GitHub Release 资产不参与安装流程。

## 发布契约

正式版本必须同时满足：

- `pyproject.toml`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本一致。
- 版本使用 `MAJOR.MINOR.PATCH`，Tag 使用对应的 `vMAJOR.MINOR.PATCH`。
- `CHANGELOG.md` 存在 `## X.Y.Z - YYYY-MM-DD` 段落，且 `未发布` 段落已经清空。
- 发布从当前 `origin/main` 提交手动触发。
- 编译、Ruff、Bandit、全部测试、Plugin 清单与两个 Skill 元数据校验通过。

Release 页面正文只使用 Changelog 中对应版本段落。工作流不自动生成提交列表，也不构建或上传可下载资产。

## 本地预检

```bash
python3 -m pip install -e ".[dev]"
python3 scripts/release.py check
python3 scripts/release.py tag
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
4. 在 GitHub Actions 打开 `Release`，选择 `main`，点击 **Run workflow**。

```bash
python3 scripts/release.py check --tag vX.Y.Z
python3 scripts/release.py notes --tag vX.Y.Z --output release-notes/RELEASE_NOTES.md
gh workflow run release.yml --repo westng/ocean-watch --ref main
```

工作流会从项目版本生成 Tag，重新执行质量门，然后创建 GitHub Release。已存在的同名 Tag 必须指向当前 `main` 提交；重跑时只更新标题和版本说明。发布任务拥有最小化的 `contents: write` 权限，验证任务保持仓库只读。
