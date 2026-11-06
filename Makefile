.PHONY: run build install test vet

# 交互窗口（本机 cgo 链接有 .sframe 问题，统一关掉 cgo）
run:
	CGO_ENABLED=0 go1.27 run .

# 分发物就是这个静态二进制，不做 AppImage/Deb/安装器
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build

install:
	go1.27 install

# -p 1：避免并行包争 CPU 让编辑器/结果网格基准超限
test:
	CGO_ENABLED=0 go1.27 test -p 1 ./...

vet:
	CGO_ENABLED=0 go1.27 vet ./...
	go1.27 tool mygo vet .
