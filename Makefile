# astro-mountain Makefile
#
# 新人友好：编译/测试前自动执行 `go generate ./internal/config`，
# 生成 internal/config/defaults/*.json（内置默认配置）。该目录已被 .gitignore 排除，
# 全新 clone 后不存在，必须先用 go:generate 从仓库根 configs/ 派生，
# 否则裸 `go build` 会因 embed 源缺失而失败。用 `make build` 即规避此坑。

GO     ?= go
BINARY ?= astro-mountain

.PHONY: all build generate test vet fmt clean

all: build

# 生成内置默认配置（configs/ -> internal/config/defaults/）
generate:
	$(GO) generate ./internal/config

# 编译（先确保内置默认配置已生成）
build: generate
	CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd/astro-mountain

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -f $(BINARY)
