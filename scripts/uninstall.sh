#!/usr/bin/env bash
# Buugoo（布谷）卸载脚本（Linux、macOS）。buugoo 程序本身损坏时也能使用。
#
#   curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/uninstall.sh | sudo bash
#
# 默认：停止并移除 systemd 服务、删除二进制，保留配置和数据并打印它们的位置。
# 参数：
#   --purge    同时删除配置和数据（不可恢复，需要确认；非交互模式下需同时加 -y）
#   -y, --yes  非交互模式
set -euo pipefail

PURGE=0
ASSUME_YES=0
while [ $# -gt 0 ]; do
  case "$1" in
    --purge) PURGE=1; shift ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    -h|--help) sed -n '2,10p' "$0" 2>/dev/null || true; exit 0 ;;
    *) echo "未知参数 $1" >&2; exit 1 ;;
  esac
done

if [ "$(id -u)" = 0 ]; then
  if [ "$(uname -s)" = Darwin ]; then CONFIG_DIR=/usr/local/etc/buugoo; else CONFIG_DIR=/etc/buugoo; fi
else
  CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/buugoo"
fi
MANIFEST="$CONFIG_DIR/install.json"

json() { sed -n "s/.*\"$1\": *\"\{0,1\}\([^\",]*\)\"\{0,1\}.*/\1/p" "$MANIFEST" | head -1; }
if [ -f "$MANIFEST" ]; then
  BIN="$(json binary)"; DATA_DIR="$(json data_dir)"; SERVICE_USER="$(json service_user)"
  CONFIG_DIR="$(json config_dir)"
else
  echo "未找到安装清单 $MANIFEST，按默认位置卸载。" >&2
  if [ "$(id -u)" = 0 ]; then
    BIN=/usr/local/bin/buugoo; DATA_DIR=/var/lib/buugoo; SERVICE_USER=buugoo
  else
    BIN="$HOME/.local/bin/buugoo"; DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/buugoo"; SERVICE_USER=""
  fi
fi

if [ "$PURGE" = 1 ]; then
  echo "将永久删除配置目录 $CONFIG_DIR 和数据目录 $DATA_DIR（包括所有聊天记录、记忆和备份），无法恢复。"
  if [ "$ASSUME_YES" != 1 ]; then
    if ! { [ -r /dev/tty ] && : < /dev/tty; } 2>/dev/null; then
      echo "非交互模式下 --purge 需要同时加 -y" >&2; exit 1
    fi
    printf '确定继续吗？输入 yes 确认：' > /dev/tty
    read -r ans < /dev/tty || ans=""
    [ "$ans" = yes ] || { echo "已取消"; exit 1; }
  fi
fi

RESULT=()
UNIT=/etc/systemd/system/buugoo.service
if [ -f "$UNIT" ]; then
  [ "$(id -u)" = 0 ] || { echo "移除系统服务需要 root 权限，请使用 sudo" >&2; exit 1; }
  systemctl disable --now buugoo >/dev/null 2>&1 || true
  rm -f "$UNIT"
  systemctl daemon-reload || true
  RESULT+=("✓ 已停止并移除 systemd 服务 buugoo")
fi
if [ -e "$BIN" ] || [ -e "$BIN.prev" ]; then
  rm -f "$BIN" "$BIN.prev" "$BIN.prev.bak" "$BIN.new" "$BIN.swap"
  RESULT+=("✓ 已删除二进制 $BIN")
fi

if [ "$PURGE" = 1 ]; then
  for d in "$CONFIG_DIR" "$DATA_DIR"; do
    if [ -n "$d" ] && [ "$d" != / ] && [ -e "$d" ]; then rm -rf "$d"; RESULT+=("✓ 已删除 $d"); fi
  done
  if [ -n "$SERVICE_USER" ] && id "$SERVICE_USER" >/dev/null 2>&1 && [ "$(id -u)" = 0 ]; then
    userdel "$SERVICE_USER" 2>/dev/null && RESULT+=("✓ 已删除系统用户 $SERVICE_USER")
  fi
else
  rm -f "$MANIFEST"
fi

echo "卸载结果："
if [ ${#RESULT[@]} -eq 0 ]; then echo "  没有找到需要卸载的内容"; else printf '  %s\n' "${RESULT[@]}"; fi
if [ "$PURGE" != 1 ]; then
  echo "已保留："
  echo "  配置 $CONFIG_DIR"
  echo "  数据 $DATA_DIR"
  echo "重新安装后会继续使用这些配置和数据。如需彻底删除，请加 --purge 重新运行。"
fi
