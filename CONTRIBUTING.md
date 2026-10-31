# 修改代码的标准流程

本文写给参与布谷（Buugoo）开发的组员：从拿到需求，到改代码、测试、提交、发布，每一步按这里做。
目标只有一个：**发出去的版本不出错，用户的钱和数据不出事。**

---

## 0. 一张图看完整流程

```
弄清需求 → 拉分支 → 读相关代码 → 改代码（后端 / 前端 / 迁移 / 文档）
       → 本地检查（格式、vet、单元测试、前端类型检查）
       → 本地跑起来实测（模拟模型服务 + 浏览器）
       → 自己审一遍 diff → 提交 → 推送 → 等 CI 全绿 → 发布 → 告诉大家怎么升级
```

任何一步不过，都回到改代码那一步，不要带着红灯往下走。

---

## 1. 开始之前

### 1.1 弄清需求

- 需求有歧义时**先问清楚再动手**，不要猜。例如「这个按钮放哪」「对已有数据怎么处理」「管理员和普通用户是否都能用」。
- 想清楚三件事：
  1. 单用户版（`mode: single`）和多用户版（`mode: multi`）是否都受影响？
  2. 会不会扣用户的额度 / 余额？要不要计费？
  3. 已经在用的老数据怎么办？（需要迁移吗？旧数据要清理吗？）

### 1.2 环境

| 工具 | 版本 |
| --- | --- |
| Go | 与 `go.mod` 中 `go` 一行一致（目前 1.27.3） |
| Node.js | 22 |
| Docker | 跑 MySQL 测试时需要 |
| Python 3 | 跑模拟模型服务时需要 |
| Chromium / Playwright | 可选，做浏览器自动化测试时用 |

```bash
cd web && npm ci && cd ..      # 安装前端依赖
make help                     # 查看所有常用命令
```

### 1.3 分支

- **不要直接在 `main` 上改**。每个需求新建分支：`feat/xxx`、`fix/xxx`。
- 开始前先同步：`git fetch origin && git checkout -b fix/xxx origin/main`。
- 一个分支只做一件事（或一组强相关的事），方便审查和回滚。

---

## 2. 代码结构速查

| 目录 | 内容 |
| --- | --- |
| `cmd/buugoo` | 程序入口 |
| `internal/cli` | 命令行（`serve`、`update`、`admin` 等） |
| `internal/httpapi` | HTTP 接口（Echo）；`server.go` 里注册路由和登录中间件 |
| `internal/auth` | 登录、令牌、密码 |
| `internal/chat` | 聊天主流程、上下文、压缩、对话管理 |
| `internal/models` | 模型管理、上下文长度识别、辅助模型 |
| `internal/billing`、`internal/quota` | 计费、额度 |
| `internal/store` | 数据接口定义；`gormstore/` 是实现和**数据库迁移** |
| `internal/settings` | 管理后台「系统设置」的字段说明和分组 |
| `internal/web/dist` | 前端构建产物（嵌进二进制，**不提交**，只保留 `.gitkeep`） |
| `web/src` | 前端（Vue 3 + TypeScript）；`api.ts` 是所有接口调用 |
| `configs/` | 示例配置和默认人格 |
| `docs/` | 设计文档；`README.md` 是用户手册 |
| `tests/e2e` | 安装 / 更新 / 回滚 / 卸载的端到端测试 |
| `tests/manual` | 本地手工测试用的配置和模拟模型服务 |

改某个功能前，先用 `grep -rn "关键字" internal web/src` 找到所有相关位置，读懂再改。
**相似的功能先看已有的写法**，照着现有风格写，不要另起一套。

---

## 3. 编写代码的规范

### 3.1 通用

- 注释、界面文字、错误提示、提交信息都用**简体中文**，说人话，不用术语。
  - 好：「上一条回复还在生成，等它说完或者先点停止」
  - 差：「Conflict: generation in progress」
- 提到用户时用「对方」「用户」「你」，**不要用「他 / 她」**（性别未知）。
- 只改需求需要的地方，不顺手大改无关代码。
- 不要留下调试代码、`console.log`、注释掉的旧代码。

### 3.2 安全（最重要，违反任何一条都不能合并）

- **密钥、密码、Token、API Key 绝不能写进代码或提交到仓库**，包括测试数据、截图、日志。
  需要的话用配置文件或环境变量，示例里用占位符。
- 每个新接口都要想清楚**谁能调用**：
  - 普通用户接口挂在 `requireUser` 下，查询数据时**必须带上当前用户 ID**，不能只按记录 ID 查（否则别人能读到你的数据）。
  - 管理后台接口挂在管理员认证下。
  - 临时登录（管理员排查问题）下不该做的操作，用 `notImpersonated(...)` 包起来。
- 涉及**扣费 / 额度 / 余额 / 卡密 / 套餐**的改动：
  - 先算清楚，写单元测试覆盖边界（余额刚好够、不够、并发两次扣费）；
  - 不能出现「免费调用付费模型」或「重复扣费」的路径；
  - 敏感操作写审计日志。
- 用户输入一律当作不可信：校验长度和范围，前端校验不能代替后端校验。
- 访问外部网址的功能要经过 `internal/netguard`，防止访问内网地址。

### 3.3 后端（Go）

- 改完必须 `gofmt`（CI 会检查）。
- 错误要么处理，要么带上下文往上返回：`fmt.Errorf("读取对话: %w", err)`。
- 后台任务（goroutine）要能随服务关闭退出，不要泄漏。
- 同时支持 **SQLite 和 MySQL**：不写只有一种数据库能用的 SQL。

### 3.4 数据库迁移

改了数据表结构（加字段、加表、改默认值、修正老数据）时：

1. 在 `internal/store/gormstore/` 新建 `migrations_vN.go`（N = 当前最大编号 + 1）。
2. ID 格式：`YYYYMMDD00NN_简短英文说明`，例如 `202610290019_unlimited_max_tokens`。
3. 在 `migrate.go` 的 `migrations()` 列表**末尾**加上它。
4. 规则：
   - **已发布的迁移永远不能改**，只能追加新的；
   - 迁移里用自己的快照结构体，不引用 `store` 包里会变的模型；
   - 必须能**重复执行**（执行一半中断后再跑一次不出错）；
   - 必须在 SQLite 和 MySQL 上都测过（见 4.2）。
5. 同时修改 `internal/store/model.go` 中的模型和 `gormstore/repos.go` 中的读写。

### 3.5 前端（Vue）

- 所有接口调用写在 `web/src/api.ts`，组件里不要直接 `fetch`。
- 样式用 `style.css` 中已有的颜色变量（`var(--color-...)`），不要写死颜色；深色模式要能看。
- **手机和电脑都要能用**：至少在 390px 宽和 1280px 宽下各看一次。
- 有动画的地方照顾 `prefers-reduced-motion`。
- 注意：`<input type="number" v-model="x">` 拿到的是**数字**（清空时是空字符串），
  不能直接 `x.trim()`；需要字符串时用 `String(x)`，需要数字时用 `v-model.number`。

### 3.6 设置项和文档

- 新增配置项：同时改 `internal/config/config.go`、`configs/config.example.yaml`、
  `internal/settings/meta.go`（后台显示的名称和说明）、`internal/settings/layout.go`（放在哪个分组）。
- 用户能看到的新功能：在 `README.md` 对应章节补上说明；设计上的变化同步改 `docs/`。

---

## 4. 测试（提交前必须全部通过）

### 4.1 快速检查（每次提交前都跑）

```bash
gofmt -l .                     # 不应输出任何文件；有输出就 gofmt -w <文件>
go vet ./...
go test -race ./...            # 单元测试（SQLite）
cd web && npm run build && cd ..   # 前端类型检查 + 构建
```

也可以 `make lint && make test`。

### 4.2 改了数据库相关代码时

```bash
make test-mysql                # 自动启动临时 MySQL 容器，跑 store / app 的测试，跑完删掉容器
```

### 4.3 写单元测试

- 修 bug：**先写一个能复现 bug 的测试**（改之前是红的），再修到绿。
- 新功能：至少覆盖正常路径、权限（别的用户 / 未登录 / 临时登录）、边界值。
- 测试放在被测代码同目录的 `xxx_test.go`，参考已有测试的写法（如 `internal/auth/service_test.go`）。
- 数据库测试写在 `gormstore_test.go`，它会同时在 SQLite 和 MySQL 上跑。

### 4.4 本地跑起来实测（界面改动必须做）

单元测试不能代替亲手点一遍。仓库里带了一个**模拟模型服务**，不用花真实模型的钱：

```bash
# 1) 构建前端并嵌入，再构建后端
cd web && npm run build && npx vite build --outDir ../internal/web/dist --emptyOutDir && cd ..
git checkout -q internal/web/dist/.gitkeep        # vite 会删掉它，恢复一下
go build -o bin/buugoo ./cmd/buugoo

# 2) 启动模拟模型服务（终端 A）
python3 tests/manual/fake_llm.py                  # 监听 127.0.0.1:19090

# 3) 建测试管理员并启动服务（终端 B）
cd tests/manual
echo "password1" | ../../bin/buugoo admin create-user tester --admin --password-stdin -c config.yaml
../../bin/buugoo serve -c config.yaml             # 打开 http://127.0.0.1:18080
```

- 用 `tester / password1` 登录；管理后台密码是 `local-admin-pass`。
- 模拟服务会把收到的请求写进 `reqlog.jsonl`，可以检查发给模型的提示词、工具是否正确。
- 消息里带「慢慢说」时模拟服务会慢速输出，用来测「生成中切走 / 停止」之类的场景。
- 只改前端时也可以用 `make web-dev` 启动 Vite 开发服务器（代理到 127.0.0.1:8080），改完即时刷新。
- 测完删掉 `tests/manual/data/` 即可重来（已在 `.gitignore` 中）。

实测清单（按改动挑相关的）：

- [ ] 改动的功能本身：正常操作、错误输入、刷新页面后状态是否还在
- [ ] **保存类功能：保存 → 关闭 → 重新打开，值还在**
- [ ] 电脑宽屏 + 手机窄屏（浏览器开发者工具切到 390px）
- [ ] 浅色 + 深色模式
- [ ] 普通用户 + 管理员；多用户版改动再看一下单用户版
- [ ] 浏览器控制台没有报错（红字）
- [ ] 涉及计费：在后台「用户管理」里看余额 / 消费记录是否和预期一致

有条件的话用 Playwright 写个小脚本自动点一遍（环境里 `require('playwright')` 即可用），可以反复跑。

---

## 5. 提交

### 5.1 自己先审一遍

```bash
git status                     # 确认没有多余文件（data/、日志、截图、.env、构建产物）
git diff                       # 逐行看一遍
```

自问：

- 有没有把密钥、密码、个人信息带进来？
- 有没有调试代码、无关改动？
- 别的用户能不能通过这个改动看到 / 改到不属于自己的数据？
- 老用户升级后会怎样？老数据兼容吗？
- 出错时用户看到的提示是不是人话？

### 5.2 提交信息

用中文，一句话说清**用户能感受到的变化**，不是「改了哪个文件」：

```
修复：模型的上下文长度保存后不生效，重新打开编辑框又变成空白
管理后台可以临时登录到用户的账号排查问题；点「新对话」不再立刻建出空对话
```

- 修 bug 以「修复：」开头。
- 需要补充原因时，空一行再写正文。
- 一次提交一件事；大功能可以拆成几次有意义的提交。

### 5.3 推送并等 CI

```bash
git push -u origin fix/xxx
```

推送后 GitHub Actions 会自动跑 CI（`.github/workflows/ci.yml`），包括：

| 任务 | 内容 |
| --- | --- |
| Go 检查与单元测试 | gofmt、go vet、`go test -race`（SQLite） |
| 数据库测试 | MySQL 8.4 上跑 store / app 测试 |
| 前端 | 类型检查与构建 |
| Docker 镜像 | 构建并冒烟测试 `/healthz` |
| 生命周期 | Linux 上 安装 → 更新 → 回滚 → 卸载 |
| Windows 安装脚本 | Windows 上的安装测试 |

**全部绿了才算完成**。红了就点进去看日志，在本地复现、修好、再推；不要靠反复重跑碰运气。

### 5.4 合并

- 在 GitHub 上开 Pull Request 到 `main`，写清：改了什么、为什么、怎么测的（贴上实测截图更好）。
- 至少一位组员看过再合并；涉及计费、登录、权限、迁移的改动必须有人认真审。

---

## 6. 发布新版本（由负责发布的人操作）

1. 确认 `main`（或要发布的分支）最新一次提交的 CI 全绿。
2. 确定版本号（`vX.Y.Z`）：
   - 修 bug：`Z + 1`，如 v0.32.0 → v0.32.1
   - 新功能：`Y + 1`，`Z` 归零，如 v0.32.1 → v0.33.0
3. GitHub → Actions → **Release** → Run workflow：选分支，填版本号（如 `v0.33.0`）。
   工作流会自动打 tag、交叉编译各平台、发布到 GitHub Releases、推送 Docker 镜像。
4. 等 Release 工作流成功后，到 Releases 页面确认文件齐全。
5. 通知大家升级：

   ```bash
   sudo buugoo update             # 系统级安装
   buugoo update --version v0.32.0   # 有问题时回到指定版本
   ```

   更新前会自动备份数据库；带迁移的版本发布前务必在 MySQL 上测过。

---

## 7. 常见坑

| 现象 | 原因 / 解决 |
| --- | --- |
| CI 的 gofmt 失败 | 本地没跑 `gofmt -w`；提交前跑 4.1 |
| 前端构建报「未使用的变量」 | vue-tsc 比较严格，删掉没用的 import / 变量 |
| 嵌入的页面还是旧的 | 没执行 `vite build --outDir ../internal/web/dist`，或没重新 `go build` |
| `internal/web/dist/.gitkeep` 显示被删除 | vite 清空目录导致的，`git checkout internal/web/dist/.gitkeep` |
| MySQL 上失败、SQLite 上正常 | 用了某种数据库特有的语法；或字段长度、默认值、大小写比较不同 |
| 模板里加了一块后整个页面空白 | 插进了 `v-if` / `v-else-if` / `v-else` 链条中间，挪到链条外面 |
| 数字输入框保存不生效 | 见 3.5：`type="number"` 的 v-model 是数字，不是字符串 |

---

有疑问先问，不确定的改动宁可多测一遍。
