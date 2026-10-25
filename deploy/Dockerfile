# Buugoo 镜像（多架构）。本地构建：
#   docker build -f deploy/Dockerfile -t buugoo .
# 多架构（发布流程使用）：
#   docker buildx build --platform linux/amd64,linux/arm64 -f deploy/Dockerfile -t ghcr.io/hugehardjiang/buugoo .
#
# 构建阶段运行在构建机自身的架构上（--platform=$BUILDPLATFORM），由 Go 交叉编译到目标架构，
# 不需要 QEMU 模拟，速度快。
#
# 国内网络可指定镜像源：
#   --build-arg GOPROXY=https://goproxy.cn,direct --build-arg NPM_REGISTRY=https://registry.npmmirror.com

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
ARG NPM_REGISTRY=https://registry.npmjs.org
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund --registry "${NPM_REGISTRY}"
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ ./internal/web/dist/
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/HugehardJiang/buugoo/internal/buildinfo.Version=${VERSION} -X github.com/HugehardJiang/buugoo/internal/buildinfo.Commit=${COMMIT} -X github.com/HugehardJiang/buugoo/internal/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /out/buugoo ./cmd/buugoo

FROM alpine:3.22
# alpine 基础镜像已包含 CA 证书；时区数据已内嵌在二进制中（time/tzdata），无需额外安装软件包。
RUN addgroup -S -g 10001 buugoo && adduser -S -u 10001 -G buugoo -h /data buugoo \
 && mkdir -p /data && chown buugoo:buugoo /data
COPY --from=build /out/buugoo /usr/local/bin/buugoo
USER buugoo
WORKDIR /data
ENV BUUGOO_DATA_DIR=/data \
    BUUGOO_SERVER__LISTEN=0.0.0.0:8080 \
    BUUGOO_UPDATE__CHECK_ON_START=false \
    TZ=Asia/Shanghai
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["buugoo"]
CMD ["serve"]
