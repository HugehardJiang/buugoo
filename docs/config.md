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
| `uploads.user_quota_mb` | 500 | 每个用户的文件空间（上传的文件 + 代码生成的图片），0 不限。用满后不能再上传；在「文件」页删除文件或删除对话即可腾出空间 |
| `uploads.doc_chars` | 20000 | 一个文档最多放进对话的字数，超出部分截断 |

图片按文件内容识别（JPG、PNG、GIF、WebP）。文档支持 PDF（扫描件提取不到文字，请截图后作为图片发送）、
Word `.docx`、以及 UTF-8 / GBK 编码的纯文本文件。上传后 24 小时内没有发送的文件会被自动清理。

用户可以在网页「文件」页查看、多选删除自己的文件，或一键删除 7 天前的全部文件。删除后文件从磁盘上删掉、
不再计入空间，聊天记录中原来的位置显示「文件已删除」；布谷之前对图片的文字描述仍会保留在对话上下文中。

如果前面有 Nginx（宝塔），请把 `client_max_body_size` 调到不小于 `max_file_mb`，例如 `client_max_body_size 25m;`。

## 人格与性格

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `persona.file` | 空 | 人格档案文件，留空使用内置的布谷；管理后台「人格」中保存的优先 |
| `persona.disabled_presets` | 空 | 关掉的预设性格：`gentle` 温柔治愈、`snarky` 毒舌损友、`strategist` 理性军师、`bubbly` 元气话痨、`alive` 人工智鸟 |

每个用户可以在「设置 → 性格」中选一种预设（见 [configs/persona.presets.yaml](../configs/persona.presets.yaml)）。
预设只替换人格档案中的自我认知、性格、说话风格和口头禅；名字、兴趣、价值观和必须遵守的原则仍然来自人格档案。
选「默认」即使用人格档案本身。被关掉的预设，已经选了的用户会改用默认。

## 工具

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `tools.python.runner` | `docker` | `docker`：一次性容器（断网、只读、非 root、限资源），所有用户可用；`local`：本机 python3，multi 模式下仅管理员；`off`：关闭 |
| `tools.python.image` | `python:3.12-slim` | docker 方式使用的镜像，可换成装好 numpy 等库的镜像 |
| `tools.python.docker` | `docker` | docker 命令 |
| `tools.python.python` | `python3` | local 方式使用的解释器 |
| `tools.python.timeout` | `30s` | 单次运行时间上限 |
| `tools.python.memory_mb` | 256 | 单次运行内存上限 |
| `tools.python.packages` | numpy, scipy, sympy, pandas, mpmath, matplotlib | 预装的库，启动时在后台安装（已经装好的不重复安装） |
| `tools.python.allow_install` | true | 允许布谷按需安装 PyPI 上的库：只装预编译包，只有安装时联网；multi 模式下仅管理员的对话 |
| `tools.python.index_url` | 空（官方 PyPI） | pip 软件源，国内服务器建议 `https://pypi.tuna.tsinghua.edu.cn/simple` |
| `tools.python.lib_dir` | `<data_dir>/pylib` | 第三方库目录，所有人共用；运行代码时只读挂载 |
| `tools.python.lib_max_mb` | 2048 | 库目录大小上限，超过后不再安装 |
| `tools.python.install_timeout` | `5m` | 一次安装的时间上限 |
| `tools.python.workspace_mb` | 50 | 每段对话工作目录（压缩后）的上限，0 表示不保留文件 |
| `tools.python.workspace_dir` | `<data_dir>/workspaces` | 对话工作目录的位置 |
| `tools.search.provider` | `auto` | `auto`：模型是 DeepSeek 时用 DeepSeek 搜索，否则用填了 Key 的博查或 Tavily；`deepseek` / `bocha` / `tavily`：只用指定的；`off`：关闭 |
| `tools.search.bocha_api_key` | 空 | [博查](https://open.bochaai.com) 的 Key，国内服务器推荐 |
| `tools.search.tavily_api_key` | 空 | [Tavily](https://app.tavily.com) 的 Key，每月有免费额度，也用于读取网页 |
| `tools.search.direct_fetch` | true | 搜索服务不能读网页时（如博查），由服务器直接下载网页正文（只能访问公网地址） |

联网搜索不再限定 DeepSeek：模型是 DeepSeek 时默认用它的 Search / Fetch 接口（各 5 元 / 千次，费用记在该模型的 Key 上），
其他模型用博查或 Tavily。模型需要勾选「支持工具调用」（配置文件中为 `llm.chat.tools`）。

运行代码时，当前目录是这段对话专属的工作目录：这段对话里上传过的文件在 `input/`（只读），代码保存的文件之后的运行还能用。
沙箱中的工作目录是有大小上限的内存盘，运行结束后有变化才打包交回保存，代码不能直接写服务器磁盘；
工作目录计入用户的文件空间，空间用满时不再保存改动，删除对话时一起删除。

## 上下文与长对话压缩

提示词的预算按**模型的上下文长度**计算，而不是按消息条数：摘要之后的消息全部原样放进提示词，
上下文长度减去给回复预留的部分（模型的最大输出，没设置时 4096）就是预算，超出时从最早的消息开始舍弃。

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `llm.chat.context_tokens` | 0 | 模型的上下文长度（token），0 表示自动识别：先问服务商的 `GET /models`，再查常见模型表，都没有时按 6.4 万。网页上以「10 万 token」为单位填写，例如 1.28 = 12.8 万 |
| `chat.summarize` | true | 长对话压缩：上下文快用满时，较早的消息由辅助模型整理成摘要；关掉后放不下的旧消息直接丢掉 |
| `chat.compact_at` | 80 | 一轮用掉的上下文超过上下文长度的这个百分比时，这条回复照常发出，**下一条消息处理前先压缩** |
| `chat.keep_recent` | 20 | 压缩时最近的消息原样保留，大约占上下文长度的这个百分比，更早的整理进摘要 |

- 网页「设置 → 模型」里添加的模型，在模型表单里单独填写上下文长度（同样留空自动识别）。
- 用户随时可以手动压缩：聊天页顶部的用量环点开后「立即压缩」，或者在微信、QQ、Telegram 里发「压缩」。
- 摘要保存在对话上（最多约 1500 字），每次在旧摘要的基础上更新；要整理的消息很多时分几次交给辅助模型。
  压缩失败不影响聊天，放不下的最早的消息会被舍弃，下一次再试。
- 工具调用（搜索、运行代码等）的输入和结果要点会随回复一起留在历史里，之后的对话还能用上当时的结果；压缩时只保留结论和关键数据。
- `chat.history_messages`、`chat.max_context_tokens` 已不再使用，旧配置文件里留着也没关系。

## 提醒与主动消息

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `proactive.reminders` | true | 布谷可以设提醒和定时任务（一次、每天、工作日、每周、每月、每隔一段时间、cron），到时间主动发消息 |
| `proactive.care` | true | 主动关心：辅助模型根据记忆、最近的聊天和心情判断要不要主动找对方、聊什么 |
| `proactive.quiet_start` / `quiet_end` | `23:00` / `08:00` | 免打扰时段（本地时间），这段时间不主动关心，提醒照常 |
| `proactive.idle_hours` | 20 | 用户多久没说话后才考虑主动关心 |
| `proactive.min_gap_hours` | 20 | 两次主动关心至少间隔 |
| `proactive.check_interval` | `30m` | 多久检查一次（同一个人至少 6 小时才判断一次） |

定时任务到时间后，布谷会按任务说明去做（可以联网搜索、读网页、运行代码，最多 10 分钟，同时最多执行 3 个），
再把结果作为一条主动消息发出来；额度用完或者没配置模型时跳过这一次。重复的间隔至少 30 分钟，
每人最多 50 个提醒、其中定时任务最多 20 个。时间按服务器时区（`timezone`）计算。

主动发的消息还没有回复时，3 天内不会再主动；用户可以在「定时任务」页关掉主动关心，或者新建、修改、暂停、删除提醒。
主动消息写进用户最近聊天的那段对话。提醒和定时任务另外发到用户绑定的**所有**聊天软件（不管是在网页还是在哪个聊天软件里设的）；
主动关心只在那段对话来自微信、QQ 或 Telegram 时发到那里。

## 聊天软件

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `channels.weixin.enabled` | false | 个人微信（微信官方 ClawBot 接口），每个用户在「聊天软件」页扫码连接自己的微信 |
| `channels.qq.enabled` | false | QQ（NapCat 等 OneBot v11 实现，反向 WebSocket 连接到 `/onebot/v11/ws`） |
| `channels.qq.access_token` | 空 | NapCat 连接令牌，至少 8 位 |
| `channels.qq.auto_accept_friend` | true | 自动通过好友申请 |
| `channels.telegram.enabled` | false | Telegram 机器人 |
| `channels.telegram.bot_token` | 空 | 在 @BotFather 创建机器人获得 |
| `channels.telegram.proxy` | 空 | 访问 Telegram 的代理，国内服务器需要 |
| `channels.telegram.api_base` | 空 | Bot API 地址，可以换成自建的反向代理 |
| `channels.dir` | `<data_dir>/channels` | 微信登录态等数据（每个用户一个文件，在 `weixin/` 中） |

只处理私聊。微信由每个用户扫码连接自己的微信，扫完自动绑定（ClawBot 是扫码人自己微信里的机器人，只有扫码的人能用）；
管理后台「聊天软件」页可以看到谁连接了微信并断开。旧版本在管理后台扫码登录的微信继续可用（靠绑定码）。
QQ、Telegram 里的人发消息时，布谷会请他先在网页「聊天软件」页生成绑定码并发过来；
绑定后在聊天软件里聊天就和在网页上一样（同一份记忆、心情）。每个聊天软件固定一段对话，网页对话列表里放在「外部聊天」下，带「微信」「QQ」等标签；
换话题直接聊就行，聊多了会自动压缩，也可以发「压缩」手动整理；发「解绑」解除绑定。
连着发的几条消息会合并成一轮回复，回复会像真人一样拆成几条、间隔着发出去。工具和网页上一样能用；
发来的图片和文件会保存到用户的文件里，代码生成的图片、用 send_file 发送的文件也会发回聊天软件。
聊天软件里的对话在网页的对话列表中带有「微信」「QQ」等标记，网页上也可以接着聊（网页上的回复不会推送到聊天软件）。

微信只能回复给发过消息的人（需要对方消息里带的凭证），所以要先在微信里给布谷发一条消息，之后提醒和主动消息才能发到微信。
接入方式参考了 [AstrBot](https://github.com/AstrBotDevs/AstrBot) 的实现；QQ 使用的 NapCat 是非官方协议，存在封号风险，建议用小号。

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
