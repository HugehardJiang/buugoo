# 配置

完整的配置项与注释见 `configs/config.example.yaml`（`buugoo init` 生成的就是它）。

## 加载顺序

内置默认值 → 配置文件 → 环境变量。配置文件按以下顺序查找：`--config` → `BUUGOO_CONFIG` → `./config.yaml`
→ 用户配置目录 → 系统配置目录。找不到时只用默认值和环境变量（数据目录为用户数据目录）。

## 环境变量

前缀 `BUUGOO_`，层级之间用双下划线：

```bash
BUUGOO_LLM__CHAT__API_KEY=sk-...
BUUGOO_LLM__EMBEDDING__API_KEY=sk-...
BUUGOO_DATABASE__DRIVER=mysql
BUUGOO_DATABASE__DSN='buugoo:密码@tcp(127.0.0.1:3306)/buugoo?charset=utf8mb4&parseTime=True'
BUUGOO_AUTH__JWT_SECRET=...
BUUGOO_SERVER__CORS_ORIGINS=https://a.com,https://b.com
```

此外：`BUUGOO_CONFIG` 指定配置文件，`BUUGOO_MIRROR` 指定下载镜像，`BUUGOO_SERVER` 指定 `buugoo chat` 连接的服务。

## 密钥

API Key、JWT 密钥、数据库密码都不在代码中，也不应提交到仓库。`buugoo init` 生成的配置文件权限为 0600，
系统安装时为 `root:buugoo 0640`。生产环境推荐用环境变量传入密钥（systemd 可用 `EnvironmentFile=`，
Docker 用 `.env`）。

## 常用调整

- 换聊天模型：修改 `llm.chat`（任何 OpenAI 兼容服务）。记忆提取可单独用便宜的模型：`llm.utility`。
- 换向量模型：修改 `llm.embedding` 后执行 `buugoo reembed`。阿里云百炼：
  `base_url: https://dashscope.aliyuncs.com/compatible-mode/v1`、`model: text-embedding-v3`、`dim: 1024`。
- 检索参数：`memory.retrieval`（权重 α/β/γ、衰减、候选数、条数、相关度阈值）。
- 记忆门槛：`memory.write.importance_threshold`。
- 官网防止费用失控：`quota.daily_messages`、`quota.daily_tokens`。
