# Buugoo Makefile
SHELL   := /bin/bash
BINARY  := buugoo
PKG     := github.com/HugehardJiang/buugoo
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(PKG)/internal/buildinfo.Date=$(DATE)
GOFLAGS := -trimpath
export CGO_ENABLED := 0

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build run test test-mysql lint web web-dev cross clean docker e2e-lifecycle help

all: web build ## 构建前端与后端

build: ## 构建当前平台的二进制到 bin/
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/buugoo

run: build ## 构建并启动服务
	./bin/$(BINARY) serve

test: ## 运行单元测试（SQLite）
	go test -race ./...

test-mysql: ## 启动临时 MySQL 容器并运行数据库测试
	./scripts/test-mysql.sh

lint: ## 静态检查
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "以下文件未格式化:"; gofmt -l .; exit 1)

web: ## 构建前端并复制到 internal/web/dist（供 embed 打包）
	cd web && npm ci && npm run build
	rm -rf internal/web/dist && mkdir -p internal/web/dist
	cp -r web/dist/. internal/web/dist/
	touch internal/web/dist/.gitkeep

web-dev: ## 启动前端开发服务器（代理到 127.0.0.1:8080）
	cd web && npm run dev

cross: ## 交叉编译所有平台到 dist/
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		echo "==> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)_$${os}_$${arch}/$(BINARY)$$ext ./cmd/buugoo || exit 1; \
	done

docker: ## 构建 Docker 镜像
	docker build -f deploy/Dockerfile -t buugoo:$(VERSION) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

e2e-lifecycle: ## 在 Docker 中测试 安装→更新→回滚→卸载→重装
	./tests/e2e/lifecycle/run.sh

clean: ## 清理构建产物
	rm -rf bin dist

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
