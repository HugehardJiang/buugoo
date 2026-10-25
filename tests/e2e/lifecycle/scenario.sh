#!/usr/bin/env bash
# 在 systemd 容器内运行（root）。镜像站：http://127.0.0.1:9000（/srv/mirror）。
set -euo pipefail
MIRROR=http://127.0.0.1:9000
INSTALL=/srv/mirror/v0.0.1/install.sh
UNINSTALL=/srv/mirror/v0.0.1/uninstall.sh
CONFIG=/etc/buugoo/config.yaml

step() { printf '\n\033[1m===== %s =====\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; exit 1; }
pass() { printf '\033[32mPASS: %s\033[0m\n' "$*"; }
health_version() {
  for _ in $(seq 1 30); do
    v="$(curl -fsS http://127.0.0.1:8080/healthz 2>/dev/null | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
    [ -n "$v" ] && { echo "$v"; return; }
    sleep 1
  done
  echo "unreachable"
}
expect_version() {
  local got; got="$(health_version)"
  [ "$got" = "$1" ] || fail "服务版本应为 $1，实际 $got"
  [ "$(buugoo version --short)" = "$1" ] || fail "二进制版本应为 $1"
  pass "运行中的版本为 $1"
}
set_latest() { echo "$1" > /srv/mirror/latest; }

(cd /srv/mirror && python3 -m http.server 9000 >/tmp/mirror.log 2>&1 &)
sleep 1

step "1. 全新安装 v0.0.1（注册服务）"
set_latest v0.0.1
bash "$INSTALL" -y --service --mirror "$MIRROR"
systemctl is-active --quiet buugoo || fail "服务未运行"
[ "$(stat -c %U /var/lib/buugoo/buugoo.db)" = buugoo ] || fail "数据库文件应归 buugoo 用户所有"
ps -o user= -C buugoo | grep -qx buugoo || fail "服务不应以 root 运行"
expect_version v0.0.1
# 写入一条消息作为数据标记（未配置模型时回复会失败，但用户消息会保存）。
curl -s -X POST -H 'Content-Type: application/json' -d '{"content":"生命周期测试标记"}' http://127.0.0.1:8080/api/v1/chat >/dev/null || true
curl -fsS http://127.0.0.1:8080/api/v1/messages | grep -q 生命周期测试标记 || fail "写入数据失败"
pass "数据已写入"
CONFIG_SUM="$(sha256sum "$CONFIG")"

step "2. 重复执行安装脚本（幂等）"
bash "$INSTALL" -y --mirror "$MIRROR"
expect_version v0.0.1
[ "$(sha256sum "$CONFIG")" = "$CONFIG_SUM" ] || fail "重复安装不应覆盖配置"
pass "配置未被覆盖"

step "3. SHA256 校验失败时中止"
cp -r /srv/mirror/v0.0.2 /srv/mirror/v9.9.9
mv /srv/mirror/v9.9.9/buugoo_0.0.2_linux_amd64.tar.gz /srv/mirror/v9.9.9/buugoo_9.9.9_linux_amd64.tar.gz
sed -i 's/0.0.2/9.9.9/' /srv/mirror/v9.9.9/checksums.txt
echo tampered >> /srv/mirror/v9.9.9/buugoo_9.9.9_linux_amd64.tar.gz
if buugoo update --version v9.9.9 -y --mirror "$MIRROR"; then fail "篡改的包应被拒绝"; fi
expect_version v0.0.1
pass "篡改的包被拒绝，版本不变"

step "4. 检查更新"
set_latest v0.0.2
buugoo update --check --mirror "$MIRROR" | tee /tmp/check.out
grep -q v0.0.2 /tmp/check.out || fail "应提示有新版本 v0.0.2"
expect_version v0.0.1

step "5. 更新到 v0.0.2"
buugoo update -y --mirror "$MIRROR"
expect_version v0.0.2
ls /var/lib/buugoo/backups/*.db >/dev/null || fail "更新前应自动备份数据库"
[ -x /usr/local/bin/buugoo.prev ] || fail "应保留上一个版本"
curl -fsS http://127.0.0.1:8080/api/v1/messages | grep -q 生命周期测试标记 || fail "更新后数据丢失"
pass "更新后数据完好"
grep -q '"version": "v0.0.2"' /etc/buugoo/install.json || fail "安装清单应记录新版本"

step "6. 更新到有问题的 v0.0.3，应自动回滚"
if buugoo update --version v0.0.3 -y --mirror "$MIRROR"; then fail "v0.0.3 迁移必定失败，更新应返回错误"; fi
expect_version v0.0.2
systemctl is-active --quiet buugoo || fail "回滚后服务应恢复运行"
curl -fsS http://127.0.0.1:8080/api/v1/messages | grep -q 生命周期测试标记 || fail "自动回滚后数据丢失"
pass "自动回滚成功"

step "7. 手动回滚到上一个版本"
buugoo update --rollback -y
expect_version v0.0.1
step "7b. 再回滚一次可以切回 v0.0.2"
buugoo update --rollback -y
expect_version v0.0.2

step "8. 重新执行安装脚本完成更新"
buugoo update --rollback -y
expect_version v0.0.1
bash "$INSTALL" -y --mirror "$MIRROR"
expect_version v0.0.2

step "9. 卸载（保留配置和数据）"
buugoo uninstall -y | tee /tmp/uninstall.out
[ ! -e /usr/local/bin/buugoo ] || fail "二进制应被删除"
[ ! -e /etc/systemd/system/buugoo.service ] || fail "服务应被移除"
systemctl is-active --quiet buugoo && fail "服务应已停止"
[ -f "$CONFIG" ] || fail "配置应保留"
[ -f /var/lib/buugoo/buugoo.db ] || fail "数据应保留"
grep -q /var/lib/buugoo /tmp/uninstall.out || fail "应打印保留的数据位置"
pass "卸载完成，配置与数据保留"

step "10. 重新安装，沿用原有数据"
bash "$INSTALL" -y --service --mirror "$MIRROR"
expect_version v0.0.2
[ "$(sha256sum "$CONFIG")" = "$CONFIG_SUM" ] || fail "重新安装不应覆盖配置"
curl -fsS http://127.0.0.1:8080/api/v1/messages | grep -q 生命周期测试标记 || fail "重新安装后数据丢失"
pass "重新安装后数据完好"

step "11. --purge 非交互模式必须加 -y"
if buugoo uninstall --purge < /dev/null; then fail "缺少 -y 时 --purge 应拒绝执行"; fi
[ -x /usr/local/bin/buugoo ] || fail "拒绝执行时不应删除任何内容"
pass "缺少 -y 时拒绝执行"

step "12. 用独立卸载脚本彻底删除"
bash "$UNINSTALL" --purge -y
[ ! -e /usr/local/bin/buugoo ] && [ ! -e /etc/buugoo ] && [ ! -e /var/lib/buugoo ] || fail "应全部删除"
id buugoo >/dev/null 2>&1 && fail "系统用户应被删除"
pass "已彻底删除"

step "13. 普通用户安装 / 更新 / 卸载"
useradd -m tester
su - tester -c "bash $INSTALL -y --mirror $MIRROR --version v0.0.1"
su - tester -c '~/.local/bin/buugoo version --short' | grep -qx v0.0.1 || fail "普通用户安装失败"
[ -f /home/tester/.config/buugoo/config.yaml ] || fail "用户配置目录不正确"
su - tester -c "~/.local/bin/buugoo update -y --mirror $MIRROR"
su - tester -c '~/.local/bin/buugoo version --short' | grep -qx v0.0.2 || fail "普通用户更新失败"
su - tester -c '~/.local/bin/buugoo uninstall -y'
[ ! -e /home/tester/.local/bin/buugoo ] || fail "普通用户卸载失败"
[ -d /home/tester/.local/share/buugoo ] || fail "普通用户卸载应保留数据"
pass "普通用户流程通过"

printf '\n\033[32m全部通过\033[0m\n'
