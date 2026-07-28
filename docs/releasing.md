# 发布指南

Ocean Watch 的正式版本同时固定源码、Marketplace 快照和签名运行时候选。Codex Marketplace 仍从 Git 仓库的版本 Tag 安装；GitHub Release 承载签名的 Go 运行时、平台 bootstrap、Plugin 候选 ZIP、校验和、SBOM 和 provenance。当前 Codex 不直接从 Release ZIP 安装 Plugin，因此 Release ZIP 是候选资产，不得描述为 Marketplace 安装路径。

Go SDK 迁移完成 G1–G5、真实 canary、五平台安装和独立签字前，仓库中的 `.codex-plugin/runtime-policy.json` 保持 `enabled:false`，生产命令继续走 Python。构建并发布签名资产不等于切换生产路由。

## 发布输出

| 输出 | 用途 |
| --- | --- |
| `vMAJOR.MINOR.PATCH` Tag | 固定 Marketplace 源码快照与发布提交 |
| GitHub Release 页面 | 使用对应中文 Changelog 版本段作为 Release notes |
| 5 个 `ocean-watch_<os>_<arch>` | Go 运行时候选；当前路由清单仍全部选择 Python |
| 5 个 `ocean-watch-bootstrap_<os>_<arch>` | 校验签名、身份、大小和 SHA-256 后下载/缓存运行时 |
| `ocean-watch-plugin_vX.Y.Z.zip` | 含五个平台 bootstrap 的候选包；不是当前 Marketplace 直接安装源 |
| `runtime-manifest.json` 与 `.sig` | 绑定版本、Tag、commit、SDK、路由和平台资产 |
| `checksums.json` 与 `.sig` | 覆盖精确的 17 个签名文件；候选目录共 19 个文件 |
| `ocean-watch.spdx.json` | SPDX 2.3 SBOM 与明确的依赖许可证 |
| `provenance.intoto.jsonl` | SLSA v1 兼容的源码、构建器和产物来源 |
| `build-summary.json`、`release-public-key.txt` | 可复现构建身份、路由计数和公开信任根 |
| `g5-seal.json` | 绑定候选、六类来源运行、最终摘要、受限签字和证据树的公开摘要；不包含审批人身份 |

候选目录固定为 19 个文件；GitHub Release 固定发布这 19 个文件和
`g5-seal.json`，合计 20 个资产。完整证据树和带审批身份的 `signoff.json` 只保留在
受限的 G5 sealed Actions artifact 中，不发布到公开 Release。

## Marketplace 边界

默认安装始终跟随 Marketplace 当前检出的仓库版本：

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

需要可复现安装时，在注册 Marketplace 时固定版本 Tag：

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

`.agents/plugins/marketplace.json` 指向仓库根目录，`.codex-plugin/plugin.json` 通过 `./skills/` 发现两个 Skill。Marketplace 安装不会自动解包 GitHub Release 中的候选 ZIP；在最终 Marketplace 装配路径通过 AC-124/AC-128 前，不得启用仓库运行时策略。

## 发布前置条件

正式版本必须同时满足：

- `pyproject.toml`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本一致。
- 版本使用 `MAJOR.MINOR.PATCH`，Tag 使用对应的 `vMAJOR.MINOR.PATCH`。
- `CHANGELOG.md` 存在 `## X.Y.Z - YYYY-MM-DD` 段落，且 `未发布` 段落没有实质内容。
- 工作流从当前 `origin/main` 的干净提交手动触发；同名 Tag 不得指向其他提交。
- GitHub Actions Secret `OCEAN_WATCH_RELEASE_SIGNING_KEY` 保存 Ed25519 seed 或私钥；任何步骤不得打印该值。
- GitHub Actions Variable `OCEAN_WATCH_RELEASE_PUBLIC_KEY` 保存批准的 32-byte 小写十六进制公开信任根，并与 Secret 推导出的公钥完全一致。
- Python、Go、供应链、Plugin/Skill 合同和适用的 P5 自动验收全部通过。
- 正式候选来自受保护的正式工作流；模型评测、真实 canary、Marketplace、前版本回滚和分批观察分别经过受保护的角色 intake。最终六类 artifact 来自六个不同且成功的可信 workflow run，全部绑定同一仓库、提交和候选身份；五个底层外部 producer run 也不得复用。
- G5 最终摘要为 `passed`，failed、blocking、missing、not-run 和过期例外均为零。
- MT、AO、QO、SO、RO、SCO 对最终摘要逐一签字；SO 与 RO 必须由不同审批人承担，全部审批发生在摘要生成之后。
- 发布资产不含真实 Token、广告主、作品链接、官方原始响应或 canary 明细。

签名密钥和公开信任根的建立、轮换与托管需要 Release Owner 和 Security Owner 独立审批。工作流只消费已配置值，不生成、回显或提交密钥。至少配置以下受保护环境：

| 环境 | 用途 | 敏感输入/权限 |
| --- | --- | --- |
| `g5-release-candidate` | 构建正式候选 | Secret `OCEAN_WATCH_RELEASE_SIGNING_KEY`；Variable `OCEAN_WATCH_RELEASE_PUBLIC_KEY` |
| `g5-external-evidence` | 复验并绑定模型、canary、Marketplace、回滚和 rollout 外部证据角色 | 无签名密钥、仓库只读、受保护审批人 |
| `g5-independent-signoff` | 验证外部汇总的六角色签字 | Secret `G5_SIGNOFF_JSON_BASE64`；受保护审批人 |
| `g5-release-seal` | 将候选、证据、摘要和签字密封 | 无签名密钥、仓库只读、受保护审批人 |
| `g5-release-publish` | 发布既有 seal | 仅该 Job 获得 `contents: write`；受保护审批人 |

## 自动门禁

发布链按职责拆分，后一步只消费前一步的不可变 artifact：

1. `.github/workflows/g5-evidence.yml` 在 `g5-release-candidate` 环境从受保护的当前 `main` 构建两次正式候选，验证批准的信任根与逐字节可复现性，并在五个平台消费同一个候选。它执行 Python/Go 质量、安全、供应链、launcher、合同、升级/回滚和合成用户旅程验收，产出正式候选与自动证据。签名 Secret 只存在于该工作流的候选构建 Job。
2. 模型评测、真实 canary、Marketplace、前版本回滚和分批观察由各自受控流程产生，artifact 名必须分别为 `g5-model-source-RUN_ID`、`g5-canary-source-RUN_ID`、`g5-marketplace-source-RUN_ID`、`g5-rollback-source-RUN_ID`、`g5-rollout-source-RUN_ID`。每类 artifact 只能包含验收矩阵为该角色声明的固定文件。
3. `.github/workflows/g5-external-evidence.yml` 在 `g5-external-evidence` 环境下载正式候选和一个底层 source artifact，复验提交、正式 candidate identity、角色文件集合和每个 AC 结果，再生成带底层 run/workflow/artifact 与逐文件 hash 的角色 attestation。五类 intake run 和五个底层 producer run 均不得复用。
4. `.github/workflows/g5-seal.yml` 的 `prepare` 模式只接收正式与五类 intake 的六个 run ID。workflow path 和 artifact name 由仓库策略固定推导；它通过 GitHub API 核对成功状态、仓库、attempt 和 head SHA，下载后再次验证角色 attestation 与五个底层 source run 的唯一性，合并时禁止覆盖同路径证据，并以 `--require-ready` 生成最终摘要。
5. 审批人在准备 artifact 上审阅同一候选和摘要。外部系统按 `contracts/gates/g5-signoff.schema.json` 汇总六角色签字，再由 `.github/workflows/g5-signoff.yml` 从受限 Secret 消费；工作流不会生成 `approved` 决策或审批身份。
6. `.github/workflows/g5-seal.yml` 的 `seal` 模式只接受不同的成功 prepare run 和 signoff run。它重新验证精确摘要、签字、候选、六来源元数据和证据 hash，生成可离线复验的 sealed artifact。
7. `.github/workflows/tag.yml` 只接收一个成功的 `sealed_run_id`。只读 `validate` Job 和写入 `publish` Job 分别独立下载并复验同一个 seal；两者都不构建候选、不读取签名 Secret。只有通过 `g5-release-publish` 环境的 `publish` Job 拥有 `contents: write`，并发布 seal 中原始候选字节。

任何普通 CI、测试签名候选、示例签字、源代码中的占位 JSON 或仅本地通过的检查都不能替代上述正式证据或审批。

## 不可变与幂等

- 工作流不使用 `--clobber`，不覆盖 Tag、Release notes 或资产。
- 已存在 Tag 必须解析到本次验证的 `GITHUB_SHA`，否则立即失败。
- 已存在 Release 必须具有相同 Tag、标题、目标 commit、非 draft/非 prerelease 状态和 Changelog notes。
- 已发布的 19 个候选资产和 `g5-seal.json` 的名称集合、大小和 SHA-256 必须与 seal 逐字节一致；缺失、额外或差异资产都会失败。
- 修复候选或回滚必须提升 patch 版本并发布新 Tag，不能修改历史 Release。

## 本地预检

本地预检不代替 GitHub 的干净双构建、Secret 信任绑定或独立审批：

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

需要验证本地合成候选时，只能使用专用测试签名种子，不能复用正式私钥；dirty 候选会记录 `source_dirty:true` 且不能以 `--release` 构建或发布。

## 维护者发版

1. 将 `CHANGELOG.md` 的 `未发布` 内容整理到 `## X.Y.Z - YYYY-MM-DD`，并保留空的 `## 未发布` 段落。
2. 同步更新项目、Python package 和 Plugin 基础版本；Plugin 的 `+codex.*` cachebuster 必须随同新产品版本发布。
3. 完成本地预检、评审和 `main` CI，确认五个受保护环境、Secret、公开信任根变量及分支保护已经配置。
4. 从 `main` 触发 **G5 Formal Candidate Evidence**，记录成功的正式证据 run ID。模型、canary、Marketplace、回滚和 rollout 外部流程分别生成固定命名且固定文件集合的 source artifact，并记录五个不同成功 source run ID。
5. 对每个外部角色触发 **Intake Role-Bound External G5 Evidence**，只传角色、正式 run ID 和该角色 source run ID。记录五个成功且不同的 intake run ID；不要手工指定 artifact name 或 workflow path。
6. 组装精确六来源 run ID JSON，通过 **Prepare or Seal G5 Release** 的 `prepare` 模式生成 ready summary。输入必须只有 `formal_evidence`、`model_evidence`、`canary_evidence`、`marketplace_evidence`、`rollback_evidence`、`rollout_evidence` 六个键，每个值都是正整数 run ID。
7. 六个 Owner 审阅准备 artifact 后，在仓库外生成与精确 `summary.json` hash 绑定的签字 JSON。经受控流程写入 `g5-independent-signoff` 的 `G5_SIGNOFF_JSON_BASE64`，再触发 **Validate Independent G5 Signoff** 并记录成功 run ID。
8. 再次触发 **Prepare or Seal G5 Release** 的 `seal` 模式，传入不同的 prepare run ID 和 signoff run ID。检查 sealed artifact 能重新验证，且 `seal.json` 的 commit、Tag、候选、六来源和摘要 hash 均正确。
9. 触发 **Publish Sealed Release**，只传成功的 seal run ID。检查 Tag commit、中文 Release notes、20 个资产、公开信任根和 `g5-seal.json`；完整签字身份不得出现在公开 Release。

```bash
python3 scripts/version_tag.py check --tag vX.Y.Z
gh workflow run g5-evidence.yml --repo westng/ocean-watch --ref main
gh workflow run g5-external-evidence.yml --repo westng/ocean-watch --ref main \
  --field role=model \
  --field formal_run_id="${FORMAL_RUN_ID}" \
  --field source_run_id="${MODEL_SOURCE_RUN_ID}"
gh workflow run g5-seal.yml --repo westng/ocean-watch --ref main \
  --field mode=prepare \
  --field source_run_ids_json="${SOURCE_RUN_IDS_JSON}"
gh workflow run g5-signoff.yml --repo westng/ocean-watch --ref main \
  --field prepared_run_id="${PREPARED_RUN_ID}"
gh workflow run g5-seal.yml --repo westng/ocean-watch --ref main \
  --field mode=seal \
  --field prepared_run_id="${PREPARED_RUN_ID}" \
  --field signoff_run_id="${SIGNOFF_RUN_ID}"
gh workflow run tag.yml --repo westng/ocean-watch --ref main \
  --field sealed_run_id="${SEALED_RUN_ID}"
```

发布成功不自动更改 `.codex-plugin/runtime-policy.json`、不执行真实 API、canary、部署或 Marketplace 全量推广。生产路由的变更必须作为独立版本通过文档中的 Gate 和签字。

## 回滚与密钥事件

- 命令或领域回滚通过新版本路由清单选择 Python；用户配置、授权和模板不降级或删除。
- 资产、缓存或运行时缺陷通过新 patch 版本修复；已发布资产保持不可变。
- 可能已执行的写请求必须先对账，不能用重跑或切路由代替恢复。
- 怀疑私钥泄露时立即停止发布、撤销/轮换密钥并由 Security Owner 评审。更换公开信任根会影响 bootstrap 信任链，必须发布新产品版本并重新完成供应链、安装和回滚验收。
