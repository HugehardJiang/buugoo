# 生命周期测试用的 systemd 容器。
FROM jrei/systemd-ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
