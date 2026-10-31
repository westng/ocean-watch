# 千川混合商品作品建计划流程

同一批作品可以属于不同商品和广告账户。入口先用当前启用的商品模板确定每行的商品、广告主和模板，再分成独立子批次预检。识别失败的行进入待确认列表；成功的子批次各自保留预检 ID，确认后按 ID 分别提交。

## 处理流程

```mermaid
flowchart TD
    A[输入多条作品链接及逐行商品标注] --> B{有明确商品或模板吗}
    B -->|有| C[按商品、广告主、模板筛选本地启用模板]
    B -->|无| D[批量解析公开作品的商品提示]
    D --> E{恰好一个有效商品吗}
    E -->|是| C
    E -->|无商品、多个商品或解析失败| P[该行待确认，保留原始行号]
    C --> F{唯一匹配一个模板吗}
    F -->|否| P
    F -->|是| G[按广告主和模板拆分子批次]
    G --> H[分别准备账户凭据]
    H --> I{具备达人身份提示吗}
    I -->|F2 或有效缓存| J[官方定向核验授权、作品归属和目标商品]
    I -->|已补准确抖音号| K[官方精确匹配，取得唯一数字 UID]
    K --> J
    I -->|缺失| Q[该行跳过，提示补充信息]
    J --> L[查询当日计划，判断新建或追加]
    L --> M[各子批次输出五列表格、预检 ID 和有效期]
    M --> N[确认各账户的具体预览]
    N --> O[按子批次预检 ID 串行提交并核对结果]
    H -->|授权失败| R[记录该账户失败，继续其他子批次]
```

这一步路由只做预检，不创建线上计划。公开商品提示和用户补充的达人信息都不能证明可投放；官方核验通过后才生成可提交快照。

## 逐行输入

普通对话调用 MCP `preflight_qianchuan_works`。`items` 为 1–100 行，`concurrency` 为 1–10，默认 8。混合商品或账户时省略全局 `plan_template`；同一模板的旧调用仍可使用全局字段。

| 字段 | 作用 |
| --- | --- |
| `work_url` | 必填，原始作品链接或分享文本 |
| `product_id` | 指定目标商品，必须属于所选模板；官方核验也只接受该商品 |
| `product_name` | 与模板的商品全称或简称精确匹配，不按标题模糊猜测 |
| `plan_template` | 指定该行模板，优先于全局模板 |
| `advertiser_id` | 限定该行广告主，必须与模板绑定一致 |
| `auth_account_id` | 可选授权选择器；同一子批次有冲突时全部待确认 |
| `creator_douyin_id` | 用户补充的准确可见抖音号，用于官方定向查询 |
| `creator_uid` | 可选数字 UID，提供时也必须提供可见抖音号 |
| `creator_name` | 用户补充的计划命名昵称，仅用于命名 |
| `plan_type` / `business` | 该行计划类型、商务，缺省时不加对应命名片段 |

商品 ID、广告主 ID、达人 UID 均使用字符串。多个筛选字段同时提供时必须同时满足。没有显式商品或模板时，公开元数据必须给出恰好一个不同的有效商品 ID；多个商品不自动取第一个。同一商品匹配多个账户或多个模板时，也不自动选第一个。

模板含多个商品时，新建计划的 `product_ids` 与素材商品列表只包含该分组已经官方核验的目标商品，避免把模板里的其他商品带入本次计划。

路由复用既有模板的商品与账户绑定，无需另建映射表。只有唯一匹配的行才会准备该广告主的凭据或发起官方查询；不会为了寻找可用账户逐个试投。

CLI 开发与诊断可使用重复 `--work-item` JSON 参数；以下名称和 ID 均为示例，需要替换为实际配置：

```bash
skills/qc-plan-monitor/run plans batch-qianchuan-works \
  --work-item '{"work_url":"https://www.douyin.com/video/10001","product_name":"商品A","advertiser_id":"20001","creator_douyin_id":"creator_a","creator_name":"达人甲"}' \
  --work-item '{"work_url":"https://www.douyin.com/video/10002","product_name":"商品B","advertiser_id":"20002","creator_douyin_id":"creator_b","creator_name":"达人乙"}'
```

`--work-item` 不能与 `--work-url`、`--plan-type`、`--business` 混用。带路由输入的直接 `--submit` 会被拒绝；先获取预览和子批次快照。

## 结果与提交

混合路由结果带 `routed=true`，包含：

- `batches[]`：每项有广告主/模板摘要、原始零起始 `input_indexes`、状态和独立预检结果。MCP 位于 `preflight`，CLI 位于 `result`；可提交的子结果带自己的 `preflight_id`、`expires_at` 和 `presentation`。
- `pending[]`：原始 `input_index`、稳定原因码和候选模板/账户。补充选择后只重跑这些行。
- 子批次失败：MCP 返回脱敏 `failure.error`，保留授权恢复码；不会丢弃其他子批次已完成的快照。恢复后只重跑失败子批次的原始行。

每个子批次先标明广告主和模板，再原样展示 `presentation.rendered_markdown`，固定为 `计划ID｜达人昵称｜商品ID｜素材ID｜素材标题`。跳过、待确认和失败原因放在表外。没有跨账户统一提交 ID。

用户确认具体账户、模板和预览后，逐个提交对应快照：

```bash
skills/qc-plan-monitor/run plans batch-qianchuan-works \
  --submit --preflight-id CONFIRMED_CHILD_PREFLIGHT_ID
```

快照最长有效 30 分钟且不跨上海业务日。提交重用已经核验的作品，不再抓取链接或调用 F2；重新核对模板、当日计划和素材差异，在对应广告主锁下串行写入。单个子批次失败不会回滚其他账户已经完成的线上操作，重试时应保留并核对已有结果。

## 解析失败时的处理

`f2_metadata_query_failed` 仅说明公开元数据查询失败，不能据此认定为反爬拦截、视频不可播放或作品未授权。包装层另区分空响应、身份不完整、HTTP 错误、网络错误、超时及输出解析错误，不输出 Cookie 或原始异常。

已有明确商品/模板且用户补充准确 `creator_douyin_id`、命名 `creator_name` 时，可以绕过 F2 身份查询。Core 必须在该广告主下完整定向查询并取得唯一精确匹配的数字 UID，再核验授权、作品归属和商品。仅有昵称不能继续；查询不完整时授权状态仍未知。
