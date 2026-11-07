.PHONY: run build release install test vet

# 发布版本号：有 tag 用 tag（v1.0.0），否则 git describe / dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# 发布构建：去掉调试信息与 inspector，mygo 置为 production
RELEASE_FLAGS := -trimpath -buildvcs=false -tags mygo_noinspector
RELEASE_LDFLAGS := -s -w -X db-g/app.buildVersion=$(VERSION) -X db-g/app.releaseBuild=1 -X github.com/egoist/mygo.production=1

# 交互窗口（本机 cgo 链接有 .sframe 问题，统一关掉 cgo）
run:
	CGO_ENABLED=0 go1.27 run .

# 分发物就是这个静态二进制，不做 AppImage/Deb/安装器
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS)"

# Release 附件：Windows / Linux 各一个静态可执行文件
release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS)" -o dist/db-g-$(VERSION)-linux-amd64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS) -H=windowsgui" -o dist/db-g-$(VERSION)-windows-amd64.exe .

install:
	go1.27 install

# -p 1：避免并行包争 CPU 让编辑器/结果网格基准超限
test:
	CGO_ENABLED=0 go1.27 test -p 1 ./...

vet:
	CGO_ENABLED=0 go1.27 vet ./...
	go1.27 tool mygo vet .
