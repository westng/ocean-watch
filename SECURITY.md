# Security

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

`ocean-watch` 调用巨量引擎官方 API，可能接触广告账户、素材、投放链接和 OAuth 凭据。请把本仓库当作公开仓库维护，不要提交任何真实密钥或运行产物。

## 不要提交

- `config/ads-plan-monitor/config.json`
- `runs/`
- `.venv/`
- `__pycache__/`
- `*.pyc`
- `*.log`
- `*.tmp`
- OAuth token、refresh token、app secret、auth code
- API 请求/响应中包含账户、素材、投放链接、转化链路的调试文件

这些路径已经在 `.gitignore` 中排除。提交前仍建议运行一次：

```bash
git status --short --ignored
```

## 凭据存储

OAuth App ID、Secret、Access Token、Refresh Token 和官方 MCP `developer_id` 不应写入项目配置。脚本会通过 `skills/ads-plan-monitor/scripts/credential_store.py` 存储在本机凭据仓库：

- macOS: Keychain
- Windows: DPAPI 保护的用户本地文件
- Linux: Secret Service (`secret-tool`)

缺少安全凭据后端时，脚本默认拒绝保存，不会静默写入明文。只有显式设置 `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1` 才会启用 `~/.codex/ads-plan-monitor/credentials.json`；该文件是明文，只适合受限开发环境。请确保它不在仓库目录内，并限制文件权限。

官方 MCP 的动态 URL 同时包含 `app_id` 和 `developer_id`，因此不会写入 `.codex-plugin/plugin.json`、Codex 配置或其他持久化文件。Codex 只注册本地 SSE→stdio 桥接脚本；动态 URL 由桥进程读取系统凭据后在内存中生成，状态输出不会打印 URL 或标识符。

## 泄露处理

如果你不小心提交了真实 token、secret、auth code 或 MCP 动态 URL：

1. 立即在巨量引擎开放平台撤销或轮换对应凭据。
2. 删除本地和远端历史中的敏感内容。
3. 重新授权并更新本机凭据仓库。
4. 检查 `runs/` 和调试输出，确认没有再次写出敏感字段。

如果只是提交了广告主 ID、商品 ID、投放链接等业务信息，请按团队内部信息分级决定是否重写历史。

## 报告问题

请不要在公开 issue 中粘贴真实 token、secret、广告账户完整数据或投放链路。可以只描述复现步骤、错误码和已脱敏字段。
