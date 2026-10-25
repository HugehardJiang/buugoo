# 路线图与扩展点

## 第二阶段

| 功能 | 预留 |
| --- | --- |
| 遗忘曲线 `R = exp(-t / (S·(1 + k·E)))` | `memories.strength`、`emotion`、`last_accessed_at`；`memory.Forgetter` 接口 |
| 每日"睡眠"巩固 | `memory.ConsolidationJob` 已按 `scheduler.consolidation_spec` 注册；`memory.Consolidator` 接口；`consolidated_at`；`memory_usages` |
| 记忆冲突更新 | `superseded_by`（检索已跳过被取代的记忆） |
| 日程管理 | 新的领域包与表，通过新的迁移添加；作为布谷的一项能力接入提示词 |

## 第三阶段

- 情绪状态机：二维效价-唤醒度离散状态空间 `x[k+1] = A·x[k] + B·u[k]`，随时间回归基线。
  计划新增 `emotion_states` 表与 `internal/emotion` 包，由 `prompt.Input` 增加一段"当前心情"。
- 关系参数（熟悉度、信任度）：新增 `relationships` 表，按用户存储，同样作为提示词的一段。

## 第四阶段

- 内在生活（后台自主思考）与主动消息：基于 `scheduler` 注册新任务；消息通过新的投递通道发送。
- 外部渠道（QQ、微信）：作为 HTTP API 的另一个客户端接入，复用 `chat.Service`。

## 技术扩展点

- `llm.ChatProvider`：Anthropic 实现（`internal/llm/anthropic` 已预留）。
- `vector.Store`：专门的向量数据库实现。
- 多会话：`conversations` 表已支持，`ConversationRepo.Primary` 之外增加列表与切换即可。
