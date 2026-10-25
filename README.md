# 布谷 BUUGOO

布谷是一个会记事的 AI 朋友。它记得你提到过的细节，会在合适的时候自然地提起；它有稳定的性格，不会一味附和；
它关心你的作息、身体和身边的人，也会提醒你别总是只和 AI 聊天。

布谷参考人类的记忆机制设计：每轮对话后挑出值得记住的事，按重要性、新近程度和与当前话题的相关度回想，
并区分"你亲口说过的"和"它自己推测的"，避免记忆失真。

## 名字的由来

布谷鸟会按时啼叫，提醒人们时节到了。布谷也一样：记得你的事，到了合适的时候主动问候一句。

## 现在能做什么

- **聊天**：一条连续的对话，按日期分隔，流式输出。Web 界面和终端界面（`buugoo chat`）都可以用。
- **记忆**：自动记下值得记住的事，打重要性分；回复时想起相关的记忆，并告诉你它想起了什么。
  记忆页可以查看、搜索、删除，删除会立即清除内容。
- **稳定的人格**：人格档案写在独立的 YAML 文件里，对话中无法被改动。
- **两种部署**：自部署单用户（默认，SQLite，零配置），或官网多用户（MySQL、邀请码注册、每日用量限额）。
- **一键安装、更新、卸载**：SHA256 校验，更新前自动备份数据库，失败自动回滚。

日程管理、遗忘曲线、每日记忆巩固、情绪与关系、主动消息会在后续阶段加入，见 [docs/roadmap.md](docs/roadmap.md)。

## 快速开始

```bash
# 1. 安装（Linux / macOS）
curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | bash

# 2. 填写模型 API Key（配置文件位置见安装结果）
#    llm.chat.api_key       DeepSeek：https://platform.deepseek.com
#    llm.embedding.api_key  硅基流动：https://cloud.siliconflow.cn
nano ~/.config/buugoo/config.yaml

# 3. 启动，然后打开 http://127.0.0.1:8080
buugoo serve

# 也可以在另一个终端里聊天
buugoo chat
```

聊天模型默认用 DeepSeek，向量模型默认用硅基流动的 `BAAI/bge-m3`。任何 OpenAI 兼容服务都可以替换，
例如阿里云百炼的 `text-embedding-v3`（见[配置说明](docs/config.md)）。

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
llm:
  chat:      { base_url: https://api.deepseek.com/v1,   api_key: "", model: deepseek-chat }
  embedding: { base_url: https://api.siliconflow.cn/v1, api_key: "", model: BAAI/bge-m3 }
quota:
  daily_messages: 0          # 每个用户每天的消息数上限，0 不限
  daily_tokens: 0
```

所有配置项都可以用环境变量覆盖：前缀 `BUUGOO_`，层级之间用双下划线，例如
`BUUGOO_LLM__CHAT__API_KEY=sk-xxx`。密钥建议只放在环境变量里，不要提交到任何仓库。
完整说明见 [docs/config.md](docs/config.md)。

自定义人格：复制 [configs/persona.default.yaml](configs/persona.default.yaml)，修改后在配置中设置
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

对外访问请放在反向代理（Nginx / Caddy）后面并开启 HTTPS，见方式三中的反向代理配置。

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
   - 「反向代理 → 添加反向代理」，目标 URL 填 `http://127.0.0.1:8080`。

   聊天使用 SSE 流式输出，请在反向代理配置中关闭缓冲并延长超时（布谷已在响应中设置
   `X-Accel-Buffering: no`，以下配置作为保险）：

   ```nginx
   location / {
       proxy_pass http://127.0.0.1:8080;
       proxy_http_version 1.1;
       proxy_set_header Host $host;
       proxy_set_header X-Real-IP $remote_addr;
       proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
       proxy_set_header X-Forwarded-Proto $scheme;
       proxy_buffering off;
       proxy_read_timeout 300s;
   }
   ```

   配置中的 `server.listen` 保持 `127.0.0.1:8080` 即可，只让反向代理访问。

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
