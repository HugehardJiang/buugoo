# 配置

完整的配置项与注释见 `configs/config.example.yaml`（`buugoo init` 生成的就是它）。

## 加载顺序

默认值 → 配置文件 → 网页管理后台保存的 `<data_dir>/settings.yaml` → 环境变量（优先级依次升高）。

配置文件按以下顺序查找：`--config` → `BUUGOO_CONFIG` → `./config.yaml`
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

## 文件上传

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `uploads.dir` | `<data_dir>/uploads` | 上传文件的存放目录，按用户分目录。请与数据库一起备份。 |
| `uploads.max_file_mb` | 20 | 单个文件上限 |
| `uploads.max_files` | 8 | 一条消息最多附带的文件数 |
| `uploads.user_quota_mb` | 500 | 每个用户的上传总量上限，0 不限。删除对话时其中的文件一并删除 |
| `uploads.doc_chars` | 20000 | 一个文档最多放进对话的字数，超出部分截断 |

图片按文件内容识别（JPG、PNG、GIF、WebP）。文档支持 PDF（扫描件提取不到文字，请截图后作为图片发送）、
Word `.docx`、以及 UTF-8 / GBK 编码的纯文本文件。上传后 24 小时内没有发送的文件会被自动清理。

如果前面有 Nginx（宝塔），请把 `client_max_body_size` 调到不小于 `max_file_mb`，例如 `client_max_body_size 25m;`。

## 工具

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `tools.python.runner` | `docker` | `docker`：一次性容器（断网、只读、非 root、限资源），所有用户可用；`local`：本机 python3，multi 模式下仅管理员；`off`：关闭 |
| `tools.python.image` | `python:3.12-slim` | docker 方式使用的镜像，可换成装好 numpy 等库的镜像 |
| `tools.python.docker` | `docker` | docker 命令 |
| `tools.python.python` | `python3` | local 方式使用的解释器 |
| `tools.python.timeout` | `30s` | 单次运行时间上限 |
| `tools.python.memory_mb` | 256 | 单次运行内存上限 |
| `tools.python.packages` | numpy, scipy, sympy, pandas, mpmath | 预装的库，启动时在后台安装（已经装好的不重复安装） |
| `tools.python.allow_install` | true | 允许布谷按需安装 PyPI 上的库：只装预编译包，只有安装时联网；multi 模式下仅管理员的对话 |
| `tools.python.index_url` | 空（官方 PyPI） | pip 软件源，国内服务器建议 `https://pypi.tuna.tsinghua.edu.cn/simple` |
| `tools.python.lib_dir` | `<data_dir>/pylib` | 第三方库目录，所有人共用；运行代码时只读挂载 |
| `tools.python.lib_max_mb` | 2048 | 库目录大小上限，超过后不再安装 |
| `tools.python.install_timeout` | `5m` | 一次安装的时间上限 |

联网搜索与读取网页使用 DeepSeek 的 Search / Fetch 接口（各 5 元 / 千次），只在当前对话的模型是 DeepSeek（`api.deepseek.com`）
时提供，费用记在该模型的 Key 上。模型需要勾选「支持工具调用」（配置文件中为 `llm.chat.tools`）。

## 情绪

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `emotion.enabled` | true | 关闭后布谷没有情绪起伏 |
| `emotion.expressiveness` | 1 | 情绪起伏的整体幅度 |
| `emotion.temper` | 1 | 易怒程度 |
| `emotion.patience` | 1 | 耐心，越大越不容易不耐烦 |
| `emotion.forgiveness` | 1 | 宽容，越大消气越快 |
| `emotion.empathy` | 1 | 共情，越大越容易被对方的情绪感染 |

## 管理后台

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `admin.password` | `1228` | 网页管理后台的密码，只能在配置文件或环境变量 `BUUGOO_ADMIN__PASSWORD` 中修改。**请务必改掉默认值。** |

管理后台保存的设置写在 `<data_dir>/settings.yaml`（权限 0600，其中可能有 API Key），重启服务后生效。
不能在网页上修改的：`admin.password`、`security.encryption_key`、`data_dir`。
