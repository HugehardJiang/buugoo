# 向量检索

MySQL 社区版不支持向量距离计算，因此第一阶段不依赖数据库的向量功能。

- 向量写入前归一化，以 float32 小端序存在 `memories.embedding`（BLOB）。归一化后余弦相似度等于点积。
- `vector.Store` 接口：`Search(user, model, query, k)`、`Upsert`、`Delete`、`Invalidate`。向量按 `(user_id, model)` 隔离。
- 当前实现 `memstore`：检索时按 `(user_id, model)` 从数据库加载该用户的全部向量到内存，用小顶堆取 Top-K。
  - LRU 缓存，按用户数（`memory.cache.max_users`）、总字节数（`max_bytes`）和空闲时间（`ttl`）淘汰。
  - 同一用户的并发加载只执行一次；写入和删除直接更新已缓存的条目。
  - 维度不一致或无法解码的向量会被跳过并记录告警。
- 持久化由 `MemoryRepo` 负责，`memstore` 的 `Upsert/Delete` 只维护缓存。

## 容量估算

bge-m3 为 1024 维，每条约 4KB。一个用户 1 万条记忆约 40MB，暴力计算一次在毫秒级。
默认缓存上限 512MB。用户量和记忆量大幅增长后，可以实现一个基于专门向量库（如 Qdrant、pgvector）的 `vector.Store`，
上层代码不需要改动。
