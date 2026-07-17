# Security

`ocean-watch` 调用巨量引擎官方 API，可能接触广告账户、素材、投放链接和 OAuth 凭据。请把本仓库当作公开仓库维护，不要提交任何真实密钥或运行产物。

## 不要提交

- `config/ads-plan-monitor/config.json`
- `runs/`
- `.venv/`
- `__pycache__/`
- `*.pyc`
- `*.log`
- `*.tmp`
- `$CODEX_HOME/ads-plan-monitor/` 下的配置、状态和回退凭据
- OAuth token、refresh token、app secret、auth code
- API 请求/响应中包含账户、素材、投放链接、转化链路的调试文件

仓库内的标准开发路径和已知回退凭据文件名已在 `.gitignore` 中排除，但用户自定义 `--out`、journal、响应文件或任意自定义 `CODEX_HOME` 无法由项目自动识别。自定义 `CODEX_HOME` 必须位于 Git 工作树外。提交前仍需运行：

```bash
git status --short --ignored
```

## 凭据存储

OAuth App ID、Secret、Access Token、Refresh Token 和官方 MCP `developer_id` 不应写入项目配置。`ocean-watch auth set-app` 和授权服务会将它们存储在本机凭据仓库：

- macOS: Keychain
- Windows: DPAPI 保护的用户本地文件
- Linux: Secret Service (`secret-tool`)

缺少安全凭据后端时，脚本默认拒绝保存，不会静默写入明文。只有显式设置 `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1` 才会在 `$CODEX_HOME/ads-plan-monitor/` 生成 `credentials.json`、`oceanengine-app-*.json` 和 `oceanengine-auth-*.json`；这些文件包含明文凭据，只适合受限开发环境。`CODEX_HOME` 未设置时默认为 `~/.codex`。请确保该目录不在仓库内，并限制文件权限。

官方 MCP 的动态 URL 同时包含 `app_id` 和 `developer_id`，因此不会写入 `.codex-plugin/plugin.json`、Codex 配置或其他持久化文件。Codex 只注册本地 SSE→stdio 桥接脚本；动态 URL 由桥进程读取系统凭据后在内存中生成，状态输出不会打印 URL 或标识符。

官方业务 API、OAuth 和 MCP 只接受巨量官方 HTTPS 主机，拒绝重定向并限制响应大小。不要通过配置把端点改到代理、调试回显服务或第三方域名；需要抓包时使用不记录请求头和正文的本地受控环境。

千川作品链接解析是可选的独立外部边界，默认未配置。真实地址只允许写入本机业务配置，不得写入仓库、Plugin 清单、Skill 或示例。启用后只发送用户提供的公开抖音链接，不发送广告主 ID、Token、Secret、模板内容或本地状态；响应限制为 1 MiB 且拒绝服务端重定向。非空商品 ID 未命中模板商品时会直接跳过，其他结果仍由巨量千川官方接口复核。使用 `--no-link-metadata-api` 可单次禁用，使用 `setup work-metadata --clear` 可从本机配置移除。

## 支持版本

安全修复只维护当前 `main` 和最新发布版本。旧版本发现漏洞时，请先升级；需要回溯修复时会在安全公告中明确标注。

## 泄露处理

如果你不小心提交了真实 token、secret、auth code 或 MCP 动态 URL：

1. 立即在巨量引擎开放平台撤销或轮换对应凭据。
2. 删除本地和远端历史中的敏感内容。
3. 重新授权并更新本机凭据仓库。
4. 检查 `runs/` 和调试输出，确认没有再次写出敏感字段。

如果只是提交了广告主 ID、商品 ID、投放链接等业务信息，请按团队内部信息分级决定是否重写历史。

## 报告问题

安全漏洞请使用 GitHub 的 [Private vulnerability reporting](https://github.com/westng/ocean-watch/security/advisories/new)。普通问题可以提交公开 issue，但不要粘贴真实 token、secret、广告账户完整数据或投放链路；只描述复现步骤、错误码和已脱敏字段。
