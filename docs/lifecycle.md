# 安装、更新与卸载

## 发布产物

打 `v*` tag 后 GitHub Actions 运行 GoReleaser，发布到 GitHub Releases：

```
buugoo_<版本>_<os>_<arch>.tar.gz   linux/darwin（amd64、arm64）
buugoo_<版本>_windows_amd64.zip
checksums.txt                      SHA256
install.sh install.ps1 uninstall.sh uninstall.ps1
```

同时构建多架构镜像推送到 `ghcr.io/hugehardjiang/buugoo`；配置了阿里云 ACR 的 secrets
（`ALIYUN_REGISTRY`、`ALIYUN_NAMESPACE`、`ALIYUN_USERNAME`、`ALIYUN_PASSWORD`）时同时推送到 ACR。

## 下载镜像

`--mirror <地址>`、环境变量 `BUUGOO_MIRROR` 或配置 `update.mirror`。镜像必须提供与 GitHub Releases 相同的布局：

```
<mirror>/latest                    文本，内容为最新版本号，例如 v0.2.0
<mirror>/<tag>/<文件名>             例如 <mirror>/v0.2.0/buugoo_0.2.0_linux_amd64.tar.gz
```

把某个版本的 Release 附件原样放到 `<tag>/` 目录下，再更新 `latest` 即可。

## 目录与安装清单

| 安装方式 | 二进制 | 配置 | 数据 |
| --- | --- | --- | --- |
| Linux root | `/usr/local/bin` | `/etc/buugoo` | `/var/lib/buugoo` |
| macOS root | `/usr/local/bin` | `/usr/local/etc/buugoo` | `/usr/local/var/buugoo` |
| Linux / macOS 普通用户 | `~/.local/bin` | `~/.config/buugoo` | `~/.local/share/buugoo` |
| Windows | `%LOCALAPPDATA%\Programs\buugoo` | `%APPDATA%\buugoo` | `%LOCALAPPDATA%\buugoo` |

安装脚本在配置目录写入 `install.json`（安装范围、二进制路径、目录、是否注册服务、服务用户、版本、镜像、
上一个版本、最近一次备份）。`buugoo update` 与 `buugoo uninstall` 依据它决定操作对象。

Linux root 安装时创建系统用户 `buugoo`（nologin），systemd 服务以该用户运行，并开启
`NoNewPrivileges`、`ProtectSystem=strict`、`ProtectHome`、`PrivateTmp`，只允许写数据目录。

## 更新流程（buugoo update）

1. 获取目标版本（最新或 `--version`），已是最新则退出；`--check` 只报告。
2. 下载发布包与 `checksums.txt`，校验 SHA256，失败立即中止；解压后确认新程序能运行且版本号正确。
3. 备份数据库到 `<数据目录>/backups/`：SQLite 用 `VACUUM INTO` 生成一致性快照；MySQL 调用 `mysqldump`
   （密码通过环境变量传入），找不到 `mysqldump` 时提示手动备份并要求确认。
4. 停止服务 → 当前二进制改名为 `.prev` → 放入新版本（Windows 下先改名再写入）。
5. 用新版本执行 `migrate`。
6. 启动服务，轮询 `/healthz` 直到返回新版本号（30 秒）。
7. 第 4–6 步任一步失败：恢复旧二进制和 SQLite 备份，重新启动服务（MySQL 会打印恢复命令）。
8. 写回 `install.json`。

`buugoo update --rollback` 在当前版本与 `.prev` 之间切换（可以再切回来）。是否恢复数据库备份会询问；
`-y` 不会自动同意恢复，需要显式加 `--restore-db`。

安装脚本重复执行时，如果已安装其他版本，会调用 `buugoo update` 完成更新，复用同样的备份与回滚逻辑。

Docker 部署（容器中没有安装清单）会拒绝自更新，提示使用 `docker compose pull && docker compose up -d`。

## 卸载

`buugoo uninstall` 与 `uninstall.sh` / `uninstall.ps1`：停止并移除服务、删除二进制，保留配置和数据并打印位置。
`--purge` 同时删除配置、数据和系统用户，必须确认；非交互模式下需同时加 `-y`。

## 测试

`make e2e-lifecycle`（`tests/e2e/lifecycle/run.sh`）在带 systemd 的容器中依次测试：全新安装、重复安装、
篡改的包被拒绝、检查更新、更新、迁移失败自动回滚、手动回滚、用安装脚本更新、卸载保留数据、
重新安装沿用数据、`--purge` 必须确认、彻底卸载、普通用户安装。CI 中还会在 Windows 上测试 `install.ps1`。
