# 布谷（Buugoo）开发工作区规范

本仓库是 Go 单二进制（Echo + GORM，同时支持 SQLite 与 MySQL）+ 内嵌 Vue 3 SPA + 终端 TUI 的 AI 朋友项目。完整贡献流程与设计说明见 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [docs/](docs/)。

## 分支与发布约定

- 远端默认分支为 `claude/upbeat-ptolemy-tmpxut`；未经用户明确要求，不要主动执行 `git push` 或触发发布工作流。
- 默认采用 **Linux systemd 快速发版** 模式，一旦用户要求「推送到 GitHub / 发版 / 上线」，则由助手**全程负责到底**：
  1. 本地通过 `powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 all` 验证后，提交代码并直接按语义化版本号规则打附注 tag `vX.Y.Z`（修 bug 为 `Z + 1`，新功能或页面改动为 `Y + 1` 且 `Z` 归零），将分支与 tag 一并推送到远端（`git push origin claude/upbeat-ptolemy-tmpxut vX.Y.Z`），**不再阻塞等待 `CI` 全部跑完再打 tag**（CI 在后台异步运行，若后续报错则在下一补丁版修复）；
  2. `release.yml` 收到 tag 推送后仅使用 `.goreleaser.linux.yaml` 构建 Linux `amd64/arm64` 二进制、`checksums.txt` 与 Linux 安装/卸载脚本，自动跳过 macOS/Windows 编译以及 Docker/沙箱镜像构建；
  3. 通过 GitHub API 使用 `schedule` 定时器每 30 秒跟进 `Release` 工作流直至 `goreleaser` 成功，并核验 GitHub Release 页面上的产物（`buugoo_*_linux_amd64.tar.gz`、`buugoo_*_linux_arm64.tar.gz`、`checksums.txt`、`install.sh`、`uninstall.sh`）齐全；
  4. 核验无误后立刻通知用户在服务器执行一键更新（`sudo buugoo update`）；
  5. 其余平台（macOS/Windows）及 Docker/沙箱镜像仅在用户明确要求「同步其他版本」时，再通过 `workflow_dispatch`（`mode: full`）按需同步。

## 中文与文案规范

- 代码注释、界面文字、报错提示、Git 提交信息一律使用**简体中文**，说人话、避免术语堆砌。
- 称呼用户统一使用「对方」「用户」或「你」，**不要使用「他 / 她」**。
- 界面文案不使用感叹号和 emoji。
- 修复 bug 的提交信息以 `修复：` 开头，描述用户可感知的变化而非改了哪个文件。

## 安全与计费红线

- 普通用户接口必须挂在 `requireUser` 下，且数据库查询必须带上当前 `user_id`，防止越权读写他人数据。
- 管理后台接口必须挂在 `requireConsole` 下；管理员临时登录下不允许的敏感操作必须包上 `notImpersonated(...)`。
- 访问外部网址的功能必须经过 `internal/netguard`，防止 SSRF 访问内网地址。
- 涉及会员、余额、卡密、额度、API 转发计费的改动，必须以「微元」整数做数据库原子更新并补齐边界与并发测试，绝不允许出现免费调用付费模型或重复扣费路径。
- 每次改动都要同时考虑单用户版（`mode: single`）与多用户版（`mode: multi`）、管理员与普通用户、老数据升级兼容。
- 严禁把任何真实密钥、密码、Token 或测试日志提交进仓库。

## 双数据库与迁移规则

- 所有数据库逻辑必须同时兼容 SQLite 与 MySQL 8.4，不写单一方言特有的 SQL。
- 修改表结构或迁移老数据时，在 `internal/store/gormstore/` 新建 `migrations_vN.go`（`N` 为当前最大编号 + 1），并在 `migrate.go` 的 `migrations()` 末尾注册。
- 每个迁移必须使用自己独立的快照结构体，不引用会演进的 `store` 模型，且必须幂等可重复执行；**已发布的迁移绝不能修改**。

## 前端与视觉规范

- 样式严格消费 `web/src/tokens.css`（由 `scripts/gen-tokens.py` 生成，不要手改）与 `web/src/style.css` 中已有变量和组件类；主按钮使用墨色 `--color-ink`，橄榄绿 `--color-accent` 仅作信号点缀，不在白色卡片内嵌套白色卡片。
- 所有 HTTP 请求统一写在 `web/src/api.ts`，不在 Vue 组件里直接调用 `fetch`。
- 兼顾 `1280px` 宽屏与 `390px` 手机窄屏，动画需尊重 `prefers-reduced-motion`。
- 注意 `<input type="number" v-model>` 返回数字或空字符串，需按字符串处理时先用 `String(...)`。

## 新增配置与文档同步

- 新增配置项需同步更新 `internal/config/config.go`、`configs/config.example.yaml`、`internal/settings/meta.go`、`internal/settings/layout.go`。
- 用户可见的新功能同步更新 `README.md`，设计变化同步更新 `docs/`。

## Windows 本地开发命令速查

- 全套静态检查、单元测试、前端构建内嵌与后端编译：
  `powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 all`
- 分步子命令：`lint`（`gofmt -l .` + `go vet ./...`）、`test`、`web`、`build`、`clean`。
- 若终端未刷新 `PATH`，Go 1.27.3 位于 `$env:USERPROFILE\sdk\go1.27.3\bin`。
- 本地实测：`python tests/manual/fake_llm.py` 配合 `tests/manual/config.yaml` 启动 `.\bin\buugoo.exe serve -c config.yaml`（访问 `http://127.0.0.1:18080`）。
- 全仓文本文件统一使用 LF（`\n`）换行，提交前确认 `gofmt -l .` 无输出。
