SHELL := /bin/sh

GO ?= go
APP_NAME := goclaw
CMD_PATH := ./cmd/goclaw
PKGS := ./...
OUT_DIR := bin
BIN_PATH := $(OUT_DIR)/$(APP_NAME)

.PHONY: help run build build-bin test test-middleware fmt vet tidy clean

help:
	@printf "可用目标:\n"
	@printf "  make run              # 本地运行服务\n"
	@printf "  make build            # 编译所有包\n"
	@printf "  make build-bin        # 生成可执行文件到 bin/\n"
	@printf "  make test             # 运行全部测试\n"
	@printf "  make test-middleware  # 仅运行 middleware 测试\n"
	@printf "  make fmt              # 格式化 Go 代码\n"
	@printf "  make vet              # 运行 go vet\n"
	@printf "  make tidy             # 整理 go.mod/go.sum\n"
	@printf "  make clean            # 清理构建产物\n"

run:
	$(GO) run $(CMD_PATH)

build:
	$(GO) build $(PKGS)

build-bin:
	mkdir -p $(OUT_DIR)
	$(GO) build -o $(BIN_PATH) $(CMD_PATH)

test:
	$(GO) test $(PKGS)

test-middleware:
	$(GO) test ./internal/middleware/...

fmt:
	$(GO) fmt $(PKGS)

vet:
	$(GO) vet $(PKGS)

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(OUT_DIR)
