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

- 换聊天模型：推荐在网页「设置 → 模型」中添加（可以添加多个，每段对话单独选择）。也可以修改 `llm.chat`，
  它会作为「配置文件」模型出现在列表里。`llm.chat.thinking_style` 决定思考等级的接法：
  `none` | `reasoning_effort` | `enable_thinking` | `model_switch`（配合 `thinking_model`，例如 `deepseek-reasoner`）。
- 记忆提取跟随当前对话使用的模型；`llm.utility` 只在没有对话模型时作为后备。
- 换向量模型：管理员在网页「设置 → 向量模型」中修改并点「重新计算」；网页中的设置优先于 `llm.embedding`。
  也可以修改 `llm.embedding` 后执行 `buugoo reembed`（网页中保存过设置时，命令行同样使用网页中的设置）。阿里云百炼：
  `base_url: https://dashscope.aliyuncs.com/compatible-mode/v1`、`model: text-embedding-v3`、`dim: 1024`。
- 检索参数：`memory.retrieval`（权重 α/β/γ、衰减、候选数、条数、相关度阈值）。
- 记忆门槛：`memory.write.importance_threshold`。
- 官网防止费用失控：`quota.daily_messages`、`quota.daily_tokens`。

## 加密与安全

| 配置 | 说明 |
| --- | --- |
| `security.encryption_key` | 64 位十六进制（32 字节）。加密网页中保存的模型 API Key（AES-256-GCM）。留空时自动生成 `<data_dir>/secret.key`。丢失后已保存的 Key 无法解密，需要在网页中重新填写。可用环境变量 `BUUGOO_SECURITY__ENCRYPTION_KEY` 提供。 |
| `security.allow_private_model_urls` | 默认 `false`。multi 模式下普通用户添加的模型只能连接公网地址，防止借模型地址探测服务器内网（SSRF）；这类连接不走 HTTP 代理。管理员与 single 模式不受限制。 |
