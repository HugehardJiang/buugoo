#!/usr/bin/env bash
# 启动一个临时 MySQL 容器，运行数据库相关测试后删除容器。
# 用法：scripts/test-mysql.sh [go test 额外参数]
set -euo pipefail

NAME="buugoo-test-mysql-$$"
PORT="${BUUGOO_TEST_MYSQL_PORT:-33061}"
IMAGE="${BUUGOO_TEST_MYSQL_IMAGE:-mysql:8.4}"
PASS="buugoo-test"

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$NAME" -p "127.0.0.1:${PORT}:3306" \
  -e MYSQL_ROOT_PASSWORD="$PASS" -e MYSQL_DATABASE=buugoo_test \
  "$IMAGE" >/dev/null

echo "等待 MySQL 就绪..."
for _ in $(seq 1 60); do
  if docker exec "$NAME" mysql -uroot -p"$PASS" -e "SELECT 1" buugoo_test >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

export BUUGOO_TEST_MYSQL_DSN="root:${PASS}@tcp(127.0.0.1:${PORT})/buugoo_test"
go test -count=1 "$@" ./internal/store/... ./internal/app/...
