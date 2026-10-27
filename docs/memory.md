# 记忆系统

## 数据（memories 表）

| 字段 | 说明 |
| --- | --- |
| `type` | `episodic` 情景记忆（具体事件）/ `semantic` 语义记忆（稳定的事实） |
| `content` | 记忆文本，以"用户"为主语，带具体日期 |
| `embedding` / `embedding_model` / `embedding_dim` | 归一化后的 float32 向量、生成它的模型与维度 |
| `importance` | 1–10，写入时由 LLM 打分 |
| `emotion` | 0–1 情绪强度 |
| `strength` | 记忆强度，初始 1，每次被使用 `+access_boost` |
| `source` | `stated` 用户亲口说的 / `inferred` AI 推断的（必填，没有默认值） |
| `source_message_id`、`conversation_id` | 溯源 |
| `last_accessed_at`、`access_count` | 访问记录 |
| `superseded_by`、`consolidated_at`、`metadata` | 第二阶段预留 |
| `deleted_at` | 软删除 |

用户删除记忆时**立即清空** `content`、`embedding`、`embedding_model` 与 `metadata`，只保留 ID、
时间戳和删除标记，不提供恢复。对话记录本身不会因此被删除。

`memory_usages` 记录每条回复用了哪些记忆和得分，用于调试"它为什么提起这件事"，第二阶段巩固时也会用到。

## 写入

每轮对话完成后异步执行（`memory.Writer`，有界队列，同一用户串行）：

1. 用 utility 模型（默认与聊天模型相同，可换成便宜的）以 JSON 模式提取候选记忆；提示词带上当前时间、
   最近几条对话和已知的相关记忆（避免重复）。
2. 严格区分来源：只有用户在本轮亲口说的才是 `stated`；模型给出的来源不明确时一律按 `inferred` 处理，宁可低估。
3. 丢弃 `importance < importance_threshold` 的候选。
4. 向量化并归一化；与已有记忆相似度 ≥ `dedup_similarity` 的视为重复，跳过。
5. 入库并更新向量缓存。提取消耗的 token 计入该用户的每日用量。

## 记忆压缩

记忆越攒越多，提取和检索的开销也跟着涨，而且其中有不少是重复、相近的。`memory.Compactor` 负责压缩：

1. **合并相近的**：向量相似度 ≥ `memory.compact.similarity` 的同类记忆归成一组（每组最多 10 条），
   请 utility 模型合成一条：保留人名、数字、日期等细节，冲突以新的为准，亲口说的和推测的不混在一起。
2. **概括零碎的**（只在自动压缩时）：还超过上限时，挑最不重要、最久没用过的记忆，按类型每 20 条请模型概括成几条，
   直到压到上限的七成左右。

合并出的新记忆重要性、情绪、强度取最大，时间取最新（检索时的新近度不变），`metadata.merged_from` 记下合并自哪些；
被合并的旧记忆删除。触发方式：

- 自动：写入新记忆后，这个人的记忆超过 `memory.compact.max_memories`（默认 300，0 关闭）条时在后台压缩；
- 手动：设置页「大脑皮层 → 整理记忆」（`POST /api/v1/memories/compact`），只做第一步。

消耗的 token 计入该用户的每日用量。

## 检索打分

```
score = α·recency + β·importance + γ·relevance
recency    = decay ^ (距 last_accessed_at 的小时数；没有访问过则从 created_at 算起)
importance = importance / 10
relevance  = max(0, 余弦相似度)
```

流程：按相似度取 `candidate_k` 条候选 → 去掉相似度低于 `min_relevance` 的 → 读取元数据并打分 →
取前 `top_k` 条 → 按提示词预算放入。被放进提示词的记忆更新 `last_accessed_at`、`access_count` 并增强 `strength`。

先按相关度筛选再加权的原因：完全无关但很新或很重要的记忆不应被提起。

## 更换向量模型

检索时只比较 `embedding_model` 与当前配置一致的向量。更换模型后运行 `buugoo reembed`
（默认只处理旧模型的向量，`--all` 全部重算，可中断后继续）。建议先停服务再执行。

## 第二阶段扩展点

- `memory.Forgetter`：遗忘曲线 `R = exp(-t / (S·(1 + k·E)))`，t 为距上次访问的时间，S 为 strength，E 为 emotion。
- `memory.Consolidator`：每日"睡眠"巩固（`memory.ConsolidationJob` 已注册在调度器上，目前是空任务）。
- 冲突更新：新记忆取代旧记忆时写 `superseded_by`，检索已经会跳过被取代的记忆。
