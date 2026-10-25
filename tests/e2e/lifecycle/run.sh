#!/usr/bin/env bash
# 在干净的 systemd 容器中测试：安装 → 更新 → 回滚 → 卸载 → 重新安装。
# 用法：tests/e2e/lifecycle/run.sh            （构建测试镜像）
#       E2E_IMAGE=<已有镜像> tests/e2e/lifecycle/run.sh
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
MIRROR="$(mktemp -d)"
NAME="buugoo-lifecycle-$$"
cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; rm -rf "$MIRROR"; }
trap cleanup EXIT

echo "构建测试发布包…"
"$HERE/make-release.sh" v0.0.1 "$MIRROR" >/dev/null
"$HERE/make-release.sh" v0.0.2 "$MIRROR" >/dev/null
"$HERE/make-release.sh" v0.0.3 "$MIRROR" broken >/dev/null
chmod -R a+rX "$MIRROR"

IMAGE="${E2E_IMAGE:-}"
if [ -z "$IMAGE" ]; then
  IMAGE=buugoo-lifecycle:test
  docker build -q -t "$IMAGE" "$HERE" >/dev/null
fi

docker run -d --name "$NAME" --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  -v "$MIRROR":/srv/mirror -v "$HERE/scenario.sh":/scenario.sh:ro \
  "$IMAGE" >/dev/null
for _ in $(seq 1 30); do
  state="$(docker exec "$NAME" systemctl is-system-running 2>/dev/null || true)"
  case "$state" in running|degraded) break ;; esac
  sleep 1
done
if ! docker exec "$NAME" bash /scenario.sh; then
  echo "---- journalctl -u buugoo ----"
  docker exec "$NAME" journalctl -u buugoo --no-pager -n 60 || true
  exit 1
fi
