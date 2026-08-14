# 配置与授权

## 配置位置

读取顺序：

1. `--config PATH`
2. `ADS_PLAN_MONITOR_CONFIG`
3. 从当前目录向上找到 Plugin 根后读取 `config/ads-plan-monitor/config.json`
4. `$CODEX_HOME/ads-plan-monitor/config.json`

`CODEX_HOME` 未设置时默认为 `~/.codex`。推荐普通用户运行 `setup init --home-config`；仓库内配置仅供本地开发且已被 Git 忽略。

配置存储渠道端点、OAuth 回调、负责账户和业务模板，不存 App Secret、Access Token 或 Refresh Token。修改后运行：

```bash
skills/ads-plan-monitor/run setup validate --mode query
skills/ads-plan-monitor/run setup validate --mode create-preview
skills/ads-plan-monitor/run setup validate --mode create-submit
```

## 凭据

- macOS：Keychain
- Windows：DPAPI 用户本地保护文件
- Linux：Secret Service（`secret-tool`）

没有安全后端时默认拒绝写入。仅限受控开发环境可显式设置 `ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK=1`；生成的明文文件必须位于工作树外。

营销与千川使用独立的 App、OAuth state、Token 和广告主映射。业务命令按 `channel + advertiser_id` 解析精确授权，不跨渠道或跨用户猜测 Token。多份授权覆盖同一广告主时必须指定可验证的授权身份。

## OAuth 与广告主快照

统一回调地址：

```text
http://127.0.0.1:8787/oauth/callback
```

营销 state 使用 `AD.<nonce>`，千川使用 `QC.<nonce>`。必须同时校验完整 state 和渠道，成功交换 Token 后先保存待同步授权。只有官方角色扩展、完整分页、去重和广告主验证全部成功时，才原子替换该授权的广告主快照；异常或部分发现保留旧快照。

```bash
skills/ads-plan-monitor/run auth status --channel marketing
skills/ads-plan-monitor/run auth sync-accounts --channel marketing --authorization-id AUTHORIZATION_ID
skills/qc-plan-monitor/run auth mappings --channel qianchuan --advertiser-id ADVERTISER_ID
```

`auth sync-accounts` 刷新 OAuth 授权覆盖，不修改 `managed_accounts`。`accounts list` 读取的是用户维护的负责账户簿，两者不能互相替代。

## 模板

默认模板是创建骨架，不可直接提交。业务模板必须绑定渠道和广告主；营销模板还绑定商品、素材模式和投放设置，千川商品模板绑定商品全称、商品简称和 1–30 个商品 ID，直播模板绑定直播账号数字 UID。

素材、作品、封面和运行时 ID 不应固化到模板。模板删除默认 dry-run，并要求 `--submit`；被引用的模板只有在展示诊断并明确接受后才能使用受保护的强制删除。

Plugin 的 `list_templates` 与 `get_template` MCP 工具只读取当前用户的 `$CODEX_HOME/ads-plan-monitor/config.json`。它们不接受 `--config`、`ADS_PLAN_MONITOR_CONFIG`、任意路径或环境覆盖，不解析仓库内开发配置，不读取凭据，也不调用官方 API。Unix 上该目录权限必须不宽于 `0700`、文件权限必须不宽于 `0600`；符号链接或路径逃逸会被拒绝。

`list_templates` 返回可继续传给 `get_template` 的字符串 `template_id`。详情查询必须同时传入精确渠道与 ID；千川显示名不能代替模板 ID。分页游标绑定本地状态版本，状态变化后应丢弃旧游标并从第一页重查，不能拼接不同版本的结果。

## 本地状态

`$CODEX_HOME/ads-plan-monitor/state/` 保存授权快照、限流控制、作品身份提示缓存和 Plugin 执行记录。它们不属于开源仓库，不应复制到其他用户环境。

30 天作品身份缓存只保存非敏感的作品 ID、可见抖音号与数字达人 UID 关系，用作下一次官方定向查询提示。缓存不证明授权、归属或商品匹配，过期或不匹配时快速跳过。

## F2

Go CLI 自动发现 Python；可用 `OCEAN_WATCH_PYTHON` 指定解释器。只在开发测试中才使用 `OCEAN_WATCH_F2_ENTRYPOINT` 覆盖包装脚本路径。

固定要求：

- Python `3.10+`
- F2 `0.0.1.7`
- `socksio>=1,<2`

可选 `OCEAN_WATCH_F2_DOUYIN_COOKIE` 只从当前进程环境读取，不写入命令、配置、输出或日志。未提供时使用 F2 自身访客初始化。F2 不下载媒体、不创建数据库、不自动读取浏览器 Cookie。
