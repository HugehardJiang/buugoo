# 布谷 BUUGOO

布谷是一个会记事的 AI 朋友。它记得你提到过的细节，会在合适的时候自然地提起；它有稳定的性格，不会一味附和；
它关心你的作息、身体和身边的人，也会提醒你别总是只和 AI 聊天。

布谷参考人类的记忆机制设计：每轮对话后挑出值得记住的事，按重要性、新近程度和与当前话题的相关度回想，
并区分"你亲口说过的"和"它自己推测的"，避免记忆失真。

## 名字的由来

布谷鸟会按时啼叫，提醒人们时节到了。布谷也一样：记得你的事，到了合适的时候主动问候一句。

## 现在能做什么

- **聊天**：可以开多段对话，各自独立，按日期分隔，流式输出。Web 界面和终端界面（`buugoo chat`）都可以用。
  布谷说话像微信上的朋友：短句、口语、不端着，一次回复常常拆成一两条气泡发出来。
- **选模型、调思考**：在网页里添加任何 OpenAI 兼容的模型（DeepSeek、通义千问、硅基流动、OpenAI、Ollama…），
  每段对话可以单独选模型和思考等级（关 / 低 / 中 / 高；DeepSeek 为关 / 低 / 高 / 最高），思考过程可以展开查看。
  多用户版本中每个人都可以添加自己的 API Key，用自己的 Key 聊天不占每日限额。
- **发图片和文件**：点回形针、粘贴或拖进输入框。图片交给能看图的模型（如 `deepseek-flash`）；模型不能看图时，
  先请能看图的模型转述成文字。PDF、Word（.docx）、TXT、Markdown、CSV、代码等文档会提取文字放进对话。
- **联网与算数**：布谷会自己判断什么时候需要联网搜索、读取网页（当前模型是 DeepSeek 时，用它的 Key 调用
  DeepSeek 的搜索与抓取接口），以及什么时候写一段 Python 代码运行来得到准确结果。过程会显示在回复上方，可以展开查看。
- **记忆**：自动记下值得记住的事，打重要性分；回复时自然地想起相关的事。记忆在所有对话间共享。
- **情绪**：布谷对每个人有自己的情绪——会开心、难过、生气、担心、惊讶、不耐烦、想念，会慢慢平复，
  也会随着相处变得亲近或疏远（见下文「情绪是怎么算的」）。被冒犯时可以明显生气，但不会辱骂人。
- **大脑皮层**：布谷记得的事和此刻的心情默认藏起来——朋友不会把心里的东西摊开给你看；想看时可以在设置里翻开，
  查看、搜索、删除记忆，看看布谷对你的心情和你们的关系。
- **公式**：回复中的 LaTeX 公式（`\(…\)`、`\[…\]`、`$…$`、`$$…$$`）会被正确渲染。
- **管理后台**：在网页上查看和修改全部系统设置（「设置 → 管理后台」，管理员密码默认 `1228`），重启服务后生效。
- **稳定的人格**：默认的布谷是一只风趣幽默、爱拿自己肚子开玩笑的大肥鸟。人格档案写在 YAML 里，对话中无法被改动；
  管理员可以在「管理后台 → 人格」直接编辑，保存后立即生效。
- **选性格**：不会调人格也没关系，每个人可以在「设置 → 性格」里给布谷选一种相处方式：默认、温柔治愈、毒舌损友、
  理性军师、元气话痨，下一条消息开始生效。换性格只改说话方式，名字、原则、记忆和心情都不变；
  管理员可以用 `persona.disabled_presets` 关掉不想提供的预设。
- **文件管理**：上传的图片和文档、布谷运行代码画的图都在「文件」页，可以按类型筛选、多选（全选）删除，
  或一键删除 7 天前的全部文件。每人的文件空间有上限（默认 500 MB），用满后不能再上传文件，删除一些即可恢复；
  删掉的文件在聊天记录里显示为「文件已删除」。
- **两种部署**：自部署单用户（默认，SQLite，零配置），或官网多用户（MySQL、邀请码注册、每日用量限额）。
- **一键安装、更新、卸载**：SHA256 校验，更新前自动备份数据库，失败自动回滚。

日程管理、遗忘曲线、每日记忆巩固、情绪与关系、主动消息会在后续阶段加入，见 [docs/roadmap.md](docs/roadmap.md)。

## 快速开始

```bash
# 1. 安装（Linux / macOS）
curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | bash

# 2. 启动，然后打开 http://127.0.0.1:8080
buugoo serve

# 3. 在网页「设置 → 模型」添加聊天模型，在「设置 → 向量模型」填写向量模型的 Key
#    DeepSeek：https://platform.deepseek.com    硅基流动：https://cloud.siliconflow.cn
#    （也可以直接写进配置文件 llm.chat / llm.embedding，配置文件位置见安装结果）

# 也可以在另一个终端里聊天
buugoo chat
```

聊天模型推荐 DeepSeek，向量模型默认用硅基流动的 `BAAI/bge-m3`。任何 OpenAI 兼容服务都可以替换，
例如阿里云百炼的 `text-embedding-v3`（见[配置说明](docs/config.md)）。

### 模型与思考等级

- 网页中添加的模型 API Key 用 AES-256-GCM 加密后存进数据库，密钥是配置中的 `security.encryption_key`
  （`buugoo init` 自动生成；没有时自动生成 `<数据目录>/secret.key`）。**请和数据库一起备份**，丢失后需要重新填写 Key。
- 思考等级的接法在添加模型时选择：`deepseek`（`thinking` 开关 + `reasoning_effort` low / high / max）、
  `reasoning_effort`（OpenAI 等）、`enable_thinking`（通义千问 Qwen3、硅基流动等）、「换用推理模型」（只有开和关）。
- DeepSeek 已停用 `deepseek-chat` / `deepseek-reasoner`，升级后会自动改用 `deepseek-flash`（配置文件中的模型会在日志里提醒你同步修改）。
- 添加模型时可以勾选「能看图」和「支持工具调用」。
- 向量模型全站只有一个，只有管理员能改。换了向量模型后，在网页上点「重新计算」即可（命令行也可以用 `buugoo reembed`）。
- 记忆提取跟随当前对话所用的模型。
- 多用户版本：管理员添加的「公共模型」所有人可用、计入每日限额；用户自己添加的模型只有自己能看到、不计限额。
  出于安全考虑，普通用户的模型地址只能是公网地址（不能连服务器内网或本机），如确需连接局域网模型，
  设置 `security.allow_private_model_urls: true`。

### 情绪是怎么算的

参考人类情绪的几个特点设计，每个用户各自一份：

- **两个时间尺度**：七种情绪由事件触发，各自按半衰期消退（惊讶几分钟、不耐烦约 20 分钟、生气约 45 分钟、
  开心约 3 小时、担心约 6 小时、难过约半天）；心境是最近一段时间心情的底色，跟随近期感受缓慢漂移，
  反过来影响对新事件的感受——心情好时更容易开心，心情差时同一句话更刺耳。
- **评价**：每轮对话后，记忆提取的同一次模型调用会顺带评价用户这句话：愉不愉快、有没有冒犯、是不是在关心布谷、
  有没有道歉、对方自己是什么情绪、是否处境危险、是否出乎意料、是否在催促，再按规则换算成情绪变化（不额外花钱）。
  情绪有惯性：这一轮的冒犯会让下一轮明显带着气。
- **关系**：亲近与信任随相处慢慢积累，冒犯的分量约是善意的两倍；越亲近越容易被对方的情绪感染、越容易消气；
  真诚的道歉能让怒气明显消退。
- **即时信号**：好几天没聊会想念、见面时开心；一分钟里连发好多条、反复问同一句话会不耐烦；深夜会有点困。
- **表达**：强度超过阈值的情绪写进提示词，按「有点 / 明显 / 非常」三档调整语气。对方遇到危险或情绪崩溃时，
  布谷会先放下自己的情绪关心对方。

脾气可以在配置 `emotion` 中调整（起伏、易怒、耐心、宽容、共情，0-3，1 为标准），也可以在管理后台修改；
用户可以在「大脑皮层」里让布谷重新认识自己（情绪与关系从头开始，记忆不受影响）。

### 管理后台

「设置 → 管理后台」用管理员密码登录（配置文件 `admin.password`，**默认 `1228`，所有人都知道，请务必改掉**），
可以查看和修改几乎全部系统设置。修改保存在 `<数据目录>/settings.yaml`，覆盖配置文件中的同名设置（环境变量仍然优先），
**需要在服务器上重启服务后才生效**（`sudo systemctl restart buugoo`）。管理员密码与加密密钥只能在配置文件中修改；
删除 `settings.yaml` 即可恢复为配置文件中的设置。还在使用默认密码时，后台和启动日志都会持续提醒。

### 运行代码的安全

模型写的 Python 代码默认在**一次性的 Docker 容器**里运行：断网、只读、以 nobody 用户运行、去掉所有权限、
限制内存（256 MB）、CPU、进程数和时间（30 秒），看不到服务器上的任何文件。这种方式下所有用户都可以用。

- 已预装 numpy、scipy、sympy、pandas、mpmath、matplotlib（启动时在后台安装到 `<数据目录>/pylib`，所有人共用）。
  缺少别的库时，布谷会自己调用「安装 Python 库」：只能从 PyPI（或配置的国内镜像 `tools.python.index_url`）安装
  **预编译包**，安装过程中不运行任何安装脚本；只有安装那一步联网，运行代码时库目录只读、仍然断网。
  multi 模式下只有管理员的对话可以安装；不想让布谷装库可以设置 `tools.python.allow_install: false`。
- **画图**：代码用 matplotlib 画的图（或保存到当前目录的 png/jpg 图片）会直接显示在回复里，一次最多 6 张，
  保存为这条回复的附件，计入用户的上传空间，删除对话时一起删除。图里想写中文，需要服务器装有中文字体
  （例如 `sudo apt install fonts-noto-cjk`，然后重启服务）；没有中文字体时布谷会用英文标注。
- 服务器需要装好 Docker，并且布谷的服务用户能使用 `docker` 命令。用安装脚本注册的服务以 `buugoo` 用户运行，
  需要执行 `sudo usermod -aG docker buugoo && sudo systemctl restart buugoo`。**注意：能使用 Docker 等同于拥有 root 权限**，
  请确认你接受这一点。第一次启动时会自动拉取 `python:3.12-slim` 镜像（只有标准库）。
- 不想装 Docker：在配置中设置 `tools.python.runner: local`，直接用本机 `python3` 运行，只限制时间和内存，
  代码能访问网络和服务用户能读到的文件——**只适合自己用**，多用户版本中这种方式只有管理员可用。
- 不需要运行代码：设置 `tools.python.runner: off`。
- 用 Docker 方式部署布谷本身时，容器里没有 Docker，运行代码功能默认不可用。

## 配置

`buugoo init` 生成带注释的配置文件（安装脚本会自动执行）。最常改的几项：

```yaml
mode: single                 # single 自部署单用户 | multi 多用户
server:
  listen: "127.0.0.1:8080"   # 对外提供服务时改为 0.0.0.0:8080，并务必设置访问密码
auth:
  access_password: ""        # single 模式的访问密码
  registration: invite       # multi 模式：open | invite | closed
database:
  driver: sqlite             # sqlite | mysql
  dsn: ""
llm:                         # 也可以在网页「设置」里添加，网页设置优先
  chat:      { base_url: https://api.deepseek.com/v1,   api_key: "", model: deepseek-chat }
  embedding: { base_url: https://api.siliconflow.cn/v1, api_key: "", model: BAAI/bge-m3 }
security:
  encryption_key: ""         # 加密网页中保存的 API Key，init 自动生成
uploads:
  max_file_mb: 20            # 单个文件上限
  user_quota_mb: 500         # 每人上传总量上限，0 不限
tools:
  python:
    runner: docker           # docker | local | off
quota:
  daily_messages: 0          # 每个用户每天的消息数上限，0 不限
  daily_tokens: 0
```

所有配置项都可以用环境变量覆盖：前缀 `BUUGOO_`，层级之间用双下划线，例如
`BUUGOO_LLM__CHAT__API_KEY=sk-xxx`。密钥建议只放在环境变量里，不要提交到任何仓库。
完整说明见 [docs/config.md](docs/config.md)。

自定义人格：在网页「设置 → 管理后台 → 人格」直接编辑，保存后立即生效（保存在 `<数据目录>/persona.yaml`，
优先于配置文件）；也可以复制 [configs/persona.default.yaml](configs/persona.default.yaml)，修改后在配置中设置
`persona.file`，重启服务。

## 安装、更新与卸载

### 安装

```bash
# Linux / macOS：普通用户安装到 ~/.local/bin
curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | bash

# Linux 服务器：root 安装到 /usr/local/bin，注册 systemd 服务（以独立的 buugoo 用户运行，开机自启）
curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | sudo bash -s -- -y --service
```

```powershell
# Windows（PowerShell）
irm https://github.com/HugehardJiang/buugoo/releases/latest/download/install.ps1 | iex
```

安装脚本参数：

| 参数 | 说明 |
| --- | --- |
| `--version v0.1.0` | 安装指定版本（默认最新） |
| `--prefix /opt/bin` | 二进制安装目录 |
| `--mirror <地址>` | 下载镜像（也可用环境变量 `BUUGOO_MIRROR`） |
| `--service` / `--no-service` | 是否注册 systemd 服务（Linux + root） |
| `-y` | 非交互模式 |
| `--force` | 即使已是该版本也重新安装 |

PowerShell 对应参数为 `-Version`、`-InstallDir`、`-Mirror`、`-Yes`、`-Force`。

安装脚本可以重复执行：已安装时会转为更新，已有的配置文件不会被覆盖。

**国内网络**：GitHub 下载慢时用 `--mirror` 指定镜像。镜像需要提供与 GitHub Releases 相同的布局：
`<镜像>/latest`（内容为最新版本号）和 `<镜像>/<版本>/<文件名>`。详见 [docs/lifecycle.md](docs/lifecycle.md)。

### 更新

```bash
buugoo update --check            # 只检查
sudo buugoo update               # 更新到最新版本（系统级安装需要 sudo）
buugoo update --version v0.2.0   # 更新到指定版本
buugoo update --rollback         # 回滚到上一个版本
```

更新会先校验 SHA256、自动备份数据库（备份路径会打印出来），然后停止服务、替换程序、执行数据库迁移、
重启服务并检查健康状态；任何一步失败都会自动恢复旧版本和数据库备份。MySQL 备份需要 `mysqldump`，
没有时会提示你先手动备份。

服务启动时如果发现新版本，会在日志里提示（`update.check_on_start: false` 可关闭）。

### 卸载

```bash
sudo buugoo uninstall            # 移除服务和程序，保留配置和数据
sudo buugoo uninstall --purge    # 连同配置和数据一起删除（需要确认，非交互模式加 -y）
```

程序损坏时也可以用独立的卸载脚本：

```bash
curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/uninstall.sh | sudo bash
```

## 部署方式

### 方式一：一键脚本（推荐）

见上文。Linux 服务器上以 root 安装并加 `--service`，布谷会作为 systemd 服务运行：

```bash
sudo systemctl status buugoo      # 状态
sudo systemctl restart buugoo     # 修改配置后重启
journalctl -u buugoo -f           # 日志
```

对外访问请放在反向代理（Nginx / Caddy）后面并开启 HTTPS，见方式三中的反向代理配置（宝塔见 [配置指南](docs/baota.md)）。

### 方式二：Docker

```bash
git clone https://github.com/HugehardJiang/buugoo.git && cd buugoo/deploy
cp .env.example .env              # 填写 API Key 和访问密码
docker compose up -d              # SQLite 单用户
# 或：docker compose -f docker-compose.mysql.yml up -d   （MySQL 多用户）
```

镜像：`ghcr.io/hugehardjiang/buugoo`（linux/amd64、linux/arm64）。数据保存在卷 `/data` 中。

更新：

```bash
docker compose pull && docker compose up -d
```

容器内的 `buugoo update` 不可用，请始终通过拉取新镜像更新。

### 方式三：宝塔面板

1. **安装程序**：在宝塔终端以 root 执行（不注册 systemd 服务，交给宝塔托管）：

   ```bash
   curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | bash -s -- -y --no-service
   ```

   程序位于 `/usr/local/bin/buugoo`，配置位于 `/etc/buugoo/config.yaml`，数据位于 `/var/lib/buugoo`。

2. **（可选）使用宝塔的 MySQL**：在「数据库」中新建数据库 `buugoo`（字符集 `utf8mb4`），然后在配置文件中设置：

   ```yaml
   database:
     driver: mysql
     dsn: "buugoo:数据库密码@tcp(127.0.0.1:3306)/buugoo?charset=utf8mb4&parseTime=True"
   ```

   多用户官网还需设置 `mode: multi` 和 `auth.jwt_secret`（`buugoo init` 已自动生成），
   再用 `buugoo admin create-user 你的用户名 --admin` 创建管理员。

3. **托管进程**，二选一：
   - 「网站 → Go 项目 → 添加 Go 项目」：项目执行文件填 `/usr/local/bin/buugoo`，
     执行命令填 `/usr/local/bin/buugoo serve --config /etc/buugoo/config.yaml`，端口填 `8080`，
     运行用户选 `buugoo`（安装脚本已创建）或 `www`。
   - 「软件商店 → 进程守护管理器」：添加守护进程，启动命令同上，运行目录 `/var/lib/buugoo`，运行用户 `buugoo`。

   运行用户需要能读取配置文件、写入数据目录。如果选择 `www`，执行：

   ```bash
   chown -R www:www /var/lib/buugoo && chown root:www /etc/buugoo/config.yaml
   ```

4. **反向代理与 HTTPS**：「网站 → 添加站点」绑定你的域名，在站点设置中：
   - 「SSL」申请 Let's Encrypt 证书并开启「强制 HTTPS」；
   - 「反向代理 → 添加反向代理」，目标 URL 填 `http://127.0.0.1:8080`，再把配置文件替换为：

   ```nginx
   location ^~ / {
       proxy_pass http://127.0.0.1:8080;
       proxy_http_version 1.1;
       proxy_set_header Connection "";
       proxy_set_header Host $host;
       proxy_set_header X-Real-IP $remote_addr;
       proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
       proxy_set_header X-Forwarded-Proto $scheme;
       proxy_buffering off;              # 聊天是流式输出（SSE），必须关闭缓冲
       proxy_cache off;
       proxy_request_buffering off;
       proxy_read_timeout 600s;          # 运行代码、安装库可能需要几分钟
       proxy_send_timeout 600s;
       client_max_body_size 25m;         # 不小于 uploads.max_file_mb
   }
   ```

   配置中的 `server.listen` 保持 `127.0.0.1:8080` 即可，只让反向代理访问。
   详细步骤、检查方法和常见问题见 [宝塔反向代理配置指南](docs/baota.md)。

5. **更新**：在宝塔中停止项目 → 终端执行 `buugoo update -y` → 启动项目。
   （没有注册 systemd 服务时，`buugoo update` 不会自动重启进程。）

## 终端聊天

```bash
buugoo chat                          # 连接本机服务
buugoo chat -s https://buugoo.example.com
buugoo chat -m "今天好累"             # 发一条消息并输出回复
echo "你好" | buugoo chat             # 从标准输入逐行发送
```

界面内命令：`/memories [关键词]` 查看记忆、`/forget <编号>` 删除记忆、`/recall` 开关"想起的记忆"、
`/prompt <内容>` 查看完整提示词、`/help`、`/quit`。快捷键：Enter 发送、Ctrl+J 换行、Esc 停止生成。

## 命令一览

| 命令 | 说明 |
| --- | --- |
| `buugoo serve` | 启动服务（含 Web 界面），自动执行数据库迁移 |
| `buugoo chat` | 终端聊天 |
| `buugoo init` | 生成默认配置文件（已存在则不覆盖） |
| `buugoo migrate [--status]` | 执行数据库迁移 / 查看状态 |
| `buugoo update` | 自更新（`--check`、`--version`、`--rollback`） |
| `buugoo uninstall` | 卸载（`--purge` 删除配置和数据） |
| `buugoo reembed` | 更换向量模型后重新计算全部记忆的向量 |
| `buugoo admin` | 用户与邀请码管理（`create-user`、`set-admin`、`users`、`invite create/list`） |
| `buugoo version` | 查看版本 |

## 开发

需要 Go 1.27 和 Node.js 22。

```bash
make web            # 构建前端并复制到 internal/web/dist（供 go:embed）
make build          # 构建 bin/buugoo
make test           # 单元测试（SQLite）
make test-mysql     # 启动临时 MySQL 容器运行数据库测试
make e2e-lifecycle  # 在 systemd 容器中测试安装 → 更新 → 回滚 → 卸载 → 重装
make cross          # 交叉编译所有平台
make web-dev        # 前端开发服务器（代理到 127.0.0.1:8080）
```

设计文档在 [docs/](docs/)：[架构](docs/architecture.md)、[记忆系统](docs/memory.md)、
[向量检索](docs/vector-store.md)、[人格与提示词](docs/persona-and-prompt.md)、
[部署模式与鉴权](docs/auth-and-modes.md)、[安装与更新](docs/lifecycle.md)、[配置](docs/config.md)、
[视觉设计](docs/design.md)、[路线图](docs/roadmap.md)。

发布：推送 `v*` tag，GitHub Actions 会构建各平台二进制、发布到 GitHub Releases，并推送 Docker 镜像。

## 许可证

[AGPL-3.0](LICENSE)。如果你修改布谷并以网络服务的形式提供给他人使用，需要向这些用户公开修改后的源代码。
