SHELL := /bin/bash
export GOTOOLCHAIN ?= go1.25.5
export GOFLAGS ?= -mod=readonly

.PHONY: help build build-local build-cross build-check run dev clean clean-dist version test test-race test-coverage test-e2e test-e2e-live fmt fmt-check lint mod-check verify

# 版本信息
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建参数
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)
BINARY_NAME := vdoc
OUTPUT_NAME := $(if $(BIN_NAME),$(BIN_NAME),$(BINARY_NAME))
BIN_DIR ?= bin

# 跨平台构建矩阵与产物目录
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
DIST_DIR ?= dist
CGO_ENABLED ?= 0
PACKAGE_FILES ?= README.md config.yaml.example
PACKAGE_DIRS ?= static

help: ## 显示帮助信息
	@echo "可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## 构建生产版本（CROSS=1 启用跨平台打包，BIN_NAME=xxx 指定二进制名称）
	@if [ "$(CROSS)" = "1" ]; then \
		$(MAKE) build-cross; \
	else \
		$(MAKE) build-local; \
	fi

build-local: ## 本地构建（默认）
	@echo "构建 $(OUTPUT_NAME)..."
	@mkdir -p "$(BIN_DIR)"
	@go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(OUTPUT_NAME)" .
	@echo "✓ 构建完成: $(BIN_DIR)/$(OUTPUT_NAME)"

build-cross: clean-dist ## 跨平台构建并打包到 dist/
	@echo "开始跨平台构建与打包: $(PLATFORMS)"
	@mkdir -p "$(DIST_DIR)"
	@set -e; \
	for platform in $(PLATFORMS); do \
		os="$${platform%/*}"; arch="$${platform#*/}"; extension=""; \
		[ "$$os" = "windows" ] && extension=".exe"; \
		output_dir="$(DIST_DIR)/$(OUTPUT_NAME)_$(VERSION)_$${os}_$${arch}"; \
		echo "- 构建 $$os/$$arch"; \
		mkdir -p "$$output_dir"; \
		GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=$(CGO_ENABLED) \
			go build -trimpath -ldflags "$(LDFLAGS) -s -w" -o "$$output_dir/$(OUTPUT_NAME)$$extension" .; \
		for file in $(PACKAGE_FILES); do [ -f "$$file" ] && cp "$$file" "$$output_dir/" || true; done; \
		for directory in $(PACKAGE_DIRS); do [ -d "$$directory" ] && cp -R "$$directory" "$$output_dir/" || true; done; \
		if command -v zip >/dev/null 2>&1 && [ "$$os" = "windows" ]; then \
			(cd "$(DIST_DIR)" && zip -q -r "$(OUTPUT_NAME)_$(VERSION)_$${os}_$${arch}.zip" "$$(basename "$$output_dir")"); \
		else \
			(cd "$(DIST_DIR)" && tar -czf "$(OUTPUT_NAME)_$(VERSION)_$${os}_$${arch}.tar.gz" "$$(basename "$$output_dir")"); \
		fi; \
		rm -rf "$$output_dir"; \
	done
	@echo "✓ 打包完成，产物位于 $(DIST_DIR)/"

build-check: ## 在临时目录验证构建，不保留产物
	@temp_dir="$$(mktemp -d "$${TMPDIR:-/tmp}/$(OUTPUT_NAME)-build.XXXXXX")"; \
	trap 'rm -rf "$$temp_dir"' EXIT INT TERM; \
	go build -trimpath -ldflags "$(LDFLAGS)" -o "$$temp_dir/$(OUTPUT_NAME)" .

run: build-local ## 构建并运行（生产模式）
	@"$(BIN_DIR)/$(OUTPUT_NAME)"

dev: build-local ## 构建并运行（开发模式）
	@"$(BIN_DIR)/$(OUTPUT_NAME)" --dev

clean: ## 清理构建文件
	@rm -rf "$(BIN_DIR)"
	@rm -f "$(OUTPUT_NAME)"

clean-dist: ## 清理打包产物（dist/）
	@rm -rf "$(DIST_DIR)"

version: ## 显示版本信息
	@echo "Version:    $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"

test: ## 使用 race detector 与随机顺序运行全量测试
	@go test -race -shuffle=on -count=1 ./...

test-race: test ## test 的兼容别名

test-coverage: ## 运行全量测试并显示覆盖率摘要
	@set -o pipefail; \
	OUT=$$(mktemp -t go-test-XXXXXX); \
	COVER=$$(mktemp -t go-cover-XXXXXX); \
	trap 'rm -f "$$OUT" "$$COVER"' EXIT; \
	if go test -v -shuffle=on -count=1 -coverprofile="$$COVER" -covermode=atomic ./... | tee "$$OUT"; then \
		STATUS=0; \
	else \
		STATUS=$$?; \
	fi; \
	PASS_PKGS=$$(grep -c '^ok[[:space:]]' "$$OUT" || true); \
	FAIL_PKGS=$$(grep -c '^FAIL[[:space:]]' "$$OUT" || true); \
	TOTAL_PKGS=$$((PASS_PKGS+FAIL_PKGS)); \
	PASS_TESTS=$$(grep -c '^--- PASS:' "$$OUT" || true); \
	FAIL_TESTS=$$(grep -c '^--- FAIL:' "$$OUT" || true); \
	SKIP_TESTS=$$(grep -c '^--- SKIP:' "$$OUT" || true); \
	if [ -f "$$COVER" ]; then \
		TOTAL_COV=$$(go tool cover -func="$$COVER" | awk '/^total:/ {print $$3}'); \
	else \
		TOTAL_COV="N/A"; \
	fi; \
	echo "测试汇总：包 总数=$$TOTAL_PKGS 通过=$$PASS_PKGS 失败=$$FAIL_PKGS"; \
	echo "用例汇总：通过=$$PASS_TESTS 失败=$$FAIL_TESTS 跳过=$$SKIP_TESTS"; \
	echo "总覆盖率：$$TOTAL_COV"; \
	exit $$STATUS

test-e2e: ## 运行 v0.1 REST+MCP E2E（默认内存；VDOC_E2E_LIVE=1 启用 PostgreSQL/RustFS）
	@./scripts/vdoc-e2e.sh all

test-e2e-live: ## 运行 PostgreSQL + RustFS live E2E（需要 VDOC_TEST_* 环境变量）
	@./scripts/vdoc-e2e.sh live

fmt: ## 格式化 Go 代码
	@gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check: ## 检查 Go 格式但不修改源码
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$files" ]; then \
		echo "以下 Go 文件需要 gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

lint: ## 运行 go vet
	@go vet ./...

mod-check: ## 检查 go.mod/go.sum 整洁性与下载模块完整性
	@go mod tidy -diff
	@go mod verify

verify: fmt-check lint test-race build-check mod-check ## 验证格式、静态检查、race 测试、构建和模块完整性

.DEFAULT_GOAL := help
