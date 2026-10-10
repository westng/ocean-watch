# DeepSeek Harness 桌面版接入

Ocean Watch 使用 Harness 原生 npm 组合包格式。安装一份包即可加载 `ads-plan-monitor`、`qc-plan-monitor` 和 18 个本地 stdio MCP 工具；业务逻辑、授权与数据由现有 Go 运行时处理。

## 兼容与开发基准

- 当前适配 DeepSeek Harness 桌面应用及内置运行时 `0.2.0-rc.2`，peer 声明精确限定该版本，无需 `compatibility.json` 豁免。宿主升级后需要重新验证再更新声明。
- 本地开发、调试和验收统一使用已安装的桌面应用及 `desktop` profile，不安装或通过 `npx` 下载独立 npm/CLI Harness。插件自身仍使用 npm 打包，并复用桌面应用提供的 MCP 客户端和 Skill 提供方。
- 启动程序覆盖 macOS Apple Silicon / Intel、Linux arm64 / amd64、Windows amd64。2026-10-10 的桌面集成验收环境为 macOS Apple Silicon；其他桌面平台的完整交互尚未验收，平台路径单元测试不能代替该验收。
- 旧 npm/CLI `0.1.5-rc.3`、`0.1.7-rc.2` 与桌面版 `0.2.0-rc.1` 不再作为兼容目标或开发环境。

## 安装与更新

1. 打开 DeepSeek Harness 桌面应用，进入「插件 → 添加插件」。
2. 输入 Ocean Watch 仓库的绝对路径，安装到当前桌面应用的 `desktop` profile。也可在仓库根目录运行 `npm pack`，将生成的 `.tgz` 文件绝对路径用于安装。
3. 在插件页面确认 `ocean-watch` 已启用、组件正在运行，再新建会话。若界面提示需要重启，先结束当前任务，再完全退出并重新打开桌面应用。
4. 确认技能列表出现 `ads-plan-monitor` 和 `qc-plan-monitor`，工具采用 `mcp__ocean-watch__<工具名>`，例如 `mcp__ocean-watch__get_capabilities`。

更新已有安装时，重新从本地路径安装当前包。曾为旧包添加版本豁免的环境，应在新包安装成功后移除该旧豁免；当前包本身应通过正常的版本检查。`web` 与 `desktop` 是不同 profile，安装到 `web` 不会更新桌面版。

本地安装无需 npm 发布。只有远端分支包含当前适配文件后，才能使用对应 Git 地址安装。

## 状态与执行

各 Host 使用相同的本地状态规则：`OCEAN_WATCH_HOME` → `CODEX_HOME` → `~/.codex`。需要隔离时，在启动 Harness 前设置独立的 `OCEAN_WATCH_HOME`。

MCP 直接启动当前平台的内置 Go 二进制，将插件根目录作为独立参数传入，支持包含空格的安装路径。两项 Skill 从各自 `SKILL.md` 的绝对目录定位 `run` / `run.cmd`，不依赖会话工作目录。千川作品链接解析仍使用 Python 与 F2。

代理先响应协议探测与工具发现；首次业务调用再准备、校验并连接私有运行时，后续调用复用连接。首次调用可能较慢，校验失败会明确返回错误。

Skill 提供方使用独立名称 `ocean-watch`，只扫描包内 `skills/`，保留宿主已有技能且不重复扫描用户或项目默认目录。

## 打包与验收

`package.json` 声明 `dsh.bundle.patch`，`harness/cordis.patch.yml` 挂载原生插件，`harness/index.mjs` 注册宿主提供方，`harness/config.mjs` 负责平台与绝对路径选择。

```bash
npm test
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
npm run test:desktop
```

桌面应用不在默认位置时：

```bash
npm run test:desktop -- "/path/to/DeepSeek Harness.app"
```

桌面探测脚本目前支持 macOS。它调用已安装应用的 Electron Node 模式和内置 ASAR 运行时，对当前仓库执行 `npm pack` 并解包到含空格的临时 `desktop` profile；全程不下载独立 Harness，不打开桌面 UI 或修改真实 profile。探测不使用真实模型凭据或账户状态，隔离 `DSH_HOME`、`DSH_AGENTS_HOME`、`CODEX_HOME` 和 `OCEAN_WATCH_HOME`，验证无需豁免的版本准入、两项技能与宿主技能共存、18 个 MCP 工具、五渠道能力查询，以及卸载、重新挂载后的资源释放与可调用性。

CI 在三个操作系统运行路径测试与打包检查，不将这些检查声称为桌面集成验收。发布前应在目标桌面应用运行上述探测，并由使用者通过真实桌面插件管理完成安装与显示检查；该步骤不属于自动化代码测试。

打包必须保留 Go 运行时校验所需的各 Host 清单、运行时清单、五平台二进制、Skill、合同与 F2 包装器。修改包清单、技能或 Harness 入口后，需要刷新 `.codex-plugin/runtime-manifest.json` 中相应资源哈希；涉及运行时修改时仍使用 `go run ./cmd/build-runtime --all` 构建。

## 官方规范

- [第一个插件与依赖注入](https://deepseek-harness.github.io/deepseek-harness/develop/basic/)
- [组合包与安装](https://deepseek-harness.github.io/deepseek-harness/develop/basic/publish)
- [生命周期与 effect](https://deepseek-harness.github.io/deepseek-harness/develop/cordis-tutorial/02-lifecycle-and-effects)
