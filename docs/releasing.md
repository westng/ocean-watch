# 发布指南

Ocean Watch 当前通过 Git 仓库的版本 Tag 固定 Codex Marketplace 源码快照，并为同一提交创建 GitHub Release 页面。Release notes 直接取自 `CHANGELOG.md` 对应的中文版本段；当前发布流程不构建或上传 Go 运行时候选资产。

生产命令仍全部走 Python。`prototype/ocean-watch-go/`、`prototype/runtime-bootstrap/` 以及签名、证据和候选验收工具用于未来 Go 切流设计；在 G1–G5、真实 canary、五平台安装和独立审批完成前，`.codex-plugin/runtime-policy.json` 必须保持 `enabled: false`。

## 当前发布输出

| 输出 | 用途 |
| --- | --- |
| `vMAJOR.MINOR.PATCH` Tag | 固定 Marketplace 源码快照和发布提交 |
| GitHub Release | 发布对应中文 Changelog 版本说明 |

Codex Marketplace 从 Git Tag 安装，不从 GitHub Release 资产安装：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

## 发布前置条件

- 当前 `pyproject.toml`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本一致。
- `CHANGELOG.md` 的 `## 未发布` 中存在至少一条实质内容；没有内容时不会发布空版本。
- 发布工作流从当前 `origin/main` 手动触发。
- `main` 的 CI 已通过，发布工作流会在生成版本文件后再次运行质量门禁。
- 线上最新正式 Release Tag 使用 `vMAJOR.MINOR.PATCH`；工作流自动递增 patch。
- 目标 Tag 不存在，或已经指向本次发布提交。
- 同名 Release 不存在，或标题、状态和 Changelog notes 与本次结果完全一致。

当前 Release 工作流不要求 GitHub Environment、签名 Secret 或公钥 Variable。未来正式发布 Go 资产时，必须另行设计并评审受保护环境、信任根、跨平台候选、证据、签字与密封流程，不能把底层工具存在视为发布门禁已经启用。

## 日常 CI

`.github/workflows/ci.yml` 只在 `main` push 和 pull request 上运行：

- Python 3.12 在 Linux、Windows 和 macOS 执行完整质量与回归测试。
- Python 3.10 在 Linux 执行生产包、CLI、首次运行和核心兼容测试。
- Linux 顺序测试并 vet 两个 Go module。

日常 CI 不构建非正式候选、不执行五平台 native candidate 消费，也不上传 P0/G5 evidence artifact。这些工作不属于当前 Python Plugin 的每次提交门禁。

## 不可变与幂等

- 工作流不使用 `--clobber`，不覆盖既有 Tag 或 Release。
- 已存在 Tag 必须解析到本次 `GITHUB_SHA`，否则立即失败。
- 已存在 Release 必须具有相同 Tag、标题、非 draft/非 prerelease 状态和 Changelog notes。
- 修复已发布版本必须提升 patch 版本并发布新 Tag，不能修改历史 Release。

## 本地预检

```bash
python3 -m pip install -e ".[dev]"
python3 scripts/version_tag.py check
python3 scripts/version_tag.py tag
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src scripts tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch scripts
GOTOOLCHAIN=go1.26.5 go -C prototype/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C prototype/runtime-bootstrap test ./...
python3 skills/ads-plan-monitor/run.py --version
python3 skills/qc-plan-monitor/run.py --version
git diff --check
```

`scripts/version_tag.py check` 在日常开发阶段只检查版本元数据一致性。Release 工作流调用 `prepare`，根据线上最新正式 Release 自动生成下一个 patch 版本，再使用 `--tag vX.Y.Z` 检查日期版本段和空的 `未发布` 段落。

## 维护者发版

1. 将所有待发布行为准确记录在 `CHANGELOG.md` 的 `## 未发布` 中并推送到 `main`。
2. 等待 `main` CI 通过。
3. 从 `main` 手动触发 GitHub Actions 的 **Release** 工作流，无需填写参数。
4. 工作流自动读取线上最新正式 Release、递增 patch、归档 Changelog、同步项目/Python package/Plugin 版本、生成发布提交并推送回 `main`。
5. 质量门禁通过后，工作流为该发布提交创建 Tag 和 GitHub Release；核对结果后再用该 Tag 注册或更新 Marketplace。

```bash
gh workflow run tag.yml --repo westng/ocean-watch --ref main
```

发布不会自动更改运行时策略，不执行真实 API、canary、部署或 Marketplace 全量推广。Go 生产切流必须作为独立版本重新完成架构、安全、发布与验收设计。

## 回滚

- Plugin 问题通过新 patch 版本修复；既有 Tag 和 Release 保持不可变。
- 用户需要立即回退时，将 Marketplace 固定到上一个已知正常 Tag。
- 可能已执行的广告写请求必须先通过官方接口对账，不能用重装 Plugin 代替业务恢复。
