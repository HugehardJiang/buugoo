#!/usr/bin/env bash
# Buugoo（布谷）一键安装 / 更新脚本（Linux、macOS）
#
#   curl -fsSL https://github.com/HugehardJiang/buugoo/releases/latest/download/install.sh | bash
#   curl -fsSL .../install.sh | sudo bash -s -- -y --service
#
# 参数：
#   --version <v>      安装指定版本（默认最新版本）
#   --prefix <dir>     二进制安装目录（默认 root 为 /usr/local/bin，否则 ~/.local/bin）
#   --mirror <url>     下载镜像地址（也可用环境变量 BUUGOO_MIRROR），布局与 GitHub Releases 相同：
#                        <mirror>/latest、<mirror>/<tag>/<文件名>
#   --service          注册 systemd 服务并开机自启（仅 Linux + root）
#   --no-service       不注册服务
#   -y, --yes          非交互模式，所有提问使用默认答案
#   --force            即使已是该版本也重新安装
#   -h, --help         显示帮助
#
# 重复执行是安全的：已安装时会转为调用 buugoo update 完成更新（含备份、迁移、失败回滚），
# 已存在的配置文件不会被覆盖。
set -euo pipefail

REPO="HugehardJiang/buugoo"
SERVICE_NAME="buugoo"
SERVICE_USER="buugoo"

VERSION=""
PREFIX=""
MIRROR="${BUUGOO_MIRROR:-}"
WANT_SERVICE=""   # yes | no | 空（询问）
ASSUME_YES=0
FORCE=0

# ---------- 输出 ----------
if [ -t 1 ]; then
  C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'; C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RESET=$'\033[0m'
else
  C_BOLD=""; C_DIM=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_RESET=""
fi
info() { printf '%s==>%s %s\n' "$C_BOLD" "$C_RESET" "$*"; }
ok()   { printf '%s✓%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
warn() { printf '%s!%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die()  { printf '%s错误：%s%s\n' "$C_RED" "$*" "$C_RESET" >&2; exit 1; }

usage() {
  cat <<'EOF_USAGE'
用法：install.sh [--version v] [--prefix 目录] [--mirror 地址] [--service|--no-service] [-y] [--force]
  --version <v>    安装指定版本（默认最新版本）
  --prefix <dir>   二进制安装目录（默认 root 为 /usr/local/bin，否则 ~/.local/bin）
  --mirror <url>   下载镜像地址（也可用环境变量 BUUGOO_MIRROR）
  --service        注册 systemd 服务并开机自启（仅 Linux + root）
  --no-service     不注册服务
  -y, --yes        非交互模式
  --force          即使已是该版本也重新安装
EOF_USAGE
}

# ask <问题> <默认 y|n>：从终端读取回答（脚本通过管道执行时 stdin 不是终端）。
ask() {
  local q="$1" def="$2" ans="" hint="[y/N]"
  [ "$def" = "y" ] && hint="[Y/n]"
  if [ "$ASSUME_YES" = 1 ] || ! { [ -r /dev/tty ] && : < /dev/tty; } 2>/dev/null; then
    [ "$def" = "y" ]
    return
  fi
  printf '%s %s ' "$q" "$hint" > /dev/tty
  read -r ans < /dev/tty || ans=""
  ans="${ans:-$def}"
  case "$ans" in y|Y|yes|YES|是) return 0 ;; *) return 1 ;; esac
}

# ---------- 参数 ----------
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --prefix) PREFIX="${2:-}"; shift 2 ;;
    --prefix=*) PREFIX="${1#*=}"; shift ;;
    --mirror) MIRROR="${2:-}"; shift 2 ;;
    --mirror=*) MIRROR="${1#*=}"; shift ;;
    --service) WANT_SERVICE=yes; shift ;;
    --no-service) WANT_SERVICE=no; shift ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数 $1（使用 --help 查看帮助）" ;;
  esac
done
MIRROR="${MIRROR%/}"

# ---------- 环境检测 ----------
need() { command -v "$1" >/dev/null 2>&1 || die "缺少命令 $1，请先安装"; }
need tar
need uname
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1"; }
  fetch_stdout() { curl -fsSL --retry 3 --connect-timeout 15 "$1"; }
  latest_redirect() { curl -fsSI --connect-timeout 15 "https://github.com/$REPO/releases/latest" | tr -d '\r' | awk 'tolower($1)=="location:"{print $2}' | tail -1; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q --tries=3 -O "$2" "$1"; }
  fetch_stdout() { wget -q --tries=3 -O - "$1"; }
  latest_redirect() { wget -q -S --max-redirect=0 -O /dev/null "https://github.com/$REPO/releases/latest" 2>&1 | awk 'tolower($1)=="location:"{print $2}' | tail -1; }
else
  die "需要 curl 或 wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "需要 sha256sum 或 shasum 来校验下载文件"
fi

OS="$(uname -s)"
case "$OS" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) die "不支持的操作系统 $OS（Windows 请使用 install.ps1）" ;;
esac
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "不支持的 CPU 架构 $ARCH（支持 amd64、arm64）" ;;
esac

if [ "$(id -u)" = 0 ]; then SCOPE=system; else SCOPE=user; fi

if [ "$SCOPE" = system ]; then
  BIN_DIR="${PREFIX:-/usr/local/bin}"
  if [ "$OS" = darwin ]; then
    CONFIG_DIR=/usr/local/etc/buugoo; DATA_DIR=/usr/local/var/buugoo
  else
    CONFIG_DIR=/etc/buugoo; DATA_DIR=/var/lib/buugoo
  fi
else
  BIN_DIR="${PREFIX:-$HOME/.local/bin}"
  CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/buugoo"
  DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/buugoo"
fi
BIN="$BIN_DIR/buugoo"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
MANIFEST="$CONFIG_DIR/install.json"

# ---------- 版本 ----------
resolve_version() {
  if [ -n "$VERSION" ]; then
    case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
    return
  fi
  if [ -n "$MIRROR" ]; then
    VERSION="$(fetch_stdout "$MIRROR/latest" | tr -d '[:space:]')" || die "无法从镜像 $MIRROR 获取最新版本"
  else
    local loc
    loc="$(latest_redirect || true)"
    VERSION="${loc##*/tag/}"
    [ -n "$loc" ] && [ "$VERSION" != "$loc" ] || die "无法获取最新版本（GitHub 访问失败？可用 --mirror 指定镜像，或用 --version 指定版本）"
  fi
  case "$VERSION" in v[0-9]*) ;; *) die "获取到的版本号无效：$VERSION" ;; esac
}
asset_url() {
  if [ -n "$MIRROR" ]; then echo "$MIRROR/$VERSION/$1"; else echo "https://github.com/$REPO/releases/download/$VERSION/$1"; fi
}

info "布谷 Buugoo 安装程序（$OS/$ARCH，$( [ "$SCOPE" = system ] && echo 系统级安装 || echo 当前用户安装 )）"
resolve_version
info "目标版本：$VERSION"

# ---------- 已安装：转为更新 ----------
if [ -x "$BIN" ] && [ -f "$MANIFEST" ] && [ "$FORCE" = 0 ]; then
  CURRENT="$("$BIN" version --short 2>/dev/null || true)"
  if [ "$CURRENT" = "$VERSION" ]; then
    ok "已安装 $VERSION，无需更新"
    INSTALLED_NOTHING=1
  elif [ -n "$CURRENT" ]; then
    info "检测到已安装 $CURRENT，通过 buugoo update 更新到 $VERSION"
    UPDATE_ARGS=(update --version "$VERSION" --config "$CONFIG_FILE" -y)
    [ -n "$MIRROR" ] && UPDATE_ARGS+=(--mirror "$MIRROR")
    "$BIN" "${UPDATE_ARGS[@]}"
    ok "更新完成"
    exit 0
  fi
fi

# ---------- 下载与校验 ----------
if [ "${INSTALLED_NOTHING:-0}" != 1 ]; then
  ASSET="buugoo_${VERSION#v}_${OS}_${ARCH}.tar.gz"
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  info "下载 $(asset_url "$ASSET")"
  fetch "$(asset_url "$ASSET")" "$TMP/$ASSET" || die "下载失败：$(asset_url "$ASSET")"
  fetch "$(asset_url checksums.txt)" "$TMP/checksums.txt" || die "下载校验文件失败：$(asset_url checksums.txt)"
  WANT_SUM="$(awk -v f="$ASSET" '$2==f || $2=="*"f {print $1}' "$TMP/checksums.txt")"
  [ -n "$WANT_SUM" ] || die "校验文件中没有 $ASSET，已中止"
  GOT_SUM="$(sha256 "$TMP/$ASSET")"
  if [ "$WANT_SUM" != "$GOT_SUM" ]; then
    die "SHA256 校验失败，文件可能被篡改或下载不完整，已中止安装
  期望：$WANT_SUM
  实际：$GOT_SUM"
  fi
  ok "SHA256 校验通过"
  tar -xzf "$TMP/$ASSET" -C "$TMP" buugoo || die "解压失败"
  "$TMP/buugoo" version --short >/dev/null 2>&1 || die "下载的程序无法在本机运行"

  mkdir -p "$BIN_DIR"
  # 先写临时文件再改名，避免覆盖正在运行的二进制时出错。
  install -m 0755 "$TMP/buugoo" "$BIN.new"
  mv -f "$BIN.new" "$BIN"
  ok "已安装 $BIN"
fi

# ---------- 系统用户、目录与配置 ----------
if [ "$SCOPE" = system ] && [ "$OS" = linux ]; then
  if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    NOLOGIN="$(command -v nologin || echo /usr/sbin/nologin)"
    useradd --system --home-dir "$DATA_DIR" --no-create-home --shell "$NOLOGIN" "$SERVICE_USER" \
      || die "创建系统用户 $SERVICE_USER 失败"
    ok "已创建系统用户 $SERVICE_USER（服务不以 root 运行）"
  fi
fi
mkdir -p "$CONFIG_DIR" "$DATA_DIR"
if [ -f "$CONFIG_FILE" ]; then
  ok "保留已有配置 $CONFIG_FILE"
else
  "$BIN" init --config "$CONFIG_FILE" --data-dir "$DATA_DIR" --scope "$SCOPE" >/dev/null
  ok "已生成配置 $CONFIG_FILE"
fi
if [ "$SCOPE" = system ] && [ "$OS" = linux ]; then
  chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
  chmod 0750 "$DATA_DIR"
  chown root:"$SERVICE_USER" "$CONFIG_FILE"
  chmod 0640 "$CONFIG_FILE"
fi
if [ "$SCOPE" = system ] && [ "$OS" = linux ]; then
  # 以服务用户身份迁移，保证数据库文件归属正确。
  if command -v runuser >/dev/null 2>&1; then
    runuser -u "$SERVICE_USER" -- "$BIN" migrate --config "$CONFIG_FILE" >/dev/null
  else
    "$BIN" migrate --config "$CONFIG_FILE" >/dev/null
    chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
  fi
else
  "$BIN" migrate --config "$CONFIG_FILE" >/dev/null
fi
ok "数据库已就绪"

# ---------- systemd 服务 ----------
SERVICE_INSTALLED=false
UNIT="/etc/systemd/system/$SERVICE_NAME.service"
if [ "$SCOPE" = system ] && [ "$OS" = linux ] && [ -d /run/systemd/system ]; then
  if [ -z "$WANT_SERVICE" ]; then
    if [ -f "$UNIT" ] || ask "是否注册为 systemd 服务（开机自启）？" y; then WANT_SERVICE=yes; else WANT_SERVICE=no; fi
  fi
  if [ "$WANT_SERVICE" = yes ]; then
    cat > "$UNIT" <<EOF
[Unit]
Description=Buugoo（布谷）
Documentation=https://github.com/$REPO
After=network-online.target mysql.service mariadb.service
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
ExecStart=$BIN serve --config $CONFIG_FILE
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable --now "$SERVICE_NAME" >/dev/null 2>&1 || die "启动服务失败，请查看 journalctl -u $SERVICE_NAME"
    SERVICE_INSTALLED=true
    ok "已注册并启动 systemd 服务 $SERVICE_NAME"
  fi
elif [ "$WANT_SERVICE" = yes ]; then
  warn "只有 Linux（systemd）且以 root 安装时才能注册服务，已跳过"
fi

# ---------- 安装清单 ----------
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
INSTALLED_AT="$NOW"
if [ -f "$MANIFEST" ]; then
  INSTALLED_AT="$(sed -n 's/.*"installed_at": *"\([^"]*\)".*/\1/p' "$MANIFEST" | head -1)"
  INSTALLED_AT="${INSTALLED_AT:-$NOW}"
fi
SVC_USER_JSON=""
[ "$SCOPE" = system ] && [ "$OS" = linux ] && SVC_USER_JSON="$SERVICE_USER"
cat > "$MANIFEST.tmp" <<EOF
{
  "scope": "$SCOPE",
  "bin_dir": "$BIN_DIR",
  "config_dir": "$CONFIG_DIR",
  "data_dir": "$DATA_DIR",
  "binary": "$BIN",
  "service": $SERVICE_INSTALLED,
  "service_user": "$SVC_USER_JSON",
  "version": "$VERSION",
  "mirror": "$MIRROR",
  "installed_at": "$INSTALLED_AT",
  "updated_at": "$NOW"
}
EOF
mv -f "$MANIFEST.tmp" "$MANIFEST"
chmod 0644 "$MANIFEST"

# ---------- 结果 ----------
LISTEN="$(sed -n 's/^  listen: *"\{0,1\}\([^"]*\)"\{0,1\}.*/\1/p' "$CONFIG_FILE" | head -1)"
PORT="${LISTEN##*:}"
HOST="${LISTEN%:*}"
case "$HOST" in ""|0.0.0.0|"[::]") HOST=127.0.0.1 ;; esac
echo
printf '%s布谷 %s 安装完成%s\n' "$C_BOLD" "$VERSION" "$C_RESET"
echo "  程序    $BIN"
echo "  配置    $CONFIG_FILE"
echo "  数据    $DATA_DIR"
echo "  地址    http://$HOST:${PORT:-8080}"
echo
echo "下一步："
echo "  1. 编辑配置文件，填写 llm.chat.api_key（DeepSeek）与 llm.embedding.api_key（硅基流动）"
if [ "$SERVICE_INSTALLED" = true ]; then
  echo "  2. 重启服务：sudo systemctl restart $SERVICE_NAME（日志：journalctl -u $SERVICE_NAME -f）"
else
  echo "  2. 启动服务：$BIN serve"
fi
echo "  3. 浏览器打开上面的地址，或在终端运行 buugoo chat"
echo "  更新：buugoo update    卸载：buugoo uninstall"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo; warn "$BIN_DIR 不在 PATH 中。请把下面这行加入 ~/.bashrc 或 ~/.zshrc：
    export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
