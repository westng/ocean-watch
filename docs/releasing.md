# 发布指南

本文说明 Ocean Watch 正式产物、下载校验、离线安装和维护者发版流程。普通在线安装仍以根目录 README 的 Codex Marketplace 方式为首选。

## 发布产物

| 文件 | 用途 | 运行条件 |
| --- | --- | --- |
| `ocean-watch-plugin-X.Y.Z.zip` | 完整 Codex Plugin 离线包 | Codex CLI、Python 3.9+ |
| `ocean_watch-X.Y.Z-py3-none-any.whl` | 独立 `ocean-watch` Python CLI | Python 3.9+ |
| `ocean_watch-X.Y.Z.tar.gz` | Python 源码分发包 | Python 构建工具 |
| `SHA256SUMS` | 所有发布文件的 SHA-256 | 校验工具 |

GitHub Release 还会记录 build provenance attestation。Plugin ZIP 和 wheel 都不是原生独立程序；它们不会消除 Python 运行时要求。

Release 页面正文由工作流从 `CHANGELOG.md` 精确提取 `## X.Y.Z - YYYY-MM-DD` 段落生成。对应版本段落不存在、内容为空或 `Unreleased` 尚有未归档内容时，发布会直接失败。

## 验证下载文件

macOS/Linux：

```bash
shasum -a 256 -c SHA256SUMS
```

Linux 也可以使用：

```bash
sha256sum -c SHA256SUMS
```

Windows PowerShell 可查看单个文件：

```powershell
Get-FileHash .\ocean-watch-plugin-X.Y.Z.zip -Algorithm SHA256
```

将输出与 `SHA256SUMS` 中同名文件比较。已安装 GitHub CLI 时还可验证来源证明：

```bash
gh attestation verify ocean-watch-plugin-X.Y.Z.zip --repo westng/ocean-watch
```

## Plugin 离线安装

1. 下载 Plugin ZIP 和 `SHA256SUMS`。
2. 校验通过后解压 ZIP。
3. 将解压根目录注册为本地 Marketplace。
4. 安装 Plugin 并新建 Codex 任务。

```bash
codex plugin marketplace add /ABSOLUTE/PATH/ocean-watch-X.Y.Z
codex plugin add ocean-watch@ocean-watch
```

离线包内的 `.agents/plugins/marketplace.json` 使用相对源路径，整个解压目录可以移动，但移动后需重新注册 Marketplace。

## CLI 安装

```bash
python3 -m pip install ./ocean_watch-X.Y.Z-py3-none-any.whl
ocean-watch --version
```

CLI 安装不会自动把 Codex Plugin 注册到 Marketplace，两种分发形式互相独立。

## 本地构建验证

```bash
python3 -m pip install -e ".[dev]"
python3 scripts/release.py check
python3 -m build
python3 scripts/release.py plugin --output-dir dist
python3 scripts/release.py checksums --directory dist
python3 scripts/release.py verify-checksums --file dist/SHA256SUMS
```

Plugin 包只从 Git 已跟踪的白名单路径构建。脚本会拒绝符号链接、路径穿越、多根归档、未声明文件、校验和不一致以及 `config/`、`runs/`、缓存或构建目录。

## 维护者发版

1. 将 `CHANGELOG.md` 的 `Unreleased` 内容整理到 `## X.Y.Z - YYYY-MM-DD`，并保留空的 `## Unreleased` 段落。
2. 同步更新 `pyproject.toml` 的 `project.version`、`ocean_watch.__version__` 和 `.codex-plugin/plugin.json` 的基础版本。Plugin 开发期 cachebuster 可以作为 `+codex.*` 构建元数据保留。
3. 运行全部本地质量检查和发布构建。
4. 提交版本改动并等待 `CI` 成功。
5. 在 GitHub Actions 打开 `Release`，选择 `main`，点击 **Run workflow** 并输入 `vX.Y.Z`。

```bash
python3 scripts/release.py check --tag vX.Y.Z
python3 scripts/release.py notes --tag vX.Y.Z --output release-notes/RELEASE_NOTES.md
gh workflow run release.yml --repo westng/ocean-watch --ref main -f release_tag=vX.Y.Z
```

`Release` 不会被普通推送或 Tag 自动触发。工作流会重新执行编译、Ruff、Bandit 和全部测试，然后在全新 Ubuntu 环境构建、验证、证明并发布产物。手动运行必须选择 `main`；版本、输入 Tag 或 Changelog 任一不一致都会在创建 GitHub Release 前失败。同名 Tag 不存在时由工作流创建；已存在时必须指向当前 `main` 提交。重跑同一版本时，资产和 Release 页面的版本说明都会同步更新。构建任务只有仓库只读权限，单独的发布任务不执行仓库代码，只下载已验证产物与版本说明、重新校验 `SHA256SUMS` 并写入 GitHub Release。
