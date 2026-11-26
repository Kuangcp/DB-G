.PHONY: run build release localRelease install test vet i18n-status i18n-check

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

# 本地发布构建：装到配置目录供自举启动用；version 文件格式与 app.InstallSelf 一致：
# [db-g] / build=10203 / version=1.2.3。build 只取 x.y.z（dev/commit hash 记 0）。
CONFIG_DIR ?= $(if $(XDG_CONFIG_HOME),$(XDG_CONFIG_HOME),$(HOME)/.config)/db-g
# Linux Local Release
lo:
	@mkdir -p "$(CONFIG_DIR)/bin"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build $(RELEASE_FLAGS) -ldflags "$(RELEASE_LDFLAGS)" -o "$(CONFIG_DIR)/bin/db-g" .
	@ver="$(VERSION)"; ver="$${ver#v}"; base="$${ver%%-*}"; \
	build=$$(printf '%s\n' "$$base" | awk -F. 'NF==3 && $$1 ~ /^[0-9]+$$/ && $$2 ~ /^[0-9]+$$/ && $$3 ~ /^[0-9]+$$/ && $$2 <= 99 && $$3 <= 99 { printf "%d", $$1*10000+$$2*100+$$3; next } { printf "0" }'); \
	printf '[db-g]\nbuild=%s\nversion=%s\n' "$$build" "$$ver" > "$(CONFIG_DIR)/version.tmp"; \
	chmod 0644 "$(CONFIG_DIR)/version.tmp"; \
	mv "$(CONFIG_DIR)/version.tmp" "$(CONFIG_DIR)/version"
	@printf 'installed %s -> %s\n' "$(VERSION)" "$(CONFIG_DIR)"

install:
	go1.27 install -ldflags "$(DEV_LDFLAGS)"

# -p 1：避免并行包争 CPU 让编辑器/结果网格基准超限
test:
	CGO_ENABLED=0 go1.27 test -p 1 ./...

vet:
	CGO_ENABLED=0 go1.27 vet ./...
	go1.27 tool mygo vet .

# 国际化：status 列出全部中文字面量，check 只报 ui/app 的未翻译项（CI 用）
i18n-status:
	CGO_ENABLED=0 go1.27 run ./tools/l10nscan -all

i18n-check:
	CGO_ENABLED=0 go1.27 run ./tools/l10nscan
	go1.27 tool mygo i18n check
