# 发布指南

Ocean Watch 以 Git Tag 对应的仓库快照作为 Codex Marketplace 安装源。快照必须已经包含五个平台的 Go CLI；GitHub Release 说明来自 `CHANGELOG.md` 对应版本段。

## 日常推送

普通代码推送继续记录在 `## 未发布`，不自动升级版本、创建 Tag 或发布 Release。推送前必须：

1. 审查相对目标远端分支的全部待推送提交和差异。
2. 确认每项用户可见、兼容性、安全或发布行为都准确记录在 `## 未发布`；纯测试或无行为内部重构可不写，但要明确说明理由。
3. 实时查询 GitHub 最新非草稿、非预发布 Release，并报告版本、发布时间和 Tag。
4. 核对线上版本、Changelog、`pyproject.toml` 与 `.codex-plugin/plugin.json` 基础版本；普通推送不得借机升级版本。
5. 在实际 `git push` 前向用户汇报检查结论；任一项不通过则停止。

## 版本关系

- `pyproject.toml` 的 `project.version` 是产品基础版本。
- `.codex-plugin/plugin.json` 使用 `<基础版本>+codex.<cachebuster>`；cachebuster 用于生成新的安装快照。兼容升级可由稳定代理在同一任务内切换 Runtime；改变工具或 Skill Host 合同仍需要新任务加载。
- Go CLI 版本在构建时从 Plugin 基础版本注入。
- Python 没有 Ocean Watch 包版本；`pyproject.toml` 只声明 F2/开发依赖边界。

日常检查：

```bash
python3 scripts/version_tag.py check
```

正式发布前，维护者实时读取最新的非草稿、非预发布 Release。已有正式版本时，运行一次本地发布准备，将 `未发布` 归档到下一个 patch 并同步两个版本元数据：

```bash
latest_tag="$(gh api --method GET 'repos/westng/ocean-watch/releases?per_page=100' \
  --jq 'map(select(.draft == false and .prerelease == false))[0].tag_name // ""')"
release_date="$(date -u +%Y-%m-%d)"
test -n "${latest_tag}"
python3 scripts/version_tag.py prepare \
  --latest-tag "${latest_tag}" \
  --date "${release_date}" \
  --cachebuster "release-${release_date//-/}"
python3 scripts/version_tag.py release-check --latest-tag "${latest_tag}"
```

若查询结果为空，必须明确报告“尚无线上发布版本”，不要伪造 `v0.0.0`。首次发布应先人工审查目标基础版本，把 `未发布` 内容归档到同版本的带日期段落并保持 `未发布` 为空，再使用 `version_tag.py check --tag v<目标版本>` 校验；常规后续版本才使用上述自动递增 patch 的 `prepare`。

审查、测试、提交并推送这份发布准备，等待 `main` CI 成功。Release 工作流不会运行 `prepare`，不会修改或回推 `main`。

## 二进制

目标集合：

- `ocean-watch_darwin_amd64`
- `ocean-watch_darwin_arm64`
- `ocean-watch_linux_amd64`
- `ocean-watch_linux_arm64`
- `ocean-watch_windows_amd64.exe`

准备和可复现核对：

```bash
GOTOOLCHAIN=go1.27.0 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all
GOTOOLCHAIN=go1.27.0 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
python3 scripts/validate_distribution.py
```

构建固定 `CGO_ENABLED=0`、目标平台、时区、locale、`-trimpath` 和空 build ID。任何源码、Go 依赖、构建参数或基础版本变化都必须重新生成五个平台产物。

构建器同时生成 `.codex-plugin/runtime-manifest.json`，绑定 Plugin 名称/版本、Plugin 清单、`.mcp.json`、两套 Skill 全部文件、F2、稳定 Unix 启动器和五个平台二进制哈希。私有 Runtime 槽位必须包含清单声明的全部平台二进制，避免同一台 Apple Silicon 设备上的原生 arm64 App 与 Rosetta/x64 CLI 争用架构专属槽位。Runtime 实现的兼容升级不得改变外层 17 个 MCP 工具的名称、输入/输出 Schema、注解或错误边界；任何合同变化必须作为非兼容 Host 合同单独评审，不能依赖 Runtime 热切换。

## 质量门

```bash
GOTOOLCHAIN=go1.27.0 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.27.0 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
skills/ads-plan-monitor/run --version
skills/qc-plan-monitor/run --help
git diff --check -- . ':(exclude).codex-plugin/bin/**'
```

五个平台 Runtime 是 30MB 以上的二进制文件，文本 whitespace 检查必须排除该目录，避免 Git 为大对象差异分配无意义的文本比较内存。二进制完整性不因此降低：前面的 `build-runtime --all --verify` 会逐字节重建并比较全部五个平台产物和 Runtime manifest。

日常 CI 在 Linux、macOS、Windows 验证 Go 运行时和当前平台启动器，在 Python 3.10/3.12 验证 F2 包装层，并验证 Plugin/Skill/二进制分发合同。

## 正式发布

只有用户或维护者明确授权正式发布时，才手动运行 GitHub Actions 的 `Release`：

1. 固定当前 `origin/main` 提交。
2. 对照线上最新正式 Release 做只读版本校验。
3. 运行 Go 测试/vet、F2 测试、启动器与分发校验。
4. 临时重建五个平台二进制并逐字节核对仓库产物。
5. 确认工作树未被修改且远端 `main` 未前进。
6. 创建或验证指向该提交的不可变 Tag 和 GitHub Release。

工作流不读取业务凭据、不调用真实广告接口、不修改版本文件、不生成机器人提交、不回推 `main`。Tag、Release 和 Marketplace 发布仍是独立操作，不包含在普通 `git push` 中。

## 回滚

已发布 Tag 和 Release 不覆盖、不移动。修复问题应提升 patch 并发布新版本；需要回滚安装时固定到先前已审查 Tag。用户的 `$CODEX_HOME` 配置与授权状态不随代码 Tag 自动转换或删除。
