.PHONY: run build release install test vet

# 发布版本号：有 tag 用 tag（v1.0.0），否则 git describe / dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# 开发构建也注入版本号，设置窗口不显示 dev；releaseBuild 不注入 → 数据落在 db-g.dev
DEV_LDFLAGS := -X db-g/app.buildVersion=$(VERSION)
# 发布构建：去掉调试信息与 inspector，mygo 置为 production
RELEASE_FLAGS := -trimpath -buildvcs=false -tags mygo_noinspector
RELEASE_LDFLAGS := -s -w -X db-g/app.buildVersion=$(VERSION) -X db-g/app.releaseBuild=1 -X github.com/egoist/mygo.production=1

# 交互窗口（本机 cgo 链接有 .sframe 问题，统一关掉 cgo）
run:
	CGO_ENABLED=0 go1.27 run -ldflags "$(DEV_LDFLAGS)" .

# 分发物就是这个静态二进制，不做 AppImage/Deb/安装器
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS)"

# 发布产物：安装包 + 签名更新清单（需要 MYGO_UPDATER_PRIVATE_KEY）
release:
	CGO_ENABLED=0 MYGO_LDFLAGS="-X db-g/app.buildVersion=$(VERSION) -X db-g/app.releaseBuild=1 -X db-g/app.updateBuild=1" \
		go tool mygo build -platform linux/amd64,windows/amd64

localRelease:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS)" -o ~/.config/db-g/bin/db-g .

install:
	go1.27 install -ldflags "$(DEV_LDFLAGS)"

# -p 1：避免并行包争 CPU 让编辑器/结果网格基准超限
test:
	CGO_ENABLED=0 go1.27 test -p 1 ./...

vet:
	CGO_ENABLED=0 go1.27 vet ./...
	go1.27 tool mygo vet .
