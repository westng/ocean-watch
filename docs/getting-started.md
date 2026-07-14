# 快速开始

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## 1. 安装 Plugin

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

安装或升级后新建 Codex 任务，然后说“用 ads-plan-monitor 初始化配置”。Skill 会运行首次向导，不会在初始化阶段调用业务 API。

## 2. 初始化配置

从源码运行：

```bash
python3 skills/ads-plan-monitor/run.py setup init --home-config
```

可编辑安装后运行：

```bash
ocean-watch setup init --home-config
```

默认用户配置位于：

```text
~/.codex/ads-plan-monitor/config.json
```

开发仓库可以使用 `config/ads-plan-monitor/config.json`。该目录已被 Git 忽略。

## 3. 保存应用凭据

当前已实现渠道为 `marketing`：

```bash
ocean-watch auth set-app --channel marketing
```

命令交互式读取 App ID 和 Secret，并保存到操作系统凭据仓库。不要把密钥写入配置或聊天。

## 4. 完成 OAuth

```bash
ocean-watch auth authorize --channel marketing
```

本地回调地址必须与开放平台设置完全一致。默认值：

```text
http://127.0.0.1:8787/oauth/callback
```

营销和千川应用可以配置同一个回调地址。插件通过官方原样返回的 OAuth `state` 区分渠道：`AD.<随机值>` 表示巨量营销，`QC.<随机值>` 表示巨量千川，并校验完整随机值防止串号。

授权完成后，插件会同步官方授权主体，按角色展开真实广告主，并保存非敏感账户索引。

## 5. 检查就绪状态

```bash
ocean-watch auth status --channel marketing
ocean-watch setup validate --mode query
```

查询就绪后即可查询素材和报表。创建计划还需要一个完整、已激活且绑定目标广告主的业务模板。

## 6. 创建业务模板

```bash
ocean-watch templates create
```

向导会要求：

1. 选择默认模板骨架或已有业务模板作为来源。
2. 明确目标渠道、广告主、平台、商品和素材来源。
3. 按归属变化清理不能跨账户或跨商品复用的资产。
4. 预览逐字段差异和完整性校验。
5. 用户确认后才写入；激活需要单独确认。

默认模板只用于创建新模板，不能直接投放。

## 7. 先查询，再创建

查询当天上传视频：

```bash
ocean-watch materials videos --mode library-get --date today --fetch-all
```

预览一条上传素材计划：

```bash
ocean-watch plans create \
  --plan-template TEMPLATE \
  --video-id VIDEO_ID
```

确认后增加 `--submit`。达人素材使用 `materials creator` 和 `plans create-creator`，批量任务使用 `plans batch-upload` 或 `plans batch-creator`。

## 8. 查看投放数据

```bash
ocean-watch reports materials
```

默认结果输出到终端，由 Codex 在对话中整理为 Markdown 表格。只有显式传入 `--out` 或 `--csv-out` 才写文件。
