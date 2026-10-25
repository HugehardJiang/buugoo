#!/usr/bin/env bash
# 生成与 GoReleaser 相同布局的本地"发布"，供生命周期测试使用：
#   <out>/<tag>/buugoo_<ver>_<os>_<arch>.tar.gz
#   <out>/<tag>/checksums.txt
# 用法：make-release.sh <tag> <out> [broken]
#   broken：生成一个能报告版本号、但数据库迁移必定失败的包，用于测试自动回滚。
set -euo pipefail
TAG="$1"; OUT="$2"; MODE="${3:-}"
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
OS="${E2E_GOOS:-linux}"; ARCH="${E2E_GOARCH:-amd64}"
DIR="$OUT/$TAG"; WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$DIR"
if [ "$MODE" = broken ]; then
  cat > "$WORK/buugoo" <<EOS
#!/bin/sh
case "\$1" in
  version) echo "$TAG" ;;
  migrate) echo "模拟迁移失败" >&2; exit 1 ;;
  *) echo "broken build" >&2; exit 1 ;;
esac
EOS
  chmod +x "$WORK/buugoo"
else
  EXE=buugoo; [ "$OS" = windows ] && EXE=buugoo.exe
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath \
    -ldflags "-s -w -X github.com/HugehardJiang/buugoo/internal/buildinfo.Version=$TAG" \
    -o "$WORK/$EXE" ./cmd/buugoo)
fi
cp "$ROOT/LICENSE" "$WORK/"
if [ "$OS" = windows ]; then
  ASSET="buugoo_${TAG#v}_${OS}_${ARCH}.zip"
  # Windows（Git Bash）上的 Python 不认识 /tmp 这类路径，需要转换为原生路径。
  native() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else echo "$1"; fi; }
  PY="$(command -v python3 || command -v python)"
  "$PY" -c "import os,sys,zipfile; z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED); [z.write(f, os.path.basename(f)) for f in sys.argv[2:]]; z.close()" \
    "$(native "$DIR/$ASSET")" "$(native "$WORK/buugoo.exe")" "$(native "$WORK/LICENSE")"
else
  ASSET="buugoo_${TAG#v}_${OS}_${ARCH}.tar.gz"
  tar -czf "$DIR/$ASSET" -C "$WORK" buugoo LICENSE
fi
(cd "$DIR" && sha256sum "$ASSET" >> checksums.txt)
cp "$ROOT"/scripts/install.* "$ROOT"/scripts/uninstall.* "$DIR/"
echo "$DIR/$ASSET"
