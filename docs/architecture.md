# 架构

## 总览

```
Web 前端（Vue 3，内嵌）─┐
buugoo chat（终端）──────┼── HTTP API /api/v1 ── 核心服务（buugoo serve）── LLM API（OpenAI 兼容）
以后的 App ─────────────┘                          │
                                                   ├── 数据库（SQLite / MySQL，GORM）
                                                   ├── 内存向量检索（VectorStore）
                                                   └── 后台调度器（定时任务）
```

同一个可执行文件通过子命令提供全部功能：`serve`、`chat`、`migrate`、`init`、`version`、
`update`、`uninstall`，以及 `reembed`（重新计算向量）和 `admin`（用户与邀请码）。

## 包结构与依赖方向

```
cmd/buugoo            入口
internal/cli          子命令（cobra）
internal/app          组装所有组件、管理生命周期
internal/httpapi      HTTP 层（Echo）：路由、鉴权中间件、SSE、错误映射、内嵌前端
internal/chat         一轮对话的编排
internal/memory       记忆：提取、打分、检索、写入、管理、第二阶段接口
internal/prompt       提示词组装
internal/persona      人格档案
internal/auth         鉴权、注册、邀请码
internal/quota        每日用量限额
internal/scheduler    定时任务框架
internal/llm          LLMProvider / EmbeddingProvider 接口（openai、anthropic 预留、provider 工厂）
internal/vector       向量编解码与 VectorStore 接口（memstore 实现）
internal/store        数据模型与 Repository 接口（gormstore 实现与迁移）
internal/selfupdate   自更新、备份、回滚
internal/service      systemd 管理
internal/client, tui  终端聊天的 API 客户端与界面
internal/config, paths, logx, buildinfo, timefmt  基础设施
```

依赖只从上往下：`httpapi → chat / memory / auth → store / llm / vector / prompt / persona`。
领域包只依赖接口；具体实现（gormstore、openai、memstore）在 `app` 中组装。

## 关键决策

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| HTTP 框架 | Echo v4 | handler 返回 error，由统一的错误处理器映射为 JSON，不容易遗漏错误 |
| 配置 | koanf，YAML + 环境变量（`BUUGOO_` 前缀，双下划线分层） | 依赖少，覆盖顺序明确；密钥可以只放在环境变量里 |
| 数据库 | GORM + glebarez/sqlite（纯 Go）+ MySQL | 无 CGO，可交叉编译成单个可执行文件 |
| 迁移 | gormigrate 版本化迁移，每个迁移使用自己的快照结构体 | 可追溯；两种数据库共用一份代码 |
| 时间 | 数据库统一存 UTC；"每天"按配置的 timezone 计算 | 避免 MySQL 与 SQLite 时区语义不同 |
| 向量 | 不依赖数据库的向量功能，float32 存 BLOB，内存中暴力计算 | MySQL 社区版不支持向量距离；见 vector-store.md |
| 记忆写入 | 回复完成后异步进行 | 不拖慢回复；失败只记日志 |
| 对话形态 | 每个用户一条连续对话，表结构保留多会话 | 更像和朋友聊天；以后可扩展 |
| 前端 | Vue 3 + Vite，构建产物 go:embed | 单文件部署；未构建时显示提示页 |
| 视觉 | 设计系统「布谷 BUUGOO」 | 见 design.md |

## 一轮对话

1. 检查每日限额。
2. 读取主对话的最近消息，检索相关记忆（检索失败时降级为不使用记忆）。
3. 组装提示词。
4. 保存用户消息与助手消息占位，打开模型的流式连接（失败直接返回错误，不开始推送）。
5. 通过 SSE 推送 `start`、`memories`、`delta`……`done`。
6. 保存回复（完成 / 中断 / 错误），记录用量、记忆使用，异步提交记忆写入。
