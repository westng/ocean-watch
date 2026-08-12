# Security

Ocean Watch 会接触广告账户、素材、投放链接和 OAuth 凭据。仓库按公开项目维护，不提交真实密钥、业务配置或运行产物。

## 不要提交

- `config/ads-plan-monitor/config.json`、`runs/`、日志和自定义输出
- `$CODEX_HOME/ads-plan-monitor/` 下的配置、授权快照、缓存、执行记录和开发回退凭据
- App Secret、Access Token、Refresh Token、授权码或 F2 Cookie
- 含真实账户、商品、作品、素材或转化链路的请求/响应

自定义 `CODEX_HOME` 必须位于 Git 工作树外。提交前检查：

```bash
git status --short --ignored
```

## 凭据

`auth set-app` 和 OAuth 服务把凭据保存到：macOS Keychain、Windows DPAPI 或 Linux Secret Service。没有安全后端时默认拒绝保存。只有受控开发环境可设置 `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1`；产生的明文文件必须位于工作树外且限制权限。

业务请求只访问固定的巨量引擎官方 HTTPS 主机，拒绝未经许可的端点和重定向，限制响应大小并脱敏错误。SDK 日志不会输出 Token 或请求正文。

## F2

千川公开作品元数据只通过固定 F2 `0.0.1.7` 的本机只读包装层解析。包装层不下载媒体、不创建数据库、不自动读取浏览器 Cookie。可选 `OCEAN_WATCH_F2_DOUYIN_COOKIE` 只存在于当前进程环境，不进入参数、配置、输出、日志或执行记录。

F2 结果是不可信的公开提示，不能证明达人授权、作品归属、商品匹配或可投放性；这些结论始终由目标广告主下的千川官方 API 定向复核。

## 发布完整性

Marketplace 使用已审查 Git Tag 的完整源码快照，其中包含五个平台的 Go CLI。Release 工作流从固定提交重建二进制并逐字节核对，不读取业务凭据、不调用真实广告接口、不修改文件或回推 `main`。既有 Tag 和 Release 不覆盖；修复通过新 patch 发布。

## 泄露处理

发现 Token、Secret、授权码或 Cookie 泄露时：

1. 立即在官方平台撤销或轮换凭据。
2. 删除本地与远端历史中的敏感内容。
3. 重新授权并检查本地执行记录与日志。
4. 必要时通过 GitHub 安全公告通知受影响用户。

安全漏洞请使用 GitHub [Private vulnerability reporting](https://github.com/westng/ocean-watch/security/advisories/new)。公开 issue 不得包含真实凭据或完整业务数据。
